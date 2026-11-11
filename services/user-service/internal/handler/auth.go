package handler

import (
	"errors"
	"math"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/password"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/repository"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/token"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,30}$`)

const (
	minPasswordLen = 8   // NIST SP 800-63B
	maxPasswordLen = 128 // bounds the hashing cost of a single request
	maxDisplayName = 60
)

// tokenResponse is what register, login and refresh answer with.
type tokenResponse struct {
	AccessToken      string    `json:"access_token"`
	TokenType        string    `json:"token_type"`
	ExpiresIn        int       `json:"expires_in"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresIn int       `json:"refresh_expires_in"`
	User             *userView `json:"user,omitempty"`
}

// issue signs an access token for u and packs it with the refresh token.
func (h *Handler) issue(u *model.User, s *model.Session, refreshToken string, withUser bool) (tokenResponse, error) {
	access, exp, err := h.tokens.Issue(u, s.ID)
	if err != nil {
		return tokenResponse{}, err
	}
	now := h.now()
	resp := tokenResponse{
		AccessToken: access, TokenType: "Bearer", ExpiresIn: int(math.Ceil(exp.Sub(now).Seconds())),
		RefreshToken: refreshToken, RefreshExpiresIn: int(math.Ceil(s.ExpiresAt.Sub(now).Seconds())),
	}
	if withUser {
		v := viewOf(u)
		resp.User = &v
	}
	return resp, nil
}

// newSession starts a session for the user on this device and makes its first refresh token.
func (h *Handler) newSession(r *http.Request, userID uuid.UUID) (*model.Session, string, []byte, error) {
	refresh, hash, err := token.NewRefreshToken()
	if err != nil {
		return nil, "", nil, err
	}
	ua, ip := clientInfo(r)
	now := h.now().UTC().Truncate(time.Microsecond)
	s := &model.Session{ID: uuid.New(), UserID: userID, UserAgent: ua, IP: ip, CreatedAt: now, LastUsedAt: now,
		ExpiresAt: now.Add(h.opts.SessionTTL)}
	return s, refresh, hash, nil
}

type registerRequest struct {
	Email       string `json:"email"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// register creates an account and signs it in. Every account starts as a plain user: admins are made by
// another admin (or the bootstrap settings), never by registering.
func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decode(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}

	var problems []string
	if !validEmail(req.Email) {
		problems = append(problems, "email must be a valid address")
	}
	if !usernamePattern.MatchString(req.Username) {
		problems = append(problems, "username must be 3 to 30 letters, digits or underscores")
	}
	if n := utf8.RuneCountInString(req.DisplayName); n > maxDisplayName {
		problems = append(problems, "display_name must be at most 60 characters")
	}
	problems = append(problems, passwordProblems(req.Password, req.Email, req.Username)...)
	if len(problems) > 0 {
		WriteError(w, http.StatusUnprocessableEntity, "validation_failed", strings.Join(problems, "; "))
		return
	}

	hash, err := password.Hash(req.Password, h.opts.PasswordParams)
	if err != nil {
		h.internalError(w, r, "hash password", err)
		return
	}
	u := &model.User{ID: uuid.New(), Email: req.Email, Username: req.Username, DisplayName: req.DisplayName,
		PasswordHash: &hash, Role: model.RoleUser, Status: model.StatusActive,
		CreatedAt: h.now().UTC().Truncate(time.Microsecond)}
	s, refresh, refreshHash, err := h.newSession(r, u.ID)
	if err != nil {
		h.internalError(w, r, "start session", err)
		return
	}
	switch err := h.store.CreateUserWithSession(r.Context(), u, s, refreshHash); {
	case errors.Is(err, repository.ErrEmailTaken):
		WriteError(w, http.StatusConflict, "email_taken", "an account with this email already exists")
		return
	case errors.Is(err, repository.ErrUsernameTaken):
		WriteError(w, http.StatusConflict, "username_taken", "this username is taken")
		return
	case err != nil:
		h.internalError(w, r, "create user", err)
		return
	}
	resp, err := h.issue(u, s, refresh, true)
	if err != nil {
		h.internalError(w, r, "issue token", err)
		return
	}
	h.log.InfoContext(r.Context(), "user registered", "user_id", u.ID, "session_id", s.ID)
	writeJSON(w, http.StatusCreated, resp)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// errBadLogin is the one answer for every way a password login can fail, so it reveals nothing about which
// emails exist or which accounts are locked.
func errBadLogin(w http.ResponseWriter) {
	WriteError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Email == "" || req.Password == "" || len(req.Password) > 4*maxPasswordLen {
		errBadLogin(w)
		return
	}
	now := h.now()
	u, err := h.store.GetUserByEmail(r.Context(), strings.TrimSpace(req.Email))
	if errors.Is(err, repository.ErrNotFound) {
		password.Burn(req.Password) // as slow as a real check: timing must not reveal unknown emails
		errBadLogin(w)
		return
	}
	if err != nil {
		h.internalError(w, r, "find user", err)
		return
	}
	if u.PasswordHash == nil || (u.LockedUntil != nil && now.Before(*u.LockedUntil)) {
		password.Burn(req.Password)
		errBadLogin(w)
		return
	}
	ok, err := password.Verify(req.Password, *u.PasswordHash)
	if err != nil {
		h.internalError(w, r, "verify password", err)
		return
	}
	if !ok {
		if err := h.store.RecordFailedLogin(r.Context(), u.ID, now, h.opts.MaxFailedLogins, h.opts.LoginLockout); err != nil {
			h.log.ErrorContext(r.Context(), "recording failed login failed", "error", err)
		}
		h.log.WarnContext(r.Context(), "failed login", "user_id", u.ID)
		errBadLogin(w)
		return
	}
	// Only now, with the right password, may the answer say more.
	if u.Status != model.StatusActive {
		WriteError(w, http.StatusForbidden, "account_disabled", "this account is disabled")
		return
	}
	if err := h.store.ResetFailedLogins(r.Context(), u.ID); err != nil {
		h.log.ErrorContext(r.Context(), "resetting failed logins failed", "error", err)
	}
	s, refresh, refreshHash, err := h.newSession(r, u.ID)
	if err == nil {
		err = h.store.CreateSession(r.Context(), s, refreshHash)
	}
	if err != nil {
		h.internalError(w, r, "start session", err)
		return
	}
	resp, err := h.issue(u, s, refresh, true)
	if err != nil {
		h.internalError(w, r, "issue token", err)
		return
	}
	h.log.InfoContext(r.Context(), "user logged in", "user_id", u.ID, "session_id", s.ID)
	writeJSON(w, http.StatusOK, resp)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// refresh swaps a refresh token for a new access token and a new refresh token. The access token carries the
// user's current role, so a role change takes effect at the next refresh at the latest.
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !decode(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		WriteError(w, http.StatusUnauthorized, "invalid_refresh_token", "sign in again")
		return
	}
	next, nextHash, err := token.NewRefreshToken()
	if err != nil {
		h.internalError(w, r, "make refresh token", err)
		return
	}
	s, u, err := h.store.RotateRefreshToken(r.Context(), token.HashRefreshToken(req.RefreshToken), nextHash, h.now())
	switch {
	case errors.Is(err, repository.ErrRefreshTokenReused):
		h.log.WarnContext(r.Context(), "refresh token reused, session revoked")
		WriteError(w, http.StatusUnauthorized, "invalid_refresh_token", "sign in again")
		return
	case errors.Is(err, repository.ErrInvalidRefreshToken), errors.Is(err, repository.ErrNotFound):
		WriteError(w, http.StatusUnauthorized, "invalid_refresh_token", "sign in again")
		return
	case err != nil:
		h.internalError(w, r, "refresh", err)
		return
	}
	resp, err := h.issue(u, s, next, false)
	if err != nil {
		h.internalError(w, r, "issue token", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// logout ends the session the refresh token belongs to. It always answers 204, even for an unknown token:
// signing out twice is not an error, and the answer must not confirm which tokens exist.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !decode(w, r, &req) {
		return
	}
	if req.RefreshToken != "" {
		if _, err := h.store.RevokeSessionByToken(r.Context(), token.HashRefreshToken(req.RefreshToken), h.now()); err != nil {
			h.internalError(w, r, "logout", err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// jwks serves the public keys the gateway verifies access tokens with. Caches may keep it for 5 minutes;
// keys are rotated with an overlap far longer than that.
func (h *Handler) jwks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write(h.tokens.JWKS()) //nolint:errcheck
}

func validEmail(s string) bool {
	if len(s) > 254 {
		return false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || addr.Name != "" {
		return false
	}
	at := strings.LastIndex(s, "@")
	return at > 0 && strings.Contains(s[at+1:], ".")
}

// passwordProblems applies NIST's rules: a length range, and nothing that is just the account's own name.
// No composition rules (a digit, a symbol...): they make passwords harder to remember, not harder to guess.
func passwordProblems(pw, email, username string) []string {
	n := utf8.RuneCountInString(pw)
	switch {
	case n < minPasswordLen:
		return []string{"password must be at least 8 characters"}
	case n > maxPasswordLen:
		return []string{"password must be at most 128 characters"}
	case strings.EqualFold(pw, email) || strings.EqualFold(pw, username):
		return []string{"password must not be your email or username"}
	}
	return nil
}
