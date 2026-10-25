package reaper

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

type fakeStore struct {
	queuedBefore, runningBefore time.Time
	message                     string
	result                      []model.Submission
}

func (f *fakeStore) ReapStuck(_ context.Context, q, r time.Time, msg string) ([]model.Submission, error) {
	f.queuedBefore, f.runningBefore, f.message = q, r, msg
	return f.result, nil
}

type fakePub struct {
	err  error
	msgs []amqp.Publishing
	exch []string
}

func (p *fakePub) Publish(_ context.Context, exchange, _ string, m amqp.Publishing) error {
	p.exch = append(p.exch, exchange)
	p.msgs = append(p.msgs, m)
	return p.err
}

type fakeHub struct{ events []model.StatusEvent }

func (h *fakeHub) Publish(ev model.StatusEvent) { h.events = append(h.events, ev) }

func reaped() model.Submission {
	v := model.VerdictInternalError
	msg := Message
	return model.Submission{ID: uuid.New(), UserID: uuid.New(), ProblemID: "two-sum",
		Status: model.StatusCompleted, Verdict: &v, ErrorMessage: &msg}
}

func newReaper(store *fakeStore, pub *fakePub, hub *fakeHub) *Reaper {
	r := New(store, pub, hub, Options{Interval: time.Second, QueuedTimeout: 30 * time.Minute,
		RunningTimeout: 5 * time.Minute, Exchange: "verdict.fanout"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.now = func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }
	return r
}

func TestReapAnnouncesThroughFanout(t *testing.T) {
	s := reaped()
	store, pub, hub := &fakeStore{result: []model.Submission{s}}, &fakePub{}, &fakeHub{}
	if n := newReaper(store, pub, hub).ReapOnce(context.Background()); n != 1 {
		t.Fatalf("reaped %d", n)
	}

	if store.queuedBefore != time.Date(2026, 10, 25, 11, 30, 0, 0, time.UTC) ||
		store.runningBefore != time.Date(2026, 10, 25, 11, 55, 0, 0, time.UTC) || store.message != Message {
		t.Fatalf("cutoffs %v / %v, message %q", store.queuedBefore, store.runningBefore, store.message)
	}
	if len(pub.msgs) != 1 || pub.exch[0] != "verdict.fanout" || pub.msgs[0].MessageId != s.ID.String() {
		t.Fatalf("published %+v to %v", pub.msgs, pub.exch)
	}
	var ev model.StatusEvent
	if err := json.Unmarshal(pub.msgs[0].Body, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Status != model.StatusCompleted || *ev.Verdict != model.VerdictInternalError || ev.UserID != s.UserID {
		t.Fatalf("event %+v", ev)
	}
	if len(hub.events) != 0 {
		t.Fatal("must not also publish locally when the fanout worked (the SSE consumer delivers it)")
	}
}

func TestReapFallsBackToLocalHub(t *testing.T) {
	store, pub, hub := &fakeStore{result: []model.Submission{reaped(), reaped()}}, &fakePub{err: errors.New("down")}, &fakeHub{}
	newReaper(store, pub, hub).ReapOnce(context.Background())
	if len(hub.events) != 2 {
		t.Fatalf("local hub got %d events, want 2", len(hub.events))
	}
}
