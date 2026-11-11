package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/repository"
)

// RunStore is the part of the repository the run endpoints use.
type RunStore interface {
	CreateRunWithOutbox(ctx context.Context, run *model.Run, ev *model.OutboxEvent) error
	GetRunByID(ctx context.Context, id uuid.UUID) (*model.Run, error)
	CountRunsSince(ctx context.Context, userID uuid.UUID, since time.Time) (int, error)
}

// maxRunInputs caps the custom inputs of one run; each one costs a sandbox execution.
const maxRunInputs = 10

type runRequest struct {
	ProblemID string   `json:"problem_id"`
	Language  string   `json:"language"`
	Code      string   `json:"code"`
	Inputs    []string `json:"inputs"`
}

type runResponse struct {
	RunID     uuid.UUID       `json:"run_id"`
	Status    model.RunStatus `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
}

// runView is the JSON shape of a run. Result is the worker's result message, passed through as it is.
type runView struct {
	RunID        uuid.UUID       `json:"run_id"`
	ProblemID    string          `json:"problem_id"`
	Language     string          `json:"language"`
	Status       model.RunStatus `json:"status"`
	Result       json.RawMessage `json:"result"`
	ErrorMessage *string         `json:"error_message"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// createRun queues a trial run of the caller's code, on the problem's sample cases or on their own inputs.
// Like a submission it goes through the outbox, but it is never scored and nothing outside this service and
// the Judge Worker ever sees it.
func (h *Handler) createRun(w http.ResponseWriter, r *http.Request) {
	caller, ok := CallerFrom(r)
	if !ok {
		unauthorized(w)
		return
	}

	// Room for the code, the inputs (together at most MaxCodeBytes) and the JSON around them.
	r.Body = http.MaxBytesReader(w, r.Body, int64(h.opts.MaxCodeBytes)*4+16*1024)
	var req runRequest
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
	problemID, ok := parseProblemID(req.ProblemID)
	if !ok {
		problems = append(problems, "problem_id must be the problem's UUID")
	}
	if !h.languages[req.Language] {
		problems = append(problems, fmt.Sprintf("language must be one of: %s", strings.Join(h.opts.AllowedLanguages, ", ")))
	}
	if strings.TrimSpace(req.Code) == "" {
		problems = append(problems, "code must not be empty")
	}
	if len(req.Inputs) > maxRunInputs {
		problems = append(problems, fmt.Sprintf("at most %d inputs per run", maxRunInputs))
	}
	inputBytes := 0
	for _, in := range req.Inputs {
		if strings.TrimSpace(in) == "" {
			problems = append(problems, "inputs must not be empty")
			break
		}
		inputBytes += len(in)
	}
	if len(problems) > 0 {
		WriteError(w, http.StatusUnprocessableEntity, "validation_failed", strings.Join(problems, "; "))
		return
	}
	if len(req.Code) > h.opts.MaxCodeBytes {
		h.codeTooLarge(w)
		return
	}
	if inputBytes > h.opts.MaxCodeBytes {
		WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("inputs must be at most %d bytes together", h.opts.MaxCodeBytes))
		return
	}

	now := h.now().UTC().Truncate(time.Microsecond)
	recent, err := h.opts.Runs.CountRunsSince(r.Context(), caller.UserID, now.Add(-time.Minute))
	if err != nil {
		h.internalError(w, r, "count recent runs", err)
		return
	}
	if recent >= h.opts.RunsPerMinute {
		w.Header().Set("Retry-After", strconv.Itoa(60))
		WriteError(w, http.StatusTooManyRequests, "too_many_requests",
			fmt.Sprintf("at most %d runs per minute; please wait and try again", h.opts.RunsPerMinute))
		return
	}

	run := &model.Run{ID: uuid.New(), UserID: caller.UserID, ProblemID: problemID, Language: req.Language, CreatedAt: now}
	payload, err := json.Marshal(model.RunJobMessage{
		RunID: run.ID, UserID: run.UserID, ProblemID: run.ProblemID, Language: run.Language, Code: req.Code,
		Inputs: req.Inputs, RequestedAt: run.CreatedAt,
	})
	if err != nil {
		h.internalError(w, r, "marshal run job", err)
		return
	}
	ev := &model.OutboxEvent{AggregateID: run.ID, EventType: model.EventRunRequested, Payload: payload}
	if err := h.opts.Runs.CreateRunWithOutbox(r.Context(), run, ev); err != nil {
		h.internalError(w, r, "store run", err)
		return
	}
	h.opts.NudgeOutbox()

	h.log.InfoContext(r.Context(), "run accepted", "run_id", run.ID, "user_id", run.UserID,
		"problem_id", run.ProblemID, "inputs", len(req.Inputs))
	w.Header().Set("Location", "/api/v1/runs/"+run.ID.String())
	writeJSON(w, http.StatusAccepted, runResponse{RunID: run.ID, Status: model.RunQueued, CreatedAt: run.CreatedAt})
}

// getRun answers with the run and, once it is COMPLETED, the worker's result. As with submissions, someone
// else's run answers 404, not 403.
func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	caller, ok := CallerFrom(r)
	if !ok {
		unauthorized(w)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	run, err := h.opts.Runs.GetRunByID(r.Context(), id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && !caller.IsAdmin && run.UserID != caller.UserID) {
		WriteError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	if err != nil {
		h.internalError(w, r, "get run", err)
		return
	}
	writeJSON(w, http.StatusOK, runView{
		RunID: run.ID, ProblemID: run.ProblemID, Language: run.Language, Status: run.Status,
		Result: json.RawMessage(run.Result), ErrorMessage: run.ErrorMessage,
		CreatedAt: run.CreatedAt.UTC(), UpdatedAt: run.UpdatedAt.UTC(),
	})
}
