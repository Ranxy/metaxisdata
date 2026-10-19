package notification

import (
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityNotificationWindow is the suppression window of a repeated data-quality
// notification. A rule that keeps failing is judged again on its own schedule,
// and a problem that is already open must not page its owner again every time;
// the window is per event and per target, so a new issue or a recovery is still
// delivered immediately.
const QualityNotificationWindow = time.Hour

// DataQualityMessage builds the envelope of one data-quality notification. The
// server stores no prose: the client renders the message from the structured
// detail and its own i18n catalog, so this only names the type and the severity.
// recipientID is 0 when the message is addressed to the workspace
// administrators; Send stamps it per recipient.
func DataQualityMessage(severity storepb.NotificationSeverity, detail *storepb.DataQualityDetail) *storepb.Notification {
	return &storepb.Notification{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
		Severity: severity,
		Detail:   &storepb.Notification_DataQuality{DataQuality: boundDataQualityDetail(detail)},
	}
}

// DataQualityDedupeKey builds the suppression key of one data-quality event.
// target is the resource an event is about: a rule for an execution failure, an
// issue for a problem transition, a run for a storm summary. An unspecified kind
// has no defined event and therefore no key: it is never suppressed.
func DataQualityDedupeKey(kind storepb.DataQualityNotificationKind, target string, now time.Time) string {
	event := ""
	switch kind {
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED:
		event = "quality.issue-opened"
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED:
		event = "quality.issue-recovered"
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED:
		event = "quality.execution-failed"
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY:
		event = "quality.storm-summary"
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_UNSPECIFIED:
		return ""
	default:
		return ""
	}
	return DedupeKey(event, target, now, QualityNotificationWindow)
}

// boundDataQualityDetail copies the detail with its text payload bounded, the way
// the sync and OpenLineage details bound their errors. The notification table is
// kept forever, and a target path or an engine error can be arbitrarily long, so
// neither is stored as the caller sent it.
func boundDataQualityDetail(detail *storepb.DataQualityDetail) *storepb.DataQualityDetail {
	if detail == nil {
		return nil
	}
	bounded := &storepb.DataQualityDetail{
		Kind:            detail.GetKind(),
		Rule:            truncateBytes(detail.GetRule(), MaxErrorBytes),
		RuleTitle:       truncateBytes(detail.GetRuleTitle(), MaxErrorBytes),
		TargetGuid:      truncateBytes(detail.GetTargetGuid(), MaxErrorBytes),
		TargetPath:      truncateBytes(detail.GetTargetPath(), MaxErrorBytes),
		FromStatus:      detail.GetFromStatus(),
		ToStatus:        detail.GetToStatus(),
		Severity:        detail.GetSeverity(),
		ObservedValue:   detail.GetObservedValue(),
		ObservedUnit:    truncateBytes(detail.GetObservedUnit(), MaxErrorBytes),
		Issue:           truncateBytes(detail.GetIssue(), MaxErrorBytes),
		Run:             truncateBytes(detail.GetRun(), MaxErrorBytes),
		FailedRows:      detail.GetFailedRows(),
		TotalRows:       detail.GetTotalRows(),
		ErrorCode:       truncateBytes(detail.GetErrorCode(), MaxErrorBytes),
		FailedRuleCount: detail.GetFailedRuleCount(),
		TotalRuleCount:  detail.GetTotalRuleCount(),
	}
	return bounded
}
