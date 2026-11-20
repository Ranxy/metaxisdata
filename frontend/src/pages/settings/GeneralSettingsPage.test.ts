import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  LlmProviderModelRefSchema,
  type LlmProviderProfile,
} from "@/types/proto-es/v1/llm_service_pb";
import {
  type WorkspaceProfileSetting,
  WorkspaceProfileSettingSchema,
} from "@/types/proto-es/v1/setting_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import GeneralSettingsPage from "./GeneralSettingsPage.vue";

const mocks = vi.hoisted(() => ({
  getWorkspaceProfileSetting: vi.fn(),
  updateWorkspaceProfileSetting: vi.fn(),
  getDebugConfig: vi.fn(),
  updateDebugConfig: vi.fn(),
  listProfiles: vi.fn(),
}));

vi.mock("@/api/setting", () => ({
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
  updateWorkspaceProfileSetting: mocks.updateWorkspaceProfileSetting,
  getDebugConfig: mocks.getDebugConfig,
  updateDebugConfig: mocks.updateDebugConfig,
}));

vi.mock("@/api/llm", () => ({ listProfiles: mocks.listProfiles }));

vi.mock("@/api/version", () => ({
  fetchBuildInfo: vi.fn(async () => null),
}));

vi.mock("@/lib/notify", () => ({
  notify: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
  },
}));

const PERMISSIONS = ["metaxisdata.settings.get", "metaxisdata.settings.update"];

function modelRef(providerProfile: string, model: string) {
  return create(LlmProviderModelRefSchema, { providerProfile, model });
}

function profile(
  name: string,
  title: string,
  models: Array<{ name: string; enabled: boolean }>
): LlmProviderProfile {
  return { name, title, models } as unknown as LlmProviderProfile;
}

interface PageVm {
  allowedModels: Array<{ providerProfile: string; model: string }>;
  unavailableModels: Array<{ providerProfile: string; model: string }>;
  toggleAllowedModel: (
    profile: string,
    model: string,
    checked: boolean
  ) => void;
  modelGroups: Array<{ profile: string; title: string; models: string[] }>;
}

function vm(wrapper: ReturnType<typeof mount>): PageVm {
  return wrapper.vm as unknown as PageVm;
}

async function mountPage() {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push("/settings/general");
  await router.isReady();
  const wrapper = mount(GeneralSettingsPage, {
    global: { plugins: [i18n, pinia, router] },
  });
  await flushPromises();
  return wrapper;
}

/**
 * The checkbox for one (profile, model) pair. The id carries the raw resource
 * name, so it is matched as an attribute value rather than a CSS id selector.
 */
function modelCheckbox(
  wrapper: ReturnType<typeof mount>,
  profileName: string,
  model: string
) {
  return wrapper.find(`[id="allowed-model-${profileName}-${model}"]`);
}

function saveButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().includes(i18n.global.t("common.save")));
}

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
  mocks.getWorkspaceProfileSetting.mockResolvedValue(
    create(WorkspaceProfileSettingSchema, {
      externalUrl: "https://metaxisdata.example.com",
      allowedLlmProviderModels: [
        modelRef("llm-provider-profiles/main", "fast"),
      ],
    })
  );
  mocks.getDebugConfig.mockResolvedValue({ enabled: false });
  mocks.updateDebugConfig.mockResolvedValue({});
  mocks.updateWorkspaceProfileSetting.mockResolvedValue(
    create(WorkspaceProfileSettingSchema, {})
  );
  mocks.listProfiles.mockResolvedValue({
    profiles: [
      profile("llm-provider-profiles/main", "Main", [
        { name: "fast", enabled: true },
        { name: "reasoner", enabled: true },
        { name: "retired", enabled: false },
      ]),
      profile("llm-provider-profiles/off", "Off", [
        { name: "idle", enabled: false },
      ]),
    ],
    nextPageToken: "",
  });
});

describe("GeneralSettingsPage", () => {
  it("renders every enabled model grouped by its profile", async () => {
    const wrapper = await mountPage();

    expect(vm(wrapper).modelGroups).toEqual([
      {
        profile: "llm-provider-profiles/main",
        title: "Main",
        models: ["fast", "reasoner"],
      },
    ]);
    expect(wrapper.text()).toContain("Main");
    expect(wrapper.text()).toContain("reasoner");
    // A disabled model and a profile without any enabled model are not offered.
    expect(wrapper.text()).not.toContain("retired");
    expect(wrapper.text()).not.toContain("Off");
    expect(
      modelCheckbox(wrapper, "llm-provider-profiles/main", "fast").attributes(
        "data-state"
      )
    ).toBe("checked");
    expect(
      modelCheckbox(
        wrapper,
        "llm-provider-profiles/main",
        "reasoner"
      ).attributes("data-state")
    ).toBe("unchecked");
  });

  it("saves the toggled model list with the model-level update mask", async () => {
    const wrapper = await mountPage();

    await modelCheckbox(
      wrapper,
      "llm-provider-profiles/main",
      "reasoner"
    ).trigger("click");
    await flushPromises();
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.updateWorkspaceProfileSetting).toHaveBeenCalledTimes(1);
    const [setting, mask] = mocks.updateWorkspaceProfileSetting.mock
      .calls[0] as [WorkspaceProfileSetting, string[]];
    expect(
      setting.allowedLlmProviderModels.map((ref) => [
        ref.providerProfile,
        ref.model,
      ])
    ).toEqual([
      ["llm-provider-profiles/main", "fast"],
      ["llm-provider-profiles/main", "reasoner"],
    ]);
    expect(mask).toContain("allowed_llm_provider_models");
    expect(mask).not.toContain("allowed_llm_provider_profiles");
  });

  it("renders a stored pair the registry no longer enables so it can be removed", async () => {
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        allowedLlmProviderModels: [
          modelRef("llm-provider-profiles/main", "fast"),
          modelRef("llm-provider-profiles/gone", "x"),
        ],
      })
    );
    const wrapper = await mountPage();
    const page = vm(wrapper);

    // The gone profile is not in the list, but the pair still restricts Explain
    // SQL while the allowlist is non-empty, so it stays visible and removable.
    expect(
      page.unavailableModels.map((ref) => [ref.providerProfile, ref.model])
    ).toEqual([["llm-provider-profiles/gone", "x"]]);
    expect(wrapper.text()).toContain(
      i18n.global.t("generalSettings.explainProvidersUnavailable")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("generalSettings.explainProvidersUnavailableMarker")
    );

    await wrapper.find('[id="unavailable-model-0"]').trigger("click");
    await flushPromises();
    expect(page.unavailableModels).toEqual([]);
    expect(wrapper.text()).not.toContain(
      i18n.global.t("generalSettings.explainProvidersUnavailable")
    );

    // Unchecking the remaining enabled pair makes the allowlist empty, which
    // allows every enabled model again.
    await modelCheckbox(wrapper, "llm-provider-profiles/main", "fast").trigger(
      "click"
    );
    await flushPromises();
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();
    const [setting] = mocks.updateWorkspaceProfileSetting.mock.calls[0] as [
      WorkspaceProfileSetting,
    ];
    expect(setting.allowedLlmProviderModels).toEqual([]);
  });

  it("keeps an unrendered pair until it is removed", async () => {
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        allowedLlmProviderModels: [
          modelRef("llm-provider-profiles/main", "fast"),
          modelRef("llm-provider-profiles/gone", "x"),
        ],
      })
    );
    const wrapper = await mountPage();

    vm(wrapper).toggleAllowedModel("llm-provider-profiles/main", "fast", false);
    await flushPromises();
    await saveButton(wrapper)?.trigger("click");
    await flushPromises();

    const [setting] = mocks.updateWorkspaceProfileSetting.mock.calls[0] as [
      WorkspaceProfileSetting,
    ];
    // The authorization survives the save instead of being dropped silently.
    expect(
      setting.allowedLlmProviderModels.map((ref) => [
        ref.providerProfile,
        ref.model,
      ])
    ).toEqual([["llm-provider-profiles/gone", "x"]]);
  });

  it("de-duplicates a pair toggled on twice", async () => {
    const wrapper = await mountPage();
    const page = vm(wrapper);

    page.toggleAllowedModel("llm-provider-profiles/main", "reasoner", true);
    page.toggleAllowedModel("llm-provider-profiles/main", "fast", true);
    await flushPromises();

    // Checking an already-authorized pair again does not add a duplicate.
    expect(
      page.allowedModels.map((ref) => [ref.providerProfile, ref.model])
    ).toEqual([
      ["llm-provider-profiles/main", "fast"],
      ["llm-provider-profiles/main", "reasoner"],
    ]);
  });

  it("explains when no profile enables a model", async () => {
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        externalUrl: "https://metaxisdata.example.com",
        allowedLlmProviderModels: [],
      })
    );
    mocks.listProfiles.mockResolvedValue({
      profiles: [
        profile("llm-provider-profiles/off", "Off", [
          { name: "idle", enabled: false },
        ]),
      ],
      nextPageToken: "",
    });
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("generalSettings.explainProvidersEmpty")
    );
  });
});
