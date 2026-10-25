package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

// Same rule as Catalog's problem slugs.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const maxSlugLength = 80

func validSlug(s string) bool {
	return len(s) <= maxSlugLength && slugPattern.MatchString(s)
}

type submitRequest struct {
	ProblemID string `json:"problem_id"`
	Language  string `json:"language"`
	Code      string `json:"code"`
}

type submitResponse struct {
	SubmissionID uuid.UUID    `json:"submission_id"`
	Status       model.Status `json:"status"`
	CreatedAt    time.Time    `json:"created_at"`
}

// submit stores the submission and its job message in one transaction and answers 202 straight away.
// Nothing here talks to RabbitMQ: the outbox relay ships the job, so a broker outage cannot lose it.
func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	caller, ok := CallerFrom(r)
	if !ok {
		unauthorized(w)
		return
	}

	// Room for the JSON around the code; the code itself is checked exactly below. JSON escaping can at most
	// sextuple a byte (\u00XX), but realistic code stays far below this.
	r.Body = http.MaxBytesReader(w, r.Body, int64(h.opts.MaxCodeBytes)*2+16*1024)
	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.codeTooLarge(w)
			return
		}
		WriteError(w, http.StatusBadRequest, "bad_request", "malformed request body")
		return
	}

	var problems []string
	if !validSlug(req.ProblemID) {
		problems = append(problems, "problem_id must be a problem slug like two-sum")
	}
	if !h.languages[req.Language] {
		problems = append(problems, fmt.Sprintf("language must be one of: %s", strings.Join(h.opts.AllowedLanguages, ", ")))
	}
	if strings.TrimSpace(req.Code) == "" {
		problems = append(problems, "code must not be empty")
	}
	if len(problems) > 0 {
		WriteError(w, http.StatusUnprocessableEntity, "validation_failed", strings.Join(problems, "; "))
		return
	}
	if len(req.Code) > h.opts.MaxCodeBytes {
		h.codeTooLarge(w)
		return
	}

	// The ID is chosen here, before the transaction, so it can go into the job payload and the response.
	// Postgres stores microseconds; truncating keeps the response equal to what a later GET returns.
	s := &model.Submission{
		ID:        uuid.New(),
		UserID:    caller.UserID,
		ProblemID: req.ProblemID,
		Language:  req.Language,
		Code:      req.Code,
		CreatedAt: h.now().UTC().Truncate(time.Microsecond),
	}
	payload, err := json.Marshal(model.JobMessage{
		SubmissionID: s.ID, UserID: s.UserID, ProblemID: s.ProblemID, Language: s.Language, Code: s.Code,
		SubmittedAt: s.CreatedAt,
	})
	if err != nil {
		h.internalError(w, r, "marshal job", err)
		return
	}
	ev := &model.OutboxEvent{AggregateID: s.ID, EventType: model.EventSubmissionCreated, Payload: payload}
	if err := h.store.CreateSubmissionWithOutbox(r.Context(), s, ev); err != nil {
		h.internalError(w, r, "store submission", err)
		return
	}
	h.opts.NudgeOutbox()

	h.log.InfoContext(r.Context(), "submission accepted",
		"submission_id", s.ID, "user_id", s.UserID, "problem_id", s.ProblemID, "code_bytes", len(s.Code))
	w.Header().Set("Location", "/api/v1/submissions/"+s.ID.String())
	writeJSON(w, http.StatusAccepted, submitResponse{SubmissionID: s.ID, Status: model.StatusQueued, CreatedAt: s.CreatedAt})
}

func (h *Handler) codeTooLarge(w http.ResponseWriter) {
	WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
		fmt.Sprintf("code must be at most %d bytes", h.opts.MaxCodeBytes))
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, what string, err error) {
	h.log.ErrorContext(r.Context(), what+" failed", "error", err)
	WriteError(w, http.StatusInternalServerError, "internal_error", "something went wrong, please retry")
}
