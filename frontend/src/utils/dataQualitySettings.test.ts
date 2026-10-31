import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  DataQualitySettingSchema,
  DayOfWeek,
  QualityDigestSettingSchema,
} from "@/types/proto-es/v1/setting_service_pb";
import {
  DAILY_LLM_ANALYSIS_LIMIT_MAX,
  type DataQualitySettingsForm,
  DIGEST_TIME_PATTERN,
  FAILURE_SAMPLE_LIMIT_MAX,
  formToSetting,
  isValidTimeZone,
  LLM_CONCURRENCY_LIMIT_MAX,
  llmDisabled,
  MAX_COLUMNS_PER_ANALYSIS_MAX,
  MAX_DAILY_COLUMN_ANALYSIS_LIMIT,
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
    weeklyDigest: {
      enabled: false,
      dayOfWeek: DayOfWeek.MONDAY,
      time: "09:00",
      timezone: "UTC",
    },
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
      // No digest stored yet: show the schedule the server resolves instead.
      weeklyDigest: {
        enabled: false,
        dayOfWeek: DayOfWeek.MONDAY,
        time: "09:00",
        timezone: "UTC",
      },
    });
  });

  it("maps a configured weekly digest", () => {
    const form = settingsToForm(
      create(DataQualitySettingSchema, {
        weeklyDigest: create(QualityDigestSettingSchema, {
          enabled: true,
          dayOfWeek: DayOfWeek.FRIDAY,
          time: "18:30",
          timezone: "Asia/Shanghai",
        }),
      })
    );

    expect(form.weeklyDigest).toEqual({
      enabled: true,
      dayOfWeek: DayOfWeek.FRIDAY,
      time: "18:30",
      timezone: "Asia/Shanghai",
    });
  });

  it("treats an unset weekday as the default one", () => {
    const form = settingsToForm(
      create(DataQualitySettingSchema, {
        weeklyDigest: create(QualityDigestSettingSchema, {
          enabled: false,
          dayOfWeek: DayOfWeek.UNSPECIFIED,
        }),
      })
    );

    expect(form.weeklyDigest.dayOfWeek).toBe(DayOfWeek.MONDAY);
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
      weeklyDigest: create(QualityDigestSettingSchema, {
        enabled: true,
        dayOfWeek: DayOfWeek.FRIDAY,
        time: "18:30",
        timezone: "Asia/Shanghai",
      }),
    });
    expect(formToSetting(settingsToForm(setting))).toEqual(setting);
  });

  it("always writes the digest, even when this page did not touch it", () => {
    // The whole data_quality field is replaced on save, so a missing digest
    // would clear a schedule configured elsewhere.
    const setting = formToSetting(baseForm());

    expect(setting.weeklyDigest).toEqual(
      create(QualityDigestSettingSchema, {
        enabled: false,
        dayOfWeek: DayOfWeek.MONDAY,
        time: "09:00",
        timezone: "UTC",
      })
    );
  });

  it("trims the digest time and zone", () => {
    const setting = formToSetting(
      baseForm({
        weeklyDigest: {
          enabled: true,
          dayOfWeek: DayOfWeek.SUNDAY,
          time: " 07:05 ",
          timezone: "  Europe/Berlin ",
        },
      })
    );

    expect(setting.weeklyDigest?.time).toBe("07:05");
    expect(setting.weeklyDigest?.timezone).toBe("Europe/Berlin");
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
          dailyColumnAnalysisLimit: String(MAX_DAILY_COLUMN_ANALYSIS_LIMIT),
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
        {
          dailyColumnAnalysisLimit: String(MAX_DAILY_COLUMN_ANALYSIS_LIMIT + 1),
        },
        "dailyColumnAnalysisLimitInvalid",
      ],
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

  it("treats zero and exactly the maximum as valid daily column analyses", () => {
    expect(
      validateSettingsForm(baseForm({ dailyColumnAnalysisLimit: "0" }))
    ).toEqual([]);
    expect(
      validateSettingsForm(
        baseForm({
          dailyColumnAnalysisLimit: String(MAX_DAILY_COLUMN_ANALYSIS_LIMIT),
        })
      )
    ).toEqual([]);
    const issue = validateSettingsForm(
      baseForm({
        dailyColumnAnalysisLimit: String(MAX_DAILY_COLUMN_ANALYSIS_LIMIT + 1),
      })
    ).find(
      (candidate) =>
        candidate.key ===
        `${SETTINGS_VALIDATION_PREFIX}dailyColumnAnalysisLimitInvalid`
    );
    expect(issue?.params).toEqual({ max: MAX_DAILY_COLUMN_ANALYSIS_LIMIT });
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

  it("requires a time and an IANA zone once the digest is on", () => {
    const issues = validateSettingsForm(
      baseForm({
        weeklyDigest: {
          enabled: true,
          dayOfWeek: DayOfWeek.MONDAY,
          time: "",
          timezone: "",
        },
      })
    ).map((issue) => issue.key.slice(SETTINGS_VALIDATION_PREFIX.length));

    expect(issues).toContain("digestTimeRequired");
    expect(issues).toContain("digestTimezoneRequired");
  });

  it("accepts blank digest fields while the digest is off", () => {
    expect(
      validateSettingsForm(
        baseForm({
          weeklyDigest: {
            enabled: false,
            dayOfWeek: DayOfWeek.MONDAY,
            time: "",
            timezone: "",
          },
        })
      )
    ).toEqual([]);
  });

  it("rejects a malformed digest time and an unknown zone", () => {
    const issues = validateSettingsForm(
      baseForm({
        weeklyDigest: {
          enabled: false,
          dayOfWeek: DayOfWeek.MONDAY,
          time: "9:00",
          timezone: "Mars/Olympus",
        },
      })
    ).map((issue) => issue.key.slice(SETTINGS_VALIDATION_PREFIX.length));

    expect(issues).toContain("digestTimeInvalid");
    expect(issues).toContain("digestTimezoneInvalid");
  });

  it("rejects a digest with no weekday while it is on", () => {
    const issues = validateSettingsForm(
      baseForm({
        weeklyDigest: {
          enabled: true,
          dayOfWeek: DayOfWeek.UNSPECIFIED,
          time: "09:00",
          timezone: "UTC",
        },
      })
    ).map((issue) => issue.key.slice(SETTINGS_VALIDATION_PREFIX.length));

    expect(issues).toContain("digestDayInvalid");
  });

  it("accepts a configured digest", () => {
    expect(
      validateSettingsForm(
        baseForm({
          weeklyDigest: {
            enabled: true,
            dayOfWeek: DayOfWeek.SUNDAY,
            time: "07:05",
            timezone: "America/Los_Angeles",
          },
        })
      )
    ).toEqual([]);
  });
});

describe("digest vocabulary", () => {
  it("accepts 24-hour HH:MM only", () => {
    for (const value of ["00:00", "09:05", "23:59"]) {
      expect(DIGEST_TIME_PATTERN.test(value)).toBe(true);
    }
    for (const value of ["", "24:00", "9:00", "09:60", "09:05:00"]) {
      expect(DIGEST_TIME_PATTERN.test(value)).toBe(false);
    }
  });

  it("recognizes IANA zone names and rejects anything else", () => {
    for (const zone of [
      "UTC",
      "Asia/Shanghai",
      "Europe/Berlin",
      "Asia/Tokyo",
    ]) {
      expect(isValidTimeZone(zone)).toBe(true);
    }
    // The browser is more lenient than the server about case (`asia/shanghai`
    // resolves), so the server's own validation stays the authority.
    for (const zone of ["", "Mars/Olympus", "GMT+8"]) {
      expect(isValidTimeZone(zone)).toBe(false);
    }
  });
});
