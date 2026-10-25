package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

// Tests run against a real PostgreSQL: TEST_DATABASE_URL, or the docker-compose one by default.
// Each test gets its own throwaway database. Without TEST_DATABASE_URL the tests are skipped if the server is
// unreachable; with it (as in CI) they fail, so a missing database can never look like a green run.
const defaultTestDatabaseURL = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"

func newTestRepo(t *testing.T) (*Repository, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	adminURL, explicit := os.LookupEnv("TEST_DATABASE_URL")
	if !explicit {
		adminURL = defaultTestDatabaseURL
	}
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		if explicit {
			t.Fatalf("TEST_DATABASE_URL not reachable: %v", err)
		}
		t.Skipf("PostgreSQL not reachable (%v); run `docker compose up -d postgres` or set TEST_DATABASE_URL", err)
	}
	dbName := "submission_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatal(err)
	}

	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = dbName
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		admin.Exec(ctx, "DROP DATABASE "+dbName+" WITH (FORCE)") //nolint:errcheck
		admin.Close(ctx)
	})

	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return New(pool), pool
}

// createSubmission inserts a fresh QUEUED submission the way the submit handler will.
func createSubmission(t *testing.T, repo *Repository, userID uuid.UUID, problemID string, createdAt time.Time) *model.Submission {
	t.Helper()
	s := &model.Submission{
		ID: uuid.New(), UserID: userID, ProblemID: problemID, Language: "python",
		Code: "print(1)", CreatedAt: createdAt.UTC().Truncate(time.Microsecond),
	}
	payload, _ := json.Marshal(model.JobMessage{
		SubmissionID: s.ID, UserID: s.UserID, ProblemID: s.ProblemID, Language: s.Language, Code: s.Code,
		SubmittedAt: s.CreatedAt,
	})
	ev := &model.OutboxEvent{AggregateID: s.ID, EventType: model.EventSubmissionCreated, Payload: payload}
	if err := repo.CreateSubmissionWithOutbox(context.Background(), s, ev); err != nil {
		t.Fatal(err)
	}
	return s
}

func verdictEvent(id uuid.UUID, v model.Verdict, timeMS int) model.StatusEvent {
	return model.StatusEvent{
		SubmissionID: id, Status: model.StatusCompleted, Verdict: &v,
		ExecutionTimeMS: timeMS, MemoryKB: 14200, TestCasesPassed: 55, TotalTestCases: 55,
	}
}

func TestMigrateTwiceIsNoOp(t *testing.T) {
	_, pool := newTestRepo(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestCreateSubmissionWithOutbox(t *testing.T) {
	repo, pool := newTestRepo(t)
	ctx := context.Background()
	s := createSubmission(t, repo, uuid.New(), "two-sum", time.Now())

	got, err := repo.GetSubmissionByID(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusQueued || got.Verdict != nil || got.Code != "print(1)" || got.UserID != s.UserID {
		t.Fatalf("unexpected submission: %+v", got)
	}
	if !got.CreatedAt.Equal(s.CreatedAt) {
		t.Fatalf("created_at = %v, want %v", got.CreatedAt, s.CreatedAt)
	}

	var status string
	var payload model.JobMessage
	if err := pool.QueryRow(ctx, `SELECT status, payload FROM outbox_events WHERE aggregate_id = $1`, s.ID).
		Scan(&status, &payload); err != nil {
		t.Fatal(err)
	}
	if status != "PENDING" || payload.SubmissionID != s.ID || payload.ProblemID != "two-sum" {
		t.Fatalf("unexpected outbox row: status=%s payload=%+v", status, payload)
	}
}

func TestCreateSubmissionWithOutboxIsAtomic(t *testing.T) {
	repo, pool := newTestRepo(t)
	ctx := context.Background()
	s := &model.Submission{ID: uuid.New(), UserID: uuid.New(), ProblemID: "two-sum", Language: "python",
		Code: "x", CreatedAt: time.Now()}
	// Invalid JSON makes the second INSERT fail after the first succeeded.
	ev := &model.OutboxEvent{AggregateID: s.ID, EventType: model.EventSubmissionCreated, Payload: []byte("{not json")}

	if err := repo.CreateSubmissionWithOutbox(ctx, s, ev); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := repo.GetSubmissionByID(ctx, s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("submission must have been rolled back, got err=%v", err)
	}
	var n int
	pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events`).Scan(&n) //nolint:errcheck
	if n != 0 {
		t.Fatalf("outbox has %d rows, want 0", n)
	}
}

func TestGetSubmissionNotFound(t *testing.T) {
	repo, _ := newTestRepo(t)
	if _, err := repo.GetSubmissionByID(context.Background(), uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestStatusOnlyMovesForward(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	s := createSubmission(t, repo, uuid.New(), "two-sum", time.Now())

	mustBool := func(want bool, got bool, err error, what string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if got != want {
			t.Fatalf("%s: applied = %v, want %v", what, got, want)
		}
	}

	ok, err := repo.MarkRunning(ctx, s.ID)
	mustBool(true, ok, err, "first RUNNING")
	ok, err = repo.MarkRunning(ctx, s.ID)
	mustBool(false, ok, err, "duplicate RUNNING")

	ok, err = repo.UpdateVerdictIdempotent(ctx, verdictEvent(s.ID, model.VerdictAccepted, 38))
	mustBool(true, ok, err, "first verdict")
	ok, err = repo.UpdateVerdictIdempotent(ctx, verdictEvent(s.ID, model.VerdictWrongAnswer, 99))
	mustBool(false, ok, err, "second verdict")
	ok, err = repo.MarkRunning(ctx, s.ID)
	mustBool(false, ok, err, "RUNNING after COMPLETED")

	got, err := repo.GetSubmissionByID(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusCompleted || *got.Verdict != model.VerdictAccepted || got.ExecutionTimeMS != 38 {
		t.Fatalf("first verdict must win, got %+v", got)
	}
}

func TestVerdictWithoutRunning(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	s := createSubmission(t, repo, uuid.New(), "two-sum", time.Now())

	ok, err := repo.UpdateVerdictIdempotent(ctx, verdictEvent(s.ID, model.VerdictTimeLimitExceeded, 2000))
	if err != nil || !ok {
		t.Fatalf("verdict on a QUEUED submission: ok=%v err=%v", ok, err)
	}
}

func TestVerdictRequiresValidVerdict(t *testing.T) {
	repo, _ := newTestRepo(t)
	s := createSubmission(t, repo, uuid.New(), "two-sum", time.Now())
	ev := verdictEvent(s.ID, "NOPE", 1)
	if _, err := repo.UpdateVerdictIdempotent(context.Background(), ev); err == nil {
		t.Fatal("expected an error for an unknown verdict")
	}
}

func TestConcurrentDuplicateVerdictsApplyOnce(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	s := createSubmission(t, repo, uuid.New(), "two-sum", time.Now())

	var applied atomic.Int32
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := repo.UpdateVerdictIdempotent(ctx, verdictEvent(s.ID, model.VerdictAccepted, i))
			if err != nil {
				t.Error(err)
			}
			if ok {
				applied.Add(1)
			}
		}()
	}
	wg.Wait()
	if applied.Load() != 1 {
		t.Fatalf("%d verdicts applied, want exactly 1", applied.Load())
	}
}

func TestListSubmissionsPaginates(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	user, other := uuid.New(), uuid.New()
	base := time.Now().Add(-time.Hour)
	var want []uuid.UUID // newest first
	for i := range 5 {
		problem := "two-sum"
		if i%2 == 1 {
			problem = "add-two-numbers"
		}
		s := createSubmission(t, repo, user, problem, base.Add(time.Duration(i)*time.Minute))
		want = append([]uuid.UUID{s.ID}, want...)
	}
	createSubmission(t, repo, other, "two-sum", base)

	var got []uuid.UUID
	var after *Cursor
	for {
		page, err := repo.ListSubmissions(ctx, ListFilter{UserID: user, Limit: 2, After: after})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range page {
			if s.Code != "" {
				t.Fatal("listing must not include code")
			}
			got = append(got, s.ID)
		}
		if len(page) < 2 {
			break
		}
		last := page[len(page)-1]
		after = &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}

	page, err := repo.ListSubmissions(ctx, ListFilter{UserID: user, ProblemID: "add-two-numbers", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("problem filter returned %d rows, want 2", len(page))
	}
}

func outboxStatus(t *testing.T, pool *pgxpool.Pool, aggregateID uuid.UUID) (status string, retries int) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`SELECT status, retry_count FROM outbox_events WHERE aggregate_id = $1`, aggregateID).Scan(&status, &retries); err != nil {
		t.Fatal(err)
	}
	return status, retries
}

func TestProcessOutboxBatchPublishesOldestFirst(t *testing.T) {
	repo, pool := newTestRepo(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Minute)
	a := createSubmission(t, repo, uuid.New(), "two-sum", base)
	b := createSubmission(t, repo, uuid.New(), "two-sum", base.Add(time.Second))

	var order []uuid.UUID
	res, err := repo.ProcessOutboxBatch(ctx, 10, 5, func(_ context.Context, ev model.OutboxEvent) error {
		var job model.JobMessage
		if err := json.Unmarshal(ev.Payload, &job); err != nil {
			t.Fatalf("payload is not a job message: %v", err)
		}
		order = append(order, job.SubmissionID)
		return nil
	})
	if err != nil || res.Published != 2 || res.PublishErr != nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if order[0] != a.ID || order[1] != b.ID {
		t.Fatalf("published out of order: %v", order)
	}
	if st, _ := outboxStatus(t, pool, a.ID); st != "PUBLISHED" {
		t.Fatalf("status = %s, want PUBLISHED", st)
	}

	res, err = repo.ProcessOutboxBatch(ctx, 10, 5, func(context.Context, model.OutboxEvent) error {
		t.Fatal("nothing should be pending")
		return nil
	})
	if err != nil || res.Published != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestProcessOutboxBatchStopsAtFirstFailure(t *testing.T) {
	repo, pool := newTestRepo(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Minute)
	a := createSubmission(t, repo, uuid.New(), "two-sum", base)
	b := createSubmission(t, repo, uuid.New(), "two-sum", base.Add(time.Second))

	brokerDown := errors.New("broker down")
	calls := 0
	fail := func(context.Context, model.OutboxEvent) error { calls++; return brokerDown }

	res, err := repo.ProcessOutboxBatch(ctx, 10, 3, fail)
	if err != nil || !errors.Is(res.PublishErr, brokerDown) || calls != 1 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, calls)
	}
	if st, n := outboxStatus(t, pool, a.ID); st != "PENDING" || n != 1 {
		t.Fatalf("first event: %s/%d, want PENDING/1", st, n)
	}
	if st, n := outboxStatus(t, pool, b.ID); st != "PENDING" || n != 0 {
		t.Fatalf("second event must be untouched: %s/%d", st, n)
	}

	repo.ProcessOutboxBatch(ctx, 10, 3, fail) //nolint:errcheck
	repo.ProcessOutboxBatch(ctx, 10, 3, fail) //nolint:errcheck
	if st, n := outboxStatus(t, pool, a.ID); st != "FAILED" || n != 3 {
		t.Fatalf("after max retries: %s/%d, want FAILED/3", st, n)
	}
}

func TestConcurrentRelaysNeverPublishTwice(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	const n = 30
	for i := range n {
		createSubmission(t, repo, uuid.New(), "two-sum", time.Now().Add(time.Duration(i)*time.Millisecond))
	}

	var mu sync.Mutex
	seen := map[uuid.UUID]int{}
	publish := func(_ context.Context, ev model.OutboxEvent) error {
		time.Sleep(2 * time.Millisecond) // keep the row locks held while the other relays look
		mu.Lock()
		seen[ev.AggregateID]++
		mu.Unlock()
		return nil
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				res, err := repo.ProcessOutboxBatch(ctx, 5, 5, publish)
				if err != nil {
					t.Error(err)
					return
				}
				if res.Published == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()
	if len(seen) != n {
		t.Fatalf("published %d distinct events, want %d", len(seen), n)
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("event %s published %d times", id, c)
		}
	}
}

func TestReapStuck(t *testing.T) {
	repo, pool := newTestRepo(t)
	ctx := context.Background()
	user := uuid.New()
	oldQueued := createSubmission(t, repo, user, "two-sum", time.Now())
	oldRunning := createSubmission(t, repo, user, "two-sum", time.Now())
	freshRunning := createSubmission(t, repo, user, "two-sum", time.Now())
	done := createSubmission(t, repo, user, "two-sum", time.Now())

	repo.MarkRunning(ctx, oldRunning.ID)                                                               //nolint:errcheck
	repo.MarkRunning(ctx, freshRunning.ID)                                                             //nolint:errcheck
	repo.UpdateVerdictIdempotent(ctx, verdictEvent(done.ID, model.VerdictAccepted, 1))                 //nolint:errcheck
	pool.Exec(ctx, `UPDATE submissions SET updated_at = NOW() - INTERVAL '1 hour' WHERE id = ANY($1)`, //nolint:errcheck
		[]uuid.UUID{oldQueued.ID, oldRunning.ID, done.ID})

	now := time.Now()
	reaped, err := repo.ReapStuck(ctx, now.Add(-30*time.Minute), now.Add(-5*time.Minute), "judge timed out")
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]bool{}
	for _, s := range reaped {
		got[s.ID] = true
		if s.Status != model.StatusCompleted || *s.Verdict != model.VerdictInternalError {
			t.Fatalf("reaped row not completed as INTERNAL_ERROR: %+v", s)
		}
	}
	if len(got) != 2 || !got[oldQueued.ID] || !got[oldRunning.ID] {
		t.Fatalf("reaped %v, want exactly the old QUEUED and old RUNNING submissions", got)
	}

	// A verdict arriving after the reaper gave up is ignored.
	ok, err := repo.UpdateVerdictIdempotent(ctx, verdictEvent(oldRunning.ID, model.VerdictAccepted, 1))
	if err != nil || ok {
		t.Fatalf("late verdict applied=%v err=%v, want ignored", ok, err)
	}
}

func TestDeletePublishedBefore(t *testing.T) {
	repo, pool := newTestRepo(t)
	ctx := context.Background()
	a := createSubmission(t, repo, uuid.New(), "two-sum", time.Now())
	createSubmission(t, repo, uuid.New(), "two-sum", time.Now()) // stays PENDING

	repo.ProcessOutboxBatch(ctx, 1, 5, func(context.Context, model.OutboxEvent) error { return nil })                 //nolint:errcheck
	pool.Exec(ctx, `UPDATE outbox_events SET published_at = NOW() - INTERVAL '2 days' WHERE aggregate_id = $1`, a.ID) //nolint:errcheck

	n, err := repo.DeletePublishedBefore(ctx, time.Now().Add(-24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("deleted %d err=%v, want 1", n, err)
	}
	var left int
	pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE status = 'PENDING'`).Scan(&left) //nolint:errcheck
	if left != 1 {
		t.Fatalf("pending rows left = %d, want 1", left)
	}
}
