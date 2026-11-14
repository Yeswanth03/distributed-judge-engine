package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

const runColumns = `id, user_id, problem_id, language, status, result, error_message, created_at, updated_at`

func scanRun(row pgx.Row) (*model.Run, error) {
	var run model.Run
	err := row.Scan(&run.ID, &run.UserID, &run.ProblemID, &run.Language, &run.Status, &run.Result,
		&run.ErrorMessage, &run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// CreateRunWithOutbox inserts the run and its job message in one transaction, exactly like a submission: the
// relay ships the message, so a broker outage delays a run but cannot lose it.
func (r *Repository) CreateRunWithOutbox(ctx context.Context, run *model.Run, ev *model.OutboxEvent) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	_, err = tx.Exec(ctx, `INSERT INTO runs (id, user_id, problem_id, language, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'QUEUED', $5, $5)`,
		run.ID, run.UserID, run.ProblemID, run.Language, run.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}

	err = tx.QueryRow(ctx, `INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload, created_at)
		VALUES ('RUN', $1, $2, $3, $4) RETURNING id`,
		ev.AggregateID, ev.EventType, string(ev.Payload), run.CreatedAt).Scan(&ev.ID)
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	run.Status = model.RunQueued
	run.UpdatedAt = run.CreatedAt
	return nil
}

func (r *Repository) GetRunByID(ctx context.Context, id uuid.UUID) (*model.Run, error) {
	run, err := scanRun(r.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return run, err
}

// CountRunsSince counts the user's runs created at or after since, for the rate limit. Every replica sees the
// same count, so the limit holds however requests are spread across them.
func (r *Repository) CountRunsSince(ctx context.Context, userID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM runs WHERE user_id = $1 AND created_at >= $2`, userID, since).Scan(&n)
	return n, err
}

// CompleteRun stores the worker's result. Only the first result for a run wins; a duplicate, or one arriving
// after the janitor gave up, reports false and changes nothing.
func (r *Repository) CompleteRun(ctx context.Context, id uuid.UUID, result []byte) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE runs SET status = 'COMPLETED', result = $2, updated_at = NOW()
		WHERE id = $1 AND status = 'QUEUED'`, id, string(result))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// FailStaleRuns gives up on runs still QUEUED since before createdBefore, so clients stop polling them.
func (r *Repository) FailStaleRuns(ctx context.Context, createdBefore time.Time, message string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE runs SET status = 'FAILED', error_message = $2, updated_at = NOW()
		WHERE status = 'QUEUED' AND created_at < $1`, createdBefore, message)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteRunsBefore removes runs created before before; nothing refers to them once the user has seen them.
func (r *Repository) DeleteRunsBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM runs WHERE created_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
