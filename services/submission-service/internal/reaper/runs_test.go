package reaper

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeRunStore struct {
	failBefore, deleteBefore time.Time
	message                  string
	failErr                  error
}

func (f *fakeRunStore) FailStaleRuns(_ context.Context, before time.Time, message string) (int64, error) {
	f.failBefore, f.message = before, message
	return 2, f.failErr
}

func (f *fakeRunStore) DeleteRunsBefore(_ context.Context, before time.Time) (int64, error) {
	f.deleteBefore = before
	return 5, nil
}

func TestRunJanitorSweep(t *testing.T) {
	store := &fakeRunStore{}
	j := NewRunJanitor(store, RunJanitorOptions{Interval: time.Second, Timeout: 2 * time.Minute, Retention: 24 * time.Hour},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	j.now = func() time.Time { return now }

	failed, deleted := j.SweepOnce(context.Background())
	if failed != 2 || deleted != 5 {
		t.Fatalf("got %d failed, %d deleted", failed, deleted)
	}
	if !store.failBefore.Equal(now.Add(-2*time.Minute)) || !store.deleteBefore.Equal(now.Add(-24*time.Hour)) ||
		store.message != RunMessage {
		t.Fatalf("unexpected cut-offs %v %v %q", store.failBefore, store.deleteBefore, store.message)
	}

	// A failure in one step must not stop the other.
	store.failErr = errors.New("db down")
	if _, deleted := j.SweepOnce(context.Background()); deleted != 5 {
		t.Fatal("deleting old runs should still happen when failing stale ones errors")
	}
}
