package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/sse"
)

type sseEvent struct {
	name string // "" for a comment line (heartbeat)
	data map[string]any
}

type sseFixture struct {
	store *fakeStore
	hub   *sse.Hub
	srv   *httptest.Server
}

func newSSEFixture(t *testing.T, heartbeat time.Duration) *sseFixture {
	t.Helper()
	f := &sseFixture{store: newFakeStore(), hub: sse.NewHub()}
	h := New(f.store, Options{
		MaxCodeBytes: 100, AllowedLanguages: []string{"python"}, Hub: f.hub, Heartbeat: heartbeat,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	f.srv = httptest.NewServer(h.Routes(nil))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *sseFixture) submission(user uuid.UUID, mutate ...func(*model.Submission)) uuid.UUID {
	s := &model.Submission{ID: uuid.New(), UserID: user, ProblemID: "two-sum", Language: "python", Code: "x",
		CreatedAt: time.Now()}
	f.store.CreateSubmissionWithOutbox(context.Background(), s, &model.OutboxEvent{}) //nolint:errcheck
	for _, m := range mutate {
		f.store.set(s.ID, m)
	}
	return s.ID
}

// open starts a stream and returns its response and a channel of parsed events (closed when the stream ends).
func (f *sseFixture) open(t *testing.T, id uuid.UUID, user uuid.UUID) (*http.Response, <-chan sseEvent) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", f.srv.URL+"/api/v1/submissions/"+id.String()+"/events", nil)
	req.Header.Set("X-User-Id", user.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan sseEvent, 16)
	go func() {
		defer close(events)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		var cur sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, ":"):
				events <- sseEvent{}
			case strings.HasPrefix(line, "event: "):
				cur.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cur.data) //nolint:errcheck
			case line == "" && cur.name != "":
				events <- cur
				cur = sseEvent{}
			}
		}
	}()
	return resp, events
}

func next(t *testing.T, events <-chan sseEvent) sseEvent {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("stream ended early")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event within 2s")
	}
	return sseEvent{}
}

func expectEnd(t *testing.T, events <-chan sseEvent) {
	t.Helper()
	select {
	case ev, ok := <-events:
		if ok {
			t.Fatalf("expected the stream to end, got %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not end")
	}
}

// waitSubscribed waits until exactly n streams are registered with the hub: after opening a stream, so
// published events are not missed; with n = 0, to prove handlers unsubscribe when they end.
func waitSubscribed(t *testing.T, hub *sse.Hub, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for hub.Count() != n {
		if time.Now().After(deadline) {
			t.Fatalf("%d streams subscribed, want %d", hub.Count(), n)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func accepted() *model.Verdict { v := model.VerdictAccepted; return &v }

func TestStreamFullLifecycle(t *testing.T) {
	f := newSSEFixture(t, time.Hour)
	id := f.submission(alice)
	resp, events := f.open(t, id, alice)

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("headers %v", resp.Header)
	}
	if ev := next(t, events); ev.name != "status" || ev.data["status"] != "QUEUED" || ev.data["submission_id"] != id.String() {
		t.Fatalf("first event %+v, want current state QUEUED", ev)
	}

	waitSubscribed(t, f.hub, 1)
	f.hub.Publish(model.StatusEvent{SubmissionID: id, Status: model.StatusRunning})
	if ev := next(t, events); ev.name != "status" || ev.data["status"] != "RUNNING" {
		t.Fatalf("got %+v, want RUNNING", ev)
	}

	f.hub.Publish(model.StatusEvent{SubmissionID: id, Status: model.StatusCompleted, Verdict: accepted(),
		ExecutionTimeMS: 38, MemoryKB: 14200, TestCasesPassed: 55, TotalTestCases: 55})
	ev := next(t, events)
	if ev.name != "verdict" || ev.data["verdict"] != "ACCEPTED" || ev.data["execution_time_ms"] != 38.0 ||
		ev.data["total_test_cases"] != 55.0 {
		t.Fatalf("got %+v, want the verdict", ev)
	}
	if _, has := ev.data["error_message"]; !has {
		t.Fatal("verdict must carry error_message (null when none)")
	}
	expectEnd(t, events)
	waitSubscribed(t, f.hub, 0) // handler unsubscribed on exit
}

func TestStreamIgnoresDuplicatesAndRegressions(t *testing.T) {
	f := newSSEFixture(t, time.Hour)
	id := f.submission(alice, func(s *model.Submission) { s.Status = model.StatusRunning })
	_, events := f.open(t, id, alice)
	if ev := next(t, events); ev.data["status"] != "RUNNING" {
		t.Fatalf("got %+v", ev)
	}
	waitSubscribed(t, f.hub, 1)

	f.hub.Publish(model.StatusEvent{SubmissionID: id, Status: model.StatusQueued})  // older
	f.hub.Publish(model.StatusEvent{SubmissionID: id, Status: model.StatusRunning}) // duplicate
	f.hub.Publish(model.StatusEvent{SubmissionID: id, Status: model.StatusCompleted, Verdict: accepted()})
	if ev := next(t, events); ev.name != "verdict" {
		t.Fatalf("got %+v, want only the verdict", ev)
	}
	expectEnd(t, events)
}

func TestStreamOfFinishedSubmissionSendsVerdictAndCloses(t *testing.T) {
	f := newSSEFixture(t, time.Hour)
	id := f.submission(alice, func(s *model.Submission) {
		s.Status, s.Verdict, s.TestCasesPassed, s.TotalTestCases = model.StatusCompleted, accepted(), 3, 3
	})
	_, events := f.open(t, id, alice)
	if ev := next(t, events); ev.name != "verdict" || ev.data["test_cases_passed"] != 3.0 {
		t.Fatalf("got %+v", ev)
	}
	expectEnd(t, events)
}

func TestStreamResyncReadsDatabase(t *testing.T) {
	f := newSSEFixture(t, time.Hour)
	id := f.submission(alice)
	_, events := f.open(t, id, alice)
	next(t, events) // QUEUED
	waitSubscribed(t, f.hub, 1)

	// The verdict was written while this replica's broker subscription was down, so no event will come.
	f.store.set(id, func(s *model.Submission) { s.Status, s.Verdict = model.StatusCompleted, accepted() })
	f.hub.Resync()
	if ev := next(t, events); ev.name != "verdict" || ev.data["verdict"] != "ACCEPTED" {
		t.Fatalf("got %+v, want the verdict read back from the database", ev)
	}
	expectEnd(t, events)
}

func TestStreamHeartbeat(t *testing.T) {
	f := newSSEFixture(t, 20*time.Millisecond)
	_, events := f.open(t, f.submission(alice), alice)
	next(t, events) // QUEUED
	if ev := next(t, events); ev.name != "" {
		t.Fatalf("got %+v, want a heartbeat comment", ev)
	}
}

func TestStreamEndsWhenHubCloses(t *testing.T) {
	f := newSSEFixture(t, time.Hour)
	_, events := f.open(t, f.submission(alice), alice)
	next(t, events)
	waitSubscribed(t, f.hub, 1)
	f.hub.Close()
	expectEnd(t, events)
}

func TestStreamAccessRules(t *testing.T) {
	f := newSSEFixture(t, time.Hour)
	id := f.submission(alice)
	get := func(path string, user string) int {
		req, _ := http.NewRequest("GET", f.srv.URL+path, nil)
		if user != "" {
			req.Header.Set("X-User-Id", user)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := get("/api/v1/submissions/"+id.String()+"/events", bob.String()); c != 404 {
		t.Fatalf("other user: %d", c)
	}
	if c := get("/api/v1/submissions/"+id.String()+"/events", ""); c != 401 {
		t.Fatalf("anonymous: %d", c)
	}
	if c := get("/api/v1/submissions/"+uuid.NewString()+"/events", alice.String()); c != 404 {
		t.Fatalf("unknown: %d", c)
	}
	if f.hub.Count() != 0 {
		t.Fatal("rejected streams must unsubscribe")
	}
}

func TestStreamEndsWhenClientLeaves(t *testing.T) {
	f := newSSEFixture(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", f.srv.URL+"/api/v1/submissions/"+f.submission(alice).String()+"/events", nil)
	req.Header.Set("X-User-Id", alice.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	waitSubscribed(t, f.hub, 1)
	cancel()
	resp.Body.Close()
	waitSubscribed(t, f.hub, 0) // would hang (and fail) if the handler leaked
	if f.hub.Count() != 0 {
		t.Fatal("handler leaked its subscription")
	}
}
