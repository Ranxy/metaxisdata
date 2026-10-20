import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  ColumnProfileSchema,
  ColumnProfileStatsSchema,
  ColumnSemanticLabelSchema,
  QualityLabelSource,
  QualityLabelStatus,
  QualityRuleConfigSchema,
  QualityRuleType,
  QualitySemanticType,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  QualityTargetSchema,
  RowCountConfigSchema,
  SemanticLabelEvidenceSchema,
  SemanticLabelSuggestionSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { emptyConfig, emptyRuleForm } from "./dataQualityForm";
import {
  columnProfileHighlights,
  configEntryLabelKey,
  formatConfidence,
  labelEvidenceLines,
  labelSourceKey,
  labelStatusKey,
  labelSuggestionOf,
  labelSummary,
  ruleConfigEntries,
  ruleDraftOf,
  semanticTypeKey,
  suggestionKindKey,
  suggestionSourceKey,
  suggestionStatusKey,
  suggestionStatusVariant,
  suggestionToEditorPrefill,
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
    expect(prefill?.form.schedule).toEqual({ value: "1", unit: "hours" });
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
