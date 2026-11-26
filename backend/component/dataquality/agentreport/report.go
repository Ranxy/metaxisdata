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
	_, _ = body.WriteString("## What the agent fixed\n\n")
	if snapshot.RepairedCount > 0 {
		fmt.Fprintf(&body, "- %d work item(s) closed by the agent itself: %s.\n",
			snapshot.RepairedCount, repairedClasses(snapshot.RepairedByClass, "en"))
	} else {
		_, _ = body.WriteString("- No work item was settled by the agent in this period.\n")
	}
	if line := emptyTargetsLine(snapshot.EmptyTargets, "en"); line != "" {
		_, _ = body.WriteString(line)
	}
	_, _ = body.WriteString("\n## What is waiting for you\n\n")
	waiting := false
	if snapshot.EscalatedCount > 0 {
		fmt.Fprintf(&body, "- %d work item(s) were escalated or frozen for a decision.\n", snapshot.EscalatedCount)
		waiting = true
	}
	if snapshot.OpenCaseCount > 0 {
		fmt.Fprintf(&body, "- %d work item(s) are still in progress.\n", snapshot.OpenCaseCount)
		waiting = true
	}
	if snapshot.PendingProposalCount > 0 {
		fmt.Fprintf(&body, "- %d proposal(s) wait for a decision.\n", snapshot.PendingProposalCount)
		waiting = true
	}
	if snapshot.OpenIssueCount > 0 {
		fmt.Fprintf(&body, "- %d issue(s) remain open on the data itself.\n", snapshot.OpenIssueCount)
		waiting = true
	}
	if snapshot.FailedCount > 0 || snapshot.ErroredCount > 0 {
		fmt.Fprintf(&body, "- %d check(s) failed and %d errored in this period; the issues above are where they land.\n",
			snapshot.FailedCount, snapshot.ErroredCount)
		waiting = true
	}
	if !waiting {
		_, _ = body.WriteString("- Nothing needs a decision in this period.\n")
	}
	return body.String()
}

// emptyTargetsLine renders the targets that could not be judged at all because they hold no rows.
// It belongs under what the agent settled rather than under what waits for a person: an empty table
// is an answer, so the report states it once instead of one escalation per silenced check.
func emptyTargetsLine(targets []store.QualityAgentTargetCaseCount, language string) string {
	if len(targets) == 0 {
		return ""
	}
	parts := make([]string, 0, len(targets))
	for _, target := range targets {
		parts = append(parts, fmt.Sprintf("%s (%d)", targetLabel(target.TargetGUID), target.Cases))
	}
	list := strings.Join(parts, ", ")
	if language == "zh" {
		return fmt.Sprintf("- %d 个目标因 0 行数据无法判定，共 %d 条检查（%s）。这是数据事实，不是检查缺陷。\n",
			len(targets), countCases(targets), list)
	}
	return fmt.Sprintf("- %d target(s) could not be judged at all because they hold no rows, silencing %d check(s) (%s). That is a fact about the data, not a defect in the checks.\n",
		len(targets), countCases(targets), list)
}

// countCases sums the checks one fact silenced.
func countCases(targets []store.QualityAgentTargetCaseCount) int32 {
	var total int32
	for _, target := range targets {
		total += target.Cases
	}
	return total
}

// targetLabel renders a target GUID as the object a person names it by: schema.object on the
// engines that have schemas, database.object on the MySQL-wire family, and the GUID when the path is
// not shaped the usual way (instance;database;schema;object).
func targetLabel(targetGUID string) string {
	parts := strings.Split(targetGUID, ";")
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" {
		return targetGUID
	}
	if len(parts) >= 4 {
		if schema := strings.TrimSpace(parts[len(parts)-2]); schema != "" {
			return schema + "." + last
		}
		if database := strings.TrimSpace(parts[len(parts)-3]); database != "" {
			return database + "." + last
		}
	}
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[len(parts)-2]) + "." + last
	}
	return last
}

// repairedClasses renders the failure classes the agent settled, in the order the read returned
// them, so a retried report renders byte-identically.
func repairedClasses(classes []store.QualityAgentCaseClassCount, language string) string {
	if len(classes) == 0 {
		return "unclassified"
	}
	parts := make([]string, 0, len(classes))
	for _, class := range classes {
		name := class.Class
		if name == "" {
			if language == "zh" {
				name = "未分类"
			} else {
				name = "unclassified"
			}
		}
		parts = append(parts, fmt.Sprintf("%s %d", name, class.Count))
	}
	return strings.Join(parts, ", ")
}

func renderChinese(snapshot *store.QualityAgentReportSnapshot, periodStart, periodEnd time.Time, location *time.Location) string {
	var body strings.Builder
	_, _ = body.WriteString("# 质量巡检报告\n\n")
	_, _ = fmt.Fprintf(&body, "周期：%s 至 %s（%s）\n\n",
		periodStart.In(location).Format("2006-01-02 15:04"),
		periodEnd.In(location).Format("2006-01-02 15:04"),
		location.String())
	_, _ = body.WriteString("## 范围\n\n")
	_, _ = fmt.Fprintf(&body, "- 数据库：%d\n", len(snapshot.Scope))
	_, _ = fmt.Fprintf(&body, "- 已发现目标：%d\n", snapshot.TargetCount)
	_, _ = fmt.Fprintf(&body, "- 已覆盖目标：%d\n", snapshot.CoveredTargetCount)
	_, _ = fmt.Fprintf(&body, "- 启用检查：%d\n\n", snapshot.RuleCount)
	_, _ = body.WriteString("## 本周期\n\n")
	_, _ = fmt.Fprintf(&body, "- 执行检查：%d（通过 %d，警告 %d，失败 %d，错误 %d）\n",
		snapshot.ExecutionCount, snapshot.PassedCount, snapshot.WarnedCount,
		snapshot.FailedCount, snapshot.ErroredCount)
	_, _ = fmt.Fprintf(&body, "- 新增问题：%d\n", snapshot.NewIssueCount)
	_, _ = fmt.Fprintf(&body, "- 未解决问题：%d\n", snapshot.OpenIssueCount)
	_, _ = fmt.Fprintf(&body, "- 待审核建议：%d\n\n", snapshot.PendingProposalCount)
	_, _ = body.WriteString("## Agent 修好了什么\n\n")
	if snapshot.RepairedCount > 0 {
		_, _ = fmt.Fprintf(&body, "- 由 Agent 自行收敛的工作项：%d 个（%s）。\n",
			snapshot.RepairedCount, repairedClasses(snapshot.RepairedByClass, "zh"))
	} else {
		_, _ = body.WriteString("- 本周期没有由 Agent 自行收敛的工作项。\n")
	}
	if line := emptyTargetsLine(snapshot.EmptyTargets, "zh"); line != "" {
		_, _ = body.WriteString(line)
	}
	_, _ = body.WriteString("\n## 等你决定\n\n")
	waiting := false
	if snapshot.EscalatedCount > 0 {
		_, _ = fmt.Fprintf(&body, "- %d 个工作项已升级或冻结，等待你决定。\n", snapshot.EscalatedCount)
		waiting = true
	}
	if snapshot.OpenCaseCount > 0 {
		_, _ = fmt.Fprintf(&body, "- %d 个工作项仍在处理中。\n", snapshot.OpenCaseCount)
		waiting = true
	}
	if snapshot.PendingProposalCount > 0 {
		_, _ = fmt.Fprintf(&body, "- %d 条建议等待决定。\n", snapshot.PendingProposalCount)
		waiting = true
	}
	if snapshot.OpenIssueCount > 0 {
		_, _ = fmt.Fprintf(&body, "- %d 个数据问题仍未解决。\n", snapshot.OpenIssueCount)
		waiting = true
	}
	if snapshot.FailedCount > 0 || snapshot.ErroredCount > 0 {
		_, _ = fmt.Fprintf(&body, "- 本周期 %d 个检查失败、%d 个检查错误，相应问题记在上面的问题台账。\n",
			snapshot.FailedCount, snapshot.ErroredCount)
		waiting = true
	}
	if !waiting {
		_, _ = body.WriteString("- 本周期没有需要你决定的事项。\n")
	}
	return body.String()
}
