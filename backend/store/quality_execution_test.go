package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityExecutionScannerStub stands in for a *sql.Row. The column count check is
// the guard that qualityExecutionColumns and scanQualityExecution stay in the
// same order and the same length: a schema change that adds a column without
// touching the scan fails here instead of mis-assigning values at runtime.
type qualityExecutionScannerStub struct {
	id                 int64
	runID              int64
	ruleID             int64
	ruleRevision       int32
	ruleEngineVersion  string
	targetGUID         string
	targetType         storepb.MetaType
	targetPath         []byte
	instanceResourceID string
	trigger            storepb.QualityExecutionTrigger
	attempt            int32
	initiatorID        sql.NullInt32
	state              storepb.QualityExecutionState
	status             storepb.QualityStatus
	observedValue      sql.NullFloat64
	observedKind       storepb.QualityObservedKind
	thresholds         []byte
	failedRows         int64
	totalRows          int64
	sampled            bool
	sampleRows         int64
	truncated          bool
	sample             []byte
	executedSQL        string
	errorCode          string
	errorMessage       string
	durationMS         int32
	startedAt          time.Time
	finishedAt         sql.NullTime
}

func (s qualityExecutionScannerStub) Scan(dest ...any) error {
	if len(dest) != 29 {
		return fmt.Errorf("scanQualityExecution scanned %d destinations, want 29", len(dest))
	}
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*int64)) = s.runID
	*(dest[2].(*int64)) = s.ruleID
	*(dest[3].(*int32)) = s.ruleRevision
	*(dest[4].(*string)) = s.ruleEngineVersion
	*(dest[5].(*string)) = s.targetGUID
	*(dest[6].(*storepb.MetaType)) = s.targetType
	*(dest[7].(*[]byte)) = s.targetPath
	*(dest[8].(*string)) = s.instanceResourceID
	*(dest[9].(*storepb.QualityExecutionTrigger)) = s.trigger
	*(dest[10].(*int32)) = s.attempt
	*(dest[11].(*sql.NullInt32)) = s.initiatorID
	*(dest[12].(*storepb.QualityExecutionState)) = s.state
	*(dest[13].(*storepb.QualityStatus)) = s.status
	*(dest[14].(*sql.NullFloat64)) = s.observedValue
	*(dest[15].(*storepb.QualityObservedKind)) = s.observedKind
	*(dest[16].(*[]byte)) = s.thresholds
	*(dest[17].(*int64)) = s.failedRows
	*(dest[18].(*int64)) = s.totalRows
	*(dest[19].(*bool)) = s.sampled
	*(dest[20].(*int64)) = s.sampleRows
	*(dest[21].(*bool)) = s.truncated
	*(dest[22].(*[]byte)) = s.sample
	*(dest[23].(*string)) = s.executedSQL
	*(dest[24].(*string)) = s.errorCode
	*(dest[25].(*string)) = s.errorMessage
	*(dest[26].(*int32)) = s.durationMS
	*(dest[27].(*time.Time)) = s.startedAt
	*(dest[28].(*sql.NullTime)) = s.finishedAt
	return nil
}

// The projection and the scan are one contract: this pins the order of every
// column scanQualityExecution consumes.
func TestQualityExecutionColumnsMatchTheScan(t *testing.T) {
	t.Parallel()

	columns := strings.Split(qualityExecutionColumns, ", ")
	require.Equal(t, []string{
		"id",
		"run_id",
		"rule_id",
		"rule_revision",
		"rule_engine_version",
		"target_guid",
		"target_type",
		"target_path",
		"instance_resource_id",
		"trigger",
		"attempt",
		"initiator_id",
		"state",
		"status",
		"observed_value",
		"observed_kind",
		"thresholds",
		"failed_rows",
		"total_rows",
		"sampled",
		"sample_rows",
		"truncated",
		"sample",
		"executed_sql",
		"error_code",
		"error_message",
		"duration_ms",
		"started_at",
		"finished_at",
	}, columns)
	require.NotContains(t, qualityExecutionColumns, "*")
}

func TestScanQualityExecutionReadsEveryColumn(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	finishedAt := startedAt.Add(3 * time.Second)
	execution, err := scanQualityExecution(qualityExecutionScannerStub{
		id:                 31,
		runID:              11,
		ruleID:             7,
		ruleRevision:       4,
		ruleEngineVersion:  "2026.10.1",
		targetGUID:         "table/abc",
		targetType:         storepb.MetaType_TABLE,
		targetPath:         []byte(`{"instance":"prod","database":"shop","schema":"public","object":"orders","column":"id"}`),
		instanceResourceID: "instances/inst1",
		trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
		attempt:            2,
		initiatorID:        sql.NullInt32{Int32: 9, Valid: true},
		state:              storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		status:             storepb.QualityStatus_QUALITY_STATUS_FAIL,
		observedValue:      sql.NullFloat64{Float64: 0.42, Valid: true},
		observedKind:       storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO,
		thresholds:         []byte(`{"warnValue": 0.1, "failValue": 0.3, "comparison": "QUALITY_COMPARISON_GREATER_THAN"}`),
		failedRows:         21,
		totalRows:          50,
		sampled:            true,
		sampleRows:         500,
		truncated:          true,
		sample:             []byte(`{"samples": [{"columns": [{"column": "id", "value": "7", "isNull": false}, {"column": "note", "isNull": true}, {"column": "legacy"}]}]}`),
		executedSQL:        "SELECT COUNT(*) FROM t WHERE t.id IS NULL",
		errorCode:          "BAD_DATA",
		errorMessage:       "unexpected value",
		durationMS:         120,
		startedAt:          startedAt,
		finishedAt:         sql.NullTime{Time: finishedAt, Valid: true},
	})
	require.NoError(t, err)

	require.Equal(t, int64(31), execution.ID)
	require.Equal(t, int64(11), execution.RunID)
	require.Equal(t, int64(7), execution.RuleID)
	require.Equal(t, int32(4), execution.RuleRevision)
	require.Equal(t, "2026.10.1", execution.RuleEngineVersion)
	require.Equal(t, "table/abc", execution.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, execution.TargetType)
	require.Equal(t, "prod", execution.TargetPath.GetInstance())
	require.Equal(t, "shop", execution.TargetPath.GetDatabase())
	require.Equal(t, "public", execution.TargetPath.GetSchema())
	require.Equal(t, "orders", execution.TargetPath.GetObject())
	require.Equal(t, "id", execution.TargetPath.GetColumn())
	require.Equal(t, "instances/inst1", execution.InstanceResourceID)
	require.Equal(t, storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL, execution.Trigger)
	require.Equal(t, int32(2), execution.Attempt)
	require.NotNil(t, execution.InitiatorID)
	require.Equal(t, int32(9), *execution.InitiatorID)
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, execution.State)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, execution.Status)
	require.NotNil(t, execution.ObservedValue)
	require.InDelta(t, 0.42, *execution.ObservedValue, 1e-9)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO, execution.ObservedKind)
	require.NotNil(t, execution.Thresholds)
	require.InDelta(t, 0.1, execution.Thresholds.GetWarnValue(), 1e-9)
	require.InDelta(t, 0.3, execution.Thresholds.GetFailValue(), 1e-9)
	require.Equal(t, storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN, execution.Thresholds.GetComparison())
	require.Equal(t, int64(21), execution.FailedRows)
	require.Equal(t, int64(50), execution.TotalRows)
	require.True(t, execution.Sampled)
	require.Equal(t, int64(500), execution.SampleRows)
	require.True(t, execution.Truncated)
	require.Len(t, execution.Sample.GetSamples(), 1)
	require.Len(t, execution.Sample.GetSamples()[0].GetColumns(), 3)
	require.Equal(t, "id", execution.Sample.GetSamples()[0].GetColumns()[0].GetColumn())
	require.Equal(t, "7", execution.Sample.GetSamples()[0].GetColumns()[0].GetValue())
	require.False(t, execution.Sample.GetSamples()[0].GetColumns()[0].GetIsNull())
	require.NotNil(t, execution.Sample.GetSamples()[0].GetColumns()[0].IsNull, "an explicit false marker survives JSONB")
	require.True(t, execution.Sample.GetSamples()[0].GetColumns()[1].GetIsNull(), "a stored NULL keeps its marker")
	require.Nil(t, execution.Sample.GetSamples()[0].GetColumns()[2].IsNull, "a legacy row has no marker, which is not the same as false")
	require.Equal(t, "SELECT COUNT(*) FROM t WHERE t.id IS NULL", execution.ExecutedSQL)
	require.Equal(t, "BAD_DATA", execution.ErrorCode)
	require.Equal(t, "unexpected value", execution.ErrorMessage)
	require.Equal(t, int32(120), execution.DurationMS)
	require.Equal(t, startedAt, execution.StartedAt)
	require.Equal(t, finishedAt, execution.FinishedAt)
}

// A NULL observed value, initiator and finished_at must stay absent rather than
// become a real zero: 0 is a legitimate observed value, and the zero time would
// render as a finished execution. The empty JSONB defaults must still produce
// usable, non-nil messages.
func TestScanQualityExecutionKeepsAbsentColumnsAbsent(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	execution, err := scanQualityExecution(qualityExecutionScannerStub{
		id:         12,
		trigger:    storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
		state:      storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		targetPath: []byte(`{}`),
		startedAt:  startedAt,
	})
	require.NoError(t, err)

	require.Nil(t, execution.ObservedValue)
	require.Nil(t, execution.InitiatorID)
	require.True(t, execution.FinishedAt.IsZero())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, execution.Status)
	require.NotNil(t, execution.TargetPath)
	require.NotNil(t, execution.Thresholds)
	require.NotNil(t, execution.Sample)
}

// An empty payload and the literal "null" both leave the message at its zero
// value, so a row written before the object shape was enforced still reads.
func TestScanQualityExecutionToleratesEmptyJSONSnapshots(t *testing.T) {
	t.Parallel()

	execution, err := scanQualityExecution(qualityExecutionScannerStub{
		targetPath: []byte(`null`),
		thresholds: nil,
		sample:     []byte(`null`),
	})
	require.NoError(t, err)
	require.NotNil(t, execution.TargetPath)
	require.Empty(t, execution.TargetPath.GetObject())
	require.NotNil(t, execution.Thresholds)
	require.NotNil(t, execution.Sample)
}

func TestScanQualityExecutionSurfacesBrokenJSONB(t *testing.T) {
	t.Parallel()

	_, err := scanQualityExecution(qualityExecutionScannerStub{thresholds: []byte(`{"failValue":"many"}`)})
	require.Error(t, err)
}

func TestBuildQualityExecutionWhereEmptyFilterListsEverything(t *testing.T) {
	t.Parallel()

	where, args := buildQualityExecutionWhere(nil)
	require.Equal(t, "TRUE", where)
	require.Empty(t, args)
}

func TestBuildQualityExecutionWhereAllFilters(t *testing.T) {
	t.Parallel()

	startAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	endAt := startAt.Add(24 * time.Hour)
	where, args := buildQualityExecutionWhere(&FindQualityExecutionMessage{
		RunID:  ptr(int64(11)),
		RuleID: ptr(int64(7)),
		States: []storepb.QualityExecutionState{
			storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
			storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING,
		},
		Statuses: []storepb.QualityStatus{
			storepb.QualityStatus_QUALITY_STATUS_FAIL,
			storepb.QualityStatus_QUALITY_STATUS_ERROR,
		},
		Trigger:            ptr(storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED),
		TargetGUID:         ptr("inst;db;;orders"),
		InstanceResourceID: ptr("instances/inst1"),
		InitiatorID:        ptr(int32(7)),
		StartAt:            &startAt,
		EndAt:              &endAt,
	})

	require.Equal(t, "run_id = $1 AND rule_id = $2 AND state = ANY($3::int[]) AND "+
		"status = ANY($4::int[]) AND trigger = $5::int AND target_guid = $6 AND "+
		"instance_resource_id = $7 AND initiator_id = $8 AND started_at >= $9 AND started_at < $10", where)
	require.Equal(t, []any{
		int64(11),
		int64(7),
		pq.Array([]int32{
			int32(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING),
			int32(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING),
		}),
		pq.Array([]int32{
			int32(storepb.QualityStatus_QUALITY_STATUS_FAIL),
			int32(storepb.QualityStatus_QUALITY_STATUS_ERROR),
		}),
		int32(storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED),
		"inst;db;;orders",
		"instances/inst1",
		int32(7),
		startAt,
		endAt,
	}, args)
}

func TestQualityExecutionOrderClause(t *testing.T) {
	t.Parallel()

	require.Equal(t, " ORDER BY started_at DESC, id DESC", qualityExecutionOrderClause(false))
	require.Equal(t, " ORDER BY started_at ASC, id ASC", qualityExecutionOrderClause(true))
}

// The plan statement inserts one PENDING row and returns its id: the caller
// enqueues exactly the rows it planned, and the row id is the public resource
// identity.
func TestQualityExecutionPlanStatementShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityExecutionPlanStatement, "INSERT INTO quality_execution")
	require.Contains(t, qualityExecutionPlanStatement, ") VALUES (")
	require.Contains(t, qualityExecutionPlanStatement, "RETURNING id")
	for _, column := range []string{
		"run_id", "rule_id", "rule_revision", "rule_engine_version",
		"target_guid", "target_type", "target_path", "instance_resource_id",
		"trigger", "initiator_id", "state", "started_at",
	} {
		require.Contains(t, qualityExecutionPlanStatement, column)
	}
	require.NotContains(t, qualityExecutionPlanStatement, "status", "a planned execution has no verdict yet")
	require.NotContains(t, qualityExecutionPlanStatement, "observed_value")
}

// Mark-running is guarded in SQL: only a PENDING row starts, so a retried
// dispatch cannot restart an execution another worker owns.
func TestQualityExecutionMarkRunningStatementShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityExecutionMarkRunningStatement, "SET state = 2")
	require.Contains(t, qualityExecutionMarkRunningStatement, "started_at = $2")
	require.Contains(t, qualityExecutionMarkRunningStatement, "WHERE id = $1 AND state = 1")
	require.Contains(t, qualityExecutionMarkRunningStatement, "RETURNING id")
}

// Finish merges the snapshot columns rather than overwriting them: a failure
// before the runner resolved the rule revision or target carries zero values,
// and the planned row's values are the correct evidence then.
func TestQualityExecutionFinishStatementMergesPlannedSnapshot(t *testing.T) {
	t.Parallel()

	for _, column := range []string{
		"rule_revision = COALESCE(NULLIF($2::int, 0), rule_revision)",
		"rule_engine_version = COALESCE(NULLIF($3::text, ''), rule_engine_version)",
		"target_guid = COALESCE(NULLIF($4::text, ''), target_guid)",
		"target_type = COALESCE(NULLIF($5::int2, 0::int2), target_type)",
		"target_path = COALESCE(NULLIF($6::jsonb, '{}'::jsonb), target_path)",
		"instance_resource_id = COALESCE(NULLIF($7::text, ''), instance_resource_id)",
	} {
		require.Contains(t, qualityExecutionFinishStatement, column)
	}
	require.Contains(t, qualityExecutionFinishStatement, "status = $8::int2")
	require.Contains(t, qualityExecutionFinishStatement, "state = $22::int2")
	require.Contains(t, qualityExecutionFinishStatement, "finished_at = $23::timestamptz")
	require.Contains(t, qualityExecutionFinishStatement, "WHERE id = $1 AND state IN (1, 2)")
	require.Contains(t, qualityExecutionFinishStatement, "RETURNING id")
	// The dispatch trigger, initiator and rule identity were written when the
	// execution was planned; finishing never rewrites them.
	require.NotContains(t, qualityExecutionFinishStatement, "run_id =")
	require.NotContains(t, qualityExecutionFinishStatement, "trigger =")
	require.NotContains(t, qualityExecutionFinishStatement, "initiator_id =")
}

// The abandon statements close only unfinished rows: a completed execution keeps
// its verdict, and the stale sweep is bounded by the started_at cutoff.
func TestQualityExecutionAbandonStatementShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityExecutionAbandonBatchStatement, "SET state = $1, finished_at = $2")
	require.Contains(t, qualityExecutionAbandonBatchStatement, "WHERE run_id = $3 AND state IN ($4, $5)")

	require.Contains(t, qualityExecutionAbandonStaleStatement, "SET state = $1, finished_at = NOW()")
	require.Contains(t, qualityExecutionAbandonStaleStatement, "WHERE started_at < $2 AND state IN ($3, $4)")
}

func TestPlanQualityExecutionsEmptyBatchIsNoop(t *testing.T) {
	t.Parallel()

	// A zero Store has no pool: reaching the database here would panic, so this
	// also pins that an empty batch is a no-op.
	s := &Store{}
	require.NoError(t, s.PlanQualityExecutions(context.Background(), nil))
	require.NoError(t, s.PlanQualityExecutions(context.Background(), []*QualityExecutionMessage{}))
}

func TestPlanQualityExecutionsValidatesInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := &Store{}

	require.Equal(t, common.Invalid, common.ErrorCode(s.PlanQualityExecutions(ctx, []*QualityExecutionMessage{nil})))
	require.Equal(t, common.Invalid, common.ErrorCode(s.PlanQualityExecutions(ctx, []*QualityExecutionMessage{{
		RuleID:  7,
		Trigger: storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
	}})))
	require.Equal(t, common.Invalid, common.ErrorCode(s.PlanQualityExecutions(ctx, []*QualityExecutionMessage{{
		RunID:   11,
		Trigger: storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
	}})))
	require.Equal(t, common.Invalid, common.ErrorCode(s.PlanQualityExecutions(ctx, []*QualityExecutionMessage{{
		RunID:  11,
		RuleID: 7,
	}})))
}

func TestQualityExecutionMutatorsValidateInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := &Store{}
	at := time.Now().UTC()

	require.Equal(t, common.Invalid, common.ErrorCode(s.FinishQualityExecution(ctx, nil)))
	require.Equal(t, common.Invalid, common.ErrorCode(s.FinishQualityExecution(ctx, &QualityExecutionMessage{
		Status: storepb.QualityStatus_QUALITY_STATUS_PASS,
	})))
	require.Equal(t, common.Invalid, common.ErrorCode(s.FinishQualityExecution(ctx, &QualityExecutionMessage{ID: 1})))

	require.Equal(t, common.Invalid, common.ErrorCode(s.MarkQualityExecutionRunning(ctx, 0, at)))
	require.Equal(t, common.Invalid, common.ErrorCode(s.MarkQualityExecutionRunning(ctx, 1, time.Time{})))

	count, err := s.AbandonQualityExecutions(ctx, 0, at)
	require.Equal(t, int64(0), count)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	count, err = s.AbandonQualityExecutions(ctx, 11, time.Time{})
	require.Equal(t, int64(0), count)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	count, err = s.AbandonStaleQualityExecutions(ctx, time.Time{})
	require.Equal(t, int64(0), count)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

func TestQualityExecutionReadsValidateArguments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := &Store{}

	execution, err := s.GetQualityExecution(ctx, 0)
	require.Nil(t, execution)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = s.ListRecentQualityExecutions(ctx, 0, 10)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	_, err = s.ListRecentQualityExecutions(ctx, 7, 0)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	latest, err := s.ListLatestQualityExecutions(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.Empty(t, latest)

	count, err := s.CountQualityExecutionsSince(ctx, time.Time{})
	require.Equal(t, 0, count)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// The recent and latest lookups are baselines for row_count_change and for the
// rule list's last result: a merely planned or abandoned execution is not an
// observation, so both filter to COMPLETED.
func TestQualityExecutionBaselineQueriesReadOnlyCompletedRows(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityExecutionLatestStatement, "FROM quality_execution")
	require.Contains(t, qualityExecutionLatestStatement, "rule_id = ANY($1::bigint[])")
	require.Contains(t, qualityExecutionLatestStatement, "state = $2")
	require.Contains(t, qualityExecutionLatestStatement, "DISTINCT ON (rule_id)")
	require.Contains(t, qualityExecutionLatestStatement, "ORDER BY rule_id, started_at DESC, id DESC")
}

// The per-target recent-executions read is the batch form of the history strip:
// it partitions by the target identity, not by rule, so a directory page costs one
// statement, and it only reads completed rows because a planned execution is not a
// verdict.
func TestQualityExecutionRecentByTargetsStatementShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityExecutionRecentByTargetsStatement, "FROM quality_execution")
	require.Contains(t, qualityExecutionRecentByTargetsStatement, "ROW_NUMBER() OVER")
	require.Contains(t, qualityExecutionRecentByTargetsStatement, "PARTITION BY e.target_guid, e.target_type")
	require.Contains(t, qualityExecutionRecentByTargetsStatement, "ORDER BY e.started_at DESC, e.id DESC")
	require.Contains(t, qualityExecutionRecentByTargetsStatement, "unnest($1::text[], $2::int2[])")
	require.Contains(t, qualityExecutionRecentByTargetsStatement, "e.state = $3")
	// The window only holds verdicts of currently enabled rules: pausing a rule
	// must not leave its history in the target's recent pass rate.
	require.Contains(t, qualityExecutionRecentByTargetsStatement,
		"JOIN quality_rule r ON r.id = e.rule_id AND r.state = $4 AND r.deleted = FALSE")
	require.Contains(t, qualityExecutionRecentByTargetsStatement, "WHERE rn <= $5")
}

// Protojson has to keep the optional null marker's three states in JSONB: absent
// (a sample recorded before the marker existed), an explicit false (the value was
// an empty string) and an explicit true (the value was SQL NULL). Losing the
// explicit false would make a new empty string read as "unknown".
func TestQualityExecutionSampleKeepsTheNullMarker(t *testing.T) {
	t.Parallel()

	samples := &storepb.QualityFailureSamples{Samples: []*storepb.FailureSample{{Columns: []*storepb.ColumnValue{
		{Column: "legacy"},
		{Column: "blank", IsNull: proto.Bool(false)},
		{Column: "missing", IsNull: proto.Bool(true)},
	}}}}
	raw, err := marshalQualityProto(samples)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"isNull":false`)
	require.Contains(t, string(raw), `"isNull":true`)

	decoded := &storepb.QualityFailureSamples{}
	require.NoError(t, unmarshalQualityProto(raw, decoded))
	columns := decoded.GetSamples()[0].GetColumns()
	require.Nil(t, columns[0].IsNull, "an absent marker stays absent")
	require.NotNil(t, columns[1].IsNull)
	require.False(t, *columns[1].IsNull)
	require.NotNil(t, columns[2].IsNull)
	require.True(t, *columns[2].IsNull)
}
