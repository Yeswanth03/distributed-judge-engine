// Package handler is the User Service's HTTP API (see docs/contracts.md section 7).
package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/oauth"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/password"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/repository"
)

// Store is the part of the repository the handlers use.
type Store interface {
	CreateUserWithSession(ctx context.Context, u *model.User, s *model.Session, refreshHash []byte) error
	GetUserByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	GetUserByEmail(ctx context.Context, email string) (*model.User, error)
	RecordFailedLogin(ctx context.Context, id uuid.UUID, now time.Time, maxFailures int, lockFor time.Duration) error
	ResetFailedLogins(ctx context.Context, id uuid.UUID) error
	UpdateProfile(ctx context.Context, id uuid.UUID, displayName string) (*model.User, error)
	SetRole(ctx context.Context, id uuid.UUID, role model.Role) (*model.User, error)
	SetStatus(ctx context.Context, id uuid.UUID, status model.Status) (*model.User, error)
	ListUsers(ctx context.Context, limit int, after *repository.UserCursor) ([]model.User, error)

	CreateSession(ctx context.Context, s *model.Session, refreshHash []byte) error
	RotateRefreshToken(ctx context.Context, oldHash, newHash []byte, now time.Time) (*model.Session, *model.User, error)
	RevokeSessionByToken(ctx context.Context, tokenHash []byte, now time.Time) (bool, error)
	ListActiveSessions(ctx context.Context, userID uuid.UUID, now time.Time) ([]model.Session, error)
	RevokeSession(ctx context.Context, userID, sessionID uuid.UUID, now time.Time) error
}

// Tokens signs access tokens and publishes the public keys (token.Issuer).
type Tokens interface {
	Issue(u *model.User, sessionID uuid.UUID) (string, time.Time, error)
	JWKS() []byte
}

// ReadinessCheck is one dependency /readyz verifies, such as the database or the broker.
type ReadinessCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

type Options struct {
	SessionTTL      time.Duration
	MaxFailedLogins int
	LoginLockout    time.Duration
	PasswordParams  password.Params
	CORSOrigin      string
	Readiness       []ReadinessCheck
	// Providers enables sign-in with Google or GitHub, keyed by name; AllowedRedirects are the origins a
	// sign-in may send the browser back to.
	Providers        map[string]oauth.Provider
	AllowedRedirects []string
	Logger           *slog.Logger
}

type Handler struct {
	store  Store
	oauth  OAuthStore
	tokens Tokens
	opts   Options
	log    *slog.Logger
	now    func() time.Time
}

// New wires the handlers. The repository serves as both stores.
func New(store interface {
	Store
	OAuthStore
}, tokens Tokens, opts Options) *Handler {
	h := &Handler{store: store, oauth: store, tokens: tokens, opts: opts, log: opts.Logger, now: time.Now}
	if h.log == nil {
		h.log = slog.Default()
	}
	if h.opts.PasswordParams == (password.Params{}) {
		h.opts.PasswordParams = password.Default
	}
	return h
}

// Routes returns the full HTTP handler: API routes, probes and metrics, wrapped in the middleware chain.
func (h *Handler) Routes(metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	// Public: the gateway lets these through without a token.
	mux.HandleFunc("POST /auth/register", h.register)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/refresh", h.refresh)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("GET /.well-known/jwks.json", h.jwks)
	mux.HandleFunc("GET /auth/oauth/providers", h.oauthProviders)
	mux.HandleFunc("GET /auth/oauth/{provider}/start", h.oauthStart)
	mux.HandleFunc("GET /auth/oauth/{provider}/callback", h.oauthCallback)
	mux.HandleFunc("POST /auth/oauth/exchange", h.oauthExchange)
	// Signed in: the gateway has verified the access token and set X-User-Id and X-User-Role.
	mux.HandleFunc("GET /users/me", h.me)
	mux.HandleFunc("PATCH /users/me", h.updateMe)
	mux.HandleFunc("GET /users/me/sessions", h.mySessions)
	mux.HandleFunc("DELETE /users/me/sessions/{id}", h.revokeMySession)
	// Admins only.
	mux.HandleFunc("GET /users", h.listUsers)
	mux.HandleFunc("PATCH /users/{id}/role", h.setRole)
	mux.HandleFunc("PATCH /users/{id}/status", h.setStatus)

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
	UserID    uuid.UUID
	IsAdmin   bool
	SessionID uuid.UUID // uuid.Nil if the gateway did not forward it
}

// CallerFrom reads the identity the gateway forwards after verifying the access token. The gateway strips any
// X-User-* and X-Session-Id headers a client sent itself, so these can be trusted (the same model as every
// other service).
func CallerFrom(r *http.Request) (Caller, bool) {
	id, err := uuid.Parse(r.Header.Get("X-User-Id"))
	if err != nil {
		return Caller{}, false
	}
	sid, _ := uuid.Parse(r.Header.Get("X-Session-Id"))
	return Caller{UserID: id, IsAdmin: r.Header.Get("X-User-Role") == string(model.RoleAdmin), SessionID: sid}, true
}

// clientInfo is what a session remembers about the device: the user agent, and the client's address as the
// gateway saw it. The gateway appends that address as the LAST X-Forwarded-For entry; anything before it was
// sent by the client and can't be trusted.
func clientInfo(r *http.Request) (userAgent, ip string) {
	userAgent = r.UserAgent()
	if len(userAgent) > 255 {
		userAgent = userAgent[:255]
	}
	if fwd := r.Header.Values("X-Forwarded-For"); len(fwd) > 0 {
		hops := strings.Split(fwd[len(fwd)-1], ",")
		ip = strings.TrimSpace(hops[len(hops)-1])
	} else if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	}
	return userAgent, ip
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store") // responses carry tokens and personal data
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

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, what string, err error) {
	h.log.ErrorContext(r.Context(), what+" failed", "error", err)
	WriteError(w, http.StatusInternalServerError, "internal_error", "something went wrong, please retry")
}

// decode reads a small JSON body, rejecting unknown fields so typos fail loudly.
func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "malformed request body")
		return false
	}
	return true
}

// userView is the JSON shape of an account in API responses. It never includes credentials.
type userView struct {
	UserID        uuid.UUID    `json:"user_id"`
	Email         string       `json:"email"`
	Username      string       `json:"username"`
	DisplayName   string       `json:"display_name"`
	Role          model.Role   `json:"role"`
	Status        model.Status `json:"status"`
	EmailVerified bool         `json:"email_verified"`
	CreatedAt     time.Time    `json:"created_at"`
}

func viewOf(u *model.User) userView {
	return userView{UserID: u.ID, Email: u.Email, Username: u.Username, DisplayName: u.DisplayName, Role: u.Role,
		Status: u.Status, EmailVerified: u.EmailVerified, CreatedAt: u.CreatedAt.UTC()}
}
