package handler

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type middleware func(http.Handler) http.Handler

// chain wraps h so that the first middleware listed is the outermost.
func chain(h http.Handler, mws ...middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

type ctxKey int

const requestIDKey ctxKey = 0

// Accept the gateway's request ID if it looks sane, so one ID follows a request across services.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !requestIDPattern.MatchString(id) {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// NewLogHandler wraps a slog handler so every record logged with a request's context carries its request_id.
func NewLogHandler(h slog.Handler) slog.Handler {
	return requestIDLogHandler{h}
}

type requestIDLogHandler struct{ slog.Handler }

func (h requestIDLogHandler) Handle(ctx context.Context, rec slog.Record) error {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		rec.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, rec)
}

func (h requestIDLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return requestIDLogHandler{h.Handler.WithAttrs(attrs)}
}

func (h requestIDLogHandler) WithGroup(name string) slog.Handler {
	return requestIDLogHandler{h.Handler.WithGroup(name)}
}

// statusRecorder remembers the response status. It passes Flush through and supports Unwrap, so SSE
// streaming (which needs http.Flusher / http.ResponseController) still works behind the middleware.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *statusRecorder) code() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}

// route is the matched pattern (e.g. "GET /api/v1/submissions/{id}"), so logs and metrics do not explode
// into one series per submission ID. The mux sets r.Pattern on the request it is given.
func route(r *http.Request) string {
	if r.Pattern == "" || r.Pattern == "/" {
		return "unmatched"
	}
	return r.Pattern
}

func logRequests(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
				return // probes and scrapes every few seconds would drown the log
			}
			log.InfoContext(r.Context(), "http request", "method", r.Method, "path", r.URL.Path,
				"route", route(r), "status", rec.code(), "duration_ms", time.Since(start).Milliseconds())
		})
	}
}

func recoverPanics(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v) // the server's own signal to abort the response; let it through
					}
					log.ErrorContext(r.Context(), "panic serving request", "panic", v, "stack", string(debug.Stack()))
					WriteError(w, http.StatusInternalServerError, "internal_error", "something went wrong, please retry")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "HTTP requests by route and status code.",
	}, []string{"method", "route", "code"})
	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency by route (SSE streams last as long as the client stays connected).",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})
)

func recordMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		rt := route(r)
		httpRequests.WithLabelValues(r.Method, rt, strconv.Itoa(rec.code())).Inc()
		httpDuration.WithLabelValues(r.Method, rt).Observe(time.Since(start).Seconds())
	})
}

// cors lets a browser app on another origin call the API directly, which only happens in local development:
// in deployment the gateway serves the frontend and the API from one origin. An empty origin disables it.
func cors(origin string) middleware {
	return func(next http.Handler) http.Handler {
		if origin == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Expose-Headers", "Location, X-Request-Id")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				// X-User-* only matter without a gateway (local dev), where the frontend sends them itself.
				h.Set("Access-Control-Allow-Headers", "Content-Type, X-User-Id, X-User-Role, X-Request-Id")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
