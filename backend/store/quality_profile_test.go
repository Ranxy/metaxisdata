package store

import (
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// columnProfileScannerStub stands in for one column_profile row, so the scan
// mapping is tested without a database.
type columnProfileScannerStub struct {
	id         int64
	tableGUID  string
	tableType  int16
	columnName string
	profile    []byte
	method     string
	sampleRows int64
	truncated  bool
	engine     string
	profiledAt time.Time
}

func (s columnProfileScannerStub) Scan(dest ...any) error {
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*string)) = s.tableGUID
	*(dest[2].(*int16)) = s.tableType
	*(dest[3].(*string)) = s.columnName
	*(dest[4].(*[]byte)) = s.profile
	*(dest[5].(*string)) = s.method
	*(dest[6].(*int64)) = s.sampleRows
	*(dest[7].(*bool)) = s.truncated
	*(dest[8].(*string)) = s.engine
	*(dest[9].(*time.Time)) = s.profiledAt
	return nil
}

// A profile is cached snapshot data keyed by (table, type, column): every filter
// must bind its value, and an omitted filter must not silently narrow or widen
// the set.
func TestColumnProfileWhereBindsEveryFilterValue(t *testing.T) {
	t.Parallel()

	where, args := columnProfileWhere(nil)
	require.Equal(t, "TRUE", where)
	require.Empty(t, args)

	tableGUID := "instance_1;db;public;orders"
	tableType := storepb.MetaType_TABLE
	before := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	where, args = columnProfileWhere(&FindColumnProfileMessage{
		TableGUID:      &tableGUID,
		TableType:      &tableType,
		ColumnNames:    []string{"email", "amount"},
		ProfiledBefore: &before,
	})
	require.Equal(t, "TRUE AND table_guid = $1 AND table_type = $2 AND column_name = ANY($3) AND profiled_at < $4", where)
	require.Len(t, args, 4)
	require.Equal(t, tableGUID, args[0])
	require.Equal(t, int32(storepb.MetaType_TABLE), args[1])
	require.Equal(t, pq.Array([]string{"email", "amount"}), args[2])
	require.Equal(t, before, args[3])

	// The filter values are arguments, never part of the statement text: a GUID
	// or column name cannot change the query's shape.
	for _, value := range []string{tableGUID, "email", "amount"} {
		require.NotContains(t, where, value)
	}
}

func TestColumnProfileQueryOrdersNewestAndPaginates(t *testing.T) {
	t.Parallel()

	limit, offset := 10, 20
	query, args := columnProfileQuery(&FindColumnProfileMessage{Limit: &limit, Offset: &offset})
	require.Contains(t, query, "FROM column_profile")
	require.Contains(t, query, "ORDER BY profiled_at DESC, id DESC")
	require.Contains(t, query, "LIMIT $1")
	require.Contains(t, query, "OFFSET $2")
	require.Equal(t, []any{10, 20}, args)
	requirePlaceholdersMatchArgs(t, query, args)

	// A filter before the pagination shifts the placeholders rather than
	// reusing one, which is the bug the numbering is asserted against.
	tableGUID := "g"
	query, args = columnProfileQuery(&FindColumnProfileMessage{TableGUID: &tableGUID, Limit: &limit})
	require.Contains(t, query, "table_guid = $1")
	require.Contains(t, query, "LIMIT $2")
	require.Equal(t, []any{"g", 10}, args)
	requirePlaceholdersMatchArgs(t, query, args)
}

func TestScanColumnProfileRoundTripsTheJSONBProfile(t *testing.T) {
	t.Parallel()

	profiledAt := time.Date(2026, time.October, 2, 8, 30, 0, 0, time.UTC)
	minLength, maxLength := int64(6), int64(42)
	profileJSON, err := protojson.Marshal(&storepb.ColumnProfile{
		TotalRows:     1200,
		NullCount:     3,
		DistinctCount: 900,
		MinLength:     &minLength,
		MaxLength:     &maxLength,
		TopValues:     []*storepb.ProfileValue{{Value: "a@b.c", Count: 40}},
		Patterns:      []string{"alpha", "mixed"},
	})
	require.NoError(t, err)

	message, err := scanColumnProfile(columnProfileScannerStub{
		id:         9,
		tableGUID:  "instance_1;db;public;orders",
		tableType:  int16(storepb.MetaType_TABLE),
		columnName: "email",
		profile:    profileJSON,
		method:     "sample",
		sampleRows: 1000,
		truncated:  true,
		engine:     "postgres-16",
		profiledAt: profiledAt,
	})
	require.NoError(t, err)
	require.Equal(t, int64(9), message.ID)
	require.Equal(t, storepb.MetaType_TABLE, message.TableType)
	require.Equal(t, "email", message.ColumnName)
	require.Equal(t, "sample", message.SampleMethod)
	require.Equal(t, int64(1000), message.SampleRows)
	require.True(t, message.Truncated)
	require.Equal(t, "postgres-16", message.EngineVersion)
	require.Equal(t, profiledAt, message.ProfiledAt)
	require.NotNil(t, message.Profile)
	require.Equal(t, int64(1200), message.Profile.GetTotalRows())
	require.Equal(t, int64(3), message.Profile.GetNullCount())
	require.Equal(t, int64(900), message.Profile.GetDistinctCount())
	require.Equal(t, int64(6), message.Profile.GetMinLength())
	require.Equal(t, int64(42), message.Profile.GetMaxLength())
	require.Len(t, message.Profile.GetTopValues(), 1)
	require.Equal(t, "a@b.c", message.Profile.GetTopValues()[0].GetValue())
	require.Equal(t, []string{"alpha", "mixed"}, message.Profile.GetPatterns())
}

// The profile column is JSONB NOT NULL DEFAULT '{}', so an empty object must read
// as an empty message rather than a nil pointer every caller has to guard.
func TestScanColumnProfileToleratesAnEmptyProfile(t *testing.T) {
	t.Parallel()

	message, err := scanColumnProfile(columnProfileScannerStub{
		tableType: int16(storepb.MetaType_VIEW),
		profile:   []byte(`{}`),
	})
	require.NoError(t, err)
	require.NotNil(t, message.Profile)
	require.Zero(t, message.Profile.GetTotalRows())
	require.Equal(t, storepb.MetaType_VIEW, message.TableType)
}

func TestScanColumnProfilePropagatesAMalformedProfile(t *testing.T) {
	t.Parallel()

	_, err := scanColumnProfile(columnProfileScannerStub{profile: []byte(`not json`)})
	require.Error(t, err)
}

func TestValidateColumnProfileRequiresTheIdentity(t *testing.T) {
	t.Parallel()

	valid := &ColumnProfileMessage{
		TableGUID:  "g",
		TableType:  storepb.MetaType_TABLE,
		ColumnName: "email",
	}

	require.NoError(t, validateColumnProfile(valid))
	require.Equal(t, common.Invalid, common.ErrorCode(validateColumnProfile(nil)))
	require.Equal(t, common.Invalid, common.ErrorCode(validateColumnProfile(&ColumnProfileMessage{
		TableType: storepb.MetaType_TABLE, ColumnName: "email",
	})))
	require.Equal(t, common.Invalid, common.ErrorCode(validateColumnProfile(&ColumnProfileMessage{
		TableGUID: "g", ColumnName: "email",
	})))
	require.Equal(t, common.Invalid, common.ErrorCode(validateColumnProfile(&ColumnProfileMessage{
		TableGUID: "g", TableType: storepb.MetaType_TABLE,
	})))
}

// The upsert must conflict on the identity index and replace every measured
// field, because a profile is a snapshot. A missing conflict target would append
// a row per profiling run and leave the unique index to reject the second one.
func TestUpsertColumnProfileQueryReplacesTheSnapshot(t *testing.T) {
	t.Parallel()

	require.Contains(t, upsertColumnProfileQuery, "INSERT INTO column_profile")
	require.Contains(t, upsertColumnProfileQuery, "ON CONFLICT (table_guid, table_type, column_name) DO UPDATE")
	for _, assignment := range []string{
		"profile = EXCLUDED.profile",
		"sample_method = EXCLUDED.sample_method",
		"sample_rows = EXCLUDED.sample_rows",
		"truncated = EXCLUDED.truncated",
		"engine_version = EXCLUDED.engine_version",
		"profiled_at = EXCLUDED.profiled_at",
	} {
		require.Contains(t, upsertColumnProfileQuery, assignment)
	}
	require.Contains(t, upsertColumnProfileQuery, "RETURNING id")

	require.Contains(t, deleteExpiredColumnProfilesQuery, "DELETE FROM column_profile")
	require.Contains(t, deleteExpiredColumnProfilesQuery, "WHERE profiled_at < $1")
}
