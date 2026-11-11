package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
)

// The outbox code is the same as the Submission Service's (see its repository package for the reasoning).

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
// delivery is at-least-once, and consumers deduplicate by event_id and drop stale versions.
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
