package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// A grant row is the durable record of one connected app: one user's
// authorization of one MCP client. The refresh credential rotates in place, so
// the row id is a stable handle an access token can carry and an administrator
// can revoke; the table name is historical (the row used to hold only the
// refresh credential).
//
// The queries are constants so a guard test can pin their shape. The token hash
// is caller-supplied (it is the digest of a string from outside), so it must stay
// a parameter, and every lookup is scoped to the client the grant was issued to,
// so a hash leaked from one registration cannot be redeemed by another.
const (
	createOAuthGrantQuery = `
		INSERT INTO oauth_refresh_token (token_hash, client_id, user_id, resource, scope, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`

	// oauthGrantColumns is the projection every token-digest lookup returns, so
	// the lookups cannot drift in what they scan.
	oauthGrantColumns = `id, token_hash, client_id, user_id, resource, scope, expires_at, created_at, last_tool_call_at, revoked_at, revoked_by`

	getOAuthGrantQuery = `
		SELECT ` + oauthGrantColumns + `
		FROM oauth_refresh_token
		WHERE token_hash = $1 AND client_id = $2`

	// getOAuthGrantByTokenHashQuery looks a grant up by token digest alone, for
	// RFC 7009 revocation: the client presents only the token, so this lookup
	// cannot also be scoped by the client the way rotation is. An unknown token
	// and another client's token are indistinguishable answers, and the caller
	// reports both as an empty success.
	getOAuthGrantByTokenHashQuery = `
		SELECT ` + oauthGrantColumns + `
		FROM oauth_refresh_token
		WHERE token_hash = $1`

	// rotateOAuthGrantQuery is the single-use rotation gate. It updates the row in
	// place rather than deleting and reinserting it, so the id survives a refresh.
	// Concurrent refreshes of the same token race on this statement: the winner
	// changes token_hash, and the loser's re-evaluated predicate matches no row, so
	// exactly one caller may issue a token pair. revoked_at is part of the
	// predicate, so a revocation that commits first wins over a rotation.
	rotateOAuthGrantQuery = `
		UPDATE oauth_refresh_token
		SET token_hash = $3, expires_at = $4
		WHERE token_hash = $1 AND client_id = $2
			AND revoked_at IS NULL AND expires_at > $5
		RETURNING id`

	revokeOAuthGrantQuery = `
		UPDATE oauth_refresh_token
		SET revoked_at = $2, revoked_by = $3
		WHERE id = $1 AND revoked_at IS NULL
		RETURNING id`

	// revokeOAuthGrantsByUserAndClientQuery retires every connection a principal
	// holds for one client. Logout calls it, so revoking one MCP token also ends
	// the refresh chain that could mint a replacement.
	revokeOAuthGrantsByUserAndClientQuery = `
		UPDATE oauth_refresh_token
		SET revoked_at = $3, revoked_by = $4
		WHERE user_id = $1 AND client_id = $2 AND revoked_at IS NULL
		RETURNING id`

	// isOAuthGrantLiveQuery answers whether an access token's grant may still be
	// used. The token's own user and client are part of the predicate, so a grant
	// id learned from one connection cannot be matched by a token for another.
	// A missing row is not live: the refresh credential outlives an access token,
	// so a pruned row can only mean the access token has expired too.
	isOAuthGrantLiveQuery = `
		SELECT EXISTS (
			SELECT 1 FROM oauth_refresh_token
			WHERE id = $1 AND user_id = $2 AND client_id = $3
				AND revoked_at IS NULL AND expires_at > NOW()
		)`

	// touchOAuthGrantQuery records the most recent tool call, at most once per
	// window. The predicate makes a throttled call a cheap no-op rather than a row
	// rewrite, and a revoked grant is not touched at all.
	touchOAuthGrantQuery = `
		UPDATE oauth_refresh_token
		SET last_tool_call_at = $2
		WHERE id = $1 AND revoked_at IS NULL
			AND (last_tool_call_at IS NULL OR last_tool_call_at < $3)`

	deleteExpiredOAuthGrantsQuery = `DELETE FROM oauth_refresh_token WHERE expires_at < $1`

	// connectedAppColumns is the joined projection the management page reads. The
	// aliases (g, p, c) are what the filter parser renders against (see
	// backend/api/v1/filter.go).
	connectedAppColumns = `g.id, g.token_hash, g.client_id, g.user_id, g.resource, g.scope, g.expires_at, g.created_at, g.last_tool_call_at, g.revoked_at, g.revoked_by, p.email, p.name, p.deleted, p.profile, COALESCE(c.client_name, '')`

	connectedAppFrom = `
		FROM oauth_refresh_token g
		LEFT JOIN oauth_client c ON c.client_id = g.client_id
		JOIN principal p ON p.id = g.user_id`
)

// OAuthGrant is one connected app: a user's authorization of one MCP client and
// the rotation state of the refresh credential behind it.
type OAuthGrant struct {
	ID int64
	// TokenHash is the SHA-256 hex digest of the opaque refresh token. The
	// plaintext is never stored, so a database read does not hand out a usable
	// credential.
	TokenHash string
	ClientID  string
	UserID    int
	// Resource is the MCP resource identifier the user consented to. A refresh is
	// refused when it no longer matches the current endpoint.
	Resource string
	// Scope is the consented scope, carried forward verbatim: a refresh re-issues
	// the grant as consented and never widens it.
	Scope     string
	ExpiresAt time.Time
	// CreatedAt is when the first access token was issued (the authorization
	// completed). A rotation carries it forward rather than stamping the rotation,
	// so it stays ordered against the user's last password change.
	CreatedAt time.Time
	// LastToolCallAt is the most recent tool call made with this connection, or nil
	// when the client has been authorized but has not called one.
	LastToolCallAt *time.Time
	// RevokedAt is a soft revocation. No path may clear it: authorizing again
	// creates a new row.
	RevokedAt *time.Time
	// RevokedBy is who revoked it. Self-service revocation and Logout record the
	// acting user; nil means the system ended the connection, which happens when a
	// refresh is refused because the principal no longer exists, is deactivated,
	// or changed their password since the grant was issued.
	RevokedBy *int

	// The fields below come from the joined registration and principal and are
	// only populated by GetConnectedApp and ListConnectedApps.
	ClientName                 string
	UserEmail                  string
	UserName                   string
	UserDeleted                bool
	UserLastChangePasswordTime *time.Time
}

// FindOAuthGrantMessage is the filter for listing connected apps.
type FindOAuthGrantMessage struct {
	// UserID scopes the list to one user's connections.
	UserID *int
	// Filter is a translated CEL predicate over the joined query.
	Filter *ListResourceFilter
	Limit  *int
	Offset *int
}

// CreateOAuthGrant stores a newly issued grant and returns its id, which the
// access token carries and the management API addresses.
func (s *Store) CreateOAuthGrant(ctx context.Context, create *OAuthGrant) (int64, error) {
	var id int64
	if err := s.GetDB().QueryRowContext(ctx, createOAuthGrantQuery,
		create.TokenHash, create.ClientID, create.UserID, create.Resource, create.Scope, create.ExpiresAt.UTC(), create.CreatedAt.UTC(),
	).Scan(&id); err != nil {
		return 0, errors.Wrap(err, "failed to create OAuth grant")
	}
	return id, nil
}

// GetOAuthGrant returns the grant for (clientID, tokenHash), or (nil, nil) when
// the token is unknown or was issued to another client. A revoked grant is
// returned as-is; the caller decides, because a revocation and an unknown token
// are the same refusal to the client.
func (s *Store) GetOAuthGrant(ctx context.Context, clientID, tokenHash string) (*OAuthGrant, error) {
	return queryOAuthGrant(s.GetDB().QueryRowContext(ctx, getOAuthGrantQuery, tokenHash, clientID), "failed to get OAuth grant")
}

// GetOAuthGrantByTokenHash returns the grant whose refresh credential has this
// digest, or (nil, nil) when no row carries it. The lookup is not scoped to a
// client because RFC 7009 revocation presents the token alone; the caller
// compares any supplied client_id against the returned row.
func (s *Store) GetOAuthGrantByTokenHash(ctx context.Context, tokenHash string) (*OAuthGrant, error) {
	return queryOAuthGrant(s.GetDB().QueryRowContext(ctx, getOAuthGrantByTokenHashQuery, tokenHash), "failed to get OAuth grant by token hash")
}

// queryOAuthGrant scans the shared projection and maps sql.ErrNoRows to an
// absent grant, which is not an error for a token lookup.
func queryOAuthGrant(row *sql.Row, action string) (*OAuthGrant, error) {
	grant := &OAuthGrant{}
	var lastToolCallAt, revokedAt sql.NullTime
	var revokedBy sql.NullInt32
	if err := row.Scan(
		&grant.ID, &grant.TokenHash, &grant.ClientID, &grant.UserID, &grant.Resource, &grant.Scope,
		&grant.ExpiresAt, &grant.CreatedAt, &lastToolCallAt, &revokedAt, &revokedBy,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, action)
	}
	if lastToolCallAt.Valid {
		grant.LastToolCallAt = &lastToolCallAt.Time
	}
	if revokedAt.Valid {
		grant.RevokedAt = &revokedAt.Time
	}
	if revokedBy.Valid {
		value := int(revokedBy.Int32)
		grant.RevokedBy = &value
	}
	return grant, nil
}

// RotateOAuthGrant atomically replaces a grant's refresh token digest and expiry,
// returning whether this call was the one that claimed the credential. false
// means the token was consumed by a concurrent refresh, revoked, expired or
// issued to another client; a non-nil error is a real failure and must abort
// issuance rather than being treated as a refusal.
func (s *Store) RotateOAuthGrant(ctx context.Context, clientID, oldHash, newHash string, expiresAt, now time.Time) (int64, bool, error) {
	var id int64
	if err := s.GetDB().QueryRowContext(ctx, rotateOAuthGrantQuery, oldHash, clientID, newHash, expiresAt.UTC(), now.UTC()).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, errors.Wrap(err, "failed to rotate OAuth grant")
	}
	return id, true, nil
}

// RevokeOAuthGrant marks one connection revoked. It reports whether this call
// changed the row; a repeated revocation is a no-op, and the caller keeps the
// first revoker.
func (s *Store) RevokeOAuthGrant(ctx context.Context, id int64, revokedAt time.Time, revokedBy *int) (bool, error) {
	var revokedID int64
	if err := s.GetDB().QueryRowContext(ctx, revokeOAuthGrantQuery, id, revokedAt.UTC(), revokedBy).Scan(&revokedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, errors.Wrap(err, "failed to revoke OAuth grant")
	}
	return true, nil
}

// RevokeOAuthGrantsByUserAndClient marks every connection a principal holds for
// one client revoked and returns their ids so the caller can invalidate its
// process-local cache. Logout calls it; the ids are not otherwise needed.
func (s *Store) RevokeOAuthGrantsByUserAndClient(ctx context.Context, userID int, clientID string, revokedAt time.Time, revokedBy *int) ([]int64, error) {
	rows, err := s.GetDB().QueryContext(ctx, revokeOAuthGrantsByUserAndClientQuery, userID, clientID, revokedAt.UTC(), revokedBy)
	if err != nil {
		return nil, errors.Wrap(err, "failed to revoke OAuth grants")
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, errors.Wrap(err, "failed to scan a revoked OAuth grant id")
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for revoked OAuth grants")
	}
	return ids, nil
}

// IsOAuthGrantLive reports whether an access token's grant may still be used.
// The token's own user and client are part of the predicate. A missing row is not
// live, which is safe because a refresh credential always outlives the access
// token minted from it.
func (s *Store) IsOAuthGrantLive(ctx context.Context, id int64, userID int, clientID string) (bool, error) {
	var live bool
	if err := s.GetDB().QueryRowContext(ctx, isOAuthGrantLiveQuery, id, userID, clientID).Scan(&live); err != nil {
		return false, errors.Wrap(err, "failed to check whether an OAuth grant is live")
	}
	return live, nil
}

// TouchOAuthGrant records a tool call on the connection, at most once per
// window. A false result means the write was throttled, the grant is revoked or
// it no longer exists, none of which is an error.
func (s *Store) TouchOAuthGrant(ctx context.Context, id int64, at, cutoff time.Time) (bool, error) {
	result, err := s.GetDB().ExecContext(ctx, touchOAuthGrantQuery, id, at.UTC(), cutoff.UTC())
	if err != nil {
		return false, errors.Wrap(err, "failed to touch OAuth grant")
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to get rows affected")
	}
	return n > 0, nil
}

// GetConnectedApp returns one connection with its joined registration and
// principal, or (nil, nil) when no such row exists.
func (s *Store) GetConnectedApp(ctx context.Context, id int64) (*OAuthGrant, error) {
	row := s.GetDB().QueryRowContext(ctx, `SELECT `+connectedAppColumns+connectedAppFrom+` WHERE g.id = $1`, id)
	grant, err := scanConnectedApp(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return grant, nil
}

// ListConnectedApps returns connections newest first, optionally scoped to one
// user and narrowed by a translated filter.
func (s *Store) ListConnectedApps(ctx context.Context, find *FindOAuthGrantMessage) ([]*OAuthGrant, error) {
	if find == nil {
		find = &FindOAuthGrantMessage{}
	}
	where, args := listConnectedAppsWhere(find)

	query := `SELECT ` + connectedAppColumns + connectedAppFrom + ` WHERE ` + where + ` ORDER BY g.created_at DESC, g.id DESC`
	if find.Limit != nil {
		args = append(args, max(*find.Limit, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if find.Offset != nil {
		args = append(args, max(*find.Offset, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query connected apps")
	}
	defer rows.Close()

	var grants []*OAuthGrant
	for rows.Next() {
		grant, err := scanConnectedApp(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for connected apps")
	}
	return grants, nil
}

// listConnectedAppsWhere builds the predicate and its positional arguments. The
// translated filter comes first and the user scope after it, because the filter's
// placeholders were numbered from $1 by translateFilter; appending the scope
// first would collide with them. The handler matrix test and the argument-order
// test in oauth_grant_test.go pin this.
func listConnectedAppsWhere(find *FindOAuthGrantMessage) (string, []any) {
	where, args := []string{"TRUE"}, []any{}
	if filter := find.Filter; filter != nil {
		where = append(where, filter.Where)
		args = append(args, filter.Args...)
	}
	if find.UserID != nil {
		where = append(where, fmt.Sprintf("g.user_id = $%d", len(args)+1))
		args = append(args, *find.UserID)
	}
	return strings.Join(where, " AND "), args
}

// DeleteExpiredOAuthGrants drops rows past their expiry and a retention window:
// past that point the connection can issue nothing, and the management page has
// already shown it long enough. The maintenance runner calls it and reports how
// many rows were dropped.
func (s *Store) DeleteExpiredOAuthGrants(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, deleteExpiredOAuthGrantsQuery, before.UTC())
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired OAuth grants")
	}
	return result.RowsAffected()
}

// rowScanner is the part of *sql.Row and *sql.Rows that scanConnectedApp needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanConnectedApp maps one joined row to a grant. The principal profile is
// parsed for the password-change cutoff the state derivation needs.
func scanConnectedApp(row rowScanner) (*OAuthGrant, error) {
	grant := &OAuthGrant{}
	var lastToolCallAt, revokedAt sql.NullTime
	var revokedBy sql.NullInt32
	var profileBytes []byte
	if err := row.Scan(
		&grant.ID, &grant.TokenHash, &grant.ClientID, &grant.UserID, &grant.Resource, &grant.Scope,
		&grant.ExpiresAt, &grant.CreatedAt, &lastToolCallAt, &revokedAt, &revokedBy,
		&grant.UserEmail, &grant.UserName, &grant.UserDeleted, &profileBytes, &grant.ClientName,
	); err != nil {
		return nil, err
	}
	if lastToolCallAt.Valid {
		grant.LastToolCallAt = &lastToolCallAt.Time
	}
	if revokedAt.Valid {
		grant.RevokedAt = &revokedAt.Time
	}
	if revokedBy.Valid {
		value := int(revokedBy.Int32)
		grant.RevokedBy = &value
	}
	if len(profileBytes) > 0 {
		profile := &storepb.UserProfile{}
		if err := common.ProtojsonUnmarshaler.Unmarshal(profileBytes, profile); err != nil {
			return nil, errors.Wrap(err, "failed to decode a principal profile")
		}
		if lastChange := profile.GetLastChangePasswordTime(); lastChange != nil {
			value := lastChange.AsTime()
			grant.UserLastChangePasswordTime = &value
		}
	}
	return grant, nil
}
