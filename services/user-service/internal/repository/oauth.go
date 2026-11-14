package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
)

// OAuthState is a sign-in in progress.
type OAuthState struct {
	Provider     string
	CodeVerifier string
	RedirectURI  string
}

func (r *Repository) SaveOAuthState(ctx context.Context, stateHash []byte, s OAuthState, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO oauth_states (state_hash, provider, code_verifier, redirect_uri, expires_at)
		VALUES ($1, $2, $3, $4, $5)`, stateHash, s.Provider, s.CodeVerifier, s.RedirectURI, expiresAt)
	return err
}

// TakeOAuthState returns and deletes the sign-in for this state, so a callback can't be replayed. An unknown or
// expired state is ErrNotFound.
func (r *Repository) TakeOAuthState(ctx context.Context, stateHash []byte, now time.Time) (*OAuthState, error) {
	var s OAuthState
	err := r.pool.QueryRow(ctx, `DELETE FROM oauth_states WHERE state_hash = $1 AND expires_at > $2
		RETURNING provider, code_verifier, redirect_uri`, stateHash, now).Scan(&s.Provider, &s.CodeVerifier, &s.RedirectURI)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

// UserByIdentity returns the user a provider account is linked to.
func (r *Repository) UserByIdentity(ctx context.Context, provider, subject string) (*model.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+prefixed("u.", userColumns)+` FROM users u
		JOIN user_identities i ON i.user_id = u.id WHERE i.provider = $1 AND i.subject = $2`, provider, subject))
}

// LinkIdentity adds a provider account to an existing user.
func (r *Repository) LinkIdentity(ctx context.Context, userID uuid.UUID, provider, subject, email string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO user_identities (provider, subject, user_id, email) VALUES ($1, $2, $3, $4)
		ON CONFLICT (provider, subject) DO NOTHING`, provider, subject, userID, email)
	return err
}

// CreateOAuthUser registers u (who has no password) linked to the provider account, and records
// user.registered, in one transaction.
func (r *Repository) CreateOAuthUser(ctx context.Context, u *model.User, provider, subject string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	created, err := scanUser(tx.QueryRow(ctx, `INSERT INTO users
		(id, email, username, display_name, password_hash, role, status, email_verified, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULL, $5, $6, $7, $8, $8) RETURNING `+userColumns,
		u.ID, u.Email, u.Username, u.DisplayName, u.Role, u.Status, u.EmailVerified, u.CreatedAt))
	if err != nil {
		return uniqueViolation(err)
	}
	*u = *created
	if _, err := tx.Exec(ctx, `INSERT INTO user_identities (provider, subject, user_id, email) VALUES ($1, $2, $3, $4)`,
		provider, subject, u.ID, u.Email); err != nil {
		return fmt.Errorf("insert identity: %w", err)
	}
	if err := insertEvent(ctx, tx, model.EventUserRegistered, u); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) SaveLoginCode(ctx context.Context, codeHash []byte, userID uuid.UUID, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO login_codes (code_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		codeHash, userID, expiresAt)
	return err
}

// TakeLoginCode returns and deletes the user a login code was issued for. Unknown, used or expired: ErrNotFound.
func (r *Repository) TakeLoginCode(ctx context.Context, codeHash []byte, now time.Time) (uuid.UUID, error) {
	var userID uuid.UUID
	err := r.pool.QueryRow(ctx, `DELETE FROM login_codes WHERE code_hash = $1 AND expires_at > $2 RETURNING user_id`,
		codeHash, now).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return userID, err
}

// DeleteExpiredSignIns removes abandoned sign-ins and unused login codes.
func (r *Repository) DeleteExpiredSignIns(ctx context.Context, now time.Time) (int64, error) {
	a, err := r.pool.Exec(ctx, `DELETE FROM oauth_states WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	b, err := r.pool.Exec(ctx, `DELETE FROM login_codes WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	return a.RowsAffected() + b.RowsAffected(), nil
}

// prefixed qualifies each column of a comma-separated list with a table alias.
func prefixed(alias, columns string) string {
	parts := strings.Split(columns, ",")
	for i, c := range parts {
		parts[i] = alias + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}
