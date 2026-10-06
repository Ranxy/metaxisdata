package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/pkg/errors"
)

// The refresh-token queries are constants so a guard test can pin their shape.
// The token hash is caller-supplied (it is the digest of a string from outside),
// so it must stay a parameter, and every lookup is scoped to the client the
// grant was issued to, so a hash leaked from one registration cannot be
// redeemed by another.
const (
	createOAuthRefreshTokenQuery = `
		INSERT INTO oauth_refresh_token (token_hash, client_id, user_id, resource, scope, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	getOAuthRefreshTokenQuery = `
		SELECT token_hash, client_id, user_id, resource, scope, expires_at, created_at
		FROM oauth_refresh_token
		WHERE token_hash = $1 AND client_id = $2`

	// DELETE ... RETURNING is the single-use rotation gate. Concurrent refreshes
	// of the same token race on this statement and only the caller that deletes
	// the row gets one back, so exactly one of them may issue a token pair.
	// RETURNING rather than RowsAffected is deliberate: a count cannot tell
	// "deleted" from "no such row" reliably here.
	consumeOAuthRefreshTokenQuery = `
		DELETE FROM oauth_refresh_token
		WHERE token_hash = $1 AND client_id = $2
		RETURNING token_hash`

	deleteOAuthRefreshTokensByUserAndClientQuery = `
		DELETE FROM oauth_refresh_token
		WHERE user_id = $1 AND client_id = $2`

	deleteExpiredOAuthRefreshTokensQuery = `DELETE FROM oauth_refresh_token WHERE expires_at < $1`
)

// OAuthRefreshToken is one MCP OAuth 2.1 grant's rotation state.
type OAuthRefreshToken struct {
	// TokenHash is the SHA-256 hex digest of the opaque token. The plaintext is
	// never stored, so a database read does not hand out a usable credential.
	TokenHash string
	ClientID  string
	UserID    int
	// Resource is the MCP resource identifier the user consented to. A refresh
	// is refused when it no longer matches the current endpoint.
	Resource string
	// Scope is the consented scope, carried forward verbatim: a refresh
	// re-issues the grant as consented and never widens it.
	Scope     string
	ExpiresAt time.Time
	// IssuedAt is when the grant was first issued. A rotation carries the
	// original value forward rather than stamping the rotation, so the grant
	// stays ordered against the user's last password change however many times
	// it is refreshed. Required on create.
	IssuedAt time.Time
}

// CreateOAuthRefreshToken stores a newly issued grant.
func (s *Store) CreateOAuthRefreshToken(ctx context.Context, create *OAuthRefreshToken) error {
	if _, err := s.GetDB().ExecContext(ctx, createOAuthRefreshTokenQuery,
		create.TokenHash, create.ClientID, create.UserID, create.Resource, create.Scope, create.ExpiresAt.UTC(), create.IssuedAt.UTC(),
	); err != nil {
		return errors.Wrap(err, "failed to create OAuth refresh token")
	}
	return nil
}

// GetOAuthRefreshToken returns the grant for (clientID, tokenHash), or (nil, nil)
// when the token is unknown, already consumed or was issued to another client.
func (s *Store) GetOAuthRefreshToken(ctx context.Context, clientID, tokenHash string) (*OAuthRefreshToken, error) {
	msg := &OAuthRefreshToken{}
	if err := s.GetDB().QueryRowContext(ctx, getOAuthRefreshTokenQuery, tokenHash, clientID).Scan(
		&msg.TokenHash, &msg.ClientID, &msg.UserID, &msg.Resource, &msg.Scope, &msg.ExpiresAt, &msg.IssuedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get OAuth refresh token")
	}
	return msg, nil
}

// ConsumeOAuthRefreshToken atomically deletes a grant and reports whether this
// call is the one that claimed it. false means the token was already consumed,
// never existed, or belongs to another client; a non-nil error is a real failure
// and must abort issuance rather than being treated as a refusal.
func (s *Store) ConsumeOAuthRefreshToken(ctx context.Context, clientID, tokenHash string) (bool, error) {
	var consumedHash string
	if err := s.GetDB().QueryRowContext(ctx, consumeOAuthRefreshTokenQuery, tokenHash, clientID).Scan(&consumedHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, errors.Wrap(err, "failed to consume OAuth refresh token")
	}
	return true, nil
}

// DeleteOAuthRefreshTokensByUserAndClient drops every grant a principal holds
// for one client. Logout calls it, so revoking an MCP token also retires the
// grant that would otherwise mint a replacement.
func (s *Store) DeleteOAuthRefreshTokensByUserAndClient(ctx context.Context, userID int, clientID string) error {
	if _, err := s.GetDB().ExecContext(ctx, deleteOAuthRefreshTokensByUserAndClientQuery, userID, clientID); err != nil {
		return errors.Wrap(err, "failed to delete OAuth refresh tokens")
	}
	return nil
}

// DeleteExpiredOAuthRefreshTokens drops grants past their expiry: past that
// point the row can issue nothing. The maintenance runner calls it and reports
// how many rows were dropped.
func (s *Store) DeleteExpiredOAuthRefreshTokens(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, deleteExpiredOAuthRefreshTokensQuery, before.UTC())
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired OAuth refresh tokens")
	}
	return result.RowsAffected()
}
