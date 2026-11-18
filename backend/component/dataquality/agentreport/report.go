// Package agentreport renders the deterministic markdown body of one per-agent
// patrol report. The counts come from store.QualityAgentReportSnapshot; this
// package only formats them, so the numbers can never come from the model.
package agentreport

import (
	"fmt"
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// Render builds the report body for one period.
func Render(snapshot *store.QualityAgentReportSnapshot, periodStart, periodEnd time.Time, location *time.Location, language string) string {
	if snapshot == nil {
		return ""
	}
	if location == nil {
		location = time.UTC
	}
	if strings.HasPrefix(strings.ToLower(language), "zh") {
		return renderChinese(snapshot, periodStart, periodEnd, location)
	}
	var body strings.Builder
	_, _ = body.WriteString("# Quality patrol report\n\n")
	fmt.Fprintf(&body, "Period: %s to %s (%s)\n\n",
		periodStart.In(location).Format("2006-01-02 15:04"),
		periodEnd.In(location).Format("2006-01-02 15:04"),
		location.String())
	_, _ = body.WriteString("## Scope\n\n")
	fmt.Fprintf(&body, "- Databases: %d\n", len(snapshot.Scope))
	fmt.Fprintf(&body, "- Targets discovered: %d\n", snapshot.TargetCount)
	fmt.Fprintf(&body, "- Targets with coverage: %d\n", snapshot.CoveredTargetCount)
	fmt.Fprintf(&body, "- Enabled checks: %d\n\n", snapshot.RuleCount)
	_, _ = body.WriteString("## This period\n\n")
	fmt.Fprintf(&body, "- Checks run: %d (pass %d, warn %d, fail %d, error %d)\n",
		snapshot.ExecutionCount, snapshot.PassedCount, snapshot.WarnedCount,
		snapshot.FailedCount, snapshot.ErroredCount)
	fmt.Fprintf(&body, "- New issues: %d\n", snapshot.NewIssueCount)
	fmt.Fprintf(&body, "- Open issues: %d\n", snapshot.OpenIssueCount)
	fmt.Fprintf(&body, "- Proposals waiting for review: %d\n\n", snapshot.PendingProposalCount)
	_, _ = body.WriteString("## What a person should look at\n\n")
	if snapshot.FailedCount > 0 || snapshot.ErroredCount > 0 {
		fmt.Fprintf(&body, "- %d check(s) failed and %d errored in this period.\n", snapshot.FailedCount, snapshot.ErroredCount)
	}
	if snapshot.OpenIssueCount > 0 {
		fmt.Fprintf(&body, "- %d issue(s) remain open.\n", snapshot.OpenIssueCount)
	}
	if snapshot.PendingProposalCount > 0 {
		fmt.Fprintf(&body, "- %d proposal(s) wait for the owner's decision.\n", snapshot.PendingProposalCount)
	}
	if snapshot.FailedCount == 0 && snapshot.ErroredCount == 0 && snapshot.OpenIssueCount == 0 && snapshot.PendingProposalCount == 0 {
		_, _ = body.WriteString("- Nothing needs attention in this period.\n")
	}
	return body.String()
}

func renderChinese(snapshot *store.QualityAgentReportSnapshot, periodStart, periodEnd time.Time, location *time.Location) string {
	var body strings.Builder
	body.WriteString("# 质量巡检报告\n\n")
	_, _ = fmt.Fprintf(&body, "周期：%s 至 %s（%s）\n\n",
		periodStart.In(location).Format("2006-01-02 15:04"),
		periodEnd.In(location).Format("2006-01-02 15:04"),
		location.String())
	body.WriteString("## 范围\n\n")
	_, _ = fmt.Fprintf(&body, "- 数据库：%d\n", len(snapshot.Scope))
	_, _ = fmt.Fprintf(&body, "- 已发现目标：%d\n", snapshot.TargetCount)
	_, _ = fmt.Fprintf(&body, "- 已覆盖目标：%d\n", snapshot.CoveredTargetCount)
	_, _ = fmt.Fprintf(&body, "- 启用检查：%d\n\n", snapshot.RuleCount)
	body.WriteString("## 本周期\n\n")
	_, _ = fmt.Fprintf(&body, "- 执行检查：%d（通过 %d，警告 %d，失败 %d，错误 %d）\n",
		snapshot.ExecutionCount, snapshot.PassedCount, snapshot.WarnedCount,
		snapshot.FailedCount, snapshot.ErroredCount)
	_, _ = fmt.Fprintf(&body, "- 新增问题：%d\n", snapshot.NewIssueCount)
	_, _ = fmt.Fprintf(&body, "- 未解决问题：%d\n", snapshot.OpenIssueCount)
	_, _ = fmt.Fprintf(&body, "- 待审核建议：%d\n\n", snapshot.PendingProposalCount)
	body.WriteString("## 需要人关注\n\n")
	if snapshot.FailedCount > 0 || snapshot.ErroredCount > 0 {
		_, _ = fmt.Fprintf(&body, "- 本周期 %d 个检查失败、%d 个检查错误。\n", snapshot.FailedCount, snapshot.ErroredCount)
	}
	if snapshot.OpenIssueCount > 0 {
		_, _ = fmt.Fprintf(&body, "- %d 个问题仍未解决。\n", snapshot.OpenIssueCount)
	}
	if snapshot.PendingProposalCount > 0 {
		_, _ = fmt.Fprintf(&body, "- %d 条建议等待负责人决定。\n", snapshot.PendingProposalCount)
	}
	if snapshot.FailedCount == 0 && snapshot.ErroredCount == 0 && snapshot.OpenIssueCount == 0 && snapshot.PendingProposalCount == 0 {
		body.WriteString("- 本周期没有需要关注的事项。\n")
	}
	return body.String()
}
