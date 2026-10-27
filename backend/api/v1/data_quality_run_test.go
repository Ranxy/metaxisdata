package v1

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// qualityRunFixture is one dispatch batch with one target's slice, shaped like
// the store reads them.
func qualityRunFixture() (*store.QualityRunMessage, []*store.QualityRunTargetMessage) {
	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	finishedAt := startedAt.Add(4 * time.Minute)
	initiator := int32(7)
	run := &store.QualityRunMessage{
		ID:                 42,
		ResourceID:         "runabc",
		InstanceResourceID: "instance_1",
		Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
		Status:             storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		Outcome:            storepb.QualityStatus_QUALITY_STATUS_FAIL,
		Counts: &storepb.QualityStatusCounts{
			Total: 3, Pass: 1, Fail: 2,
		},
		InitiatorID: &initiator,
		StartedAt:   startedAt,
		FinishedAt:  &finishedAt,
	}
	slices := []*store.QualityRunTargetMessage{{
		RunID:      42,
		TargetGUID: "instance_1;shop;public;orders",
		TargetType: storepb.MetaType_TABLE,
		TargetPath: &storepb.QualityTargetPath{
			Instance: "prod", Database: "shop", Schema: "public", Object: "orders",
		},
		Counts: &storepb.QualityStatusCounts{
			Total: 2, Pass: 1, Fail: 1,
		},
		Outcome:         storepb.QualityStatus_QUALITY_STATUS_FAIL,
		RuleRevisionMin: 1,
		RuleRevisionMax: 3,
		StartedAt:       startedAt,
		FinishedAt:      &finishedAt,
	}}
	return run, slices
}

// The run converter carries the batch identity, lifecycle, counts and its
// per-target slices, and the slice repeats the parent's name/state/trigger so it
// can stand alone as a target's latest_run.
func TestConvertQualityRun(t *testing.T) {
	t.Parallel()

	run, slices := qualityRunFixture()
	converted := convertQualityRun("ws1", run, slices)
	require.NotNil(t, converted)
	require.Equal(t, "workspaces/ws1/qualityRuns/42", converted.GetName())
	require.Equal(t, "instances/instance_1", converted.GetInstance())
	require.Equal(t, v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL, converted.GetTrigger())
	require.Equal(t, v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, converted.GetState())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, converted.GetOutcome())
	require.Equal(t, int32(3), converted.GetCounts().GetTotal())
	require.Equal(t, int32(1), converted.GetCounts().GetPass())
	require.Equal(t, int32(2), converted.GetCounts().GetFail())
	require.Equal(t, "users/7", converted.GetInitiator())
	require.Equal(t, run.StartedAt, converted.GetStartTime().AsTime())
	require.Equal(t, run.FinishedAt.UTC(), converted.GetFinishTime().AsTime())

	require.Len(t, converted.GetTargets(), 1)
	slice := converted.GetTargets()[0]
	require.Equal(t, "instance_1;shop;public;orders", slice.GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.MetaType_TABLE, slice.GetTarget().GetMetaType())
	require.Equal(t, "orders", slice.GetTarget().GetPath().GetObject())
	require.Equal(t, int32(2), slice.GetCounts().GetTotal())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, slice.GetOutcome())
	require.Equal(t, int32(1), slice.GetRuleRevisionMin())
	require.Equal(t, int32(3), slice.GetRuleRevisionMax())
	require.Equal(t, "workspaces/ws1/qualityRuns/42", slice.GetRun(),
		"the slice repeats its parent run so latest_run stands alone")
	require.Equal(t, v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, slice.GetState())
	require.Equal(t, v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL, slice.GetTrigger())
	require.Equal(t, slices[0].StartedAt, slice.GetStartTime().AsTime())
	require.Equal(t, slices[0].FinishedAt.UTC(), slice.GetFinishTime().AsTime())

	// A run without a finished time keeps the field unset rather than showing the
	// epoch, and a run with no slices converts to an empty targets list.
	run.FinishedAt = nil
	require.Nil(t, convertQualityRun("ws1", run, nil).GetFinishTime())
	require.Empty(t, convertQualityRun("ws1", run, nil).GetTargets())
	require.Nil(t, convertQualityRun("ws1", nil, slices))

	// A slice read without its parent still renders its own fields; the three
	// parent-derived fields stay unset.
	orphanSlice := convertQualityRunTarget("ws1", nil, slices[0])
	require.NotNil(t, orphanSlice)
	require.Empty(t, orphanSlice.GetRun())
	require.Equal(t, v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_UNSPECIFIED, orphanSlice.GetState())
	require.Equal(t, v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_UNSPECIFIED, orphanSlice.GetTrigger())
	require.Nil(t, convertQualityRunTarget("ws1", run, nil))
}

// The run list filters validate every enum instead of silently widening the
// directory, and map the remaining filters one-to-one.
func TestApplyQualityRunListFilters(t *testing.T) {
	t.Parallel()

	startedAt := timestamppb.New(time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC))
	finishedAt := timestamppb.New(time.Date(2026, time.October, 8, 0, 0, 0, 0, time.UTC))
	find := &store.FindQualityRunMessage{}
	require.NoError(t, applyQualityRunListFilters(find, &v1pb.ListQualityRunsRequest{
		Instance:   "instances/instance_1",
		TargetGuid: " instance_1;shop;public;orders ",
		Trigger:    v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
		State:      v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING,
		Outcome:    v1pb.QualityStatus_QUALITY_STATUS_WARN,
		StartTime:  startedAt,
		EndTime:    finishedAt,
	}))
	require.NotNil(t, find.InstanceResourceID)
	require.Equal(t, "instance_1", *find.InstanceResourceID)
	require.NotNil(t, find.TargetGUID)
	require.Equal(t, "instance_1;shop;public;orders", *find.TargetGUID)
	require.NotNil(t, find.Trigger)
	require.Equal(t, storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED, *find.Trigger)
	require.NotNil(t, find.Status)
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING, *find.Status)
	require.NotNil(t, find.Outcome)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, *find.Outcome)
	require.NotNil(t, find.StartedAfter)
	require.Equal(t, startedAt.AsTime(), *find.StartedAfter)
	require.NotNil(t, find.StartedBefore)
	require.Equal(t, finishedAt.AsTime(), *find.StartedBefore)

	// An unspecified enum means "no filter", and every field left empty keeps its
	// zero value.
	empty := &store.FindQualityRunMessage{}
	require.NoError(t, applyQualityRunListFilters(empty, &v1pb.ListQualityRunsRequest{}))
	require.Nil(t, empty.Trigger)
	require.Nil(t, empty.Status)
	require.Nil(t, empty.Outcome)
	require.Nil(t, empty.TargetGUID)
	require.Nil(t, empty.InstanceResourceID)

	invalid := []struct {
		name    string
		request *v1pb.ListQualityRunsRequest
	}{
		{name: "trigger", request: &v1pb.ListQualityRunsRequest{Trigger: v1pb.QualityExecutionTrigger(99)}},
		{name: "state", request: &v1pb.ListQualityRunsRequest{State: v1pb.QualityExecutionState(99)}},
		{name: "outcome", request: &v1pb.ListQualityRunsRequest{Outcome: v1pb.QualityStatus(99)}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := applyQualityRunListFilters(&store.FindQualityRunMessage{}, test.request)
			require.Error(t, err)
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

// The run resource name round-trips, and a name without a positive numeric id is
// refused before any store read.
func TestQualityRunNameHelpers(t *testing.T) {
	t.Parallel()

	require.Equal(t, "workspaces/ws1/qualityRuns/42", formatQualityRunName("ws1", 42))

	workspace, runID, err := parseQualityRunID("workspaces/ws1/qualityRuns/42")
	require.NoError(t, err)
	require.Equal(t, "ws1", workspace)
	require.Equal(t, int64(42), runID)

	for _, name := range []string{
		"",
		"workspaces/ws1/qualityExecutions/42",
		"workspaces/ws1/qualityRuns/not-a-number",
		"workspaces/ws1/qualityRuns/0",
		"workspaces/ws1/qualityRuns/-3",
	} {
		_, _, err := parseQualityRunID(name)
		require.Errorf(t, err, "name %q must be refused", name)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "name %q", name)
	}
}
