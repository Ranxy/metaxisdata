package store

import (
	"context"
	"time"
)

// The revocation queries are constants so a guard test can pin their shape: the
// jti is caller-controlled (it comes from a token this server signed, but still
// from outside), so it must stay a parameter.
const (
	revokeTokenQuery = `
		INSERT INTO revoked_token (jti, expires_at)
		VALUES ($1, $2)
		ON CONFLICT (jti) DO NOTHING`

	isTokenRevokedQuery = `SELECT EXISTS (SELECT 1 FROM revoked_token WHERE jti = $1)`

	deleteExpiredRevokedTokensQuery = `DELETE FROM revoked_token WHERE expires_at < $1`
)

// RevokeToken records that a token is no longer accepted, keeping the record until
// the token itself would have expired. Revoking the same token twice is a no-op,
// so a repeated logout is harmless.
func (s *Store) RevokeToken(ctx context.Context, tokenID string, expiresAt time.Time) error {
	if tokenID == "" {
		return nil
	}
	_, err := s.GetDB().ExecContext(ctx, revokeTokenQuery, tokenID, expiresAt.UTC())
	return err
}

// IsTokenRevoked reports whether the token was revoked. The persistent table is
// the authority; callers cache the answer only to save this lookup.
func (s *Store) IsTokenRevoked(ctx context.Context, tokenID string) (bool, error) {
	if tokenID == "" {
		return false, nil
	}
	var revoked bool
	if err := s.GetDB().QueryRowContext(ctx, isTokenRevokedQuery, tokenID).Scan(&revoked); err != nil {
		return false, err
	}
	return revoked, nil
}

// DeleteExpiredRevokedTokens drops records whose token has expired: past that
// point the token is refused by its own exp claim, so the row can refuse nothing.
// The maintenance runner calls this; it returns how many rows were dropped.
func (s *Store) DeleteExpiredRevokedTokens(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, deleteExpiredRevokedTokensQuery, before.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
