package agentreport

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestRenderLocalizesTheBody pins the report contract: the language selects the
// localized skeleton, the period and every count come from the snapshot, and the
// attention section fires only for the categories that are non-zero.
func TestRenderLocalizesTheBody(t *testing.T) {
	t.Parallel()

	snapshot := &store.QualityAgentReportSnapshot{
		Scope: []string{"instances/i/databases/d"}, TargetCount: 4, CoveredTargetCount: 2,
		RuleCount: 3, ExecutionCount: 5, PassedCount: 4, FailedCount: 1, OpenIssueCount: 1,
	}
	start := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	english := Render(snapshot, start, end, time.UTC, "en-US")
	require.Contains(t, english, "Quality patrol report")
	require.Contains(t, english, "Period: 2026-11-01 00:00 to 2026-11-02 00:00 (UTC)")
	require.Contains(t, english, "- Targets discovered: 4")
	require.Contains(t, english, "- Targets with coverage: 2")
	require.Contains(t, english, "- Enabled checks: 3")
	require.Contains(t, english, "- Checks run: 5 (pass 4, warn 0, fail 1, error 0)")
	require.Contains(t, english, "- Open issues: 1")
	require.Contains(t, english, "- 1 check(s) failed and 0 errored in this period")
	require.NotContains(t, english, "- Nothing needs a decision in this period.")

	chinese := Render(snapshot, start, end, time.UTC, "zh-CN")
	require.Contains(t, chinese, "质量巡检报告")
	require.Contains(t, chinese, "- 已发现目标：4")
	require.Contains(t, chinese, "- 未解决问题：1")
	require.NotEqual(t, english, chinese)

	// The nothing-to-decide line belongs to a period with nothing waiting; the repaired section still
	// states its own honest zero.
	clean := &store.QualityAgentReportSnapshot{}
	cleanBody := Render(clean, start, end, time.UTC, "en-US")
	require.Contains(t, cleanBody, "- Nothing needs a decision in this period.")
	require.Contains(t, cleanBody, "- No work item was settled by the agent in this period.")
}

// A patrol report answers what the agent fixed and what is waiting for a person. The two sections
// have to carry the work-item counts, because a report that only lists executions cannot tell an
// owner whether delegating the database is working.
func TestRenderNamesWhatWasFixedAndWhatIsWaiting(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	snapshot := &store.QualityAgentReportSnapshot{
		TargetCount:          2,
		RepairedCount:        3,
		EscalatedCount:       1,
		OpenCaseCount:        2,
		PendingProposalCount: 4,
		RepairedByClass: []store.QualityAgentCaseClassCount{
			{Class: "CONFIG_VALUE_INVALID", Count: 2},
			{Class: "STRUCTURE_DRIFT", Count: 1},
		},
	}
	english := Render(snapshot, start, end, time.UTC, "en-US")
	require.Contains(t, english, "## What the agent fixed")
	require.Contains(t, english, "- 3 work item(s) closed by the agent itself: CONFIG_VALUE_INVALID 2, STRUCTURE_DRIFT 1.")
	require.Contains(t, english, "## What is waiting for you")
	require.Contains(t, english, "- 1 work item(s) were escalated or frozen for a decision.")
	require.Contains(t, english, "- 2 work item(s) are still in progress.")
	require.Contains(t, english, "- 4 proposal(s) wait for a decision.")
	require.NotContains(t, english, "- Nothing needs a decision in this period.")

	chinese := Render(snapshot, start, end, time.UTC, "zh-CN")
	require.Contains(t, chinese, "## Agent 修好了什么")
	require.Contains(t, chinese, "CONFIG_VALUE_INVALID 2")
	require.Contains(t, chinese, "## 等你决定")

	// The same snapshot renders byte-identically, so a retried report does not churn the body.
	require.Equal(t, english, Render(snapshot, start, end, time.UTC, "en-US"))
}

// An empty target is a fact the report states once, not a decision per silenced check: the
// escalation path used to hand the owner one work item per check on a table that holds no rows.
func TestRenderStatesEmptyTargetsAsAFact(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	snapshot := &store.QualityAgentReportSnapshot{
		TargetCount: 2, RuleCount: 9, RepairedCount: 9,
		EmptyTargets: []store.QualityAgentTargetCaseCount{
			{TargetGUID: "test-pg-1;gug;public;stock_alert", Cases: 8},
			{TargetGUID: "test-pg-1;gug;;audit_log", Cases: 1},
		},
	}

	english := Render(snapshot, start, end, time.UTC, "en-US")
	require.Contains(t, english, "- 2 target(s) could not be judged at all because they hold no rows, silencing 9 check(s)")
	require.Contains(t, english, "public.stock_alert (8)")
	require.Contains(t, english, "audit_log (1)", "a MySQL-wire path names the database, not an empty schema")

	chinese := Render(snapshot, start, end, time.UTC, "zh-CN")
	require.Contains(t, chinese, "- 2 个目标因 0 行数据无法判定，共 9 条检查")
	require.Contains(t, chinese, "public.stock_alert (8)")
	require.Contains(t, chinese, "这是数据事实，不是检查缺陷")

	// A period with no empty target says nothing at all about them.
	require.NotContains(t, Render(&store.QualityAgentReportSnapshot{}, start, end, time.UTC, "en-US"), "hold no rows")
}
