package qualityrunner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/component/notification"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// notifyIssueOpened announces a new open problem to its rule's owner, or to the
// workspace administrators when the rule has no owner.
func (r *Runner) notifyIssueOpened(ctx context.Context, workspaceID string, execution *ruleExecution, issue *store.QualityIssueMessage, now time.Time) {
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID:     workspaceID,
		kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		execution:       execution,
		issueResourceID: issue.ResourceID,
	})
	r.sendDataQualityNotification(ctx, execution.ownerID(), detail, qualityNotificationSeverity(execution.severity()), now)
}

// notifyExecutionFailed announces a rule that could not be executed at all. The
// design makes this alert unmutable and addresses it to the workspace
// administrators only: a broken rule is an operational failure, not a problem the
// rule's owner or mute is meant to absorb.
func (r *Runner) notifyExecutionFailed(ctx context.Context, workspaceID string, execution *ruleExecution, now time.Time) {
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID: workspaceID,
		kind:        storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		execution:   execution,
	})
	r.sendDataQualityNotification(ctx, execution.ownerID(), detail, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, now)
}

// sendDataQualityNotification addresses one message and hands it to the service.
// Delivery is best effort: the notification service logs its own failures, so a
// failed write only adds a log line here.
func (r *Runner) sendDataQualityNotification(ctx context.Context, ownerID *int32, detail *storepb.DataQualityDetail, severity storepb.NotificationSeverity, now time.Time) {
	r.sendDataQualityNotificationFor(ctx, ownerID, detail, severity, qualityNotificationTarget(detail), now)
}

// sendDataQualityNotificationFor delivers one message with an explicit dedupe
// target. It exists for the storm summary, whose event is one internal batch
// rather than one resource name.
func (r *Runner) sendDataQualityNotificationFor(ctx context.Context, ownerID *int32, detail *storepb.DataQualityDetail, severity storepb.NotificationSeverity, dedupeTarget string, now time.Time) {
	if r.notifier == nil || detail == nil {
		return
	}
	message := notification.DataQualityMessage(severity, detail)
	message.DedupeKey = notification.DataQualityDedupeKey(detail.GetKind(), dedupeTarget, now)
	recipientID, toAdmins := qualityNotificationRecipient(detail.GetKind(), ownerID)
	if toAdmins {
		if err := r.notifier.SendToWorkspaceAdmins(ctx, message); err != nil {
			notification.LogFailure(err, slog.String("quality_event", detail.GetKind().String()))
		}
		return
	}
	message.RecipientId = recipientID
	if err := r.notifier.Send(ctx, message); err != nil {
		notification.LogFailure(err, slog.String("quality_event", detail.GetKind().String()))
	}
}

// reportQualityStorm replaces a burst of per-rule notifications with one summary
// addressed to the workspace administrators. A batch belongs to one target, so
// the summary names that target (and the instance that admitted the batch), not an
// execution; the dashboard links the executions. It is sent after the batch
// finished, so the executions are terminal when the recipient opens one.
func (r *Runner) reportQualityStorm(ctx context.Context, batchID int64, instanceResourceID string, execution *ruleExecution, failed, total int32, now time.Time) {
	if r.notifier == nil {
		return
	}
	targetGUID := ""
	targetPath := ""
	if execution != nil {
		targetGUID = execution.targetGUID()
		if execution.snapshot != nil {
			targetPath = qualityTargetDisplayPath(execution.snapshot.Target.Path)
		}
	}
	detail := buildStormDetail(formatInstanceResourceName(instanceResourceID), targetGUID, targetPath, failed, total)
	// The dedupe key is scoped to the internal batch, not the instance: one
	// scheduler tick can chunk an instance into several batches, and each storm is
	// its own event. The batch id is internal and never leaves the server.
	r.sendDataQualityNotificationFor(ctx, nil, detail, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
		fmt.Sprintf("qualityBatch/%d", batchID), now)
}

// dataQualityDetailInput is everything a notification detail is built from. It is
// a struct so the mapping is one function instead of one per event kind.
type dataQualityDetailInput struct {
	workspaceID     string
	kind            storepb.DataQualityNotificationKind
	execution       *ruleExecution
	issueResourceID string
}

// buildDataQualityDetail maps one execution to the structured payload the client
// renders. It deliberately carries no sample rows or prose: the detail is a
// pointer into the product, and the notification table is kept forever.
func buildDataQualityDetail(in dataQualityDetailInput) *storepb.DataQualityDetail {
	execution := in.execution
	result := execution.result
	detail := &storepb.DataQualityDetail{
		Kind:       in.kind,
		Rule:       formatQualityRuleName(in.workspaceID, execution.ruleResourceID()),
		RuleTitle:  execution.ruleTitle(),
		TargetGuid: execution.targetGUID(),
		Execution:  formatQualityExecutionName(in.workspaceID, execution.executionID()),
		Severity:   execution.severity(),
		FromStatus: statusOrUnspecified(execution.previous),
		ToStatus:   result.Status,
		FailedRows: result.FailedRows,
		TotalRows:  result.TotalRows,
	}
	if execution.snapshot != nil {
		detail.TargetPath = qualityTargetDisplayPath(execution.snapshot.Target.Path)
	}
	if result.ObservedValue != nil {
		detail.ObservedValue = *result.ObservedValue
		detail.ObservedUnit = qualityObservedUnit(result.ObservedKind)
	}
	if in.issueResourceID != "" {
		detail.Issue = formatQualityIssueName(in.workspaceID, in.issueResourceID)
	}
	if in.kind == storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED {
		detail.ErrorCode = result.ErrorCode
	}
	return detail
}

// buildStormDetail is the summary one storm notification carries: how many
// executions failed out of how many the target's batch ran, plus the target and
// the instance whose batch merged them.
func buildStormDetail(instanceResourceName, targetGUID, targetPath string, failed, total int32) *storepb.DataQualityDetail {
	return &storepb.DataQualityDetail{
		Kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY,
		Instance:        instanceResourceName,
		TargetGuid:      targetGUID,
		TargetPath:      targetPath,
		Severity:        storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		FailedRuleCount: failed,
		TotalRuleCount:  total,
	}
}

// isQualityStorm reports whether one batch failed enough rules to be summarized
// instead of announced rule by rule. Only FAIL verdicts count: a batch that could
// not execute its rules is a different alert, not a data-quality storm.
func isQualityStorm(executions []*ruleExecution) bool {
	failed := 0
	for _, execution := range executions {
		if execution != nil && execution.result != nil && execution.result.Status == storepb.QualityStatus_QUALITY_STATUS_FAIL {
			failed++
		}
	}
	return failed > QualityStormThreshold
}

// qualityNotificationTarget returns the resource the dedupe key is scoped to: the
// issue for a problem transition, the rule for an execution failure, the instance
// for a storm.
func qualityNotificationTarget(detail *storepb.DataQualityDetail) string {
	switch detail.GetKind() {
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED:
		if detail.GetIssue() != "" {
			return detail.GetIssue()
		}
		return detail.GetRule()
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED:
		return detail.GetRule()
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY:
		// Unreachable in production: reportQualityStorm passes the internal batch
		// id to sendDataQualityNotificationFor. Kept explicit so the exhaustive
		// switch states the intent instead of falling through to the default.
		return ""
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

// qualityNotificationRecipient picks the inbox a data-quality message goes to.
// A problem transition goes to the rule's owner when it has one and to the
// workspace administrators otherwise, so whoever owns the rule is the one asked
// to look. An execution failure and a storm summary go to the administrators
// only (design §8.2): the owner already sees the failed execution on the
// executions page, and paging them again for every broken rule would bury the
// problem alerts that actually need their attention.
func qualityNotificationRecipient(kind storepb.DataQualityNotificationKind, ownerID *int32) (recipientID int32, toAdmins bool) {
	switch kind {
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY:
		return 0, true
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED:
		if ownerID == nil || *ownerID <= 0 {
			return 0, true
		}
		return *ownerID, false
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_UNSPECIFIED:
		return 0, true
	default:
		return 0, true
	}
}

// qualityNotificationSeverity maps the rule's severity onto the notification
// envelope's three levels.
func qualityNotificationSeverity(severity storepb.QualitySeverity) storepb.NotificationSeverity {
	switch severity {
	case storepb.QualitySeverity_QUALITY_SEVERITY_WARNING:
		return storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING
	case storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL:
		return storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR
	case storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED:
		return storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING
	default:
		return storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING
	}
}

// qualityRuleMuted reports whether the rule's mute window suppresses notifications
// about the problem it watches. It never suppresses execution-failure alerts.
func qualityRuleMuted(head *store.QualityRuleMessage, now time.Time) bool {
	if head == nil || head.MuteUntil == nil {
		return false
	}
	return now.Before(*head.MuteUntil)
}

// qualityIssueMuted reports whether one issue's own mute window suppresses its
// problem notifications.
func qualityIssueMuted(issue *store.QualityIssueMessage, now time.Time) bool {
	if issue == nil || issue.MutedUntil == nil {
		return false
	}
	return now.Before(*issue.MutedUntil)
}
