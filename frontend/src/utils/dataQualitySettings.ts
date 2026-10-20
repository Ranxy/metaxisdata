import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import {
  type DataQualitySetting,
  DataQualitySettingSchema,
} from "@/types/proto-es/v1/setting_service_pb";
import {
  durationToSchedule,
  MAX_SCHEDULE_MINUTES,
  MIN_SCHEDULE_MINUTES,
  type ScheduleUnit,
  scheduleMinutes,
  scheduleToDuration,
  type ValidationIssue,
} from "./dataQualityForm";

/**
 * The settings form's pure layer: the editable shape, its conversion to and
 * from `DataQualitySetting`, and the client-side checks.
 *
 * Numbers are strings while being edited (an empty field is not the number 0),
 * and a stored 0 means "use the server default", so the form renders it blank
 * with a hint rather than pretending the default is the configured value.
 */

export const FAILURE_SAMPLE_LIMIT_MAX = 100;
export const MAX_COLUMNS_PER_ANALYSIS_MAX = 200;

export interface DataQualitySettingsForm {
  enableLlmAnalysis: boolean;
  /** Format: a profile resource name; empty means the first allowed profile. */
  llmProviderProfile: string;
  storeFailureSamples: boolean;
  /** Blank means the server default (20). */
  failureSampleLimit: string;
  defaultSchedule: { value: string; unit: ScheduleUnit };
  /** Blank/0 means the server default. */
  dailyExecutionLimit: string;
  /** Blank/0 means the server default. */
  dailyColumnAnalysisLimit: string;
  /** Blank/0 means the server default (50); the form caps at 200. */
  maxColumnsPerAnalysis: string;
  sendSampleValuesToLlm: boolean;
}

/** The form for a workspace's effective setting. */
export function settingsToForm(
  setting: DataQualitySetting | undefined
): DataQualitySettingsForm {
  return {
    enableLlmAnalysis: setting?.enableLlmAnalysis ?? false,
    llmProviderProfile: setting?.llmProviderProfile ?? "",
    storeFailureSamples: setting?.storeFailureSamples ?? false,
    failureSampleLimit: numberToInput(setting?.failureSampleLimit),
    defaultSchedule: setting?.defaultScheduleInterval
      ? durationToSchedule(setting.defaultScheduleInterval)
      : { value: "1", unit: "hours" },
    dailyExecutionLimit: numberToInput(setting?.dailyExecutionLimit),
    dailyColumnAnalysisLimit: numberToInput(setting?.dailyColumnAnalysisLimit),
    maxColumnsPerAnalysis: numberToInput(setting?.maxColumnsPerAnalysis),
    sendSampleValuesToLlm: setting?.sendSampleValuesToLlm ?? false,
  };
}

/** The protobuf setting the save request carries. */
export function formToSetting(
  form: DataQualitySettingsForm
): DataQualitySetting {
  return create(DataQualitySettingSchema, {
    enableLlmAnalysis: form.enableLlmAnalysis,
    llmProviderProfile: form.llmProviderProfile.trim(),
    storeFailureSamples: form.storeFailureSamples,
    failureSampleLimit: inputToInt(form.failureSampleLimit) ?? 0,
    // A blank schedule cannot happen after validation; fall back to one hour so
    // a hand-built form still produces a usable message.
    defaultScheduleInterval:
      scheduleMinutes(form.defaultSchedule) === undefined
        ? create(DurationSchema, { seconds: 3_600n, nanos: 0 })
        : scheduleToDuration(form.defaultSchedule),
    dailyExecutionLimit: inputToInt(form.dailyExecutionLimit) ?? 0,
    dailyColumnAnalysisLimit: inputToInt(form.dailyColumnAnalysisLimit) ?? 0,
    maxColumnsPerAnalysis: inputToInt(form.maxColumnsPerAnalysis) ?? 0,
    sendSampleValuesToLlm: form.sendSampleValuesToLlm,
  });
}

/** True when the LLM-dependent fields are irrelevant because LLM is off. */
export function llmDisabled(form: DataQualitySettingsForm): boolean {
  return !form.enableLlmAnalysis;
}

/** Every key the settings validation can return lives under this prefix. */
export const SETTINGS_VALIDATION_PREFIX = "dataQualitySettings.validation.";

/**
 * Client-side checks. The server clamps the same bounds again; this is what
 * makes a bad number visible before a round trip.
 */
export function validateSettingsForm(
  form: DataQualitySettingsForm
): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  const issue = (key: string, params?: Record<string, string | number>) =>
    issues.push({ key: SETTINGS_VALIDATION_PREFIX + key, params });

  const sampleLimit = checkRange(
    form.failureSampleLimit,
    0,
    FAILURE_SAMPLE_LIMIT_MAX
  );
  if (sampleLimit === "invalid") {
    issue("failureSampleLimitInvalid", { max: FAILURE_SAMPLE_LIMIT_MAX });
  }
  const schedule = scheduleMinutes(form.defaultSchedule);
  if (
    schedule === undefined ||
    schedule < MIN_SCHEDULE_MINUTES ||
    schedule > MAX_SCHEDULE_MINUTES
  ) {
    issue("scheduleInvalid");
  }
  const execution = checkNonNegative(form.dailyExecutionLimit);
  if (execution === "invalid") {
    issue("dailyExecutionLimitInvalid");
  }
  const columnAnalysis = checkNonNegative(form.dailyColumnAnalysisLimit);
  if (columnAnalysis === "invalid") {
    issue("dailyColumnAnalysisLimitInvalid");
  }
  const maxColumns = checkRange(
    form.maxColumnsPerAnalysis,
    0,
    MAX_COLUMNS_PER_ANALYSIS_MAX
  );
  if (maxColumns === "invalid") {
    issue("maxColumnsPerAnalysisInvalid", {
      max: MAX_COLUMNS_PER_ANALYSIS_MAX,
    });
  }
  return issues;
}

/** A number field: blank is allowed (server default); anything else is checked. */
type RangeCheck = "empty" | "valid" | "invalid";

function checkRange(value: string, min: number, max: number): RangeCheck {
  const text = value.trim();
  if (text === "") {
    return "empty";
  }
  const parsed = Number(text);
  if (!Number.isInteger(parsed) || parsed < min || parsed > max) {
    return "invalid";
  }
  return "valid";
}

function checkNonNegative(value: string): RangeCheck {
  const text = value.trim();
  if (text === "") {
    return "empty";
  }
  const parsed = Number(text);
  return Number.isInteger(parsed) && parsed >= 0 ? "valid" : "invalid";
}

/** A stored number as an input value; 0 means "server default" and is blank. */
function numberToInput(value: number | undefined): string {
  return value !== undefined && value > 0 ? String(value) : "";
}

/** An input value as a non-negative integer, or undefined when blank. */
function inputToInt(value: string): number | undefined {
  const text = value.trim();
  if (text === "") {
    return undefined;
  }
  const parsed = Number(text);
  return Number.isInteger(parsed) && parsed >= 0 ? parsed : undefined;
}
