package handler

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/repository"
)

// get returns one submission. Someone else's submission answers 404, not 403, so IDs cannot be probed.
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	caller, ok := CallerFrom(r)
	if !ok {
		unauthorized(w)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "submission not found")
		return
	}
	s, err := h.store.GetSubmissionByID(r.Context(), id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && !caller.CanSee(s)) {
		WriteError(w, http.StatusNotFound, "not_found", "submission not found")
		return
	}
	if err != nil {
		h.internalError(w, r, "get submission", err)
		return
	}
	writeJSON(w, http.StatusOK, viewOf(s, r.URL.Query().Get("include") == "code"))
}

type listResponse struct {
	Items      []submissionView `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// list returns the caller's own submissions, newest first, optionally for one problem.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	caller, ok := CallerFrom(r)
	if !ok {
		unauthorized(w)
		return
	}
	q := r.URL.Query()
	f := repository.ListFilter{UserID: caller.UserID, ProblemID: q.Get("problem_id"), Limit: defaultListLimit}
	if f.ProblemID != "" && !validSlug(f.ProblemID) {
		WriteError(w, http.StatusBadRequest, "bad_request", "invalid value for problem_id")
		return
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxListLimit {
			WriteError(w, http.StatusBadRequest, "bad_request", "limit must be between 1 and 100")
			return
		}
		f.Limit = n
	}
	if v := q.Get("cursor"); v != "" {
		c, err := decodeCursor(v)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid value for cursor")
			return
		}
		f.After = &c
	}

	// One extra row tells us whether there is a next page.
	want := f.Limit
	f.Limit++
	rows, err := h.store.ListSubmissions(r.Context(), f)
	if err != nil {
		h.internalError(w, r, "list submissions", err)
		return
	}
	resp := listResponse{Items: make([]submissionView, 0, min(len(rows), want))}
	if len(rows) > want {
		rows = rows[:want]
		last := rows[want-1]
		next := encodeCursor(repository.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		resp.NextCursor = &next
	}
	for i := range rows {
		resp.Items = append(resp.Items, viewOf(&rows[i], false))
	}
	writeJSON(w, http.StatusOK, resp)
}

// Cursors are opaque to clients: base64url("<created_at RFC3339Nano>|<id>").
func encodeCursor(c repository.Cursor) string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (repository.Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return repository.Cursor{}, err
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return repository.Cursor{}, errors.New("malformed cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return repository.Cursor{}, err
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return repository.Cursor{}, err
	}
	return repository.Cursor{CreatedAt: t, ID: u}, nil
}
