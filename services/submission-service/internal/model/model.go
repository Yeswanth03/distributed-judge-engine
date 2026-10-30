// Package model holds the domain types of the Submission Service and the wire formats it shares with
// other services (see docs/contracts.md).
package model

import (
	"time"

	"github.com/google/uuid"
)

// Status is the lifecycle of a submission. It only moves forward: QUEUED → RUNNING → COMPLETED.
type Status string

const (
	StatusQueued    Status = "QUEUED"
	StatusRunning   Status = "RUNNING"
	StatusCompleted Status = "COMPLETED"
)

// Verdict is the judge's result, set once the status is COMPLETED.
type Verdict string

const (
	VerdictAccepted            Verdict = "ACCEPTED"
	VerdictWrongAnswer         Verdict = "WRONG_ANSWER"
	VerdictTimeLimitExceeded   Verdict = "TIME_LIMIT_EXCEEDED"
	VerdictMemoryLimitExceeded Verdict = "MEMORY_LIMIT_EXCEEDED"
	VerdictRuntimeError        Verdict = "RUNTIME_ERROR"
	VerdictCompilationError    Verdict = "COMPILATION_ERROR"
	VerdictInternalError       Verdict = "INTERNAL_ERROR"
)

func (v Verdict) Valid() bool {
	switch v {
	case VerdictAccepted, VerdictWrongAnswer, VerdictTimeLimitExceeded, VerdictMemoryLimitExceeded,
		VerdictRuntimeError, VerdictCompilationError, VerdictInternalError:
		return true
	}
	return false
}

// Submission is one row of the submissions table.
type Submission struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	ProblemID       string
	Language        string
	Code            string
	Status          Status
	Verdict         *Verdict
	ExecutionTimeMS int
	MemoryKB        int
	TestCasesPassed int
	TotalTestCases  int
	ErrorMessage    *string
	TestsetVersion  *int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Outbox event types.
const EventSubmissionCreated = "SUBMISSION_CREATED"

// OutboxEvent is one row of the outbox_events table: a message waiting to be published to RabbitMQ.
type OutboxEvent struct {
	ID          uuid.UUID
	AggregateID uuid.UUID
	EventType   string
	Payload     []byte // JSON, published as-is
	RetryCount  int
}

// JobMessage is published to jobs.queue for the Judge Worker.
type JobMessage struct {
	SubmissionID uuid.UUID `json:"submission_id"`
	UserID       uuid.UUID `json:"user_id"`
	ProblemID    string    `json:"problem_id"`
	Language     string    `json:"language"`
	Code         string    `json:"code"`
	SubmittedAt  time.Time `json:"submitted_at"`
}

// StatusEvent is what the Judge Worker publishes to the verdicts exchange: a RUNNING event when it starts a job,
// and a COMPLETED event carrying the verdict when it finishes. The Submission Service also emits it to its
// own SSE clients when the reaper gives up on a submission.
type StatusEvent struct {
	SubmissionID    uuid.UUID `json:"submission_id"`
	UserID          uuid.UUID `json:"user_id"`
	ProblemID       string    `json:"problem_id"`
	Status          Status    `json:"status"`
	Verdict         *Verdict  `json:"verdict,omitempty"`
	ExecutionTimeMS int       `json:"execution_time_ms,omitempty"`
	MemoryKB        int       `json:"memory_kb,omitempty"`
	TestCasesPassed int       `json:"test_cases_passed,omitempty"`
	TotalTestCases  int       `json:"total_test_cases,omitempty"`
	ErrorMessage    *string   `json:"error_message,omitempty"`
	TestsetVersion  *int      `json:"testset_version,omitempty"`
}

// EventFromSubmission describes a submission's current state as a StatusEvent.
func EventFromSubmission(s *Submission) StatusEvent {
	return StatusEvent{
		SubmissionID:    s.ID,
		UserID:          s.UserID,
		ProblemID:       s.ProblemID,
		Status:          s.Status,
		Verdict:         s.Verdict,
		ExecutionTimeMS: s.ExecutionTimeMS,
		MemoryKB:        s.MemoryKB,
		TestCasesPassed: s.TestCasesPassed,
		TotalTestCases:  s.TotalTestCases,
		ErrorMessage:    s.ErrorMessage,
		TestsetVersion:  s.TestsetVersion,
	}
}
