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

// qualityCodeTableColumns is the full column list of one code table, in the order
// scanQualityCodeTable consumes it. A get reads it; a list does not, because the
// codes payload can be large and a list only needs the size.
const qualityCodeTableColumns = `id, resource_id, title, description, codes,
	value_count, created_by, updated_by, created_at, updated_at`

// qualityCodeTableSummaryColumns is the list projection: everything except the
// codes payload, which value_count stands in for. Keeping it separate is what makes
// a 1000-row list read bounded rather than megabytes per row.
const qualityCodeTableSummaryColumns = `id, resource_id, title, description,
	value_count, created_by, updated_by, created_at, updated_at`

// QualityCodeTableMessage is the store representation of a row in
// quality_code_table: a workspace-defined set of accepted codes a standard-code
// check validates against.
type QualityCodeTableMessage struct {
	ID          int64
	ResourceID  string
	Title       string
	Description string
	// Values is the accepted code list, normalized by the caller (trimmed,
	// de-duplicated, sorted). The codes column stores it as a
	// QualityCodeTableValues JSONB payload.
	Values []string
	// ValueCount is the stored len(Values). It is kept in its own column so a list
	// read reports the size without parsing the payload.
	ValueCount int
	CreatedBy  *int32
	UpdatedBy  *int32
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// FindQualityCodeTableMessage is the query filter for code tables.
type FindQualityCodeTableMessage struct {
	ResourceID *string
	Limit      *int
	Offset     *int
	// SummaryOnly asks for the row without its codes payload (Values stays nil).
	// A caller that only proves existence or reports the size uses it.
	SummaryOnly bool
}

// UpdateQualityCodeTableMessage contains the mutable fields of a code table. Only
// non-nil fields change; Values replaces the whole list and its count.
type UpdateQualityCodeTableMessage struct {
	Title       *string
	Description *string
	Values      *[]string
	UpdatedBy   *int32
}

// qualityCodeTableScanner is the row shape scanned from either a *sql.Row or
// *sql.Rows. It exists so the mapping is testable without a database.
type qualityCodeTableScanner interface {
	Scan(dest ...any) error
}

// scanQualityCodeTable maps one quality_code_table row to the message. The codes
// column is a protojson payload of QualityCodeTableValues, so a row written by an
// older build still reads.
func scanQualityCodeTable(row qualityCodeTableScanner) (*QualityCodeTableMessage, error) {
	var (
		msg       QualityCodeTableMessage
		codesJSON []byte
		createdBy sql.NullInt32
		updatedBy sql.NullInt32
	)
	if err := row.Scan(
		&msg.ID,
		&msg.ResourceID,
		&msg.Title,
		&msg.Description,
		&codesJSON,
		&msg.ValueCount,
		&createdBy,
		&updatedBy,
		&msg.CreatedAt,
		&msg.UpdatedAt,
	); err != nil {
		return nil, err
	}
	values := &storepb.QualityCodeTableValues{}
	if err := unmarshalQualityProto(codesJSON, values); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal quality code table values")
	}
	msg.Values = append([]string(nil), values.GetValues()...)
	msg.CreatedBy = qualityRuleInt32(createdBy)
	msg.UpdatedBy = qualityRuleInt32(updatedBy)
	return &msg, nil
}

// scanQualityCodeTableSummary maps one quality_code_table list row: the same
// columns without the codes payload, in qualityCodeTableSummaryColumns order.
func scanQualityCodeTableSummary(row qualityCodeTableScanner) (*QualityCodeTableMessage, error) {
	var (
		msg       QualityCodeTableMessage
		createdBy sql.NullInt32
		updatedBy sql.NullInt32
	)
	scanErr := row.Scan(
		&msg.ID,
		&msg.ResourceID,
		&msg.Title,
		&msg.Description,
		&msg.ValueCount,
		&createdBy,
		&updatedBy,
		&msg.CreatedAt,
		&msg.UpdatedAt,
	)
	if scanErr != nil {
		return nil, scanErr
	}
	msg.CreatedBy = qualityRuleInt32(createdBy)
	msg.UpdatedBy = qualityRuleInt32(updatedBy)
	return &msg, nil
}

// buildQualityCodeTableWhere renders the filter as a SQL predicate plus its
// parameterized arguments. It is a pure function so the scoping contract is
// guard-tested without a live PostgreSQL. The newest-first order lives in
// ListQualityCodeTables.
func buildQualityCodeTableWhere(find *FindQualityCodeTableMessage) (string, []any) {
	if find == nil {
		find = &FindQualityCodeTableMessage{}
	}
	where := []string{"TRUE"}
	args := []any{}
	if find.ResourceID != nil && strings.TrimSpace(*find.ResourceID) != "" {
		args = append(args, strings.TrimSpace(*find.ResourceID))
		where = append(where, fmt.Sprintf("resource_id = $%d", len(args)))
	}
	return strings.Join(where, " AND "), args
}

// CreateQualityCodeTable inserts one code table. The caller provides the public
// resource id (it names the API resource and is never reused) and the already
// normalized values; value_count is derived from them so the two cannot
// disagree.
func (s *Store) CreateQualityCodeTable(ctx context.Context, table *QualityCodeTableMessage) (*QualityCodeTableMessage, error) {
	if table == nil {
		return nil, common.Errorf(common.Invalid, "a quality code table is required")
	}
	resourceID := strings.TrimSpace(table.ResourceID)
	if resourceID == "" {
		return nil, common.Errorf(common.Invalid, "quality code table resource id is required")
	}
	codesJSON, err := marshalQualityProto(&storepb.QualityCodeTableValues{Values: table.Values})
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal quality code table values")
	}
	created, err := scanQualityCodeTable(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_code_table (
			resource_id, title, description, codes, value_count, created_by,
			updated_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+qualityCodeTableColumns,
		resourceID,
		strings.TrimSpace(table.Title),
		strings.TrimSpace(table.Description),
		codesJSON,
		len(table.Values),
		qualityRuleWriteInt32(table.CreatedBy),
		qualityRuleWriteInt32(table.UpdatedBy),
	))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "quality code table %q already exists", resourceID)
		}
		return nil, errors.Wrap(err, "failed to create quality code table")
	}
	return created, nil
}

// GetQualityCodeTable returns the code table matching the filter, or (nil, nil)
// when none matches. The read carries the codes payload unless the filter asks for
// a summary, which a caller that only needs existence or the size uses.
func (s *Store) GetQualityCodeTable(ctx context.Context, find *FindQualityCodeTableMessage) (*QualityCodeTableMessage, error) {
	if find == nil {
		return nil, common.Errorf(common.Invalid, "a quality code table filter is required")
	}
	where, args := buildQualityCodeTableWhere(find)
	columns := qualityCodeTableColumns
	scan := scanQualityCodeTable
	if find.SummaryOnly {
		columns = qualityCodeTableSummaryColumns
		scan = scanQualityCodeTableSummary
	}
	query := `SELECT ` + columns + ` FROM quality_code_table WHERE ` + where +
		` ORDER BY created_at DESC, id DESC LIMIT 2`
	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query quality code table")
	}
	defer rows.Close()
	var result []*QualityCodeTableMessage
	for rows.Next() {
		msg, scanErr := scan(rows)
		if scanErr != nil {
			return nil, errors.Wrap(scanErr, "failed to scan quality code table")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for quality code table")
	}
	if len(result) == 0 {
		return nil, nil
	}
	if len(result) > 1 {
		return nil, common.Errorf(common.Internal, "found %d quality code tables with the same filter", len(result))
	}
	return result[0], nil
}

// ListQualityCodeTables lists code tables matching the filter, newest first. It is
// the summary projection: the codes payload is represented by value_count, so a page
// of large tables does not transfer their contents.
func (s *Store) ListQualityCodeTables(ctx context.Context, find *FindQualityCodeTableMessage) ([]*QualityCodeTableMessage, error) {
	if find == nil {
		find = &FindQualityCodeTableMessage{}
	}
	where, args := buildQualityCodeTableWhere(find)
	query := `SELECT ` + qualityCodeTableSummaryColumns + ` FROM quality_code_table WHERE ` + where +
		` ORDER BY created_at DESC, id DESC`
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
		return nil, errors.Wrap(err, "failed to query quality code tables")
	}
	defer rows.Close()

	var result []*QualityCodeTableMessage
	for rows.Next() {
		msg, err := scanQualityCodeTableSummary(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan quality code table")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for quality code tables")
	}
	return result, nil
}

// UpdateQualityCodeTable patches one code table by its public resource id. Only
// the fields the patch sets change; replacing Values also rewrites value_count in
// the same statement so a list read cannot report a stale size.
func (s *Store) UpdateQualityCodeTable(ctx context.Context, resourceID string, patch *UpdateQualityCodeTableMessage) (*QualityCodeTableMessage, error) {
	if patch == nil {
		return nil, common.Errorf(common.Invalid, "a quality code table update patch is required")
	}
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, common.Errorf(common.Invalid, "quality code table resource id is required")
	}
	sets := []string{"updated_at = NOW()"}
	args := []any{resourceID}
	if v := patch.Title; v != nil {
		args = append(args, strings.TrimSpace(*v))
		sets = append(sets, fmt.Sprintf("title = $%d", len(args)))
	}
	if v := patch.Description; v != nil {
		args = append(args, strings.TrimSpace(*v))
		sets = append(sets, fmt.Sprintf("description = $%d", len(args)))
	}
	if v := patch.Values; v != nil {
		codesJSON, err := marshalQualityProto(&storepb.QualityCodeTableValues{Values: *v})
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal quality code table values")
		}
		args = append(args, codesJSON)
		sets = append(sets, fmt.Sprintf("codes = $%d", len(args)))
		args = append(args, len(*v))
		sets = append(sets, fmt.Sprintf("value_count = $%d", len(args)))
	}
	if v := patch.UpdatedBy; v != nil {
		args = append(args, qualityRuleWriteInt32(v))
		sets = append(sets, fmt.Sprintf("updated_by = $%d", len(args)))
	}
	updated, err := scanQualityCodeTable(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_code_table
		SET `+strings.Join(sets, ", ")+`
		WHERE resource_id = $1
		RETURNING `+qualityCodeTableColumns, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "quality code table %q not found", resourceID)
		}
		return nil, errors.Wrap(err, "failed to update quality code table")
	}
	return updated, nil
}

// DeleteQualityCodeTable hard-deletes one code table. A caller refuses the delete
// while QualityCodeTableReferences reports a user, because those rules and
// templates would be left pointing at a table that no longer exists.
func (s *Store) DeleteQualityCodeTable(ctx context.Context, resourceID string) error {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return common.Errorf(common.Invalid, "quality code table resource id is required")
	}
	result, err := s.GetDB().ExecContext(ctx,
		`DELETE FROM quality_code_table WHERE resource_id = $1`, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to delete quality code table")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to count the deleted quality code tables")
	}
	if affected == 0 {
		return common.Errorf(common.NotFound, "quality code table %q not found", resourceID)
	}
	return nil
}

// qualityCodeTableReferenceQuery counts the live rules and the templates that
// name one code table. A rule's config stores the reference at
// standard_code.code_table, which protojson writes as standardCode.codeTable.
// Archived rules carry deleted = TRUE and are excluded: archiving is the delete
// path, and an archived rule can never execute again.
const qualityCodeTableReferenceQuery = `
	SELECT
		(SELECT count(*) FROM quality_rule
			WHERE deleted = FALSE
				AND config->'standardCode'->>'codeTable' = $1),
		(SELECT count(*) FROM quality_rule_template
			WHERE config->'standardCode'->>'codeTable' = $1)`

// QualityCodeTableReferences reports how many rules and how many rule templates
// still name resourceName. The delete path refuses while either count is positive,
// because those checks would silently break instead of reporting a verdict.
func (s *Store) QualityCodeTableReferences(ctx context.Context, resourceName string) (rules int64, templates int64, err error) {
	resourceName = strings.TrimSpace(resourceName)
	if resourceName == "" {
		return 0, 0, common.Errorf(common.Invalid, "a code table resource name is required")
	}
	scanErr := s.GetDB().QueryRowContext(ctx, qualityCodeTableReferenceQuery, resourceName).
		Scan(&rules, &templates)
	if scanErr != nil {
		return 0, 0, errors.Wrap(scanErr, "failed to count quality code table references")
	}
	return rules, templates, nil
}
