import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import enUS from "@/locales/en-US.json";
import zhCN from "@/locales/zh-CN.json";
import {
  NotificationSchema,
  NotificationSeverity,
  NotificationType,
  OpenLineageDetailSchema,
  OpenLineageFailureKind,
  SchemaSyncDetailSchema,
  SyncTrigger,
} from "@/types/proto-es/v1/notification_service_pb";
import { databaseLabel, describeNotification } from "./notificationText";

const CATALOGS: Record<string, unknown> = { "en-US": enUS, "zh-CN": zhCN };

function label(catalog: unknown, key: string): unknown {
  return key.split(".").reduce<unknown>((node, part) => {
    if (typeof node !== "object" || node === null) {
      return undefined;
    }
    return (node as Record<string, unknown>)[part];
  }, catalog);
}

function schemaSync(
  detail: Partial<{
    instance: string;
    instanceTitle: string;
    trigger: SyncTrigger;
    succeeded: number;
    failed: number;
    unfinished: number;
  }>,
  severity: NotificationSeverity
) {
  return create(NotificationSchema, {
    type: NotificationType.SCHEMA_SYNC,
    severity,
    detail: {
      case: "schemaSync",
      value: create(SchemaSyncDetailSchema, {
        instance: detail.instance ?? "instances/inst1",
        instanceTitle: detail.instanceTitle ?? "",
        trigger: detail.trigger ?? SyncTrigger.MANUAL,
        succeededCount: detail.succeeded ?? 0,
        failedCount: detail.failed ?? 0,
        unfinishedCount: detail.unfinished ?? 0,
      }),
    },
  });
}

function openLineage(kind: OpenLineageFailureKind, fields = {}) {
  return create(NotificationSchema, {
    type: NotificationType.OPENLINEAGE,
    severity: NotificationSeverity.WARNING,
    detail: {
      case: "openlineage",
      value: create(OpenLineageDetailSchema, { kind, ...fields }),
    },
  });
}

describe("describeNotification", () => {
  it("titles a successful sync by the instance and points at it", () => {
    const text = describeNotification(
      schemaSync(
        { instanceTitle: "prod", succeeded: 4 },
        NotificationSeverity.INFO
      )
    );
    expect(text.titleKey).toBe("notifications.schemaSyncSucceededTitle");
    expect(text.titleParams).toEqual({ instance: "prod" });
    expect(text.messageKey).toBe("notifications.schemaSyncSummary");
    expect(text.messageParams).toEqual({
      succeeded: 4,
      failed: 0,
      unfinished: 0,
    });
    expect(text.href).toBe("/instances/inst1");
  });

  // A user who asked for the sync gets the instance's name; the periodic scan
  // has no initiator, so the same failure reads as one the scheduler hit.
  it("distinguishes a failed manual sync from a failed scheduled one", () => {
    const manual = describeNotification(
      schemaSync({ failed: 1 }, NotificationSeverity.ERROR)
    );
    expect(manual.titleKey).toBe("notifications.schemaSyncFailedTitle");

    const background = describeNotification(
      schemaSync(
        { trigger: SyncTrigger.BACKGROUND, failed: 1 },
        NotificationSeverity.ERROR
      )
    );
    expect(background.titleKey).toBe(
      "notifications.schemaSyncBackgroundFailedTitle"
    );
  });

  it("falls back to the resource name when the instance has no title", () => {
    const text = describeNotification(
      schemaSync({}, NotificationSeverity.INFO)
    );
    expect(text.titleParams).toEqual({ instance: "instances/inst1" });
  });

  it("has no link for an instance name it cannot address", () => {
    const text = describeNotification(
      schemaSync(
        { instance: "not-an-instance", succeeded: 1 },
        NotificationSeverity.INFO
      )
    );
    expect(text.href).toBeUndefined();
  });

  it("names every OpenLineage failure kind", () => {
    const cases: Array<[OpenLineageFailureKind, string, string]> = [
      [
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_INVALID_EVENT,
        "notifications.openlineageInvalidEventTitle",
        "notifications.openlineageInvalidEventMessage",
      ],
      [
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_LIMIT_EXCEEDED,
        "notifications.openlineageLimitExceededTitle",
        "notifications.openlineageLimitExceededMessage",
      ],
      [
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_SCOPE_MISMATCH,
        "notifications.openlineageScopeMismatchTitle",
        "notifications.openlineageScopeMismatchMessage",
      ],
      [
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_PERSIST_FAILED,
        "notifications.openlineageFailedTitle",
        "notifications.openlineageFailedMessage",
      ],
      [
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_PROCESS_FAILED,
        "notifications.openlineageFailedTitle",
        "notifications.openlineageFailedMessage",
      ],
      [
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED,
        "notifications.openlineageNamespaceUnmappedTitle",
        "notifications.openlineageNamespaceUnmappedMessage",
      ],
      // The server always sets a kind; a numeric value this build does not know
      // still has to render.
      [
        99,
        "notifications.openlineageUnknownTitle",
        "notifications.openlineageUnknownMessage",
      ],
    ];
    for (const [kind, titleKey, messageKey] of cases) {
      const text = describeNotification(openLineage(kind));
      expect(text.titleKey, `kind ${kind}`).toBe(titleKey);
      expect(text.messageKey, `kind ${kind}`).toBe(messageKey);
    }
  });

  it("sends a producer-side failure to the events page and a mapping gap to the mappings", () => {
    expect(
      describeNotification(
        openLineage(
          OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_INVALID_EVENT
        )
      ).href
    ).toBe("/openlineage/events");
    expect(
      describeNotification(
        openLineage(
          OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED,
          {
            dataset: "shop.orders",
          }
        )
      )
    ).toMatchObject({
      href: "/settings/openlineage",
      messageParams: { dataset: "shop.orders" },
    });
  });

  it("carries the namespace a scoped key collided with", () => {
    const text = describeNotification(
      openLineage(
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_SCOPE_MISMATCH,
        {
          namespace: "mysql://db:3306",
        }
      )
    );
    expect(text.messageParams).toEqual({ namespace: "mysql://db:3306" });
  });

  // A client older than the server must render its inbox, not an empty row.
  it("falls back for a type or a detail this build does not know", () => {
    const unknown = describeNotification(
      create(NotificationSchema, { type: 99 as NotificationType })
    );
    expect(unknown.titleKey).toBe("notifications.unknownTitle");
    expect(unknown.messageKey).toBe("notifications.unknownMessage");

    const detailMismatch = describeNotification(
      create(NotificationSchema, { type: NotificationType.OPENLINEAGE })
    );
    expect(detailMismatch.titleKey).toBe("notifications.unknownTitle");

    const unspecified = describeNotification(
      create(NotificationSchema, {
        type: NotificationType.SCHEMA_SYNC,
        detail: { case: undefined },
      })
    );
    expect(unspecified.messageKey).toBe("notifications.unknownMessage");
  });

  // The static i18n checks cannot follow a key a helper returns, so this is what
  // keeps the messages and the catalogs from drifting apart.
  it("names keys every catalog defines", () => {
    const notifications = [
      schemaSync({ succeeded: 1 }, NotificationSeverity.INFO),
      schemaSync({ failed: 1 }, NotificationSeverity.ERROR),
      schemaSync(
        { trigger: SyncTrigger.BACKGROUND, failed: 1 },
        NotificationSeverity.ERROR
      ),
      openLineage(
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_INVALID_EVENT
      ),
      openLineage(
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_LIMIT_EXCEEDED
      ),
      openLineage(
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_SCOPE_MISMATCH
      ),
      openLineage(
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_PERSIST_FAILED
      ),
      openLineage(
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_PROCESS_FAILED
      ),
      openLineage(
        OpenLineageFailureKind.OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED
      ),
      create(NotificationSchema, { type: 99 as NotificationType }),
    ];
    for (const notification of notifications) {
      const text = describeNotification(notification);
      for (const key of [text.titleKey, text.messageKey]) {
        for (const [locale, catalog] of Object.entries(CATALOGS)) {
          expect(label(catalog, key), `${locale} is missing ${key}`).toEqual(
            expect.any(String)
          );
        }
      }
    }
  });
});

describe("databaseLabel", () => {
  it("shows the database name of a resource name", () => {
    expect(databaseLabel("databases/inst1/app")).toBe("app");
    expect(databaseLabel("app")).toBe("app");
  });
});
