package store

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityCodeTableRowStub produces a quality_code_table row for
// scanQualityCodeTable without a database. The destination order is
// qualityCodeTableColumns.
type qualityCodeTableRowStub struct {
	id          int64
	resourceID  string
	title       string
	description string
	codes       []byte
	valueCount  int
	createdBy   sql.NullInt32
	updatedBy   sql.NullInt32
	createdAt   time.Time
	updatedAt   time.Time
}

func (s qualityCodeTableRowStub) Scan(dest ...any) error {
	if len(dest) != 10 {
		return fmt.Errorf("quality code table stub got %d destinations, want 10", len(dest))
	}
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*string)) = s.resourceID
	*(dest[2].(*string)) = s.title
	*(dest[3].(*string)) = s.description
	*(dest[4].(*[]byte)) = s.codes
	*(dest[5].(*int)) = s.valueCount
	*(dest[6].(*sql.NullInt32)) = s.createdBy
	*(dest[7].(*sql.NullInt32)) = s.updatedBy
	*(dest[8].(*time.Time)) = s.createdAt
	*(dest[9].(*time.Time)) = s.updatedAt
	return nil
}

// The codes payload is a QualityCodeTableValues JSONB object, and the nullable
// principal columns must surface as nil rather than as principal zero.
func TestScanQualityCodeTableReadsValuesAndNullableColumns(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.November, 2, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	codesJSON, err := marshalQualityProto(&storepb.QualityCodeTableValues{Values: []string{"A1", "B2"}})
	require.NoError(t, err)

	msg, err := scanQualityCodeTable(qualityCodeTableRowStub{
		id:          7,
		resourceID:  "code-table-1",
		title:       "division codes",
		description: "the customer's current division list",
		codes:       codesJSON,
		valueCount:  2,
		createdBy:   sql.NullInt32{Int32: 3, Valid: true},
		updatedBy:   sql.NullInt32{},
		createdAt:   createdAt,
		updatedAt:   updatedAt,
	})
	require.NoError(t, err)
	require.Equal(t, int64(7), msg.ID)
	require.Equal(t, "code-table-1", msg.ResourceID)
	require.Equal(t, "division codes", msg.Title)
	require.Equal(t, "the customer's current division list", msg.Description)
	require.Equal(t, []string{"A1", "B2"}, msg.Values)
	require.Equal(t, 2, msg.ValueCount)
	require.Equal(t, proto.Int32(3), msg.CreatedBy)
	require.Nil(t, msg.UpdatedBy)
	require.Equal(t, createdAt, msg.CreatedAt)
	require.Equal(t, updatedAt, msg.UpdatedAt)
}

// A codes column written before the payload carried any values reads as an
// empty list rather than failing the whole page.
func TestScanQualityCodeTableReadsAnEmptyPayload(t *testing.T) {
	t.Parallel()

	for _, payload := range [][]byte{nil, []byte("{}"), []byte("null")} {
		msg, err := scanQualityCodeTable(qualityCodeTableRowStub{codes: payload})
		require.NoError(t, err)
		require.Empty(t, msg.Values)
		require.Zero(t, msg.ValueCount)
	}
}

func TestBuildQualityCodeTableWhereScopesByResourceID(t *testing.T) {
	t.Parallel()

	where, args := buildQualityCodeTableWhere(&FindQualityCodeTableMessage{ResourceID: proto.String(" code-table-1 ")})
	require.Equal(t, "TRUE AND resource_id = $1", where)
	require.Equal(t, []any{"code-table-1"}, args)

	where, args = buildQualityCodeTableWhere(nil)
	require.Equal(t, "TRUE", where)
	require.Empty(t, args)

	empty := ""
	where, args = buildQualityCodeTableWhere(&FindQualityCodeTableMessage{ResourceID: &empty})
	require.Equal(t, "TRUE", where, "an empty resource id must not match every row")
	require.Empty(t, args)
}

// The reference check is what keeps DeleteQualityCodeTable from orphaning a live
// check. It has to read the standard-code reference under the camelCase JSONB key
// the store writes, count both users (rules and templates), and ignore archived
// rules, which are the rows the rule delete path leaves behind.
func TestQualityCodeTableReferenceQueryShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityCodeTableReferenceQuery, "FROM quality_rule")
	require.Contains(t, qualityCodeTableReferenceQuery, "FROM quality_rule_template")
	require.Contains(t, qualityCodeTableReferenceQuery, "config->'standardCode'->>'codeTable' = $1")
	require.Contains(t, qualityCodeTableReferenceQuery, "deleted = FALSE")
}
