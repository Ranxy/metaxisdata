import { Code, ConnectError } from "@connectrpc/connect";
import type {
  ExplainQualityExecutionResponse,
  QualitySuggestion,
} from "@/types/proto-es/v1/data_quality_service_pb";
import type {
  ExplainSQLMetadata,
  ExplainSQLProgress,
} from "@/types/proto-es/v1/explain_sql_service_pb";
import { qualityRuleTypeKey } from "@/utils/dataQuality";
import {
  formatConfidence,
  ruleDraftOf,
  suggestionToEditorPrefill,
} from "@/utils/dataQualitySuggestion";

/**
 * The LLM surfaces' pure layer: folding the explanation stream, deciding whether
 * an entry point is usable, classifying server errors into copy, and the short
 * form of a suggested draft shown in the draft dialog.
 */

// ---- explanation stream ----

export interface QualityExplanationState {
  /** Accumulated markdown fragments. */
  content: string;
  /** The agent steps worth showing; content frames are not steps. */
  progress: ExplainSQLProgress[];
  /** The final frame; its presence means the server finished. */
  metadata?: ExplainSQLMetadata;
  done: boolean;
}

export function emptyExplanationState(): QualityExplanationState {
  return { content: "", progress: [], metadata: undefined, done: false };
}

/**
 * Fold one stream frame into the state. Progress only keeps tool boundaries:
 * intermediate `thinking` frames would turn the panel into a log.
 */
export function applyExplanationChunk(
  state: QualityExplanationState,
  chunk: ExplainQualityExecutionResponse
): QualityExplanationState {
  switch (chunk.payload.case) {
    case "content":
      state.content += chunk.payload.value;
      break;
    case "progress": {
      const step = chunk.payload.value;
      if (step?.type === "tool_start" || step?.type === "tool_end") {
        state.progress.push(step);
      }
      break;
    }
    case "metadata":
      state.metadata = chunk.payload.value;
      state.done = true;
      break;
    default:
      break;
  }
  return state;
}

/** The server finished and the markdown can be rendered as final. */
export function explanationComplete(state: QualityExplanationState): boolean {
  return state.done;
}

/** There is something to render, final or still streaming. */
export function hasExplanationContent(state: QualityExplanationState): boolean {
  return state.content.trim() !== "";
}

// ---- availability ----

export interface LlmFeatureAvailability {
  available: boolean;
  /** Set when unavailable: the key explaining why. */
  reasonKey?: string;
}

/**
 * An LLM entry point needs both the workspace switch and the caller's
 * permission. The switch alone is not enough: a reader without `suggest` must
 * not queue drafts, and a permission without the switch would call a subsystem
 * the workspace turned off.
 */
export function llmFeatureAvailability(
  enabled: boolean,
  permitted: boolean
): LlmFeatureAvailability {
  if (!enabled) {
    return { available: false, reasonKey: "dataQuality.llm.disabledHint" };
  }
  if (!permitted) {
    return { available: false, reasonKey: "dataQuality.llm.noPermission" };
  }
  return { available: true };
}

// ---- error copy ----

/** The Connect code behind an unknown error, if it is one. */
export function connectErrorCode(error: unknown): Code | undefined {
  if (error instanceof ConnectError) {
    return error.code;
  }
  try {
    return ConnectError.from(error).code;
  } catch {
    return undefined;
  }
}

/**
 * The copy key for a failed LLM call. The raw server message stays available in
 * the panel's details; this is the sentence a person can act on.
 */
export function explainErrorMessageKey(error: unknown): string {
  const code = connectErrorCode(error);
  const message = String(
    error instanceof Error ? error.message : (error ?? "")
  ).toLowerCase();
  switch (code) {
    case Code.FailedPrecondition:
      return "dataQuality.llm.errors.notConfigured";
    case Code.PermissionDenied:
      return "dataQuality.llm.errors.noPermission";
    case Code.ResourceExhausted:
      return /quota|budget|token/.test(message)
        ? "dataQuality.llm.errors.budget"
        : "dataQuality.llm.errors.rateLimited";
    case Code.DeadlineExceeded:
      return "dataQuality.llm.errors.timeout";
    case Code.Unavailable:
      return "dataQuality.llm.errors.unavailable";
    case Code.InvalidArgument:
      return "dataQuality.llm.errors.invalidOutput";
    case Code.Internal:
    case Code.Unknown:
      return /invalid|malformed|parse|schema|output/.test(message)
        ? "dataQuality.llm.errors.invalidOutput"
        : "dataQuality.llm.errors.generic";
    default:
      return "dataQuality.llm.errors.generic";
  }
}

// ---- suggested draft display ----

export interface DraftSuggestionSummary {
  title: string;
  typeKey: string;
  confidence: string;
  rationale: string;
}

/** The short form of a suggested draft for the AI draft dialog's result list. */
export function draftSuggestionSummary(
  suggestion: QualitySuggestion
): DraftSuggestionSummary | undefined {
  const draft = ruleDraftOf(suggestion);
  if (!draft) {
    return undefined;
  }
  const prefill = suggestionToEditorPrefill(suggestion);
  return {
    title: draft.title || suggestion.name.split("/").pop() || "",
    typeKey: prefill ? qualityRuleTypeKey(prefill.ruleType) : "",
    confidence: formatConfidence(draft.confidence),
    rationale: draft.rationale,
  };
}
