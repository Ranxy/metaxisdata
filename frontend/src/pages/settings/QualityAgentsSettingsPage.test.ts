import { create } from "@bufbuild/protobuf";
import { enableAutoUnmount, flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityAgentSchema,
  QualityAgentState,
} from "@/types/proto-es/v1/data_quality_service_pb";
import type { LlmProviderProfile } from "@/types/proto-es/v1/llm_service_pb";
import {
  DataQualitySettingSchema,
  WorkspaceProfileSettingSchema,
} from "@/types/proto-es/v1/setting_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import QualityAgentsSettingsPage from "./QualityAgentsSettingsPage.vue";

// Each case mounts its own page; a leftover teleported dialog would answer the next
// case's queries.
enableAutoUnmount(afterEach);

const mocks = vi.hoisted(() => ({
  createQualityAgent: vi.fn(),
  disableQualityAgent: vi.fn(),
  listQualityAgents: vi.fn(),
  pauseQualityAgent: vi.fn(),
  startQualityAgent: vi.fn(),
  updateQualityAgent: vi.fn(),
  listDatabases: vi.fn(),
  listProfiles: vi.fn(),
  getWorkspaceProfileSetting: vi.fn(),
  updateDataQualitySetting: vi.fn(),
  listAllUsers: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  createQualityAgent: mocks.createQualityAgent,
  disableQualityAgent: mocks.disableQualityAgent,
  listQualityAgents: mocks.listQualityAgents,
  pauseQualityAgent: mocks.pauseQualityAgent,
  startQualityAgent: mocks.startQualityAgent,
  updateQualityAgent: mocks.updateQualityAgent,
}));

vi.mock("@/api/database", () => ({ listDatabases: mocks.listDatabases }));

vi.mock("@/api/llm", () => ({ listProfiles: mocks.listProfiles }));

vi.mock("@/api/setting", () => ({
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
  updateDataQualitySetting: mocks.updateDataQualitySetting,
}));

vi.mock("@/api/user", () => ({ listAllUsers: mocks.listAllUsers }));

vi.mock("@/lib/notify", () => ({
  notify: { success: vi.fn(), error: vi.fn() },
}));

const allowedProfile = "llm-provider-profiles/main";

function providerProfiles(): LlmProviderProfile[] {
  return [
    {
      name: allowedProfile,
      title: "Main",
      type: 1,
      baseUrl: "",
      apiKey: "",
      models: [
        { name: "fast", enabled: true },
        { name: "reasoner", enabled: true },
        { name: "retired", enabled: false },
      ],
      createTime: undefined,
      updateTime: undefined,
      maskedApiKey: "",
      maxOutputTokens: 0,
      $typeName: "metaxisdata.v1.LlmProviderProfile",
    } as LlmProviderProfile,
    {
      name: "llm-provider-profiles/other",
      title: "Other",
      models: [{ name: "other-model", enabled: true }],
      $typeName: "metaxisdata.v1.LlmProviderProfile",
    } as LlmProviderProfile,
    {
      name: "llm-provider-profiles/disabled",
      title: "Disabled",
      models: [{ name: "idle", enabled: false }],
      $typeName: "metaxisdata.v1.LlmProviderProfile",
    } as LlmProviderProfile,
  ];
}

interface PageVm {
  form: {
    displayName: string;
    owner: string;
    providerProfile: string;
    providerModel: string;
    databases: string[];
  };
  providerOptions: Array<{ name: string; title: string }>;
  modelOptions: string[];
  providerUnavailable: boolean;
  modelUnavailable: boolean;
  canCreate: boolean;
  setProviderProfile: (value: unknown) => void;
}

function vm(wrapper: ReturnType<typeof mount>): PageVm {
  return wrapper.vm as unknown as PageVm;
}

function dialog(): HTMLElement | undefined {
  return (
    document.body.querySelector<HTMLElement>('[role="dialog"]') ?? undefined
  );
}

function dialogText(): string {
  return dialog()?.textContent ?? "";
}

function dialogButton(text: string): HTMLButtonElement | undefined {
  return [...(dialog()?.querySelectorAll("button") ?? [])].find(
    (button) => button.textContent?.trim() === text
  );
}

async function mountPage() {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, {
    permissions: ["metaxisdata.dataQuality.agents.manage"],
  });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/data-quality/agents",
        name: "QualityAgents",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/agents/:workspace/:agentId",
        name: "QualityAgent",
        component: { template: "<div />" },
      },
      { path: "/:pathMatch(.*)*", component: { template: "<div />" } },
    ],
  });
  await router.push("/settings/quality-agents");
  await router.isReady();
  const wrapper = mount(QualityAgentsSettingsPage, {
    global: { plugins: [i18n, pinia, router] },
  });
  await flushPromises();
  return wrapper;
}

async function openCreate(wrapper: ReturnType<typeof mount>) {
  const button = wrapper
    .findAll("button")
    .find(
      (candidate) =>
        candidate.text().trim() === i18n.global.t("qualityAgents.create")
    );
  await button?.trigger("click");
  await flushPromises();
}

describe("QualityAgentsSettingsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listQualityAgents.mockResolvedValue([]);
    mocks.listDatabases.mockResolvedValue({
      databases: [{ name: "instances/i/databases/d" }],
      nextPageToken: "",
    });
    mocks.listAllUsers.mockResolvedValue([
      create(UserSchema, { name: "users/1", email: "owner@example.com" }),
    ]);
    mocks.listProfiles.mockResolvedValue({ profiles: providerProfiles() });
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        allowedLlmProviderProfiles: [allowedProfile],
        dataQuality: create(DataQualitySettingSchema, {
          enableLlmAnalysis: true,
          agentRunnerEnabled: true,
        }),
      })
    );
    mocks.createQualityAgent.mockResolvedValue({ agent: undefined });
    mocks.updateQualityAgent.mockResolvedValue(undefined);
    mocks.updateDataQualitySetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {})
    );
  });

  it("offers only the allowed profiles that enable a model", async () => {
    const wrapper = await mountPage();
    await openCreate(wrapper);

    expect(vm(wrapper).providerOptions.map((option) => option.name)).toEqual([
      allowedProfile,
    ]);
    // The dialog renders the selected value; the list is filtered by the helper.
    expect(dialogText()).toContain(
      i18n.global.t("qualityAgents.providerProfile")
    );
    // An empty pair explains why the primary action is disabled.
    expect(dialogText()).toContain(
      i18n.global.t("qualityAgents.providerRequiredHint")
    );
  });

  it("follows the chosen profile's models and sends the pair on create", async () => {
    const wrapper = await mountPage();
    await openCreate(wrapper);

    vm(wrapper).setProviderProfile(allowedProfile);
    await nextTick();

    expect(vm(wrapper).modelOptions).toEqual(["fast", "reasoner"]);
    // Several enabled models require an explicit choice, because their price and
    // quality differ.
    expect(vm(wrapper).form.providerModel).toBe("");

    vm(wrapper).form.displayName = "Agent A";
    vm(wrapper).form.providerModel = "reasoner";
    vm(wrapper).form.databases = ["instances/i/databases/d"];
    await nextTick();
    expect(vm(wrapper).canCreate).toBe(true);
    const createButton = dialogButton(
      i18n.global.t("qualityAgents.createAction")
    );
    expect(createButton?.disabled).toBe(false);
    createButton?.click();
    await flushPromises();

    expect(mocks.createQualityAgent).toHaveBeenCalledTimes(1);
    const request = mocks.createQualityAgent.mock.calls[0]?.[0];
    expect(request.config).toMatchObject({
      providerProfile: allowedProfile,
      providerModel: "reasoner",
    });
  });

  it("keeps an unavailable stored pair and saves both mask paths", async () => {
    mocks.listQualityAgents.mockResolvedValue([
      create(QualityAgentSchema, {
        name: "workspaces/ws/qualityAgents/a",
        displayName: "Agent A",
        owner: "users/1",
        providerProfile: "llm-provider-profiles/gone",
        providerModel: "ghost",
        capabilities: ["suggest"],
        databases: ["instances/i/databases/d"],
        state: QualityAgentState.PAUSED,
      }),
    ]);
    const wrapper = await mountPage();

    const edit = wrapper
      .findAll("button")
      .find(
        (candidate) =>
          candidate.text().trim() === i18n.global.t("qualityAgents.edit")
      );
    await edit?.trigger("click");
    await flushPromises();

    // An edit keeps a stored value it can no longer resolve, marked instead of
    // silently cleared.
    expect(vm(wrapper).form.providerProfile).toBe("llm-provider-profiles/gone");
    expect(vm(wrapper).providerUnavailable).toBe(true);
    expect(vm(wrapper).modelUnavailable).toBe(true);
    expect(dialogText()).toContain(
      i18n.global.t("qualityAgents.providerUnavailableHint")
    );

    dialogButton(i18n.global.t("qualityAgents.saveAction"))?.click();
    await flushPromises();

    expect(mocks.updateQualityAgent).toHaveBeenCalledTimes(1);
    const paths = mocks.updateQualityAgent.mock.calls[0]?.[1];
    expect(paths).toEqual(
      expect.arrayContaining(["provider_profile", "provider_model"])
    );
  });
});
