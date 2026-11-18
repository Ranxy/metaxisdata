package agentreport

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestRenderLocalizesTheBody(t *testing.T) {
	t.Parallel()

	snapshot := &store.QualityAgentReportSnapshot{
		Scope: []string{"instances/i/databases/d"}, TargetCount: 4, CoveredTargetCount: 2,
		RuleCount: 3, ExecutionCount: 5, PassedCount: 4, FailedCount: 1, OpenIssueCount: 1,
	}
	start := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	require.Contains(t, Render(snapshot, start, end, time.UTC, "zh-CN"), "质量巡检报告")
	require.Contains(t, Render(snapshot, start, end, time.UTC, "en-US"), "Quality patrol report")
}
