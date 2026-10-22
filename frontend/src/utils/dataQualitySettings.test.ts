import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { DataQualitySettingSchema } from "@/types/proto-es/v1/setting_service_pb";
import {
  DAILY_LLM_ANALYSIS_LIMIT_MAX,
  type DataQualitySettingsForm,
  FAILURE_SAMPLE_LIMIT_MAX,
  formToSetting,
  LLM_CONCURRENCY_LIMIT_MAX,
  llmDisabled,
  MAX_COLUMNS_PER_ANALYSIS_MAX,
  SETTINGS_VALIDATION_PREFIX,
  settingsToForm,
  validateSettingsForm,
} from "./dataQualitySettings";

function baseForm(
  overrides: Partial<DataQualitySettingsForm> = {}
): DataQualitySettingsForm {
  return {
    enableLlmAnalysis: false,
    llmProviderProfile: "",
    storeFailureSamples: false,
    failureSampleLimit: "",
    defaultSchedule: { value: "1", unit: "hours" },
    dailyExecutionLimit: "",
    dailyColumnAnalysisLimit: "",
    dailyLlmAnalysisLimit: "",
    llmConcurrencyLimit: "",
    maxColumnsPerAnalysis: "",
    sendSampleValuesToLlm: false,
    ...overrides,
  };
}

describe("settingsToForm", () => {
  it("uses the subsystem defaults for a workspace with no setting", () => {
    expect(settingsToForm(undefined)).toEqual(baseForm());
  });

  it("maps the stored setting, leaving zeroes blank as 'server default'", () => {
    const form = settingsToForm(
      create(DataQualitySettingSchema, {
        enableLlmAnalysis: true,
        llmProviderProfile: "llmProfiles/p1",
        storeFailureSamples: true,
        failureSampleLimit: 20,
        defaultScheduleInterval: create(DurationSchema, {
          seconds: 900n,
          nanos: 0,
        }),
        dailyExecutionLimit: 10000,
        dailyColumnAnalysisLimit: 0,
        dailyLlmAnalysisLimit: 5000,
        llmConcurrencyLimit: 12,
        maxColumnsPerAnalysis: 50,
        sendSampleValuesToLlm: true,
      })
    );
    expect(form).toEqual({
      enableLlmAnalysis: true,
      llmProviderProfile: "llmProfiles/p1",
      storeFailureSamples: true,
      failureSampleLimit: "20",
      defaultSchedule: { value: "15", unit: "minutes" },
      dailyExecutionLimit: "10000",
      dailyColumnAnalysisLimit: "",
      dailyLlmAnalysisLimit: "5000",
      llmConcurrencyLimit: "12",
      maxColumnsPerAnalysis: "50",
      sendSampleValuesToLlm: true,
    });
  });
});

describe("formToSetting", () => {
  it("round-trips a stored setting", () => {
    const setting = create(DataQualitySettingSchema, {
      enableLlmAnalysis: true,
      llmProviderProfile: "llmProfiles/p1",
      storeFailureSamples: true,
      failureSampleLimit: 20,
      defaultScheduleInterval: create(DurationSchema, {
        seconds: 172_800n,
        nanos: 0,
      }),
      dailyExecutionLimit: 10000,
      dailyColumnAnalysisLimit: 500,
      dailyLlmAnalysisLimit: 5000,
      llmConcurrencyLimit: 12,
      maxColumnsPerAnalysis: 50,
      sendSampleValuesToLlm: true,
    });
    expect(formToSetting(settingsToForm(setting))).toEqual(setting);
  });

  it("treats blank numbers as the server default and trims the profile", () => {
    const setting = formToSetting(
      baseForm({
        llmProviderProfile: "  llmProfiles/p2  ",
        failureSampleLimit: "",
        dailyExecutionLimit: "",
        dailyColumnAnalysisLimit: "",
        dailyLlmAnalysisLimit: "",
        llmConcurrencyLimit: "",
        maxColumnsPerAnalysis: "",
      })
    );
    expect(setting.llmProviderProfile).toBe("llmProfiles/p2");
    expect(setting.failureSampleLimit).toBe(0);
    expect(setting.dailyExecutionLimit).toBe(0);
    expect(setting.dailyColumnAnalysisLimit).toBe(0);
    expect(setting.dailyLlmAnalysisLimit).toBe(0);
    expect(setting.llmConcurrencyLimit).toBe(0);
    expect(setting.maxColumnsPerAnalysis).toBe(0);
    expect(setting.defaultScheduleInterval?.seconds).toBe(3_600n);
  });

  it("falls back to one hour when the schedule is unusable", () => {
    const setting = formToSetting(
      baseForm({ defaultSchedule: { value: "", unit: "hours" } })
    );
    expect(setting.defaultScheduleInterval?.seconds).toBe(3_600n);
  });

  it("reports whether the LLM-dependent fields are irrelevant", () => {
    expect(llmDisabled(baseForm())).toBe(true);
    expect(llmDisabled(baseForm({ enableLlmAnalysis: true }))).toBe(false);
  });
});

describe("validateSettingsForm", () => {
  it("accepts every bound's edges and blanks", () => {
    expect(validateSettingsForm(baseForm())).toEqual([]);
    expect(
      validateSettingsForm(
        baseForm({
          failureSampleLimit: String(FAILURE_SAMPLE_LIMIT_MAX),
          maxColumnsPerAnalysis: String(MAX_COLUMNS_PER_ANALYSIS_MAX),
          dailyExecutionLimit: "0",
          dailyColumnAnalysisLimit: "100",
          dailyLlmAnalysisLimit: String(DAILY_LLM_ANALYSIS_LIMIT_MAX),
          llmConcurrencyLimit: String(LLM_CONCURRENCY_LIMIT_MAX),
          defaultSchedule: { value: "7", unit: "days" },
        })
      )
    ).toEqual([]);
  });

  it("rejects out-of-range numbers", () => {
    const cases: Array<[Partial<DataQualitySettingsForm>, string]> = [
      [{ failureSampleLimit: "-1" }, "failureSampleLimitInvalid"],
      [
        { failureSampleLimit: String(FAILURE_SAMPLE_LIMIT_MAX + 1) },
        "failureSampleLimitInvalid",
      ],
      [{ failureSampleLimit: "1.5" }, "failureSampleLimitInvalid"],
      [{ failureSampleLimit: "many" }, "failureSampleLimitInvalid"],
      [{ dailyExecutionLimit: "-2" }, "dailyExecutionLimitInvalid"],
      [{ dailyColumnAnalysisLimit: "x" }, "dailyColumnAnalysisLimitInvalid"],
      [
        { dailyLlmAnalysisLimit: String(DAILY_LLM_ANALYSIS_LIMIT_MAX + 1) },
        "dailyLlmAnalysisLimitInvalid",
      ],
      [{ dailyLlmAnalysisLimit: "1.5" }, "dailyLlmAnalysisLimitInvalid"],
      [
        { llmConcurrencyLimit: String(LLM_CONCURRENCY_LIMIT_MAX + 1) },
        "llmConcurrencyLimitInvalid",
      ],
      [{ llmConcurrencyLimit: "-1" }, "llmConcurrencyLimitInvalid"],
      [
        { maxColumnsPerAnalysis: String(MAX_COLUMNS_PER_ANALYSIS_MAX + 1) },
        "maxColumnsPerAnalysisInvalid",
      ],
    ];
    for (const [overrides, key] of cases) {
      expect(
        validateSettingsForm(baseForm(overrides)).map((issue) => issue.key),
        key
      ).toContain(`${SETTINGS_VALIDATION_PREFIX}${key}`);
    }
  });

  it("keeps the schedule inside 15 minutes - 7 days", () => {
    for (const schedule of [
      { value: "14", unit: "minutes" as const },
      { value: "8", unit: "days" as const },
      { value: "", unit: "hours" as const },
    ]) {
      expect(
        validateSettingsForm(baseForm({ defaultSchedule: schedule })).map(
          (issue) => issue.key
        )
      ).toContain(`${SETTINGS_VALIDATION_PREFIX}scheduleInvalid`);
    }
  });

  it("checks both new LLM limits", () => {
    const daily = validateSettingsForm(
      baseForm({
        dailyLlmAnalysisLimit: String(DAILY_LLM_ANALYSIS_LIMIT_MAX + 1),
      })
    ).find(
      (candidate) =>
        candidate.key ===
        `${SETTINGS_VALIDATION_PREFIX}dailyLlmAnalysisLimitInvalid`
    );
    expect(daily?.params).toEqual({ max: DAILY_LLM_ANALYSIS_LIMIT_MAX });
    const concurrency = validateSettingsForm(
      baseForm({ llmConcurrencyLimit: "0" })
    );
    expect(concurrency).toEqual([]);
  });

  it("carries the limit in the message parameters", () => {
    const issue = validateSettingsForm(
      baseForm({ failureSampleLimit: "1000" })
    ).find(
      (candidate) =>
        candidate.key ===
        `${SETTINGS_VALIDATION_PREFIX}failureSampleLimitInvalid`
    );
    expect(issue?.params).toEqual({ max: FAILURE_SAMPLE_LIMIT_MAX });
  });
});
