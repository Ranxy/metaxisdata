package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/bcrypt"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// OpenLineageAPIKeyMessage is the store representation of an API key.
type OpenLineageAPIKeyMessage struct {
	ID          int64
	KeyHash     string
	MaskedKey   string
	Description string
	CreatedBy   string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
	// ScopeNamespace restricts the key to one OpenLineage namespace. Empty
	// means the key may submit events for any namespace.
	ScopeNamespace string
}

// openLineageAPIKeyDigest derives the deterministic lookup digest of a plaintext
// key. It is not a password hash: the bcrypt key_hash still decides acceptance,
// the digest only avoids scanning every row.
func openLineageAPIKeyDigest(plainKey string) string {
	sum := sha256.Sum256([]byte(plainKey))
	return hex.EncodeToString(sum[:])
}

// CreateOpenLineageAPIKey generates a new API key and stores its bcrypt hash.
// Returns the plain-text key (only available at creation time) and the stored message.
func (s *Store) CreateOpenLineageAPIKey(ctx context.Context, description, createdBy, scopeNamespace string) (string, *OpenLineageAPIKeyMessage, error) {
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", nil, errors.Wrap(err, "failed to generate random key")
	}
	plainKey := "ol_" + hex.EncodeToString(keyBytes)
	maskedKey := maskOpenLineageAPIKey(plainKey)

	hash, err := bcrypt.GenerateFromPassword([]byte(plainKey), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to hash API key")
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	var msg OpenLineageAPIKeyMessage
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO openlineage_api_key (key_hash, key_digest, masked_key, description, created_by, scope_namespace)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, key_hash, masked_key, description, created_by, created_at, last_used_at, revoked_at, scope_namespace
	`, string(hash), openLineageAPIKeyDigest(plainKey), maskedKey, description, createdBy, scopeNamespace).Scan(
		&msg.ID, &msg.KeyHash, &msg.MaskedKey, &msg.Description, &msg.CreatedBy, &msg.CreatedAt, &msg.LastUsedAt, &msg.RevokedAt, &msg.ScopeNamespace,
	); err != nil {
		return "", nil, errors.Wrap(err, "failed to create API key")
	}

	if err := tx.Commit(); err != nil {
		return "", nil, errors.Wrap(err, "failed to commit transaction")
	}
	return plainKey, &msg, nil
}

// ValidateOpenLineageAPIKey checks the key against the stored digest and hash.
// Returns the matching key record or an error if no match is found.
func (s *Store) ValidateOpenLineageAPIKey(ctx context.Context, plainKey string) (*OpenLineageAPIKeyMessage, error) {
	var msg OpenLineageAPIKeyMessage
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT id, key_hash, masked_key, description, created_by, created_at, last_used_at, revoked_at, scope_namespace
		FROM openlineage_api_key
		WHERE key_digest = $1 AND revoked_at IS NULL
	`, openLineageAPIKeyDigest(plainKey)).Scan(
		&msg.ID, &msg.KeyHash, &msg.MaskedKey, &msg.Description, &msg.CreatedBy, &msg.CreatedAt, &msg.LastUsedAt, &msg.RevokedAt, &msg.ScopeNamespace,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("invalid API key")
		}
		return nil, errors.Wrap(err, "failed to query API key")
	}
	if bcrypt.CompareHashAndPassword([]byte(msg.KeyHash), []byte(plainKey)) != nil {
		return nil, errors.New("invalid API key")
	}

	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE openlineage_api_key
		SET last_used_at = NOW()
		WHERE id = $1
	`, msg.ID); err != nil {
		return nil, errors.Wrap(err, "failed to update API key last used time")
	}
	now := time.Now().UTC()
	msg.LastUsedAt = &now
	return &msg, nil
}

// ListOpenLineageAPIKey returns all API keys (without hashes exposed).
func (s *Store) ListOpenLineageAPIKey(ctx context.Context) ([]*OpenLineageAPIKeyMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, key_hash, masked_key, description, created_by, created_at, last_used_at, revoked_at, scope_namespace
		FROM openlineage_api_key
		WHERE revoked_at IS NULL
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query API keys")
	}
	defer rows.Close()

	var result []*OpenLineageAPIKeyMessage
	for rows.Next() {
		var msg OpenLineageAPIKeyMessage
		if err := rows.Scan(
			&msg.ID, &msg.KeyHash, &msg.MaskedKey, &msg.Description, &msg.CreatedBy, &msg.CreatedAt, &msg.LastUsedAt, &msg.RevokedAt, &msg.ScopeNamespace,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan API key")
		}
		result = append(result, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return result, nil
}

// RevokeOpenLineageAPIKey soft-deletes an API key by setting revoked_at.
func (s *Store) RevokeOpenLineageAPIKey(ctx context.Context, id int64) error {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE openlineage_api_key SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL
	`, id)
	if err != nil {
		return errors.Wrap(err, "failed to revoke API key")
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if n == 0 {
		return common.Errorf(common.NotFound, "API key %d not found or already revoked", id)
	}
	return nil
}

func maskOpenLineageAPIKey(plainKey string) string {
	const visiblePrefixLength = 8
	const visibleSuffixLength = 9

	if len(plainKey) <= visiblePrefixLength+visibleSuffixLength {
		if len(plainKey) <= 1 {
			return "*"
		}
		return plainKey[:1] + strings.Repeat("*", len(plainKey)-1)
	}

	maskedLength := len(plainKey) - visiblePrefixLength - visibleSuffixLength
	return plainKey[:visiblePrefixLength] + strings.Repeat("*", maskedLength) + plainKey[len(plainKey)-visibleSuffixLength:]
}

// FindExternalDatasetByGUIDs returns external datasets matching the given GUIDs.
func (s *Store) FindExternalDatasetByGUIDs(ctx context.Context, guids []string) ([]*ExternalDatasetMessage, error) {
	if len(guids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(guids))
	args := make([]any, len(guids))
	for i, g := range guids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = g
	}

	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, guid, namespace, name, dataset_type, created_at, updated_at
		FROM external_dataset
		WHERE guid IN (`+strings.Join(placeholders, ", ")+`)
		ORDER BY id ASC
	`, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query external datasets by guids")
	}
	defer rows.Close()

	var result []*ExternalDatasetMessage
	for rows.Next() {
		var msg ExternalDatasetMessage
		if err := rows.Scan(
			&msg.ID, &msg.GUID, &msg.Namespace, &msg.Name, &msg.DatasetType,
			&msg.CreatedAt, &msg.UpdatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan external dataset")
		}
		result = append(result, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return result, nil
}
