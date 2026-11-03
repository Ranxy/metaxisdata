package v1

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/qualityreport"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The requested window is clamped into the documented range: zero means the
// 30-day default, and the 90-day execution retention caps a longer one.
func TestQualityReportTrendDays(t *testing.T) {
	t.Parallel()

	require.Equal(t, 30, qualityReportTrendDays(0))
	require.Equal(t, 30, qualityReportTrendDays(-5))
	require.Equal(t, 1, qualityReportTrendDays(1))
	require.Equal(t, 30, qualityReportTrendDays(30))
	require.Equal(t, 90, qualityReportTrendDays(90))
	require.Equal(t, 90, qualityReportTrendDays(91))
	require.Equal(t, 90, qualityReportTrendDays(1000))
}

// A window of N days starts at the UTC midnight N-1 days before the report's
// day, so "today" is a full bucket.
func TestQualityReportWindowStart(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 31, 15, 4, 5, 0, time.UTC)
	require.Equal(t, time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC), qualityReportWindowStart(now, 1))
	require.Equal(t, time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC), qualityReportWindowStart(now, 30))
}

// The download name is the proto's documented shape.
func TestQualityReportFileName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "quality-report-20261031T120000Z.html",
		qualityReportFileName(time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC)))
}

// Enum keys drop the proto prefix and keep NOT_EVALUATED intact; the metadata
// object type is already bare.
func TestQualityReportEnumKey(t *testing.T) {
	t.Parallel()

	require.Equal(t, "FAIL", qualityReportEnumKey(storepb.QualityStatus_QUALITY_STATUS_FAIL))
	require.Equal(t, "NOT_EVALUATED", qualityReportEnumKey(storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED))
	require.Equal(t, "ROW_COUNT", qualityReportEnumKey(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT))
	require.Equal(t, "TABLE", qualityReportEnumKey(storepb.MetaType_TABLE))
}

// The report is assembled from the same workspace GetQualityTarget returns: the
// rule list carries the last result, the issue list carries the severity, and
// the failed-sample section carries the evidence.
func TestBuildQualityReport(t *testing.T) {
	t.Parallel()

	workspace := qualityReportWorkspaceFixture()
	points := qualityReportTrendPointsFixture()
	failed := qualityReportFailedExecutionsFixture()
	windowStart := time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC)
	generatedAt := time.Date(2026, time.October, 31, 9, 30, 0, 0, time.UTC)

	report := buildQualityReport(workspace, points, failed, nil, windowStart, generatedAt, "admin@example.com")

	require.Equal(t, "instance-1;shop;;orders", report.Target.GUID)
	require.Equal(t, "TABLE", report.Target.TypeKey)
	require.Equal(t, "instance-1/shop/orders", report.Target.Path)
	require.Equal(t, "instance-1", report.Target.Instance)
	require.Equal(t, "MYSQL", report.Target.Engine)
	require.False(t, report.Orphaned)
	require.Equal(t, 3, report.WindowDays)
	require.Equal(t, windowStart, report.WindowStart)
	require.Equal(t, "admin@example.com", report.GeneratedBy)

	// The six-state counts are the sum of the daily points.
	require.Equal(t, int32(9), report.Counts.Total)
	require.Equal(t, int32(5), report.Counts.Pass)
	require.Equal(t, int32(2), report.Counts.Fail)
	require.Equal(t, int32(1), report.Counts.Error)
	require.Equal(t, int32(1), report.Counts.Warn)
	require.Equal(t, int32(2), report.FailedCheckTotal, "every fail in the window is a failed check")
	require.Equal(t, int32(2), report.RuleCount)
	require.Equal(t, int32(1), report.EnabledRuleCount)
	require.NotNil(t, report.QualityScore, "the enabled rule's latest result is judged")
	require.InDelta(t, 100.0, *report.QualityScore, 1e-9, "a passing verdict-scored rule scores 100")
	require.Equal(t, int32(1), report.ScoredRuleCount)
	require.Equal(t, int32(0), report.UnscoredRuleCount)
	require.Equal(t, qualityreport.StatusPass, report.WorstStatusKey, "only the passing rule has a completed result")
	require.Len(t, report.Trend, 3)

	require.Len(t, report.Rules, 2)
	first := report.Rules[0]
	require.Equal(t, "orders row count", first.Title)
	require.Equal(t, "ROW_COUNT", first.TypeKey)
	require.Equal(t, "COMPLETENESS", first.DimensionKey)
	require.Equal(t, "ENABLED", first.StateKey)
	require.Equal(t, "users/7", first.Owner)
	require.Equal(t, "PASS", first.LastStatusKey)
	require.NotNil(t, first.ObservedValue)
	require.Equal(t, 3.0, *first.ObservedValue)
	require.Equal(t, "COUNT", first.ObservedKindKey)
	require.NotNil(t, first.Thresholds)
	require.Equal(t, int32(3), first.Revision)
	neverRun := report.Rules[1]
	require.Empty(t, neverRun.LastStatusKey)
	require.Equal(t, "email", neverRun.Column, "the column comes from the rule config")

	require.Len(t, report.OpenIssues, 1)
	require.Equal(t, "email not null", report.OpenIssues[0].RuleTitle)
	require.Equal(t, qualityreport.SeverityError, report.OpenIssues[0].SeverityKey)
	require.Equal(t, "OPEN", report.OpenIssues[0].StatusKey)
	require.Equal(t, int32(4), report.OpenIssues[0].Occurrences)

	require.Len(t, report.FailedChecks, 2)
	require.Equal(t, "email not null", report.FailedChecks[0].RuleTitle)
	require.Equal(t, "email", report.FailedChecks[0].Column)
	require.Len(t, report.FailedChecks[0].Samples, 1)
	require.Equal(t, "2", report.FailedChecks[0].Samples[0].Cells[0].Value)
	require.True(t, report.FailedChecks[0].Samples[0].Cells[1].IsNull)

	// The basis records the schedule, the revision range and the engine version.
	require.Equal(t, 2*time.Hour, report.Basis.ScheduleInterval)
	require.NotNil(t, report.Basis.NextRunTime)
	require.True(t, report.Basis.HasRevisionRange)
	require.Equal(t, int32(1), report.Basis.RuleRevisionMin)
	require.Equal(t, int32(3), report.Basis.RuleRevisionMax)
	require.Equal(t, []string{"dq.v1"}, report.Basis.EngineVersions)
	require.Equal(t, int32(3), report.Basis.TargetColumnCount)
}

// A cron target records its expression and resolved zone in the report basis, so a
// forwarded document still says how the checks are scheduled.
func TestQualityReportBasisCronSchedule(t *testing.T) {
	t.Parallel()

	nextRun := time.Date(2026, time.October, 31, 18, 0, 0, 0, time.UTC)
	workspace := qualityReportWorkspaceFixture()
	workspace.schedule = &store.QualityTargetScheduleMessage{
		TargetGUID: "instance-1;shop;;orders", TargetType: storepb.MetaType_TABLE,
		ScheduleInterval: 24 * time.Hour, Cron: "0 2 * * *", TimeZone: "Asia/Shanghai",
		NextRunAt: &nextRun,
	}
	basis := qualityReportBasis(workspace)
	require.Equal(t, "0 2 * * *", basis.Cron)
	require.Equal(t, "Asia/Shanghai", basis.TimeZone)
	require.Equal(t, 24*time.Hour, basis.ScheduleInterval, "the nominal cadence is still recorded")
	require.NotNil(t, basis.NextRunTime)
	require.Equal(t, nextRun, *basis.NextRunTime)
}

// The omitted-count denominator is the larger of the trend tally and the fetched
// list: a failure that completed between the two queries must never make the
// report claim to show more checks than its own total.
func TestQualityReportFailedCheckTotalBoundary(t *testing.T) {
	t.Parallel()

	workspace := qualityReportWorkspaceFixture()
	windowStart := time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC)
	generatedAt := time.Date(2026, time.October, 31, 9, 30, 0, 0, time.UTC)
	failedChecks := func(count int) []*store.QualityExecutionMessage {
		executions := make([]*store.QualityExecutionMessage, 0, count)
		for index := 0; index < count; index++ {
			executions = append(executions, &store.QualityExecutionMessage{
				ID: int64(index + 1), RuleID: 1,
				Status:     storepb.QualityStatus_QUALITY_STATUS_FAIL,
				FinishedAt: generatedAt,
			})
		}
		return executions
	}

	// The cap+1 boundary: 21 failures in the window, the newest 20 in the model.
	points := qualityReportTrendPointsFixture()
	points[2].Fail = 20 // total window fail = 1 + 0 + 20 = 21
	report := buildQualityReport(workspace, points, failedChecks(20), nil, windowStart, generatedAt, "")
	require.Len(t, report.FailedChecks, 20)
	require.Equal(t, int32(21), report.FailedCheckTotal)
	document, err := qualityreport.Render(qualityreport.LanguageEnglish, report)
	require.NoError(t, err)
	require.Contains(t, document, "Showing 20 of 21 failed checks in the window.")

	// The race case: the trend tally is stale (3) but the sample query already
	// saw 5, so the total follows the larger observation.
	points = qualityReportTrendPointsFixture()
	points[0].Fail = 2
	points[2].Fail = 1
	racy := buildQualityReport(workspace, points, failedChecks(5), nil, windowStart, generatedAt, "")
	require.Equal(t, int32(5), racy.FailedCheckTotal)
	require.Len(t, racy.FailedChecks, 5)
}

// The assembled report renders as one document in both languages; this is the
// only test that spans the handler mapping and the renderer.
func TestBuildQualityReportRenders(t *testing.T) {
	t.Parallel()

	report := buildQualityReport(qualityReportWorkspaceFixture(), qualityReportTrendPointsFixture(),
		qualityReportFailedExecutionsFixture(), nil, time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.October, 31, 9, 30, 0, 0, time.UTC), "admin@example.com")
	for _, language := range []string{"en-US", "zh-CN"} {
		document, err := qualityreport.Render(language, report)
		require.NoError(t, err, language)
		require.Contains(t, document, "instance-1/shop/orders")
		require.Contains(t, document, "orders row count")
		label := "Quality score"
		if language == "zh-CN" {
			label = "质量分"
		}
		require.Containsf(t, document, `<div class="value">100.0</div><div class="label">`+label+`</div>`,
			"%s: the report renders the target's quality score card, not just any 100.0", language)
		require.Contains(t, document, "<svg")
	}
}

// An orphaned target has no resolution and no live columns, so the report keeps
// the path snapshot, marks itself orphaned and still renders the rules.
func TestBuildQualityReportOrphaned(t *testing.T) {
	t.Parallel()

	workspace := qualityReportWorkspaceFixture()
	workspace.resolution = nil
	workspace.orphanedReason = "the target table was dropped"
	report := buildQualityReport(workspace, qualityReportTrendPointsFixture(), nil, nil,
		time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.October, 31, 9, 30, 0, 0, time.UTC), "")
	require.True(t, report.Orphaned)
	require.Equal(t, "the target table was dropped", report.OrphanedReason)
	require.Equal(t, "instance-1/shop/orders", report.Target.Path, "the rule snapshot still describes the target")
	require.Equal(t, "instance-1", report.Target.Instance, "the instance falls back to the GUID's first segment")
	require.Empty(t, report.Target.Engine)
	require.Zero(t, report.Basis.TargetColumnCount)
	require.Len(t, report.Rules, 2)
}

// An archived rule is not part of the workspace, so a failed execution it left
// behind is still identified by its row id rather than left anonymous.
func TestQualityReportFailedCheckTitleFallsBackToTheRuleID(t *testing.T) {
	t.Parallel()

	execution := &store.QualityExecutionMessage{RuleID: 42}
	require.Equal(t, "#42", qualityReportFailedCheckTitle(execution, nil))
	require.Equal(t, "archived rule", qualityReportFailedCheckTitle(execution, map[int64]*store.QualityRuleMessage{
		42: {ID: 42, Title: "archived rule"},
	}))
}

// The attribution is the display name only: the report is forwardable, so a
// contact address stays in the audit row. Without a name it falls back to the
// principal resource name.
func TestQualityReportGeneratedBy(t *testing.T) {
	t.Parallel()

	require.Empty(t, qualityReportGeneratedBy(context.Background()))

	ctx := context.WithValue(context.Background(), common.UserContextKey, &store.UserMessage{
		ID: 7, Name: "Alice", Email: "alice@example.com",
	})
	require.Equal(t, "Alice", qualityReportGeneratedBy(ctx))

	ctx = context.WithValue(context.Background(), common.UserContextKey, &store.UserMessage{ID: 7, Email: "alice@example.com"})
	require.Equal(t, "users/7", qualityReportGeneratedBy(ctx), "an email alone is not the display name")

	ctx = context.WithValue(context.Background(), common.UserContextKey, &store.UserMessage{ID: 7})
	require.Equal(t, "users/7", qualityReportGeneratedBy(ctx))
}

// qualityReportPtr is the generic pointer helper the report fixtures use.
func qualityReportPtr[T any](value T) *T { return &value }

// qualityReportWorkspaceFixture is one resolved target with a passing rule, a
// paused rule that never ran and one open issue.
func qualityReportWorkspaceFixture() *qualityTargetWorkspace {
	targetPath := &storepb.QualityTargetPath{
		Instance: "instance-1", Database: "shop", Object: "orders",
	}
	minRows := int64(1)
	nextRun := time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC)
	return &qualityTargetWorkspace{
		targetGUID: "instance-1;shop;;orders",
		metaType:   storepb.MetaType_TABLE,
		path:       targetPath,
		rules: []*store.QualityRuleMessage{
			{
				ID: 1, ResourceID: "rule-1", Title: "orders row count",
				RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
				Dimension:  storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
				Severity:   storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
				State:      storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
				TargetGUID: "instance-1;shop;;orders", TargetType: storepb.MetaType_TABLE,
				TargetPath: targetPath,
				Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
					RowCount: &storepb.RowCountConfig{MinRows: &minRows},
				}},
				OwnerID:  qualityReportPtr(int32(7)),
				Revision: 3,
			},
			{
				ID: 2, ResourceID: "rule-2", Title: "email not null",
				RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
				Dimension:  storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
				Severity:   storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
				State:      storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED,
				TargetGUID: "instance-1;shop;;orders", TargetType: storepb.MetaType_TABLE,
				TargetPath: targetPath,
				Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
					NullPercent: &storepb.NullPercentConfig{Column: "email"},
				}},
				Revision: 1,
			},
		},
		latest: map[int64]*store.QualityExecutionMessage{
			1: {
				RuleID:            1,
				TargetGUID:        "instance-1;shop;;orders",
				TargetType:        storepb.MetaType_TABLE,
				Status:            storepb.QualityStatus_QUALITY_STATUS_PASS,
				ObservedValue:     qualityReportPtr(3.0),
				ObservedKind:      storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
				Thresholds:        &storepb.QualityThresholds{Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN},
				FinishedAt:        time.Date(2026, time.October, 31, 8, 0, 0, 0, time.UTC),
				RuleEngineVersion: "dq.v1",
			},
		},
		openIssues: []*store.QualityIssueMessage{
			{
				RuleID: 2, TargetGUID: "instance-1;shop;;orders", TargetType: storepb.MetaType_TABLE,
				Severity:        storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
				Status:          storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
				FirstSeenAt:     time.Date(2026, time.October, 30, 7, 0, 0, 0, time.UTC),
				LastSeenAt:      time.Date(2026, time.October, 31, 7, 0, 0, 0, time.UTC),
				OccurrenceCount: 4,
			},
		},
		resolution: &dataquality.Resolution{
			Target: dataquality.ResolvedTarget{
				Target:             dataquality.Target{GUID: "instance-1;shop;;orders", MetaType: storepb.MetaType_TABLE, Path: targetPath},
				Engine:             storepb.Engine_MYSQL,
				InstanceResourceID: "instance-1",
				Columns: []dataquality.ColumnInfo{
					{Name: "id", Type: "int", Nullable: false},
					{Name: "email", Type: "varchar", Nullable: true},
					{Name: "amount", Type: "decimal", Nullable: false},
				},
			},
			Instance: &store.InstanceMessage{ResourceID: "instance-1", Metadata: &storepb.Instance{Engine: storepb.Engine_MYSQL}},
		},
		schedule: &store.QualityTargetScheduleMessage{
			TargetGUID: "instance-1;shop;;orders", TargetType: storepb.MetaType_TABLE,
			ScheduleInterval: 2 * time.Hour, NextRunAt: &nextRun,
		},
	}
}

// qualityReportTrendPointsFixture is three days with a quiet middle day.
func qualityReportTrendPointsFixture() []*v1pb.QualityTrendPoint {
	return []*v1pb.QualityTrendPoint{
		{Date: "2026-10-29", Total: 3, Pass: 2, Fail: 1},
		{Date: "2026-10-30", Total: 0},
		{Date: "2026-10-31", Total: 6, Pass: 3, Warn: 1, Fail: 1, Error: 1},
	}
}

// qualityReportFailedExecutionsFixture is two failed checks; the first carries a
// sample row with a NULL cell.
func qualityReportFailedExecutionsFixture() []*store.QualityExecutionMessage {
	nullMarker := true
	return []*store.QualityExecutionMessage{
		{
			ID: 101, RuleID: 2, TargetGUID: "instance-1;shop;;orders", TargetType: storepb.MetaType_TABLE,
			TargetPath: &storepb.QualityTargetPath{Instance: "instance-1", Database: "shop", Object: "orders", Column: "email"},
			Status:     storepb.QualityStatus_QUALITY_STATUS_FAIL,
			FailedRows: 2, TotalRows: 10,
			FinishedAt: time.Date(2026, time.October, 31, 7, 0, 0, 0, time.UTC),
			Sample: &storepb.QualityExecutionEvidence{Samples: []*storepb.FailureSample{
				{Columns: []*storepb.ColumnValue{
					{Column: "id", Value: "2"},
					{Column: "email", IsNull: &nullMarker},
				}},
			}},
		},
		{
			ID: 100, RuleID: 99, TargetGUID: "instance-1;shop;;orders", TargetType: storepb.MetaType_TABLE,
			TargetPath: &storepb.QualityTargetPath{Instance: "instance-1", Database: "shop", Object: "orders", Column: "amount"},
			Status:     storepb.QualityStatus_QUALITY_STATUS_FAIL,
			FailedRows: 1, TotalRows: 10,
			FinishedAt: time.Date(2026, time.October, 31, 6, 0, 0, 0, time.UTC),
			ErrorCode:  "SQL_SYNTAX",
		},
	}
}

// The impact answer maps onto the renderer's model with the same monitoring
// three-state the panel uses — no summary means unmonitored, and an all-paused
// target stays a separate state — and root causes are only carried when a failure
// anchored the window.
func TestQualityReportImpactOf(t *testing.T) {
	t.Parallel()

	anchor := time.Date(2026, time.October, 31, 7, 0, 0, 0, time.UTC)
	lastRun := time.Date(2026, time.October, 31, 8, 0, 0, 0, time.UTC)
	changedAt := time.Date(2026, time.October, 30, 22, 0, 0, 0, time.UTC)
	response := &v1pb.GetQualityTargetImpactResponse{
		DownstreamAssets: []*v1pb.QualityImpactAsset{
			{
				Target: &v1pb.QualityTarget{
					MetaGuid: "instance-1;shop;public;order_summary",
					MetaType: v1pb.MetaType_VIEW,
					Path: &v1pb.QualityTargetPath{
						Instance: "instance-1", Database: "shop", Schema: "public", Object: "order_summary",
					},
				},
				Distance: 1,
				Summary: &v1pb.QualityTargetSummary{
					EnabledRuleCount: 2,
					WorstStatus:      v1pb.QualityStatus_QUALITY_STATUS_FAIL,
					OpenIssueCount:   1,
					LastRunTime:      timestamppb.New(lastRun),
				},
			},
			{
				Target: &v1pb.QualityTarget{
					MetaGuid: "instance-1;shop;public;orders_report",
					MetaType: v1pb.MetaType_TABLE,
					Path: &v1pb.QualityTargetPath{
						Instance: "instance-1", Database: "shop", Schema: "public", Object: "orders_report",
					},
				},
				Distance: 2,
			},
			{
				// Monitored but never judged: the report must say "never run", not
				// leak the raw UNSPECIFIED enum.
				Target: &v1pb.QualityTarget{
					MetaGuid: "instance-1;shop;public;never_run",
					MetaType: v1pb.MetaType_TABLE,
					Path: &v1pb.QualityTargetPath{
						Instance: "instance-1", Database: "shop", Schema: "public", Object: "never_run",
					},
				},
				Distance: 2,
				Summary: &v1pb.QualityTargetSummary{
					EnabledRuleCount: 1,
					WorstStatus:      v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
				},
			},
		},
		RootCauses: []*v1pb.QualityRootCauseCandidate{{
			Target: &v1pb.QualityTarget{
				MetaGuid: "instance-1;shop;public;raw_orders",
				MetaType: v1pb.MetaType_TABLE,
				Path: &v1pb.QualityTargetPath{
					Instance: "instance-1", Database: "shop", Schema: "public", Object: "raw_orders",
				},
			},
			Distance:              -1,
			FailingRuleCount:      2,
			FailingExecutionCount: 5,
			LatestFailureTime:     timestamppb.New(anchor),
			StructureChangedTime:  timestamppb.New(changedAt),
		}},
		OmittedDownstreamCount: 1,
		DownstreamTruncated:    true,
		OmittedRootCauseCount:  2,
		UpstreamTruncated:      true,
		WindowStart:            timestamppb.New(anchor.Add(-24 * time.Hour)),
		WindowEnd:              timestamppb.New(anchor),
	}

	impact := qualityReportImpactOf(response, &anchor)
	require.Len(t, impact.Downstream, 3)
	require.Equal(t, "instance-1/shop/public/order_summary", impact.Downstream[0].Path)
	require.Equal(t, "VIEW", impact.Downstream[0].TypeKey)
	require.True(t, impact.Downstream[0].Monitored)
	require.False(t, impact.Downstream[0].AllPaused)
	require.Equal(t, "FAIL", impact.Downstream[0].WorstStatusKey)
	require.Equal(t, int32(1), impact.Downstream[0].OpenIssueCount)
	require.Equal(t, lastRun, *impact.Downstream[0].LastRunTime)

	require.False(t, impact.Downstream[1].Monitored)
	require.Nil(t, impact.Downstream[1].LastRunTime)

	require.True(t, impact.Downstream[2].Monitored)
	require.Empty(t, impact.Downstream[2].WorstStatusKey, "a never-run asset must not map the raw UNSPECIFIED enum")

	require.Equal(t, int32(1), impact.DownstreamOmitted)
	require.True(t, impact.DownstreamTruncated)
	require.True(t, impact.UpstreamTruncated)
	require.Equal(t, int32(2), impact.RootCausesOmitted)
	require.Len(t, impact.RootCauses, 1)
	require.Equal(t, "instance-1/shop/public/raw_orders", impact.RootCauses[0].Path)
	require.Equal(t, int32(-1), impact.RootCauses[0].Distance)
	require.Equal(t, int32(2), impact.RootCauses[0].FailingRuleCount)
	require.Equal(t, int32(5), impact.RootCauses[0].FailingExecutionCount)
	require.Equal(t, anchor, *impact.RootCauses[0].LatestFailureTime)
	require.Equal(t, changedAt, *impact.RootCauses[0].StructureChangedTime)
	require.Equal(t, anchor.Add(-24*time.Hour), impact.WindowStart)
	require.Equal(t, anchor, impact.WindowEnd)

	// Without an anchor the root causes are dropped: a window ending "now" does not
	// explain a failure the report cannot point at.
	unanchored := qualityReportImpactOf(response, nil)
	require.Empty(t, unanchored.RootCauses)
	require.Zero(t, unanchored.WindowStart)
	require.Len(t, unanchored.Downstream, 3)
}
