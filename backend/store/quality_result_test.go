package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type qualityRuleResultScannerStub struct {
	id                int64
	runID             int64
	ruleID            int64
	ruleRevision      int32
	ruleEngineVersion string
	targetGUID        string
	targetType        storepb.MetaType
	status            storepb.QualityStatus
	observedValue     sql.NullFloat64
	observedKind      storepb.QualityObservedKind
	thresholds        []byte
	failedRows        int64
	totalRows         int64
	sampled           bool
	sampleRows        int64
	truncated         bool
	sample            []byte
	executedSQL       string
	errorCode         string
	errorMessage      string
	durationMS        int32
	startedAt         time.Time
	finishedAt        time.Time
}

func (s qualityRuleResultScannerStub) Scan(dest ...any) error {
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*int64)) = s.runID
	*(dest[2].(*int64)) = s.ruleID
	*(dest[3].(*int32)) = s.ruleRevision
	*(dest[4].(*string)) = s.ruleEngineVersion
	*(dest[5].(*string)) = s.targetGUID
	*(dest[6].(*storepb.MetaType)) = s.targetType
	*(dest[7].(*storepb.QualityStatus)) = s.status
	*(dest[8].(*sql.NullFloat64)) = s.observedValue
	*(dest[9].(*storepb.QualityObservedKind)) = s.observedKind
	*(dest[10].(*[]byte)) = s.thresholds
	*(dest[11].(*int64)) = s.failedRows
	*(dest[12].(*int64)) = s.totalRows
	*(dest[13].(*bool)) = s.sampled
	*(dest[14].(*int64)) = s.sampleRows
	*(dest[15].(*bool)) = s.truncated
	*(dest[16].(*[]byte)) = s.sample
	*(dest[17].(*string)) = s.executedSQL
	*(dest[18].(*string)) = s.errorCode
	*(dest[19].(*string)) = s.errorMessage
	*(dest[20].(*int32)) = s.durationMS
	*(dest[21].(*time.Time)) = s.startedAt
	*(dest[22].(*time.Time)) = s.finishedAt
	return nil
}

func TestScanQualityRuleResultUnmarshalsJSONBSnapshots(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	finishedAt := startedAt.Add(3 * time.Second)
	result, err := scanQualityRuleResult(qualityRuleResultScannerStub{
		id:                31,
		runID:             11,
		ruleID:            7,
		ruleRevision:      4,
		ruleEngineVersion: "2026.10.1",
		targetGUID:        "table/abc",
		targetType:        storepb.MetaType_TABLE,
		status:            storepb.QualityStatus_QUALITY_STATUS_FAIL,
		observedValue:     sql.NullFloat64{Float64: 0.42, Valid: true},
		observedKind:      storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO,
		thresholds:        []byte(`{"warnValue": 0.1, "failValue": 0.3, "comparison": "QUALITY_COMPARISON_GREATER_THAN"}`),
		failedRows:        21,
		totalRows:         50,
		sampled:           true,
		sampleRows:        500,
		truncated:         true,
		sample:            []byte(`{"samples": [{"columns": [{"column": "id", "value": "7"}]}]}`),
		executedSQL:       "SELECT COUNT(*) FROM t WHERE t.id IS NULL",
		errorCode:         "BAD_DATA",
		errorMessage:      "unexpected value",
		durationMS:        120,
		startedAt:         startedAt,
		finishedAt:        finishedAt,
	})
	require.NoError(t, err)

	require.Equal(t, int64(31), result.ID)
	require.Equal(t, int64(11), result.RunID)
	require.Equal(t, int64(7), result.RuleID)
	require.Equal(t, int32(4), result.RuleRevision)
	require.Equal(t, "2026.10.1", result.RuleEngineVersion)
	require.Equal(t, "table/abc", result.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, result.TargetType)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, result.Status)
	require.NotNil(t, result.ObservedValue)
	require.InDelta(t, 0.42, *result.ObservedValue, 1e-9)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO, result.ObservedKind)
	require.NotNil(t, result.Thresholds)
	require.InDelta(t, 0.1, result.Thresholds.GetWarnValue(), 1e-9)
	require.InDelta(t, 0.3, result.Thresholds.GetFailValue(), 1e-9)
	require.Equal(t, storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN, result.Thresholds.GetComparison())
	require.Equal(t, int64(21), result.FailedRows)
	require.Equal(t, int64(50), result.TotalRows)
	require.True(t, result.Sampled)
	require.Equal(t, int64(500), result.SampleRows)
	require.True(t, result.Truncated)
	require.Len(t, result.Sample.GetSamples(), 1)
	require.Len(t, result.Sample.GetSamples()[0].GetColumns(), 1)
	require.Equal(t, "id", result.Sample.GetSamples()[0].GetColumns()[0].GetColumn())
	require.Equal(t, "7", result.Sample.GetSamples()[0].GetColumns()[0].GetValue())
	require.Equal(t, "SELECT COUNT(*) FROM t WHERE t.id IS NULL", result.ExecutedSQL)
	require.Equal(t, "BAD_DATA", result.ErrorCode)
	require.Equal(t, "unexpected value", result.ErrorMessage)
	require.Equal(t, int32(120), result.DurationMS)
	require.Equal(t, startedAt, result.StartedAt)
	require.Equal(t, finishedAt, result.FinishedAt)
}

func TestScanQualityRuleResultKeepsNullObservedValue(t *testing.T) {
	t.Parallel()

	result, err := scanQualityRuleResult(qualityRuleResultScannerStub{
		id:           32,
		status:       storepb.QualityStatus_QUALITY_STATUS_ERROR,
		thresholds:   []byte(`{}`),
		observedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_UNSPECIFIED,
		sample:       []byte(`{}`),
	})
	require.NoError(t, err)

	// NULL observed_value must stay nil: ERROR and NOT_EVALUATED results have no
	// value, and a zero would be indistinguishable from a real 0 observation.
	require.Nil(t, result.ObservedValue)
	require.NotNil(t, result.Thresholds)
	require.NotNil(t, result.Sample)
}

func TestScanQualityRuleResultSurfacesBrokenJSONB(t *testing.T) {
	t.Parallel()

	_, err := scanQualityRuleResult(qualityRuleResultScannerStub{
		thresholds: []byte(`{"warnValue":"low"}`),
		sample:     []byte(`{}`),
	})
	require.Error(t, err)
}

func TestBuildQualityRuleResultWhereEmptyFilterListsEverything(t *testing.T) {
	t.Parallel()

	where, args := buildQualityRuleResultWhere(nil)
	require.Equal(t, "TRUE", where)
	require.Empty(t, args)
}

func TestBuildQualityRuleResultWhereAllFilters(t *testing.T) {
	t.Parallel()

	startAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	endAt := startAt.Add(24 * time.Hour)
	where, args := buildQualityRuleResultWhere(&FindQualityRuleResultMessage{
		RunID:  ptr(int64(11)),
		RuleID: ptr(int64(7)),
		Statuses: []storepb.QualityStatus{
			storepb.QualityStatus_QUALITY_STATUS_PASS,
			storepb.QualityStatus_QUALITY_STATUS_FAIL,
		},
		TargetGUID: ptr("table/abc"),
		StartAt:    &startAt,
		EndAt:      &endAt,
	})

	require.Equal(t, "run_id = $1 AND rule_id = $2 AND status = ANY($3::int[]) AND "+
		"target_guid = $4 AND started_at >= $5 AND started_at < $6", where)
	require.Equal(t, []any{
		int64(11),
		int64(7),
		pq.Array([]int32{
			int32(storepb.QualityStatus_QUALITY_STATUS_PASS),
			int32(storepb.QualityStatus_QUALITY_STATUS_FAIL),
		}),
		"table/abc",
		startAt,
		endAt,
	}, args)
}

func TestQualityRuleResultOrderClause(t *testing.T) {
	t.Parallel()

	require.Equal(t, " ORDER BY started_at DESC, id DESC", qualityRuleResultOrderClause(false))
	require.Equal(t, " ORDER BY started_at ASC, id ASC", qualityRuleResultOrderClause(true))
}

// The latest-result query shape is the invariant behind row_count_change
// baselines: one row per rule, chosen by started_at, in a single statement.
func TestQualityRuleResultLatestStatementShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityRuleResultLatestStatement, "DISTINCT ON (rule_id)")
	require.Contains(t, qualityRuleResultLatestStatement, "WHERE rule_id = ANY($1::bigint[])")
	require.Contains(t, qualityRuleResultLatestStatement, "ORDER BY rule_id, started_at DESC, id DESC")
}

func TestBatchCreateQualityRuleResultsEmptyBatchIsNoop(t *testing.T) {
	t.Parallel()

	s := &Store{}
	require.NoError(t, s.BatchCreateQualityRuleResults(context.Background(), nil))
	require.NoError(t, s.BatchCreateQualityRuleResults(context.Background(), []*QualityRuleResultMessage{}))
}

func TestListLatestQualityRuleResultsEmptyInputIsEmptyMap(t *testing.T) {
	t.Parallel()

	s := &Store{}
	latest, err := s.ListLatestQualityRuleResults(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.Empty(t, latest)
}

func TestListRecentQualityRuleResultsValidatesArguments(t *testing.T) {
	t.Parallel()

	s := &Store{}

	_, err := s.ListRecentQualityRuleResults(context.Background(), 0, 10)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = s.ListRecentQualityRuleResults(context.Background(), 7, 0)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// The insert statement is the other half of the column contract: if a column is
// added to the projection but not to the UNNEST argument list, results would be
// written in the wrong column.
func TestQualityRuleResultInsertStatementShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityRuleResultInsertStatement, "INSERT INTO quality_rule_result")
	require.Contains(t, qualityRuleResultInsertStatement, "SELECT * FROM UNNEST(")
	// int2 columns (target_type, status, observed_kind) are narrowed explicitly.
	for _, cast := range []string{
		"$1::bigint[]",
		"$6::int2[]",
		"$7::int2[]",
		"$8::double precision[]",
		"$9::int2[]",
		"$10::jsonb[]",
		"$13::boolean[]",
		"$16::jsonb[]",
		"$21::timestamptz[]",
		"$22::timestamptz[]",
	} {
		require.Contains(t, qualityRuleResultInsertStatement, cast)
	}
}

// The result getter is addressed by the row id the public resource name carries.
// A non-positive id is refused before any query, and the statement reads the same
// projection every list in this file does.
func TestGetQualityRuleResultRejectsNonPositiveID(t *testing.T) {
	t.Parallel()

	// A nil store would panic if the validation did not run first, which is the
	// assertion: a malformed name never reaches the database.
	store := &Store{}
	for _, id := range []int64{0, -1} {
		_, err := store.GetQualityRuleResult(t.Context(), id)
		require.Error(t, err)
		require.Equal(t, common.Invalid, common.ErrorCode(err))
	}
}

func TestQualityRuleResultByIDStatementShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityRuleResultByIDStatement, "FROM quality_rule_result")
	require.Contains(t, qualityRuleResultByIDStatement, "WHERE id = $1")
	require.Contains(t, qualityRuleResultByIDStatement, qualityRuleResultColumns)
	require.NotContains(t, qualityRuleResultByIDStatement, "%s",
		"a formatted query would hide an argument splice from this guard")
}
