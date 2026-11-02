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

// qualityRuleTemplateColumns is the column list every quality_rule_template read
// returns, in the order scanQualityRuleTemplate consumes it.
const qualityRuleTemplateColumns = `id, resource_id, title, description, dimension,
	rule_type, config, severity, created_by, updated_by, created_at, updated_at`

// QualityRuleTemplateMessage is a custom rule template: a check type and its
// parameters, without the target or the columns a target owns. Every field is
// part of the definition, so an update writes the whole row.
type QualityRuleTemplateMessage struct {
	ID          int64
	ResourceID  string
	Title       string
	Description string
	Dimension   storepb.QualityDimension
	RuleType    storepb.QualityRuleType
	// Config holds the parameters that are not target-bound: thresholds, values,
	// patterns, sampling, timeout and empty-data behavior.
	Config    *storepb.QualityRuleConfig
	Severity  storepb.QualitySeverity
	CreatedBy *int32
	UpdatedBy *int32
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FindQualityRuleTemplateMessage is the query filter for custom templates. The
// list has no user-facing filters yet, so a resource id is the only predicate the
// store supports.
type FindQualityRuleTemplateMessage struct {
	ResourceID *string
	Limit      *int
	Offset     *int
}

// scanQualityRuleTemplate maps one quality_rule_template row to the message. The
// config column is the protojson payload of QualityRuleConfig, named in the
// column's SQL comment.
func scanQualityRuleTemplate(row qualityRuleScanner) (*QualityRuleTemplateMessage, error) {
	var (
		msg        QualityRuleTemplateMessage
		configJSON []byte
		createdBy  sql.NullInt32
		updatedBy  sql.NullInt32
	)
	if err := row.Scan(
		&msg.ID,
		&msg.ResourceID,
		&msg.Title,
		&msg.Description,
		&msg.Dimension,
		&msg.RuleType,
		&configJSON,
		&msg.Severity,
		&createdBy,
		&updatedBy,
		&msg.CreatedAt,
		&msg.UpdatedAt,
	); err != nil {
		return nil, err
	}

	msg.Config = &storepb.QualityRuleConfig{}
	if err := unmarshalQualityProto(configJSON, msg.Config); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal quality rule template config")
	}
	msg.CreatedBy = qualityRuleInt32(createdBy)
	msg.UpdatedBy = qualityRuleInt32(updatedBy)
	return &msg, nil
}

// buildQualityRuleTemplateWhere renders the filter as a SQL predicate plus its
// parameterized arguments. It is a pure function so the scoping rule is
// guard-tested without a live PostgreSQL; the listing direction is fixed in
// ListQualityRuleTemplates, not here.
func buildQualityRuleTemplateWhere(find *FindQualityRuleTemplateMessage) (string, []any) {
	if find == nil {
		find = &FindQualityRuleTemplateMessage{}
	}
	where := []string{"TRUE"}
	args := []any{}
	if v := find.ResourceID; v != nil && strings.TrimSpace(*v) != "" {
		args = append(args, strings.TrimSpace(*v))
		where = append(where, fmt.Sprintf("resource_id = $%d", len(args)))
	}
	return strings.Join(where, " AND "), args
}

// qualityRuleTemplateListQuery renders the list statement: one page of the
// newest templates first, with created_at then id breaking timestamp ties so a
// page boundary cannot repeat or skip a row.
func qualityRuleTemplateListQuery(find *FindQualityRuleTemplateMessage) (string, []any) {
	if find == nil {
		find = &FindQualityRuleTemplateMessage{}
	}
	where, args := buildQualityRuleTemplateWhere(find)
	query := `SELECT ` + qualityRuleTemplateColumns + ` FROM quality_rule_template WHERE ` + where +
		` ORDER BY created_at DESC, id DESC`
	if find.Limit != nil {
		args = append(args, max(*find.Limit, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if find.Offset != nil {
		args = append(args, max(*find.Offset, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}
	return query, args
}

// validateQualityRuleTemplateIdentity checks the fields a template must carry.
// The enums are NOT NULL columns, so an UNSPECIFIED value would either fail the
// insert or store a template no editor can apply.
func validateQualityRuleTemplateIdentity(template *QualityRuleTemplateMessage) error {
	if template == nil {
		return common.Errorf(common.Invalid, "a quality rule template is required")
	}
	if strings.TrimSpace(template.ResourceID) == "" {
		return common.Errorf(common.Invalid, "quality rule template resource id is required")
	}
	if template.Dimension == storepb.QualityDimension_QUALITY_DIMENSION_UNSPECIFIED {
		return common.Errorf(common.Invalid, "quality rule template dimension is required")
	}
	if template.RuleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED {
		return common.Errorf(common.Invalid, "quality rule template rule type is required")
	}
	if template.Severity == storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED {
		return common.Errorf(common.Invalid, "quality rule template severity is required")
	}
	return nil
}

// CreateQualityRuleTemplate inserts a template. The caller provides the public
// resource id: it names the API resource and is never reused, so the store must
// not derive it from the row id.
func (s *Store) CreateQualityRuleTemplate(ctx context.Context, template *QualityRuleTemplateMessage) (*QualityRuleTemplateMessage, error) {
	if err := validateQualityRuleTemplateIdentity(template); err != nil {
		return nil, err
	}
	config := template.Config
	if config == nil {
		config = &storepb.QualityRuleConfig{}
	}
	configJSON, err := marshalQualityProto(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal quality rule template config")
	}

	created, err := scanQualityRuleTemplate(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_rule_template (
			resource_id, title, description, dimension, rule_type, config, severity,
			created_by, updated_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+qualityRuleTemplateColumns,
		strings.TrimSpace(template.ResourceID),
		strings.TrimSpace(template.Title),
		strings.TrimSpace(template.Description),
		template.Dimension,
		template.RuleType,
		configJSON,
		template.Severity,
		qualityRuleWriteInt32(template.CreatedBy),
		qualityRuleWriteInt32(template.UpdatedBy),
	))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "quality rule template %q already exists", template.ResourceID)
		}
		return nil, errors.Wrap(err, "failed to create quality rule template")
	}
	return created, nil
}

// GetQualityRuleTemplate reads one template by its public resource id. A nil
// message means no row carries that id.
func (s *Store) GetQualityRuleTemplate(ctx context.Context, resourceID string) (*QualityRuleTemplateMessage, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, common.Errorf(common.Invalid, "quality rule template resource id is required")
	}
	template, err := scanQualityRuleTemplate(s.GetDB().QueryRowContext(ctx, `
		SELECT `+qualityRuleTemplateColumns+`
		FROM quality_rule_template
		WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read quality rule template")
	}
	return template, nil
}

// ListQualityRuleTemplates lists templates matching the filter, newest first.
func (s *Store) ListQualityRuleTemplates(ctx context.Context, find *FindQualityRuleTemplateMessage) ([]*QualityRuleTemplateMessage, error) {
	query, args := qualityRuleTemplateListQuery(find)
	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query quality rule templates")
	}
	defer rows.Close()

	var result []*QualityRuleTemplateMessage
	for rows.Next() {
		msg, err := scanQualityRuleTemplate(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan quality rule template")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for quality rule templates")
	}
	return result, nil
}

// UpdateQualityRuleTemplate writes the whole row of one template, identified by
// its public resource id, and returns the stored row. A template is a single
// definition with no history, so there is no revision to bump and no partial
// patch: the caller merges an update mask into the current message first.
func (s *Store) UpdateQualityRuleTemplate(ctx context.Context, template *QualityRuleTemplateMessage) (*QualityRuleTemplateMessage, error) {
	if err := validateQualityRuleTemplateIdentity(template); err != nil {
		return nil, err
	}
	config := template.Config
	if config == nil {
		config = &storepb.QualityRuleConfig{}
	}
	configJSON, err := marshalQualityProto(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal quality rule template config")
	}

	updated, err := scanQualityRuleTemplate(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_rule_template SET
			title = $1, description = $2, dimension = $3, rule_type = $4,
			config = $5, severity = $6, updated_by = $7, updated_at = NOW()
		WHERE resource_id = $8
		RETURNING `+qualityRuleTemplateColumns,
		strings.TrimSpace(template.Title),
		strings.TrimSpace(template.Description),
		template.Dimension,
		template.RuleType,
		configJSON,
		template.Severity,
		qualityRuleWriteInt32(template.UpdatedBy),
		strings.TrimSpace(template.ResourceID),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "quality rule template %q not found", template.ResourceID)
		}
		return nil, errors.Wrap(err, "failed to update quality rule template")
	}
	return updated, nil
}

// DeleteQualityRuleTemplate hard-deletes one template by its public resource id.
// A template carries no history, so nothing else references it; checks created
// from it were copied at creation time and are unaffected.
func (s *Store) DeleteQualityRuleTemplate(ctx context.Context, resourceID string) error {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return common.Errorf(common.Invalid, "quality rule template resource id is required")
	}
	result, err := s.GetDB().ExecContext(ctx, `DELETE FROM quality_rule_template WHERE resource_id = $1`, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to delete quality rule template")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to read the deleted quality rule template count")
	}
	if affected == 0 {
		return common.Errorf(common.NotFound, "quality rule template %q not found", resourceID)
	}
	return nil
}
