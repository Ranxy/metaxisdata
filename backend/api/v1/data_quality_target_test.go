package v1

import (
	"testing"

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

// A summary keeps the snapshot path and the counts the directory renders.
func TestConvertQualityTargetSummary(t *testing.T) {
	t.Parallel()

	summary := convertQualityTargetSummary(&store.QualityTargetMessage{
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
}
