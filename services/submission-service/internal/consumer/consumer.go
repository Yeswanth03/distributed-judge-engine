// Package consumer reads the status events the Judge Worker publishes to verdict.fanout.
//
// Two consumers share the exchange:
//   - DBConsumer reads the durable, shared queue: across all replicas each event is applied to the database
//     once (well, at least once; the repository's guarded UPDATEs absorb duplicates).
//   - SSEConsumer reads a private, temporary queue per replica: every replica sees every event, so a browser
//     gets its update whichever replica its SSE connection landed on.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/broker"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/sse"
)

var verdictEvents = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "verdict_events_total",
	Help: "Status events consumed into the database, by status and result (applied, duplicate, invalid).",
}, []string{"status", "result"})

// ParseEvent decodes and validates a status event from verdict.fanout.
func ParseEvent(body []byte) (model.StatusEvent, error) {
	var ev model.StatusEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return ev, fmt.Errorf("not a JSON status event: %w", err)
	}
	if ev.SubmissionID == uuid.Nil {
		return ev, errors.New("missing submission_id")
	}
	switch ev.Status {
	case model.StatusRunning:
	case model.StatusCompleted:
		if ev.Verdict == nil || !ev.Verdict.Valid() {
			return ev, errors.New("COMPLETED event without a valid verdict")
		}
	default:
		return ev, fmt.Errorf("unexpected status %q", ev.Status)
	}
	return ev, nil
}

type Store interface {
	MarkRunning(ctx context.Context, id uuid.UUID) (bool, error)
	UpdateVerdictIdempotent(ctx context.Context, ev model.StatusEvent) (bool, error)
}

// DBConsumer applies status events to the submissions table.
type DBConsumer struct {
	Conn     *broker.Conn
	Store    Store
	Queue    string // broker.VerdictQueue
	Prefetch int
	Workers  int
	Log      *slog.Logger
}

// Run consumes until ctx is cancelled, resubscribing whenever the connection comes back.
func (c *DBConsumer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		conn, err := c.Conn.WaitConnected(ctx)
		if err != nil {
			return
		}
		if err := c.session(ctx, conn); err != nil && ctx.Err() == nil {
			c.Log.Warn("verdict consumer stopped, resubscribing", "error", err)
			sleep(ctx, time.Second)
		}
	}
}

func (c *DBConsumer) session(ctx context.Context, conn *amqp.Connection) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close() //nolint:errcheck
	if err := ch.Qos(c.Prefetch, 0, false); err != nil {
		return err
	}
	tag := "submission-service-db-" + uuid.NewString()[:8]
	deliveries, err := ch.Consume(c.Queue, tag, false, false, false, false, nil)
	if err != nil {
		return err
	}
	c.Log.Info("verdict consumer subscribed", "queue", c.Queue)

	// On shutdown, cancel the subscription instead of closing the channel: messages already delivered to
	// us are still handed to the workers and acked, and the deliveries channel then closes by itself.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			ch.Cancel(tag, false) //nolint:errcheck
		case <-stop:
		}
	}()

	var wg sync.WaitGroup
	for range c.Workers {
		wg.Go(func() {
			for d := range deliveries {
				c.handle(ctx, d)
			}
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil
	}
	return errors.New("delivery channel closed")
}

func (c *DBConsumer) handle(ctx context.Context, d amqp.Delivery) {
	ev, err := ParseEvent(d.Body)
	if err != nil {
		// Retrying cannot fix a malformed message: reject it to the dead-letter queue for inspection.
		verdictEvents.WithLabelValues("unknown", "invalid").Inc()
		c.Log.Warn("rejecting invalid status event", "error", err, "message_id", d.MessageId, "body", truncate(d.Body, 200))
		d.Nack(false, false) //nolint:errcheck
		return
	}

	// A database error is (almost always) temporary. Keep the message unacked and retry here: requeueing at
	// once would spin through the queue's delivery limit in milliseconds and dead-letter a good verdict.
	backoff := 200 * time.Millisecond
	for attempt := 1; ; attempt++ {
		// The write itself is not cut short by shutdown; it gets a few seconds to finish.
		opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		applied, err := c.apply(opCtx, ev)
		cancel()
		if err == nil {
			result := "applied"
			if !applied {
				result = "duplicate"
			}
			verdictEvents.WithLabelValues(string(ev.Status), result).Inc()
			c.Log.Info("status event consumed", "submission_id", ev.SubmissionID, "status", ev.Status, "result", result)
			d.Ack(false) //nolint:errcheck
			return
		}
		c.Log.Error("applying status event failed, retrying", "submission_id", ev.SubmissionID, "attempt", attempt,
			"error", err, "retry_in", backoff.String())
		if !sleep(ctx, backoff) {
			d.Nack(false, true) //nolint:errcheck // shutting down: give it back for another replica
			return
		}
		backoff = min(backoff*2, 5*time.Second)
	}
}

func (c *DBConsumer) apply(ctx context.Context, ev model.StatusEvent) (bool, error) {
	if ev.Status == model.StatusRunning {
		return c.Store.MarkRunning(ctx, ev.SubmissionID)
	}
	return c.Store.UpdateVerdictIdempotent(ctx, ev)
}

// SSEConsumer feeds this replica's SSE hub from a private queue bound to verdict.fanout.
type SSEConsumer struct {
	Conn     *broker.Conn
	Hub      *sse.Hub
	Exchange string // broker.VerdictExchange
	Log      *slog.Logger
}

func (c *SSEConsumer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		conn, err := c.Conn.WaitConnected(ctx)
		if err != nil {
			return
		}
		if err := c.session(ctx, conn); err != nil && ctx.Err() == nil {
			c.Log.Warn("sse consumer stopped, resubscribing", "error", err)
			sleep(ctx, time.Second)
		}
	}
}

func (c *SSEConsumer) session(ctx context.Context, conn *amqp.Connection) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close() //nolint:errcheck

	// Exclusive + auto-delete: the queue lives exactly as long as this subscription, so a replica that dies
	// leaves nothing behind. Live updates are best effort (auto-ack): the database stays the source of truth.
	host, _ := os.Hostname()
	q, err := ch.QueueDeclare("submission-service.sse."+host+"."+uuid.NewString()[:8], false, true, true, false, nil)
	if err != nil {
		return err
	}
	if err := ch.QueueBind(q.Name, "", c.Exchange, false, nil); err != nil {
		return err
	}
	deliveries, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		return err
	}
	c.Log.Info("sse consumer subscribed", "queue", q.Name)
	// Events published while we had no queue are gone; open streams re-read their state instead.
	c.Hub.Resync()

	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("delivery channel closed")
			}
			if ev, err := ParseEvent(d.Body); err == nil {
				c.Hub.Publish(ev)
			}
		}
	}
}

// sleep waits for d, returning false if ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func truncate(b []byte, n int) string {
	s := string(b)
	if len(s) > n {
		s = s[:n] + "…"
	}
	return strings.ToValidUTF8(s, "?") // cutting may have split a character
}
