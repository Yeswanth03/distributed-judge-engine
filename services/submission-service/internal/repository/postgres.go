// Package repository is the Submission Service's only access to PostgreSQL.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

var ErrNotFound = errors.New("not found")

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

const submissionColumns = `id, user_id, problem_id, language, code, status, verdict, execution_time_ms, memory_kb,
	test_cases_passed, total_test_cases, error_message, testset_version, created_at, updated_at`

// Same as submissionColumns but without the (up to 64 KB) code, for listings.
const submissionColumnsNoCode = `id, user_id, problem_id, language, '' AS code, status, verdict, execution_time_ms,
	memory_kb, test_cases_passed, total_test_cases, error_message, testset_version, created_at, updated_at`

func scanSubmission(row pgx.Row) (*model.Submission, error) {
	var s model.Submission
	err := row.Scan(&s.ID, &s.UserID, &s.ProblemID, &s.Language, &s.Code, &s.Status, &s.Verdict,
		&s.ExecutionTimeMS, &s.MemoryKB, &s.TestCasesPassed, &s.TotalTestCases, &s.ErrorMessage,
		&s.TestsetVersion, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CreateSubmissionWithOutbox inserts the submission and its outbox event in one transaction: either both
// exist or neither does. This is the only write the HTTP request makes; RabbitMQ is never touched here.
// s.ID and s.CreatedAt are chosen by the caller so they can go into the event payload before the insert.
func (r *Repository) CreateSubmissionWithOutbox(ctx context.Context, s *model.Submission, ev *model.OutboxEvent) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	_, err = tx.Exec(ctx, `INSERT INTO submissions (id, user_id, problem_id, language, code, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'QUEUED', $6, $6)`,
		s.ID, s.UserID, s.ProblemID, s.Language, s.Code, s.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert submission: %w", err)
	}

	err = tx.QueryRow(ctx, `INSERT INTO outbox_events (aggregate_id, event_type, payload, created_at)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		ev.AggregateID, ev.EventType, string(ev.Payload), s.CreatedAt).Scan(&ev.ID)
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	s.Status = model.StatusQueued
	s.UpdatedAt = s.CreatedAt
	return nil
}

func (r *Repository) GetSubmissionByID(ctx context.Context, id uuid.UUID) (*model.Submission, error) {
	s, err := scanSubmission(r.pool.QueryRow(ctx,
		`SELECT `+submissionColumns+` FROM submissions WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// Cursor is a position in a newest-first listing: the last row of the previous page.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type ListFilter struct {
	UserID    uuid.UUID
	ProblemID string // optional
	Limit     int
	After     *Cursor // optional
}

// ListSubmissions returns a user's submissions newest first, without their code.
func (r *Repository) ListSubmissions(ctx context.Context, f ListFilter) ([]model.Submission, error) {
	var where []string
	args := []any{f.UserID}
	where = append(where, "user_id = $1")
	if f.ProblemID != "" {
		args = append(args, f.ProblemID)
		where = append(where, fmt.Sprintf("problem_id = $%d", len(args)))
	}
	if f.After != nil {
		args = append(args, f.After.CreatedAt, f.After.ID)
		where = append(where, fmt.Sprintf("(created_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, f.Limit)
	query := `SELECT ` + submissionColumnsNoCode + ` FROM submissions WHERE ` + strings.Join(where, " AND ") +
		fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Submission
	for rows.Next() {
		s, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// MarkRunning moves a QUEUED submission to RUNNING. It reports false, without error, when the submission
// is already RUNNING or COMPLETED (a duplicate or late event) or does not exist.
func (r *Repository) MarkRunning(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE submissions SET status = 'RUNNING', updated_at = NOW() WHERE id = $1 AND status = 'QUEUED'`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// UpdateVerdictIdempotent records the final verdict. Only the first verdict for a submission wins; any
// later one (redelivery, duplicate job) reports false and changes nothing. It also accepts a submission
// still QUEUED, in case the RUNNING event was lost or arrives after the verdict.
func (r *Repository) UpdateVerdictIdempotent(ctx context.Context, ev model.StatusEvent) (bool, error) {
	if ev.Verdict == nil || !ev.Verdict.Valid() {
		return false, fmt.Errorf("verdict event for %s has no valid verdict", ev.SubmissionID)
	}
	tag, err := r.pool.Exec(ctx, `UPDATE submissions
		SET status = 'COMPLETED', verdict = $2, execution_time_ms = $3, memory_kb = $4, test_cases_passed = $5,
		    total_test_cases = $6, error_message = $7, testset_version = $8, updated_at = NOW()
		WHERE id = $1 AND status IN ('QUEUED', 'RUNNING')`,
		ev.SubmissionID, *ev.Verdict, ev.ExecutionTimeMS, ev.MemoryKB, ev.TestCasesPassed, ev.TotalTestCases,
		ev.ErrorMessage, ev.TestsetVersion)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ReapStuck gives up on submissions that have been QUEUED since before queuedBefore or RUNNING since before
// runningBefore, completing them as INTERNAL_ERROR so clients stop waiting. A verdict that arrives later is
// ignored by UpdateVerdictIdempotent. It returns the submissions it completed.
func (r *Repository) ReapStuck(ctx context.Context, queuedBefore, runningBefore time.Time, message string) ([]model.Submission, error) {
	rows, err := r.pool.Query(ctx, `UPDATE submissions
		SET status = 'COMPLETED', verdict = 'INTERNAL_ERROR', error_message = $3, updated_at = NOW()
		WHERE (status = 'QUEUED' AND updated_at < $1) OR (status = 'RUNNING' AND updated_at < $2)
		RETURNING `+submissionColumnsNoCode, queuedBefore, runningBefore, message)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Submission
	for rows.Next() {
		s, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// PublishFunc publishes one outbox event. A nil error must mean the broker confirmed the message.
type PublishFunc func(ctx context.Context, ev model.OutboxEvent) error

type BatchResult struct {
	Published  int
	PublishErr error // the publish failure that stopped the batch, if any
}

// ProcessOutboxBatch locks up to limit pending events (oldest first) and publishes them one by one,
// marking each PUBLISHED once confirmed. FOR UPDATE SKIP LOCKED lets several relays run side by side
// without picking the same rows.
//
// On a publish failure the event's retry_count goes up (it becomes FAILED at maxRetries) and the batch stops,
// leaving the remaining rows untouched: the broker is most likely unhealthy, and they should not be charged
// a retry for it.
//
// If the process dies after a publish but before the commit, the row stays PENDING and is published again:
// delivery is at-least-once, and consumers deduplicate by submission_id.
func (r *Repository) ProcessOutboxBatch(ctx context.Context, limit, maxRetries int, publish PublishFunc) (BatchResult, error) {
	var res BatchResult
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return res, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	rows, err := tx.Query(ctx, `SELECT id, aggregate_id, event_type, payload, retry_count
		FROM outbox_events WHERE status = 'PENDING'
		ORDER BY created_at LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return res, fmt.Errorf("select pending: %w", err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.OutboxEvent, error) {
		var ev model.OutboxEvent
		var payload string
		err := row.Scan(&ev.ID, &ev.AggregateID, &ev.EventType, &payload, &ev.RetryCount)
		ev.Payload = []byte(payload)
		return ev, err
	})
	if err != nil {
		return res, fmt.Errorf("scan pending: %w", err)
	}

	for _, ev := range events {
		if perr := publish(ctx, ev); perr != nil {
			res.PublishErr = perr
			if _, err := tx.Exec(ctx, `UPDATE outbox_events
				SET retry_count = retry_count + 1, last_error = $2,
				    status = CASE WHEN retry_count + 1 >= $3 THEN 'FAILED' ELSE 'PENDING' END
				WHERE id = $1`, ev.ID, perr.Error(), maxRetries); err != nil {
				return res, fmt.Errorf("record retry: %w", err)
			}
			break
		}
		if _, err := tx.Exec(ctx,
			`UPDATE outbox_events SET status = 'PUBLISHED', published_at = NOW() WHERE id = $1`, ev.ID); err != nil {
			return res, fmt.Errorf("mark published: %w", err)
		}
		res.Published++
	}

	if err := tx.Commit(ctx); err != nil {
		return BatchResult{PublishErr: res.PublishErr}, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}

// OutboxCounts is how many events are waiting to be published, and how many gave up.
type OutboxCounts struct {
	Pending       int
	Failed        int
	OldestPending *time.Time
}

func (r *Repository) CountOutbox(ctx context.Context) (OutboxCounts, error) {
	var c OutboxCounts
	err := r.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE status = 'PENDING'),
		count(*) FILTER (WHERE status = 'FAILED'),
		min(created_at) FILTER (WHERE status = 'PENDING')
		FROM outbox_events WHERE status <> 'PUBLISHED'`).Scan(&c.Pending, &c.Failed, &c.OldestPending)
	return c, err
}

// DeletePublishedBefore removes published outbox rows older than before; they are only kept for debugging.
func (r *Repository) DeletePublishedBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM outbox_events WHERE status = 'PUBLISHED' AND published_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
