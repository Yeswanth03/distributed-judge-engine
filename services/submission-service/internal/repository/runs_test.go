package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

func createRun(t *testing.T, repo *Repository, userID uuid.UUID, createdAt time.Time) *model.Run {
	t.Helper()
	run := &model.Run{ID: uuid.New(), UserID: userID, ProblemID: "11111111-1111-1111-1111-111111111111",
		Language: "python", CreatedAt: createdAt.UTC().Truncate(time.Microsecond)}
	payload, _ := json.Marshal(model.RunJobMessage{RunID: run.ID, UserID: userID, ProblemID: run.ProblemID})
	ev := &model.OutboxEvent{AggregateID: run.ID, EventType: model.EventRunRequested, Payload: payload}
	if err := repo.CreateRunWithOutbox(context.Background(), run, ev); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestCreateRunWithOutbox(t *testing.T) {
	repo, pool := newTestRepo(t)
	ctx := context.Background()
	run := createRun(t, repo, uuid.New(), time.Now())

	got, err := repo.GetRunByID(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.RunQueued || got.Result != nil || got.UserID != run.UserID || !got.CreatedAt.Equal(run.CreatedAt) {
		t.Fatalf("unexpected run %+v", got)
	}

	var aggregate, eventType, status string
	err = pool.QueryRow(ctx, `SELECT aggregate_type, event_type, status FROM outbox_events WHERE aggregate_id = $1`,
		run.ID).Scan(&aggregate, &eventType, &status)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate != "RUN" || eventType != model.EventRunRequested || status != "PENDING" {
		t.Fatalf("unexpected outbox row %s %s %s", aggregate, eventType, status)
	}

	if _, err := repo.GetRunByID(ctx, uuid.New()); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCountRunsSince(t *testing.T) {
	repo, _ := newTestRepo(t)
	user := uuid.New()
	now := time.Now()
	createRun(t, repo, user, now.Add(-2*time.Minute))
	createRun(t, repo, user, now.Add(-10*time.Second))
	createRun(t, repo, user, now)
	createRun(t, repo, uuid.New(), now) // someone else

	n, err := repo.CountRunsSince(context.Background(), user, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 runs in the last minute, got %d", n)
	}
}

func TestCompleteRunOnlyOnce(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	run := createRun(t, repo, uuid.New(), time.Now())

	first := []byte(`{"status":"COMPLETED","verdict":"ACCEPTED"}`)
	if ok, err := repo.CompleteRun(ctx, run.ID, first); err != nil || !ok {
		t.Fatalf("first result: %v %v", ok, err)
	}
	if ok, err := repo.CompleteRun(ctx, run.ID, []byte(`{"status":"COMPLETED","verdict":"WRONG_ANSWER"}`)); err != nil || ok {
		t.Fatalf("a duplicate result must change nothing: %v %v", ok, err)
	}
	got, _ := repo.GetRunByID(ctx, run.ID)
	var result map[string]any
	if err := json.Unmarshal(got.Result, &result); err != nil {
		t.Fatal(err)
	}
	if got.Status != model.RunCompleted || result["verdict"] != "ACCEPTED" {
		t.Fatalf("expected the first result to win, got %s %v", got.Status, result)
	}
}

func TestFailStaleAndDeleteOldRuns(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	now := time.Now()
	stale := createRun(t, repo, uuid.New(), now.Add(-5*time.Minute))
	fresh := createRun(t, repo, uuid.New(), now)
	answered := createRun(t, repo, uuid.New(), now.Add(-5*time.Minute))
	if _, err := repo.CompleteRun(ctx, answered.ID, []byte(`{"status":"COMPLETED"}`)); err != nil {
		t.Fatal(err)
	}

	n, err := repo.FailStaleRuns(ctx, now.Add(-2*time.Minute), "gave up")
	if err != nil || n != 1 {
		t.Fatalf("expected exactly the stale run failed, got %d %v", n, err)
	}
	got, _ := repo.GetRunByID(ctx, stale.ID)
	if got.Status != model.RunFailed || got.ErrorMessage == nil || *got.ErrorMessage != "gave up" {
		t.Fatalf("unexpected failed run %+v", got)
	}
	// A result arriving after the janitor gave up is ignored.
	if ok, _ := repo.CompleteRun(ctx, stale.ID, []byte(`{"status":"COMPLETED"}`)); ok {
		t.Fatal("a late result must not revive a failed run")
	}
	if got, _ := repo.GetRunByID(ctx, fresh.ID); got.Status != model.RunQueued {
		t.Fatalf("a fresh run must stay QUEUED, got %s", got.Status)
	}

	deleted, err := repo.DeleteRunsBefore(ctx, now.Add(-time.Minute))
	if err != nil || deleted != 2 {
		t.Fatalf("expected the two old runs deleted, got %d %v", deleted, err)
	}
	if _, err := repo.GetRunByID(ctx, fresh.ID); err != nil {
		t.Fatalf("the fresh run must remain: %v", err)
	}
}
