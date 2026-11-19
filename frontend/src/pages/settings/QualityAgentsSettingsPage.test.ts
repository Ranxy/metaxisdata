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

const mainProfile = "llm-provider-profiles/main";
const secondaryProfile = "llm-provider-profiles/secondary";
const singleProfile = "llm-provider-profiles/single";

function providerProfile(
  name: string,
  title: string,
  models: Array<{ name: string; enabled: boolean }>
): LlmProviderProfile {
  return {
    name,
    title,
    models,
    $typeName: "metaxisdata.v1.LlmProviderProfile",
  } as LlmProviderProfile;
}

function providerProfiles(): LlmProviderProfile[] {
  return [
    providerProfile(mainProfile, "Main", [
      { name: "fast", enabled: true },
      { name: "reasoner", enabled: true },
      { name: "retired", enabled: false },
    ]),
    providerProfile(secondaryProfile, "Secondary", [
      { name: "fast", enabled: true },
      { name: "other", enabled: true },
    ]),
    providerProfile(singleProfile, "Single", [{ name: "only", enabled: true }]),
    providerProfile("llm-provider-profiles/excluded", "Excluded", [
      { name: "x", enabled: true },
    ]),
    providerProfile("llm-provider-profiles/disabled", "Disabled", [
      { name: "idle", enabled: false },
    ]),
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
  profilesError: boolean;
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

async function openEdit(wrapper: ReturnType<typeof mount>) {
  const button = wrapper
    .findAll("button")
    .find(
      (candidate) =>
        candidate.text().trim() === i18n.global.t("qualityAgents.edit")
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
        allowedLlmProviderProfiles: [
          mainProfile,
          secondaryProfile,
          singleProfile,
        ],
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
      mainProfile,
      secondaryProfile,
      singleProfile,
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

    vm(wrapper).setProviderProfile(mainProfile);
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
      providerProfile: mainProfile,
      providerModel: "reasoner",
    });
  });

  it("keeps an enabled model, auto-selects a single one and ignores a same-value click", async () => {
    const wrapper = await mountPage();
    await openCreate(wrapper);
    const page = vm(wrapper);

    page.setProviderProfile(mainProfile);
    await nextTick();
    page.form.providerModel = "reasoner";
    await nextTick();

    // Clicking the already-selected provider must not silently move the model.
    page.setProviderProfile(mainProfile);
    await nextTick();
    expect(page.form.providerModel).toBe("reasoner");

    // A profile that does not enable the current model requires a new choice.
    page.setProviderProfile(secondaryProfile);
    await nextTick();
    expect(page.modelOptions).toEqual(["fast", "other"]);
    expect(page.form.providerModel).toBe("");

    // A profile that also enables the current model keeps it.
    page.form.providerModel = "fast";
    await nextTick();
    page.setProviderProfile(mainProfile);
    await nextTick();
    expect(page.form.providerModel).toBe("fast");

    // A profile that enables exactly one model selects it; several require a choice.
    page.setProviderProfile(singleProfile);
    await nextTick();
    expect(page.form.providerModel).toBe("only");
  });

  it("keeps an unavailable stored pair, marks it and still saves other edits", async () => {
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

    // The list marks the pair and does not invent a title it could not load.
    expect(wrapper.text()).toContain(
      "llm-provider-profiles/gone · ghost (unavailable)"
    );

    await openEdit(wrapper);
    // An edit keeps a stored value it can no longer resolve, marked instead of
    // silently cleared.
    expect(vm(wrapper).form.providerProfile).toBe("llm-provider-profiles/gone");
    expect(vm(wrapper).providerUnavailable).toBe(true);
    expect(vm(wrapper).modelUnavailable).toBe(true);
    expect(dialogText()).toContain(
      i18n.global.t("qualityAgents.providerUnavailableHint")
    );

    const save = dialogButton(i18n.global.t("qualityAgents.saveAction"));
    // The server accepts an update that leaves the stale pair alone, so an unrelated
    // edit is not a dead end.
    expect(save?.disabled).toBe(false);
    save?.click();
    await flushPromises();

    expect(mocks.updateQualityAgent).toHaveBeenCalledTimes(1);
    const [entity, paths] = mocks.updateQualityAgent.mock.calls[0] ?? [];
    expect(entity.providerProfile).toBe("llm-provider-profiles/gone");
    expect(entity.providerModel).toBe("ghost");
    // An unchanged pair is not rewritten, so a dialog opened earlier cannot revert a
    // provider change made meanwhile.
    expect(paths).not.toContain("provider_profile");
    expect(paths).not.toContain("provider_model");
  });

  it("writes both pair paths when the model changes", async () => {
    mocks.listQualityAgents.mockResolvedValue([
      create(QualityAgentSchema, {
        name: "workspaces/ws/qualityAgents/c",
        displayName: "Agent C",
        owner: "users/1",
        providerProfile: mainProfile,
        providerModel: "fast",
        capabilities: ["suggest"],
        databases: ["instances/i/databases/d"],
        state: QualityAgentState.ACTIVE,
      }),
    ]);
    const wrapper = await mountPage();
    await openEdit(wrapper);

    vm(wrapper).form.providerModel = "reasoner";
    await nextTick();
    dialogButton(i18n.global.t("qualityAgents.saveAction"))?.click();
    await flushPromises();

    expect(mocks.updateQualityAgent).toHaveBeenCalledTimes(1);
    const [entity, paths] = mocks.updateQualityAgent.mock.calls[0] ?? [];
    expect(entity.providerModel).toBe("reasoner");
    expect(paths).toEqual(
      expect.arrayContaining(["provider_profile", "provider_model"])
    );
  });

  it("reports an unreadable profile list instead of marking pairs unavailable", async () => {
    mocks.listProfiles.mockRejectedValue(new Error("permission denied"));
    mocks.listQualityAgents.mockResolvedValue([
      create(QualityAgentSchema, {
        name: "workspaces/ws/qualityAgents/a",
        displayName: "Agent A",
        owner: "users/1",
        providerProfile: mainProfile,
        providerModel: "fast",
        capabilities: ["suggest"],
        databases: ["instances/i/databases/d"],
        state: QualityAgentState.PAUSED,
      }),
    ]);
    const wrapper = await mountPage();

    // The title is unknown when the list failed, but the pair is not called unavailable.
    expect(wrapper.text()).toContain(`${mainProfile} · fast`);
    expect(wrapper.text()).not.toContain(
      i18n.global.t("qualityAgents.providerUnavailable")
    );
    await openEdit(wrapper);
    expect(vm(wrapper).profilesError).toBe(true);
    expect(dialogText()).toContain(
      i18n.global.t("qualityAgents.providerProfilesError")
    );
  });

  it("shows not-configured for an agent without a pair", async () => {
    mocks.listQualityAgents.mockResolvedValue([
      create(QualityAgentSchema, {
        name: "workspaces/ws/qualityAgents/b",
        displayName: "Agent B",
        owner: "users/1",
        capabilities: ["suggest"],
        databases: ["instances/i/databases/d"],
        state: QualityAgentState.DRAFT,
      }),
    ]);
    const wrapper = await mountPage();
    expect(wrapper.text()).toContain(
      i18n.global.t("qualityAgents.providerNotConfigured")
    );
  });
});
