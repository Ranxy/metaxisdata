package v1

import (
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// dataqualityMaxDraftColumnsForTest mirrors the component limit the handler
// enforces; the constant itself lives in component/dataquality.
const dataqualityMaxDraftColumnsForTest = dataquality.MaxDraftColumns

func TestParseQualitySuggestionJobName(t *testing.T) {
	t.Parallel()

	workspace, resourceID, err := parseQualitySuggestionJobName("workspaces/ws1/qualitySuggestionJobs/job1")
	require.NoError(t, err)
	require.Equal(t, "ws1", workspace)
	require.Equal(t, "job1", resourceID)
	require.Equal(t, "workspaces/ws1/qualitySuggestionJobs/job1", formatQualitySuggestionJobName(workspace, resourceID))

	for _, name := range []string{
		"",
		"qualitySuggestionJobs/job1",
		"workspaces/ws1/qualitySuggestions/job1",
		"workspaces/ws1/qualitySuggestionJobs/",
		"workspaces//qualitySuggestionJobs/job1",
	} {
		_, _, err := parseQualitySuggestionJobName(name)
		require.Error(t, err, "name %q", name)
	}
}

// The public job view is a projection of the stored row: states, suggestion names,
// epoch-millisecond timestamps and the counts the page shows.
func TestConvertQualitySuggestionJob(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	startedAt := createdAt.Add(time.Minute)
	finishedAt := startedAt.Add(time.Minute)
	createdBy := int32(7)
	job := &store.QualitySuggestionJobMessage{
		ID:            31,
		ResourceID:    "job1",
		Status:        storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED,
		Intent:        "email must not be empty",
		Language:      "en",
		Provider:      "profiles/openai",
		Model:         "gpt-4",
		PromptVersion: "dq.rule_draft.v1",
		Targets: &storepb.QualitySuggestionJobTargets{Targets: []*storepb.QualitySuggestionJobTarget{
			{
				TargetGuid:            "instances/inst-1;shop;public;orders",
				TargetType:            int32(storepb.MetaType_TABLE),
				Path:                  &storepb.QualityTargetPath{Instance: "inst-1", Database: "shop", Schema: "public", Object: "orders"},
				Columns:               []string{"email"},
				State:                 storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE,
				SuggestionResourceIds: []string{"sugg1", "sugg2"},
				StartedAtUnixMs:       startedAt.UnixMilli(),
				FinishedAtUnixMs:      finishedAt.UnixMilli(),
			},
			{
				TargetGuid: "instances/inst-1;shop;public;customers",
				TargetType: int32(storepb.MetaType_VIEW),
				State:      storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED,
				Error:      "the model returned nonsense",
			},
			{
				TargetGuid: "instances/inst-1;shop;public;pending",
				TargetType: int32(storepb.MetaType_TABLE),
				State:      storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
			},
		}},
		CreatedBy:  &createdBy,
		CreatedAt:  createdAt,
		StartedAt:  &startedAt,
		FinishedAt: &finishedAt,
		Error:      "",
	}

	converted := convertQualitySuggestionJob("ws1", job)
	require.Equal(t, "workspaces/ws1/qualitySuggestionJobs/job1", converted.GetName())
	require.Equal(t, v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED, converted.GetStatus())
	require.Equal(t, "email must not be empty", converted.GetIntent())
	require.Equal(t, "en", converted.GetLanguage())
	require.Equal(t, "profiles/openai", converted.GetProvider())
	require.Equal(t, "gpt-4", converted.GetModel())
	require.Equal(t, "dq.rule_draft.v1", converted.GetPromptVersion())
	require.Equal(t, "users/7", converted.GetCreatedBy())
	require.Equal(t, timestamppb.New(createdAt), converted.GetCreateTime())
	require.Equal(t, timestamppb.New(startedAt), converted.GetStartTime())
	require.Equal(t, timestamppb.New(finishedAt), converted.GetFinishTime())

	require.Equal(t, int32(3), converted.GetTargetCount())
	require.Equal(t, int32(2), converted.GetCompletedTargetCount(), "DONE and FAILED are completed; PENDING is not")
	require.Equal(t, int32(2), converted.GetSuggestionCount())
	require.Len(t, converted.GetTargets(), 3)

	first := converted.GetTargets()[0]
	require.Equal(t, "instances/inst-1;shop;public;orders", first.GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.MetaType_TABLE, first.GetTarget().GetMetaType())
	require.Equal(t, "orders", first.GetTarget().GetPath().GetObject())
	require.Equal(t, []string{"email"}, first.GetTarget().GetColumns())
	require.Equal(t, v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE, first.GetState())
	require.Equal(t, []string{
		"workspaces/ws1/qualitySuggestions/sugg1",
		"workspaces/ws1/qualitySuggestions/sugg2",
	}, first.GetSuggestions())
	require.Equal(t, timestamppb.New(startedAt), first.GetStartTime())
	require.Equal(t, timestamppb.New(finishedAt), first.GetFinishTime())

	second := converted.GetTargets()[1]
	require.Equal(t, v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED, second.GetState())
	require.Equal(t, "the model returned nonsense", second.GetError())
	require.Nil(t, second.GetStartTime(), "a target that never started has no start time")

	third := converted.GetTargets()[2]
	require.Equal(t, v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING, third.GetState())

	require.Nil(t, convertQualitySuggestionJob("ws1", nil))
}

// An unknown stored enum is not leaked through a numeric cast: it reads as
// UNSPECIFIED and is not counted as a completed target.
func TestConvertQualitySuggestionJobUnknownStates(t *testing.T) {
	t.Parallel()

	converted := convertQualitySuggestionJob("ws1", &store.QualitySuggestionJobMessage{
		ResourceID: "job1",
		Status:     storepb.QualitySuggestionJobStatus(99),
		Targets: &storepb.QualitySuggestionJobTargets{Targets: []*storepb.QualitySuggestionJobTarget{
			{TargetGuid: "g", TargetType: int32(storepb.MetaType_TABLE), State: storepb.QualitySuggestionJobTargetState(99)},
		}},
	})
	require.Equal(t, v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED, converted.GetStatus())
	require.Equal(t, v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED, converted.GetTargets()[0].GetState())
	require.Zero(t, converted.GetCompletedTargetCount())
	require.Equal(t, int32(1), converted.GetTargetCount())
}

func TestApplyQualitySuggestionJobListFilters(t *testing.T) {
	t.Parallel()

	find := &store.FindQualitySuggestionJobMessage{}
	require.NoError(t, applyQualitySuggestionJobListFilters(find, &v1pb.ListQualitySuggestionJobsRequest{
		Status: v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING,
	}))
	require.Equal(t, []storepb.QualitySuggestionJobStatus{
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING,
	}, find.Statuses)

	empty := &store.FindQualitySuggestionJobMessage{}
	require.NoError(t, applyQualitySuggestionJobListFilters(empty, &v1pb.ListQualitySuggestionJobsRequest{}))
	require.Empty(t, empty.Statuses)

	err := applyQualitySuggestionJobListFilters(&store.FindQualitySuggestionJobMessage{}, &v1pb.ListQualitySuggestionJobsRequest{
		Status: v1pb.QualitySuggestionJobStatus(99),
	})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// The create handler refuses a nil request before touching the store.
func TestCreateQualitySuggestionJobRequiresARequest(t *testing.T) {
	t.Parallel()

	service := &DataQualityService{}
	_, err := service.CreateQualitySuggestionJob(t.Context(), nil)
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// Snapshot validation runs before the first registry read for the checks that do
// not need one: count, shape and column-list bounds.
func TestSnapshotSuggestionJobTargetsRejectsBadShapes(t *testing.T) {
	t.Parallel()

	service := &DataQualityService{}
	_, err := service.snapshotSuggestionJobTargets(t.Context(), nil)
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	many := make([]*v1pb.QualityTarget, 0, qualitySuggestionJobTargetLimit+1)
	for index := 0; index <= qualitySuggestionJobTargetLimit; index++ {
		many = append(many, &v1pb.QualityTarget{
			MetaGuid: "instances/inst;shop;public;orders",
			MetaType: v1pb.MetaType_TABLE,
		})
	}
	_, err = service.snapshotSuggestionJobTargets(t.Context(), many)
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = service.snapshotSuggestionJobTargets(t.Context(), []*v1pb.QualityTarget{nil})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = service.snapshotSuggestionJobTargets(t.Context(), []*v1pb.QualityTarget{{MetaType: v1pb.MetaType_TABLE}})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = service.snapshotSuggestionJobTargets(t.Context(), []*v1pb.QualityTarget{{
		MetaGuid: "instances/inst;shop;public;orders",
	}})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	columns := make([]string, 0, dataqualityMaxDraftColumnsForTest+1)
	for index := 0; index <= dataqualityMaxDraftColumnsForTest; index++ {
		columns = append(columns, fmt.Sprintf("column_%d", index))
	}
	_, err = service.snapshotSuggestionJobTargets(t.Context(), []*v1pb.QualityTarget{{
		MetaGuid: "instances/inst;shop;public;orders",
		MetaType: v1pb.MetaType_TABLE,
		Columns:  columns,
	}})
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
