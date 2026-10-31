package outbound

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Render turns one delivery event into the title, body and link a destination
// receives. It is a pure function of the event, the language and the deployment's
// external URL: the dispatcher renders at send time, so a template fix reaches
// deliveries that were queued before it.
func Render(language string, event *storepb.NotificationDeliveryEvent, externalURL string, occurredAt time.Time) Message {
	zh := NormalizeLanguage(language) == "zh-CN"
	message := Message{
		EventType:  event.GetType(),
		Severity:   event.GetSeverity(),
		OccurredAt: occurredAt,
		Link:       link(externalURL, event),
	}
	switch detail := event.GetDetail().(type) {
	case *storepb.NotificationDeliveryEvent_DataQuality:
		renderDataQuality(zh, detail.DataQuality, &message)
	case *storepb.NotificationDeliveryEvent_DataQualityDigest:
		renderDataQualityDigest(zh, detail.DataQualityDigest, &message)
	case *storepb.NotificationDeliveryEvent_SchemaSync:
		renderSchemaSync(zh, detail.SchemaSync, &message)
	case *storepb.NotificationDeliveryEvent_Openlineage:
		renderOpenLineage(zh, detail.Openlineage, &message)
	default:
		// A message with no detail keeps the type as its title; there is nothing
		// else to say about it.
	}
	if message.Title == "" {
		message.Title = strings.TrimPrefix(event.GetType().String(), "NOTIFICATION_TYPE_")
	}
	return message
}

func renderDataQuality(zh bool, detail *storepb.DataQualityDetail, message *Message) {
	target := detail.GetTargetPath()
	title := detail.GetRuleTitle()
	if title == "" {
		title = detail.GetRule()
	}
	switch detail.GetKind() {
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED:
		if zh {
			message.Title = fmt.Sprintf("数据质量：%s 在 %s 上失败", title, target)
			message.Text = joinFacts(zh,
				fact(zh, "状态", fmt.Sprintf("%s → %s", shortEnum(detail.GetFromStatus()), shortEnum(detail.GetToStatus()))),
				fact(zh, "严重级别", shortEnum(detail.GetSeverity())),
				fact(zh, "观测值", formatObserved(zh, detail.GetObservedValue(), detail.GetObservedUnit())),
				fact(zh, "失败行", fmt.Sprintf("%d/%d", detail.GetFailedRows(), detail.GetTotalRows())),
			)
		} else {
			message.Title = fmt.Sprintf("Data quality: %s failed on %s", title, target)
			message.Text = joinFacts(zh,
				fact(zh, "Status", fmt.Sprintf("%s → %s", shortEnum(detail.GetFromStatus()), shortEnum(detail.GetToStatus()))),
				fact(zh, "Severity", shortEnum(detail.GetSeverity())),
				fact(zh, "Observed", formatObserved(zh, detail.GetObservedValue(), detail.GetObservedUnit())),
				fact(zh, "Failed rows", fmt.Sprintf("%d/%d", detail.GetFailedRows(), detail.GetTotalRows())),
			)
		}
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED:
		if zh {
			message.Title = fmt.Sprintf("数据质量：%s 在 %s 上已恢复", title, target)
			message.Text = joinFacts(zh, fact(zh, "状态", fmt.Sprintf("%s → %s", shortEnum(detail.GetFromStatus()), shortEnum(detail.GetToStatus()))))
		} else {
			message.Title = fmt.Sprintf("Data quality: %s recovered on %s", title, target)
			message.Text = joinFacts(zh, fact(zh, "Status", fmt.Sprintf("%s → %s", shortEnum(detail.GetFromStatus()), shortEnum(detail.GetToStatus()))))
		}
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED:
		if zh {
			message.Title = fmt.Sprintf("数据质量：%s 执行失败", title)
			message.Text = joinFacts(zh,
				fact(zh, "目标", target),
				fact(zh, "错误码", detail.GetErrorCode()),
				fact(zh, "说明", "该检查未能执行，数据本身没有被判定为失败"),
			)
		} else {
			message.Title = fmt.Sprintf("Data quality: %s could not run", title)
			message.Text = joinFacts(zh,
				fact(zh, "Target", target),
				fact(zh, "Error code", detail.GetErrorCode()),
				fact(zh, "Note", "the check could not run; the data was not judged"),
			)
		}
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY:
		if zh {
			message.Title = fmt.Sprintf("数据质量：%s 本次扫描 %d/%d 项检查失败", target, detail.GetFailedRuleCount(), detail.GetTotalRuleCount())
			message.Text = joinFacts(zh, fact(zh, "实例", shortResource(detail.GetInstance())))
		} else {
			message.Title = fmt.Sprintf("Data quality: %d/%d checks failed in one scan on %s", detail.GetFailedRuleCount(), detail.GetTotalRuleCount(), target)
			message.Text = joinFacts(zh, fact(zh, "Instance", shortResource(detail.GetInstance())))
		}
	default:
		if zh {
			message.Title = fmt.Sprintf("数据质量：%s", target)
		} else {
			message.Title = fmt.Sprintf("Data quality: %s", target)
		}
	}
}

// renderDataQualityDigest renders the weekly digest: the covered period in the
// title, then the period's verdict tally, the problem ledger's movement, the
// coverage and the owners and targets worth looking at. It is deliberately a
// summary — the totals carry the whole picture and the link opens the page that
// lists every row behind them.
func renderDataQualityDigest(zh bool, detail *storepb.DataQualityDigestDetail, message *Message) {
	title := "Data quality weekly digest"
	if zh {
		title = "数据质量周报"
	}
	if period := digestPeriodLabel(detail); period != "" {
		if zh {
			title = fmt.Sprintf("%s（%s）", title, period)
		} else {
			title = fmt.Sprintf("%s (%s)", title, period)
		}
	}
	message.Title = title
	message.Text = digestLines(
		digestPeriodLine(zh, detail),
		digestExecutionLine(zh, detail),
		digestIssueLine(zh, detail),
		digestCoverageLine(zh, detail),
		digestOwnerLine(zh, detail.GetOwners()),
		digestTargetLine(zh, detail.GetTargets()),
	)
}

// digestPeriodLocation resolves the zone the period is labelled in: the digest
// schedule's IANA zone, carried on the detail, with an empty or unreadable value
// falling back to UTC rather than dropping the label.
func digestPeriodLocation(detail *storepb.DataQualityDigestDetail) *time.Location {
	timezone := strings.TrimSpace(detail.GetTimezone())
	if timezone == "" {
		return time.UTC
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.UTC
	}
	return location
}

// digestPeriodBounds returns the covered half-open window [start, end) in the
// schedule's zone.
func digestPeriodBounds(detail *storepb.DataQualityDigestDetail) (start, end time.Time, ok bool) {
	if detail.GetPeriodStart() == nil || detail.GetPeriodEnd() == nil {
		return time.Time{}, time.Time{}, false
	}
	location := digestPeriodLocation(detail)
	return detail.GetPeriodStart().AsTime().In(location), detail.GetPeriodEnd().AsTime().In(location), true
}

// digestPeriodLabel renders the covered period as its local calendar days. It is
// the schedule's zone, not UTC: a UTC label names the wrong days for schedules
// east of UTC+9.
func digestPeriodLabel(detail *storepb.DataQualityDigestDetail) string {
	start, end, ok := digestPeriodBounds(detail)
	if !ok {
		return ""
	}
	return start.Format("2006-01-02") + " ~ " + end.Format("2006-01-02")
}

// digestPeriodWindow renders the half-open window with its zone: two bare dates
// read as an inclusive range, and the zone is what makes them mean anything.
func digestPeriodWindow(zh bool, detail *storepb.DataQualityDigestDetail) string {
	start, end, ok := digestPeriodBounds(detail)
	if !ok {
		return ""
	}
	window := start.Format("2006-01-02 15:04") + " → " + end.Format("2006-01-02 15:04")
	if zh {
		return window + "（" + digestPeriodLocation(detail).String() + "）"
	}
	return window + " (" + digestPeriodLocation(detail).String() + ")"
}

func digestPeriodLine(zh bool, detail *storepb.DataQualityDigestDetail) string {
	window := digestPeriodWindow(zh, detail)
	if window == "" {
		return ""
	}
	if zh {
		return "周期：" + window
	}
	return "Period: " + window
}

func digestExecutionLine(zh bool, detail *storepb.DataQualityDigestDetail) string {
	if zh {
		return fmt.Sprintf("执行：完成 %d 项 · 通过 %d · 警告 %d · 失败 %d · 错误 %d",
			detail.GetExecutionCount(), detail.GetPassedCount(), detail.GetWarnedCount(),
			detail.GetFailedCount(), detail.GetErroredCount())
	}
	return fmt.Sprintf("Executions: %d completed · %d passed · %d warned · %d failed · %d errored",
		detail.GetExecutionCount(), detail.GetPassedCount(), detail.GetWarnedCount(),
		detail.GetFailedCount(), detail.GetErroredCount())
}

func digestIssueLine(zh bool, detail *storepb.DataQualityDigestDetail) string {
	if zh {
		return fmt.Sprintf("问题：新增 %d · 恢复 %d · 未解决 %d（警告 %d · 错误 %d · 严重 %d）",
			detail.GetNewIssueCount(), detail.GetRecoveredIssueCount(), detail.GetOpenIssueCount(),
			detail.GetOpenWarningCount(), detail.GetOpenErrorCount(), detail.GetOpenCriticalCount())
	}
	return fmt.Sprintf("Problems: %d new · %d recovered · %d open (%d warning · %d error · %d critical)",
		detail.GetNewIssueCount(), detail.GetRecoveredIssueCount(), detail.GetOpenIssueCount(),
		detail.GetOpenWarningCount(), detail.GetOpenErrorCount(), detail.GetOpenCriticalCount())
}

func digestCoverageLine(zh bool, detail *storepb.DataQualityDigestDetail) string {
	if zh {
		return fmt.Sprintf("覆盖：%d 个目标 · %d 条规则", detail.GetTargetCount(), detail.GetRuleCount())
	}
	return fmt.Sprintf("Coverage: %d targets · %d checks", detail.GetTargetCount(), detail.GetRuleCount())
}

// digestOwnerLine names the owners with the most open problems. An unassigned
// bucket is part of the summary: nobody is watching those rules.
func digestOwnerLine(zh bool, owners []*storepb.DataQualityDigestOwner) string {
	if len(owners) == 0 {
		return ""
	}
	parts := make([]string, 0, len(owners))
	for _, owner := range owners {
		name := owner.GetOwnerName()
		if name == "" {
			name = lastSegment(owner.GetOwner())
		}
		if name == "" {
			name = "unassigned"
			if zh {
				name = "未指派"
			}
		}
		if zh {
			parts = append(parts, fmt.Sprintf("%s（未解决 %d · 失败规则 %d）",
				name, owner.GetOpenIssueCount(), owner.GetFailingRuleCount()))
		} else {
			parts = append(parts, fmt.Sprintf("%s (%d open · %d failing)",
				name, owner.GetOpenIssueCount(), owner.GetFailingRuleCount()))
		}
	}
	if zh {
		return "主要负责人：" + strings.Join(parts, "、")
	}
	return "Top owners: " + strings.Join(parts, ", ")
}

// digestTargetLine names the targets with the most failed checks in the period,
// with the error and open-issue counts that explain how bad it was.
func digestTargetLine(zh bool, targets []*storepb.DataQualityDigestTarget) string {
	if len(targets) == 0 {
		return ""
	}
	parts := make([]string, 0, len(targets))
	for _, target := range targets {
		path := strings.TrimSpace(target.GetTargetPath())
		if path == "" {
			path = "-"
		}
		facts := make([]string, 0, 3)
		if zh {
			facts = append(facts, fmt.Sprintf("失败 %d", target.GetFailedExecutionCount()))
			if target.GetErroredExecutionCount() > 0 {
				facts = append(facts, fmt.Sprintf("错误 %d", target.GetErroredExecutionCount()))
			}
			if target.GetOpenIssueCount() > 0 {
				facts = append(facts, fmt.Sprintf("未解决 %d", target.GetOpenIssueCount()))
			}
			parts = append(parts, fmt.Sprintf("%s（%s）", path, strings.Join(facts, " · ")))
			continue
		}
		facts = append(facts, fmt.Sprintf("%d failed", target.GetFailedExecutionCount()))
		if target.GetErroredExecutionCount() > 0 {
			facts = append(facts, fmt.Sprintf("%d errored", target.GetErroredExecutionCount()))
		}
		if target.GetOpenIssueCount() > 0 {
			facts = append(facts, fmt.Sprintf("%d open", target.GetOpenIssueCount()))
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", path, strings.Join(facts, " · ")))
	}
	if zh {
		return "失败最多的目标：" + strings.Join(parts, "、")
	}
	return "Top failing targets: " + strings.Join(parts, ", ")
}

// digestLines joins the non-empty lines of a digest body. Unlike joinFacts it
// keeps one fact group per line: a digest has more to say than fits one line.
func digestLines(lines ...string) string {
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func renderSchemaSync(zh bool, detail *storepb.SchemaSyncDetail, message *Message) {
	instance := detail.GetInstanceTitle()
	if instance == "" {
		instance = shortResource(detail.GetInstance())
	}
	healthy := detail.GetInstanceError() == "" && detail.GetFailedCount() == 0 && detail.GetUnfinishedCount() == 0
	if healthy {
		if zh {
			message.Title = fmt.Sprintf("模式同步：%s 已完成", instance)
			message.Text = joinFacts(zh,
				fact(zh, "触发", shortEnum(detail.GetTrigger())),
				fact(zh, "成功", strconv.Itoa(int(detail.GetSucceededCount()))),
			)
		} else {
			message.Title = fmt.Sprintf("Schema sync: %s finished", instance)
			message.Text = joinFacts(zh,
				fact(zh, "Trigger", shortEnum(detail.GetTrigger())),
				fact(zh, "Succeeded", strconv.Itoa(int(detail.GetSucceededCount()))),
			)
		}
		return
	}
	if zh {
		message.Title = fmt.Sprintf("模式同步：%s 有失败", instance)
		message.Text = joinFacts(zh,
			fact(zh, "触发", shortEnum(detail.GetTrigger())),
			fact(zh, "成功", strconv.Itoa(int(detail.GetSucceededCount()))),
			fact(zh, "失败", strconv.Itoa(int(detail.GetFailedCount()))),
			fact(zh, "未完成", strconv.Itoa(int(detail.GetUnfinishedCount()))),
			fact(zh, "错误", truncate(detail.GetInstanceError(), 300)),
			fact(zh, "失败库", failedDatabases(detail.GetDatabases(), 5)),
		)
		return
	}
	message.Title = fmt.Sprintf("Schema sync: %s had failures", instance)
	message.Text = joinFacts(zh,
		fact(zh, "Trigger", shortEnum(detail.GetTrigger())),
		fact(zh, "Succeeded", strconv.Itoa(int(detail.GetSucceededCount()))),
		fact(zh, "Failed", strconv.Itoa(int(detail.GetFailedCount()))),
		fact(zh, "Unfinished", strconv.Itoa(int(detail.GetUnfinishedCount()))),
		fact(zh, "Error", truncate(detail.GetInstanceError(), 300)),
		fact(zh, "Failed databases", failedDatabases(detail.GetDatabases(), 5)),
	)
}

func renderOpenLineage(zh bool, detail *storepb.OpenLineageDetail, message *Message) {
	// The failure kind prefix carries the domain; the label switch keys on the
	// bare reason, so trim the whole prefix rather than the last segment.
	kind := strings.TrimPrefix(detail.GetKind().String(), "OPENLINEAGE_FAILURE_KIND_")
	if zh {
		message.Title = fmt.Sprintf("OpenLineage：%s", openLineageKindLabel(kind, true))
		message.Text = joinFacts(zh,
			fact(zh, "命名空间", detail.GetNamespace()),
			fact(zh, "作业", detail.GetJob()),
			fact(zh, "数据集", detail.GetDataset()),
			fact(zh, "事件", fmt.Sprintf("%d/%d 失败", detail.GetFailedCount(), detail.GetReceivedCount())),
			fact(zh, "错误", truncate(detail.GetError(), 300)),
		)
		return
	}
	message.Title = fmt.Sprintf("OpenLineage: %s", openLineageKindLabel(kind, false))
	message.Text = joinFacts(zh,
		fact(zh, "Namespace", detail.GetNamespace()),
		fact(zh, "Job", detail.GetJob()),
		fact(zh, "Dataset", detail.GetDataset()),
		fact(zh, "Events", fmt.Sprintf("%d/%d failed", detail.GetFailedCount(), detail.GetReceivedCount())),
		fact(zh, "Error", truncate(detail.GetError(), 300)),
	)
}

// link points at the page that owns the event. It is empty when the deployment
// has no external URL: a relative link is useless in an email or a chat message.
func link(externalURL string, event *storepb.NotificationDeliveryEvent) string {
	base := strings.TrimRight(strings.TrimSpace(externalURL), "/")
	if base == "" {
		return ""
	}
	switch detail := event.GetDetail().(type) {
	case *storepb.NotificationDeliveryEvent_DataQuality:
		// The data-quality detail pages carry the whole resource name in their
		// route (the pages join the segments back to call the RPC), so the link
		// must append the name, not its last segment.
		if detail.DataQuality.GetExecution() != "" {
			return base + "/data-quality/executions/" + detail.DataQuality.GetExecution()
		}
		if detail.DataQuality.GetIssue() != "" {
			return base + "/data-quality/issues/" + detail.DataQuality.GetIssue()
		}
		return base + "/data-quality/executions"
	case *storepb.NotificationDeliveryEvent_DataQualityDigest:
		// A digest is workspace-level, so it links the overview rather than one
		// resource; that page is what the numbers in the message describe.
		return base + "/data-quality"
	case *storepb.NotificationDeliveryEvent_SchemaSync:
		if detail.SchemaSync.GetInstance() != "" {
			return base + "/instances/" + lastSegment(detail.SchemaSync.GetInstance())
		}
		return base + "/instances"
	case *storepb.NotificationDeliveryEvent_Openlineage:
		return base + "/openlineage/events"
	default:
		return base
	}
}

func lastSegment(resource string) string {
	trimmed := strings.TrimRight(resource, "/")
	if i := strings.LastIndexByte(trimmed, '/'); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}

// shortResource trims a resource name to its last segment for display.
func shortResource(resource string) string {
	return lastSegment(resource)
}

// shortEnum drops the enum prefix: QUALITY_STATUS_FAIL becomes FAIL. The server
// stores enums, not labels, and the destination has no i18n catalog, so the bare
// value is the honest rendering.
func shortEnum(value fmt.Stringer) string {
	name := value.String()
	if i := strings.LastIndexByte(name, '_'); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}

func formatObserved(zh bool, value float64, unit string) string {
	switch unit {
	case "%":
		return strconv.FormatFloat(value, 'f', -1, 64) + "%"
	case "rows":
		if zh {
			return strconv.FormatFloat(value, 'f', 0, 64) + " 行"
		}
		return strconv.FormatFloat(value, 'f', 0, 64) + " rows"
	case "s":
		if zh {
			return strconv.FormatFloat(value, 'f', 0, 64) + " 秒"
		}
		return strconv.FormatFloat(value, 'f', 0, 64) + "s"
	case "":
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return strconv.FormatFloat(value, 'f', -1, 64) + " " + unit
	}
}

func fact(zh bool, label, value string) string {
	if value == "" {
		return ""
	}
	if zh {
		return label + "：" + value
	}
	return label + ": " + value
}

func joinFacts(_ bool, facts ...string) string {
	var kept []string
	for _, fact := range facts {
		if fact != "" {
			kept = append(kept, fact)
		}
	}
	return strings.Join(kept, " · ")
}

func failedDatabases(databases []*storepb.SyncDatabaseResult, limit int) string {
	var names []string
	for _, database := range databases {
		if database.GetState() != storepb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED &&
			database.GetState() != storepb.SyncDatabaseState_SYNC_DATABASE_STATE_UNFINISHED {
			continue
		}
		names = append(names, shortResource(database.GetDatabase()))
		if len(names) >= limit {
			break
		}
	}
	return strings.Join(names, ", ")
}

func openLineageKindLabel(kind string, zh bool) string {
	switch kind {
	case "INVALID_EVENT":
		if zh {
			return "事件无法解析"
		}
		return "event could not be parsed"
	case "LIMIT_EXCEEDED":
		if zh {
			return "超出限额"
		}
		return "limit exceeded"
	case "SCOPE_MISMATCH":
		if zh {
			return "命名空间与密钥作用域不符"
		}
		return "namespace outside the key scope"
	case "PERSIST_FAILED":
		if zh {
			return "事件保存失败"
		}
		return "event could not be stored"
	case "PROCESS_FAILED":
		if zh {
			return "血缘解析失败"
		}
		return "lineage could not be derived"
	case "NAMESPACE_UNMAPPED":
		if zh {
			return "命名空间未映射到实例"
		}
		return "namespace maps to no instance"
	default:
		return kind
	}
}

// truncate cuts a stored error to a size a chat message can carry. The error is
// already bounded at the door; this keeps the message readable.
func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
