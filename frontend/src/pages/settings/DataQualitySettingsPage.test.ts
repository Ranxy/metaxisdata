import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import { QualityLLMUsageSchema } from "@/types/proto-es/v1/data_quality_service_pb";
import {
  DataQualitySettingSchema,
  DayOfWeek,
  QualityDigestSettingSchema,
  WorkspaceProfileSettingSchema,
} from "@/types/proto-es/v1/setting_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualitySettingsPage from "./DataQualitySettingsPage.vue";

const mocks = vi.hoisted(() => ({
  getWorkspaceProfileSetting: vi.fn(),
  updateDataQualitySetting: vi.fn(),
  listProfiles: vi.fn(),
  getQualityLLMUsage: vi.fn(),
}));

vi.mock("@/api/setting", () => ({
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
  updateDataQualitySetting: mocks.updateDataQualitySetting,
}));

vi.mock("@/api/llm", () => ({
  listProfiles: mocks.listProfiles,
}));

vi.mock("@/api/data-quality", () => ({
  getQualityLLMUsage: mocks.getQualityLLMUsage,
}));

const PERMISSIONS = [
  "metaxisdata.settings.get",
  "metaxisdata.settings.update",
  "metaxisdata.dataQuality.suggest",
];

async function mountPage(permissions = PERMISSIONS) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(DataQualitySettingsPage, {
    global: { plugins: [i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

function fieldInput(
  wrapper: ReturnType<typeof mount>,
  label: string
): ReturnType<ReturnType<typeof mount>["find"]> | undefined {
  const field = wrapper
    .findAllComponents(FormField)
    .find((candidate) => candidate.props("label") === label);
  return field?.find("input");
}

/** The header's save button. */
function saveButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().includes(i18n.global.t("common.save")));
}

/** The digest's weekday select, found by the id its trigger carries. */
function digestDaySelect(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAllComponents(Select)
    .find((candidate) => candidate.find("#dq-setting-digest-day").exists());
}

describe("DataQualitySettingsPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        allowedLlmProviderProfiles: ["llmProfiles/p1"],
        dataQuality: create(DataQualitySettingSchema, {
          enableLlmAnalysis: true,
          llmProviderProfile: "llmProfiles/p1",
          storeFailureSamples: true,
          failureSampleLimit: 20,
          defaultScheduleInterval: create(DurationSchema, {
            seconds: 3_600n,
            nanos: 0,
          }),
          dailyExecutionLimit: 10000,
          dailyLlmAnalysisLimit: 5000,
          llmConcurrencyLimit: 12,
          maxColumnsPerAnalysis: 50,
          sendSampleValuesToLlm: false,
        }),
      })
    );
    mocks.listProfiles.mockResolvedValue({
      profiles: [
        {
          name: "llmProfiles/p1",
          title: "Primary",
          models: [{ enabled: true }],
        },
      ],
    });
    mocks.updateDataQualitySetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {})
    );
    mocks.getQualityLLMUsage.mockResolvedValue(
      create(QualityLLMUsageSchema, {
        dailyAnalysisUsed: 1200,
        dailyAnalysisLimit: 10000,
        dailyColumnAnalysisUsed: 30,
        dailyColumnAnalysisLimit: 50,
        inFlight: 2,
        concurrencyLimit: 12,
        resetsAt: { seconds: 1_700_035_200n, nanos: 0 },
      })
    );
  });

  it("shows the effective values the server returned", async () => {
    const wrapper = await mountPage();

    expect(
      (
        fieldInput(
          wrapper,
          i18n.global.t("dataQualitySettings.failureSampleLimit")
        )?.element as HTMLInputElement
      ).value
    ).toBe("20");
    expect(
      (wrapper.find("#dq-setting-schedule").element as HTMLInputElement).value
    ).toBe("1");
    expect(
      (
        fieldInput(
          wrapper,
          i18n.global.t("dataQualitySettings.maxColumnsPerAnalysis")
        )?.element as HTMLInputElement
      ).value
    ).toBe("50");
    expect(
      (
        fieldInput(
          wrapper,
          i18n.global.t("dataQualitySettings.dailyLlmAnalysisLimit")
        )?.element as HTMLInputElement
      ).value
    ).toBe("5000");
    expect(
      (
        fieldInput(
          wrapper,
          i18n.global.t("dataQualitySettings.llmConcurrencyLimit")
        )?.element as HTMLInputElement
      ).value
    ).toBe("12");
    // The LLM profile select lists the allowed profile.
    expect(wrapper.text()).toContain("Primary");
  });

  it("saves the whole section with the expected setting", async () => {
    const wrapper = await mountPage();
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.failureSampleLimit")
    )?.setValue("25");
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.dailyLlmAnalysisLimit")
    )?.setValue("25000");
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.llmConcurrencyLimit")
    )?.setValue("6");

    const save = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.save")));
    expect(save).toBeTruthy();
    await save?.trigger("click");
    await flushPromises();

    expect(mocks.updateDataQualitySetting).toHaveBeenCalledTimes(1);
    const setting = mocks.updateDataQualitySetting.mock.calls[0]?.[0];
    expect(setting).toMatchObject({
      enableLlmAnalysis: true,
      llmProviderProfile: "llmProfiles/p1",
      storeFailureSamples: true,
      failureSampleLimit: 25,
      dailyExecutionLimit: 10000,
      dailyLlmAnalysisLimit: 25000,
      llmConcurrencyLimit: 6,
      maxColumnsPerAnalysis: 50,
    });
    expect(setting.defaultScheduleInterval?.seconds).toBe(3_600n);
  });

  it("reports a bad number inline and does not save", async () => {
    const wrapper = await mountPage();
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.failureSampleLimit")
    )?.setValue("1000");

    const save = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.save")));
    await save?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t(
        "dataQualitySettings.validation.failureSampleLimitInvalid",
        {
          max: 100,
        }
      )
    );
    expect(mocks.updateDataQualitySetting).not.toHaveBeenCalled();
  });

  it("shows the process-wide AI usage counters", async () => {
    const wrapper = await mountPage();

    expect(mocks.getQualityLLMUsage).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageRatio", {
        used: "1,200",
        limit: "10,000",
      })
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageRatio", {
        used: "30",
        limit: "50",
      })
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageRatio", {
        used: "2",
        limit: "12",
      })
    );
  });

  it("shows a usage error with a retry that recovers", async () => {
    mocks.getQualityLLMUsage.mockRejectedValueOnce(new Error("usage down"));
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageFetchFailed")
    );

    mocks.getQualityLLMUsage.mockResolvedValue(
      create(QualityLLMUsageSchema, {
        dailyAnalysisUsed: 5,
        dailyAnalysisLimit: 100,
      })
    );
    const refresh = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("common.refresh"))
      );
    await refresh?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageRatio", {
        used: "5",
        limit: "100",
      })
    );
  });

  it("explains the usage counter is unavailable without the suggest permission", async () => {
    const wrapper = await mountPage([
      "metaxisdata.settings.get",
      "metaxisdata.settings.update",
    ]);

    expect(mocks.getQualityLLMUsage).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.usageNoPermission")
    );
  });

  it("refetches the usage counters after a save", async () => {
    const wrapper = await mountPage();
    expect(mocks.getQualityLLMUsage).toHaveBeenCalledTimes(1);

    const save = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.save")));
    await save?.trigger("click");
    await flushPromises();

    expect(mocks.getQualityLLMUsage).toHaveBeenCalledTimes(2);
  });

  it("validates the daily column-analysis ceiling inline", async () => {
    const wrapper = await mountPage();
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.dailyColumnAnalysisLimit")
    )?.setValue("1000001");

    const save = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.save")));
    await save?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t(
        "dataQualitySettings.validation.dailyColumnAnalysisLimitInvalid",
        { max: 1_000_000 }
      )
    );
    expect(mocks.updateDataQualitySetting).not.toHaveBeenCalled();
  });

  it("validates the new LLM limits before saving", async () => {
    const wrapper = await mountPage();
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.dailyLlmAnalysisLimit")
    )?.setValue("1000001");
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.llmConcurrencyLimit")
    )?.setValue("65");

    const save = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.save")));
    await save?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t(
        "dataQualitySettings.validation.dailyLlmAnalysisLimitInvalid",
        {
          max: 1_000_000,
        }
      )
    );
    expect(wrapper.text()).toContain(
      i18n.global.t(
        "dataQualitySettings.validation.llmConcurrencyLimitInvalid",
        {
          max: 64,
        }
      )
    );
    expect(mocks.updateDataQualitySetting).not.toHaveBeenCalled();
  });

  it("disables the LLM profile when LLM analysis is off", async () => {
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        dataQuality: create(DataQualitySettingSchema, {
          enableLlmAnalysis: false,
          failureSampleLimit: 20,
          defaultScheduleInterval: create(DurationSchema, {
            seconds: 3_600n,
            nanos: 0,
          }),
        }),
      })
    );
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQualitySettings.llmDisabledHint")
    );
  });

  it("shows the stored weekly digest schedule", async () => {
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        dataQuality: create(DataQualitySettingSchema, {
          weeklyDigest: create(QualityDigestSettingSchema, {
            enabled: true,
            dayOfWeek: DayOfWeek.FRIDAY,
            time: "18:30",
            timezone: "Asia/Shanghai",
          }),
        }),
      })
    );
    const wrapper = await mountPage();

    expect(
      wrapper.find("#dq-setting-digest-enabled").attributes("data-state")
    ).toBe("checked");
    expect(
      (
        fieldInput(wrapper, i18n.global.t("dataQualitySettings.digestTime"))
          ?.element as HTMLInputElement
      ).value
    ).toBe("18:30");
    expect(
      (
        fieldInput(wrapper, i18n.global.t("dataQualitySettings.digestTimezone"))
          ?.element as HTMLInputElement
      ).value
    ).toBe("Asia/Shanghai");
    expect(wrapper.html()).toContain(
      i18n.global.t("dataQualitySettings.weekdayFriday")
    );
  });

  it("saves the weekly digest with the rest of the settings", async () => {
    const wrapper = await mountPage();

    await wrapper.find("#dq-setting-digest-enabled").trigger("click");
    await flushPromises();
    await digestDaySelect(wrapper)?.vm.$emit(
      "update:modelValue",
      String(DayOfWeek.SUNDAY)
    );
    await flushPromises();
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.digestTime")
    )?.setValue("07:05");
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.digestTimezone")
    )?.setValue("Europe/Berlin");

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.updateDataQualitySetting).toHaveBeenCalledTimes(1);
    const setting = mocks.updateDataQualitySetting.mock.calls[0]?.[0];
    expect(setting.weeklyDigest).toMatchObject({
      enabled: true,
      dayOfWeek: DayOfWeek.SUNDAY,
      time: "07:05",
      timezone: "Europe/Berlin",
    });
  });

  it("carries the untouched digest so saving cannot clear it", async () => {
    const wrapper = await mountPage();

    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    const setting = mocks.updateDataQualitySetting.mock.calls[0]?.[0];
    // The whole data_quality field is replaced, so even an untouched digest has
    // to travel with the request.
    expect(setting.weeklyDigest).toMatchObject({
      enabled: false,
      dayOfWeek: DayOfWeek.MONDAY,
      time: "09:00",
      timezone: "UTC",
    });
  });

  it("surfaces the server's digest validation error", async () => {
    const message =
      'weekly_digest.timezone "asia/shanghai" is not a valid IANA time zone';
    mocks.updateDataQualitySetting.mockRejectedValueOnce(
      new ConnectError(message, Code.InvalidArgument)
    );
    const wrapper = await mountPage();

    await wrapper.find("#dq-setting-digest-enabled").trigger("click");
    await flushPromises();
    // The browser accepts the lower-case name, so only the server can reject it.
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.digestTimezone")
    )?.setValue("asia/shanghai");
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(message);
  });

  it("keeps the digest controls read-only without settings.update", async () => {
    const wrapper = await mountPage(["metaxisdata.settings.get"]);

    expect(
      wrapper.find("#dq-setting-digest-enabled").attributes("disabled")
    ).toBeDefined();
    expect(
      (
        fieldInput(wrapper, i18n.global.t("dataQualitySettings.digestTimezone"))
          ?.element as HTMLInputElement
      ).disabled
    ).toBe(true);
  });
});
