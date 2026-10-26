import { QualityObservedKind } from "@/types/proto-es/v1/data_quality_service_pb";
import {
  type DataQualityDetail,
  DataQualityNotificationKind,
  type Notification,
  NotificationSeverity,
  NotificationType,
  type OpenLineageDetail,
  OpenLineageFailureKind,
  type SchemaSyncDetail,
  SyncTrigger,
} from "@/types/proto-es/v1/notification_service_pb";
import { formatObservedValue as formatDataQualityValue } from "@/utils/dataQuality";

/**
 * Everything the UI needs to render one message, as i18n keys and parameters
 * rather than text. The server stores no prose — a type plus a structured detail
 * is the whole payload — so the rendering lives here, where it is a pure
 * function the tests can pin without mounting a component and where both locales
 * are one catalog apart.
 */
export interface NotificationText {
  /** Title line, translated with the parameters below. */
  titleKey: string;
  titleParams: Record<string, string | number>;
  /**
   * Body line: what happened, in one sentence. Facts the sentence would only
   * repeat — the failing databases, the ingestion key, the raw error — are
   * rendered from the detail by the page instead of folded in here.
   */
  messageKey: string;
  messageParams: Record<string, string | number>;
  /** Where the message points, when it points anywhere. */
  href?: string;
}

const INSTANCE_PREFIX = "instances/";

/** The route of an instance resource name (`instances/{id}`). */
function instanceHref(instance: string): string | undefined {
  const id = instance.startsWith(INSTANCE_PREFIX)
    ? instance.slice(INSTANCE_PREFIX.length)
    : "";
  return id ? `/instances/${id}` : undefined;
}

/** The database's display name, from `databases/{instance}/{database}`. */
export function databaseLabel(database: string): string {
  const parts = database.split("/");
  return parts.length === 3 ? parts[2] : database;
}

function schemaSyncText(
  detail: SchemaSyncDetail,
  severity: NotificationSeverity
): NotificationText {
  const text: NotificationText = {
    titleKey: "notifications.unknownTitle",
    titleParams: { instance: detail.instanceTitle || detail.instance },
    messageKey: "notifications.schemaSyncSummary",
    messageParams: {
      succeeded: detail.succeededCount,
      failed: detail.failedCount,
      unfinished: detail.unfinishedCount,
    },
    href: instanceHref(detail.instance),
  };

  if (severity === NotificationSeverity.INFO) {
    text.titleKey = "notifications.schemaSyncSucceededTitle";
  } else if (detail.trigger === SyncTrigger.BACKGROUND) {
    // Nobody asked for this one: it is the periodic scan reporting a target that
    // stays broken, which reads differently from a failure the user just caused.
    text.titleKey = "notifications.schemaSyncBackgroundFailedTitle";
  } else {
    text.titleKey = "notifications.schemaSyncFailedTitle";
  }
  return text;
}

function openLineageText(detail: OpenLineageDetail): NotificationText {
  const text: NotificationText = {
    titleKey: "notifications.openlineageUnknownTitle",
    titleParams: {},
    messageKey: "notifications.openlineageUnknownMessage",
    messageParams: {},
    href: "/openlineage/events",
  };

  switch (detail.kind) {
    case OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_INVALID_EVENT:
      text.titleKey = "notifications.openlineageInvalidEventTitle";
      text.messageKey = "notifications.openlineageInvalidEventMessage";
      break;
    case OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_LIMIT_EXCEEDED:
      text.titleKey = "notifications.openlineageLimitExceededTitle";
      text.messageKey = "notifications.openlineageLimitExceededMessage";
      break;
    case OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_SCOPE_MISMATCH:
      text.titleKey = "notifications.openlineageScopeMismatchTitle";
      text.messageKey = "notifications.openlineageScopeMismatchMessage";
      text.messageParams = { namespace: detail.namespace };
      break;
    case OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_PERSIST_FAILED:
    case OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_PROCESS_FAILED:
      text.titleKey = "notifications.openlineageFailedTitle";
      text.messageKey = "notifications.openlineageFailedMessage";
      text.messageParams = { namespace: detail.namespace };
      break;
    case OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED:
      text.titleKey = "notifications.openlineageNamespaceUnmappedTitle";
      text.messageKey = "notifications.openlineageNamespaceUnmappedMessage";
      text.messageParams = { dataset: detail.dataset };
      // A dataset the registry could not place is fixed in the namespace mapping
      // settings, not on the events page.
      text.href = "/settings/openlineage";
      break;
    default:
      // A kind this build does not know: the title and message stay generic, and
      // the message still points at the events page.
      break;
  }
  return text;
}

const DATA_QUALITY_PATH = "/data-quality";

/** The app path of a quality issue or execution, from its full resource name. */
function qualityResourceHref(
  resource: "issues" | "executions",
  name: string
): string | undefined {
  return name ? `${DATA_QUALITY_PATH}/${resource}/${name}` : undefined;
}

/** The unit a data quality detail recorded, mapped to the observed-value kind. */
function observedKindFromUnit(unit: string): QualityObservedKind {
  switch (unit) {
    case "%":
      return QualityObservedKind.RATIO;
    case "rows":
      return QualityObservedKind.COUNT;
    case "s":
      return QualityObservedKind.SECONDS;
    case "timestamp":
      return QualityObservedKind.TIMESTAMP;
    default:
      return QualityObservedKind.NUMBER;
  }
}

/** The observed value as a sentence fragment: "4%", "1,204", "12 s". */
function observedLabel(detail: DataQualityDetail): string {
  const kind = observedKindFromUnit(detail.observedUnit);
  const value = formatDataQualityValue(detail.observedValue, kind);
  return kind === QualityObservedKind.NUMBER && detail.observedUnit
    ? `${value} ${detail.observedUnit}`
    : value;
}

function dataQualityText(detail: DataQualityDetail): NotificationText {
  const rule = detail.ruleTitle || detail.rule.split("/").pop() || "";
  const target = detail.targetPath || detail.targetGuid;
  const issue = qualityResourceHref("issues", detail.issue);
  const execution = qualityResourceHref("executions", detail.execution);
  const executionsPage = `${DATA_QUALITY_PATH}/executions`;
  switch (detail.kind) {
    case DataQualityNotificationKind.ISSUE_OPENED:
      return {
        titleKey: "notifications.dataQualityIssueOpenedTitle",
        titleParams: { rule },
        messageKey: "notifications.dataQualityIssueOpenedMessage",
        messageParams: { observed: observedLabel(detail), target },
        href: issue ?? execution,
      };
    case DataQualityNotificationKind.ISSUE_RECOVERED:
      return {
        titleKey: "notifications.dataQualityIssueRecoveredTitle",
        titleParams: { rule },
        messageKey: "notifications.dataQualityIssueRecoveredMessage",
        messageParams: { target },
        href: issue ?? execution,
      };
    case DataQualityNotificationKind.EXECUTION_FAILED:
      return {
        titleKey: "notifications.dataQualityExecutionFailedTitle",
        titleParams: { rule },
        messageKey: "notifications.dataQualityExecutionFailedMessage",
        messageParams: { code: detail.errorCode || "-" },
        href: execution ?? executionsPage,
      };
    case DataQualityNotificationKind.STORM_SUMMARY: {
      // A run belongs to one target now, so the summary names that target. Old
      // rows may predate the target fields and fall back to the instance.
      const stormTarget =
        detail.targetPath || detail.targetGuid || detail.instance || "-";
      return {
        titleKey: "notifications.dataQualityStormSummaryTitle",
        titleParams: {},
        messageKey: "notifications.dataQualityStormSummaryMessage",
        messageParams: {
          failed: detail.failedRuleCount,
          total: detail.totalRuleCount,
          target: stormTarget,
        },
        // A storm summary merges one target's batch, so it links to no single
        // execution: the executions list is where the individual rows live.
        href: execution ?? executionsPage,
      };
    }
    default:
      return {
        titleKey: "notifications.dataQualityUnknownTitle",
        titleParams: {},
        messageKey: "notifications.dataQualityUnknownMessage",
        messageParams: {},
      };
  }
}

/**
 * Describes one notification in the reader's language. A type or a detail this
 * build does not know degrades to a generic line instead of an empty row, so a
 * client older than its server still renders its inbox.
 */
export function describeNotification(
  notification: Notification
): NotificationText {
  switch (notification.type) {
    case NotificationType.SCHEMA_SYNC:
      if (notification.detail.case === "schemaSync") {
        return schemaSyncText(notification.detail.value, notification.severity);
      }
      break;
    case NotificationType.OPENLINEAGE:
      if (notification.detail.case === "openlineage") {
        return openLineageText(notification.detail.value);
      }
      break;
    case NotificationType.DATA_QUALITY:
      if (notification.detail.case === "dataQuality") {
        return dataQualityText(notification.detail.value);
      }
      break;
    default:
      // An unknown type, or a known one whose detail the server did not send.
      break;
  }
  return {
    titleKey: "notifications.unknownTitle",
    titleParams: {},
    messageKey: "notifications.unknownMessage",
    messageParams: {},
  };
}
