package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/bcrypt"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// applicationSQLKeyPrefix marks a key as belonging to this endpoint, so a key pasted in
// the wrong place is recognizable. It is not a secret.
const applicationSQLKeyPrefix = "asql_"

// ApplicationSQLAPIKeyMessage is the store representation of one reporting key.
type ApplicationSQLAPIKeyMessage struct {
	ID int64
	// Application is the public id of the owning application.
	Application string
	KeyHash     string
	MaskedKey   string

	Description string
	CreatedBy   string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time

	// LastReportID is the last report id this key was accepted for. A repeat of it is
	// answered from the previous outcome instead of counting twice.
	LastReportID string

	// Per-key counters, which stand in for one audit row per request. audit_log is kept
	// forever, so auditing machine traffic would make it grow with request volume rather
	// than with distinct statements.
	RequestCount           int64
	StatementAcceptedCount int64
	StatementRejectedCount int64
}

// applicationSQLAPIKeyColumns is the key half of every read below.
const applicationSQLAPIKeyColumns = `k.id, k.application_id, k.key_hash, k.masked_key, k.description, k.created_by,
	k.created_at, k.last_used_at, k.revoked_at, k.last_report_id,
	k.request_count, k.statement_accepted_count, k.statement_rejected_count`

// applicationSQLKeyDigest is the deterministic lookup digest of a plaintext key. It is not
// a password hash: the bcrypt key_hash still decides acceptance, the digest only keeps a
// request from bcrypt-comparing against every live key.
func applicationSQLKeyDigest(plainKey string) string {
	sum := sha256.Sum256([]byte(plainKey))
	return hex.EncodeToString(sum[:])
}

// CreateApplicationSQLAPIKey issues a key for one application. The plaintext key is
// returned once and never stored.
func (s *Store) CreateApplicationSQLAPIKey(ctx context.Context, application string, description, createdBy string) (string, *ApplicationSQLAPIKeyMessage, error) {
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", nil, errors.Wrap(err, "failed to generate a random key")
	}
	plainKey := applicationSQLKeyPrefix + hex.EncodeToString(keyBytes)

	hash, err := bcrypt.GenerateFromPassword([]byte(plainKey), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to hash the API key")
	}

	// INSERT ... SELECT so an unknown application yields no row rather than a foreign key
	// violation to translate.
	msg := &ApplicationSQLAPIKeyMessage{}
	err = s.GetDB().QueryRowContext(ctx, `
		INSERT INTO application_sql_api_key (application_id, key_hash, key_digest, masked_key, description, created_by)
		SELECT a.application_id, $2, $3, $4, $5, $6 FROM application a WHERE a.application_id = $1
		RETURNING id, application_id, key_hash, masked_key, description, created_by, created_at,
		          last_used_at, revoked_at, last_report_id,
		          request_count, statement_accepted_count, statement_rejected_count
	`, application, string(hash), applicationSQLKeyDigest(plainKey), maskApplicationSQLAPIKey(plainKey),
		strings.TrimSpace(description), strings.TrimSpace(createdBy)).Scan(
		&msg.ID, &msg.Application, &msg.KeyHash, &msg.MaskedKey, &msg.Description, &msg.CreatedBy, &msg.CreatedAt,
		&msg.LastUsedAt, &msg.RevokedAt, &msg.LastReportID,
		&msg.RequestCount, &msg.StatementAcceptedCount, &msg.StatementRejectedCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, common.Errorf(common.NotFound, "application %q not found", application)
	}
	if err != nil {
		return "", nil, errors.Wrap(err, "failed to create the API key")
	}
	return plainKey, msg, nil
}

// ListApplicationSQLAPIKeys lists one application's live keys without exposing hashes.
func (s *Store) ListApplicationSQLAPIKeys(ctx context.Context, application string) ([]*ApplicationSQLAPIKeyMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, application_id, masked_key, description, created_by, created_at,
		       last_used_at, revoked_at, last_report_id,
		       request_count, statement_accepted_count, statement_rejected_count
		FROM application_sql_api_key
		WHERE application_id = $1 AND revoked_at IS NULL
		ORDER BY id ASC
	`, application)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the API keys")
	}
	defer rows.Close()

	var result []*ApplicationSQLAPIKeyMessage
	for rows.Next() {
		msg := &ApplicationSQLAPIKeyMessage{}
		if err := rows.Scan(
			&msg.ID, &msg.Application, &msg.MaskedKey, &msg.Description, &msg.CreatedBy, &msg.CreatedAt,
			&msg.LastUsedAt, &msg.RevokedAt, &msg.LastReportID,
			&msg.RequestCount, &msg.StatementAcceptedCount, &msg.StatementRejectedCount,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan an API key")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for the API keys")
	}
	return result, nil
}

// RevokeApplicationSQLAPIKey stops one key without touching the application's history.
func (s *Store) RevokeApplicationSQLAPIKey(ctx context.Context, application string, id int64) error {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE application_sql_api_key SET revoked_at = NOW()
		WHERE id = $1 AND application_id = $2 AND revoked_at IS NULL
	`, id, application)
	if err != nil {
		return errors.Wrap(err, "failed to revoke the API key")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected for the key revocation")
	}
	if affected == 0 {
		return common.Errorf(common.NotFound, "API key %d not found or already revoked", id)
	}
	return nil
}

// ApplicationSQLIdentity is what a validated key resolves to. The application comes from
// the key and never from the request payload, which is what stops a report from claiming to
// be another application.
type ApplicationSQLIdentity struct {
	Key         *ApplicationSQLAPIKeyMessage
	Application *ApplicationMessage
	// ApplicationName is resolved with the key so a caller can log it without a second read.
	ApplicationName string
}

// AuthenticateApplicationSQLAPIKey validates a plaintext key, resolves its application, and
// records the use. A key whose application has been stopped is refused here rather than
// later, so a stopped application cannot write anything at all.
func (s *Store) AuthenticateApplicationSQLAPIKey(ctx context.Context, plainKey string) (*ApplicationSQLIdentity, error) {
	if strings.TrimSpace(plainKey) == "" {
		return nil, common.Errorf(common.NotAuthorized, "an API key is required")
	}

	var (
		key           ApplicationSQLAPIKeyMessage
		app           ApplicationMessage
		applicationID string
	)
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT `+applicationSQLAPIKeyColumns+`,
		       a.id, a.application_id, a.name, a.description, a.disabled, a.created_by, a.created_at, a.updated_at
		FROM application_sql_api_key k
		JOIN application a ON a.application_id = k.application_id
		WHERE k.key_digest = $1 AND k.revoked_at IS NULL
	`, applicationSQLKeyDigest(plainKey)).Scan(
		&key.ID, &key.Application, &key.KeyHash, &key.MaskedKey, &key.Description, &key.CreatedBy,
		&key.CreatedAt, &key.LastUsedAt, &key.RevokedAt, &key.LastReportID,
		&key.RequestCount, &key.StatementAcceptedCount, &key.StatementRejectedCount,
		&app.ID, &applicationID, &app.Name, &app.Description, &app.Disabled, &app.CreatedBy, &app.CreatedAt, &app.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, common.Errorf(common.NotAuthorized, "invalid API key")
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the API key")
	}
	// The digest matched, so this is the only row bcrypt has to run against.
	if bcrypt.CompareHashAndPassword([]byte(key.KeyHash), []byte(plainKey)) != nil {
		return nil, common.Errorf(common.NotAuthorized, "invalid API key")
	}
	if app.Disabled {
		return nil, common.Errorf(common.NotAuthorized, "application %q is stopped", app.Name)
	}
	app.ApplicationID = applicationID

	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE application_sql_api_key
		SET last_used_at = NOW(), request_count = request_count + 1
		WHERE id = $1
	`, key.ID); err != nil {
		return nil, errors.Wrap(err, "failed to record the key use")
	}
	now := time.Now().UTC()
	key.LastUsedAt = &now
	key.RequestCount++

	return &ApplicationSQLIdentity{Key: &key, Application: &app, ApplicationName: app.Name}, nil
}

// RecordApplicationSQLReport stores one report's outcome on its key: the counters, and the
// report id that a retry carries.
func (s *Store) RecordApplicationSQLReport(ctx context.Context, keyID int64, reportID string, accepted, rejected int) error {
	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE application_sql_api_key
		SET statement_accepted_count = statement_accepted_count + $2,
		    statement_rejected_count = statement_rejected_count + $3,
		    last_report_id = COALESCE(NULLIF($4, ''), last_report_id)
		WHERE id = $1
	`, keyID, accepted, rejected, strings.TrimSpace(reportID)); err != nil {
		return errors.Wrap(err, "failed to record the report outcome")
	}
	return nil
}

func maskApplicationSQLAPIKey(plainKey string) string {
	const visiblePrefixLength = 13
	const visibleSuffixLength = 4
	if len(plainKey) <= visiblePrefixLength+visibleSuffixLength {
		if len(plainKey) <= 1 {
			return "*"
		}
		return plainKey[:1] + strings.Repeat("*", len(plainKey)-1)
	}
	maskedLength := len(plainKey) - visiblePrefixLength - visibleSuffixLength
	return plainKey[:visiblePrefixLength] + strings.Repeat("*", maskedLength) + plainKey[len(plainKey)-visibleSuffixLength:]
}
