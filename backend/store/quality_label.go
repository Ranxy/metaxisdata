package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// columnSemanticLabelColumns lists the columns every read of
// column_semantic_label returns, in the order scanColumnSemanticLabel expects
// them.
const columnSemanticLabelColumns = `id, table_guid, table_type, column_name, label, source, status,
	confidence, evidence, accepted_by, accepted_at, created_at, updated_at`

// upsertColumnSemanticLabelQuery writes one column's semantic label. The table
// holds at most one label per column, so the conflict target is the unique index
// over the column's identity. created_at is deliberately not overwritten: it is
// when the label was first asserted, while updated_at is when it last changed.
const upsertColumnSemanticLabelQuery = `
	INSERT INTO column_semantic_label (
		table_guid, table_type, column_name, label, source, status,
		confidence, evidence, accepted_by, accepted_at, created_at, updated_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	ON CONFLICT (table_guid, table_type, column_name) DO UPDATE SET
		label = EXCLUDED.label,
		source = EXCLUDED.source,
		status = EXCLUDED.status,
		confidence = EXCLUDED.confidence,
		evidence = EXCLUDED.evidence,
		accepted_by = EXCLUDED.accepted_by,
		accepted_at = EXCLUDED.accepted_at,
		updated_at = EXCLUDED.updated_at
	RETURNING id, created_at, updated_at`

// deleteColumnSemanticLabelQuery removes one column's label. It is the explicit
// "clear this label" path: a label is product data, so it is never deleted as a
// side effect of a schema sync.
const deleteColumnSemanticLabelQuery = `
	DELETE FROM column_semantic_label
	WHERE table_guid = $1 AND table_type = $2 AND column_name = $3`

// ColumnSemanticLabelMessage is one row of column_semantic_label: the semantic
// type a column is asserted to hold, who asserted it, and the evidence behind it.
// It is keyed by the table's GUID plus the column name because a view or a
// materialized view has no COLUMN registry row to key on.
type ColumnSemanticLabelMessage struct {
	ID         int64
	TableGUID  string
	TableType  storepb.MetaType
	ColumnName string
	Label      storepb.QualitySemanticType
	Source     storepb.QualityLabelSource
	Status     storepb.QualityLabelStatus
	Confidence float64
	// Evidence cites the inputs the label was derived from; never nil on a row
	// that was read.
	Evidence   *storepb.SemanticLabelEvidence
	AcceptedBy *int32
	AcceptedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// FindColumnSemanticLabelMessage filters semantic labels. Every field is
// optional.
type FindColumnSemanticLabelMessage struct {
	TableGUID   *string
	TableType   *storepb.MetaType
	ColumnNames []string
	Label       *storepb.QualitySemanticType
	Statuses    []storepb.QualityLabelStatus
	Limit       *int
	Offset      *int
}

// columnSemanticLabelWhere builds the predicate and positional arguments for a
// find. Every value is bound and the compared columns are the table's own, so a
// caller cannot change the statement's shape.
func columnSemanticLabelWhere(find *FindColumnSemanticLabelMessage) (string, []any) {
	where := []string{"TRUE"}
	args := []any{}
	if find == nil {
		return strings.Join(where, " AND "), args
	}
	if find.TableGUID != nil {
		args = append(args, *find.TableGUID)
		where = append(where, fmt.Sprintf("table_guid = $%d", len(args)))
	}
	if find.TableType != nil {
		args = append(args, int32(*find.TableType))
		where = append(where, fmt.Sprintf("table_type = $%d", len(args)))
	}
	if len(find.ColumnNames) > 0 {
		args = append(args, pq.Array(find.ColumnNames))
		where = append(where, fmt.Sprintf("column_name = ANY($%d)", len(args)))
	}
	if find.Label != nil {
		args = append(args, int32(*find.Label))
		where = append(where, fmt.Sprintf("label = $%d", len(args)))
	}
	if len(find.Statuses) > 0 {
		statuses := make([]int32, 0, len(find.Statuses))
		for _, status := range find.Statuses {
			statuses = append(statuses, int32(status))
		}
		args = append(args, pq.Array(statuses))
		where = append(where, fmt.Sprintf("status = ANY($%d::int[])", len(args)))
	}
	return strings.Join(where, " AND "), args
}

// columnSemanticLabelQuery assembles one filtered, ordered read of labels.
func columnSemanticLabelQuery(find *FindColumnSemanticLabelMessage) (string, []any) {
	where, args := columnSemanticLabelWhere(find)
	query := `SELECT ` + columnSemanticLabelColumns + ` FROM column_semantic_label WHERE ` + where +
		` ORDER BY table_guid ASC, column_name ASC, id ASC`
	if find != nil && find.Limit != nil {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, *find.Limit)
	}
	if find != nil && find.Offset != nil {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, *find.Offset)
	}
	return query, args
}

// scanColumnSemanticLabel maps one column_semantic_label row. The nullable
// acceptance columns stay nil pointers so "never accepted" is distinct from
// "accepted at the zero time".
func scanColumnSemanticLabel(row rowScanner) (*ColumnSemanticLabelMessage, error) {
	var (
		message      ColumnSemanticLabelMessage
		tableType    int16
		label        int16
		source       int16
		status       int16
		evidenceJSON []byte
		acceptedBy   sql.NullInt32
		acceptedAt   sql.NullTime
	)
	if err := row.Scan(
		&message.ID, &message.TableGUID, &tableType, &message.ColumnName, &label, &source, &status,
		&message.Confidence, &evidenceJSON, &acceptedBy, &acceptedAt, &message.CreatedAt, &message.UpdatedAt,
	); err != nil {
		return nil, err
	}
	message.TableType = storepb.MetaType(tableType)
	message.Label = storepb.QualitySemanticType(label)
	message.Source = storepb.QualityLabelSource(source)
	message.Status = storepb.QualityLabelStatus(status)
	evidence := &storepb.SemanticLabelEvidence{}
	if len(evidenceJSON) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(evidenceJSON, evidence); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal semantic label evidence")
		}
	}
	message.Evidence = evidence
	if acceptedBy.Valid {
		value := acceptedBy.Int32
		message.AcceptedBy = &value
	}
	if acceptedAt.Valid {
		value := acceptedAt.Time
		message.AcceptedAt = &value
	}
	return &message, nil
}

// validateColumnSemanticLabel checks what a label row cannot be stored without.
// The status defaults to ACCEPTED because this table holds labels that are in
// effect: a pending proposal belongs in quality_suggestion until a reviewer
// decides it.
func validateColumnSemanticLabel(message *ColumnSemanticLabelMessage) error {
	if message == nil {
		return common.Errorf(common.Invalid, "column semantic label is required")
	}
	if strings.TrimSpace(message.TableGUID) == "" {
		return common.Errorf(common.Invalid, "column semantic label table guid is required")
	}
	if message.TableType == storepb.MetaType_UNSPECIFIED {
		return common.Errorf(common.Invalid, "column semantic label table type is required")
	}
	if strings.TrimSpace(message.ColumnName) == "" {
		return common.Errorf(common.Invalid, "column semantic label column name is required")
	}
	if message.Label == storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNSPECIFIED {
		return common.Errorf(common.Invalid, "column semantic label label is required")
	}
	if message.Source == storepb.QualityLabelSource_QUALITY_LABEL_SOURCE_UNSPECIFIED {
		return common.Errorf(common.Invalid, "column semantic label source is required")
	}
	return nil
}

// columnSemanticLabelStatus resolves the status a label is stored with. This
// table holds labels that are in effect, so an unset status reads as ACCEPTED; a
// proposal a reviewer has not decided belongs in quality_suggestion instead.
func columnSemanticLabelStatus(status storepb.QualityLabelStatus) storepb.QualityLabelStatus {
	if status == storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_UNSPECIFIED {
		return storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED
	}
	return status
}

// UpsertColumnSemanticLabel writes one column's label and returns it with the
// timestamps the store assigned. Overwriting a manually accepted label is the
// caller's decision: the store is the faithful writer, and the explicit "never
// let an LLM overwrite a manual label" rule lives in the service that decides
// whether to call this at all.
func (s *Store) UpsertColumnSemanticLabel(ctx context.Context, message *ColumnSemanticLabelMessage) (*ColumnSemanticLabelMessage, error) {
	if err := validateColumnSemanticLabel(message); err != nil {
		return nil, err
	}

	status := columnSemanticLabelStatus(message.Status)

	evidence := message.Evidence
	if evidence == nil {
		evidence = &storepb.SemanticLabelEvidence{}
	}
	evidenceJSON, err := protojson.Marshal(evidence)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal semantic label evidence")
	}

	now := time.Now().UTC()

	var (
		id        int64
		createdAt time.Time
		updatedAt time.Time
	)
	if err := s.GetDB().QueryRowContext(ctx, upsertColumnSemanticLabelQuery,
		message.TableGUID, int32(message.TableType), message.ColumnName, int32(message.Label),
		int32(message.Source), int32(status), message.Confidence, evidenceJSON,
		message.AcceptedBy, message.AcceptedAt, now, now,
	).Scan(&id, &createdAt, &updatedAt); err != nil {
		return nil, errors.Wrap(err, "failed to upsert a column semantic label")
	}

	stored := *message
	stored.ID = id
	stored.Status = status
	stored.Evidence = evidence
	stored.CreatedAt = createdAt
	stored.UpdatedAt = updatedAt
	return &stored, nil
}

// GetColumnSemanticLabel returns one column's label, or (nil, nil) when the
// column has none.
func (s *Store) GetColumnSemanticLabel(ctx context.Context, tableGUID string, tableType storepb.MetaType, columnName string) (*ColumnSemanticLabelMessage, error) {
	if strings.TrimSpace(tableGUID) == "" || strings.TrimSpace(columnName) == "" {
		return nil, common.Errorf(common.Invalid, "column semantic label table guid and column name are required")
	}
	list, err := s.ListColumnSemanticLabels(ctx, &FindColumnSemanticLabelMessage{
		TableGUID:   &tableGUID,
		TableType:   &tableType,
		ColumnNames: []string{columnName},
	})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, common.Errorf(common.Internal, "found %d semantic labels for one column, expect 1", len(list))
	}
	return list[0], nil
}

// ListColumnSemanticLabels lists labels matching the filter, ordered by table and
// column so a table's labels render in schema order.
func (s *Store) ListColumnSemanticLabels(ctx context.Context, find *FindColumnSemanticLabelMessage) ([]*ColumnSemanticLabelMessage, error) {
	db := s.GetDB()

	query, args := columnSemanticLabelQuery(find)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query column semantic labels")
	}
	defer rows.Close()

	var labels []*ColumnSemanticLabelMessage
	for rows.Next() {
		label, err := scanColumnSemanticLabel(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a column semantic label")
		}
		labels = append(labels, label)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate column semantic labels")
	}
	return labels, nil
}

// DeleteColumnSemanticLabel removes one column's label and reports whether a row
// was there to remove.
func (s *Store) DeleteColumnSemanticLabel(ctx context.Context, tableGUID string, tableType storepb.MetaType, columnName string) (bool, error) {
	if strings.TrimSpace(tableGUID) == "" || strings.TrimSpace(columnName) == "" {
		return false, common.Errorf(common.Invalid, "column semantic label table guid and column name are required")
	}
	result, err := s.GetDB().ExecContext(ctx, deleteColumnSemanticLabelQuery, tableGUID, int32(tableType), columnName)
	if err != nil {
		return false, errors.Wrap(err, "failed to delete a column semantic label")
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count deleted column semantic labels")
	}
	return deleted > 0, nil
}
