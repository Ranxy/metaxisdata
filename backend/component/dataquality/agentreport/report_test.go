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
	require.Contains(t, english, "- 1 check(s) failed and 0 errored in this period.")
	require.NotContains(t, english, "Nothing needs attention in this period.")

	chinese := Render(snapshot, start, end, time.UTC, "zh-CN")
	require.Contains(t, chinese, "质量巡检报告")
	require.Contains(t, chinese, "- 已发现目标：4")
	require.Contains(t, chinese, "- 未解决问题：1")
	require.NotEqual(t, english, chinese)

	// The nothing-to-attention line belongs to a period with no findings only.
	clean := &store.QualityAgentReportSnapshot{}
	require.Contains(t, Render(clean, start, end, time.UTC, "en-US"), "- Nothing needs attention in this period.")
}
