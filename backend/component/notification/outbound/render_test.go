package outbound

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func dataQualityEvent(detail *storepb.DataQualityDetail) *storepb.NotificationDeliveryEvent {
	return &storepb.NotificationDeliveryEvent{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
		Severity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
		Detail:   &storepb.NotificationDeliveryEvent_DataQuality{DataQuality: detail},
	}
}

func TestRenderDataQualityIssueOpened(t *testing.T) {
	t.Parallel()

	event := dataQualityEvent(&storepb.DataQualityDetail{
		Kind:          storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		RuleTitle:     "orders id not null",
		TargetPath:    "prod/db/public/orders",
		FromStatus:    storepb.QualityStatus_QUALITY_STATUS_PASS,
		ToStatus:      storepb.QualityStatus_QUALITY_STATUS_FAIL,
		Severity:      storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		ObservedValue: 12,
		ObservedUnit:  "rows",
		FailedRows:    12,
		TotalRows:     1000,
	})

	en := Render("en-US", event, "https://app.example.com/", time.Now())
	require.Contains(t, en.Title, "orders id not null")
	require.Contains(t, en.Title, "prod/db/public/orders")
	require.Contains(t, en.Text, "PASS → FAIL")
	require.Contains(t, en.Text, "12 rows")
	require.Equal(t, "https://app.example.com/data-quality/executions", en.Link)

	zh := Render("zh-CN", event, "", time.Now())
	require.Contains(t, zh.Title, "数据质量")
	require.Contains(t, zh.Text, "严重级别：ERROR")
	require.Empty(t, zh.Link, "no external URL means no link")
}

func TestRenderDataQualityExecutionFailed(t *testing.T) {
	t.Parallel()

	event := dataQualityEvent(&storepb.DataQualityDetail{
		Kind:       storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		RuleTitle:  "freshness",
		TargetPath: "prod/db/public/events",
		ErrorCode:  "TIMEOUT",
	})
	rendered := Render("en-US", event, "https://app.example.com", time.Now())
	require.Contains(t, rendered.Title, "could not run")
	require.Contains(t, rendered.Text, "TIMEOUT")
	require.Contains(t, rendered.Text, "not judged")
}

func TestRenderDataQualityStormSummary(t *testing.T) {
	t.Parallel()

	event := dataQualityEvent(&storepb.DataQualityDetail{
		Kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY,
		TargetPath:      "prod/db/public/orders",
		FailedRuleCount: 14,
		TotalRuleCount:  30,
		Instance:        "instances/i1",
	})
	rendered := Render("en-US", event, "", time.Now())
	require.Contains(t, rendered.Title, "14/30")
	require.Contains(t, rendered.Text, "i1")
}

func TestRenderDataQualityRecovered(t *testing.T) {
	t.Parallel()

	event := dataQualityEvent(&storepb.DataQualityDetail{
		Kind:       storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED,
		RuleTitle:  "orders id not null",
		TargetPath: "prod/db/public/orders",
		FromStatus: storepb.QualityStatus_QUALITY_STATUS_FAIL,
		ToStatus:   storepb.QualityStatus_QUALITY_STATUS_PASS,
	})
	rendered := Render("en-US", event, "", time.Now())
	require.Contains(t, rendered.Title, "recovered")
	require.Contains(t, rendered.Text, "FAIL → PASS")
}

func TestRenderSchemaSyncFailure(t *testing.T) {
	t.Parallel()

	event := &storepb.NotificationDeliveryEvent{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
		Severity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
		Detail: &storepb.NotificationDeliveryEvent_SchemaSync{SchemaSync: &storepb.SchemaSyncDetail{
			Instance:       "instances/i1",
			InstanceTitle:  "prod",
			Trigger:        storepb.SyncTrigger_SYNC_TRIGGER_BACKGROUND,
			SucceededCount: 3,
			FailedCount:    2,
			Databases: []*storepb.SyncDatabaseResult{
				{Database: "instances/i1/databases/app", State: storepb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED, Error: "denied"},
				{Database: "instances/i1/databases/ops", State: storepb.SyncDatabaseState_SYNC_DATABASE_STATE_UNFINISHED},
			},
		}},
	}
	rendered := Render("en-US", event, "https://app.example.com", time.Now())
	require.Contains(t, rendered.Title, "prod")
	require.Contains(t, rendered.Title, "failures")
	require.Contains(t, rendered.Text, "Failed databases: app, ops")
	require.Equal(t, "https://app.example.com/instances/i1", rendered.Link)

	healthy := &storepb.NotificationDeliveryEvent{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
		Severity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO,
		Detail: &storepb.NotificationDeliveryEvent_SchemaSync{SchemaSync: &storepb.SchemaSyncDetail{
			InstanceTitle:  "prod",
			SucceededCount: 4,
		}},
	}
	require.Contains(t, Render("en-US", healthy, "", time.Now()).Title, "finished")
}

func TestRenderOpenLineage(t *testing.T) {
	t.Parallel()

	event := &storepb.NotificationDeliveryEvent{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE,
		Severity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
		Detail: &storepb.NotificationDeliveryEvent_Openlineage{Openlineage: &storepb.OpenLineageDetail{
			Kind:          storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED,
			Namespace:     "hive://cluster",
			Dataset:       "db.orders",
			ReceivedCount: 5,
			FailedCount:   1,
			Error:         "no mapping",
		}},
	}
	rendered := Render("en-US", event, "https://app.example.com", time.Now())
	require.Contains(t, rendered.Title, "no instance")
	require.Contains(t, rendered.Text, "hive://cluster")
	require.Equal(t, "https://app.example.com/openlineage/events", rendered.Link)

	zh := Render("zh-CN", event, "", time.Now())
	require.Contains(t, zh.Title, "命名空间未映射")
}

func TestRenderWithoutKnownDetailFallsBackToType(t *testing.T) {
	t.Parallel()

	rendered := Render("en-US", &storepb.NotificationDeliveryEvent{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
		Severity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO,
	}, "", time.Now())
	require.Equal(t, "DATA_QUALITY", rendered.Title)
}

func TestNormalizeLanguage(t *testing.T) {
	t.Parallel()

	require.Equal(t, "zh-CN", NormalizeLanguage("zh"))
	require.Equal(t, "zh-CN", NormalizeLanguage("zh-CN"))
	require.Equal(t, "zh-CN", NormalizeLanguage("ZH-TW"))
	require.Equal(t, "en-US", NormalizeLanguage("en-US"))
	require.Equal(t, "en-US", NormalizeLanguage(""))
}

// The data-quality detail routes carry the whole resource name (the pages join
// the path segments back to call the RPC), so the deep link must append the name,
// not its last segment.
func TestRenderDataQualityLinkUsesWholeResourceName(t *testing.T) {
	t.Parallel()

	issued := dataQualityEvent(&storepb.DataQualityDetail{
		Kind:       storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		RuleTitle:  "no nulls",
		TargetPath: "prod/db/public/orders",
		Issue:      "workspaces/ws-1/qualityIssues/abc123",
	})
	require.Equal(t,
		"https://app.example.com/data-quality/issues/workspaces/ws-1/qualityIssues/abc123",
		Render("en-US", issued, "https://app.example.com", time.Now()).Link)

	executed := dataQualityEvent(&storepb.DataQualityDetail{
		Kind:       storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		RuleTitle:  "no nulls",
		TargetPath: "prod/db/public/orders",
		Execution:  "workspaces/ws-1/qualityExecutions/981",
	})
	require.Equal(t,
		"https://app.example.com/data-quality/executions/workspaces/ws-1/qualityExecutions/981",
		Render("en-US", executed, "https://app.example.com", time.Now()).Link)
}

// A trailing slash on the instance resource must not leak into the id.
func TestRenderSchemaSyncLinkTrimsTrailingSlash(t *testing.T) {
	t.Parallel()

	event := &storepb.NotificationDeliveryEvent{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
		Severity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
		Detail: &storepb.NotificationDeliveryEvent_SchemaSync{SchemaSync: &storepb.SchemaSyncDetail{
			Instance: "instances/i1/",
		}},
	}
	require.Equal(t, "https://app.example.com/instances/i1", Render("en-US", event, "https://app.example.com", time.Now()).Link)
}

func TestNormalizeLanguageIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	require.Equal(t, "zh-CN", NormalizeLanguage("zH-CN"))
	require.Equal(t, "zh-CN", NormalizeLanguage("zh-Hant"))
}
