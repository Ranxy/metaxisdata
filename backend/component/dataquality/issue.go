package dataquality

import (
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// IssueAction is what the runner should do to a rule's open issue after one
// result. It is a decision, not a database operation: the runner commits the
// result first and then applies the action, because results and issues cannot be
// written in one transaction through the store's API.
type IssueAction int

const (
	// IssueActionNone leaves the issue ledger untouched: the transition is not a
	// problem event (a passing run without an open issue, any ERROR result, or a
	// status that says nothing about the problem).
	IssueActionNone IssueAction = iota
	// IssueActionOpen records a new open issue for a rule that keeps failing.
	IssueActionOpen
	// IssueActionTouch records another observation of an already open issue.
	IssueActionTouch
	// IssueActionResolve closes the open issue: the rule started passing again.
	IssueActionResolve
)

// PlanIssueTransition applies the problem-transition table of the design (§3.3)
// to one result:
//
//   - the first FAIL with no open issue opens one;
//   - another FAIL keeps the existing issue alive (only last_seen and
//     occurrence_count move);
//   - FAIL → PASS resolves it;
//   - FAIL → WARN/NOT_EVALUATED/SKIPPED keeps it alive but does not resolve it;
//   - any ERROR changes nothing: an execution failure says nothing about the
//     problem, and it must not resolve an issue either;
//   - a status that is not FAIL with no open issue is a no-op.
//
// previous is the rule's last result status before this run, nil when the rule
// has no earlier result. It decides only the recovery transition: PASS with an
// open issue resolves it only when the issue came from a FAIL, not when the
// previous run was an ERROR (ERROR → PASS is "no change" in the design table).
func PlanIssueTransition(previous *storepb.QualityStatus, current storepb.QualityStatus, hasOpenIssue bool) IssueAction {
	switch current {
	case storepb.QualityStatus_QUALITY_STATUS_FAIL:
		if hasOpenIssue {
			return IssueActionTouch
		}
		return IssueActionOpen
	case storepb.QualityStatus_QUALITY_STATUS_PASS:
		if hasOpenIssue && previous != nil && *previous == storepb.QualityStatus_QUALITY_STATUS_FAIL {
			return IssueActionResolve
		}
		return IssueActionNone
	case storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED:
		if hasOpenIssue && previous != nil && *previous == storepb.QualityStatus_QUALITY_STATUS_FAIL {
			// A failing rule that degrades into a warn is still failing in the
			// operator's sense; the design deliberately does not auto-recover it.
			return IssueActionTouch
		}
		return IssueActionNone
	case storepb.QualityStatus_QUALITY_STATUS_ERROR:
		return IssueActionNone
	case storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
		return IssueActionNone
	default:
		return IssueActionNone
	}
}
