package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/broker"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

var runResults = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "run_results_total",
	Help: "Run results consumed into the database, by result (applied, duplicate, invalid).",
}, []string{"result"})

// ParseRunResult checks a run result from the Judge Worker just enough to store it and returns its run ID.
// The rest of the message is handed to the client as it is (see docs/contracts.md).
func ParseRunResult(body []byte) (uuid.UUID, error) {
	var msg struct {
		RunID  uuid.UUID `json:"run_id"`
		Status string    `json:"status"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		return uuid.Nil, fmt.Errorf("not a JSON run result: %w", err)
	}
	if msg.RunID == uuid.Nil {
		return uuid.Nil, errors.New("missing run_id")
	}
	if msg.Status != string(model.RunCompleted) {
		return uuid.Nil, fmt.Errorf("unexpected status %q", msg.Status)
	}
	return msg.RunID, nil
}

type RunStore interface {
	CompleteRun(ctx context.Context, id uuid.UUID, result []byte) (bool, error)
}

// RunResultConsumer stores the run results the Judge Worker sends to broker.RunResultsQueue. Results are
// small and rare next to verdicts, so one worker is plenty.
type RunResultConsumer struct {
	Conn     *broker.Conn
	Store    RunStore
	Queue    string // broker.RunResultsQueue
	Prefetch int
	Log      *slog.Logger
}

// Run consumes until ctx is cancelled, resubscribing whenever the connection comes back.
func (c *RunResultConsumer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		conn, err := c.Conn.WaitConnected(ctx)
		if err != nil {
			return
		}
		if err := c.session(ctx, conn); err != nil && ctx.Err() == nil {
			c.Log.Warn("run result consumer stopped, resubscribing", "error", err)
			sleep(ctx, time.Second)
		}
	}
}

func (c *RunResultConsumer) session(ctx context.Context, conn *amqp.Connection) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close() //nolint:errcheck
	if err := ch.Qos(c.Prefetch, 0, false); err != nil {
		return err
	}
	deliveries, err := ch.Consume(c.Queue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	c.Log.Info("run result consumer subscribed", "queue", c.Queue)
	for {
		select {
		case <-ctx.Done():
			return nil // closing the channel hands any unacked message back to the broker
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("delivery channel closed")
			}
			c.handle(ctx, d)
		}
	}
}

func (c *RunResultConsumer) handle(ctx context.Context, d amqp.Delivery) {
	id, err := ParseRunResult(d.Body)
	if err != nil {
		runResults.WithLabelValues("invalid").Inc()
		c.Log.Warn("rejecting invalid run result", "error", err, "message_id", d.MessageId, "body", truncate(d.Body, 200))
		d.Nack(false, false) //nolint:errcheck // to the dead-letter queue
		return
	}

	// Same reasoning as DBConsumer: a database error is almost always temporary, so retry here instead of
	// burning through the queue's delivery limit.
	backoff := 200 * time.Millisecond
	for attempt := 1; ; attempt++ {
		opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		applied, err := c.Store.CompleteRun(opCtx, id, d.Body)
		cancel()
		if err == nil {
			result := "applied"
			if !applied {
				result = "duplicate"
			}
			runResults.WithLabelValues(result).Inc()
			c.Log.Info("run result consumed", "run_id", id, "result", result)
			d.Ack(false) //nolint:errcheck
			return
		}
		c.Log.Error("storing run result failed, retrying", "run_id", id, "attempt", attempt, "error", err,
			"retry_in", backoff.String())
		if !sleep(ctx, backoff) {
			d.Nack(false, true) //nolint:errcheck // shutting down: give it back for another replica
			return
		}
		backoff = min(backoff*2, 5*time.Second)
	}
}
