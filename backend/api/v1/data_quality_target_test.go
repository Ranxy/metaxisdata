package v1

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// An unknown or unspecified order falls back to attention: the directory always
// has a deterministic order, even for an older client.
func TestQualityTargetOrderFromV1(t *testing.T) {
	t.Parallel()

	require.Equal(t, store.QualityTargetOrderAttention, qualityTargetOrderFromV1(v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_UNSPECIFIED))
	require.Equal(t, store.QualityTargetOrderAttention, qualityTargetOrderFromV1(v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_ATTENTION))
	require.Equal(t, store.QualityTargetOrderName, qualityTargetOrderFromV1(v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_NAME))
	require.Equal(t, store.QualityTargetOrderRuleCount, qualityTargetOrderFromV1(v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_RULE_COUNT))
	require.Equal(t, store.QualityTargetOrderLastRun, qualityTargetOrderFromV1(v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_LAST_RUN))
	require.Equal(t, store.QualityTargetOrderAttention, qualityTargetOrderFromV1(v1pb.QualityTargetOrder(99)))
}

// The target header's worst status must agree with the store aggregate: ERROR
// outranks FAIL, and a status that observed nothing ranks below PASS.
func TestWorstQualityStatus(t *testing.T) {
	t.Parallel()

	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_ERROR,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_FAIL, v1pb.QualityStatus_QUALITY_STATUS_ERROR))
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_PASS, v1pb.QualityStatus_QUALITY_STATUS_FAIL))
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_WARN,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, v1pb.QualityStatus_QUALITY_STATUS_WARN))
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_SKIPPED, v1pb.QualityStatus_QUALITY_STATUS_PASS))
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED))
	// NOT_EVALUATED outranks SKIPPED: the check ran and had nothing to judge,
	// whereas a skipped check never ran.
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
		worstQualityStatus(v1pb.QualityStatus_QUALITY_STATUS_SKIPPED, v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED))
}

// The worst-status filter accepts the six real verdicts, keeps UNSPECIFIED as "no
// filter", and refuses a value outside the enum instead of silently matching the
// "never run" rank (the ListQualityExecutions list already behaves this way).
func TestQualityTargetWorstStatusFromV1(t *testing.T) {
	t.Parallel()

	for _, status := range []v1pb.QualityStatus{
		v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
		v1pb.QualityStatus_QUALITY_STATUS_PASS,
		v1pb.QualityStatus_QUALITY_STATUS_WARN,
		v1pb.QualityStatus_QUALITY_STATUS_FAIL,
		v1pb.QualityStatus_QUALITY_STATUS_ERROR,
		v1pb.QualityStatus_QUALITY_STATUS_SKIPPED,
		v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
	} {
		mapped, err := qualityTargetWorstStatusFromV1(status)
		require.NoError(t, err, status.String())
		require.Equal(t, storepb.QualityStatus(status), mapped)
	}

	_, err := qualityTargetWorstStatusFromV1(v1pb.QualityStatus(99))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// The list filters map resource names and raw ids alike; the rule-only filters
// (owner, rule type) have no target meaning and stay unset.
func TestApplyQualityTargetListFilters(t *testing.T) {
	t.Parallel()

	find := &store.FindQualityTargetMessage{}
	applyQualityTargetListFilters(find, &v1pb.ListQualityTargetsRequest{
		Instance: "instances/instance-1",
		Database: "shop",
		Schema:   "public",
		Table:    "orders",
		Search:   "  orders  ",
	})
	require.Equal(t, "instance-1", find.Instance)
	require.Equal(t, "shop", find.Database)
	require.Equal(t, "public", find.Schema)
	require.Equal(t, "orders", find.TablePrefix)
	require.NotNil(t, find.Search)
	require.Equal(t, "orders", *find.Search)
}

// A summary keeps the snapshot path, the counts the directory renders, and the
// target's newest run slice.
func TestConvertQualityTargetSummary(t *testing.T) {
	t.Parallel()

	finishedAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	nextRun := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	summary := convertQualityTargetSummary("ws1", &store.QualityTargetMessage{
		GUID:              "instance-1;shop;public;orders",
		Type:              storepb.MetaType_TABLE,
		Path:              &storepb.QualityTargetPath{Instance: "instance-1", Database: "shop", Schema: "public", Object: "orders"},
		RuleCount:         3,
		EnabledCount:      2,
		PausedCount:       1,
		ColumnCount:       2,
		OpenIssueCount:    1,
		PendingDraftCount: 4,
		PendingLabelCount: 5,
		WorstStatus:       storepb.QualityStatus_QUALITY_STATUS_FAIL,
		SchemaChanged:     true,
		Orphaned:          true,
		RunCount:          2,
		ScheduleInterval:  2 * time.Hour,
		NextRunAt:         &nextRun,
		LatestRun: &store.QualityTargetRunSummaryMessage{
			TargetGUID: "instance-1;shop;public;orders",
			TargetType: storepb.MetaType_TABLE,
			Run: &store.QualityRunMessage{
				ID:              42,
				TargetGUID:      "instance-1;shop;public;orders",
				TargetType:      storepb.MetaType_TABLE,
				Trigger:         storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
				Status:          storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
				Outcome:         storepb.QualityStatus_QUALITY_STATUS_FAIL,
				Counts:          &storepb.QualityStatusCounts{Total: 3, Fail: 1, Pass: 2},
				RuleRevisionMin: 1,
				RuleRevisionMax: 2,
				StartedAt:       finishedAt.Add(-time.Minute),
			},
		},
	})
	require.Equal(t, "orders", summary.GetTarget().GetPath().GetObject())
	require.Equal(t, v1pb.MetaType_TABLE, summary.GetTarget().GetMetaType())
	require.Equal(t, int32(3), summary.GetRuleCount())
	require.Equal(t, int32(2), summary.GetEnabledRuleCount())
	require.Equal(t, int32(1), summary.GetPausedRuleCount())
	require.Equal(t, int32(2), summary.GetColumnCount())
	require.Equal(t, int32(1), summary.GetOpenIssueCount())
	require.Equal(t, int32(4), summary.GetPendingDraftCount())
	require.Equal(t, int32(5), summary.GetPendingLabelCount())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, summary.GetWorstStatus())
	require.True(t, summary.GetSchemaChanged())
	require.True(t, summary.GetOrphaned())
	require.Equal(t, int32(2), summary.GetRunCount())
	require.NotNil(t, summary.GetLatestRun())
	require.Equal(t, "workspaces/ws1/qualityRuns/42", summary.GetLatestRun().GetName())
	require.Equal(t, "instance-1;shop;public;orders", summary.GetLatestRun().GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL, summary.GetLatestRun().GetTrigger())
	require.Equal(t, v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, summary.GetLatestRun().GetState())
	require.Equal(t, int32(3), summary.GetLatestRun().GetCounts().GetTotal())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, summary.GetLatestRun().GetOutcome())
	require.Equal(t, int32(1), summary.GetLatestRun().GetRuleRevisionMin())
	require.Equal(t, int32(2), summary.GetLatestRun().GetRuleRevisionMax())
	require.Equal(t, 2*time.Hour, summary.GetSchedule().GetScheduleInterval().AsDuration())
	require.Equal(t, nextRun, summary.GetSchedule().GetNextRunTime().AsTime())

	// A target that has never run carries zero values rather than an empty slice.
	never := convertQualityTargetSummary("ws1", &store.QualityTargetMessage{
		GUID: "instance-1;shop;public;orders",
	})
	require.Equal(t, int32(0), never.GetRunCount())
	require.Nil(t, never.GetLatestRun())
	require.Nil(t, never.GetSchedule(), "a target without a schedule row has no schedule")
}
