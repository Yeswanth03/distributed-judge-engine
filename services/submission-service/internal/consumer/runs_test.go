package consumer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestParseRunResult(t *testing.T) {
	id := uuid.New()
	got, err := ParseRunResult([]byte(`{"run_id":"` + id.String() + `","status":"COMPLETED","cases":[]}`))
	if err != nil || got != id {
		t.Fatalf("valid result: %v %v", got, err)
	}
	for name, body := range map[string]string{
		"not json":       `nope`,
		"no run_id":      `{"status":"COMPLETED"}`,
		"bad run_id":     `{"run_id":"x","status":"COMPLETED"}`,
		"wrong status":   `{"run_id":"` + id.String() + `","status":"RUNNING"}`,
		"missing status": `{"run_id":"` + id.String() + `"}`,
	} {
		if _, err := ParseRunResult([]byte(body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// fakeAcker records what the consumer did with a delivery.
type fakeAcker struct{ acked, nacked, requeued bool }

func (a *fakeAcker) Ack(uint64, bool) error { a.acked = true; return nil }
func (a *fakeAcker) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked, a.requeued = true, requeue
	return nil
}
func (a *fakeAcker) Reject(uint64, bool) error { return nil }

type fakeRunStore struct {
	results  map[uuid.UUID][]byte
	failures int // CompleteRun fails this many times first
}

func (f *fakeRunStore) CompleteRun(_ context.Context, id uuid.UUID, result []byte) (bool, error) {
	if f.failures > 0 {
		f.failures--
		return false, errors.New("db down")
	}
	if _, done := f.results[id]; done {
		return false, nil
	}
	f.results[id] = result
	return true, nil
}

func newRunConsumer(store *fakeRunStore) *RunResultConsumer {
	return &RunResultConsumer{Store: store, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func deliver(c *RunResultConsumer, body string) *fakeAcker {
	a := &fakeAcker{}
	c.handle(context.Background(), amqp.Delivery{Acknowledger: a, Body: []byte(body)})
	return a
}

func TestRunResultConsumerStoresAndAcks(t *testing.T) {
	store := &fakeRunStore{results: map[uuid.UUID][]byte{}, failures: 1}
	c := newRunConsumer(store)
	id := uuid.New()
	body := `{"run_id":"` + id.String() + `","status":"COMPLETED","verdict":"ACCEPTED"}`

	// The first database error is retried in place, then the result is stored and acked.
	if a := deliver(c, body); !a.acked || a.nacked {
		t.Fatalf("expected ack after a retried database error, got %+v", a)
	}
	if string(store.results[id]) != body {
		t.Fatalf("expected the message stored as received, got %s", store.results[id])
	}
	// A duplicate is acked too: there is nothing to retry.
	if a := deliver(c, body); !a.acked {
		t.Fatalf("expected a duplicate to be acked, got %+v", a)
	}
}

func TestRunResultConsumerDeadLettersInvalidMessages(t *testing.T) {
	c := newRunConsumer(&fakeRunStore{results: map[uuid.UUID][]byte{}})
	if a := deliver(c, `{"status":"COMPLETED"}`); !a.nacked || a.requeued {
		t.Fatalf("expected nack without requeue (to the DLQ), got %+v", a)
	}
}
