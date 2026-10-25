package consumer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/broker"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/sse"
)

func TestParseEvent(t *testing.T) {
	id := uuid.NewString()
	valid := []string{
		`{"submission_id":"` + id + `","status":"RUNNING"}`,
		`{"submission_id":"` + id + `","status":"COMPLETED","verdict":"WRONG_ANSWER","test_cases_passed":3}`,
	}
	invalid := []string{
		`not json`,
		`{"status":"RUNNING"}`,
		`{"submission_id":"` + id + `","status":"QUEUED"}`,
		`{"submission_id":"` + id + `","status":"COMPLETED"}`,
		`{"submission_id":"` + id + `","status":"COMPLETED","verdict":"MAYBE"}`,
	}
	for _, b := range valid {
		if _, err := ParseEvent([]byte(b)); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
	for _, b := range invalid {
		if _, err := ParseEvent([]byte(b)); err == nil {
			t.Errorf("%s: expected an error", b)
		}
	}
}

// --- integration tests against RabbitMQ, each on its own temporary exchange and queue ---

func amqpURL(t *testing.T) string {
	t.Helper()
	u, explicit := os.LookupEnv("TEST_AMQP_URL")
	if !explicit {
		u = "amqp://judge:judge@localhost:5672/"
	}
	c, err := amqp.Dial(u)
	if err != nil {
		if explicit {
			t.Fatalf("TEST_AMQP_URL not reachable: %v", err)
		}
		t.Skipf("RabbitMQ not reachable: %v", err)
	}
	c.Close()
	return u
}

type rig struct {
	exchange, queue string
	ch              *amqp.Channel
	conn            *broker.Conn
}

func newRig(t *testing.T) *rig {
	t.Helper()
	url := amqpURL(t)
	raw, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := raw.Channel()
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{exchange: "test.verdict." + uuid.NewString()[:8], queue: "test.verdict.q." + uuid.NewString()[:8], ch: ch}
	if err := ch.ExchangeDeclare(r.exchange, "fanout", false, true, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ch.QueueDeclare(r.queue, false, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := ch.QueueBind(r.queue, "", r.exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ch.QueueDelete(r.queue, false, false, false) //nolint:errcheck
		raw.Close()
	})

	r.conn = broker.NewConn(url, quiet())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.conn.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return r
}

func (r *rig) publish(t *testing.T, body string) {
	t.Helper()
	if err := r.ch.PublishWithContext(context.Background(), r.exchange, "", false, false,
		amqp.Publishing{ContentType: "application/json", Body: []byte(body)}); err != nil {
		t.Fatal(err)
	}
}

// ready is the number of messages waiting in the test queue (not delivered, or delivered and given back).
func (r *rig) ready(t *testing.T) int {
	t.Helper()
	q, err := r.ch.QueueDeclarePassive(r.queue, false, false, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return q.Messages
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type call struct {
	op string
	ev model.StatusEvent
}

type fakeStore struct {
	mu       sync.Mutex
	calls    []call
	failNext int // fail this many calls before succeeding
	applied  map[uuid.UUID]model.Status
}

func (f *fakeStore) record(op string, ev model.StatusEvent) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{op, ev})
	if f.failNext > 0 {
		f.failNext--
		return false, errors.New("database unavailable")
	}
	if f.applied == nil {
		f.applied = map[uuid.UUID]model.Status{}
	}
	if f.applied[ev.SubmissionID] == ev.Status {
		return false, nil
	}
	f.applied[ev.SubmissionID] = ev.Status
	return true, nil
}

func (f *fakeStore) MarkRunning(_ context.Context, id uuid.UUID) (bool, error) {
	return f.record("running", model.StatusEvent{SubmissionID: id, Status: model.StatusRunning})
}

func (f *fakeStore) UpdateVerdictIdempotent(_ context.Context, ev model.StatusEvent) (bool, error) {
	return f.record("verdict", ev)
}

func (f *fakeStore) appliedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applied)
}

func (f *fakeStore) snapshot() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

func eventually(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// runDB starts a DBConsumer; the returned stop function cancels it and waits until it has fully stopped.
func runDB(t *testing.T, r *rig, store Store) (stop func()) {
	t.Helper()
	c := &DBConsumer{Conn: r.conn, Store: store, Queue: r.queue, Prefetch: 10, Workers: 2, Log: quiet()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	var once sync.Once
	stop = func() { once.Do(func() { cancel(); <-done }) }
	t.Cleanup(stop)
	return stop
}

func TestDBConsumerAppliesAndAcks(t *testing.T) {
	r := newRig(t)
	store := &fakeStore{}
	stop := runDB(t, r, store)
	id := uuid.NewString()

	r.publish(t, `{"submission_id":"`+id+`","status":"RUNNING"}`)
	r.publish(t, `{"submission_id":"`+id+`","status":"COMPLETED","verdict":"ACCEPTED","execution_time_ms":38}`)
	r.publish(t, `{"submission_id":"`+id+`","status":"COMPLETED","verdict":"ACCEPTED","execution_time_ms":38}`) // redelivery
	eventually(t, func() bool { return len(store.snapshot()) == 3 }, "3 store calls")

	stop()
	// Unacked messages would be back in the queue once the consumer is gone.
	if n := r.ready(t); n != 0 {
		t.Fatalf("%d messages back in the queue, want all acked", n)
	}
	// Workers run in parallel, so the order of calls is not guaranteed (COMPLETED can be applied before
	// RUNNING). That is fine: the repository's guarded UPDATEs give the same final state in any order.
	ops := map[string]int{}
	for _, c := range store.snapshot() {
		ops[c.op]++
		if c.op == "verdict" && c.ev.ExecutionTimeMS != 38 {
			t.Fatalf("verdict decoded wrongly: %+v", c.ev)
		}
	}
	if ops["running"] != 1 || ops["verdict"] != 2 {
		t.Fatalf("store calls %v, want 1 running and 2 verdict", ops)
	}
}

func TestDBConsumerRejectsInvalidMessages(t *testing.T) {
	r := newRig(t)
	store := &fakeStore{}
	stop := runDB(t, r, store)

	r.publish(t, `{"oops":`)
	r.publish(t, `{"submission_id":"`+uuid.NewString()+`","status":"COMPLETED"}`)
	marker := uuid.NewString()
	r.publish(t, `{"submission_id":"`+marker+`","status":"RUNNING"}`)
	eventually(t, func() bool { return len(store.snapshot()) == 1 }, "the valid message")

	stop()
	if n := r.ready(t); n != 0 {
		t.Fatalf("%d messages in the queue: invalid ones must be rejected, not requeued", n)
	}
	if got := store.snapshot()[0].ev.SubmissionID.String(); got != marker {
		t.Fatalf("store saw %s", got)
	}
}

func TestDBConsumerRetriesDatabaseErrorsWithoutRequeueing(t *testing.T) {
	r := newRig(t)
	store := &fakeStore{failNext: 2}
	stop := runDB(t, r, store)

	r.publish(t, `{"submission_id":"`+uuid.NewString()+`","status":"COMPLETED","verdict":"RUNTIME_ERROR"}`)
	eventually(t, func() bool { return store.appliedCount() == 1 }, "the verdict applied after 2 failures")
	stop()

	if calls := len(store.snapshot()); calls != 3 {
		t.Fatalf("%d store calls, want 3 (2 failures + 1 success)", calls)
	}
	if n := r.ready(t); n != 0 {
		t.Fatalf("%d messages left, want 0", n)
	}
}

func TestDBConsumerShutdownReturnsUnfinishedMessage(t *testing.T) {
	r := newRig(t)
	store := &fakeStore{failNext: 1 << 30} // database never comes back
	stop := runDB(t, r, store)

	r.publish(t, `{"submission_id":"`+uuid.NewString()+`","status":"RUNNING"}`)
	eventually(t, func() bool { return len(store.snapshot()) >= 1 }, "a first attempt")
	stop() // must not hang in the retry loop

	if n := r.ready(t); n != 1 {
		t.Fatalf("%d messages in the queue, want the unfinished one given back", n)
	}
}

func TestSSEConsumerFeedsHub(t *testing.T) {
	r := newRig(t)
	hub := sse.NewHub()
	id := uuid.New()
	sub := hub.Subscribe(id)

	c := &SSEConsumer{Conn: r.conn, Hub: hub, Exchange: r.exchange, Log: quiet()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	// Subscribing triggers a resync, which also tells us the queue is bound and it is safe to publish.
	select {
	case m := <-sub.C:
		if !m.Resync {
			t.Fatalf("got %+v, want resync first", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no resync after subscribing")
	}

	r.publish(t, `garbage`)
	r.publish(t, `{"submission_id":"`+id.String()+`","status":"RUNNING"}`)
	select {
	case m := <-sub.C:
		if m.Event == nil || m.Event.Status != model.StatusRunning {
			t.Fatalf("got %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event never reached the hub")
	}
}
