package model

import (
	"time"

	"github.com/google/uuid"
)

// RunStatus is the lifecycle of a trial run: QUEUED until the worker's result arrives (COMPLETED), or FAILED
// when the janitor gives up on it.
type RunStatus string

const (
	RunQueued    RunStatus = "QUEUED"
	RunCompleted RunStatus = "COMPLETED"
	RunFailed    RunStatus = "FAILED"
)

// EventRunRequested is the outbox event type of a trial run. The relay sends it to runs.queue, not jobs.queue.
const EventRunRequested = "RUN_REQUESTED"

// Run is a trial run: code executed against a problem's sample cases or the caller's own inputs. It is never
// scored, never reaches Catalog and is deleted after a while.
type Run struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	ProblemID    string
	Language     string
	Status       RunStatus
	Result       []byte // the worker's result message, JSON, as received; nil until COMPLETED
	ErrorMessage *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// RunJobMessage is published to runs.queue for the Judge Worker. With no Inputs the worker runs the problem's
// sample cases; otherwise it runs each input instead (see docs/contracts.md).
type RunJobMessage struct {
	RunID       uuid.UUID `json:"run_id"`
	UserID      uuid.UUID `json:"user_id"`
	ProblemID   string    `json:"problem_id"`
	Language    string    `json:"language"`
	Code        string    `json:"code"`
	Inputs      []string  `json:"inputs"`
	RequestedAt time.Time `json:"requested_at"`
}
