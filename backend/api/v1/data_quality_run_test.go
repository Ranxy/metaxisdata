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

// qualityRunFixture is one dispatch batch that belongs to one target, shaped like
// the store reads it.
func qualityRunFixture() *store.QualityRunMessage {
	startedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	finishedAt := startedAt.Add(4 * time.Minute)
	initiator := int32(7)
	return &store.QualityRunMessage{
		ID:                 42,
		ResourceID:         "runabc",
		InstanceResourceID: "instance_1",
		TargetGUID:         "instance_1;shop;public;orders",
		TargetType:         storepb.MetaType_TABLE,
		TargetPath: &storepb.QualityTargetPath{
			Instance: "prod", Database: "shop", Schema: "public", Object: "orders",
		},
		Trigger: storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
		Status:  storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		Outcome: storepb.QualityStatus_QUALITY_STATUS_FAIL,
		Counts: &storepb.QualityStatusCounts{
			Total: 3, Pass: 1, Fail: 2,
		},
		InitiatorID:     &initiator,
		StartedAt:       startedAt,
		FinishedAt:      &finishedAt,
		RuleRevisionMin: 1,
		RuleRevisionMax: 3,
		RetryCount:      2,
	}
}

// The run converter carries the batch identity, its one target, lifecycle,
// counts and the revision range its executions judged.
func TestConvertQualityRun(t *testing.T) {
	t.Parallel()

	run := qualityRunFixture()
	converted := convertQualityRun("ws1", run)
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

	require.Equal(t, "instance_1;shop;public;orders", converted.GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.MetaType_TABLE, converted.GetTarget().GetMetaType())
	require.Equal(t, "orders", converted.GetTarget().GetPath().GetObject())
	require.Equal(t, int32(1), converted.GetRuleRevisionMin())
	require.Equal(t, int32(3), converted.GetRuleRevisionMax())
	require.Equal(t, int32(2), converted.GetRetryCount())

	// A run without a finished time keeps the field unset rather than showing the
	// epoch; a nil run converts to nil.
	run.FinishedAt = nil
	require.Nil(t, convertQualityRun("ws1", run).GetFinishTime())
	require.Nil(t, convertQualityRun("ws1", nil))
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
		MetaType:   v1pb.MetaType_TABLE,
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
	require.NotNil(t, find.TargetType)
	require.Equal(t, storepb.MetaType_TABLE, *find.TargetType)
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
	require.Nil(t, empty.TargetType)
	require.Nil(t, empty.InstanceResourceID)

	invalid := []struct {
		name    string
		request *v1pb.ListQualityRunsRequest
	}{
		{name: "trigger", request: &v1pb.ListQualityRunsRequest{Trigger: v1pb.QualityExecutionTrigger(99)}},
		{name: "state", request: &v1pb.ListQualityRunsRequest{State: v1pb.QualityExecutionState(99)}},
		{name: "outcome", request: &v1pb.ListQualityRunsRequest{Outcome: v1pb.QualityStatus(99)}},
		{name: "meta type", request: &v1pb.ListQualityRunsRequest{MetaType: v1pb.MetaType(99)}},
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
