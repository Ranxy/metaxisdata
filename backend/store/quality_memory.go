package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityMemoryMessage is one deterministic asset fact with its evidence. A fact
// is written confirmed; the previous fact of the same subject is superseded, never
// overwritten, so the correction path keeps its history.
type QualityMemoryMessage struct {
	ID           int64
	ResourceID   string
	Kind         storepb.QualityMemoryKind
	Subject      string
	Statement    string
	Source       storepb.QualityMemorySource
	Status       storepb.QualityMemoryStatus
	Evidence     *storepb.QualityMemoryEvidenceList
	ExpiresAt    *time.Time
	SupersededBy string
	CreatedBy    *int32
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const qualityMemoryColumns = `id, resource_id, kind, subject, statement, source, status,
	evidence, expires_at, superseded_by, created_by, created_at, updated_at`

// CreateQualityMemoryInput is one asset fact to record.
type CreateQualityMemoryInput struct {
	ResourceID string
	// Subject is the target the fact is about.
	Subject   string
	Statement string
	Evidence  *storepb.QualityMemoryEvidenceList
	ExpiresAt *time.Time
	CreatedBy *int32
}

// CreateQualityMemory records one confirmed asset fact. A new fact supersedes the
// previous confirmed fact of the same subject in the same transaction, and a
// transaction-scoped advisory lock serializes concurrent writers so exactly one row
// stays current.
func (s *Store) CreateQualityMemory(ctx context.Context, input *CreateQualityMemoryInput) (*QualityMemoryMessage, error) {
	if input == nil || input.ResourceID == "" {
		return nil, common.Errorf(common.Invalid, "a resource id is required")
	}
	if input.Evidence == nil {
		input.Evidence = &storepb.QualityMemoryEvidenceList{}
	}
	evidence, err := protojson.Marshal(input.Evidence)
	if err != nil {
		return nil, err
	}
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		qualityMemorySubjectLockKey(input.Subject)); err != nil {
		return nil, errors.Wrap(err, "failed to lock the memory subject")
	}
	message, err := scanQualityMemory(tx.QueryRowContext(ctx, `
		INSERT INTO quality_memory
			(resource_id, kind, subject, statement, source, status, evidence, expires_at,
			 created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+qualityMemoryColumns,
		input.ResourceID,
		int16(storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT), input.Subject,
		input.Statement,
		int16(storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT),
		int16(storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED), evidence,
		input.ExpiresAt, input.CreatedBy))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the quality memory")
	}
	if err := supersedeQualityMemory(ctx, tx, input.Subject, input.ResourceID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit the quality memory")
	}
	return message, nil
}

// supersedeQualityMemory marks the previous confirmed fact of one subject as
// superseded. It runs inside the caller's transaction, after the new row is written.
func supersedeQualityMemory(ctx context.Context, tx *sql.Tx, subject, resourceID string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE quality_memory
		SET status = $3, superseded_by = $4, updated_at = NOW()
		WHERE kind = $1 AND subject = $2 AND status = $5 AND resource_id <> $4
	`, int16(storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT), subject,
		int16(storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_SUPERSEDED),
		resourceID,
		int16(storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED)); err != nil {
		return errors.Wrap(err, "failed to supersede the previous memory")
	}
	return nil
}

// qualityMemorySubjectLockKey is the advisory-lock key for one subject: only one
// writer may insert and supersede for a subject at a time.
func qualityMemorySubjectLockKey(subject string) string {
	return strings.ToLower(storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT.String()) + "|" + subject
}

// FindQualityMemoryMessage filters an asset-fact list; every field is optional.
type FindQualityMemoryMessage struct {
	Subject *string
	Limit   *int
	Offset  *int
}

// ListQualityMemories lists the confirmed asset facts newest first. A fact whose
// expiry has passed is reported as EXPIRED on read, so a caller cannot mistake stale
// knowledge for current knowledge; the row itself is kept.
func (s *Store) ListQualityMemories(ctx context.Context, find *FindQualityMemoryMessage) ([]*QualityMemoryMessage, error) {
	if find == nil {
		find = &FindQualityMemoryMessage{}
	}
	args := []any{int16(storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED)}
	where := "status = $1"
	arg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	if find.Subject != nil && strings.TrimSpace(*find.Subject) != "" {
		where += ` AND subject = ` + arg(strings.TrimSpace(*find.Subject))
	}
	limit, offset := 100, 0
	if find.Limit != nil && *find.Limit > 0 {
		limit = *find.Limit
	}
	if find.Offset != nil && *find.Offset > 0 {
		offset = *find.Offset
	}
	query := `SELECT ` + qualityMemoryColumns + ` FROM quality_memory WHERE ` + where +
		` ORDER BY created_at DESC, id DESC LIMIT ` + arg(limit) + ` OFFSET ` + arg(offset)
	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality memories")
	}
	defer rows.Close()
	now := time.Now()
	var messages []*QualityMemoryMessage
	for rows.Next() {
		message, err := scanQualityMemory(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality memory")
		}
		if message.Status == storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED &&
			message.ExpiresAt != nil && !message.ExpiresAt.After(now) {
			message.Status = storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_EXPIRED
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality memories")
	}
	return messages, nil
}

// scanQualityMemory reads one quality_memory row.
func scanQualityMemory(scanner interface{ Scan(dest ...any) error }) (*QualityMemoryMessage, error) {
	var (
		message   QualityMemoryMessage
		kind      int16
		source    int16
		status    int16
		evidence  []byte
		expiresAt sql.NullTime
		createdBy sql.NullInt32
	)
	if err := scanner.Scan(&message.ID, &message.ResourceID, &kind, &message.Subject,
		&message.Statement, &source, &status, &evidence, &expiresAt,
		&message.SupersededBy, &createdBy, &message.CreatedAt, &message.UpdatedAt); err != nil {
		return nil, err
	}
	message.Kind = storepb.QualityMemoryKind(kind)
	message.Source = storepb.QualityMemorySource(source)
	message.Status = storepb.QualityMemoryStatus(status)
	message.Evidence = &storepb.QualityMemoryEvidenceList{}
	if len(evidence) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(evidence, message.Evidence); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the memory evidence")
		}
	}
	if expiresAt.Valid {
		value := expiresAt.Time
		message.ExpiresAt = &value
	}
	if createdBy.Valid {
		id := createdBy.Int32
		message.CreatedBy = &id
	}
	return &message, nil
}
