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
func Render(snapshot *store.QualityAgentReportSnapshot, periodStart, periodEnd time.Time, location *time.Location) string {
	if snapshot == nil {
		return ""
	}
	if location == nil {
		location = time.UTC
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
