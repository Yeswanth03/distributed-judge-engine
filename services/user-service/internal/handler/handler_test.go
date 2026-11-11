package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/password"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/repository"
	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/token"
)

// These tests drive the real HTTP handlers against a real PostgreSQL (TEST_DATABASE_URL, or the
// docker-compose one) and a real token issuer: the parts most worth testing here are the database rules.

type testServer struct {
	h      http.Handler
	repo   *repository.Repository
	issuer *token.Issuer
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ctx := context.Background()
	adminURL, explicit := os.LookupEnv("TEST_DATABASE_URL")
	if !explicit {
		adminURL = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		if explicit {
			t.Fatalf("TEST_DATABASE_URL not reachable: %v", err)
		}
		t.Skipf("PostgreSQL not reachable (%v); set TEST_DATABASE_URL", err)
	}
	dbName := "users_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatal(err)
	}
	cfg, _ := pgxpool.ParseConfig(adminURL)
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
	if err := repository.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(pool)
	key, _ := token.GenerateKey()
	issuer, err := token.NewIssuer(key, nil, "test-issuer", "test-audience", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	h := New(repo, issuer, Options{
		SessionTTL: time.Hour, MaxFailedLogins: 3, LoginLockout: 15 * time.Minute,
		PasswordParams: password.Params{MemoryKiB: 64, Iterations: 1, Threads: 1}, // fast hashes for tests
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return &testServer{h: h.Routes(nil), repo: repo, issuer: issuer}
}

// identity is what the gateway would forward after verifying an access token.
type identity struct {
	userID, sessionID, role string
}

func (ts *testServer) do(t *testing.T, method, path string, who *identity, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if who != nil {
		req.Header.Set("X-User-Id", who.userID)
		req.Header.Set("X-Session-Id", who.sessionID)
		req.Header.Set("X-User-Role", who.role)
	}
	rec := httptest.NewRecorder()
	ts.h.ServeHTTP(rec, req)
	return rec
}

func decodeAs[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func expectError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body)
	}
	if got := decodeAs[map[string]string](t, rec)["error"]; got != code {
		t.Fatalf("error %q, want %q", got, code)
	}
}

// claimsOf verifies an access token against the server's JWKS, as the gateway does, and returns its claims.
func (ts *testServer) claimsOf(t *testing.T, raw string) token.Claims {
	t.Helper()
	var set jose.JSONWebKeySet
	rec := ts.do(t, "GET", "/.well-known/jwks.json", nil, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &set); err != nil {
		t.Fatal(err)
	}
	tok, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatal(err)
	}
	var c token.Claims
	if err := tok.Claims(set.Key(tok.Headers[0].KeyID)[0].Key, &c); err != nil {
		t.Fatalf("access token does not verify with the JWKS: %v", err)
	}
	return c
}

func (ts *testServer) register(t *testing.T, email, username, pw string) tokenResponse {
	t.Helper()
	rec := ts.do(t, "POST", "/auth/register", nil,
		`{"email":"`+email+`","username":"`+username+`","password":"`+pw+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	return decodeAs[tokenResponse](t, rec)
}

func (ts *testServer) identityOf(t *testing.T, tr tokenResponse) *identity {
	t.Helper()
	c := ts.claimsOf(t, tr.AccessToken)
	return &identity{userID: c.Subject, sessionID: c.SessionID.String(), role: string(c.Role)}
}

func TestRegisterSignsIn(t *testing.T) {
	ts := newTestServer(t)
	tr := ts.register(t, "ada@example.com", "ada", "correct horse")
	if tr.TokenType != "Bearer" || tr.ExpiresIn != 600 || tr.RefreshToken == "" || tr.User == nil ||
		tr.User.Role != model.RoleUser || tr.User.Username != "ada" || tr.User.DisplayName != "ada" {
		t.Fatalf("unexpected response %+v", tr)
	}
	c := ts.claimsOf(t, tr.AccessToken)
	if c.Subject != tr.User.UserID.String() || c.Role != model.RoleUser || c.SessionID == uuid.Nil {
		t.Fatalf("unexpected claims %+v", c)
	}
	if !strings.Contains(ts.do(t, "POST", "/auth/register", nil, `{"email":"x@y.co"}`).Header().Get("Cache-Control"), "no-store") {
		t.Fatal("token responses must not be cached")
	}
}

func TestRegisterRejections(t *testing.T) {
	ts := newTestServer(t)
	ts.register(t, "taken@example.com", "taken", "correct horse")
	cases := []struct {
		name, body string
		status     int
		code       string
	}{
		{"malformed", `{"email":`, 400, "bad_request"},
		{"unknown field", `{"email":"a@b.co","username":"abc","password":"12345678","role":"admin"}`, 400, "bad_request"},
		{"bad email", `{"email":"nope","username":"abc","password":"12345678"}`, 422, "validation_failed"},
		{"bad username", `{"email":"a@b.co","username":"a b","password":"12345678"}`, 422, "validation_failed"},
		{"short password", `{"email":"a@b.co","username":"abc","password":"1234567"}`, 422, "validation_failed"},
		{"password is username", `{"email":"a@b.co","username":"abcdefgh","password":"ABCDEFGH"}`, 422, "validation_failed"},
		{"email taken", `{"email":"TAKEN@example.com","username":"fresh","password":"12345678"}`, 409, "email_taken"},
		{"username taken", `{"email":"new@example.com","username":"Taken","password":"12345678"}`, 409, "username_taken"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectError(t, ts.do(t, "POST", "/auth/register", nil, c.body), c.status, c.code)
		})
	}
}

func TestLoginFailuresAllLookTheSame(t *testing.T) {
	ts := newTestServer(t)
	ts.register(t, "bob@example.com", "bob", "correct horse")
	wrong := ts.do(t, "POST", "/auth/login", nil, `{"email":"bob@example.com","password":"wrong horse"}`)
	unknown := ts.do(t, "POST", "/auth/login", nil, `{"email":"nobody@example.com","password":"wrong horse"}`)
	expectError(t, wrong, 401, "invalid_credentials")
	expectError(t, unknown, 401, "invalid_credentials")
	if wrong.Body.String() != unknown.Body.String() {
		t.Fatal("an unknown email and a wrong password must get the same answer")
	}

	rec := ts.do(t, "POST", "/auth/login", nil, `{"email":"BOB@example.com","password":"correct horse"}`)
	if rec.Code != 200 || decodeAs[tokenResponse](t, rec).AccessToken == "" {
		t.Fatalf("login with the right password: %d %s", rec.Code, rec.Body)
	}
}

func TestLockoutAfterRepeatedFailures(t *testing.T) {
	ts := newTestServer(t)
	ts.register(t, "cat@example.com", "cat", "correct horse")
	for i := 0; i < 3; i++ { // MaxFailedLogins is 3 in tests
		ts.do(t, "POST", "/auth/login", nil, `{"email":"cat@example.com","password":"guess `+string(rune('a'+i))+`"}`)
	}
	// Locked: even the right password is refused, with the same answer as a wrong one.
	expectError(t, ts.do(t, "POST", "/auth/login", nil, `{"email":"cat@example.com","password":"correct horse"}`),
		401, "invalid_credentials")
}

func TestRefreshRotatesAndDetectsReuse(t *testing.T) {
	ts := newTestServer(t)
	first := ts.register(t, "dan@example.com", "dan", "correct horse")

	rec := ts.do(t, "POST", "/auth/refresh", nil, `{"refresh_token":"`+first.RefreshToken+`"}`)
	if rec.Code != 200 {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	second := decodeAs[tokenResponse](t, rec)
	if second.RefreshToken == first.RefreshToken || second.User != nil {
		t.Fatal("refresh must return a new refresh token (and no user body)")
	}
	if ts.claimsOf(t, second.AccessToken).SessionID != ts.claimsOf(t, first.AccessToken).SessionID {
		t.Fatal("a refresh stays in the same session")
	}

	// The first token again: it was copied. Refused, and the session is gone for the holder of the new one too.
	expectError(t, ts.do(t, "POST", "/auth/refresh", nil, `{"refresh_token":"`+first.RefreshToken+`"}`), 401, "invalid_refresh_token")
	expectError(t, ts.do(t, "POST", "/auth/refresh", nil, `{"refresh_token":"`+second.RefreshToken+`"}`), 401, "invalid_refresh_token")
	expectError(t, ts.do(t, "POST", "/auth/refresh", nil, `{"refresh_token":"made-up"}`), 401, "invalid_refresh_token")
}

func TestRefreshPicksUpRoleChanges(t *testing.T) {
	ts := newTestServer(t)
	adm := ts.register(t, "root@example.com", "root", "correct horse")
	usr := ts.register(t, "eve@example.com", "eve", "correct horse")
	if _, err := ts.repo.SetRole(context.Background(), adm.User.UserID, model.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	admin := &identity{userID: adm.User.UserID.String(), role: "admin"}

	rec := ts.do(t, "PATCH", "/users/"+usr.User.UserID.String()+"/role", admin, `{"role":"admin"}`)
	if rec.Code != 200 || decodeAs[userView](t, rec).Role != model.RoleAdmin {
		t.Fatalf("set role: %d %s", rec.Code, rec.Body)
	}
	refreshed := decodeAs[tokenResponse](t, ts.do(t, "POST", "/auth/refresh", nil, `{"refresh_token":"`+usr.RefreshToken+`"}`))
	if ts.claimsOf(t, refreshed.AccessToken).Role != model.RoleAdmin {
		t.Fatal("the next access token must carry the new role")
	}
}

func TestLogout(t *testing.T) {
	ts := newTestServer(t)
	tr := ts.register(t, "fay@example.com", "fay", "correct horse")
	for i := 0; i < 2; i++ { // twice: logging out again is fine
		if rec := ts.do(t, "POST", "/auth/logout", nil, `{"refresh_token":"`+tr.RefreshToken+`"}`); rec.Code != 204 {
			t.Fatalf("logout: %d", rec.Code)
		}
	}
	expectError(t, ts.do(t, "POST", "/auth/refresh", nil, `{"refresh_token":"`+tr.RefreshToken+`"}`), 401, "invalid_refresh_token")
}

func TestProfileAndSessions(t *testing.T) {
	ts := newTestServer(t)
	tr := ts.register(t, "gus@example.com", "gus", "correct horse")
	me := ts.identityOf(t, tr)
	other := decodeAs[tokenResponse](t, ts.do(t, "POST", "/auth/login", nil, `{"email":"gus@example.com","password":"correct horse"}`))

	expectError(t, ts.do(t, "GET", "/users/me", nil, ""), 401, "unauthorized")
	if v := decodeAs[userView](t, ts.do(t, "GET", "/users/me", me, "")); v.Email != "gus@example.com" {
		t.Fatalf("me: %+v", v)
	}
	if v := decodeAs[userView](t, ts.do(t, "PATCH", "/users/me", me, `{"display_name":"  Gus G.  "}`)); v.DisplayName != "Gus G." {
		t.Fatalf("update: %+v", v)
	}
	expectError(t, ts.do(t, "PATCH", "/users/me", me, `{"display_name":""}`), 422, "validation_failed")

	type list struct{ Items []sessionView }
	sessions := decodeAs[list](t, ts.do(t, "GET", "/users/me/sessions", me, "")).Items
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	current := 0
	for _, s := range sessions {
		if s.Current {
			current++
			if s.SessionID.String() != me.sessionID {
				t.Fatal("the current session must be the request's own")
			}
		}
	}
	if current != 1 {
		t.Fatalf("exactly one session is current, got %d", current)
	}

	otherID := ts.claimsOf(t, other.AccessToken).SessionID.String()
	if rec := ts.do(t, "DELETE", "/users/me/sessions/"+otherID, me, ""); rec.Code != 204 {
		t.Fatalf("revoke: %d", rec.Code)
	}
	expectError(t, ts.do(t, "POST", "/auth/refresh", nil, `{"refresh_token":"`+other.RefreshToken+`"}`), 401, "invalid_refresh_token")
	stranger := &identity{userID: uuid.NewString(), role: "user"}
	expectError(t, ts.do(t, "DELETE", "/users/me/sessions/"+me.sessionID, stranger, ""), 404, "not_found")
}

func TestAdminEndpoints(t *testing.T) {
	ts := newTestServer(t)
	root := ts.register(t, "root@example.com", "root", "correct horse")
	hal := ts.register(t, "hal@example.com", "hal", "correct horse")
	admin := &identity{userID: root.User.UserID.String(), role: "admin"}
	user := ts.identityOf(t, hal)
	halPath := "/users/" + hal.User.UserID.String()

	expectError(t, ts.do(t, "GET", "/users", user, ""), 403, "forbidden")
	expectError(t, ts.do(t, "PATCH", halPath+"/status", user, `{"status":"disabled"}`), 403, "forbidden")
	expectError(t, ts.do(t, "PATCH", "/users/"+admin.userID+"/role", admin, `{"role":"user"}`), 409, "cannot_change_self")
	expectError(t, ts.do(t, "PATCH", halPath+"/role", admin, `{"role":"owner"}`), 422, "validation_failed")
	expectError(t, ts.do(t, "PATCH", "/users/"+uuid.NewString()+"/role", admin, `{"role":"admin"}`), 404, "not_found")

	type page struct {
		Items      []userView
		NextCursor *string `json:"next_cursor"`
	}
	p := decodeAs[page](t, ts.do(t, "GET", "/users?limit=1", admin, ""))
	if len(p.Items) != 1 || p.NextCursor == nil {
		t.Fatalf("first page: %+v", p)
	}
	p2 := decodeAs[page](t, ts.do(t, "GET", "/users?limit=1&cursor="+*p.NextCursor, admin, ""))
	if len(p2.Items) != 1 || p2.NextCursor != nil || p2.Items[0].UserID == p.Items[0].UserID {
		t.Fatalf("second page: %+v", p2)
	}

	// Disabling signs hal out everywhere, and login now says so (with the right password only).
	if rec := ts.do(t, "PATCH", halPath+"/status", admin, `{"status":"disabled"}`); rec.Code != 200 {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body)
	}
	expectError(t, ts.do(t, "POST", "/auth/refresh", nil, `{"refresh_token":"`+hal.RefreshToken+`"}`), 401, "invalid_refresh_token")
	expectError(t, ts.do(t, "POST", "/auth/login", nil, `{"email":"hal@example.com","password":"correct horse"}`), 403, "account_disabled")
	expectError(t, ts.do(t, "POST", "/auth/login", nil, `{"email":"hal@example.com","password":"wrong horse"}`), 401, "invalid_credentials")
}

func TestJWKSAndProbes(t *testing.T) {
	ts := newTestServer(t)
	rec := ts.do(t, "GET", "/.well-known/jwks.json", nil, "")
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "max-age=300") {
		t.Fatalf("jwks: %d %v", rec.Code, rec.Header())
	}
	if strings.Contains(rec.Body.String(), `"d"`) {
		t.Fatal("the JWKS must never contain the private key")
	}
	if rec := ts.do(t, "GET", "/healthz", nil, ""); rec.Code != 200 {
		t.Fatalf("healthz: %d", rec.Code)
	}
	expectError(t, ts.do(t, "GET", "/nope", nil, ""), 404, "not_found")
}
