import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  ColumnProfileSchema,
  ColumnProfileStatsSchema,
  ColumnSemanticLabelSchema,
  QualityDimension,
  QualityLabelSource,
  QualityLabelStatus,
  QualityLLMDiagnosticSchema,
  QualityRuleConfigSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySemanticType,
  QualitySeverity,
  QualitySuggestionJobSchema,
  QualitySuggestionJobStatus,
  QualitySuggestionJobTargetSchema,
  QualitySuggestionJobTargetState,
  QualitySuggestionKind,
  QualitySuggestionQueueSummarySchema,
  QualitySuggestionSchema,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  QualityTargetSchema,
  RowCountConfigSchema,
  SemanticLabelEvidenceSchema,
  SemanticLabelSuggestionSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { qualityRuleTypeKey } from "@/utils/dataQuality";
import { emptyConfig, emptyRuleForm } from "./dataQualityForm";
import {
  buildRuleFromSuggestion,
  columnProfileHighlights,
  configEntryLabelKey,
  diagnosticCodeKey,
  diagnosticHintKey,
  diagnosticSummary,
  formatConfidence,
  formatDiagnosticCount,
  groupSelectionState,
  groupSuggestionsByTarget,
  isActiveSuggestionJob,
  isLabelSuggestion,
  isPendingDraft,
  isPendingLabel,
  isPendingSuggestion,
  jobIntentSnippet,
  jobSuggestionNames,
  jobTargetLabel,
  labelEvidenceLines,
  labelSourceKey,
  labelStatusKey,
  labelSuggestionOf,
  labelSummary,
  MAX_SUGGESTION_JOB_TARGETS,
  ruleConfigEntries,
  ruleDraftOf,
  selectableSuggestions,
  selectionCountsByKind,
  semanticTypeKey,
  suggestionConfidence,
  suggestionConfidencePercent,
  suggestionGroupMetaType,
  suggestionJobProgress,
  suggestionJobStatusKey,
  suggestionJobStatusVariant,
  suggestionJobTargetStateKey,
  suggestionJobTargetStateVariant,
  suggestionKindKey,
  suggestionLabelTypeKey,
  suggestionRationale,
  suggestionRowText,
  suggestionRuleDraftTypeKey,
  suggestionSeverity,
  suggestionSourceKey,
  suggestionStatusKey,
  suggestionStatusVariant,
  suggestionSummaryReason,
  suggestionSummaryView,
  suggestionTargetLabel,
  suggestionTargetPath,
  suggestionToEditorPrefill,
  summarizeBatchResults,
  syncDraftErrorHintKey,
} from "./dataQualitySuggestion";

function ruleSuggestion() {
  return create(QualitySuggestionSchema, {
    name: "workspaces/ws/qualitySuggestions/s1",
    kind: QualitySuggestionKind.RULE_DRAFT,
    source: QualitySuggestionSource.NATURAL_LANGUAGE,
    status: QualitySuggestionStatus.PENDING,
    target: create(QualityTargetSchema, {
      metaGuid: "inst1;db1;public;orders",
      metaType: MetaType.TABLE,
      columns: [],
    }),
    payload: {
      payload: {
        case: "rule",
        value: {
          title: "Orders should have rows",
          rationale: "The table is the fact table for orders.",
          severity: 2,
          confidence: 0.82,
          config: create(QualityRuleConfigSchema, {
            config: {
              case: "rowCount",
              value: create(RowCountConfigSchema, { minRows: 1n }),
            },
            thresholdMetric: 1,
          }),
        },
      },
    },
  });
}

describe("suggestion vocabulary", () => {
  it("names every kind, source and state", () => {
    expect(suggestionKindKey(QualitySuggestionKind.RULE_DRAFT)).toBe(
      "dataQuality.suggestionKind.ruleDraft"
    );
    expect(suggestionKindKey(QualitySuggestionKind.LABEL)).toBe(
      "dataQuality.suggestionKind.label"
    );
    expect(suggestionKindKey(QualitySuggestionKind.UNSPECIFIED)).toBe(
      "dataQuality.suggestionKind.unspecified"
    );
    expect(suggestionKindKey(99 as QualitySuggestionKind)).toBe(
      "dataQuality.suggestionKind.unspecified"
    );

    expect(suggestionSourceKey(QualitySuggestionSource.NATURAL_LANGUAGE)).toBe(
      "dataQuality.suggestionSource.naturalLanguage"
    );
    expect(suggestionSourceKey(QualitySuggestionSource.PROFILE)).toBe(
      "dataQuality.suggestionSource.profile"
    );
    expect(suggestionSourceKey(QualitySuggestionSource.UNSPECIFIED)).toBe(
      "dataQuality.suggestionSource.unspecified"
    );
    expect(suggestionSourceKey(99 as QualitySuggestionSource)).toBe(
      "dataQuality.suggestionSource.unspecified"
    );

    expect(suggestionStatusKey(QualitySuggestionStatus.PENDING)).toBe(
      "dataQuality.suggestionStatus.pending"
    );
    expect(suggestionStatusKey(QualitySuggestionStatus.ACCEPTED)).toBe(
      "dataQuality.suggestionStatus.accepted"
    );
    expect(suggestionStatusKey(QualitySuggestionStatus.REJECTED)).toBe(
      "dataQuality.suggestionStatus.rejected"
    );
    expect(suggestionStatusKey(QualitySuggestionStatus.SUPERSEDED)).toBe(
      "dataQuality.suggestionStatus.superseded"
    );
    expect(suggestionStatusKey(QualitySuggestionStatus.UNSPECIFIED)).toBe(
      "dataQuality.suggestionStatus.unspecified"
    );
    expect(suggestionStatusKey(99 as QualitySuggestionStatus)).toBe(
      "dataQuality.suggestionStatus.unspecified"
    );
  });

  it("tones the decision state", () => {
    expect(suggestionStatusVariant(QualitySuggestionStatus.PENDING)).toBe(
      "warning"
    );
    expect(suggestionStatusVariant(QualitySuggestionStatus.ACCEPTED)).toBe(
      "success"
    );
    expect(suggestionStatusVariant(QualitySuggestionStatus.REJECTED)).toBe(
      "neutral"
    );
    // The server closed this one; it is never a person's decision, so it does
    // not share the rejected tone.
    expect(suggestionStatusVariant(QualitySuggestionStatus.SUPERSEDED)).toBe(
      "outline"
    );
    expect(suggestionStatusVariant(99 as QualitySuggestionStatus)).toBe(
      "secondary"
    );
  });

  it("names label provenance, review state and every semantic type", () => {
    expect(labelSourceKey(QualityLabelSource.MANUAL)).toBe(
      "dataQuality.labelSource.manual"
    );
    expect(labelSourceKey(QualityLabelSource.LLM)).toBe(
      "dataQuality.labelSource.llm"
    );
    expect(labelSourceKey(QualityLabelSource.UNSPECIFIED)).toBe(
      "dataQuality.labelSource.unspecified"
    );
    expect(labelSourceKey(99 as QualityLabelSource)).toBe(
      "dataQuality.labelSource.unspecified"
    );

    expect(labelStatusKey(QualityLabelStatus.SUGGESTED)).toBe(
      "dataQuality.labelStatus.suggested"
    );
    expect(labelStatusKey(QualityLabelStatus.ACCEPTED)).toBe(
      "dataQuality.labelStatus.accepted"
    );
    expect(labelStatusKey(QualityLabelStatus.REJECTED)).toBe(
      "dataQuality.labelStatus.rejected"
    );
    expect(labelStatusKey(QualityLabelStatus.UNSPECIFIED)).toBe(
      "dataQuality.labelStatus.unspecified"
    );
    expect(labelStatusKey(99 as QualityLabelStatus)).toBe(
      "dataQuality.labelStatus.unspecified"
    );

    const types: Array<[QualitySemanticType, string]> = [
      [QualitySemanticType.EMAIL, "email"],
      [QualitySemanticType.PHONE, "phone"],
      [QualitySemanticType.URL, "url"],
      [QualitySemanticType.UUID, "uuid"],
      [QualitySemanticType.IP, "ip"],
      [QualitySemanticType.COUNTRY_CODE, "countryCode"],
      [QualitySemanticType.CURRENCY, "currency"],
      [QualitySemanticType.AMOUNT, "amount"],
      [QualitySemanticType.QUANTITY, "quantity"],
      [QualitySemanticType.PERCENTAGE, "percentage"],
      [QualitySemanticType.ID, "id"],
      [QualitySemanticType.BUSINESS_KEY, "businessKey"],
      [QualitySemanticType.TIMESTAMP, "timestamp"],
      [QualitySemanticType.DATE, "date"],
      [QualitySemanticType.ENUM, "enum"],
      [QualitySemanticType.FREE_TEXT, "freeText"],
      [QualitySemanticType.UNKNOWN, "unknown"],
      [QualitySemanticType.UNSPECIFIED, "unspecified"],
    ];
    for (const [label, suffix] of types) {
      expect(semanticTypeKey(label)).toBe(`dataQuality.semanticType.${suffix}`);
    }
    expect(semanticTypeKey(99 as QualitySemanticType)).toBe(
      "dataQuality.semanticType.unspecified"
    );
  });

  it("formats confidence without faking a zero", () => {
    expect(formatConfidence(0.82)).toBe("82.0%");
    expect(formatConfidence(0)).toBe("-");
  });

  it("splits the two payload kinds", () => {
    const rule = ruleSuggestion();
    expect(ruleDraftOf(rule)?.title).toBe("Orders should have rows");
    expect(labelSuggestionOf(rule)).toBeUndefined();

    const label = create(QualitySuggestionSchema, {
      kind: QualitySuggestionKind.LABEL,
      payload: {
        payload: {
          case: "label",
          value: create(SemanticLabelSuggestionSchema, {
            column: "email",
            label: QualitySemanticType.EMAIL,
            confidence: 0.9,
            evidence: {
              metrics: ["distinct=12"],
              rationale: "Values look like addresses.",
            },
          }),
        },
      },
    });
    expect(labelSuggestionOf(label)?.column).toBe("email");
    expect(ruleDraftOf(label)).toBeUndefined();
    expect(isLabelSuggestion(label)).toBe(true);
    expect(isLabelSuggestion(rule)).toBe(false);
  });

  it("lists a label's evidence", () => {
    expect(
      labelEvidenceLines(
        create(SemanticLabelEvidenceSchema, {
          columns: ["customer_email"],
          metrics: ["null_rate=0.02"],
          lineage: ["join key of orders"],
          rationale: "Looks like an address.",
        })
      )
    ).toEqual([
      "null_rate=0.02",
      "customer_email",
      "join key of orders",
      "Looks like an address.",
    ]);
  });

  it("collapses a proposal's rationale to one line, whichever kind it is", () => {
    expect(suggestionRationale(ruleSuggestion())).toBe(
      "The table is the fact table for orders."
    );
    const label = create(QualitySuggestionSchema, {
      kind: QualitySuggestionKind.LABEL,
      payload: {
        payload: {
          case: "label",
          value: create(SemanticLabelSuggestionSchema, {
            column: "email",
            evidence: { rationale: "  Values look\n like addresses. " },
          }),
        },
      },
    });
    expect(suggestionRationale(label)).toBe("Values look like addresses.");
    // A proposal whose payload carries no reason reads as empty, not as a dash.
    expect(suggestionRationale(create(QualitySuggestionSchema, {}))).toBe("");
  });
});

describe("generation summary", () => {
  function summary(
    fields: Partial<{
      created: number;
      refreshed: number;
      superseded: number;
      skippedExistingFact: number;
      skippedPending: number;
      skippedDecided: number;
    }>
  ) {
    return create(QualitySuggestionQueueSummarySchema, fields);
  }

  it("shows the non-zero counts a pass produced, in display order", () => {
    const view = suggestionSummaryView(
      summary({
        created: 2,
        refreshed: 1,
        superseded: 1,
        skippedExistingFact: 3,
        skippedPending: 0,
        skippedDecided: 0,
      })
    );

    expect(view.available).toBe(true);
    expect(view.queued).toBe(true);
    expect(view.skipped).toBe(3);
    expect(view.counts).toEqual([
      { key: "dataQuality.suggestionSummary.created", count: 2 },
      { key: "dataQuality.suggestionSummary.refreshed", count: 1 },
      { key: "dataQuality.suggestionSummary.superseded", count: 1 },
      { key: "dataQuality.suggestionSummary.skipped", count: 3 },
    ]);
  });

  it("does not invent counts when the pass queued nothing", () => {
    const view = suggestionSummaryView(summary({}));

    expect(view.queued).toBe(false);
    expect(view.counts).toEqual([]);
  });

  it("marks the summary unavailable when the server did not report one", () => {
    const view = suggestionSummaryView(undefined);

    expect(view.available).toBe(false);
    expect(view.queued).toBe(false);
    expect(view.counts).toEqual([]);
  });

  it("explains an empty pass by its strongest reason", () => {
    expect(
      suggestionSummaryReason(
        summary({ skippedExistingFact: 2, skippedPending: 1 }),
        "label"
      )
    ).toEqual({
      key: "dataQuality.suggestionSummary.emptyLabelExistingFact",
      count: 2,
    });
    expect(
      suggestionSummaryReason(summary({ skippedExistingFact: 2 }), "rule")
    ).toEqual({
      key: "dataQuality.suggestionSummary.emptyRuleExistingFact",
      count: 2,
    });
  });

  it("falls back to pending, decided, superseded, then generic", () => {
    expect(
      suggestionSummaryReason(
        summary({ skippedPending: 3, skippedDecided: 1 }),
        "rule"
      )
    ).toEqual({
      key: "dataQuality.suggestionSummary.emptyPending",
      count: 3,
    });
    expect(
      suggestionSummaryReason(summary({ skippedDecided: 2 }), "rule")
    ).toEqual({
      key: "dataQuality.suggestionSummary.emptyDecided",
      count: 2,
    });
    expect(suggestionSummaryReason(summary({ superseded: 1 }), "rule")).toEqual(
      {
        key: "dataQuality.suggestionSummary.emptySuperseded",
        count: 1,
      }
    );
    expect(suggestionSummaryReason(summary({}), "rule")).toEqual({
      key: "dataQuality.suggestionSummary.emptyGeneric",
      count: 0,
    });
    expect(suggestionSummaryReason(undefined, "rule")).toEqual({
      key: "dataQuality.suggestionSummary.emptyGeneric",
      count: 0,
    });
  });
});

describe("suggestion → editor prefill", () => {
  it("converts a rule draft into the editor form and its target", () => {
    const prefill = suggestionToEditorPrefill(ruleSuggestion());
    expect(prefill?.ruleType).toBe(QualityRuleType.ROW_COUNT);
    expect(prefill?.form.title).toBe("Orders should have rows");
    expect(prefill?.form.description).toBe(
      "The table is the fact table for orders."
    );
    expect(prefill?.form.config.minRows).toBe("1");
    expect(prefill?.target?.guid).toBe("inst1;db1;public;orders");
    // Fields the draft does not name come from the new-rule defaults so the
    // form opens valid instead of with empty selects.
    expect(prefill?.form.dimension).toBe(1);
    expect(prefill?.form.state).toBe(1);
  });

  it("refuses a label suggestion or an unusable payload", () => {
    expect(
      suggestionToEditorPrefill(
        create(QualitySuggestionSchema, {
          kind: QualitySuggestionKind.LABEL,
        })
      )
    ).toBeUndefined();
    expect(
      suggestionToEditorPrefill(create(QualitySuggestionSchema, {}))
    ).toBeUndefined();
  });
});

describe("draft parameters", () => {
  it("lists the non-default config fields in order", () => {
    const config = emptyConfig();
    config.minRows = " 10 ";
    config.warnMaxRows = "";
    config.countNulls = true;
    config.inclusive = false;
    config.expectedColumns = [{ name: "id", type: "bigint", nullable: false }];
    expect(ruleConfigEntries(config)).toEqual([
      { field: "minRows", value: "10" },
      { field: "countNulls", value: "✓" },
      { field: "expectedColumns", value: "1" },
    ]);
    expect(configEntryLabelKey("minRows")).toBe("dataQuality.editor.minRows");
  });
});

describe("target grouping", () => {
  it("splits a metadata GUID into its levels", () => {
    expect(suggestionTargetPath("inst1;db1;public;orders")).toEqual({
      instance: "inst1",
      database: "db1",
      schema: "public",
      object: "orders",
    });
    // The MySQL family keeps an empty schema segment.
    expect(suggestionTargetPath("inst1;db1;;orders")).toEqual({
      instance: "inst1",
      database: "db1",
      schema: "",
      object: "orders",
    });
    expect(suggestionTargetPath("inst1;db1;orders")).toEqual({
      instance: "inst1",
      database: "db1",
      schema: "",
      object: "orders",
    });
    expect(suggestionTargetPath("inst1;db1")).toEqual({
      instance: "inst1",
      database: "db1",
      schema: "",
      object: "",
    });
    expect(suggestionTargetPath("inst1")).toEqual({
      instance: "inst1",
      database: "",
      schema: "",
      object: "",
    });
  });

  it("labels a target with the instance title when it is known", () => {
    expect(suggestionTargetLabel("inst1;db1;public;orders")).toBe(
      "inst1 · db1 · public · orders"
    );
    expect(
      suggestionTargetLabel("inst1;db1;public;orders", "Orders warehouse")
    ).toBe("Orders warehouse · db1 · public · orders");
    expect(suggestionTargetLabel("")).toBe("-");
  });

  it("groups the queue by target in first-seen order", () => {
    const first = create(QualitySuggestionSchema, {
      name: "s1",
      target: create(QualityTargetSchema, {
        metaGuid: "inst1;db1;public;orders",
      }),
    });
    const second = create(QualitySuggestionSchema, {
      name: "s2",
      target: create(QualityTargetSchema, {
        metaGuid: "inst1;db1;public;users",
      }),
    });
    const third = create(QualitySuggestionSchema, {
      name: "s3",
      target: create(QualityTargetSchema, {
        metaGuid: "inst1;db1;public;orders",
      }),
    });
    const groups = groupSuggestionsByTarget([first, second, third], {
      inst1: "Warehouse",
    });
    expect(groups.map((group) => group.targetGuid)).toEqual([
      "inst1;db1;public;orders",
      "inst1;db1;public;users",
    ]);
    expect(groups[0]?.suggestions.map((s) => s.name)).toEqual(["s1", "s3"]);
    expect(groups[0]?.targetLabel).toBe("Warehouse · db1 · public · orders");
    expect(groups[1]?.suggestions).toHaveLength(1);
  });
});

describe("selection", () => {
  it("counts the selection by kind", () => {
    const draft = create(QualitySuggestionSchema, {
      kind: QualitySuggestionKind.RULE_DRAFT,
      status: QualitySuggestionStatus.PENDING,
    });
    const label = create(QualitySuggestionSchema, {
      kind: QualitySuggestionKind.LABEL,
      status: QualitySuggestionStatus.PENDING,
    });
    expect(selectionCountsByKind([draft, label, draft])).toEqual({
      total: 3,
      drafts: 2,
      labels: 1,
    });
    expect(selectionCountsByKind([])).toEqual({
      total: 0,
      drafts: 0,
      labels: 0,
    });
  });

  it("separates pending drafts from pending labels", () => {
    const draft = create(QualitySuggestionSchema, {
      kind: QualitySuggestionKind.RULE_DRAFT,
      status: QualitySuggestionStatus.PENDING,
    });
    const decided = create(QualitySuggestionSchema, {
      kind: QualitySuggestionKind.RULE_DRAFT,
      status: QualitySuggestionStatus.ACCEPTED,
    });
    const label = create(QualitySuggestionSchema, {
      kind: QualitySuggestionKind.LABEL,
      status: QualitySuggestionStatus.PENDING,
    });
    expect(isPendingSuggestion(draft)).toBe(true);
    expect(isPendingSuggestion(decided)).toBe(false);
    expect(isPendingDraft(draft)).toBe(true);
    expect(isPendingDraft(label)).toBe(false);
    expect(isPendingLabel(label)).toBe(true);
    expect(isPendingLabel(draft)).toBe(false);
  });
});

describe("suggestion → batch rule", () => {
  it("builds an enabled rule from the draft; scheduling is the target's", () => {
    const rule = buildRuleFromSuggestion({ suggestion: ruleSuggestion() });
    expect(rule?.title).toBe("Orders should have rows");
    expect(rule?.ruleType).toBe(QualityRuleType.ROW_COUNT);
    expect(rule?.state).toBe(QualityRuleState.ENABLED);
    expect(rule?.target?.metaGuid).toBe("inst1;db1;public;orders");
    expect(rule?.config?.config.case).toBe("rowCount");
    expect(rule?.owner).toBe("");
  });

  it("applies the batch overrides", () => {
    const rule = buildRuleFromSuggestion({
      suggestion: ruleSuggestion(),
      overrides: {
        severity: QualitySeverity.ERROR,
        owner: "users/owner-1",
        dimension: QualityDimension.ACCURACY,
      },
    });
    expect(rule?.severity).toBe(QualitySeverity.ERROR);
    expect(rule?.owner).toBe("users/owner-1");
    expect(rule?.dimension).toBe(QualityDimension.ACCURACY);
  });

  it("uses an inline-edited form for the row", () => {
    const prefill = suggestionToEditorPrefill(ruleSuggestion());
    expect(prefill).toBeDefined();
    if (!prefill) {
      return;
    }
    const rule = buildRuleFromSuggestion({
      suggestion: ruleSuggestion(),
      form: { ...prefill.form, title: "Renamed rule" },
    });
    expect(rule?.title).toBe("Renamed rule");
  });

  it("refuses a label or a draft without a target", () => {
    expect(
      buildRuleFromSuggestion({
        suggestion: create(QualitySuggestionSchema, {
          kind: QualitySuggestionKind.LABEL,
        }),
      })
    ).toBeUndefined();
    expect(
      buildRuleFromSuggestion({
        suggestion: create(QualitySuggestionSchema, {
          payload: ruleSuggestion().payload,
        }),
      })
    ).toBeUndefined();
  });
});

describe("batch results", () => {
  it("summarises successes and the index of each failure", () => {
    expect(
      summarizeBatchResults([
        { error: "" },
        { error: "compile failed" },
        { error: "" },
        { error: "already decided" },
      ])
    ).toEqual({
      succeeded: 2,
      failed: [
        { index: 1, message: "compile failed" },
        { index: 3, message: "already decided" },
      ],
    });
    expect(summarizeBatchResults([])).toEqual({ succeeded: 0, failed: [] });
  });
});

describe("drafting jobs", () => {
  function jobTarget(
    options: {
      guid?: string;
      state?: QualitySuggestionJobTargetState;
      suggestions?: string[];
    } = {}
  ) {
    return create(QualitySuggestionJobTargetSchema, {
      target: create(QualityTargetSchema, {
        metaGuid: options.guid ?? "inst1;db1;public;orders",
        metaType: MetaType.TABLE,
        path: {
          instance: "inst1",
          database: "db1",
          schema: "public",
          object: "orders",
        },
      }),
      state: options.state ?? QualitySuggestionJobTargetState.PENDING,
      suggestions: options.suggestions ?? [],
    });
  }

  function job(
    options: {
      status?: QualitySuggestionJobStatus;
      targets?: ReturnType<typeof jobTarget>[];
      targetCount?: number;
      completedTargetCount?: number;
      suggestionCount?: number;
      intent?: string;
    } = {}
  ) {
    return create(QualitySuggestionJobSchema, {
      name: "workspaces/ws/qualitySuggestionJobs/job-1",
      status: options.status ?? QualitySuggestionJobStatus.RUNNING,
      intent: options.intent ?? "Keep orders healthy",
      targets: options.targets ?? [jobTarget()],
      targetCount: options.targetCount ?? 2,
      completedTargetCount: options.completedTargetCount ?? 1,
      suggestionCount: options.suggestionCount ?? 3,
    });
  }

  it("names the job lifecycle and per-target state", () => {
    expect(suggestionJobStatusKey(QualitySuggestionJobStatus.PENDING)).toBe(
      "dataQuality.suggestionJobStatus.pending"
    );
    expect(suggestionJobStatusKey(QualitySuggestionJobStatus.RUNNING)).toBe(
      "dataQuality.suggestionJobStatus.running"
    );
    expect(suggestionJobStatusKey(QualitySuggestionJobStatus.COMPLETED)).toBe(
      "dataQuality.suggestionJobStatus.completed"
    );
    expect(suggestionJobStatusKey(QualitySuggestionJobStatus.FAILED)).toBe(
      "dataQuality.suggestionJobStatus.failed"
    );
    expect(suggestionJobStatusKey(QualitySuggestionJobStatus.CANCELED)).toBe(
      "dataQuality.suggestionJobStatus.canceled"
    );
    expect(suggestionJobStatusKey(QualitySuggestionJobStatus.UNSPECIFIED)).toBe(
      "dataQuality.suggestionJobStatus.unspecified"
    );
    expect(suggestionJobStatusKey(99 as QualitySuggestionJobStatus)).toBe(
      "dataQuality.suggestionJobStatus.unspecified"
    );

    expect(
      suggestionJobTargetStateKey(QualitySuggestionJobTargetState.PENDING)
    ).toBe("dataQuality.suggestionJobTargetState.pending");
    expect(
      suggestionJobTargetStateKey(QualitySuggestionJobTargetState.RUNNING)
    ).toBe("dataQuality.suggestionJobTargetState.running");
    expect(
      suggestionJobTargetStateKey(QualitySuggestionJobTargetState.DONE)
    ).toBe("dataQuality.suggestionJobTargetState.done");
    expect(
      suggestionJobTargetStateKey(QualitySuggestionJobTargetState.FAILED)
    ).toBe("dataQuality.suggestionJobTargetState.failed");
    expect(
      suggestionJobTargetStateKey(QualitySuggestionJobTargetState.SKIPPED)
    ).toBe("dataQuality.suggestionJobTargetState.skipped");
    expect(
      suggestionJobTargetStateKey(QualitySuggestionJobTargetState.UNSPECIFIED)
    ).toBe("dataQuality.suggestionJobTargetState.unspecified");
    expect(
      suggestionJobTargetStateKey(99 as QualitySuggestionJobTargetState)
    ).toBe("dataQuality.suggestionJobTargetState.unspecified");
  });

  it("tones the job and target badges", () => {
    expect(suggestionJobStatusVariant(QualitySuggestionJobStatus.PENDING)).toBe(
      "warning"
    );
    expect(suggestionJobStatusVariant(QualitySuggestionJobStatus.RUNNING)).toBe(
      "info"
    );
    expect(
      suggestionJobStatusVariant(QualitySuggestionJobStatus.COMPLETED)
    ).toBe("success");
    expect(suggestionJobStatusVariant(QualitySuggestionJobStatus.FAILED)).toBe(
      "danger"
    );
    expect(
      suggestionJobStatusVariant(QualitySuggestionJobStatus.CANCELED)
    ).toBe("neutral");
    expect(
      suggestionJobStatusVariant(QualitySuggestionJobStatus.UNSPECIFIED)
    ).toBe("secondary");
    expect(
      suggestionJobTargetStateVariant(QualitySuggestionJobTargetState.RUNNING)
    ).toBe("info");
    expect(
      suggestionJobTargetStateVariant(QualitySuggestionJobTargetState.DONE)
    ).toBe("success");
    expect(
      suggestionJobTargetStateVariant(QualitySuggestionJobTargetState.FAILED)
    ).toBe("danger");
    expect(
      suggestionJobTargetStateVariant(QualitySuggestionJobTargetState.SKIPPED)
    ).toBe("neutral");
    expect(
      suggestionJobTargetStateVariant(
        QualitySuggestionJobTargetState.UNSPECIFIED
      )
    ).toBe("secondary");
  });

  it("knows which jobs are still active", () => {
    expect(
      isActiveSuggestionJob(job({ status: QualitySuggestionJobStatus.PENDING }))
    ).toBe(true);
    expect(
      isActiveSuggestionJob(job({ status: QualitySuggestionJobStatus.RUNNING }))
    ).toBe(true);
    for (const status of [
      QualitySuggestionJobStatus.COMPLETED,
      QualitySuggestionJobStatus.FAILED,
      QualitySuggestionJobStatus.CANCELED,
      QualitySuggestionJobStatus.UNSPECIFIED,
    ]) {
      expect(isActiveSuggestionJob(job({ status }))).toBe(false);
    }
  });

  it("summarises target progress and clamps nonsense", () => {
    expect(suggestionJobProgress(job())).toEqual({
      total: 2,
      completed: 1,
      percent: 50,
      suggestions: 3,
    });
    expect(
      suggestionJobProgress(
        job({
          targetCount: 0,
          targets: [],
          completedTargetCount: 0,
          suggestionCount: 0,
        })
      )
    ).toEqual({ total: 0, completed: 0, percent: 0, suggestions: 0 });
    expect(
      suggestionJobProgress(
        job({ targetCount: 2, completedTargetCount: 9, suggestionCount: 0 })
      ).percent
    ).toBe(100);
    // A job without the computed counts falls back to its target list.
    expect(
      suggestionJobProgress(job({ targets: [jobTarget(), jobTarget()] }))
    ).toMatchObject({ total: 2 });
  });

  it("collects the suggestions the job queued", () => {
    expect(
      jobSuggestionNames(
        job({
          targets: [
            jobTarget({ suggestions: ["s1", "s2"] }),
            jobTarget({ guid: "inst1;db1;public;users" }),
            jobTarget({ suggestions: ["s3"] }),
          ],
        })
      )
    ).toEqual(["s1", "s2", "s3"]);
    expect(jobSuggestionNames(job({ targets: [] }))).toEqual([]);
  });

  it("labels a target from the job's path snapshot", () => {
    expect(jobTargetLabel(jobTarget())).toBe("inst1 · db1 · public · orders");
    expect(jobTargetLabel(jobTarget(), "Warehouse")).toBe(
      "Warehouse · db1 · public · orders"
    );
    // A target without a snapshot path falls back to the GUID.
    const guidOnly = create(QualitySuggestionJobTargetSchema, {
      target: create(QualityTargetSchema, {
        metaGuid: "inst1;db1;public;orders",
        metaType: MetaType.TABLE,
      }),
    });
    expect(jobTargetLabel(guidOnly)).toBe("inst1 · db1 · public · orders");
    expect(jobTargetLabel(create(QualitySuggestionJobTargetSchema, {}))).toBe(
      "-"
    );
  });

  it("snippets a long intent", () => {
    expect(
      jobIntentSnippet(job({ intent: "  Keep\norders   healthy  " }))
    ).toBe("Keep orders healthy");
    const long = "x".repeat(120);
    const snippet = jobIntentSnippet(job({ intent: long }));
    expect(snippet).toHaveLength(80);
    expect(snippet.endsWith("…")).toBe(true);
    expect(MAX_SUGGESTION_JOB_TARGETS).toBe(20);
  });
});

describe("LLM failure diagnostics", () => {
  const CODES: Array<[string, string]> = [
    ["TRUNCATED", "truncated"],
    ["STREAM_INCOMPLETE", "streamIncomplete"],
    ["PROVIDER_STATUS", "providerStatus"],
    ["PROVIDER_REQUEST", "providerRequest"],
    ["RESPONSE_TOO_LARGE", "responseTooLarge"],
    ["UNEXPECTED_FINISH_REASON", "unexpectedFinishReason"],
    ["TIMEOUT", "timeout"],
    ["MALFORMED_RESPONSE", "malformedResponse"],
    ["PARSE", "parse"],
  ];

  it("maps every classifier code to its label and hint key", () => {
    for (const [code, suffix] of CODES) {
      expect(diagnosticCodeKey(code)).toBe(
        `dataQuality.suggestions.diagnostic.code.${suffix}`
      );
      expect(diagnosticHintKey(code)).toBe(
        `dataQuality.suggestions.diagnostic.hint.${suffix}`
      );
    }
    // The server sends upper case; a lower-case copy still classifies.
    expect(diagnosticCodeKey("truncated")).toBe(
      "dataQuality.suggestions.diagnostic.code.truncated"
    );
    expect(diagnosticCodeKey("")).toBe(
      "dataQuality.suggestions.diagnostic.code.unknown"
    );
    expect(diagnosticHintKey("SOMETHING_NEW")).toBe(
      "dataQuality.suggestions.diagnostic.hint.unknown"
    );
  });

  it("formats reported counts and hides unreported zeros", () => {
    expect(formatDiagnosticCount(0n)).toBe("-");
    expect(formatDiagnosticCount(0)).toBe("-");
    expect(formatDiagnosticCount(8192)).toBe("8,192");
    expect(formatDiagnosticCount(1_234n)).toBe("1,234");
  });

  it("summarises the provider facts behind a failure", () => {
    const diagnostic = create(QualityLLMDiagnosticSchema, {
      errorCode: "TRUNCATED",
      provider: "openai",
      model: "gpt-test",
      promptVersion: "dq-rules-v3",
      finishReason: "length",
      maxOutputTokens: 8192,
      promptTokens: 1200n,
      completionTokens: 8192n,
      totalTokens: 9392n,
      promptChars: 5400n,
      correlationId: "corr-123",
      providerStatus: 200,
      debugPayloadAvailable: true,
    });
    expect(diagnosticSummary(diagnostic)).toEqual({
      providerModel: "openai · gpt-test",
      promptVersion: "dq-rules-v3",
      finishReason: "length",
      maxOutputTokens: "8,192",
      promptTokens: "1,200",
      completionTokens: "8,192",
      totalTokens: "9,392",
      promptChars: "5,400",
      providerStatus: "200",
      correlationId: "corr-123",
    });
  });

  it("leaves unreported summary facts empty-looking", () => {
    const summary = diagnosticSummary(create(QualityLLMDiagnosticSchema, {}));
    expect(summary.providerModel).toBe("-");
    expect(summary.promptVersion).toBe("-");
    expect(summary.finishReason).toBe("-");
    expect(summary.maxOutputTokens).toBe("-");
    expect(summary.promptTokens).toBe("-");
    expect(summary.completionTokens).toBe("-");
    expect(summary.totalTokens).toBe("-");
    expect(summary.promptChars).toBe("-");
    expect(summary.providerStatus).toBe("-");
    expect(summary.correlationId).toBe("");
  });

  it("recognises a synchronous truncation error", () => {
    const key = "dataQuality.suggestions.diagnostic.syncTruncatedHint";
    expect(
      syncDraftErrorHintKey(
        "internal: the LLM analysis failed: LLM response was truncated by the provider (finish_reason=length)"
      )
    ).toBe(key);
    expect(
      syncDraftErrorHintKey("the model stopped at the output token limit")
    ).toBe(key);
    expect(syncDraftErrorHintKey("internal: connection reset")).toBeUndefined();
    expect(syncDraftErrorHintKey("")).toBeUndefined();
  });
});

describe("column profiles and labels", () => {
  it("formats the profile facts people read", () => {
    const profile = create(ColumnProfileSchema, {
      tableGuid: "inst1;db1;public;orders",
      metaType: MetaType.TABLE,
      column: "id",
      sampleMethod: "sample",
      sampleRows: 900n,
      truncated: true,
      profile: create(ColumnProfileStatsSchema, {
        totalRows: 1000n,
        nullCount: 40n,
        distinctCount: 990n,
        minValue: "1",
        maxValue: "1000",
        minLength: 1n,
        maxLength: 4n,
        topValues: [{ value: "1", count: 10n }],
      }),
    });
    const highlights = columnProfileHighlights(profile);
    expect(highlights).toContainEqual({
      key: "dataQuality.profile.nullRate",
      params: { percent: "4.0%" },
    });
    expect(highlights).toContainEqual({
      key: "dataQuality.profile.distinctValues",
      params: { count: "990" },
    });
    expect(highlights).toContainEqual({
      key: "dataQuality.profile.minValue",
      params: { value: "1" },
    });
    expect(highlights).toContainEqual({
      key: "dataQuality.profile.maxValue",
      params: { value: "1000" },
    });
    expect(highlights).toContainEqual({
      key: "dataQuality.profile.length",
      params: { min: "1", max: "4" },
    });
    expect(highlights).toContainEqual({
      key: "dataQuality.profile.topValues",
      params: { values: "1" },
    });
    expect(highlights).toContainEqual({
      key: "dataQuality.profile.truncated",
      params: {},
    });
    expect(highlights).toContainEqual({
      key: "dataQuality.profile.sampled",
      params: { method: "sample", rows: "900" },
    });
    expect(columnProfileHighlights(undefined)).toEqual([]);
    expect(columnProfileHighlights(create(ColumnProfileSchema, {}))).toEqual(
      []
    );
  });

  it("marks a sample-based distinct count as approximate", () => {
    const profile = create(ColumnProfileSchema, {
      column: "payload",
      profile: create(ColumnProfileStatsSchema, {
        totalRows: 1000n,
        distinctCount: 40n,
        distinctEstimated: true,
      }),
    });
    expect(columnProfileHighlights(profile)).toContainEqual({
      key: "dataQuality.profile.distinctValues",
      params: { count: "≈40" },
    });
  });

  it("summarises a stored label", () => {
    const label = create(ColumnSemanticLabelSchema, {
      tableGuid: "inst1;db1;public;orders",
      metaType: MetaType.TABLE,
      column: "email",
      label: QualitySemanticType.EMAIL,
      source: QualityLabelSource.MANUAL,
      status: QualityLabelStatus.ACCEPTED,
      confidence: 0.75,
      evidence: { metrics: ["distinct=12"] },
    });
    expect(labelSummary(label)).toEqual(["75.0%", "distinct=12"]);
  });
});

describe("empty draft defaults", () => {
  it("keeps an empty form usable", () => {
    expect(emptyRuleForm().config.minRows).toBe("");
    expect(emptyRuleForm().config.celSampleRows).toBe("10000");
  });
});

describe("suggestion row readings", () => {
  function labelSuggestion(
    overrides: {
      column?: string;
      label?: QualitySemanticType;
      confidence?: number;
    } = {}
  ) {
    return create(QualitySuggestionSchema, {
      name: "workspaces/ws/qualitySuggestions/l1",
      kind: QualitySuggestionKind.LABEL,
      status: QualitySuggestionStatus.PENDING,
      target: create(QualityTargetSchema, {
        metaGuid: "inst1;db1;public;orders",
        metaType: MetaType.TABLE,
      }),
      payload: {
        payload: {
          case: "label",
          value: create(SemanticLabelSuggestionSchema, {
            column: overrides.column ?? "",
            label: overrides.label ?? QualitySemanticType.UNSPECIFIED,
            confidence: overrides.confidence ?? 0,
          }),
        },
      },
    });
  }

  it("uses a draft's title as the row text", () => {
    expect(suggestionRowText(ruleSuggestion())).toBe("Orders should have rows");
  });

  it("uses a label's column as the row text", () => {
    expect(suggestionRowText(labelSuggestion({ column: "email" }))).toBe(
      "email"
    );
  });

  it("falls back to the resource id", () => {
    expect(suggestionRowText(labelSuggestion())).toBe("l1");
  });

  it("names the semantic type a label proposes", () => {
    expect(
      suggestionLabelTypeKey(
        labelSuggestion({ label: QualitySemanticType.EMAIL })
      )
    ).toBe(semanticTypeKey(QualitySemanticType.EMAIL));
    expect(
      suggestionLabelTypeKey(
        labelSuggestion({ label: QualitySemanticType.UNSPECIFIED })
      )
    ).toBeUndefined();
    expect(suggestionLabelTypeKey(ruleSuggestion())).toBeUndefined();
  });

  it("names the rule type a draft would prefill", () => {
    expect(suggestionRuleDraftTypeKey(ruleSuggestion())).toBe(
      qualityRuleTypeKey(QualityRuleType.ROW_COUNT)
    );
    expect(suggestionRuleDraftTypeKey(labelSuggestion())).toBeUndefined();
  });

  it("reads the declared severity and confidence", () => {
    expect(suggestionSeverity(ruleSuggestion())).toBe(QualitySeverity.ERROR);
    expect(suggestionSeverity(labelSuggestion())).toBeUndefined();
    expect(suggestionConfidence(ruleSuggestion())).toBeCloseTo(0.82);
    expect(suggestionConfidence(labelSuggestion({ confidence: 0.9 }))).toBe(
      0.9
    );
    expect(suggestionConfidencePercent(ruleSuggestion())).toBe(82);
    expect(
      suggestionConfidencePercent(labelSuggestion({ confidence: 1.4 }))
    ).toBe(100);
    expect(
      suggestionConfidencePercent(labelSuggestion({ confidence: -0.2 }))
    ).toBe(0);
  });

  it("reads a group's metadata type", () => {
    const [group] = groupSuggestionsByTarget([ruleSuggestion()], {});
    expect(suggestionGroupMetaType(group)).toBe(MetaType.TABLE);
    expect(
      suggestionGroupMetaType({
        targetGuid: "",
        targetLabel: "",
        suggestions: [],
      })
    ).toBe(0);
  });

  it("selects only the pending rows of a group", () => {
    const accepted = ruleSuggestion();
    accepted.status = QualitySuggestionStatus.ACCEPTED;
    const group = {
      targetGuid: "g",
      targetLabel: "orders",
      suggestions: [ruleSuggestion(), accepted],
    };

    expect(selectableSuggestions(group)).toHaveLength(1);
    expect(selectableSuggestions(group)[0]?.status).toBe(
      QualitySuggestionStatus.PENDING
    );
  });

  it("reports the tri-state of a group's selection", () => {
    const first = ruleSuggestion();
    const second = ruleSuggestion();
    second.name = "workspaces/ws/qualitySuggestions/s2";
    const closed = ruleSuggestion();
    closed.name = "workspaces/ws/qualitySuggestions/s3";
    closed.status = QualitySuggestionStatus.ACCEPTED;
    const group = {
      targetGuid: "g",
      targetLabel: "orders",
      suggestions: [first, second, closed],
    };

    expect(groupSelectionState(group, [])).toBe(false);
    expect(groupSelectionState(group, [first.name])).toBe("indeterminate");
    expect(groupSelectionState(group, [first.name, second.name])).toBe(true);
    expect(groupSelectionState({ ...group, suggestions: [closed] }, [])).toBe(
      false
    );
  });
});
