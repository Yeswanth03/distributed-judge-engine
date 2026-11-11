// Package outbox ships the events the HTTP API wrote to outbox_events on to RabbitMQ.
package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/broker"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/repository"
)

type Store interface {
	ProcessOutboxBatch(ctx context.Context, limit, maxRetries int, publish repository.PublishFunc) (repository.BatchResult, error)
	DeletePublishedBefore(ctx context.Context, before time.Time) (int64, error)
	CountOutbox(ctx context.Context) (repository.OutboxCounts, error)
}

type Publisher interface {
	Ready() bool
	Publish(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) error
}

type Options struct {
	PollInterval    time.Duration
	BatchSize       int
	MaxRetries      int
	Retention       time.Duration // PUBLISHED rows older than this are deleted
	CleanupInterval time.Duration
	GaugeInterval   time.Duration // how often the outbox gauges are refreshed
	MaxBackoff      time.Duration
}

var (
	published = promauto.NewCounter(prometheus.CounterOpts{
		Name: "outbox_published_total", Help: "Outbox events published and confirmed by the broker.",
	})
	publishFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "outbox_publish_failures_total", Help: "Failed publish attempts (each costs the event one retry).",
	})
	pendingGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "outbox_pending_events", Help: "Events waiting to be published.",
	})
	failedGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "outbox_failed_events", Help: "Events that ran out of retries and need attention.",
	})
	lagGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "outbox_lag_seconds", Help: "Age of the oldest pending event (0 when none).",
	})
)

// Relay polls the outbox and publishes pending events to the users exchange, routed by event type. Any number of replicas can run it at
// once: rows are claimed with FOR UPDATE SKIP LOCKED.
type Relay struct {
	store Store
	pub   Publisher
	opts  Options
	log   *slog.Logger
	nudge chan struct{}
}

func NewRelay(store Store, pub Publisher, opts Options, log *slog.Logger) *Relay {
	return &Relay{store: store, pub: pub, opts: opts, log: log, nudge: make(chan struct{}, 1)}
}

// Nudge asks the relay to look at the outbox now instead of at its next poll. It never blocks.
func (r *Relay) Nudge() {
	select {
	case r.nudge <- struct{}{}:
	default: // a nudge is already pending; that one will pick this event up too
	}
}

// Run publishes until ctx is cancelled.
func (r *Relay) Run(ctx context.Context) {
	poll := time.NewTicker(r.opts.PollInterval)
	defer poll.Stop()
	cleanup := time.NewTicker(r.opts.CleanupInterval)
	defer cleanup.Stop()
	gauges := time.NewTicker(r.opts.GaugeInterval)
	defer gauges.Stop()

	var backoff time.Duration
	var retryAt time.Time
	r.refreshGauges(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
		case <-r.nudge:
		case <-cleanup.C:
			r.cleanup(ctx)
			continue
		case <-gauges.C:
			r.refreshGauges(ctx)
			continue
		}
		if time.Now().Before(retryAt) {
			continue // backing off after a failure; ticks and nudges wait
		}
		if !r.pub.Ready() {
			continue // broker down: checking is free, so poll on and resume the moment it is back
		}
		if r.drain(ctx) {
			backoff = 0
			retryAt = time.Time{}
			continue
		}
		backoff = min(max(2*backoff, r.opts.PollInterval), r.opts.MaxBackoff)
		retryAt = time.Now().Add(backoff)
	}
}

// drain publishes batches until the outbox is empty. It returns false if it stopped on a failure.
func (r *Relay) drain(ctx context.Context) bool {
	for ctx.Err() == nil {
		// While the broker is down, leave the rows alone: a failed attempt would cost each event a retry,
		// and a long outage would then mark perfectly good events FAILED. They wait as PENDING instead.
		if !r.pub.Ready() {
			return false
		}
		res, err := r.store.ProcessOutboxBatch(ctx, r.opts.BatchSize, r.opts.MaxRetries, r.publish)
		published.Add(float64(res.Published))
		if err != nil {
			if ctx.Err() == nil {
				r.log.Error("outbox batch failed", "error", err)
			}
			return false
		}
		if res.PublishErr != nil {
			publishFailures.Inc()
			r.log.Warn("outbox publish failed", "error", res.PublishErr, "published_before_failure", res.Published)
			return false
		}
		if res.Published > 0 {
			r.log.Debug("outbox events published", "count", res.Published)
		}
		if res.Published < r.opts.BatchSize {
			return true // nothing more pending right now
		}
	}
	return false
}

func (r *Relay) publish(ctx context.Context, ev model.OutboxEvent) error {
	return r.pub.Publish(ctx, broker.UsersExchange, ev.EventType, amqp.Publishing{
		ContentType: "application/json",
		MessageId:   ev.ID.String(), // the outbox row's ID, unique per event
		Type:        ev.EventType,
		Timestamp:   time.Now(),
		Body:        ev.Payload,
	})
}

func (r *Relay) cleanup(ctx context.Context) {
	n, err := r.store.DeletePublishedBefore(ctx, time.Now().Add(-r.opts.Retention))
	if err != nil {
		r.log.Error("outbox cleanup failed", "error", err)
		return
	}
	if n > 0 {
		r.log.Info("outbox cleanup", "deleted", n)
	}
}

func (r *Relay) refreshGauges(ctx context.Context) {
	c, err := r.store.CountOutbox(ctx)
	if err != nil {
		return
	}
	pendingGauge.Set(float64(c.Pending))
	failedGauge.Set(float64(c.Failed))
	if c.OldestPending != nil {
		lagGauge.Set(time.Since(*c.OldestPending).Seconds())
	} else {
		lagGauge.Set(0)
	}
}
