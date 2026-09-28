package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// OAuthClient is one registered OAuth client (RFC 7591).
type OAuthClient struct {
	ClientID                string
	ClientName              string
	RedirectURIs            []string
	TokenEndpointAuthMethod string
	CreatedTime             time.Time
	LastUsedTime            *time.Time
}

// oauthClientColumns is the column list every oauth_client query selects, in
// the order oauthClientFromScan expects. The surrogate id is internal and is
// deliberately not read.
const oauthClientColumns = `client_id, client_name, redirect_uris, token_endpoint_auth_method, created_at, last_used_at`

// CreateOAuthClient stores a new registration and returns the stored row.
func (s *Store) CreateOAuthClient(ctx context.Context, client *OAuthClient) (*OAuthClient, error) {
	redirectURIs, err := encodeOAuthRedirectURIs(client.RedirectURIs)
	if err != nil {
		return nil, err
	}

	var (
		clientID     string
		clientName   string
		redirects    []byte
		authMethod   string
		createdTime  time.Time
		lastUsedTime *time.Time
	)
	if err := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO oauth_client (client_id, client_name, redirect_uris, token_endpoint_auth_method)
		VALUES ($1, $2, $3, $4)
		RETURNING `+oauthClientColumns+`
	`, client.ClientID, client.ClientName, redirectURIs, client.TokenEndpointAuthMethod).Scan(
		&clientID, &clientName, &redirects, &authMethod, &createdTime, &lastUsedTime,
	); err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "OAuth client %q already registered", client.ClientID)
		}
		return nil, errors.Wrap(err, "failed to create OAuth client")
	}
	return oauthClientFromScan(clientID, clientName, redirects, authMethod, createdTime, lastUsedTime)
}

// GetOAuthClient returns the registration for clientID, or (nil, nil) when no
// such client exists. The nil result matters: a client deleted after it
// registered must make the token endpoint answer invalid_client, not 500.
func (s *Store) GetOAuthClient(ctx context.Context, clientID string) (*OAuthClient, error) {
	var (
		storedClientID string
		clientName     string
		redirects      []byte
		authMethod     string
		createdTime    time.Time
		lastUsedTime   *time.Time
	)
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT `+oauthClientColumns+`
		FROM oauth_client
		WHERE client_id = $1
	`, clientID).Scan(&storedClientID, &clientName, &redirects, &authMethod, &createdTime, &lastUsedTime); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to query OAuth client")
	}
	return oauthClientFromScan(storedClientID, clientName, redirects, authMethod, createdTime, lastUsedTime)
}

// ListOAuthClients returns every registration, oldest first.
func (s *Store) ListOAuthClients(ctx context.Context) ([]*OAuthClient, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+oauthClientColumns+`
		FROM oauth_client
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query OAuth clients")
	}
	defer rows.Close()

	var clients []*OAuthClient
	for rows.Next() {
		var (
			clientID     string
			clientName   string
			redirects    []byte
			authMethod   string
			createdTime  time.Time
			lastUsedTime *time.Time
		)
		if err := rows.Scan(&clientID, &clientName, &redirects, &authMethod, &createdTime, &lastUsedTime); err != nil {
			return nil, errors.Wrap(err, "failed to scan OAuth client")
		}
		client, err := oauthClientFromScan(clientID, clientName, redirects, authMethod, createdTime, lastUsedTime)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return clients, nil
}

// TouchOAuthClient stamps last_used_time with NOW(). A client that no longer
// exists is reported as NotFound rather than silently ignored.
func (s *Store) TouchOAuthClient(ctx context.Context, clientID string) error {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE oauth_client SET last_used_at = NOW() WHERE client_id = $1
	`, clientID)
	if err != nil {
		return errors.Wrap(err, "failed to touch OAuth client")
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if n == 0 {
		return common.Errorf(common.NotFound, "OAuth client %q not found", clientID)
	}
	return nil
}

// DeleteOAuthClient removes a registration. The row is deleted rather than
// revoked: a client_id is an opaque registration token with no history to keep.
func (s *Store) DeleteOAuthClient(ctx context.Context, clientID string) error {
	result, err := s.GetDB().ExecContext(ctx, `
		DELETE FROM oauth_client WHERE client_id = $1
	`, clientID)
	if err != nil {
		return errors.Wrap(err, "failed to delete OAuth client")
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if n == 0 {
		return common.Errorf(common.NotFound, "OAuth client %q not found", clientID)
	}
	return nil
}

// oauthClientFromScan maps one row's column values to the message. It is the
// pure half of the row mapping so the redirect-URI decoding is testable without
// a database.
func oauthClientFromScan(clientID, clientName string, redirectURIsRaw []byte, authMethod string, createdTime time.Time, lastUsedTime *time.Time) (*OAuthClient, error) {
	redirectURIs, err := decodeOAuthRedirectURIs(redirectURIsRaw)
	if err != nil {
		return nil, err
	}
	return &OAuthClient{
		ClientID:                clientID,
		ClientName:              clientName,
		RedirectURIs:            redirectURIs,
		TokenEndpointAuthMethod: authMethod,
		CreatedTime:             createdTime,
		LastUsedTime:            lastUsedTime,
	}, nil
}

// encodeOAuthRedirectURIs renders the registration's redirect list as the JSON
// array oauth_client.redirect_uris stores. A nil or empty list encodes as "[]",
// never as null, because the column is NOT NULL.
func encodeOAuthRedirectURIs(redirectURIs []string) ([]byte, error) {
	if redirectURIs == nil {
		redirectURIs = []string{}
	}
	data, err := json.Marshal(redirectURIs)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode OAuth redirect URIs")
	}
	return data, nil
}

// decodeOAuthRedirectURIs is the inverse of encodeOAuthRedirectURIs. It returns
// an empty, non-nil list for a null or empty column value so callers can range
// over the result.
func decodeOAuthRedirectURIs(data []byte) ([]string, error) {
	if len(data) == 0 {
		return []string{}, nil
	}
	var redirectURIs []string
	if err := json.Unmarshal(data, &redirectURIs); err != nil {
		return nil, errors.Wrap(err, "failed to decode OAuth redirect URIs")
	}
	if redirectURIs == nil {
		redirectURIs = []string{}
	}
	return redirectURIs, nil
}
