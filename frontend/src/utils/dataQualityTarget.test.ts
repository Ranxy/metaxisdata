import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  AggregateConfigSchema,
  CrossColumnConfigSchema,
  FreshnessConfigSchema,
  NullPercentConfigSchema,
  QualityExecutionSummarySchema,
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
  QualityTargetPathSchema,
  QualityTargetSchema,
  ReferentialIntegrityConfigSchema,
  RowCountConfigSchema,
  UniqueCombinationConfigSchema,
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
  summarizeQualityTargets,
  targetDisplayName,
  targetFullPath,
  targetMatchesSearch,
  targetScope,
  unresolvedIssuesByRule,
  worstQualityStatus,
} from "./dataQualityTarget";

function target(guid: string, object: string, column = "") {
  return create(QualityTargetSchema, {
    metaGuid: guid,
    metaType: MetaType.TABLE,
    path: create(QualityTargetPathSchema, {
      instance: "instance-1",
      database: "sales",
      schema: "public",
      object,
      column,
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
  status?: QualityStatus;
  finishSeconds?: bigint;
  orphaned?: boolean;
  schemaChanged?: boolean;
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
    orphanedReason: options.orphaned ? "table dropped" : "",
    targetSchemaChanged: options.schemaChanged ?? false,
    lastResult: options.status
      ? create(QualityExecutionSummarySchema, {
          status: options.status,
          finishTime: create(TimestampSchema, {
            seconds: options.finishSeconds ?? 1_700_000_000n,
          }),
        })
      : undefined,
  });
}

function issue(guid: string, ruleName: string, status: QualityIssueStatus) {
  return create(QualityIssueSchema, {
    name: `workspaces/ws/qualityIssues/${ruleName}`,
    rule: ruleName,
    target: target(guid, "orders"),
    status,
  });
}

function draft(options: {
  guid: string;
  name: string;
  column?: string;
  status?: QualitySuggestionStatus;
  kind?: QualitySuggestionKind;
}) {
  const config = create(QualityRuleConfigSchema, {
    config: options.column
      ? {
          case: "nullPercent",
          value: create(NullPercentConfigSchema, { column: options.column }),
        }
      : undefined,
  });
  return create(QualitySuggestionSchema, {
    name: options.name,
    kind: options.kind ?? QualitySuggestionKind.RULE_DRAFT,
    source: QualitySuggestionSource.NATURAL_LANGUAGE,
    target: target(options.guid, "orders"),
    payload: create(QualitySuggestionPayloadSchema, {
      payload: {
        case: "rule",
        value: create(QualityRuleDraftConfigSchema, {
          config,
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

  it("files a rule that reads several columns as cross-column even without the case", () => {
    const multi = rule({ guid: "g1", columns: ["a", "b"] });
    expect(isCrossColumnRule(multi)).toBe(true);
  });
});

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

describe("draftColumn / draftSectionKey", () => {
  it("reads the column out of a single-column draft config", () => {
    const suggestion = draft({ guid: "g1", name: "d1", column: "email" });
    expect(draftColumn(suggestion)).toBe("email");
    expect(draftSectionKey(suggestion)).toBe("column:email");
  });

  it("files a draft with no named column under the dataset section", () => {
    const suggestion = draft({ guid: "g1", name: "d1" });
    expect(draftColumn(suggestion)).toBe("");
    expect(draftSectionKey(suggestion)).toBe("dataset");
  });

  it("reads the column from the arm that names it differently", () => {
    const config = create(QualityRuleConfigSchema, {
      config: {
        case: "freshness",
        value: create(FreshnessConfigSchema, { timestampColumn: "updated_at" }),
      },
    });
    expect(draftColumn(draftWithConfig("fresh", config))).toBe("updated_at");

    const join = create(QualityRuleConfigSchema, {
      config: {
        case: "referentialIntegrity",
        value: create(ReferentialIntegrityConfigSchema, {
          childColumn: "customer_id",
        }),
      },
    });
    expect(draftColumn(draftWithConfig("join", join))).toBe("customer_id");

    const aggregate = create(QualityRuleConfigSchema, {
      config: {
        case: "aggregate",
        value: create(AggregateConfigSchema, { column: "amount" }),
      },
    });
    expect(draftColumn(draftWithConfig("sum", aggregate))).toBe("amount");
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

describe("summarizeQualityTargets", () => {
  it("groups rules by target and counts their health", () => {
    const summaries = summarizeQualityTargets(
      [
        rule({
          guid: "g1",
          column: "email",
          status: QualityStatus.PASS,
          finishSeconds: 10n,
        }),
        rule({
          guid: "g1",
          column: "phone",
          status: QualityStatus.FAIL,
          finishSeconds: 20n,
        }),
        rule({
          guid: "g1",
          column: "phone",
          state: QualityRuleState.PAUSED,
        }),
        rule({
          guid: "g2",
          object: "customers",
          status: QualityStatus.ERROR,
          orphaned: true,
          schemaChanged: true,
        }),
      ],
      [
        issue("g1", "r1", QualityIssueStatus.OPEN),
        issue("g1", "r1", QualityIssueStatus.ACKNOWLEDGED),
        issue("g1", "r1", QualityIssueStatus.RESOLVED),
        issue("g2", "r2", QualityIssueStatus.OPEN),
      ],
      [
        draft({ guid: "g1", name: "d1", column: "email" }),
        draft({ guid: "g1", name: "d2" }),
        draft({
          guid: "g1",
          name: "d3",
          kind: QualitySuggestionKind.LABEL,
          status: QualitySuggestionStatus.PENDING,
        }),
        draft({
          guid: "g1",
          name: "d4",
          status: QualitySuggestionStatus.ACCEPTED,
        }),
      ]
    );

    expect(summaries).toHaveLength(2);
    const first = summaries.find((summary) => summary.guid === "g1");
    expect(first).toBeDefined();
    expect(first?.ruleCount).toBe(3);
    expect(first?.enabledCount).toBe(2);
    expect(first?.pausedCount).toBe(1);
    expect(first?.columns).toEqual(["email", "phone"]);
    expect(first?.worstStatus).toBe(QualityStatus.FAIL);
    expect(first?.lastRunTime?.seconds).toBe(20n);
    expect(first?.openIssueCount).toBe(2);
    expect(first?.pendingDraftCount).toBe(2);
    expect(first?.pendingLabelCount).toBe(1);
    expect(first?.orphaned).toBe(false);

    const second = summaries.find((summary) => summary.guid === "g2");
    expect(second?.ruleCount).toBe(1);
    expect(second?.orphaned).toBe(true);
    expect(second?.schemaChanged).toBe(true);
    expect(second?.openIssueCount).toBe(1);
  });

  it("skips archived rules and rules without a target GUID", () => {
    const archived = rule({
      guid: "g1",
      state: QualityRuleState.ARCHIVED,
    });
    const noTarget = rule({ guid: "" });
    expect(summarizeQualityTargets([archived, noTarget])).toEqual([]);
  });

  it("does not invent a target for issues or suggestions of an unknown target", () => {
    const summaries = summarizeQualityTargets(
      [rule({ guid: "g1" })],
      [issue("g2", "r2", QualityIssueStatus.OPEN)],
      [draft({ guid: "g2", name: "d1" })]
    );
    expect(summaries).toHaveLength(1);
    expect(summaries[0].openIssueCount).toBe(0);
    expect(summaries[0].pendingDraftCount).toBe(0);
  });
});

describe("sortQualityTargets", () => {
  const summaries = summarizeQualityTargets([
    rule({ guid: "g1", object: "orders", status: QualityStatus.PASS }),
    rule({ guid: "g2", object: "customers", status: QualityStatus.FAIL }),
    rule({ guid: "g3", object: "buses" }),
  ]);

  it("orders by attention: failing, then never-run, then passing", () => {
    const ordered = sortQualityTargets(summaries, "attention");
    expect(ordered.map((summary) => summary.guid)).toEqual(["g2", "g1", "g3"]);
  });

  it("orders by name, rule count and last run", () => {
    expect(
      sortQualityTargets(summaries, "name").map((summary) => summary.guid)
    ).toEqual(["g3", "g2", "g1"]);
    expect(
      sortQualityTargets(summaries, "rules").map((summary) => summary.guid)
    ).toEqual(["g3", "g2", "g1"]);
    expect(
      sortQualityTargets(summaries, "lastRun").map((summary) => summary.guid)
    ).toEqual(["g2", "g1", "g3"]);
  });
});

describe("search and labels", () => {
  const summaries = summarizeQualityTargets([
    rule({ guid: "instance-1;db;public;orders", object: "orders" }),
  ]);

  it("matches the object, the path and the GUID", () => {
    expect(targetMatchesSearch(summaries[0], "")).toBe(true);
    expect(targetMatchesSearch(summaries[0], "ORDER")).toBe(true);
    expect(targetMatchesSearch(summaries[0], "public")).toBe(true);
    expect(targetMatchesSearch(summaries[0], "instance-1")).toBe(true);
    expect(targetMatchesSearch(summaries[0], "customers")).toBe(false);
  });

  it("renders the target name, scope and full path", () => {
    expect(targetDisplayName(summaries[0])).toBe("orders");
    expect(targetScope(summaries[0])).toBe("instance-1 · sales");
    expect(targetFullPath(summaries[0])).toBe("public.orders");
  });

  it("falls back to the GUID's own segments when the path is missing", () => {
    const summary = summarizeQualityTargets([rule({ guid: "g1" })])[0];
    summary.path = undefined;
    expect(targetDisplayName(summary)).toBe("g1");
    expect(targetFullPath(summary)).toBe("-");
  });
});

describe("worstQualityStatus", () => {
  it("ranks error above fail and ignores unset results", () => {
    expect(worstQualityStatus([])).toBeUndefined();
    expect(
      worstQualityStatus([QualityStatus.PASS, undefined, QualityStatus.WARN])
    ).toBe(QualityStatus.WARN);
    expect(worstQualityStatus([QualityStatus.FAIL, QualityStatus.ERROR])).toBe(
      QualityStatus.ERROR
    );
    expect(worstQualityStatus([QualityStatus.UNSPECIFIED])).toBeUndefined();
    expect(worstQualityStatus([QualityStatus.SKIPPED])).toBe(
      QualityStatus.SKIPPED
    );
    expect(
      worstQualityStatus([QualityStatus.NOT_EVALUATED, QualityStatus.PASS])
    ).toBe(QualityStatus.PASS);
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
        draft({ guid: "g1", name: "d-email", column: "email" }),
        draft({ guid: "g1", name: "d-table" }),
        draft({
          guid: "g1",
          name: "d-decided",
          column: "email",
          status: QualitySuggestionStatus.ACCEPTED,
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
      issue("g1", "rule-a", QualityIssueStatus.OPEN),
      issue("g1", "rule-a", QualityIssueStatus.RESOLVED),
      issue("g1", "rule-b", QualityIssueStatus.ACKNOWLEDGED),
    ]);
    expect(byRule.get("rule-a")).toHaveLength(1);
    expect(byRule.get("rule-b")).toHaveLength(1);
    expect(byRule.get("rule-a")?.[0].status).toBe(QualityIssueStatus.OPEN);
  });
});

describe("target summary edge cases", () => {
  function ruleWithFinish(guid: string, seconds: bigint) {
    return rule({
      guid,
      status: QualityStatus.PASS,
      finishSeconds: seconds,
    });
  }

  it("keeps the later of two results in the same second", () => {
    const early = ruleWithFinish("g1", 100n);
    early.lastResult = create(QualityExecutionSummarySchema, {
      status: QualityStatus.PASS,
      finishTime: create(TimestampSchema, { seconds: 100n, nanos: 100 }),
    });
    const late = ruleWithFinish("g1", 100n);
    late.lastResult = create(QualityExecutionSummarySchema, {
      status: QualityStatus.PASS,
      finishTime: create(TimestampSchema, { seconds: 100n, nanos: 500 }),
    });
    const [summary] = summarizeQualityTargets([early, late]);
    expect(summary.lastRunTime?.nanos).toBe(500);
  });

  it("counts a rule whose state is unset as neither enabled nor paused", () => {
    const [summary] = summarizeQualityTargets([
      rule({ guid: "g1", state: QualityRuleState.UNSPECIFIED }),
    ]);
    expect(summary.ruleCount).toBe(1);
    expect(summary.enabledCount).toBe(0);
    expect(summary.pausedCount).toBe(0);
  });

  it("derives the scope and path from the GUID when the snapshot is missing", () => {
    const [summary] = summarizeQualityTargets([
      rule({ guid: "instance-1;db;public;orders" }),
    ]);
    summary.path = undefined;
    expect(targetScope(summary)).toBe("instance-1 · db");
    expect(targetFullPath(summary)).toBe("public.orders");
  });
});

describe("stable ordering", () => {
  it("keeps equal names in a stable order", () => {
    const a = rule({ guid: "instance-1;db;public;orders", object: "orders" });
    const b = rule({ guid: "instance-1;db;other;orders", object: "orders" });
    b.target = create(QualityTargetSchema, {
      metaGuid: "instance-1;db;other;orders",
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "instance-1",
        database: "db",
        schema: "other",
        object: "orders",
      }),
      columns: [],
    });
    const ordered = sortQualityTargets(summarizeQualityTargets([a, b]), "name");
    expect(ordered).toHaveLength(2);
    expect(
      ordered.map((summary) => summary.guid).sort((x, y) => x.localeCompare(y))
    ).toEqual(["instance-1;db;other;orders", "instance-1;db;public;orders"]);
  });
});

describe("missing finish timestamps", () => {
  it("sorts a timestamp with no seconds below a real one", () => {
    const undated = rule({ guid: "g1", status: QualityStatus.PASS });
    undated.lastResult = create(QualityExecutionSummarySchema, {
      status: QualityStatus.PASS,
      finishTime: create(TimestampSchema, {}),
    });
    const dated = rule({
      guid: "g1",
      status: QualityStatus.PASS,
      finishSeconds: 50n,
    });
    const [summary] = summarizeQualityTargets([undated, dated]);
    expect(summary.lastRunTime?.seconds).toBe(50n);
  });

  it("falls back to the GUID name when the path has no object", () => {
    const [summary] = summarizeQualityTargets([rule({ guid: "g1" })]);
    summary.path = create(QualityTargetPathSchema, { object: "" });
    expect(targetDisplayName(summary)).toBe("g1");
  });
});

describe("bare messages", () => {
  it("treats a rule with no config or target as dataset-level", () => {
    const bare = create(QualityRuleSchema, {});
    expect(ruleColumn(bare)).toBe("");
    expect(isCrossColumnRule(bare)).toBe(false);
    expect(ruleSectionKey(bare)).toBe("dataset");
  });

  it("skips a rule that carries no target at all", () => {
    const bare = create(QualityRuleSchema, {
      state: QualityRuleState.ENABLED,
    });
    expect(summarizeQualityTargets([bare])).toEqual([]);
  });
});
