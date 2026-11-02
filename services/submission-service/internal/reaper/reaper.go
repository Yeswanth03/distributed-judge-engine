// Package reaper finishes submissions the judge never finished, so no client waits forever.
package reaper

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

// Message is the error_message of a reaped submission; it is shown to the user.
const Message = "The judge did not finish this submission in time. Please submit again."

type Store interface {
	ReapStuck(ctx context.Context, queuedBefore, runningBefore time.Time, message string) ([]model.Submission, error)
}

type Publisher interface {
	Publish(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) error
}

// LocalHub is the fallback for telling this replica's SSE streams when the broker is unavailable.
type LocalHub interface {
	Publish(ev model.StatusEvent)
}

type Options struct {
	Interval       time.Duration
	QueuedTimeout  time.Duration
	RunningTimeout time.Duration
	Exchange       string // broker.VerdictExchange
	RoutingKey     string // broker.StatusRoutingKey
}

type Reaper struct {
	store Store
	pub   Publisher
	hub   LocalHub
	opts  Options
	log   *slog.Logger
	now   func() time.Time
}

func New(store Store, pub Publisher, hub LocalHub, opts Options, log *slog.Logger) *Reaper {
	return &Reaper{store: store, pub: pub, hub: hub, opts: opts, log: log, now: time.Now}
}

// Run reaps every Interval until ctx is cancelled. Several replicas may run it: the guarded UPDATE hands
// each stuck submission to exactly one of them.
func (r *Reaper) Run(ctx context.Context) {
	t := time.NewTicker(r.opts.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.ReapOnce(ctx)
		}
	}
}

// ReapOnce completes every stuck submission as INTERNAL_ERROR and announces it.
func (r *Reaper) ReapOnce(ctx context.Context) int {
	now := r.now()
	reaped, err := r.store.ReapStuck(ctx, now.Add(-r.opts.QueuedTimeout), now.Add(-r.opts.RunningTimeout), Message)
	if err != nil {
		r.log.Error("reaping stuck submissions failed", "error", err)
		return 0
	}
	for i := range reaped {
		s := &reaped[i]
		ev := model.EventFromSubmission(s)
		r.log.Warn("reaped stuck submission", "submission_id", s.ID, "user_id", s.UserID, "problem_id", s.ProblemID,
			"stuck_since", s.CreatedAt)
		// Through the exchange, so SSE streams on every replica hear about it, exactly as if the judge had sent
		// the verdict. The routing key keeps it from Catalog: this is the judge's failure, not a scored attempt.
		// If the broker is down, at least this replica's streams are told; the others pick it up from the
		// database when their clients reconnect.
		body, _ := json.Marshal(ev)
		err := r.pub.Publish(ctx, r.opts.Exchange, r.opts.RoutingKey, amqp.Publishing{
			ContentType: "application/json", MessageId: s.ID.String(), Timestamp: now, Body: body,
		})
		if err != nil {
			r.log.Warn("announcing reaped submission failed; telling local streams only", "submission_id", s.ID, "error", err)
			r.hub.Publish(ev)
		}
	}
	return len(reaped)
}
