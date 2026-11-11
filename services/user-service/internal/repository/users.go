// Package repository is the User Service's only access to PostgreSQL.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrEmailTaken    = errors.New("email already registered")
	ErrUsernameTaken = errors.New("username already taken")
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

const userColumns = `id, email, username, display_name, password_hash, role, status, email_verified, failed_logins,
	locked_until, version, created_at, updated_at`

func scanUser(row pgx.Row) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.PasswordHash, &u.Role, &u.Status,
		&u.EmailVerified, &u.FailedLogins, &u.LockedUntil, &u.Version, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// insertEvent writes u's current state to the outbox as an event of the given type, inside tx.
func insertEvent(ctx context.Context, tx pgx.Tx, eventType string, u *model.User) error {
	payload, err := json.Marshal(model.EventFor(eventType, u))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_events (aggregate_id, event_type, payload) VALUES ($1, $2, $3)`,
		u.ID, eventType, string(payload))
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

// uniqueViolation maps a duplicate email or username to its error.
func uniqueViolation(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		switch pg.ConstraintName {
		case "users_email_key":
			return ErrEmailTaken
		case "users_username_key":
			return ErrUsernameTaken
		}
	}
	return err
}

// CreateUserWithSession registers u, signs them in on a new session with the given refresh token, and records
// user.registered, all in one transaction. u.ID and u.CreatedAt are chosen by the caller.
func (r *Repository) CreateUserWithSession(ctx context.Context, u *model.User, s *model.Session, refreshHash []byte) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	created, err := scanUser(tx.QueryRow(ctx, `INSERT INTO users
		(id, email, username, display_name, password_hash, role, status, email_verified, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9) RETURNING `+userColumns,
		u.ID, u.Email, u.Username, u.DisplayName, u.PasswordHash, u.Role, u.Status, u.EmailVerified, u.CreatedAt))
	if err != nil {
		return uniqueViolation(err)
	}
	*u = *created
	if s != nil {
		if err := insertSession(ctx, tx, s, refreshHash); err != nil {
			return err
		}
	}
	if err := insertEvent(ctx, tx, model.EventUserRegistered, u); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) GetUserByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1)`, email))
}

// AnyAdmin reports whether at least one admin exists, for bootstrapping the first one.
func (r *Repository) AnyAdmin(ctx context.Context) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE role = 'admin')`).Scan(&exists)
	return exists, err
}

// RecordFailedLogin counts a wrong password and, after maxFailures in a row, locks the account until
// now+lockFor. Locking happens in the same statement as counting, so concurrent guesses can't slip past it.
func (r *Repository) RecordFailedLogin(ctx context.Context, id uuid.UUID, now time.Time, maxFailures int, lockFor time.Duration) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET
		failed_logins = CASE WHEN failed_logins + 1 >= $2 THEN 0 ELSE failed_logins + 1 END,
		locked_until  = CASE WHEN failed_logins + 1 >= $2 THEN $3::timestamptz ELSE locked_until END
		WHERE id = $1`, id, maxFailures, now.Add(lockFor))
	return err
}

// ResetFailedLogins clears the failure count after a successful login.
func (r *Repository) ResetFailedLogins(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET failed_logins = 0, locked_until = NULL WHERE id = $1 AND (failed_logins <> 0 OR locked_until IS NOT NULL)`, id)
	return err
}

// UpdateProfile changes the display name and records user.updated.
func (r *Repository) UpdateProfile(ctx context.Context, id uuid.UUID, displayName string) (*model.User, error) {
	return r.updateUser(ctx, id, model.EventUserUpdated,
		`UPDATE users SET display_name = $2, version = version + 1, updated_at = NOW()
		 WHERE id = $1 AND display_name <> $2 RETURNING `+userColumns, displayName)
}

// SetRole changes a user's role and records user.role_changed. Setting the role a user already has changes
// nothing and records no event.
func (r *Repository) SetRole(ctx context.Context, id uuid.UUID, role model.Role) (*model.User, error) {
	return r.updateUser(ctx, id, model.EventUserRoleChanged,
		`UPDATE users SET role = $2, version = version + 1, updated_at = NOW()
		 WHERE id = $1 AND role <> $2 RETURNING `+userColumns, role)
}

// SetStatus enables or disables a user and records user.status_changed. Disabling also revokes every
// session, so the user can't refresh: they are out once their current access token expires.
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, status model.Status) (*model.User, error) {
	u, err := r.updateUser(ctx, id, model.EventUserStatusChanged,
		`UPDATE users SET status = $2, version = version + 1, updated_at = NOW()
		 WHERE id = $1 AND status <> $2 RETURNING `+userColumns, status)
	if err != nil || status != model.StatusDisabled {
		return u, err
	}
	_, err = r.RevokeAllSessions(ctx, id, "account disabled")
	return u, err
}

// updateUser runs a guarded UPDATE ... RETURNING and records the event in the same transaction. When the
// guard matches nothing because the value is already set, it returns the user unchanged without an event.
func (r *Repository) updateUser(ctx context.Context, id uuid.UUID, eventType, query string, value any) (*model.User, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	u, err := scanUser(tx.QueryRow(ctx, query, id, value))
	if errors.Is(err, ErrNotFound) {
		return r.GetUserByID(ctx, id) // unchanged, or ErrNotFound if there is no such user
	}
	if err != nil {
		return nil, err
	}
	if err := insertEvent(ctx, tx, eventType, u); err != nil {
		return nil, err
	}
	return u, tx.Commit(ctx)
}

// UserCursor is a position in the newest-first user listing: the last row of the previous page.
type UserCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// ListUsers returns up to limit users newest first, after the cursor if one is given.
func (r *Repository) ListUsers(ctx context.Context, limit int, after *UserCursor) ([]model.User, error) {
	query := `SELECT ` + userColumns + ` FROM users`
	args := []any{limit}
	if after != nil {
		query += ` WHERE (created_at, id) < ($2, $3)`
		args = append(args, after.CreatedAt, after.ID)
	}
	rows, err := r.pool.Query(ctx, query+` ORDER BY created_at DESC, id DESC LIMIT $1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// SigningKey returns the stored signing key, creating it with create if there is none yet. Replicas starting
// together agree on one key: the first insert wins and the others read it back.
func (r *Repository) SigningKey(ctx context.Context, create func() (kid, pemText string, err error)) (string, error) {
	var pemText string
	err := r.pool.QueryRow(ctx, `SELECT private_pem FROM signing_keys ORDER BY created_at LIMIT 1`).Scan(&pemText)
	if err == nil {
		return pemText, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	kid, newPEM, err := create()
	if err != nil {
		return "", err
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO signing_keys (kid, private_pem) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		kid, newPEM); err != nil {
		return "", err
	}
	err = r.pool.QueryRow(ctx, `SELECT private_pem FROM signing_keys ORDER BY created_at LIMIT 1`).Scan(&pemText)
	return pemText, err
}
