package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
)

// Tests run against a real PostgreSQL: TEST_DATABASE_URL, or the docker-compose one by default. Each test gets
// its own throwaway database. Without TEST_DATABASE_URL the tests are skipped if the server is unreachable;
// with it (as in CI) they fail, so a missing database can never look like a green run.
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
		t.Skipf("PostgreSQL not reachable (%v); set TEST_DATABASE_URL", err)
	}
	dbName := "users_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
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

func newUser(email, username string) *model.User {
	hash := "$argon2id$v=19$m=64,t=1,p=1$c2FsdA$aGFzaA"
	return &model.User{ID: uuid.New(), Email: email, Username: username, DisplayName: username, PasswordHash: &hash,
		Role: model.RoleUser, Status: model.StatusActive, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
}

func newSession(userID uuid.UUID, now time.Time) *model.Session {
	return &model.Session{ID: uuid.New(), UserID: userID, UserAgent: "test", IP: "10.0.0.1", CreatedAt: now,
		LastUsedAt: now, ExpiresAt: now.Add(time.Hour)}
}

// events returns the outbox payloads for a user, oldest first.
func events(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) []model.UserEvent {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT payload FROM outbox_events WHERE aggregate_id = $1 ORDER BY created_at, (payload->>'version')::int`, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []model.UserEvent
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, "@") {
			t.Fatalf("an event must never carry the email address: %s", raw)
		}
		var ev model.UserEvent
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func TestCreateUserWithSession(t *testing.T) {
	repo, pool := newTestRepo(t)
	ctx := context.Background()
	u := newUser("Ada@Example.com", "ada")
	s := newSession(u.ID, u.CreatedAt)
	if err := repo.CreateUserWithSession(ctx, u, s, []byte("hash-1")); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetUserByEmail(ctx, "ada@example.COM") // case doesn't matter
	if err != nil || got.ID != u.ID || got.Version != 1 {
		t.Fatalf("lookup by email: %+v %v", got, err)
	}
	evs := events(t, pool, u.ID)
	if len(evs) != 1 || evs[0].Type != model.EventUserRegistered || evs[0].Username != "ada" || evs[0].Version != 1 {
		t.Fatalf("expected one user.registered event, got %+v", evs)
	}

	if err := repo.CreateUserWithSession(ctx, newUser("ADA@example.com", "other"), nil, nil); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate email (any case): %v", err)
	}
	if err := repo.CreateUserWithSession(ctx, newUser("b@example.com", "ADA"), nil, nil); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate username (any case): %v", err)
	}
}

func TestUpdatesBumpVersionAndRecordEvents(t *testing.T) {
	repo, pool := newTestRepo(t)
	ctx := context.Background()
	u := newUser("c@example.com", "carol")
	if err := repo.CreateUserWithSession(ctx, u, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateProfile(ctx, u.ID, "Carol C."); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetRole(ctx, u.ID, model.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	// Setting what is already set changes nothing and publishes nothing.
	same, err := repo.SetRole(ctx, u.ID, model.RoleAdmin)
	if err != nil || same.Version != 3 {
		t.Fatalf("a no-op must not bump the version: %+v %v", same, err)
	}
	evs := events(t, pool, u.ID)
	types := []string{evs[0].Type, evs[1].Type, evs[2].Type}
	if len(evs) != 3 || types[1] != model.EventUserUpdated || types[2] != model.EventUserRoleChanged ||
		evs[1].DisplayName != "Carol C." || evs[2].Role != model.RoleAdmin || evs[2].Version != 3 {
		t.Fatalf("unexpected events %+v", evs)
	}
	if _, err := repo.SetRole(ctx, uuid.New(), model.RoleAdmin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestFailedLoginsLockTheAccount(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	u := newUser("d@example.com", "dan")
	if err := repo.CreateUserWithSession(ctx, u, nil, nil); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 3; i++ {
		if err := repo.RecordFailedLogin(ctx, u.ID, now, 3, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := repo.GetUserByID(ctx, u.ID)
	if got.LockedUntil == nil || got.LockedUntil.Sub(now) < 14*time.Minute {
		t.Fatalf("expected a lock after 3 failures, got %+v", got.LockedUntil)
	}
	if err := repo.ResetFailedLogins(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.GetUserByID(ctx, u.ID)
	if got.LockedUntil != nil || got.FailedLogins != 0 {
		t.Fatalf("reset should clear the lock, got %+v", got)
	}
}

func TestRefreshTokenRotationAndReuse(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	u := newUser("e@example.com", "eve")
	now := time.Now().UTC().Truncate(time.Microsecond)
	s := newSession(u.ID, now)
	if err := repo.CreateUserWithSession(ctx, u, s, []byte("t1")); err != nil {
		t.Fatal(err)
	}

	gotS, gotU, err := repo.RotateRefreshToken(ctx, []byte("t1"), []byte("t2"), now.Add(time.Second))
	if err != nil || gotS.ID != s.ID || gotU.ID != u.ID {
		t.Fatalf("first refresh: %v", err)
	}
	if _, _, err := repo.RotateRefreshToken(ctx, []byte("t2"), []byte("t3"), now.Add(2*time.Second)); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	// t1 again: someone kept a copy. The whole session goes, including the newest token t3.
	if _, _, err := repo.RotateRefreshToken(ctx, []byte("t1"), []byte("x"), now.Add(3*time.Second)); !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("reuse must be detected, got %v", err)
	}
	if _, _, err := repo.RotateRefreshToken(ctx, []byte("t3"), []byte("t4"), now.Add(4*time.Second)); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("after reuse the session must be dead, got %v", err)
	}
	if _, _, err := repo.RotateRefreshToken(ctx, []byte("unknown"), []byte("t5"), now); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("unknown token: %v", err)
	}
	active, _ := repo.ListActiveSessions(ctx, u.ID, now)
	if len(active) != 0 {
		t.Fatalf("the revoked session must not be listed, got %d", len(active))
	}
}

func TestRefreshFailsForExpiredSessionsAndDisabledUsers(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	u := newUser("f@example.com", "fay")
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.CreateUserWithSession(ctx, u, newSession(u.ID, now), []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.RotateRefreshToken(ctx, []byte("a"), []byte("b"), now.Add(2*time.Hour)); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("an expired session must not refresh, got %v", err)
	}

	if err := repo.CreateSession(ctx, newSession(u.ID, now), []byte("c")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetStatus(ctx, u.ID, model.StatusDisabled); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.RotateRefreshToken(ctx, []byte("c"), []byte("d"), now.Add(time.Second)); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("a disabled user must not refresh, got %v", err)
	}
}

func TestLogoutAndSessionManagement(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	u := newUser("g@example.com", "gus")
	other := newUser("h@example.com", "hal")
	now := time.Now().UTC().Truncate(time.Microsecond)
	s1 := newSession(u.ID, now)
	if err := repo.CreateUserWithSession(ctx, u, s1, []byte("s1")); err != nil {
		t.Fatal(err)
	}
	s2 := newSession(u.ID, now.Add(time.Second))
	if err := repo.CreateSession(ctx, s2, []byte("s2")); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateUserWithSession(ctx, other, nil, nil); err != nil {
		t.Fatal(err)
	}

	if ok, err := repo.RevokeSessionByToken(ctx, []byte("s1"), now); err != nil || !ok {
		t.Fatalf("logout: %v %v", ok, err)
	}
	if ok, _ := repo.RevokeSessionByToken(ctx, []byte("s1"), now); ok {
		t.Fatal("logging out twice is a no-op")
	}
	if err := repo.RevokeSession(ctx, other.ID, s2.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else's session must look unknown, got %v", err)
	}
	active, _ := repo.ListActiveSessions(ctx, u.ID, now)
	if len(active) != 1 || active[0].ID != s2.ID {
		t.Fatalf("expected only s2 active, got %+v", active)
	}
	if err := repo.RevokeSession(ctx, u.ID, s2.ID, now); err != nil {
		t.Fatal(err)
	}
	n, err := repo.DeleteDeadSessions(ctx, now.Add(time.Minute))
	if err != nil || n != 2 {
		t.Fatalf("expected both revoked sessions deleted, got %d %v", n, err)
	}
}

func TestListUsersPages(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Microsecond)
	for i, name := range []string{"u1", "u2", "u3"} {
		u := newUser(name+"@example.com", name)
		u.CreatedAt = base.Add(time.Duration(i) * time.Second)
		if err := repo.CreateUserWithSession(ctx, u, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := repo.ListUsers(ctx, 2, nil)
	if len(first) != 2 || first[0].Username != "u3" || first[1].Username != "u2" {
		t.Fatalf("first page newest first, got %v", names(first))
	}
	rest, _ := repo.ListUsers(ctx, 2, &UserCursor{CreatedAt: first[1].CreatedAt, ID: first[1].ID})
	if len(rest) != 1 || rest[0].Username != "u1" {
		t.Fatalf("second page, got %v", names(rest))
	}
}

func names(us []model.User) []string {
	var out []string
	for _, u := range us {
		out = append(out, u.Username)
	}
	return out
}

func TestSigningKeyIsCreatedOnce(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	calls := 0
	create := func() (string, string, error) { calls++; return "kid-1", "PEM-1", nil }
	a, err := repo.SigningKey(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := repo.SigningKey(ctx, func() (string, string, error) { return "kid-2", "PEM-2", nil })
	if a != "PEM-1" || b != "PEM-1" || calls != 1 {
		t.Fatalf("every caller must get the first key, got %q %q", a, b)
	}
}

func TestAnyAdmin(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	if ok, _ := repo.AnyAdmin(ctx); ok {
		t.Fatal("no admin yet")
	}
	u := newUser("i@example.com", "ivy")
	u.Role = model.RoleAdmin
	if err := repo.CreateUserWithSession(ctx, u, nil, nil); err != nil {
		t.Fatal(err)
	}
	if ok, _ := repo.AnyAdmin(ctx); !ok {
		t.Fatal("expected an admin")
	}
}
