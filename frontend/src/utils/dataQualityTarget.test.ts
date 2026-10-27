import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  AcceptedValuesConfigSchema,
  AggregateConfigSchema,
  CrossColumnConfigSchema,
  FreshnessConfigSchema,
  NullPercentConfigSchema,
  QualityIssueSchema,
  QualityIssueStatus,
  QualityRuleConfigSchema,
  QualityRuleDraftConfigSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityStatus,
  QualitySuggestionKind,
  QualitySuggestionPayloadSchema,
  QualitySuggestionSchema,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  QualityTargetOrder,
  QualityTargetPathSchema,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
  RangeConfigSchema,
  ReferentialIntegrityConfigSchema,
  RegexConfigSchema,
  RowCountConfigSchema,
  StringLengthConfigSchema,
  UniqueCombinationConfigSchema,
  UniqueConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  draftColumn,
  draftSectionKey,
  groupTargetChecks,
  isCrossColumnRule,
  ruleColumn,
  ruleSectionKey,
  sortQualityTargets,
  targetDisplayName,
  targetFullPath,
  targetMatchesSearch,
  targetScope,
  unresolvedIssuesByRule,
} from "./dataQualityTarget";

// The server never writes the checked column into the target path snapshot; it
// lives in the rule config and, for display, in the target's column list.
function target(guid: string, object: string, column = "") {
  return create(QualityTargetSchema, {
    metaGuid: guid,
    metaType: MetaType.TABLE,
    path: create(QualityTargetPathSchema, {
      instance: "instance-1",
      database: "sales",
      schema: "public",
      object,
    }),
    columns: column ? [column] : [],
  });
}

function rule(options: {
  guid: string;
  object?: string;
  column?: string;
  name?: string;
  state?: QualityRuleState;
  columns?: string[];
  configCase?: "rowCount" | "nullPercent" | "crossColumn";
}) {
  const column = options.column ?? "";
  const configCase =
    options.configCase ?? (column ? "nullPercent" : "rowCount");
  const config = create(QualityRuleConfigSchema, {
    config:
      configCase === "nullPercent"
        ? {
            case: "nullPercent",
            value: create(NullPercentConfigSchema, { column }),
          }
        : configCase === "crossColumn"
          ? {
              case: "crossColumn",
              value: create(CrossColumnConfigSchema, {
                leftColumn: "a",
                rightColumn: "b",
              }),
            }
          : undefined,
  });
  return create(QualityRuleSchema, {
    name:
      options.name ??
      `workspaces/ws/qualityRules/${options.guid}-${column || "table"}`,
    title: `rule ${column || "table"}`,
    ruleType: column ? QualityRuleType.NULL_PERCENT : QualityRuleType.ROW_COUNT,
    target: create(QualityTargetSchema, {
      metaGuid: options.guid,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "instance-1",
        database: "sales",
        schema: "public",
        object: options.object ?? "orders",
        column,
      }),
      columns: options.columns ?? (column ? [column] : []),
    }),
    config,
    severity: QualitySeverity.WARNING,
    state: options.state ?? QualityRuleState.ENABLED,
  });
}

function issue(ruleName: string, status: QualityIssueStatus) {
  return create(QualityIssueSchema, {
    name: `workspaces/ws/qualityIssues/${ruleName}`,
    rule: ruleName,
    target: target("g1", "orders"),
    status,
  });
}

function draft(options: {
  guid?: string;
  name: string;
  column?: string;
  kind?: QualitySuggestionKind;
  status?: QualitySuggestionStatus;
}) {
  return create(QualitySuggestionSchema, {
    name: options.name,
    kind: options.kind ?? QualitySuggestionKind.RULE_DRAFT,
    source: QualitySuggestionSource.NATURAL_LANGUAGE,
    target: target(options.guid ?? "g1", "orders"),
    payload: create(QualitySuggestionPayloadSchema, {
      payload: {
        case: "rule",
        value: create(QualityRuleDraftConfigSchema, {
          config: create(QualityRuleConfigSchema, {
            config: options.column
              ? {
                  case: "nullPercent",
                  value: create(NullPercentConfigSchema, {
                    column: options.column,
                  }),
                }
              : undefined,
          }),
          title: `draft ${options.column ?? "table"}`,
          severity: QualitySeverity.WARNING,
          confidence: 0.8,
          rationale: "because",
        }),
      },
    }),
    status: options.status ?? QualitySuggestionStatus.PENDING,
  });
}

function draftWithConfig(
  name: string,
  config: ReturnType<typeof create<typeof QualityRuleConfigSchema>>
) {
  return create(QualitySuggestionSchema, {
    name,
    kind: QualitySuggestionKind.RULE_DRAFT,
    source: QualitySuggestionSource.NATURAL_LANGUAGE,
    target: target("g1", "orders"),
    payload: create(QualitySuggestionPayloadSchema, {
      payload: {
        case: "rule",
        value: create(QualityRuleDraftConfigSchema, {
          config,
          title: name,
          severity: QualitySeverity.WARNING,
          confidence: 0.6,
          rationale: "because",
        }),
      },
    }),
    status: QualitySuggestionStatus.PENDING,
  });
}

/**
 * A directory row shaped like the server's `QualityTargetSummary`. `noTarget`
 * and `noPath` model the degraded rows a reader must never see panic on.
 */
function summary(
  options: {
    guid?: string;
    instance?: string;
    database?: string;
    schema?: string;
    object?: string;
    noTarget?: boolean;
    noPath?: boolean;
    ruleCount?: number;
    openIssueCount?: number;
    pendingDraftCount?: number;
    worstStatus?: QualityStatus;
    lastRunTime?: ReturnType<typeof create<typeof TimestampSchema>>;
    schemaChanged?: boolean;
  } = {}
): QualityTargetSummary {
  const guid = options.guid ?? "instance-1;shop;public;orders";
  const target = options.noTarget
    ? undefined
    : create(QualityTargetSchema, {
        metaGuid: guid,
        metaType: MetaType.TABLE,
        path: options.noPath
          ? undefined
          : create(QualityTargetPathSchema, {
              instance: options.instance ?? "instance-1",
              database: options.database ?? "shop",
              schema: options.schema ?? "public",
              object: options.object ?? "orders",
            }),
      });
  return create(QualityTargetSummarySchema, {
    target,
    ruleCount: options.ruleCount ?? 1,
    enabledRuleCount: options.ruleCount ?? 1,
    pausedRuleCount: 0,
    openIssueCount: options.openIssueCount ?? 0,
    pendingDraftCount: options.pendingDraftCount ?? 0,
    worstStatus: options.worstStatus ?? QualityStatus.UNSPECIFIED,
    lastRunTime: options.lastRunTime,
    schemaChanged: options.schemaChanged ?? false,
  });
}

function guids(
  summaries: readonly QualityTargetSummary[]
): Array<string | undefined> {
  return summaries.map((item) => item.target?.metaGuid);
}

describe("ruleColumn / isCrossColumnRule / ruleSectionKey", () => {
  it("files a column rule under its column", () => {
    expect(ruleColumn(rule({ guid: "g1", column: "email" }))).toBe("email");
    expect(ruleSectionKey(rule({ guid: "g1", column: "email" }))).toBe(
      "column:email"
    );
  });

  it("files a table-level rule under the dataset section", () => {
    const tableRule = rule({ guid: "g1" });
    expect(ruleColumn(tableRule)).toBe("");
    expect(isCrossColumnRule(tableRule)).toBe(false);
    expect(ruleSectionKey(tableRule)).toBe("dataset");
  });

  it("treats a rule with no path snapshot as dataset-level", () => {
    const noPath = rule({ guid: "g1" });
    noPath.target = create(QualityTargetSchema, { metaGuid: "g1" });
    expect(ruleColumn(noPath)).toBe("");
    expect(ruleSectionKey(noPath)).toBe("dataset");
  });

  it("files a config with several columns under the cross-column section", () => {
    const cross = rule({
      guid: "g1",
      configCase: "crossColumn",
      columns: ["a", "b"],
    });
    expect(isCrossColumnRule(cross)).toBe(true);
    expect(ruleSectionKey(cross)).toBe("multi");
  });

  it("does not read filter or snapshot columns as the checked column set", () => {
    // A table-level check whose filter (or drift snapshot) references several
    // columns is still a dataset-level check.
    const filtered = rule({ guid: "g1", columns: ["email", "status"] });
    expect(isCrossColumnRule(filtered)).toBe(false);
    expect(ruleSectionKey(filtered)).toBe("dataset");
  });
});

describe("draftColumn / draftSectionKey", () => {
  it("reads the column out of a single-column draft config", () => {
    const suggestion = draft({ name: "d1", column: "email" });
    expect(draftColumn(suggestion)).toBe("email");
    expect(draftSectionKey(suggestion)).toBe("column:email");
  });

  it("files a draft with no named column under the dataset section", () => {
    const suggestion = draft({ name: "d1" });
    expect(draftColumn(suggestion)).toBe("");
    expect(draftSectionKey(suggestion)).toBe("dataset");
  });

  it("reads the column from every config arm that names one", () => {
    const arms: Array<
      [string, ReturnType<typeof create<typeof QualityRuleConfigSchema>>]
    > = [
      [
        "null",
        create(QualityRuleConfigSchema, {
          config: {
            case: "nullPercent",
            value: create(NullPercentConfigSchema, { column: "email" }),
          },
        }),
      ],
      [
        "unique",
        create(QualityRuleConfigSchema, {
          config: {
            case: "unique",
            value: create(UniqueConfigSchema, { column: "email" }),
          },
        }),
      ],
      [
        "accepted",
        create(QualityRuleConfigSchema, {
          config: {
            case: "acceptedValues",
            value: create(AcceptedValuesConfigSchema, { column: "status" }),
          },
        }),
      ],
      [
        "range",
        create(QualityRuleConfigSchema, {
          config: {
            case: "range",
            value: create(RangeConfigSchema, { column: "amount" }),
          },
        }),
      ],
      [
        "regex",
        create(QualityRuleConfigSchema, {
          config: {
            case: "regex",
            value: create(RegexConfigSchema, { column: "code" }),
          },
        }),
      ],
      [
        "length",
        create(QualityRuleConfigSchema, {
          config: {
            case: "stringLength",
            value: create(StringLengthConfigSchema, { column: "name" }),
          },
        }),
      ],
      [
        "freshness",
        create(QualityRuleConfigSchema, {
          config: {
            case: "freshness",
            value: create(FreshnessConfigSchema, {
              timestampColumn: "updated_at",
            }),
          },
        }),
      ],
      [
        "join",
        create(QualityRuleConfigSchema, {
          config: {
            case: "referentialIntegrity",
            value: create(ReferentialIntegrityConfigSchema, {
              childColumn: "customer_id",
            }),
          },
        }),
      ],
      [
        "aggregate",
        create(QualityRuleConfigSchema, {
          config: {
            case: "aggregate",
            value: create(AggregateConfigSchema, { column: "amount" }),
          },
        }),
      ],
    ];
    for (const [name, config] of arms) {
      expect(draftColumn(draftWithConfig(name, config))).not.toBe("");
    }
  });

  it("has no column for a typeless or column-less draft config", () => {
    const blank = create(QualityRuleConfigSchema, {
      config: {
        case: "nullPercent",
        value: create(NullPercentConfigSchema, {}),
      },
    });
    expect(draftColumn(draftWithConfig("blank", blank))).toBe("");
    expect(draftSectionKey(draftWithConfig("blank", blank))).toBe("dataset");

    const rowCount = create(QualityRuleConfigSchema, {
      config: { case: "rowCount", value: create(RowCountConfigSchema, {}) },
    });
    expect(draftColumn(draftWithConfig("rows", rowCount))).toBe("");
    expect(draftSectionKey(draftWithConfig("rows", rowCount))).toBe("dataset");

    const noPayload = create(QualitySuggestionSchema, {
      name: "no-payload",
      kind: QualitySuggestionKind.RULE_DRAFT,
      status: QualitySuggestionStatus.PENDING,
      target: target("g1", "orders"),
    });
    expect(draftColumn(noPayload)).toBe("");
    expect(draftSectionKey(noPayload)).toBe("dataset");
  });

  it("files a draft about several columns under the cross-column section", () => {
    const config = create(QualityRuleConfigSchema, {
      config: {
        case: "uniqueCombination",
        value: create(UniqueCombinationConfigSchema, { columns: ["a", "b"] }),
      },
    });
    const suggestion = draftWithConfig("pair", config);
    expect(draftColumn(suggestion)).toBe("");
    expect(draftSectionKey(suggestion)).toBe("multi");
  });
});

describe("groupTargetChecks", () => {
  it("orders dataset, cross-column and column sections", () => {
    const sections = groupTargetChecks(
      [
        rule({ guid: "g1", column: "phone" }),
        rule({ guid: "g1" }),
        rule({ guid: "g1", column: "email" }),
        rule({ guid: "g1", configCase: "crossColumn", columns: ["a", "b"] }),
      ],
      [
        draft({ name: "d-email", column: "email" }),
        draft({ name: "d-table" }),
        draft({
          name: "d-decided",
          column: "email",
          status: QualitySuggestionStatus.ACCEPTED,
        }),
        draft({
          name: "d-label",
          kind: QualitySuggestionKind.LABEL,
        }),
      ]
    );

    expect(sections.map((section) => section.key)).toEqual([
      "dataset",
      "multi",
      "column:email",
      "column:phone",
    ]);
    expect(sections[0].rules).toHaveLength(1);
    expect(sections[0].drafts.map((suggestion) => suggestion.name)).toEqual([
      "d-table",
    ]);
    expect(sections[2].rules).toHaveLength(1);
    expect(sections[2].drafts.map((suggestion) => suggestion.name)).toEqual([
      "d-email",
    ]);
    expect(sections[3].drafts).toEqual([]);
  });

  it("skips archived rules and leaves a target with no rules empty", () => {
    const sections = groupTargetChecks([
      rule({ guid: "g1", state: QualityRuleState.ARCHIVED }),
    ]);
    expect(sections).toEqual([]);
  });
});

describe("unresolvedIssuesByRule", () => {
  it("groups unresolved issues by rule and drops resolved ones", () => {
    const byRule = unresolvedIssuesByRule([
      issue("rule-a", QualityIssueStatus.OPEN),
      issue("rule-a", QualityIssueStatus.RESOLVED),
      issue("rule-b", QualityIssueStatus.ACKNOWLEDGED),
    ]);
    expect(byRule.get("rule-a")).toHaveLength(1);
    expect(byRule.get("rule-b")).toHaveLength(1);
    expect(byRule.get("rule-a")?.[0].status).toBe(QualityIssueStatus.OPEN);
  });
});

describe("target labels", () => {
  it("names the target by its path snapshot", () => {
    const row = summary({ object: "orders" });
    expect(targetDisplayName(row)).toBe("orders");
    expect(targetScope(row)).toBe("instance-1 · shop");
    expect(targetFullPath(row)).toBe("public.orders");
  });

  it("falls back to the GUID's own path segments when the snapshot is missing", () => {
    const row = summary({ noPath: true, guid: "instance-1;db;public;orders" });
    expect(targetDisplayName(row)).toBe("orders");
    expect(targetScope(row)).toBe("instance-1 · db");
    expect(targetFullPath(row)).toBe("public.orders");
  });

  it("falls back from an empty path to the GUID segments", () => {
    const row = summary({
      guid: "instance-1;shop;public;orders",
      instance: "",
      database: "",
      schema: "",
      object: "",
    });
    expect(targetDisplayName(row)).toBe("orders");
    expect(targetScope(row)).toBe("instance-1 · shop");
    expect(targetFullPath(row)).toBe("public.orders");
  });

  it("falls back to the GUID when it carries no object segment", () => {
    const row = summary({ noPath: true, guid: "g1" });
    expect(targetDisplayName(row)).toBe("g1");
    expect(targetScope(row)).toBe("g1");
    expect(targetFullPath(row)).toBe("-");
  });

  it("renders a bare summary without panicking", () => {
    const row = summary({ noTarget: true, guid: "" });
    expect(targetDisplayName(row)).toBe("");
    expect(targetScope(row)).toBe("-");
    expect(targetFullPath(row)).toBe("-");
  });

  it("uses the GUID for scope when the path has no instance or database", () => {
    const row = summary({
      guid: "instance-1;shop;public;orders",
      noPath: true,
    });
    expect(targetScope(row)).toBe("instance-1 · shop");
  });
});

describe("targetMatchesSearch", () => {
  const row = summary({
    object: "orders",
    guid: "instance-1;shop;public;orders",
  });

  it("matches the object, the path and the GUID", () => {
    expect(targetMatchesSearch(row, "")).toBe(true);
    expect(targetMatchesSearch(row, "ORDER")).toBe(true);
    expect(targetMatchesSearch(row, "public")).toBe(true);
    expect(targetMatchesSearch(row, "instance-1")).toBe(true);
    expect(targetMatchesSearch(row, "customers")).toBe(false);
  });

  it("matches through the GUID's path when the snapshot is missing", () => {
    const noTarget = summary({
      noPath: true,
      guid: "instance-1;shop;public;orders",
    });
    expect(targetMatchesSearch(noTarget, "orders")).toBe(true);
    expect(targetMatchesSearch(noTarget, "customers")).toBe(false);
  });

  it("does not match a bare summary against a real term", () => {
    expect(
      targetMatchesSearch(summary({ noTarget: true, guid: "" }), "orders")
    ).toBe(false);
  });
});

describe("sortQualityTargets", () => {
  function run(
    guid: string,
    object: string,
    extra: Parameters<typeof summary>[0] = {}
  ) {
    return summary({ guid, object, ...extra });
  }

  it("orders by attention: error, fail, warn, pass, skipped, never run", () => {
    const rows = [
      run("g-never", "target-never"),
      run("g-skipped", "target-skipped", {
        worstStatus: QualityStatus.SKIPPED,
      }),
      run("g-not-evaluated", "target-not-evaluated", {
        worstStatus: QualityStatus.NOT_EVALUATED,
      }),
      run("g-pass", "target-pass", { worstStatus: QualityStatus.PASS }),
      run("g-warn", "target-warn", { worstStatus: QualityStatus.WARN }),
      run("g-fail", "target-fail", { worstStatus: QualityStatus.FAIL }),
      run("g-error", "target-error", { worstStatus: QualityStatus.ERROR }),
    ];

    expect(
      guids(sortQualityTargets(rows, QualityTargetOrder.ATTENTION))
    ).toEqual([
      "g-error",
      "g-fail",
      "g-warn",
      "g-pass",
      "g-not-evaluated",
      "g-skipped",
      "g-never",
    ]);
  });

  it("breaks attention ties by open issues, then drafts, then name", () => {
    const rows = [
      run("g-beta", "beta", { worstStatus: QualityStatus.FAIL }),
      run("g-alpha", "alpha", { worstStatus: QualityStatus.FAIL }),
      run("g-issues", "gamma", {
        worstStatus: QualityStatus.FAIL,
        openIssueCount: 2,
      }),
      run("g-issue", "delta", {
        worstStatus: QualityStatus.FAIL,
        openIssueCount: 1,
      }),
      run("g-draft", "epsilon", {
        worstStatus: QualityStatus.FAIL,
        pendingDraftCount: 1,
      }),
    ];

    expect(
      guids(sortQualityTargets(rows, QualityTargetOrder.ATTENTION))
    ).toEqual(["g-issues", "g-issue", "g-draft", "g-alpha", "g-beta"]);
  });

  it("orders by name and falls back to the GUID for equal names", () => {
    const rows = [
      run("x;db;public;orders", "orders"),
      run("a;db;public;orders", "orders"),
      run("g-buses", "buses"),
      run("g-customers", "customers"),
    ];

    expect(guids(sortQualityTargets(rows, QualityTargetOrder.NAME))).toEqual([
      "g-buses",
      "g-customers",
      "a;db;public;orders",
      "x;db;public;orders",
    ]);
  });

  it("orders by rule count, most rules first", () => {
    const rows = [
      run("g-one", "one", { ruleCount: 1 }),
      run("g-three", "three", { ruleCount: 3 }),
      run("g-two", "two", { ruleCount: 2 }),
    ];

    expect(
      guids(sortQualityTargets(rows, QualityTargetOrder.RULE_COUNT))
    ).toEqual(["g-three", "g-two", "g-one"]);
  });

  it("orders by last run, newest first, with never-run targets last", () => {
    const time = (seconds: bigint, nanos = 0) =>
      create(TimestampSchema, { seconds, nanos });
    const rows = [
      run("g-old", "old", { lastRunTime: time(100n) }),
      run("g-new", "new", { lastRunTime: time(200n) }),
      run("g-late", "late", { lastRunTime: time(100n, 500) }),
      run("g-never", "never"),
      run("g-never-b", "never-b"),
    ];

    expect(
      guids(sortQualityTargets(rows, QualityTargetOrder.LAST_RUN))
    ).toEqual(["g-new", "g-late", "g-old", "g-never", "g-never-b"]);
  });

  it("treats an unspecified order as attention", () => {
    const rows = [
      run("g-pass", "pass", { worstStatus: QualityStatus.PASS }),
      run("g-fail", "fail", { worstStatus: QualityStatus.FAIL }),
    ];

    expect(
      guids(sortQualityTargets(rows, QualityTargetOrder.UNSPECIFIED))
    ).toEqual(["g-fail", "g-pass"]);
  });

  it("keeps duplicate rows stable when every key ties", () => {
    const rows = [
      run("g-same", "same", { worstStatus: QualityStatus.PASS }),
      run("g-same", "same", { worstStatus: QualityStatus.PASS }),
    ];

    expect(
      guids(sortQualityTargets(rows, QualityTargetOrder.ATTENTION))
    ).toEqual(["g-same", "g-same"]);
    expect(guids(sortQualityTargets(rows, QualityTargetOrder.NAME))).toEqual([
      "g-same",
      "g-same",
    ]);
    expect(
      guids(sortQualityTargets(rows, QualityTargetOrder.LAST_RUN))
    ).toEqual(["g-same", "g-same"]);
  });
});
