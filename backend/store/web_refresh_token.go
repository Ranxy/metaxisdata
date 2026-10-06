package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/pkg/errors"
)

// The web-session refresh queries are constants so a guard test can pin the
// single-use consume. The hash is the only lookup key: the session carries no
// client identity, because the SPA is the deployment's own front end.
const (
	createWebRefreshTokenQuery = `
		INSERT INTO web_refresh_token (token_hash, user_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4)`

	// consumeWebRefreshTokenQuery is the rotation gate. DELETE ... RETURNING
	// makes "claim the row and read it" one atomic statement, so two concurrent
	// refreshes of one session cannot both rotate it.
	consumeWebRefreshTokenQuery = `
		DELETE FROM web_refresh_token
		WHERE token_hash = $1
		RETURNING token_hash, user_id, expires_at, created_at`

	deleteWebRefreshTokenQuery = `DELETE FROM web_refresh_token WHERE token_hash = $1`

	deleteExpiredWebRefreshTokensQuery = `DELETE FROM web_refresh_token WHERE expires_at < $1`
)

// WebRefreshToken is one SPA session's rotation state.
type WebRefreshToken struct {
	// TokenHash is the SHA-256 hex digest of the opaque token. The plaintext is
	// never stored.
	TokenHash string
	UserID    int
	ExpiresAt time.Time
	// IssuedAt is when the session was established. A rotated token carries the
	// original value forward rather than stamping the rotation, so the session
	// stays ordered against the user's last password change however many times
	// it is refreshed. Required on create.
	IssuedAt time.Time
}

// CreateWebRefreshToken stores a newly issued session.
func (s *Store) CreateWebRefreshToken(ctx context.Context, create *WebRefreshToken) error {
	if _, err := s.GetDB().ExecContext(ctx, createWebRefreshTokenQuery,
		create.TokenHash, create.UserID, create.ExpiresAt.UTC(), create.IssuedAt.UTC(),
	); err != nil {
		return errors.Wrap(err, "failed to create web refresh token")
	}
	return nil
}

// ConsumeWebRefreshToken atomically deletes a session and returns it, or
// (nil, nil) when the token is unknown or already consumed. A non-nil error is a
// real failure and must abort issuance.
func (s *Store) ConsumeWebRefreshToken(ctx context.Context, tokenHash string) (*WebRefreshToken, error) {
	msg := &WebRefreshToken{}
	if err := s.GetDB().QueryRowContext(ctx, consumeWebRefreshTokenQuery, tokenHash).Scan(
		&msg.TokenHash, &msg.UserID, &msg.ExpiresAt, &msg.IssuedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to consume web refresh token")
	}
	return msg, nil
}

// DeleteWebRefreshToken drops one session. Logout calls it, so the cookie it is
// about to clear cannot be replayed.
func (s *Store) DeleteWebRefreshToken(ctx context.Context, tokenHash string) error {
	if _, err := s.GetDB().ExecContext(ctx, deleteWebRefreshTokenQuery, tokenHash); err != nil {
		return errors.Wrap(err, "failed to delete web refresh token")
	}
	return nil
}

// DeleteExpiredWebRefreshTokens drops sessions past their expiry. The
// maintenance runner calls it and reports how many rows were dropped.
func (s *Store) DeleteExpiredWebRefreshTokens(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, deleteExpiredWebRefreshTokensQuery, before.UTC())
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired web refresh tokens")
	}
	return result.RowsAffected()
}
