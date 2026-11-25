import { create } from "@bufbuild/protobuf";
import { enableAutoUnmount, flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { Select, SelectItem, SelectTrigger } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityAgentAdoptionMode,
  QualityAgentAdoptionSettingSchema,
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

// Radix opens a select on pointerdown and calls pointer-capture APIs that jsdom
// does not implement.
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.setPointerCapture ??= () => {};
Element.prototype.releasePointerCapture ??= () => {};

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

function selectById(wrapper: ReturnType<typeof mount>, id: string) {
  return wrapper
    .findAllComponents(Select)
    .find((select) =>
      select
        .findAllComponents(SelectTrigger)
        .some((trigger) => trigger.attributes("id") === id)
    );
}

/** Set a teleported input's value the way a keystroke would. */
function setInput(selector: string, value: string) {
  const element = document.querySelector(selector) as HTMLInputElement;
  Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value"
  )?.set?.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

/** The value the control renders as chosen. */
function chosen(wrapper: ReturnType<typeof mount>, id: string): unknown {
  return selectById(wrapper, id)?.props("modelValue");
}

/** Pick a value through the select's own event, as the UI would. */
async function choose(
  wrapper: ReturnType<typeof mount>,
  id: string,
  value: string
) {
  selectById(wrapper, id)?.vm.$emit("update:modelValue", value);
  await nextTick();
}

/** Open a select and read the values it offers. */
async function optionValues(
  wrapper: ReturnType<typeof mount>,
  id: string
): Promise<string[]> {
  document
    .querySelector<HTMLElement>(`#${id}`)
    ?.dispatchEvent(
      new MouseEvent("pointerdown", { bubbles: true, button: 0 })
    );
  await flushPromises();
  const items = selectById(wrapper, id)?.findAllComponents(SelectItem) ?? [];
  return items.map((item) => String(item.props("value")));
}

/** Click the checkbox inside the dialog label that carries `text`. */
async function clickDialogCheckbox(text: string) {
  const label = [...(dialog()?.querySelectorAll("label") ?? [])].find(
    (item) => item.textContent?.trim() === text
  );
  await clickCheckboxIn(label);
}

/** Click the checkbox of a label whose long text contains `text`. */
async function clickDialogCheckboxContaining(text: string) {
  const label = [...(dialog()?.querySelectorAll("label") ?? [])].find((item) =>
    item.textContent?.includes(text)
  );
  await clickCheckboxIn(label);
}

async function clickCheckboxIn(label: Element | undefined) {
  (label?.querySelector('[role="checkbox"]') as HTMLElement | null)?.click();
  await flushPromises();
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
        allowedLlmProviderModels: [
          { providerProfile: mainProfile, model: "fast" },
          { providerProfile: secondaryProfile, model: "fast" },
          { providerProfile: singleProfile, model: "only" },
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

  it("offers every profile that enables a model, ignoring the ExplainSQL allowlist", async () => {
    const wrapper = await mountPage();
    await openCreate(wrapper);

    // The fixture's allowed_llm_provider_models admits only three profiles, and
    // `excluded` is outside it; it is still offered because that allowlist
    // restricts Explain SQL only and an agent may pin any enabled model.
    expect(await optionValues(wrapper, "qa-provider")).toEqual([
      mainProfile,
      secondaryProfile,
      singleProfile,
      "llm-provider-profiles/excluded",
    ]);
    // An empty pair explains why the primary action is disabled.
    expect(dialogText()).toContain(
      i18n.global.t("qualityAgents.providerRequiredHint")
    );
  });

  it("follows the chosen profile's models and sends the pair on create", async () => {
    const wrapper = await mountPage();
    await openCreate(wrapper);

    await choose(wrapper, "qa-provider", mainProfile);
    // Several enabled models require an explicit choice, because their price and
    // quality differ.
    expect(chosen(wrapper, "qa-model")).toBe("");

    setInput("#qa-name", "Agent A");
    await nextTick();
    await choose(wrapper, "qa-model", "reasoner");
    await clickDialogCheckbox("d");
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

    await choose(wrapper, "qa-provider", mainProfile);
    await choose(wrapper, "qa-model", "reasoner");

    // Clicking the already-selected provider must not silently move the model.
    await choose(wrapper, "qa-provider", mainProfile);
    expect(chosen(wrapper, "qa-model")).toBe("reasoner");

    // A profile that does not enable the current model requires a new choice.
    await choose(wrapper, "qa-provider", secondaryProfile);
    expect(chosen(wrapper, "qa-model")).toBe("");

    // A profile that also enables the current model keeps it.
    await choose(wrapper, "qa-model", "fast");
    await choose(wrapper, "qa-provider", mainProfile);
    expect(chosen(wrapper, "qa-model")).toBe("fast");

    // A profile that enables exactly one model selects it; several require a choice.
    await choose(wrapper, "qa-provider", singleProfile);
    expect(chosen(wrapper, "qa-model")).toBe("only");
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
    expect(chosen(wrapper, "qa-provider")).toBe("llm-provider-profiles/gone");
    expect(chosen(wrapper, "qa-model")).toBe("ghost");
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

    await choose(wrapper, "qa-model", "reasoner");
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
    expect(dialogText()).toContain(
      i18n.global.t("qualityAgents.providerProfilesError")
    );
  });

  it("treats a profile outside the ExplainSQL allowlist as available", async () => {
    mocks.listQualityAgents.mockResolvedValue([
      create(QualityAgentSchema, {
        name: "workspaces/ws/qualityAgents/e",
        displayName: "Agent E",
        owner: "users/1",
        // Enabled, but deliberately absent from the workspace allowlist.
        providerProfile: "llm-provider-profiles/excluded",
        providerModel: "x",
        capabilities: ["suggest"],
        databases: ["instances/i/databases/d"],
        state: QualityAgentState.PAUSED,
      }),
    ]);
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain("Excluded · x");
    expect(wrapper.text()).not.toContain(
      i18n.global.t("qualityAgents.providerUnavailable")
    );

    await openEdit(wrapper);
    // The stored pair resolves against the profile list, so the dialog opens on it.
    expect(chosen(wrapper, "qa-provider")).toBe(
      "llm-provider-profiles/excluded"
    );
    expect(chosen(wrapper, "qa-model")).toBe("x");
    expect(dialogText()).not.toContain(
      i18n.global.t("qualityAgents.providerUnavailableHint")
    );
  });

  it("marks a stored pair whose profile enables no model as unavailable", async () => {
    mocks.listQualityAgents.mockResolvedValue([
      create(QualityAgentSchema, {
        name: "workspaces/ws/qualityAgents/f",
        displayName: "Agent F",
        owner: "users/1",
        providerProfile: "llm-provider-profiles/disabled",
        providerModel: "idle",
        capabilities: ["suggest"],
        databases: ["instances/i/databases/d"],
        state: QualityAgentState.PAUSED,
      }),
    ]);
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      `Disabled · idle (${i18n.global.t("qualityAgents.providerUnavailable")})`
    );

    await openEdit(wrapper);
    // The stale pair is kept, marked, and an unrelated edit can still be saved.
    expect(chosen(wrapper, "qa-provider")).toBe(
      "llm-provider-profiles/disabled"
    );
    expect(chosen(wrapper, "qa-model")).toBe("idle");
    expect(dialogText()).toContain(
      i18n.global.t("qualityAgents.providerUnavailableHint")
    );
    expect(
      dialogButton(i18n.global.t("qualityAgents.saveAction"))?.disabled
    ).toBe(false);
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

  it("round-trips OFF instead of coercing it to PAUSED", async () => {
    mocks.listQualityAgents.mockResolvedValue([
      create(QualityAgentSchema, {
        name: "workspaces/ws/qualityAgents/g",
        displayName: "Agent G",
        owner: "users/1",
        providerProfile: mainProfile,
        providerModel: "fast",
        capabilities: ["suggest", "adopt"],
        databases: ["instances/i/databases/d"],
        state: QualityAgentState.ACTIVE,
        adoption: create(QualityAgentAdoptionSettingSchema, {
          mode: QualityAgentAdoptionMode.OFF,
        }),
      }),
    ]);
    const wrapper = await mountPage();
    await openEdit(wrapper);

    // The stored OFF is shown as OFF, not silently turned into PAUSED.
    expect(
      wrapper
        .findAllComponents(Select)
        .some((select) => select.props("modelValue") === "off")
    ).toBe(true);
    dialogButton(i18n.global.t("qualityAgents.saveAction"))?.click();
    await flushPromises();
    const [edited] = mocks.updateQualityAgent.mock.calls[0] ?? [];
    expect(edited.adoption.mode).toBe(QualityAgentAdoptionMode.OFF);
  });

  it("sends each adoption mode chosen on create, OFF included", async () => {
    const wrapper = await mountPage();
    await openCreate(wrapper);
    setInput("#qa-name", "Agent H");
    await nextTick();
    await choose(wrapper, "qa-provider", mainProfile);
    await choose(wrapper, "qa-model", "fast");
    await clickDialogCheckbox("d");
    // Enabling adopt reveals its controls; pick OFF explicitly.
    await clickDialogCheckboxContaining(
      i18n.global.t("qualityAgents.capabilityAdopt")
    );
    const adoption = wrapper
      .findAllComponents(Select)
      .find((select) => select.props("modelValue") === "paused");
    expect(adoption).toBeDefined();
    adoption?.vm.$emit("update:modelValue", "off");
    await nextTick();
    dialogButton(i18n.global.t("qualityAgents.createAction"))?.click();
    await flushPromises();
    expect(
      mocks.createQualityAgent.mock.calls[0]?.[0].config.adoption.mode
    ).toBe(QualityAgentAdoptionMode.OFF);
  });
});
