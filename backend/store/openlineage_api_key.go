package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/bcrypt"
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
}

// CreateOpenLineageAPIKey generates a new API key and stores its bcrypt hash.
// Returns the plain-text key (only available at creation time) and the stored message.
func (s *Store) CreateOpenLineageAPIKey(ctx context.Context, description, createdBy string) (string, *OpenLineageAPIKeyMessage, error) {
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
		INSERT INTO openlineage_api_key (key_hash, masked_key, description, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id, key_hash, masked_key, description, created_by, created_at, last_used_at, revoked_at
	`, string(hash), maskedKey, description, createdBy).Scan(
		&msg.ID, &msg.KeyHash, &msg.MaskedKey, &msg.Description, &msg.CreatedBy, &msg.CreatedAt, &msg.LastUsedAt, &msg.RevokedAt,
	); err != nil {
		return "", nil, errors.Wrap(err, "failed to create API key")
	}

	if err := tx.Commit(); err != nil {
		return "", nil, errors.Wrap(err, "failed to commit transaction")
	}
	return plainKey, &msg, nil
}

// ValidateOpenLineageAPIKey checks the key against stored hashes.
// Returns the matching key record or an error if no match is found.
func (s *Store) ValidateOpenLineageAPIKey(ctx context.Context, plainKey string) (*OpenLineageAPIKeyMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, key_hash, masked_key, description, created_by, created_at, last_used_at, revoked_at
		FROM openlineage_api_key
		WHERE revoked_at IS NULL
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query API keys")
	}
	defer rows.Close()

	for rows.Next() {
		var msg OpenLineageAPIKeyMessage
		if err := rows.Scan(
			&msg.ID, &msg.KeyHash, &msg.MaskedKey, &msg.Description, &msg.CreatedBy, &msg.CreatedAt, &msg.LastUsedAt, &msg.RevokedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan API key")
		}
		if bcrypt.CompareHashAndPassword([]byte(msg.KeyHash), []byte(plainKey)) == nil {
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
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return nil, errors.New("invalid API key")
}

// ListOpenLineageAPIKey returns all API keys (without hashes exposed).
func (s *Store) ListOpenLineageAPIKey(ctx context.Context) ([]*OpenLineageAPIKeyMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, key_hash, masked_key, description, created_by, created_at, last_used_at, revoked_at
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
			&msg.ID, &msg.KeyHash, &msg.MaskedKey, &msg.Description, &msg.CreatedBy, &msg.CreatedAt, &msg.LastUsedAt, &msg.RevokedAt,
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
		return errors.Errorf("API key %d not found or already revoked", id)
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
		SELECT id, guid, namespace, name, dataset_type, schema_fields, created_at, updated_at
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
			&schemaFieldsScanner{fields: &msg.SchemaFields},
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
