package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Yeswanth03/distributed-judge-engine/services/user-service/internal/model"
)

var (
	// ErrInvalidRefreshToken covers every refresh that must fail without saying why: unknown token, expired or
	// revoked session, disabled account.
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	// ErrRefreshTokenReused means an already-used token came back: it was copied, so the session was revoked.
	ErrRefreshTokenReused = errors.New("refresh token reused")
)

const sessionColumns = `id, user_id, user_agent, ip, created_at, last_used_at, expires_at, revoked_at`

func scanSession(row pgx.Row) (*model.Session, error) {
	var s model.Session
	err := row.Scan(&s.ID, &s.UserID, &s.UserAgent, &s.IP, &s.CreatedAt, &s.LastUsedAt, &s.ExpiresAt, &s.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func insertSession(ctx context.Context, tx pgx.Tx, s *model.Session, refreshHash []byte) error {
	if _, err := tx.Exec(ctx, `INSERT INTO sessions (id, user_id, user_agent, ip, created_at, last_used_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $5, $6)`, s.ID, s.UserID, s.UserAgent, s.IP, s.CreatedAt, s.ExpiresAt); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO refresh_tokens (token_hash, session_id, created_at) VALUES ($1, $2, $3)`,
		refreshHash, s.ID, s.CreatedAt); err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// CreateSession signs an existing user in on a new device.
func (r *Repository) CreateSession(ctx context.Context, s *model.Session, refreshHash []byte) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := insertSession(ctx, tx, s, refreshHash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RotateRefreshToken spends the refresh token whose hash is oldHash and issues newHash in its place, returning
// the session and its user so the caller can sign a fresh access token with the user's current role.
//
// The token row is locked, so two refreshes racing with the same token can't both succeed. If the token was
// already used, someone holds a copy: the whole session is revoked and ErrRefreshTokenReused returned. The
// revocation is committed even though the refresh fails.
func (r *Repository) RotateRefreshToken(ctx context.Context, oldHash, newHash []byte, now time.Time) (*model.Session, *model.User, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var sessionID uuid.UUID
	var usedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT session_id, used_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`, oldHash).
		Scan(&sessionID, &usedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrInvalidRefreshToken
	}
	if err != nil {
		return nil, nil, err
	}
	s, err := scanSession(tx.QueryRow(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id = $1 FOR UPDATE`, sessionID))
	if err != nil {
		return nil, nil, err
	}
	if s.RevokedAt != nil || !now.Before(s.ExpiresAt) {
		return nil, nil, ErrInvalidRefreshToken
	}
	if usedAt != nil {
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = $2, revoke_reason = 'refresh token reused' WHERE id = $1`,
			s.ID, now); err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		return nil, nil, ErrRefreshTokenReused
	}

	u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, s.UserID))
	if err != nil {
		return nil, nil, err
	}
	if u.Status != model.StatusActive {
		return nil, nil, ErrInvalidRefreshToken
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET used_at = $2 WHERE token_hash = $1`, oldHash, now); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO refresh_tokens (token_hash, session_id, created_at) VALUES ($1, $2, $3)`,
		newHash, s.ID, now); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET last_used_at = $2 WHERE id = $1`, s.ID, now); err != nil {
		return nil, nil, err
	}
	s.LastUsedAt = now
	return s, u, tx.Commit(ctx)
}

// RevokeSessionByToken signs out the session the refresh token belongs to (logout). Unknown tokens report
// false: logging out twice is not an error.
func (r *Repository) RevokeSessionByToken(ctx context.Context, tokenHash []byte, now time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE sessions SET revoked_at = $2, revoke_reason = 'logout'
		WHERE id = (SELECT session_id FROM refresh_tokens WHERE token_hash = $1) AND revoked_at IS NULL`, tokenHash, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ListActiveSessions returns the user's sessions that are neither revoked nor expired, newest first.
func (r *Repository) ListActiveSessions(ctx context.Context, userID uuid.UUID, now time.Time) ([]model.Session, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+sessionColumns+` FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > $2 ORDER BY created_at DESC`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// RevokeSession signs out one of the user's own sessions. A session of someone else is ErrNotFound.
func (r *Repository) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID, now time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE sessions SET revoked_at = COALESCE(revoked_at, $3), revoke_reason = COALESCE(revoke_reason, 'signed out')
		WHERE id = $1 AND user_id = $2`, sessionID, userID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAllSessions signs the user out everywhere.
func (r *Repository) RevokeAllSessions(ctx context.Context, userID uuid.UUID, reason string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE sessions SET revoked_at = NOW(), revoke_reason = $2
		WHERE user_id = $1 AND revoked_at IS NULL`, userID, reason)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteDeadSessions removes sessions that expired or were revoked before before, with their tokens.
func (r *Repository) DeleteDeadSessions(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < $1 OR revoked_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
