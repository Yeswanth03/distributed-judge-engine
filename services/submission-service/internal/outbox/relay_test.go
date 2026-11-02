package outbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/broker"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/repository"
)

// fakeStore is an in-memory outbox with the same batch semantics as the repository.
type fakeStore struct {
	mu      sync.Mutex
	pending []model.OutboxEvent
	batches int
}

func (f *fakeStore) add(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for range n {
		id := uuid.New()
		f.pending = append(f.pending, model.OutboxEvent{ID: uuid.New(), AggregateID: id,
			EventType: model.EventSubmissionCreated, Payload: []byte(`{"submission_id":"` + id.String() + `"}`)})
	}
}

func (f *fakeStore) left() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending)
}

func (f *fakeStore) ProcessOutboxBatch(ctx context.Context, limit, _ int, publish repository.PublishFunc) (repository.BatchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches++
	var res repository.BatchResult
	for len(f.pending) > 0 && res.Published < limit {
		if err := publish(ctx, f.pending[0]); err != nil {
			res.PublishErr = err
			return res, nil
		}
		f.pending = f.pending[1:]
		res.Published++
	}
	return res, nil
}

func (f *fakeStore) DeletePublishedBefore(context.Context, time.Time) (int64, error) { return 0, nil }
func (f *fakeStore) CountOutbox(context.Context) (repository.OutboxCounts, error) {
	return repository.OutboxCounts{Pending: f.left()}, nil
}

type fakePublisher struct {
	ready atomic.Bool
	fail  atomic.Bool
	mu    sync.Mutex
	msgs  []amqp.Publishing
	keys  []string
}

func (p *fakePublisher) Ready() bool { return p.ready.Load() }
func (p *fakePublisher) Publish(_ context.Context, exchange, key string, msg amqp.Publishing) error {
	if p.fail.Load() {
		return errors.New("nacked")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append(p.msgs, msg)
	p.keys = append(p.keys, exchange+"/"+key)
	return nil
}

func (p *fakePublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.msgs)
}

func startRelay(t *testing.T, store *fakeStore, pub *fakePublisher, poll time.Duration) *Relay {
	t.Helper()
	r := NewRelay(store, pub, Options{
		PollInterval: poll, BatchSize: 10, MaxRetries: 5, Retention: time.Hour,
		CleanupInterval: time.Hour, GaugeInterval: time.Hour, MaxBackoff: 50 * time.Millisecond,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return r
}

func eventually(t *testing.T, within time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRelayPublishesJobs(t *testing.T) {
	store, pub := &fakeStore{}, &fakePublisher{}
	pub.ready.Store(true)
	store.add(3)
	startRelay(t, store, pub, 10*time.Millisecond)

	eventually(t, time.Second, func() bool { return pub.count() == 3 }, "3 publishes")
	pub.mu.Lock()
	defer pub.mu.Unlock()
	m := pub.msgs[0]
	if pub.keys[0] != "/"+broker.JobsQueue || m.ContentType != "application/json" || m.MessageId == "" || m.Type != model.EventSubmissionCreated {
		t.Fatalf("unexpected publish %s %+v", pub.keys[0], m)
	}
}

func TestRelayDrainsBurstInOneWakeup(t *testing.T) {
	store, pub := &fakeStore{}, &fakePublisher{}
	pub.ready.Store(true)
	store.add(35)                             // 4 batches of 10
	r := startRelay(t, store, pub, time.Hour) // polling effectively off
	r.Nudge()
	eventually(t, time.Second, func() bool { return pub.count() == 35 }, "burst drained after one nudge")
}

func TestNudgeWakesRelayBeforePoll(t *testing.T) {
	store, pub := &fakeStore{}, &fakePublisher{}
	pub.ready.Store(true)
	r := startRelay(t, store, pub, time.Hour)
	store.add(1)
	r.Nudge()
	r.Nudge() // never blocks, even when a nudge is pending
	eventually(t, time.Second, func() bool { return pub.count() == 1 }, "nudged publish")
}

func TestRelayLeavesRowsAloneWhileBrokerDown(t *testing.T) {
	store, pub := &fakeStore{}, &fakePublisher{}
	store.add(5)
	startRelay(t, store, pub, 5*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	store.mu.Lock()
	batches := store.batches
	store.mu.Unlock()
	if batches != 0 || pub.count() != 0 {
		t.Fatalf("broker down: %d batches ran, %d published; rows must not be touched", batches, pub.count())
	}

	pub.ready.Store(true) // broker back
	eventually(t, time.Second, func() bool { return store.left() == 0 }, "outbox drained after recovery")
}

func TestRelayBacksOffAfterFailures(t *testing.T) {
	store, pub := &fakeStore{}, &fakePublisher{}
	pub.ready.Store(true)
	pub.fail.Store(true)
	store.add(1)
	startRelay(t, store, pub, time.Millisecond)

	time.Sleep(200 * time.Millisecond)
	store.mu.Lock()
	attempts := store.batches
	store.mu.Unlock()
	// Without backoff a 1ms poll would try ~200 times; capped at 50ms it is roughly 200/50 plus the ramp-up.
	if attempts > 20 {
		t.Fatalf("%d attempts in 200ms: relay is not backing off", attempts)
	}

	pub.fail.Store(false)
	eventually(t, time.Second, func() bool { return store.left() == 0 }, "publish after failures stop")
}
