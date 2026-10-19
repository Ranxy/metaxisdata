package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// columnSemanticLabelScannerStub stands in for one column_semantic_label row.
type columnSemanticLabelScannerStub struct {
	id         int64
	tableGUID  string
	tableType  int16
	columnName string
	label      int16
	source     int16
	status     int16
	confidence float64
	evidence   []byte
	acceptedBy sql.NullInt32
	acceptedAt sql.NullTime
	createdAt  time.Time
	updatedAt  time.Time
}

func (s columnSemanticLabelScannerStub) Scan(dest ...any) error {
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*string)) = s.tableGUID
	*(dest[2].(*int16)) = s.tableType
	*(dest[3].(*string)) = s.columnName
	*(dest[4].(*int16)) = s.label
	*(dest[5].(*int16)) = s.source
	*(dest[6].(*int16)) = s.status
	*(dest[7].(*float64)) = s.confidence
	*(dest[8].(*[]byte)) = s.evidence
	*(dest[9].(*sql.NullInt32)) = s.acceptedBy
	*(dest[10].(*sql.NullTime)) = s.acceptedAt
	*(dest[11].(*time.Time)) = s.createdAt
	*(dest[12].(*time.Time)) = s.updatedAt
	return nil
}

// The label table is keyed by (table, type, column) and is product data, so every
// filter has to bind its value and the identity filters have to keep working
// together.
func TestColumnSemanticLabelWhereBindsEveryFilterValue(t *testing.T) {
	t.Parallel()

	where, args := columnSemanticLabelWhere(nil)
	require.Equal(t, "TRUE", where)
	require.Empty(t, args)

	tableGUID := "instance_1;db;public;customers"
	tableType := storepb.MetaType_TABLE
	label := storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL
	where, args = columnSemanticLabelWhere(&FindColumnSemanticLabelMessage{
		TableGUID:   &tableGUID,
		TableType:   &tableType,
		ColumnNames: []string{"email", "backup_email"},
		Label:       &label,
		Statuses: []storepb.QualityLabelStatus{
			storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED,
			storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_SUGGESTED,
		},
	})
	require.Equal(t,
		"TRUE AND table_guid = $1 AND table_type = $2 AND column_name = ANY($3) AND label = $4 AND status = ANY($5::int[])",
		where)
	require.Len(t, args, 5)
	require.Equal(t, tableGUID, args[0])
	require.Equal(t, int32(storepb.MetaType_TABLE), args[1])
	require.Equal(t, pq.Array([]string{"email", "backup_email"}), args[2])
	require.Equal(t, int32(storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL), args[3])
	// The status list is compared as integers, so a future enum renumbering is a
	// data migration rather than a silent mismatch between Go and the column.
	require.Equal(t, pq.Array([]int32{
		int32(storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED),
		int32(storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_SUGGESTED),
	}), args[4])

	for _, value := range []string{tableGUID, "email", "backup_email"} {
		require.NotContains(t, where, value)
	}
}

func TestColumnSemanticLabelQueryOrdersBySchemaPosition(t *testing.T) {
	t.Parallel()

	limit, offset := 25, 50
	query, args := columnSemanticLabelQuery(&FindColumnSemanticLabelMessage{Limit: &limit, Offset: &offset})
	require.Contains(t, query, "FROM column_semantic_label")
	require.Contains(t, query, "ORDER BY table_guid ASC, column_name ASC, id ASC")
	require.Contains(t, query, "LIMIT $1")
	require.Contains(t, query, "OFFSET $2")
	requirePlaceholdersMatchArgs(t, query, args)
	require.Equal(t, []any{25, 50}, args)
}

func TestScanColumnSemanticLabelReadsAcceptanceColumns(t *testing.T) {
	t.Parallel()

	acceptedAt := time.Date(2026, time.October, 3, 10, 0, 0, 0, time.UTC)
	createdAt := acceptedAt.Add(-time.Hour)
	updatedAt := acceptedAt.Add(time.Minute)
	evidenceJSON, err := protojson.Marshal(&storepb.SemanticLabelEvidence{
		Columns:   []string{"email"},
		Metrics:   []string{"distinct=900"},
		Lineage:   []string{"join key of orders"},
		Rationale: "values look like addresses",
	})
	require.NoError(t, err)

	message, err := scanColumnSemanticLabel(columnSemanticLabelScannerStub{
		id:         7,
		tableGUID:  "instance_1;db;public;customers",
		tableType:  int16(storepb.MetaType_TABLE),
		columnName: "email",
		label:      int16(storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL),
		source:     int16(storepb.QualityLabelSource_QUALITY_LABEL_SOURCE_LLM),
		status:     int16(storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED),
		confidence: 0.92,
		evidence:   evidenceJSON,
		acceptedBy: sql.NullInt32{Int32: 11, Valid: true},
		acceptedAt: sql.NullTime{Time: acceptedAt, Valid: true},
		createdAt:  createdAt,
		updatedAt:  updatedAt,
	})
	require.NoError(t, err)
	require.Equal(t, int64(7), message.ID)
	require.Equal(t, storepb.MetaType_TABLE, message.TableType)
	require.Equal(t, storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL, message.Label)
	require.Equal(t, storepb.QualityLabelSource_QUALITY_LABEL_SOURCE_LLM, message.Source)
	require.Equal(t, storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED, message.Status)
	require.InDelta(t, 0.92, message.Confidence, 1e-9)
	require.NotNil(t, message.Evidence)
	require.Equal(t, []string{"email"}, message.Evidence.GetColumns())
	require.Equal(t, "values look like addresses", message.Evidence.GetRationale())
	require.NotNil(t, message.AcceptedBy)
	require.Equal(t, int32(11), *message.AcceptedBy)
	require.NotNil(t, message.AcceptedAt)
	require.Equal(t, acceptedAt, *message.AcceptedAt)
	require.Equal(t, createdAt, message.CreatedAt)
	require.Equal(t, updatedAt, message.UpdatedAt)
}

// A label that was never accepted has NULL acceptance columns; they must read as
// nil pointers, which is distinct from "accepted at the zero time".
func TestScanColumnSemanticLabelLeavesUnacceptedColumnsNil(t *testing.T) {
	t.Parallel()

	message, err := scanColumnSemanticLabel(columnSemanticLabelScannerStub{
		tableType:  int16(storepb.MetaType_VIEW),
		columnName: "amount",
		label:      int16(storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_AMOUNT),
		source:     int16(storepb.QualityLabelSource_QUALITY_LABEL_SOURCE_MANUAL),
		status:     int16(storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_SUGGESTED),
		evidence:   []byte(`{}`),
	})
	require.NoError(t, err)
	require.Nil(t, message.AcceptedBy)
	require.Nil(t, message.AcceptedAt)
	require.NotNil(t, message.Evidence)
	require.Equal(t, storepb.MetaType_VIEW, message.TableType)
	require.Equal(t, storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_AMOUNT, message.Label)
}

func TestValidateColumnSemanticLabelRequiresIdentityAndLabel(t *testing.T) {
	t.Parallel()

	valid := &ColumnSemanticLabelMessage{
		TableGUID:  "g",
		TableType:  storepb.MetaType_TABLE,
		ColumnName: "email",
		Label:      storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL,
		Source:     storepb.QualityLabelSource_QUALITY_LABEL_SOURCE_MANUAL,
	}
	require.NoError(t, validateColumnSemanticLabel(valid))
	require.Equal(t, common.Invalid, common.ErrorCode(validateColumnSemanticLabel(nil)))

	broken := *valid
	broken.TableGUID = " "
	require.Equal(t, common.Invalid, common.ErrorCode(validateColumnSemanticLabel(&broken)))

	broken = *valid
	broken.TableType = storepb.MetaType_UNSPECIFIED
	require.Equal(t, common.Invalid, common.ErrorCode(validateColumnSemanticLabel(&broken)))

	broken = *valid
	broken.ColumnName = ""
	require.Equal(t, common.Invalid, common.ErrorCode(validateColumnSemanticLabel(&broken)))

	broken = *valid
	broken.Label = storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNSPECIFIED
	require.Equal(t, common.Invalid, common.ErrorCode(validateColumnSemanticLabel(&broken)))

	broken = *valid
	broken.Source = storepb.QualityLabelSource_QUALITY_LABEL_SOURCE_UNSPECIFIED
	require.Equal(t, common.Invalid, common.ErrorCode(validateColumnSemanticLabel(&broken)))
}

// This table holds labels that are in effect; a proposal belongs in
// quality_suggestion until a reviewer decides it, so an unset status is an
// accepted label rather than a suggestion.
func TestColumnSemanticLabelStatusDefaultsToAccepted(t *testing.T) {
	t.Parallel()

	require.Equal(t, storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED,
		columnSemanticLabelStatus(storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_UNSPECIFIED))
	require.Equal(t, storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_SUGGESTED,
		columnSemanticLabelStatus(storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_SUGGESTED))
	require.Equal(t, storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_REJECTED,
		columnSemanticLabelStatus(storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_REJECTED))
}

// The upsert replaces the label's content but keeps created_at, which records
// when the label was first asserted; updated_at moves. The delete is the only
// explicit way to clear a label.
func TestUpsertColumnSemanticLabelQueryKeepsFirstAssertionTime(t *testing.T) {
	t.Parallel()

	require.Contains(t, upsertColumnSemanticLabelQuery, "INSERT INTO column_semantic_label")
	require.Contains(t, upsertColumnSemanticLabelQuery, "ON CONFLICT (table_guid, table_type, column_name) DO UPDATE")
	for _, assignment := range []string{
		"label = EXCLUDED.label",
		"source = EXCLUDED.source",
		"status = EXCLUDED.status",
		"confidence = EXCLUDED.confidence",
		"evidence = EXCLUDED.evidence",
		"accepted_by = EXCLUDED.accepted_by",
		"accepted_at = EXCLUDED.accepted_at",
		"updated_at = EXCLUDED.updated_at",
	} {
		require.Contains(t, upsertColumnSemanticLabelQuery, assignment)
	}
	require.NotContains(t, upsertColumnSemanticLabelQuery, "created_at = EXCLUDED.created_at",
		"created_at records the first assertion and must survive an upsert")
	require.Contains(t, upsertColumnSemanticLabelQuery, "RETURNING id, created_at, updated_at")

	require.Contains(t, deleteColumnSemanticLabelQuery, "DELETE FROM column_semantic_label")
	require.Contains(t, deleteColumnSemanticLabelQuery,
		"WHERE table_guid = $1 AND table_type = $2 AND column_name = $3")
}
