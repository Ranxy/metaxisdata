package notification

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestDataQualityMessageCarriesTypeSeverityAndDetail(t *testing.T) {
	t.Parallel()

	detail := &storepb.DataQualityDetail{
		Kind:          storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		Rule:          "workspaces/ws/qualityRules/rule1",
		RuleTitle:     "orders must not be null",
		TargetGuid:    "inst;db;public;orders",
		TargetPath:    "inst/db/public/orders.status",
		FromStatus:    storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
		ToStatus:      storepb.QualityStatus_QUALITY_STATUS_FAIL,
		Severity:      storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		ObservedValue: 42,
		ObservedUnit:  "rows",
		Issue:         "workspaces/ws/qualityIssues/issue1",
		Run:           "workspaces/ws/qualityRuns/run1",
		FailedRows:    42,
		TotalRows:     100,
	}
	message := DataQualityMessage(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, detail)

	require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY, message.GetType())
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, message.GetSeverity())
	require.NotNil(t, message.GetDataQuality())
	require.Equal(t, detail.GetKind(), message.GetDataQuality().GetKind())
	require.Equal(t, "workspaces/ws/qualityRules/rule1", message.GetDataQuality().GetRule())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, message.GetDataQuality().GetToStatus())
	require.Equal(t, int64(42), message.GetDataQuality().GetFailedRows())
	require.Equal(t, int64(100), message.GetDataQuality().GetTotalRows())
	require.Equal(t, "workspaces/ws/qualityIssues/issue1", message.GetDataQuality().GetIssue())
	require.Equal(t, "workspaces/ws/qualityRuns/run1", message.GetDataQuality().GetRun())
	// The envelope has no recipient: the runner chooses the owner or the admins.
	require.Zero(t, message.GetRecipientId())
}

func TestDataQualityMessageBoundsTextPayload(t *testing.T) {
	t.Parallel()

	long := make([]byte, MaxErrorBytes+100)
	for i := range long {
		long[i] = 'x'
	}
	message := DataQualityMessage(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, &storepb.DataQualityDetail{
		Kind:       storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		TargetPath: string(long),
		ErrorCode:  string(long),
	})
	require.Len(t, message.GetDataQuality().GetTargetPath(), MaxErrorBytes)
	require.Len(t, message.GetDataQuality().GetErrorCode(), MaxErrorBytes)
}

func TestDataQualityMessageWithoutDetail(t *testing.T) {
	t.Parallel()

	message := DataQualityMessage(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, nil)
	require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY, message.GetType())
	require.Nil(t, message.GetDataQuality())
}

func TestDataQualityDedupeKeyBucketsWithinTheWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 10, 5, 0, 0, time.UTC)
	kind := storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED
	target := "workspaces/ws/qualityIssues/issue1"

	// Two occurrences in the same hour share one key: the second is suppressed.
	require.Equal(t,
		DataQualityDedupeKey(kind, target, now),
		DataQualityDedupeKey(kind, target, now.Add(30*time.Minute)),
	)
	// The next window is a different key.
	require.NotEqual(t,
		DataQualityDedupeKey(kind, target, now),
		DataQualityDedupeKey(kind, target, now.Add(2*time.Hour)),
	)
	// The kind and the target are part of the identity.
	require.NotEqual(t,
		DataQualityDedupeKey(kind, target, now),
		DataQualityDedupeKey(storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED, target, now),
	)
	require.NotEqual(t,
		DataQualityDedupeKey(kind, target, now),
		DataQualityDedupeKey(kind, "workspaces/ws/qualityIssues/issue2", now),
	)
	// An unspecified kind is not a reportable event and is never suppressed.
	require.Empty(t,
		DataQualityDedupeKey(storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_UNSPECIFIED, target, now),
	)
}

func TestDataQualityDedupeKeyDistinguishesEveryEvent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 10, 5, 0, 0, time.UTC)
	kinds := []storepb.DataQualityNotificationKind{
		storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED,
		storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY,
	}
	seen := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		key := DataQualityDedupeKey(kind, "workspaces/ws/qualityRuns/run1", now)
		require.NotEmpty(t, key)
		_, duplicate := seen[key]
		require.False(t, duplicate, "kind %s reused another kind's key", kind)
		seen[key] = struct{}{}
	}
}
