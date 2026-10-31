package outbound

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

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

func dataQualityDigestEvent(detail *storepb.DataQualityDigestDetail) *storepb.NotificationDeliveryEvent {
	return &storepb.NotificationDeliveryEvent{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST,
		Severity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
		Detail:   &storepb.NotificationDeliveryEvent_DataQualityDigest{DataQualityDigest: detail},
	}
}

func dataQualityDigestDetail() *storepb.DataQualityDigestDetail {
	start := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	return &storepb.DataQualityDigestDetail{
		PeriodStart:         timestamppb.New(start),
		PeriodEnd:           timestamppb.New(end),
		Timezone:            "UTC",
		TargetCount:         8,
		RuleCount:           25,
		ExecutionCount:      42,
		PassedCount:         30,
		WarnedCount:         4,
		FailedCount:         6,
		ErroredCount:        2,
		NewIssueCount:       5,
		RecoveredIssueCount: 3,
		OpenIssueCount:      12,
		OpenWarningCount:    7,
		OpenErrorCount:      4,
		OpenCriticalCount:   1,
		Owners: []*storepb.DataQualityDigestOwner{
			{Owner: "users/7", OwnerName: "Alice", OpenIssueCount: 6, FailingRuleCount: 4},
			{Owner: "", OpenIssueCount: 3, FailingRuleCount: 2},
		},
		Targets: []*storepb.DataQualityDigestTarget{
			{TargetPath: "prod/shop/public/orders", FailedExecutionCount: 4, ErroredExecutionCount: 1, OpenIssueCount: 3},
		},
	}
}

// The digest is a weekly summary: the period in the title, then the verdict
// tally, the ledger's movement and the owners and targets worth looking at, and
// a link to the overview whose numbers it repeats.
func TestRenderDataQualityDigest(t *testing.T) {
	t.Parallel()

	en := Render("en-US", dataQualityDigestEvent(dataQualityDigestDetail()), "https://app.example.com/", time.Now())
	require.Contains(t, en.Title, "Data quality weekly digest")
	require.Contains(t, en.Title, "2026-10-20 ~ 2026-10-27")
	require.Contains(t, en.Text, "Period: 2026-10-20 01:00 → 2026-10-27 01:00 (UTC)")
	require.Contains(t, en.Text, "Executions: 42 completed · 30 passed · 4 warned · 6 failed · 2 errored")
	require.Contains(t, en.Text, "Problems: 5 new · 3 recovered · 12 open (7 warning · 4 error · 1 critical)")
	require.Contains(t, en.Text, "Coverage: 8 targets · 25 checks")
	require.Contains(t, en.Text, "Top owners: Alice (6 open · 4 failing), unassigned (3 open · 2 failing)")
	require.Contains(t, en.Text, "Top failing targets: prod/shop/public/orders (4 failed · 1 errored · 3 open)")
	require.Equal(t, "https://app.example.com/data-quality", en.Link)

	zh := Render("zh-CN", dataQualityDigestEvent(dataQualityDigestDetail()), "", time.Now())
	require.Contains(t, zh.Title, "数据质量周报")
	require.Contains(t, zh.Title, "2026-10-20 ~ 2026-10-27")
	require.Contains(t, zh.Text, "周期：2026-10-20 01:00 → 2026-10-27 01:00（UTC）")
	require.Contains(t, zh.Text, "执行：完成 42 项 · 通过 30 · 警告 4 · 失败 6 · 错误 2")
	require.Contains(t, zh.Text, "问题：新增 5 · 恢复 3 · 未解决 12（警告 7 · 错误 4 · 严重 1）")
	require.Contains(t, zh.Text, "覆盖：8 个目标 · 25 条规则")
	require.Contains(t, zh.Text, "主要负责人：Alice（未解决 6 · 失败规则 4）、未指派（未解决 3 · 失败规则 2）")
	require.Contains(t, zh.Text, "失败最多的目标：prod/shop/public/orders（失败 4 · 错误 1 · 未解决 3）")
	require.Empty(t, zh.Link, "no external URL means no link")
}

// A digest with no period and no owners is still renderable: the totals carry the
// summary, and the empty sections are left out rather than rendered as blanks.
func TestRenderDataQualityDigestWithoutPeriodOrBreakdown(t *testing.T) {
	t.Parallel()

	rendered := Render("en-US", dataQualityDigestEvent(&storepb.DataQualityDigestDetail{
		ExecutionCount: 3,
		PassedCount:    3,
	}), "", time.Now())
	require.Equal(t, "Data quality weekly digest", rendered.Title)
	require.Contains(t, rendered.Text, "Executions: 3 completed · 3 passed · 0 warned · 0 failed · 0 errored")
	require.NotContains(t, rendered.Text, "Top owners")
	require.NotContains(t, rendered.Text, "Top failing targets")
}

// The period label is written in the schedule's zone, not UTC: a UTC+13 morning
// slot falls on the previous UTC day, so a UTC label would name the wrong dates.
// The text window spells the half-open range out, with the zone it is in.
func TestRenderDataQualityDigestUsesTheScheduleZone(t *testing.T) {
	t.Parallel()

	// Monday 2026-10-19 09:00 Pacific/Auckland (+13, DST) is Sunday 2026-10-18
	// 20:00 UTC; the digest covers Monday to Monday local.
	start := time.Date(2026, 10, 19, 9, 0, 0, 0, mustLoadLocation(t, "Pacific/Auckland"))
	end := start.Add(7 * 24 * time.Hour)
	detail := &storepb.DataQualityDigestDetail{
		PeriodStart: timestamppb.New(start),
		PeriodEnd:   timestamppb.New(end),
		Timezone:    "Pacific/Auckland",
	}

	en := Render("en-US", dataQualityDigestEvent(detail), "", time.Now())
	require.Contains(t, en.Title, "2026-10-19 ~ 2026-10-26", "the label names local days, not UTC days")
	require.Contains(t, en.Text, "Period: 2026-10-19 09:00 → 2026-10-26 09:00 (Pacific/Auckland)")

	zh := Render("zh-CN", dataQualityDigestEvent(detail), "", time.Now())
	require.Contains(t, zh.Title, "2026-10-19 ~ 2026-10-26")
	require.Contains(t, zh.Text, "周期：2026-10-19 09:00 → 2026-10-26 09:00（Pacific/Auckland）")
}

// An empty or unreadable zone falls back to UTC instead of dropping the period.
func TestRenderDataQualityDigestFallsBackToUTC(t *testing.T) {
	t.Parallel()

	detail := dataQualityDigestDetail()
	detail.Timezone = "Not/AZone"
	rendered := Render("en-US", dataQualityDigestEvent(detail), "", time.Now())
	require.Contains(t, rendered.Text, "Period: 2026-10-20 01:00 → 2026-10-27 01:00 (UTC)")

	detail.Timezone = ""
	rendered = Render("en-US", dataQualityDigestEvent(detail), "", time.Now())
	require.Contains(t, rendered.Title, "2026-10-20 ~ 2026-10-27")
	require.Contains(t, rendered.Text, "(UTC)")
}

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	require.NoError(t, err)
	return location
}

// An owner whose display name is gone (a deleted account, or a row written before
// names were stored) falls back to the id in the resource name, and the
// unassigned bucket keeps its neutral label rather than printing an empty name.
func TestRenderDataQualityDigestOwnerWithoutDisplayName(t *testing.T) {
	t.Parallel()

	rendered := Render("en-US", dataQualityDigestEvent(&storepb.DataQualityDigestDetail{
		Owners: []*storepb.DataQualityDigestOwner{
			{Owner: "users/9", OpenIssueCount: 2},
			{OpenIssueCount: 1},
		},
	}), "", time.Now())
	require.Contains(t, rendered.Text, "Top owners: 9 (2 open · 0 failing), unassigned (1 open · 0 failing)")

	zh := Render("zh-CN", dataQualityDigestEvent(&storepb.DataQualityDigestDetail{
		Owners: []*storepb.DataQualityDigestOwner{{Owner: "users/9", OpenIssueCount: 2}},
	}), "", time.Now())
	require.Contains(t, zh.Text, "主要负责人：9（未解决 2 · 失败规则 0）")
}
