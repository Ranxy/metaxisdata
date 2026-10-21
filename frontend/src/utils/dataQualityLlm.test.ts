import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import {
  ExplainQualityExecutionResponseSchema,
  QualityRuleConfigSchema,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualityTargetSchema,
  RowCountConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { ExplainSQLProgressSchema } from "@/types/proto-es/v1/explain_sql_service_pb";
import {
  applyExplanationChunk,
  connectErrorCode,
  draftSuggestionSummary,
  emptyExplanationState,
  explainErrorMessageKey,
  explanationComplete,
  hasExplanationContent,
  llmFeatureAvailability,
} from "./dataQualityLlm";

function contentChunk(value: string) {
  return create(ExplainQualityExecutionResponseSchema, {
    payload: { case: "content", value },
  });
}

function progressChunk(type: string, toolName = "profile") {
  return create(ExplainQualityExecutionResponseSchema, {
    payload: {
      case: "progress",
      value: create(ExplainSQLProgressSchema, { type, toolName }),
    },
  });
}

function metadataChunk() {
  return create(ExplainQualityExecutionResponseSchema, {
    payload: {
      case: "metadata",
      value: {
        provider: "openai",
        model: "gpt-test",
        fromCache: true,
      },
    },
  });
}

describe("explanation stream", () => {
  it("starts empty and incomplete", () => {
    const state = emptyExplanationState();
    expect(state).toEqual({
      content: "",
      progress: [],
      metadata: undefined,
      done: false,
    });
    expect(explanationComplete(state)).toBe(false);
    expect(hasExplanationContent(state)).toBe(false);
  });

  it("folds content, tool boundaries and the final metadata frame", () => {
    const state = emptyExplanationState();
    applyExplanationChunk(state, contentChunk("The null rate "));
    applyExplanationChunk(state, progressChunk("thinking"));
    applyExplanationChunk(state, progressChunk("tool_start"));
    applyExplanationChunk(state, progressChunk("tool_end"));
    applyExplanationChunk(state, contentChunk("rose to 4%."));
    applyExplanationChunk(state, metadataChunk());

    expect(state.content).toBe("The null rate rose to 4%.");
    // Only tool boundaries are steps; a thinking frame would be log noise.
    expect(state.progress.map((step) => step.type)).toEqual([
      "tool_start",
      "tool_end",
    ]);
    expect(state.metadata).toMatchObject({
      provider: "openai",
      model: "gpt-test",
      fromCache: true,
    });
    expect(explanationComplete(state)).toBe(true);
    expect(hasExplanationContent(state)).toBe(true);
  });

  it("ignores unknown frames and whitespace-only content", () => {
    const state = emptyExplanationState();
    applyExplanationChunk(
      state,
      create(ExplainQualityExecutionResponseSchema, {})
    );
    applyExplanationChunk(state, contentChunk("   "));
    expect(state.content).toBe("   ");
    expect(hasExplanationContent(state)).toBe(false);
    expect(state.done).toBe(false);
  });
});

describe("LLM availability", () => {
  it("needs both the workspace switch and the permission", () => {
    expect(llmFeatureAvailability(true, true)).toEqual({ available: true });
    expect(llmFeatureAvailability(false, true)).toEqual({
      available: false,
      reasonKey: "dataQuality.llm.disabledHint",
    });
    expect(llmFeatureAvailability(true, false)).toEqual({
      available: false,
      reasonKey: "dataQuality.llm.noPermission",
    });
    expect(llmFeatureAvailability(false, false)).toEqual({
      available: false,
      reasonKey: "dataQuality.llm.disabledHint",
    });
  });
});

describe("error copy", () => {
  it("reads the Connect code of an error", () => {
    expect(
      connectErrorCode(new ConnectError("no provider", Code.FailedPrecondition))
    ).toBe(Code.FailedPrecondition);
    expect(connectErrorCode(new Error("boom"))).toBe(Code.Unknown);
    expect(connectErrorCode("boom")).toBe(Code.Unknown);
  });

  it("classifies the failures a person can act on", () => {
    expect(
      explainErrorMessageKey(
        new ConnectError("LLM analysis is disabled", Code.FailedPrecondition)
      )
    ).toBe("dataQuality.llm.errors.notConfigured");
    expect(
      explainErrorMessageKey(
        new ConnectError("quota exhausted", Code.PermissionDenied)
      )
    ).toBe("dataQuality.llm.errors.noPermission");
    expect(
      explainErrorMessageKey(
        new ConnectError("daily token budget exhausted", Code.ResourceExhausted)
      )
    ).toBe("dataQuality.llm.errors.budget");
    expect(
      explainErrorMessageKey(
        new ConnectError("slow down", Code.ResourceExhausted)
      )
    ).toBe("dataQuality.llm.errors.rateLimited");
    expect(
      explainErrorMessageKey(
        new ConnectError("deadline", Code.DeadlineExceeded)
      )
    ).toBe("dataQuality.llm.errors.timeout");
    expect(
      explainErrorMessageKey(new ConnectError("down", Code.Unavailable))
    ).toBe("dataQuality.llm.errors.unavailable");
    expect(
      explainErrorMessageKey(
        new ConnectError("bad argument", Code.InvalidArgument)
      )
    ).toBe("dataQuality.llm.errors.invalidOutput");
    expect(
      explainErrorMessageKey(
        new ConnectError("model returned malformed JSON", Code.Internal)
      )
    ).toBe("dataQuality.llm.errors.invalidOutput");
    expect(
      explainErrorMessageKey(new ConnectError("oops", Code.Internal))
    ).toBe("dataQuality.llm.errors.generic");
    expect(explainErrorMessageKey(new Error("boom"))).toBe(
      "dataQuality.llm.errors.generic"
    );
    expect(explainErrorMessageKey(undefined)).toBe(
      "dataQuality.llm.errors.generic"
    );
  });
});

describe("draft suggestion summary", () => {
  it("summarises a rule draft and ignores label suggestions", () => {
    const suggestion = create(QualitySuggestionSchema, {
      name: "workspaces/ws/qualitySuggestions/s1",
      kind: QualitySuggestionKind.RULE_DRAFT,
      target: create(QualityTargetSchema, {
        metaGuid: "inst1;db1;public;orders",
        metaType: MetaType.TABLE,
      }),
      payload: {
        payload: {
          case: "rule",
          value: {
            title: "Orders should have rows",
            rationale: "Fact table.",
            severity: 2,
            confidence: 0.8,
            config: create(QualityRuleConfigSchema, {
              config: {
                case: "rowCount",
                value: create(RowCountConfigSchema, { minRows: 1n }),
              },
            }),
          },
        },
      },
    });
    expect(draftSuggestionSummary(suggestion)).toEqual({
      title: "Orders should have rows",
      typeKey: "dataQuality.ruleType.rowCount",
      confidence: "80.0%",
      rationale: "Fact table.",
    });
    expect(
      draftSuggestionSummary(
        create(QualitySuggestionSchema, {
          kind: QualitySuggestionKind.LABEL,
        })
      )
    ).toBeUndefined();
  });
});
