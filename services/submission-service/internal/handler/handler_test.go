package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/repository"
)

// withClock replaces the handler's clock, so tests control created_at.
func (h *Handler) withClock(now func() time.Time) *Handler {
	h.now = now
	return h
}

// fakeStore keeps submissions in memory and mimics the repository's ordering and cursor rules.
type fakeStore struct {
	mu        sync.Mutex
	subs      map[uuid.UUID]*model.Submission
	events    []model.OutboxEvent
	createErr error
}

func newFakeStore() *fakeStore { return &fakeStore{subs: map[uuid.UUID]*model.Submission{}} }

func (f *fakeStore) CreateSubmissionWithOutbox(_ context.Context, s *model.Submission, ev *model.OutboxEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	s.Status, s.UpdatedAt = model.StatusQueued, s.CreatedAt
	cp := *s
	f.subs[s.ID] = &cp
	f.events = append(f.events, *ev)
	return nil
}

func (f *fakeStore) GetSubmissionByID(_ context.Context, id uuid.UUID) (*model.Submission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.subs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (f *fakeStore) ListSubmissions(_ context.Context, lf repository.ListFilter) ([]model.Submission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.Submission
	for _, s := range f.subs {
		if s.UserID != lf.UserID || (lf.ProblemID != "" && s.ProblemID != lf.ProblemID) {
			continue
		}
		if lf.After != nil && !before(s, lf.After) {
			continue
		}
		cp := *s
		cp.Code = ""
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return before(&out[j], &repository.Cursor{CreatedAt: out[i].CreatedAt, ID: out[i].ID})
	})
	if len(out) > lf.Limit {
		out = out[:lf.Limit]
	}
	return out, nil
}

// before reports whether s sorts after the cursor in newest-first order, i.e. (created_at, id) < cursor.
func before(s *model.Submission, c *repository.Cursor) bool {
	if !s.CreatedAt.Equal(c.CreatedAt) {
		return s.CreatedAt.Before(c.CreatedAt)
	}
	return strings.Compare(s.ID.String(), c.ID.String()) < 0
}

type testServer struct {
	store  *fakeStore
	nudges int
	h      http.Handler
}

func newTestServer(t *testing.T, mutate ...func(*Options)) *testServer {
	t.Helper()
	ts := &testServer{store: newFakeStore()}
	opts := Options{
		MaxCodeBytes:     100,
		AllowedLanguages: []string{"python"},
		NudgeOutbox:      func() { ts.nudges++ },
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, m := range mutate {
		m(&opts)
	}
	ts.h = New(ts.store, opts).Routes(nil)
	return ts
}

var alice, bob = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

func (ts *testServer) do(t *testing.T, method, path string, user *uuid.UUID, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != nil {
		req.Header.Set("X-User-Id", user.String())
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	ts.h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not JSON (%v): %s", err, rec.Body.String())
	}
	return v
}

func expectError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, status, rec.Body.String())
	}
	if got := decode[map[string]string](t, rec)["error"]; got != code {
		t.Fatalf("error = %q, want %q", got, code)
	}
}

const validBody = `{"problem_id":"two-sum","language":"python","code":"print(1)"}`

func TestSubmitAccepted(t *testing.T) {
	ts := newTestServer(t)
	rec := ts.do(t, "POST", "/api/v1/submissions", &alice, validBody)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	resp := decode[submitResponse](t, rec)
	if resp.Status != model.StatusQueued || resp.SubmissionID == uuid.Nil {
		t.Fatalf("unexpected response %+v", resp)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/v1/submissions/"+resp.SubmissionID.String() {
		t.Fatalf("Location = %q", loc)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id")
	}
	if ts.nudges != 1 {
		t.Fatalf("outbox nudged %d times, want 1", ts.nudges)
	}

	stored := ts.store.subs[resp.SubmissionID]
	if stored == nil || stored.UserID != alice || stored.Code != "print(1)" {
		t.Fatalf("stored submission %+v", stored)
	}
	var job model.JobMessage
	if err := json.Unmarshal(ts.store.events[0].Payload, &job); err != nil {
		t.Fatal(err)
	}
	want := model.JobMessage{SubmissionID: resp.SubmissionID, UserID: alice, ProblemID: "two-sum",
		Language: "python", Code: "print(1)", SubmittedAt: resp.CreatedAt}
	if job != want {
		t.Fatalf("job payload\n got %+v\nwant %+v", job, want)
	}
}

func TestSubmitIgnoresUserIDInBody(t *testing.T) {
	ts := newTestServer(t)
	body := `{"user_id":"` + bob.String() + `","problem_id":"two-sum","language":"python","code":"x"}`
	resp := decode[submitResponse](t, ts.do(t, "POST", "/api/v1/submissions", &alice, body))
	if got := ts.store.subs[resp.SubmissionID].UserID; got != alice {
		t.Fatalf("user = %s, want the X-User-Id caller", got)
	}
}

func TestSubmitRejections(t *testing.T) {
	ts := newTestServer(t)
	long := strings.Repeat("x", 101)
	cases := []struct {
		name   string
		user   *uuid.UUID
		body   string
		status int
		code   string
	}{
		{"no user", nil, validBody, 401, "unauthorized"},
		{"malformed json", &alice, `{"problem_id":`, 400, "bad_request"},
		{"bad slug", &alice, `{"problem_id":"Two Sum","language":"python","code":"x"}`, 422, "validation_failed"},
		{"slug too long", &alice, `{"problem_id":"` + strings.Repeat("a", 81) + `","language":"python","code":"x"}`, 422, "validation_failed"},
		{"unknown language", &alice, `{"problem_id":"two-sum","language":"cobol","code":"x"}`, 422, "validation_failed"},
		{"blank code", &alice, `{"problem_id":"two-sum","language":"python","code":"  \n"}`, 422, "validation_failed"},
		{"code over limit", &alice, `{"problem_id":"two-sum","language":"python","code":"` + long + `"}`, 413, "payload_too_large"},
		{"body far over limit", &alice, `{"code":"` + strings.Repeat("x", 100_000) + `"}`, 413, "payload_too_large"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectError(t, ts.do(t, "POST", "/api/v1/submissions", c.user, c.body), c.status, c.code)
		})
	}
	if len(ts.store.subs) != 0 || ts.nudges != 0 {
		t.Fatal("rejected requests must not store anything")
	}
}

func TestSubmitStoreFailure(t *testing.T) {
	ts := newTestServer(t)
	ts.store.createErr = errors.New("db down")
	expectError(t, ts.do(t, "POST", "/api/v1/submissions", &alice, validBody), 500, "internal_error")
	if ts.nudges != 0 {
		t.Fatal("must not nudge the outbox when nothing was stored")
	}
}

func submitAs(t *testing.T, ts *testServer, user uuid.UUID, problem string) uuid.UUID {
	t.Helper()
	body := `{"problem_id":"` + problem + `","language":"python","code":"print(1)"}`
	return decode[submitResponse](t, ts.do(t, "POST", "/api/v1/submissions", &user, body)).SubmissionID
}

func TestGetSubmission(t *testing.T) {
	ts := newTestServer(t)
	id := submitAs(t, ts, alice, "two-sum")
	path := "/api/v1/submissions/" + id.String()

	rec := ts.do(t, "GET", path, &alice, "")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	raw := decode[map[string]any](t, rec)
	if raw["status"] != "QUEUED" || raw["verdict"] != nil || raw["problem_id"] != "two-sum" {
		t.Fatalf("unexpected body %v", raw)
	}
	if _, has := raw["code"]; has {
		t.Fatal("code must only be included on request")
	}
	if _, has := raw["verdict"]; !has {
		t.Fatal("verdict must be present (as null) before judging")
	}

	withCode := decode[map[string]any](t, ts.do(t, "GET", path+"?include=code", &alice, ""))
	if withCode["code"] != "print(1)" {
		t.Fatalf("code = %v", withCode["code"])
	}

	expectError(t, ts.do(t, "GET", path, &bob, ""), 404, "not_found")
	if rec := ts.do(t, "GET", path, &bob, "", "X-User-Role", "admin"); rec.Code != 200 {
		t.Fatalf("admin status %d", rec.Code)
	}
	expectError(t, ts.do(t, "GET", path, nil, ""), 401, "unauthorized")
	expectError(t, ts.do(t, "GET", "/api/v1/submissions/not-a-uuid", &alice, ""), 404, "not_found")
	expectError(t, ts.do(t, "GET", "/api/v1/submissions/"+uuid.NewString(), &alice, ""), 404, "not_found")
}

func TestListSubmissionsPagination(t *testing.T) {
	ts := newTestServer(t)
	clock := time.Date(2026, 10, 25, 11, 0, 0, 0, time.UTC)
	ts.h = New(ts.store, Options{MaxCodeBytes: 100, AllowedLanguages: []string{"python"},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).withClock(func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}).Routes(nil)

	var want []string // newest first
	for i := range 5 {
		problem := "two-sum"
		if i == 2 {
			problem = "add-two-numbers"
		}
		want = append([]string{submitAs(t, ts, alice, problem).String()}, want...)
	}
	submitAs(t, ts, bob, "two-sum")

	type page struct {
		Items []struct {
			SubmissionID string `json:"submission_id"`
			Code         *string
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	var got []string
	path := "/api/v1/submissions?limit=2"
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("pagination does not terminate")
		}
		p := decode[page](t, ts.do(t, "GET", path, &alice, ""))
		for _, it := range p.Items {
			if it.Code != nil {
				t.Fatal("listing must not include code")
			}
			got = append(got, it.SubmissionID)
		}
		if p.NextCursor == nil {
			break
		}
		path = "/api/v1/submissions?limit=2&cursor=" + *p.NextCursor
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got  %v\nwant %v", got, want)
	}

	one := decode[page](t, ts.do(t, "GET", "/api/v1/submissions?problem_id=add-two-numbers", &alice, ""))
	if len(one.Items) != 1 || one.NextCursor != nil {
		t.Fatalf("problem filter: %+v", one)
	}

	empty := ts.do(t, "GET", "/api/v1/submissions", &uuid.UUID{1}, "")
	if !strings.Contains(empty.Body.String(), `"items":[]`) {
		t.Fatalf("empty listing must be [], got %s", empty.Body.String())
	}

	expectError(t, ts.do(t, "GET", "/api/v1/submissions?limit=0", &alice, ""), 400, "bad_request")
	expectError(t, ts.do(t, "GET", "/api/v1/submissions?limit=101", &alice, ""), 400, "bad_request")
	expectError(t, ts.do(t, "GET", "/api/v1/submissions?cursor=!!!", &alice, ""), 400, "bad_request")
	expectError(t, ts.do(t, "GET", "/api/v1/submissions?cursor=bm9waXBl", &alice, ""), 400, "bad_request") // "nopipe"
	expectError(t, ts.do(t, "GET", "/api/v1/submissions?problem_id=NOPE", &alice, ""), 400, "bad_request")
}

func TestCursorRoundTrip(t *testing.T) {
	c := repository.Cursor{CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC), ID: uuid.New()}
	got, err := decodeCursor(encodeCursor(c))
	if err != nil || !got.CreatedAt.Equal(c.CreatedAt) || got.ID != c.ID {
		t.Fatalf("round trip: %+v %v", got, err)
	}
}

func TestProbes(t *testing.T) {
	healthy := newTestServer(t, func(o *Options) {
		o.Readiness = []ReadinessCheck{{Name: "database", Check: func(context.Context) error { return nil }}}
	})
	if rec := healthy.do(t, "GET", "/readyz", nil, ""); rec.Code != 200 {
		t.Fatalf("readyz %d", rec.Code)
	}

	broken := newTestServer(t, func(o *Options) {
		o.Readiness = []ReadinessCheck{
			{Name: "database", Check: func(context.Context) error { return nil }},
			{Name: "broker", Check: func(context.Context) error { return errors.New("not connected") }},
		}
	})
	rec := broken.do(t, "GET", "/readyz", nil, "")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "not connected") {
		t.Fatalf("readyz %d %s", rec.Code, rec.Body.String())
	}
	if rec := broken.do(t, "GET", "/healthz", nil, ""); rec.Code != 200 {
		t.Fatalf("healthz must not depend on dependencies, got %d", rec.Code)
	}
}

func TestUnknownRouteIsJSON404(t *testing.T) {
	expectError(t, newTestServer(t).do(t, "GET", "/nope", nil, ""), 404, "not_found")
}

func TestCORS(t *testing.T) {
	off := newTestServer(t)
	if rec := off.do(t, "GET", "/healthz", nil, "", "Origin", "http://localhost:5173"); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS must be off without CORSOrigin")
	}

	on := newTestServer(t, func(o *Options) { o.CORSOrigin = "http://localhost:5173" })
	rec := on.do(t, "OPTIONS", "/api/v1/submissions", nil, "",
		"Origin", "http://localhost:5173", "Access-Control-Request-Method", "POST")
	if rec.Code != 204 || !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "X-User-Id") {
		t.Fatalf("preflight %d %v", rec.Code, rec.Header())
	}
	rec = on.do(t, "POST", "/api/v1/submissions", &alice, validBody, "Origin", "http://localhost:5173")
	if rec.Code != 202 || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("actual request %d %v", rec.Code, rec.Header())
	}
}

func TestPanicBecomes500(t *testing.T) {
	ts := newTestServer(t, func(o *Options) {
		o.Events = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	})
	expectError(t, ts.do(t, "GET", "/api/v1/submissions/"+uuid.NewString()+"/events", &alice, ""), 500, "internal_error")
}

func TestStatusRecorderKeepsFlusher(t *testing.T) {
	flushed := false
	ts := newTestServer(t, func(o *Options) {
		o.Events = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Errorf("flush through middleware: %v", err)
			}
			flushed = true
		})
	})
	rec := ts.do(t, "GET", "/api/v1/submissions/"+uuid.NewString()+"/events", &alice, "")
	if !flushed || !rec.Flushed {
		t.Fatal("SSE handlers must be able to flush through the middleware chain")
	}
}
