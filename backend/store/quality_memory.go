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

// QualityMemoryMessage is one durable piece of knowledge with its evidence and its
// correction path. Superseded and expired rows stay for that path; a delete is a soft
// delete that only removes the row from reads.
type QualityMemoryMessage struct {
	ID           int64
	ResourceID   string
	Kind         storepb.QualityMemoryKind
	Subject      string
	Statement    string
	Source       storepb.QualityMemorySource
	Status       storepb.QualityMemoryStatus
	Evidence     *storepb.QualityMemoryEvidenceList
	DomainRef    string
	ExpiresAt    *time.Time
	ConfirmedBy  *int32
	ConfirmedAt  *time.Time
	SupersededBy string
	CreatedBy    *int32
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const qualityMemoryColumns = `id, resource_id, kind, subject, statement, source, status,
	evidence, domain_ref, expires_at, confirmed_by, confirmed_at, superseded_by,
	created_by, created_at, updated_at`

// CreateQualityMemoryInput is one record attempt.
type CreateQualityMemoryInput struct {
	ResourceID  string
	Kind        storepb.QualityMemoryKind
	Subject     string
	Statement   string
	Source      storepb.QualityMemorySource
	Status      storepb.QualityMemoryStatus
	Evidence    *storepb.QualityMemoryEvidenceList
	DomainRef   string
	ExpiresAt   *time.Time
	ConfirmedBy *int32
	CreatedBy   *int32
}

// CreateQualityMemory records one memory. A confirmed one supersedes the confirmed
// memory of the same kind and subject: the previous row keeps its evidence and gains
// superseded_by, it is never overwritten.
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
	defer tx.Rollback()
	// A confirmed memory records when it was confirmed; a proposal records nothing yet.
	var confirmedAt *time.Time
	if input.Status == storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED {
		now := time.Now()
		confirmedAt = &now
	}
	row := tx.QueryRowContext(ctx, `
		INSERT INTO quality_memory
			(resource_id, kind, subject, statement, source, status, evidence, domain_ref,
			 expires_at, confirmed_by, confirmed_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+qualityMemoryColumns,
		input.ResourceID, int16(input.Kind), input.Subject, input.Statement,
		int16(input.Source), int16(input.Status), evidence, input.DomainRef,
		input.ExpiresAt, input.ConfirmedBy, confirmedAt, input.CreatedBy)
	message, err := scanQualityMemory(row)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the quality memory")
	}
	if message.Status == storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED {
		if err := supersedeQualityMemory(ctx, tx, input.Kind, input.Subject, input.ResourceID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit the quality memory")
	}
	return message, nil
}

// GetQualityMemory reads one memory; nil means it does not exist or was deleted.
func (s *Store) GetQualityMemory(ctx context.Context, resourceID string) (*QualityMemoryMessage, error) {
	message, err := scanQualityMemory(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityMemoryColumns+` FROM quality_memory WHERE resource_id = $1 AND deleted = FALSE`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality memory")
	}
	return message, nil
}

// FindQualityMemoryMessage filters a list; every field is optional.
type FindQualityMemoryMessage struct {
	Kind    *storepb.QualityMemoryKind
	Status  *storepb.QualityMemoryStatus
	Subject *string
	Limit   *int
	Offset  *int
}

// ListQualityMemories lists memories newest first. An expired confirmation is filtered
// as EXPIRED, not as CONFIRMED, so a reader cannot mistake stale knowledge for current.
func (s *Store) ListQualityMemories(ctx context.Context, find *FindQualityMemoryMessage) ([]*QualityMemoryMessage, error) {
	if find == nil {
		find = &FindQualityMemoryMessage{}
	}
	where := "deleted = FALSE"
	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	if find.Kind != nil {
		where += ` AND kind = ` + arg(int16(*find.Kind))
	}
	if find.Subject != nil && strings.TrimSpace(*find.Subject) != "" {
		where += ` AND subject = ` + arg(strings.TrimSpace(*find.Subject))
	}
	if find.Status != nil {
		switch *find.Status {
		case storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_EXPIRED:
			where += ` AND status = ` + arg(int16(storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED)) +
				` AND expires_at IS NOT NULL AND expires_at <= NOW()`
		case storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED:
			where += ` AND status = ` + arg(int16(storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED)) +
				` AND (expires_at IS NULL OR expires_at > NOW())`
		default:
			where += ` AND status = ` + arg(int16(*find.Status))
		}
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
	var messages []*QualityMemoryMessage
	for rows.Next() {
		message, err := scanQualityMemory(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality memory")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality memories")
	}
	return messages, nil
}

// ConfirmQualityMemory moves a PROPOSED memory to CONFIRMED or REJECTED. The compare
// and set on the previous status is what makes two reviewers' clicks safe, and a
// confirmed memory supersedes the previous one of the same kind and subject in the
// same transaction, so exactly one row is current.
func (s *Store) ConfirmQualityMemory(ctx context.Context, resourceID string, status storepb.QualityMemoryStatus, confirmedBy *int32) (*QualityMemoryMessage, error) {
	if status != storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED &&
		status != storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_REJECTED {
		return nil, common.Errorf(common.Invalid, "a confirmation must confirm or reject")
	}
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var kind int16
	var subject string
	if err := tx.QueryRowContext(ctx, `
		SELECT kind, subject FROM quality_memory WHERE resource_id = $1 AND deleted = FALSE`,
		resourceID).Scan(&kind, &subject); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "memory %q not found", resourceID)
		}
		return nil, errors.Wrap(err, "failed to read the memory to confirm")
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		strings.ToLower(storepb.QualityMemoryKind(kind).String())+"|"+subject); err != nil {
		return nil, errors.Wrap(err, "failed to lock the memory subject")
	}
	message, err := scanQualityMemory(tx.QueryRowContext(ctx, `
		UPDATE quality_memory
		SET status = $2, confirmed_by = $3, confirmed_at = NOW(), updated_at = NOW()
		WHERE resource_id = $1 AND status = $4 AND deleted = FALSE
		RETURNING `+qualityMemoryColumns,
		resourceID, int16(status), confirmedBy,
		int16(storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_PROPOSED)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.Conflict, "the memory was already decided")
		}
		return nil, errors.Wrap(err, "failed to confirm the quality memory")
	}
	if message.Status == storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED {
		if err := supersedeQualityMemory(ctx, tx, storepb.QualityMemoryKind(kind), subject, resourceID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit the memory confirmation")
	}
	return message, nil
}

// supersedeQualityMemory marks the previous confirmed memory of one (kind, subject) as
// superseded. It runs inside the caller's transaction, after the new row is locked in.
func supersedeQualityMemory(ctx context.Context, tx *sql.Tx, kind storepb.QualityMemoryKind, subject, resourceID string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE quality_memory
		SET status = $3, superseded_by = $4, updated_at = NOW()
		WHERE kind = $1 AND subject = $2 AND status = $5 AND resource_id <> $4 AND deleted = FALSE
	`, int16(kind), subject,
		int16(storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_SUPERSEDED),
		resourceID,
		int16(storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED)); err != nil {
		return errors.Wrap(err, "failed to supersede the previous memory")
	}
	return nil
}

// DeleteQualityMemory soft-deletes a memory; the audit row records who removed it.
func (s *Store) DeleteQualityMemory(ctx context.Context, resourceID string) error {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_memory SET deleted = TRUE, updated_at = NOW()
		WHERE resource_id = $1 AND deleted = FALSE`, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to delete the quality memory")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to count the deleted memory")
	}
	if affected == 0 {
		return common.Errorf(common.NotFound, "memory %q not found", resourceID)
	}
	return nil
}

// scanQualityMemory reads one quality_memory row.
func scanQualityMemory(scanner interface{ Scan(dest ...any) error }) (*QualityMemoryMessage, error) {
	var (
		message     QualityMemoryMessage
		kind        int16
		source      int16
		status      int16
		evidence    []byte
		expiresAt   sql.NullTime
		confirmedBy sql.NullInt32
		confirmedAt sql.NullTime
		createdBy   sql.NullInt32
	)
	if err := scanner.Scan(&message.ID, &message.ResourceID, &kind, &message.Subject,
		&message.Statement, &source, &status, &evidence, &message.DomainRef, &expiresAt,
		&confirmedBy, &confirmedAt, &message.SupersededBy, &createdBy,
		&message.CreatedAt, &message.UpdatedAt); err != nil {
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
	if confirmedBy.Valid {
		id := confirmedBy.Int32
		message.ConfirmedBy = &id
	}
	if confirmedAt.Valid {
		value := confirmedAt.Time
		message.ConfirmedAt = &value
	}
	if createdBy.Valid {
		id := createdBy.Int32
		message.CreatedBy = &id
	}
	return &message, nil
}
