import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import {
  type DataQualitySetting,
  DataQualitySettingSchema,
  DayOfWeek,
  QualityDigestSettingSchema,
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
export const DAILY_LLM_ANALYSIS_LIMIT_MAX = 1_000_000;
export const MAX_DAILY_COLUMN_ANALYSIS_LIMIT = 1_000_000;
export const LLM_CONCURRENCY_LIMIT_MAX = 64;

/** The digest's clock value: a 24-hour "HH:MM". */
export const DIGEST_TIME_PATTERN = /^([01]\d|2[0-3]):[0-5]\d$/;

/** The digest schedule's editable shape. */
export interface WeeklyDigestForm {
  enabled: boolean;
  /** An ISO 8601 weekday; the server rejects UNSPECIFIED while enabled. */
  dayOfWeek: DayOfWeek;
  /** "HH:MM" in `timezone`; a disabled digest may leave it blank. */
  time: string;
  /** An IANA time-zone name; a disabled digest may leave it blank. */
  timezone: string;
}

/**
 * The schedule of a workspace that never configured one. The server resolves a
 * missing digest to the same Monday 09:00 UTC, so the form starts there instead
 * of at an empty field the save would then reject.
 */
const DEFAULT_DIGEST: WeeklyDigestForm = {
  enabled: false,
  dayOfWeek: DayOfWeek.MONDAY,
  time: "09:00",
  timezone: "UTC",
};

/** The ISO 8601 weekdays a digest may be sent on, Monday first. */
export const DIGEST_WEEKDAYS: readonly DayOfWeek[] = [
  DayOfWeek.MONDAY,
  DayOfWeek.TUESDAY,
  DayOfWeek.WEDNESDAY,
  DayOfWeek.THURSDAY,
  DayOfWeek.FRIDAY,
  DayOfWeek.SATURDAY,
  DayOfWeek.SUNDAY,
];

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
  /** Blank/0 means the server default (10,000). */
  dailyLlmAnalysisLimit: string;
  /** Blank/0 means the server default (8). */
  llmConcurrencyLimit: string;
  /** Blank/0 means the server default (50); the form caps at 200. */
  maxColumnsPerAnalysis: string;
  sendSampleValuesToLlm: boolean;
  weeklyDigest: WeeklyDigestForm;
}

/** The form for a workspace's effective setting. */
export function settingsToForm(
  setting: DataQualitySetting | undefined
): DataQualitySettingsForm {
  const digest = setting?.weeklyDigest;
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
    dailyLlmAnalysisLimit: numberToInput(setting?.dailyLlmAnalysisLimit),
    llmConcurrencyLimit: numberToInput(setting?.llmConcurrencyLimit),
    maxColumnsPerAnalysis: numberToInput(setting?.maxColumnsPerAnalysis),
    sendSampleValuesToLlm: setting?.sendSampleValuesToLlm ?? false,
    weeklyDigest: {
      enabled: digest?.enabled ?? DEFAULT_DIGEST.enabled,
      // An unset weekday is not a weekday; show the default the server resolves.
      dayOfWeek: digest?.dayOfWeek || DEFAULT_DIGEST.dayOfWeek,
      time: digest?.time || DEFAULT_DIGEST.time,
      timezone: digest?.timezone || DEFAULT_DIGEST.timezone,
    },
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
    dailyLlmAnalysisLimit: inputToInt(form.dailyLlmAnalysisLimit) ?? 0,
    llmConcurrencyLimit: inputToInt(form.llmConcurrencyLimit) ?? 0,
    maxColumnsPerAnalysis: inputToInt(form.maxColumnsPerAnalysis) ?? 0,
    sendSampleValuesToLlm: form.sendSampleValuesToLlm,
    // The whole `data_quality` field is written on save, so the digest has to be
    // part of the message even when this page did not touch it; otherwise the
    // write would clear a schedule configured elsewhere.
    weeklyDigest: create(QualityDigestSettingSchema, {
      enabled: form.weeklyDigest.enabled,
      dayOfWeek: form.weeklyDigest.dayOfWeek,
      time: form.weeklyDigest.time.trim(),
      timezone: form.weeklyDigest.timezone.trim(),
    }),
  });
}

/** True when the LLM-dependent fields are irrelevant because LLM is off. */
export function llmDisabled(form: DataQualitySettingsForm): boolean {
  return !form.enableLlmAnalysis;
}

/** Whether `zone` names an IANA time zone this runtime knows. */
export function isValidTimeZone(zone: string): boolean {
  const value = zone.trim();
  if (value === "") {
    return false;
  }
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: value });
    return true;
  } catch {
    return false;
  }
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
  const columnAnalysis = checkRange(
    form.dailyColumnAnalysisLimit,
    0,
    MAX_DAILY_COLUMN_ANALYSIS_LIMIT
  );
  if (columnAnalysis === "invalid") {
    issue("dailyColumnAnalysisLimitInvalid", {
      max: MAX_DAILY_COLUMN_ANALYSIS_LIMIT,
    });
  }
  const dailyAnalyses = checkRange(
    form.dailyLlmAnalysisLimit,
    0,
    DAILY_LLM_ANALYSIS_LIMIT_MAX
  );
  if (dailyAnalyses === "invalid") {
    issue("dailyLlmAnalysisLimitInvalid", {
      max: DAILY_LLM_ANALYSIS_LIMIT_MAX,
    });
  }
  const concurrency = checkRange(
    form.llmConcurrencyLimit,
    0,
    LLM_CONCURRENCY_LIMIT_MAX
  );
  if (concurrency === "invalid") {
    issue("llmConcurrencyLimitInvalid", { max: LLM_CONCURRENCY_LIMIT_MAX });
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
  validateDigest(form.weeklyDigest, issue);
  return issues;
}

/**
 * The digest checks mirror the server's: a disabled schedule may leave fields
 * blank, an enabled one must name a weekday, a clock time and an IANA zone.
 */
function validateDigest(
  digest: WeeklyDigestForm,
  issue: (key: string, params?: Record<string, string | number>) => void
) {
  if (digest.enabled && !DIGEST_WEEKDAYS.includes(digest.dayOfWeek)) {
    issue("digestDayInvalid");
  }
  const time = digest.time.trim();
  if (time === "") {
    if (digest.enabled) {
      issue("digestTimeRequired");
    }
  } else if (!DIGEST_TIME_PATTERN.test(time)) {
    issue("digestTimeInvalid");
  }
  const timezone = digest.timezone.trim();
  if (timezone === "") {
    if (digest.enabled) {
      issue("digestTimezoneRequired");
    }
  } else if (!isValidTimeZone(timezone)) {
    issue("digestTimezoneInvalid");
  }
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
