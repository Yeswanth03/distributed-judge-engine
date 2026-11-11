package reaper

import (
	"context"
	"log/slog"
	"time"
)

// RunMessage is the error_message of a run the judge never answered; it is shown to the user.
const RunMessage = "The judge did not run your code in time. Please try again."

type RunStore interface {
	FailStaleRuns(ctx context.Context, createdBefore time.Time, message string) (int64, error)
	DeleteRunsBefore(ctx context.Context, before time.Time) (int64, error)
}

type RunJanitorOptions struct {
	Interval  time.Duration
	Timeout   time.Duration // a run still QUEUED after this is FAILED
	Retention time.Duration // runs older than this are deleted
}

// RunJanitor fails runs the judge never answered and deletes old ones. Several replicas may run it: both are
// single guarded statements.
type RunJanitor struct {
	store RunStore
	opts  RunJanitorOptions
	log   *slog.Logger
	now   func() time.Time
}

func NewRunJanitor(store RunStore, opts RunJanitorOptions, log *slog.Logger) *RunJanitor {
	return &RunJanitor{store: store, opts: opts, log: log, now: time.Now}
}

// Run sweeps every Interval until ctx is cancelled.
func (j *RunJanitor) Run(ctx context.Context) {
	t := time.NewTicker(j.opts.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.SweepOnce(ctx)
		}
	}
}

// SweepOnce fails stale runs and deletes expired ones, returning how many of each.
func (j *RunJanitor) SweepOnce(ctx context.Context) (failed, deleted int64) {
	now := j.now()
	failed, err := j.store.FailStaleRuns(ctx, now.Add(-j.opts.Timeout), RunMessage)
	if err != nil {
		j.log.Error("failing stale runs failed", "error", err)
	} else if failed > 0 {
		j.log.Warn("failed runs the judge never answered", "count", failed)
	}
	deleted, err = j.store.DeleteRunsBefore(ctx, now.Add(-j.opts.Retention))
	if err != nil {
		j.log.Error("deleting old runs failed", "error", err)
	} else if deleted > 0 {
		j.log.Info("deleted old runs", "count", deleted)
	}
	return failed, deleted
}
