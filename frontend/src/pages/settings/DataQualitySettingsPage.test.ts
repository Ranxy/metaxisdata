import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FormField from "@/components/ui/form-field/FormField.vue";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  DataQualitySettingSchema,
  WorkspaceProfileSettingSchema,
} from "@/types/proto-es/v1/setting_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualitySettingsPage from "./DataQualitySettingsPage.vue";

const mocks = vi.hoisted(() => ({
  getWorkspaceProfileSetting: vi.fn(),
  updateDataQualitySetting: vi.fn(),
  listProfiles: vi.fn(),
}));

vi.mock("@/api/setting", () => ({
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
  updateDataQualitySetting: mocks.updateDataQualitySetting,
}));

vi.mock("@/api/llm", () => ({
  listProfiles: mocks.listProfiles,
}));

const PERMISSIONS = ["metaxisdata.settings.get", "metaxisdata.settings.update"];

async function mountPage() {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
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
    // The LLM profile select lists the allowed profile.
    expect(wrapper.text()).toContain("Primary");
  });

  it("saves the whole section with the expected setting", async () => {
    const wrapper = await mountPage();
    await fieldInput(
      wrapper,
      i18n.global.t("dataQualitySettings.failureSampleLimit")
    )?.setValue("25");

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
});
