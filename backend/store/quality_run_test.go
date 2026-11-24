package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type qualityRunScannerStub struct {
	id                 int64
	resourceID         string
	instanceResourceID string
	targetGUID         string
	targetType         storepb.MetaType
	targetPath         []byte
	trigger            storepb.QualityExecutionTrigger
	status             storepb.QualityExecutionState
	outcome            sql.NullInt32
	counts             []byte
	initiatorID        sql.NullInt32
	startedAt          time.Time
	finishedAt         sql.NullTime
}

func (s qualityRunScannerStub) Scan(dest ...any) error {
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*string)) = s.resourceID
	*(dest[2].(*string)) = s.instanceResourceID
	*(dest[3].(*string)) = s.targetGUID
	*(dest[4].(*storepb.MetaType)) = s.targetType
	*(dest[5].(*[]byte)) = s.targetPath
	*(dest[6].(*storepb.QualityExecutionTrigger)) = s.trigger
	*(dest[7].(*storepb.QualityExecutionState)) = s.status
	*(dest[8].(*sql.NullInt32)) = s.outcome
	*(dest[9].(*[]byte)) = s.counts
	*(dest[10].(*sql.NullInt32)) = s.initiatorID
	*(dest[11].(*time.Time)) = s.startedAt
	*(dest[12].(*sql.NullTime)) = s.finishedAt
	return nil
}

func TestScanQualityRunPendingHasNoOutcomeYet(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	run, err := scanQualityRun(qualityRunScannerStub{
		id:                 11,
		resourceID:         "runs/abc",
		instanceResourceID: "instances/inst1",
		targetGUID:         "inst1;shop;public;orders",
		targetType:         storepb.MetaType_TABLE,
		trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
		status:             storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		outcome:            sql.NullInt32{},
		counts:             []byte(`{"total":4,"pass":2,"fail":1,"error":1}`),
		initiatorID:        sql.NullInt32{},
		startedAt:          startedAt,
		finishedAt:         sql.NullTime{},
	})
	require.NoError(t, err)

	require.Equal(t, int64(11), run.ID)
	require.Equal(t, "runs/abc", run.ResourceID)
	require.Equal(t, "instances/inst1", run.InstanceResourceID)
	require.Equal(t, storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL, run.Trigger)
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING, run.Status)
	// A NULL outcome column must stay UNSPECIFIED in memory, not PASS (the zero
	// value of the quality status enum is UNSPECIFIED, and PASS is 1).
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, run.Outcome)
	require.Equal(t, int32(4), run.Counts.GetTotal())
	require.Equal(t, int32(2), run.Counts.GetPass())
	require.Equal(t, int32(1), run.Counts.GetFail())
	require.Equal(t, int32(1), run.Counts.GetError())
	require.Nil(t, run.InitiatorID)
	require.Equal(t, startedAt, run.StartedAt)
	require.Nil(t, run.FinishedAt)
}

func TestScanQualityRunFinishedCarriesOutcomeAndFinisher(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	finishedAt := startedAt.Add(2 * time.Minute)
	run, err := scanQualityRun(qualityRunScannerStub{
		id:         12,
		resourceID: "runs/def",
		targetGUID: "inst1;shop;public;orders",
		targetType: storepb.MetaType_TABLE,
		trigger:    storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
		status:     storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		outcome:    sql.NullInt32{Int32: int32(storepb.QualityStatus_QUALITY_STATUS_FAIL), Valid: true},
		counts:     []byte(`{}`),
		initiatorID: sql.NullInt32{
			Int32: 7,
			Valid: true,
		},
		startedAt:  startedAt,
		finishedAt: sql.NullTime{Time: finishedAt, Valid: true},
	})
	require.NoError(t, err)

	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, run.Status)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, run.Outcome)
	require.NotNil(t, run.InitiatorID)
	require.Equal(t, int32(7), *run.InitiatorID)
	require.NotNil(t, run.FinishedAt)
	require.Equal(t, finishedAt, *run.FinishedAt)
	// The empty JSONB default still produces a usable, non-nil message.
	require.NotNil(t, run.Counts)
}

func TestScanQualityRunSurfacesBrokenCountsJSON(t *testing.T) {
	t.Parallel()

	_, err := scanQualityRun(qualityRunScannerStub{counts: []byte(`{"total":"many"}`)})
	require.Error(t, err)
}

func TestCreateQualityRunValidatesInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := &Store{}

	_, err := s.CreateQualityRun(ctx, nil)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = s.CreateQualityRun(ctx, &QualityRunMessage{Trigger: storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL})
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = s.CreateQualityRun(ctx, &QualityRunMessage{ResourceID: "runs/abc"})
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = s.CreateQualityRun(ctx, &QualityRunMessage{
		ResourceID: "runs/abc",
		TargetGUID: "inst1;shop;public;orders",
		Trigger:    storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
	})
	require.Equal(t, common.Invalid, common.ErrorCode(err), "missing target type")

	_, err = s.CreateQualityRun(ctx, &QualityRunMessage{
		ResourceID: "runs/abc",
		TargetGUID: "inst1;shop;public;orders",
		TargetType: storepb.MetaType_TABLE,
		Trigger:    storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
		Status:     storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
	})
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

func TestQualityRunMutatorsValidateInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := &Store{}
	at := time.Now().UTC()

	err := s.FinishQualityRun(ctx, 1, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING, storepb.QualityStatus_QUALITY_STATUS_PASS, nil, at)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	err = s.FinishQualityRun(ctx, 1, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, nil, at)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	err = s.MarkQualityRunRunning(ctx, 0, at)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = s.AbandonStaleQualityRuns(ctx, time.Time{})
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// The batch is internal machinery: its retention statement deletes from
// quality_run and relies on the ON DELETE CASCADE to reclaim the executions it
// planned, so the store never needs an execution-level retention sweep.
func TestDeleteExpiredQualityRunsStatementShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityRunDeleteExpiredStatement, "DELETE FROM quality_run")
	require.Contains(t, qualityRunDeleteExpiredStatement, "WHERE started_at < $1")
}

// An empty run filter matches every run and binds no argument.
func TestBuildQualityRunWhereEmpty(t *testing.T) {
	t.Parallel()

	where, args := buildQualityRunWhere(nil)
	require.Equal(t, []string{"TRUE"}, where)
	require.Empty(t, args)

	where, args = buildQualityRunWhere(&FindQualityRunMessage{})
	require.Equal(t, []string{"TRUE"}, where)
	require.Empty(t, args)
}

// Every run filter maps to one predicate with contiguous placeholders; the target
// filter is an EXISTS over the run's executions, not a join.
func TestBuildQualityRunWhere(t *testing.T) {
	t.Parallel()

	instance := "instance_1"
	trigger := storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED
	status := storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED
	outcome := storepb.QualityStatus_QUALITY_STATUS_FAIL
	target := "instance_1;shop;public;orders"
	targetType := storepb.MetaType_TABLE
	after := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	before := after.Add(24 * time.Hour)

	where, args := buildQualityRunWhere(&FindQualityRunMessage{
		InstanceResourceID: &instance,
		Trigger:            &trigger,
		Status:             &status,
		Outcome:            &outcome,
		TargetGUID:         &target,
		TargetType:         &targetType,
		StartedAfter:       &after,
		StartedBefore:      &before,
	})
	joined := strings.Join(where, " AND ")
	require.Equal(t, []any{"instance_1", int32(trigger), int32(status), int32(outcome), target, int32(targetType), after, before}, args)
	require.Contains(t, joined, "r.instance_resource_id = $1")
	require.Contains(t, joined, "r.trigger = $2::int")
	require.Contains(t, joined, "r.status = $3::int")
	require.Contains(t, joined, "r.outcome = $4::int")
	require.Contains(t, joined, "r.target_guid = $5")
	require.Contains(t, joined, "r.target_type = $6::int2")
	require.Contains(t, joined, "r.started_at >= $7")
	require.Contains(t, joined, "r.started_at < $8")
}

// qualityTargetWorstRankCaseAt renders the worst-status CASE for a caller whose
// status arguments start at a given placeholder. The caller binds the statuses in
// the documented order (ERROR, FAIL, WARN, PASS, SKIPPED, NOT_EVALUATED), so a WHEN
// arm that moves relative to that order silently mis-ranks every result.
func TestQualityTargetWorstRankCaseAtShiftsItsPlaceholderBase(t *testing.T) {
	t.Parallel()

	const base = 5
	expr := qualityTargetWorstRankCaseAt("e.status", base)
	require.Contains(t, expr, "CASE e.status")
	statuses := []storepb.QualityStatus{
		storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_FAIL,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
	}
	for offset, status := range statuses {
		require.Contains(t, expr, fmt.Sprintf("WHEN $%d THEN %d", base+offset, QualityTargetStatusRank(status)))
	}
	require.Contains(t, expr, "ELSE 0")
}

// qualifyQualityRunColumns prefixes every selected column so a query that joins
// quality_run can read it without an ambiguous column reference. The expectation
// is derived from qualityRunColumns: a column added to the projection must not
// force this test red, but a column left unqualified must.
func TestQualifyQualityRunColumns(t *testing.T) {
	t.Parallel()

	qualified := strings.Split(qualifyQualityRunColumns("r"), ", ")
	columns := strings.Split(qualityRunColumns, ",")
	require.Len(t, qualified, len(columns))
	for index, column := range columns {
		require.Equal(t, "r."+strings.TrimSpace(column), qualified[index])
	}
}
