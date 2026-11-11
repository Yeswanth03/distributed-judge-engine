package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/repository"
)

type fakeRunStore struct {
	mu        sync.Mutex
	runs      map[uuid.UUID]*model.Run
	events    []model.OutboxEvent
	recent    int // what CountRunsSince answers
	countErr  error
	createErr error
}

func newFakeRunStore() *fakeRunStore { return &fakeRunStore{runs: map[uuid.UUID]*model.Run{}} }

func (f *fakeRunStore) CreateRunWithOutbox(_ context.Context, run *model.Run, ev *model.OutboxEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	run.Status, run.UpdatedAt = model.RunQueued, run.CreatedAt
	cp := *run
	f.runs[run.ID] = &cp
	f.events = append(f.events, *ev)
	return nil
}

func (f *fakeRunStore) GetRunByID(_ context.Context, id uuid.UUID) (*model.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *run
	return &cp, nil
}

func (f *fakeRunStore) CountRunsSince(_ context.Context, _ uuid.UUID, _ time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recent, f.countErr
}

func newRunServer(t *testing.T) (*testServer, *fakeRunStore) {
	t.Helper()
	runs := newFakeRunStore()
	ts := newTestServer(t, func(o *Options) {
		o.Runs = runs
		o.RunsPerMinute = 3
	})
	return ts, runs
}

const problem = "11111111-1111-1111-1111-111111111111"

func TestCreateRunQueuesJobMessage(t *testing.T) {
	ts, runs := newRunServer(t)
	body := `{"problem_id":"` + problem + `","language":"python","code":"print(1)","inputs":["{\"a\":1}","{\"a\":2}"]}`
	rec := ts.do(t, "POST", "/api/v1/runs", &alice, body)
	if rec.Code != 202 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	resp := decode[runResponse](t, rec)
	if resp.Status != model.RunQueued || rec.Header().Get("Location") != "/api/v1/runs/"+resp.RunID.String() {
		t.Fatalf("unexpected response %+v, Location %q", resp, rec.Header().Get("Location"))
	}
	if ts.nudges != 1 || len(runs.events) != 1 || runs.events[0].EventType != model.EventRunRequested {
		t.Fatalf("expected one RUN_REQUESTED event and a nudge, got %d events, %d nudges", len(runs.events), ts.nudges)
	}
	var job model.RunJobMessage
	if err := json.Unmarshal(runs.events[0].Payload, &job); err != nil {
		t.Fatal(err)
	}
	if job.RunID != resp.RunID || job.UserID != alice || job.ProblemID != problem || job.Code != "print(1)" ||
		len(job.Inputs) != 2 || job.Inputs[1] != `{"a":2}` {
		t.Fatalf("unexpected job %+v", job)
	}
}

func TestCreateRunWithoutInputsSendsNoInputs(t *testing.T) {
	ts, runs := newRunServer(t)
	rec := ts.do(t, "POST", "/api/v1/runs", &alice, validBody)
	if rec.Code != 202 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var job model.RunJobMessage
	if err := json.Unmarshal(runs.events[0].Payload, &job); err != nil {
		t.Fatal(err)
	}
	if len(job.Inputs) != 0 {
		t.Fatalf("expected no inputs (the worker then runs the samples), got %v", job.Inputs)
	}
}

func TestCreateRunRejections(t *testing.T) {
	ts, _ := newRunServer(t)
	run := func(fields string) string {
		return `{"problem_id":"` + problem + `","language":"python","code":"x"` + fields + `}`
	}
	tooMany := `,"inputs":["1","2","3","4","5","6","7","8","9","10","11"]`
	cases := []struct {
		name   string
		user   *uuid.UUID
		body   string
		status int
		code   string
	}{
		{"no user", nil, run(""), 401, "unauthorized"},
		{"malformed json", &alice, `{"code":`, 400, "bad_request"},
		{"slug instead of uuid", &alice, `{"problem_id":"two-sum","language":"python","code":"x"}`, 422, "validation_failed"},
		{"blank code", &alice, `{"problem_id":"` + problem + `","language":"python","code":" "}`, 422, "validation_failed"},
		{"too many inputs", &alice, run(tooMany), 422, "validation_failed"},
		{"empty input", &alice, run(`,"inputs":["  "]`), 422, "validation_failed"},
		{"inputs over limit", &alice, run(`,"inputs":["` + strings.Repeat("1", 101) + `"]`), 413, "payload_too_large"},
		{"code over limit", &alice, `{"problem_id":"` + problem + `","language":"python","code":"` + strings.Repeat("x", 101) + `"}`, 413, "payload_too_large"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectError(t, ts.do(t, "POST", "/api/v1/runs", c.user, c.body), c.status, c.code)
		})
	}
}

func TestCreateRunRateLimit(t *testing.T) {
	ts, runs := newRunServer(t)
	runs.recent = 3 // already at the limit of 3 per minute
	rec := ts.do(t, "POST", "/api/v1/runs", &alice, validBody)
	expectError(t, rec, 429, "too_many_requests")
	if rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After %q", rec.Header().Get("Retry-After"))
	}
	if len(runs.events) != 0 {
		t.Fatal("a refused run must not be stored")
	}
}

func TestCreateRunStoreFailures(t *testing.T) {
	ts, runs := newRunServer(t)
	runs.countErr = errors.New("db down")
	expectError(t, ts.do(t, "POST", "/api/v1/runs", &alice, validBody), 500, "internal_error")
	runs.countErr, runs.createErr = nil, errors.New("db down")
	expectError(t, ts.do(t, "POST", "/api/v1/runs", &alice, validBody), 500, "internal_error")
}

func TestGetRun(t *testing.T) {
	ts, runs := newRunServer(t)
	id := decode[runResponse](t, ts.do(t, "POST", "/api/v1/runs", &alice, validBody)).RunID

	queued := decode[map[string]any](t, ts.do(t, "GET", "/api/v1/runs/"+id.String(), &alice, ""))
	if queued["status"] != "QUEUED" || queued["result"] != nil {
		t.Fatalf("unexpected queued run %v", queued)
	}

	runs.runs[id].Status = model.RunCompleted
	runs.runs[id].Result = []byte(`{"run_id":"` + id.String() + `","status":"COMPLETED","verdict":"ACCEPTED","cases":[]}`)
	done := decode[map[string]any](t, ts.do(t, "GET", "/api/v1/runs/"+id.String(), &alice, ""))
	result, _ := done["result"].(map[string]any)
	if done["status"] != "COMPLETED" || result["verdict"] != "ACCEPTED" {
		t.Fatalf("expected the worker's result passed through, got %v", done)
	}

	// Someone else's run, an unknown one and a malformed ID all look the same.
	expectError(t, ts.do(t, "GET", "/api/v1/runs/"+id.String(), &bob, ""), 404, "not_found")
	expectError(t, ts.do(t, "GET", "/api/v1/runs/"+uuid.NewString(), &alice, ""), 404, "not_found")
	expectError(t, ts.do(t, "GET", "/api/v1/runs/nope", &alice, ""), 404, "not_found")
	if rec := ts.do(t, "GET", "/api/v1/runs/"+id.String(), &bob, "", "X-User-Role", "admin"); rec.Code != 200 {
		t.Fatalf("admin should see any run, got %d", rec.Code)
	}
}

func TestRunRoutesNeedARunStore(t *testing.T) {
	ts := newTestServer(t)
	expectError(t, ts.do(t, "POST", "/api/v1/runs", &alice, validBody), 404, "not_found")
}
