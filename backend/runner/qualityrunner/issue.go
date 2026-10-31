package qualityrunner

import (
	"context"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// settleQualityExecutions applies a batch's committed verdicts to the problem
// ledger and emits the notifications their transitions imply. Both are best
// effort: the execution rows are already stored, so a lost issue write or a lost
// notification is logged and never turns a completed batch into a failure.
func (r *Runner) settleQualityExecutions(ctx context.Context, workspaceID string, executions []*ruleExecution, storm bool, now time.Time) {
	for _, execution := range executions {
		if execution == nil || execution.result == nil {
			continue
		}
		r.settleRuleExecution(ctx, workspaceID, execution, storm, now)
	}
}

// settleRuleExecution decides what one verdict means for the rule's open issue and
// its owner's inbox. The issue read is the only state the planner needs: the open
// issue row and the rule's previous status decide the transition together.
func (r *Runner) settleRuleExecution(ctx context.Context, workspaceID string, execution *ruleExecution, storm bool, now time.Time) {
	result := execution.result
	issue, err := r.stores.GetOpenQualityIssueForRule(ctx, result.RuleID)
	if err != nil {
		slog.Warn("failed to read the open quality issue of a rule",
			slog.Int64("rule", result.RuleID),
			log.WithError(err))
	} else {
		action := dataquality.PlanIssueTransition(execution.previous, result.Status, issue != nil)
		switch action {
		case dataquality.IssueActionOpen:
			r.openQualityIssue(ctx, workspaceID, execution, storm, now)
		case dataquality.IssueActionTouch:
			if issue != nil {
				if err := r.stores.TouchQualityIssue(ctx, issue.ID, execution.executionID(), execution.severity(), now); err != nil {
					slog.Warn("failed to touch a quality issue",
						slog.Int64("issue", issue.ID),
						log.WithError(err))
				}
			}
		case dataquality.IssueActionResolve:
			if issue != nil {
				r.resolveQualityIssue(ctx, workspaceID, execution, issue, now)
			}
		case dataquality.IssueActionNone:
			// A passing execution without an issue, or a status that says nothing
			// about the problem: the ledger is already correct.
		default:
			// An action this build does not know is a defect, not a reason to stop
			// the batch; the verdict is stored and the next execution will retry the
			// ledger.
			slog.Warn("unknown quality issue action",
				slog.Int64("rule", result.RuleID),
				slog.Int("action", int(action)))
		}
	}

	// An execution failure is its own alert: it must reach someone even when the
	// rule is muted, because the mute is about the problem the rule watches, not
	// about the rule being broken. A storm merges these into one summary.
	if result.Status == storepb.QualityStatus_QUALITY_STATUS_ERROR && !storm {
		r.notifyExecutionFailed(ctx, workspaceID, execution, now)
	}
}

// openQualityIssue records a new open problem and, unless the batch is a storm or
// the rule is muted, announces it.
func (r *Runner) openQualityIssue(ctx context.Context, workspaceID string, execution *ruleExecution, storm bool, now time.Time) {
	resourceID, err := common.RandomString(qualityIssueResourceIDLength)
	if err != nil {
		slog.Warn("failed to generate a quality issue resource id", log.WithError(err))
		return
	}
	executionID := execution.executionID()
	created, err := r.stores.OpenQualityIssue(ctx, &store.QualityIssueMessage{
		ResourceID:           resourceID,
		RuleID:               execution.result.RuleID,
		TargetGUID:           execution.targetGUID(),
		TargetType:           execution.targetType(),
		Status:               storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
		Severity:             execution.severity(),
		FirstSeenExecutionID: &executionID,
		LastSeenExecutionID:  &executionID,
		FirstSeenAt:          now,
		LastSeenAt:           now,
		OccurrenceCount:      1,
	})
	if err != nil {
		slog.Warn("failed to open a quality issue",
			slog.Int64("rule", execution.result.RuleID),
			log.WithError(err))
		return
	}
	if created == nil {
		// The partial unique index refused the insert: another writer opened the
		// rule's issue between our read and this write. It is not a new problem, so
		// it is not announced again.
		return
	}
	if storm || qualityRuleMuted(execution.head, now) {
		return
	}
	r.notifyIssueOpened(ctx, workspaceID, execution, created, now)
}

// resolveQualityIssue closes a recovered problem and tells its recipient, unless
// the rule or the issue is muted. A recovery is never suppressed by a storm: a
// storm is a burst of failures, and hiding the recoveries would leave the inbox
// with only the alarming half of the story.
func (r *Runner) resolveQualityIssue(ctx context.Context, workspaceID string, execution *ruleExecution, issue *store.QualityIssueMessage, now time.Time) {
	if err := r.stores.ResolveQualityIssue(ctx, issue.ID, storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_AUTO_RECOVERED, "", now); err != nil {
		slog.Warn("failed to resolve a quality issue",
			slog.Int64("issue", issue.ID),
			log.WithError(err))
		return
	}
	if qualityRuleMuted(execution.head, now) || qualityIssueMuted(issue, now) {
		return
	}
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID:     workspaceID,
		kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED,
		execution:       execution,
		issueResourceID: issue.ResourceID,
	})
	r.sendDataQualityNotification(ctx, execution.ownerID(), detail, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, now)
}

// previousQualityStatus returns the rule's last terminal verdict before this
// execution, which is what tells a recovery apart from a rule that never failed. A
// missing or unreadable history is treated as "no previous result": opening an
// issue is still correct and the partial unique index prevents a duplicate.
func previousQualityStatus(ctx context.Context, st batchExecutionStore, ruleID int64) *storepb.QualityStatus {
	executions, err := st.ListRecentQualityExecutions(ctx, ruleID, 1)
	if err != nil {
		slog.Warn("failed to read a quality rule's previous execution",
			slog.Int64("rule", ruleID),
			log.WithError(err))
		return nil
	}
	for _, execution := range executions {
		if execution == nil {
			continue
		}
		status := execution.Status
		return &status
	}
	return nil
}

// workspaceID is the workspace every notification resource name is scoped to. A
// read failure leaves the name in its degraded form rather than dropping the
// message: a delivery is still useful without the workspace prefix.
func (r *Runner) workspaceID(ctx context.Context) string {
	workspaceID, err := r.stores.GetWorkspaceID(ctx)
	if err != nil {
		slog.Warn("failed to read the workspace id for a quality notification", log.WithError(err))
		return ""
	}
	return workspaceID
}

// resolveRuleHead reads the mutable rule row of an execution by its internal id.
// The lookup has no lifecycle filter, so an archived or deleted rule still
// resolves and retires; a missing row stays nil and the execution record
// degrades gracefully.
func (r *Runner) resolveRuleHead(ctx context.Context, ruleID int64) *store.QualityRuleMessage {
	// ListQualityRulesByIDs has no lifecycle filter, so an archived or deleted
	// rule still resolves: loadRuleSnapshot must see it and report SKIPPED rather
	// than executing a retired definition.
	rules, err := r.stores.ListQualityRulesByIDs(ctx, []int64{ruleID})
	if err != nil {
		slog.Warn("failed to resolve a quality rule's head",
			slog.Int64("rule", ruleID),
			log.WithError(err))
		return nil
	}
	for _, rule := range rules {
		if rule.ID == ruleID {
			return rule
		}
	}
	return nil
}
