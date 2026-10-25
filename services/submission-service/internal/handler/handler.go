// Package handler is the Submission Service's HTTP API (see docs/contracts.md §4).
package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/repository"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/sse"
)

// Store is the part of the repository the handlers use.
type Store interface {
	CreateSubmissionWithOutbox(ctx context.Context, s *model.Submission, ev *model.OutboxEvent) error
	GetSubmissionByID(ctx context.Context, id uuid.UUID) (*model.Submission, error)
	ListSubmissions(ctx context.Context, f repository.ListFilter) ([]model.Submission, error)
}

// ReadinessCheck is one dependency /readyz verifies, such as the database or the broker.
type ReadinessCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

type Options struct {
	MaxCodeBytes     int
	AllowedLanguages []string
	CORSOrigin       string
	// NudgeOutbox wakes the outbox relay after a submission is stored, so it does not wait for its next poll.
	// Optional; it must not block.
	NudgeOutbox func()
	Readiness   []ReadinessCheck
	// Hub feeds GET /api/v1/submissions/{id}/events (SSE); without one the route is not served.
	Hub *sse.Hub
	// Heartbeat is how often an idle SSE stream sends a keep-alive comment (default 15s).
	Heartbeat time.Duration
	Logger    *slog.Logger
}

type Handler struct {
	store     Store
	opts      Options
	languages map[string]bool
	log       *slog.Logger
	now       func() time.Time
}

func New(store Store, opts Options) *Handler {
	h := &Handler{store: store, opts: opts, languages: map[string]bool{}, log: opts.Logger, now: time.Now}
	for _, l := range opts.AllowedLanguages {
		h.languages[l] = true
	}
	if h.log == nil {
		h.log = slog.Default()
	}
	if h.opts.NudgeOutbox == nil {
		h.opts.NudgeOutbox = func() {}
	}
	if h.opts.Heartbeat <= 0 {
		h.opts.Heartbeat = 15 * time.Second
	}
	return h
}

// Routes returns the full HTTP handler: API routes, probes and metrics, wrapped in the middleware chain.
func (h *Handler) Routes(metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/submissions", h.submit)
	mux.HandleFunc("GET /api/v1/submissions", h.list)
	mux.HandleFunc("GET /api/v1/submissions/{id}", h.get)
	if h.opts.Hub != nil {
		mux.HandleFunc("GET /api/v1/submissions/{id}/events", h.events)
	}
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	if metrics != nil {
		mux.Handle("GET /metrics", metrics)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found", "no such route")
	})

	return chain(mux,
		requestID,
		logRequests(h.log),
		recoverPanics(h.log),
		recordMetrics,
		cors(h.opts.CORSOrigin),
	)
}

// Caller is who the gateway says is making the request.
type Caller struct {
	UserID  uuid.UUID
	IsAdmin bool
}

// CallerFrom reads the identity the gateway forwards. The gateway has verified the login token and strips
// any X-User-* headers a client sent itself, so these headers can be trusted (same model as Catalog).
func CallerFrom(r *http.Request) (Caller, bool) {
	id, err := uuid.Parse(r.Header.Get("X-User-Id"))
	if err != nil {
		return Caller{}, false
	}
	return Caller{UserID: id, IsAdmin: r.Header.Get("X-User-Role") == "admin"}, true
}

// CanSee reports whether the caller may read the submission: their own, or any if they are an admin.
func (c Caller) CanSee(s *model.Submission) bool {
	return c.IsAdmin || s.UserID == c.UserID
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body) //nolint:errcheck // the client is gone if this fails
}

// WriteError writes the error body every service in the project uses: {"error": code, "message": text}.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func unauthorized(w http.ResponseWriter) {
	WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid X-User-Id")
}

// submissionView is the JSON shape of a submission in API responses.
type submissionView struct {
	SubmissionID    uuid.UUID      `json:"submission_id"`
	UserID          uuid.UUID      `json:"user_id"`
	ProblemID       string         `json:"problem_id"`
	Language        string         `json:"language"`
	Code            *string        `json:"code,omitempty"`
	Status          model.Status   `json:"status"`
	Verdict         *model.Verdict `json:"verdict"`
	ExecutionTimeMS int            `json:"execution_time_ms"`
	MemoryKB        int            `json:"memory_kb"`
	TestCasesPassed int            `json:"test_cases_passed"`
	TotalTestCases  int            `json:"total_test_cases"`
	ErrorMessage    *string        `json:"error_message"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func viewOf(s *model.Submission, withCode bool) submissionView {
	v := submissionView{
		SubmissionID: s.ID, UserID: s.UserID, ProblemID: s.ProblemID, Language: s.Language,
		Status: s.Status, Verdict: s.Verdict, ExecutionTimeMS: s.ExecutionTimeMS, MemoryKB: s.MemoryKB,
		TestCasesPassed: s.TestCasesPassed, TotalTestCases: s.TotalTestCases, ErrorMessage: s.ErrorMessage,
		CreatedAt: s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC(),
	}
	if withCode {
		code := s.Code
		v.Code = &code
	}
	return v
}
