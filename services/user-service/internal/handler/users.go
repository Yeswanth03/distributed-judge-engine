package handler

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/repository"
)

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	caller, ok := CallerFrom(r)
	if !ok {
		unauthorized(w)
		return
	}
	u, err := h.store.GetUserByID(r.Context(), caller.UserID)
	if errors.Is(err, repository.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		h.internalError(w, r, "get user", err)
		return
	}
	writeJSON(w, http.StatusOK, viewOf(u))
}

type updateMeRequest struct {
	DisplayName *string `json:"display_name"`
}

func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	caller, ok := CallerFrom(r)
	if !ok {
		unauthorized(w)
		return
	}
	var req updateMeRequest
	if !decode(w, r, &req) {
		return
	}
	if req.DisplayName == nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation_failed", "nothing to update: send display_name")
		return
	}
	name := strings.TrimSpace(*req.DisplayName)
	if name == "" || utf8.RuneCountInString(name) > maxDisplayName {
		WriteError(w, http.StatusUnprocessableEntity, "validation_failed", "display_name must be 1 to 60 characters")
		return
	}
	u, err := h.store.UpdateProfile(r.Context(), caller.UserID, name)
	if errors.Is(err, repository.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		h.internalError(w, r, "update profile", err)
		return
	}
	writeJSON(w, http.StatusOK, viewOf(u))
}

type sessionView struct {
	SessionID  uuid.UUID `json:"session_id"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"` // the session the request's access token belongs to
}

func (h *Handler) mySessions(w http.ResponseWriter, r *http.Request) {
	caller, ok := CallerFrom(r)
	if !ok {
		unauthorized(w)
		return
	}
	sessions, err := h.store.ListActiveSessions(r.Context(), caller.UserID, h.now())
	if err != nil {
		h.internalError(w, r, "list sessions", err)
		return
	}
	items := make([]sessionView, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, sessionView{SessionID: s.ID, UserAgent: s.UserAgent, IP: s.IP,
			CreatedAt: s.CreatedAt.UTC(), LastUsedAt: s.LastUsedAt.UTC(), ExpiresAt: s.ExpiresAt.UTC(),
			Current: s.ID == caller.SessionID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// revokeMySession signs one of the caller's devices out. Its access token keeps working until it expires
// (at most ACCESS_TOKEN_TTL); it just can't be refreshed.
func (h *Handler) revokeMySession(w http.ResponseWriter, r *http.Request) {
	caller, ok := CallerFrom(r)
	if !ok {
		unauthorized(w)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	err = h.store.RevokeSession(r.Context(), caller.UserID, id, h.now())
	if errors.Is(err, repository.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	if err != nil {
		h.internalError(w, r, "revoke session", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// admin answers 401/403 unless the gateway says the caller is an admin, and returns the caller.
func admin(w http.ResponseWriter, r *http.Request) (Caller, bool) {
	caller, ok := CallerFrom(r)
	if !ok {
		unauthorized(w)
		return Caller{}, false
	}
	if !caller.IsAdmin {
		WriteError(w, http.StatusForbidden, "forbidden", "admin role required")
		return Caller{}, false
	}
	return caller, true
}

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := admin(w, r); !ok {
		return
	}
	q := r.URL.Query()
	limit := defaultListLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxListLimit {
			WriteError(w, http.StatusBadRequest, "bad_request", "limit must be between 1 and 100")
			return
		}
		limit = n
	}
	var after *repository.UserCursor
	if v := q.Get("cursor"); v != "" {
		c, ok := decodeCursor(v)
		if !ok {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid cursor")
			return
		}
		after = &c
	}
	users, err := h.store.ListUsers(r.Context(), limit+1, after)
	if err != nil {
		h.internalError(w, r, "list users", err)
		return
	}
	var next *string
	if len(users) > limit {
		users = users[:limit]
		last := users[limit-1]
		c := encodeCursor(repository.UserCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		next = &c
	}
	items := make([]userView, 0, len(users))
	for i := range users {
		items = append(items, viewOf(&users[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

type roleRequest struct {
	Role model.Role `json:"role"`
}

func (h *Handler) setRole(w http.ResponseWriter, r *http.Request) {
	caller, ok := admin(w, r)
	if !ok {
		return
	}
	id, ok := h.targetUser(w, r, caller)
	if !ok {
		return
	}
	var req roleRequest
	if !decode(w, r, &req) {
		return
	}
	if !req.Role.Valid() {
		WriteError(w, http.StatusUnprocessableEntity, "validation_failed", "role must be user or admin")
		return
	}
	h.respondUpdated(w, r, "set role", func() (*model.User, error) {
		u, err := h.store.SetRole(r.Context(), id, req.Role)
		if err == nil {
			h.log.InfoContext(r.Context(), "role set", "admin_id", caller.UserID, "user_id", id, "role", req.Role)
		}
		return u, err
	})
}

type statusRequest struct {
	Status model.Status `json:"status"`
}

// setStatus enables or disables an account. Disabling signs the user out everywhere: their sessions can't be
// refreshed, and their current access token expires within ACCESS_TOKEN_TTL.
func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
	caller, ok := admin(w, r)
	if !ok {
		return
	}
	id, ok := h.targetUser(w, r, caller)
	if !ok {
		return
	}
	var req statusRequest
	if !decode(w, r, &req) {
		return
	}
	if !req.Status.Valid() {
		WriteError(w, http.StatusUnprocessableEntity, "validation_failed", "status must be active or disabled")
		return
	}
	h.respondUpdated(w, r, "set status", func() (*model.User, error) {
		u, err := h.store.SetStatus(r.Context(), id, req.Status)
		if err == nil {
			h.log.InfoContext(r.Context(), "status set", "admin_id", caller.UserID, "user_id", id, "status", req.Status)
		}
		return u, err
	})
}

// targetUser parses the {id} of an admin change. An admin can't change their own role or status, so the last
// admin can't lock everyone out by accident.
func (h *Handler) targetUser(w http.ResponseWriter, r *http.Request, caller Caller) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "user not found")
		return uuid.Nil, false
	}
	if id == caller.UserID {
		WriteError(w, http.StatusConflict, "cannot_change_self", "ask another admin to change your own role or status")
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) respondUpdated(w http.ResponseWriter, r *http.Request, what string, update func() (*model.User, error)) {
	u, err := update()
	if errors.Is(err, repository.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		h.internalError(w, r, what, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOf(u))
}

// Cursors are opaque to clients: base64 of "<created_at unix nanos>|<id>".
func encodeCursor(c repository.UserCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(c.CreatedAt.UnixNano(), 10) + "|" + c.ID.String()))
}

func decodeCursor(s string) (repository.UserCursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return repository.UserCursor{}, false
	}
	ts, id, found := strings.Cut(string(raw), "|")
	if !found {
		return repository.UserCursor{}, false
	}
	nanos, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return repository.UserCursor{}, false
	}
	uid, err := uuid.Parse(id)
	if err != nil {
		return repository.UserCursor{}, false
	}
	return repository.UserCursor{CreatedAt: time.Unix(0, nanos).UTC(), ID: uid}, true
}
