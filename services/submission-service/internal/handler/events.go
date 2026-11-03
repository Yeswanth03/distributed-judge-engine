package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/repository"
)

// rank orders statuses so a stream only ever moves forward, whatever order duplicates arrive in.
func rank(s model.Status) int {
	switch s {
	case model.StatusQueued:
		return 1
	case model.StatusRunning:
		return 2
	case model.StatusCompleted:
		return 3
	}
	return 0
}

type statusData struct {
	SubmissionID uuid.UUID    `json:"submission_id"`
	Status       model.Status `json:"status"`
}

type verdictData struct {
	SubmissionID    uuid.UUID      `json:"submission_id"`
	Status          model.Status   `json:"status"`
	Verdict         *model.Verdict `json:"verdict"`
	ExecutionTimeMS int            `json:"execution_time_ms"`
	MemoryKB        int            `json:"memory_kb"`
	TestCasesPassed int            `json:"test_cases_passed"`
	TotalTestCases  int            `json:"total_test_cases"`
	ErrorMessage    *string        `json:"error_message"`
}

// events streams a submission's lifecycle as Server-Sent Events (docs/contracts.md §4): the current state
// first, then every forward change, ending after the verdict.
func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
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

	// Subscribe before reading the current state: an event that lands in between is then buffered rather
	// than lost. Reading first would leave a gap where the verdict could slip past unseen.
	sub := h.opts.Hub.Subscribe(id)
	defer h.opts.Hub.Unsubscribe(sub)

	s, err := h.store.GetSubmissionByID(r.Context(), id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && !caller.CanSee(s)) {
		WriteError(w, http.StatusNotFound, "not_found", "submission not found")
		return
	}
	if err != nil {
		h.internalError(w, r, "get submission", err)
		return
	}

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no") // tell nginx-style proxies not to buffer the stream
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)

	last := 0
	// send writes ev if it moves the stream forward. It reports whether the stream should end.
	send := func(ev model.StatusEvent) (done bool, err error) {
		if rank(ev.Status) <= last {
			return false, nil // duplicate, or an older event arriving late
		}
		last = rank(ev.Status)
		if ev.Status == model.StatusCompleted {
			err = writeEvent(w, "verdict", verdictData{
				SubmissionID: ev.SubmissionID, Status: ev.Status, Verdict: ev.Verdict,
				ExecutionTimeMS: ev.ExecutionTimeMS, MemoryKB: ev.MemoryKB, TestCasesPassed: ev.TestCasesPassed,
				TotalTestCases: ev.TotalTestCases, ErrorMessage: ev.ErrorMessage,
			})
			done = true
		} else {
			err = writeEvent(w, "status", statusData{SubmissionID: ev.SubmissionID, Status: ev.Status})
		}
		if err == nil {
			err = rc.Flush()
		}
		return done, err
	}

	if done, err := send(model.EventFromSubmission(s)); done || err != nil {
		return
	}

	heartbeat := time.NewTicker(h.opts.Heartbeat)
	defer heartbeat.Stop()
	for {
		var ev model.StatusEvent
		select {
		case <-r.Context().Done():
			return // client went away
		case <-h.opts.Hub.Done():
			return // server shutting down; EventSource reconnects (to another replica) and gets the state
		case <-heartbeat.C:
			// A comment line: ignored by EventSource, but keeps proxies from closing an idle connection.
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
			continue
		case m := <-sub.C:
			if m.Event != nil {
				ev = *m.Event
				break
			}
			// Resync: events may have been missed, so read the current state again.
			fresh, err := h.store.GetSubmissionByID(r.Context(), id)
			if err != nil {
				h.log.WarnContext(r.Context(), "sse resync failed", "submission_id", id, "error", err)
				continue
			}
			ev = model.EventFromSubmission(fresh)
		}
		if done, err := send(ev); done || err != nil {
			return
		}
	}
}

func writeEvent(w http.ResponseWriter, name string, data any) error {
	b, err := json.Marshal(data) // single line: json.Marshal never emits raw newlines
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
	return err
}
