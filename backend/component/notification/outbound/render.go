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
		if detail.DataQuality.GetExecution() != "" {
			return base + "/data-quality/executions/" + lastSegment(detail.DataQuality.GetExecution())
		}
		if detail.DataQuality.GetIssue() != "" {
			return base + "/data-quality/issues/" + lastSegment(detail.DataQuality.GetIssue())
		}
		return base + "/data-quality/executions"
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
	if i := strings.LastIndexByte(strings.TrimRight(resource, "/"), '/'); i >= 0 {
		return resource[i+1:]
	}
	return resource
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
