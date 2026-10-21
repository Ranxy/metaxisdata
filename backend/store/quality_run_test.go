package store

import (
	"context"
	"database/sql"
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
	*(dest[3].(*storepb.QualityExecutionTrigger)) = s.trigger
	*(dest[4].(*storepb.QualityExecutionState)) = s.status
	*(dest[5].(*sql.NullInt32)) = s.outcome
	*(dest[6].(*[]byte)) = s.counts
	*(dest[7].(*sql.NullInt32)) = s.initiatorID
	*(dest[8].(*time.Time)) = s.startedAt
	*(dest[9].(*sql.NullTime)) = s.finishedAt
	return nil
}

func TestScanQualityRunPendingHasNoOutcomeYet(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	run, err := scanQualityRun(qualityRunScannerStub{
		id:                 11,
		resourceID:         "runs/abc",
		instanceResourceID: "instances/inst1",
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
