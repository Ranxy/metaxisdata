//go:build integration

package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// This scenario drives P1-7 through the public API. The fixture's two synced tables
// are wired with one recorded lineage edge (the analyzer and the ingestion path
// write the same shape, and this test does not need a SQL analysis to prove the read
// model), an upstream check is made to fail, and the impact read answers "what does
// this target feed" and "what may explain its failure". The consumption coverage is
// a workspace-wide read, so this test does not run in parallel: a snapshot other
// scenarios keep editing could not be asserted.
func TestDataQualityImpactRealServerIntegration(t *testing.T) {
	fixture := setupQualityMySQLFixture(t, true)
	ctx := fixture.ctx
	guidPrefix := fmt.Sprintf("%s;%s", fixture.instanceID, fixture.sourceDatabase)
	ordersGUID := waitForMetaGUIDByName(ctx, t, fixture.env, guidPrefix, storepb.MetaType_TABLE, "orders")

	require.NoError(t, replaceQualityImpactLineageEdges(ctx, fixture, fixture.usersGUID, ordersGUID))
	t.Cleanup(func() {
		// The edge belongs to this test's database; removing it keeps the shared
		// fixture's lineage table from growing with rows the next scenario did not
		// write.
		_ = fixture.env.Store.BatchReplaceColumnLineage(context.Background(), fixture.usersGUID, storepb.MetaType_TABLE, nil)
	})

	// This scenario creates workspace-global rows (rules, and the issue a failing
	// run opens) even though its source database is dropped after the test. It is
	// one of the few non-parallel scenarios, so it runs before the others can
	// pause: archiving its rules keeps a later scenario that lists the whole
	// workspace (the target directory asserts one row per target) from seeing
	// them.
	archiveRule := func(rule *v1pb.QualityRule) {
		t.Cleanup(func() {
			_, err := fixture.dataQuality.DeleteQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(),
				&v1pb.DeleteQualityRuleRequest{Name: rule.GetName()}))
			require.NoError(t, err, "archive the scenario's rule %s", rule.GetName())
		})
	}

	// The downstream table is watched, the upstream one is not yet.
	ordersRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		ordersGUID, qualityRowCountConfig(1))
	archiveRule(ordersRule)

	before := fixture.consumptionCoverage(t, 100)
	require.Contains(t, qualityConsumptionGapGUIDs(before), fixture.usersGUID,
		"the upstream table is read by the downstream one and has no enabled check")
	require.Equal(t, before.GetConsumedAssetCount()-before.GetGapCount(), before.GetMonitoredAssetCount())
	require.GreaterOrEqual(t, before.GetConsumedAssetCount(), int32(1))

	// An upstream failure inside the window is what the root-cause read explains.
	usersRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_CRITICAL,
		fixture.usersGUID, qualityRowCountConfig(100000))
	archiveRule(usersRule)
	execution := fixture.runToCompletion(t, usersRule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, execution.GetOutcome())

	after := fixture.consumptionCoverage(t, 100)
	require.NotContains(t, qualityConsumptionGapGUIDs(after), fixture.usersGUID,
		"an enabled check moves the consumed asset out of the gaps")
	require.Less(t, after.GetGapCount(), before.GetGapCount())

	// Downstream: the target's data reaches the watched table, and the answer carries
	// that table's monitoring state through the same read model the asset page uses.
	downstream := fixture.targetImpact(t, fixture.usersGUID, 2, 24)
	require.Len(t, downstream.GetDownstreamAssets(), 1)
	asset := downstream.GetDownstreamAssets()[0]
	require.Equal(t, ordersGUID, asset.GetTarget().GetMetaGuid())
	require.Equal(t, int32(1), asset.GetDistance())
	require.Equal(t, "orders", asset.GetTarget().GetPath().GetObject())
	require.NotNil(t, asset.GetSummary(), "the downstream table is monitored")
	require.Equal(t, int32(1), asset.GetSummary().GetEnabledRuleCount())
	require.Equal(t, int32(1), downstream.GetDepthReached())
	require.Empty(t, downstream.GetRootCauses(), "the upstream table has no upstream of its own")
	require.False(t, downstream.GetDownstreamTruncated())

	// Upstream: the failing check is the root-cause candidate for the downstream
	// table, with the window and the execution link that let a reviewer check it.
	upstream := fixture.targetImpact(t, ordersGUID, 2, 24)
	require.Empty(t, upstream.GetDownstreamAssets())
	require.Len(t, upstream.GetRootCauses(), 1)
	cause := upstream.GetRootCauses()[0]
	require.Equal(t, fixture.usersGUID, cause.GetTarget().GetMetaGuid())
	require.Equal(t, int32(-1), cause.GetDistance())
	require.Equal(t, int32(1), cause.GetFailingRuleCount())
	require.Equal(t, int32(1), cause.GetFailingExecutionCount())
	require.Equal(t, []string{execution.GetName()}, cause.GetRecentFailingExecutions())
	require.NotNil(t, cause.GetLatestFailureTime())
	require.Nil(t, cause.GetStructureChangedTime(), "the table's shape did not change in the window")
	require.NotNil(t, upstream.GetWindowStart())
	require.NotNil(t, upstream.GetWindowEnd())

	// The report carries the same impact: its target feeds the watched table, and the
	// section states that no upstream check explains the users failure.
	report := fixture.qualityReport(t, fixture.usersGUID, v1pb.MetaType_TABLE)
	for _, want := range []string{
		"Impact",
		"Downstream assets",
		"orders",
		"No upstream check failed or changed structure in the window.",
		"This is not a complete list of consumers.",
	} {
		require.Contains(t, report.GetHtmlContent(), want)
	}

	// Keep the variable read: the rule is created for its side effect on coverage.
	require.NotEmpty(t, ordersRule.GetName())
}

// replaceQualityImpactLineageEdges records one column-level edge from source to
// target. It is the write the analyzer performs for a view definition and the
// ingestion revalidation performs for a reported statement, restricted to the one
// fact this scenario needs.
func replaceQualityImpactLineageEdges(ctx context.Context, fixture *qualityFixture, sourceGUID, targetGUID string) error {
	return fixture.env.Store.BatchReplaceColumnLineage(ctx, sourceGUID, storepb.MetaType_TABLE, []*store.ColumnLineage{{
		SourceGUID:   sourceGUID,
		SourceColumn: "id",
		SourceType:   storepb.MetaType_TABLE,
		TargetGUID:   targetGUID,
		TargetColumn: "user_id",
		TargetType:   storepb.MetaType_TABLE,
		// The writers always pass a non-nil list; the store's insert renders a nil
		// one as SQL NULL, which the NOT NULL column refuses.
		Transformation: []model.Transformation{},
	}})
}

// consumptionCoverage reads the workspace's consumption coverage.
func (f *qualityFixture) consumptionCoverage(t *testing.T, pageSize int32) *v1pb.GetQualityConsumptionCoverageResponse {
	t.Helper()

	response, err := f.dataQuality.GetQualityConsumptionCoverage(f.ctx, withToken(f.env.AdminToken(),
		&v1pb.GetQualityConsumptionCoverageRequest{Parent: "workspaces/-", PageSize: pageSize}))
	require.NoError(t, err, "read the consumption coverage")
	return response.Msg
}

// targetImpact reads one target's lineage impact.
func (f *qualityFixture) targetImpact(t *testing.T, targetGUID string, depth, windowHours int32) *v1pb.GetQualityTargetImpactResponse {
	t.Helper()

	response, err := f.dataQuality.GetQualityTargetImpact(f.ctx, withToken(f.env.AdminToken(),
		&v1pb.GetQualityTargetImpactRequest{
			Parent: "workspaces/-",
			Target: &v1pb.QualityTargetKey{TargetGuid: targetGUID, MetaType: v1pb.MetaType_TABLE},
			Depth:  depth, WindowHours: windowHours,
		}))
	require.NoError(t, err, "read the target impact")
	return response.Msg
}

// qualityReport renders one target's report.
func (f *qualityFixture) qualityReport(t *testing.T, targetGUID string, metaType v1pb.MetaType) *v1pb.ExportQualityReportResponse {
	t.Helper()

	response, err := f.dataQuality.ExportQualityReport(f.ctx, withToken(f.env.AdminToken(),
		&v1pb.ExportQualityReportRequest{
			Parent: "workspaces/-", TargetGuid: targetGUID, MetaType: metaType,
			TrendDays: 1, Language: "en-US",
		}))
	require.NoError(t, err, "render the quality report")
	return response.Msg
}

// qualityConsumptionGapGUIDs lists the gaps' target GUIDs.
func qualityConsumptionGapGUIDs(coverage *v1pb.GetQualityConsumptionCoverageResponse) []string {
	guids := make([]string, 0, len(coverage.GetGaps()))
	for _, gap := range coverage.GetGaps() {
		guids = append(guids, gap.GetTarget().GetMetaGuid())
	}
	return guids
}
