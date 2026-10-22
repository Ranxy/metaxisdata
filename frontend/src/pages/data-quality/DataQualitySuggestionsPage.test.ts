import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { notify } from "@/lib/notify";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  QualityTargetSchema,
  RowCountConfigSchema,
  SemanticLabelSuggestionSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualitySuggestionsPage from "./DataQualitySuggestionsPage.vue";

const mocks = vi.hoisted(() => ({
  listQualitySuggestions: vi.fn(),
  updateQualitySuggestion: vi.fn(),
  suggestQualityRule: vi.fn(),
  batchCreateQualityRules: vi.fn(),
  batchUpdateQualitySuggestions: vi.fn(),
  listQualitySuggestionJobs: vi.fn(),
  cancelQualitySuggestionJob: vi.fn(),
  getWorkspaceProfileSetting: vi.fn(),
  listMetadata: vi.fn(),
  listAllInstances: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualitySuggestions: mocks.listQualitySuggestions,
  updateQualitySuggestion: mocks.updateQualitySuggestion,
  suggestQualityRule: mocks.suggestQualityRule,
  batchCreateQualityRules: mocks.batchCreateQualityRules,
  batchUpdateQualitySuggestions: mocks.batchUpdateQualitySuggestions,
  listQualitySuggestionJobs: mocks.listQualitySuggestionJobs,
  cancelQualitySuggestionJob: mocks.cancelQualitySuggestionJob,
}));

vi.mock("@/api/setting", () => ({
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
}));

vi.mock("@/api/database", () => ({
  listMetadata: mocks.listMetadata,
}));

vi.mock("@/api/instance", () => ({
  listAllInstances: mocks.listAllInstances,
}));

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.dataQuality.suggest",
];

const TARGET = "inst1;db1;public;orders";
const OTHER_TARGET = "inst1;db1;public;customers";
const DRAFT_ONE = "workspaces/ws/qualitySuggestions/draft-1";
const DRAFT_TWO = "workspaces/ws/qualitySuggestions/draft-2";
const LABEL_ONE = "workspaces/ws/qualitySuggestions/label-1";
const Dummy = { template: "<div />" };

function draftSuggestion(name: string, title: string, target = TARGET) {
  return create(QualitySuggestionSchema, {
    name,
    kind: QualitySuggestionKind.RULE_DRAFT,
    source: QualitySuggestionSource.NATURAL_LANGUAGE,
    status: QualitySuggestionStatus.PENDING,
    provider: "openai",
    model: "gpt-test",
    target: create(QualityTargetSchema, {
      metaGuid: target,
      metaType: MetaType.TABLE,
    }),
    payload: {
      payload: {
        case: "rule",
        value: {
          title,
          rationale: "Fact table.",
          severity: 2,
          confidence: 0.8,
          config: create(QualityRuleConfigSchema, {
            config: {
              case: "rowCount",
              value: create(RowCountConfigSchema, { minRows: 1n }),
            },
          }),
        },
      },
    },
  });
}

function labelSuggestion(target = OTHER_TARGET) {
  return create(QualitySuggestionSchema, {
    name: LABEL_ONE,
    kind: QualitySuggestionKind.LABEL,
    source: QualitySuggestionSource.PROFILE,
    status: QualitySuggestionStatus.PENDING,
    target: create(QualityTargetSchema, {
      metaGuid: target,
      metaType: MetaType.TABLE,
    }),
    payload: {
      payload: {
        case: "label",
        value: create(SemanticLabelSuggestionSchema, {
          column: "email",
          confidence: 0.9,
        }),
      },
    },
  });
}

function queue() {
  return [
    draftSuggestion(DRAFT_ONE, "Orders should have rows"),
    draftSuggestion(DRAFT_TWO, "Orders must stay fresh"),
    labelSuggestion(),
  ];
}

function batchCreateResult(error = "", suggestion = "") {
  return {
    rule: error
      ? undefined
      : create(QualityRuleSchema, {
          name: "workspaces/ws/qualityRules/rule-1",
          title: "Orders should have rows",
        }),
    error,
    suggestion,
  };
}

async function mountPage(path = "/data-quality/suggestions") {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/data-quality/suggestions",
        name: "DataQualitySuggestions",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/new",
        name: "DataQualityRuleNew",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/:ruleId+/detail",
        name: "DataQualityRuleDetail",
        component: Dummy,
      },
      {
        path: "/data-quality/rules",
        name: "DataQualityRules",
        component: Dummy,
      },
      {
        path: "/settings/data-quality",
        name: "DataQualitySettings",
        component: Dummy,
      },
    ],
  });
  await router.push(path);
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  const wrapper = mount(DataQualitySuggestionsPage, {
    attachTo: document.body,
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

function buttonByText(text: string): HTMLElement | undefined {
  return [...document.body.querySelectorAll<HTMLElement>("button")].find(
    (button) => button.textContent?.includes(text)
  );
}

function dialogButtonByText(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.includes(text));
}

function checkboxes(): HTMLElement[] {
  return [...document.body.querySelectorAll<HTMLElement>('[role="checkbox"]')];
}

/** The first checkbox in each group header selects all of that target's rows. */
function groupCheckbox(index: number): HTMLElement | undefined {
  return checkboxes()[index];
}

function reviewButton(index = 0): HTMLElement | undefined {
  return document.body.querySelectorAll<HTMLElement>('[aria-label="Review"]')[
    index
  ];
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

describe("DataQualitySuggestionsPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.listQualitySuggestions.mockResolvedValue({
      items: queue(),
      nextPageToken: "",
    });
    mocks.getWorkspaceProfileSetting.mockResolvedValue({
      dataQuality: { enableLlmAnalysis: true },
    });
    mocks.listMetadata.mockResolvedValue({ typesStoredMetadata: [] });
    mocks.listAllInstances.mockResolvedValue([]);
    mocks.listQualitySuggestionJobs.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    mocks.updateQualitySuggestion.mockImplementation(async (options) =>
      create(QualitySuggestionSchema, { name: options.name })
    );
    mocks.batchUpdateQualitySuggestions.mockImplementation(
      async (items: Array<{ name: string }>) =>
        items.map((item) => ({
          suggestion: create(QualitySuggestionSchema, { name: item.name }),
          error: "",
        }))
    );
    mocks.batchCreateQualityRules.mockResolvedValue([
      batchCreateResult("", DRAFT_ONE),
      batchCreateResult("compile failed", DRAFT_TWO),
    ]);
  });

  it("loads the pending queue and groups it by target", async () => {
    const { wrapper } = await mountPage();

    expect(mocks.listQualitySuggestions).toHaveBeenCalledWith(
      expect.objectContaining({
        status: QualitySuggestionStatus.PENDING,
      })
    );
    // Two targets: the two order drafts share a group, the label has its own.
    expect(wrapper.text()).toContain("Orders should have rows");
    expect(wrapper.text()).toContain("Orders must stay fresh");
    expect(wrapper.text()).toContain("email");
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.groupCount", { count: 2 })
    );
    expect(wrapper.text()).toContain(TARGET.split(";").join(" · "));
    expect(wrapper.text()).toContain(OTHER_TARGET.split(";").join(" · "));

    // The group header's collapse control exposes its state.
    const collapse = document.body.querySelector<HTMLElement>(
      `[aria-label="${label("dataQuality.suggestions.collapseGroup")}"]`
    );
    expect(collapse?.getAttribute("aria-expanded")).toBe("true");
    collapse?.click();
    await flushPromises();
    expect(
      document.body
        .querySelector<HTMLElement>(
          `[aria-label="${label("dataQuality.suggestions.expandGroup")}"]`
        )
        ?.getAttribute("aria-expanded")
    ).toBe("false");
    wrapper.unmount();
  });

  it("creates rules for the selected drafts and reports partial failures", async () => {
    const { wrapper } = await mountPage();

    groupCheckbox(0)?.click();
    await flushPromises();
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.selectedCount", { count: 2 })
    );

    buttonByText(
      label("dataQuality.suggestions.createRules", { count: 2 })
    )?.click();
    await flushPromises();
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.batchCreate.title")
    );

    dialogButtonByText(
      label("dataQuality.suggestions.batchCreate.submit", { count: 2 })
    )?.click();
    await flushPromises();

    expect(mocks.batchCreateQualityRules).toHaveBeenCalledTimes(1);
    const items = mocks.batchCreateQualityRules.mock.calls[0]?.[0] as Array<{
      rule: unknown;
      suggestion: string;
    }>;
    expect(items).toHaveLength(2);
    expect(items.map((item) => item.suggestion)).toEqual([
      DRAFT_ONE,
      DRAFT_TWO,
    ]);

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.batchCreate.partial", {
        succeeded: 1,
        failed: 1,
      })
    );
    expect(document.body.textContent).toContain("compile failed");

    // Closing the dialog refreshes the queue and keeps only the failed draft
    // selected, so a retry does not need the selection rebuilt.
    dialogButtonByText(label("common.close"))?.click();
    await flushPromises();
    expect(mocks.listQualitySuggestions).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.selectedCount", { count: 1 })
    );
    wrapper.unmount();
  });

  it("rejects the selected drafts in one batch", async () => {
    const { wrapper } = await mountPage();

    groupCheckbox(0)?.click();
    await flushPromises();
    buttonByText(
      label("dataQuality.suggestions.ignoreDrafts", { count: 2 })
    )?.click();
    await flushPromises();

    dialogButtonByText(label("dataQuality.suggestions.reject"))?.click();
    await flushPromises();

    expect(mocks.batchUpdateQualitySuggestions).toHaveBeenCalledWith([
      { name: DRAFT_ONE, decision: QualitySuggestionStatus.REJECTED, note: "" },
      { name: DRAFT_TWO, decision: QualitySuggestionStatus.REJECTED, note: "" },
    ]);
    expect(mocks.listQualitySuggestions).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("keeps the draft and label bulk actions separate for a mixed selection", async () => {
    const { wrapper } = await mountPage();

    // Group headers and row checkboxes share the DOM order: orders group,
    // draft one, draft two, customers group, label.
    checkboxes()[0]?.click();
    checkboxes()[4]?.click();
    await flushPromises();

    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.selectedCount", { count: 3 })
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.createRules", { count: 2 })
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.ignoreDrafts", { count: 2 })
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.acceptLabels", { count: 1 })
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.rejectLabels", { count: 1 })
    );

    // Rejecting only the labels must leave the selected drafts alone.
    buttonByText(
      label("dataQuality.suggestions.rejectLabels", { count: 1 })
    )?.click();
    await flushPromises();
    dialogButtonByText(label("dataQuality.suggestions.reject"))?.click();
    await flushPromises();

    expect(mocks.batchUpdateQualitySuggestions).toHaveBeenCalledWith([
      { name: LABEL_ONE, decision: QualitySuggestionStatus.REJECTED, note: "" },
    ]);
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.selectedCount", { count: 2 })
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.createRules", { count: 2 })
    );
    expect(wrapper.text()).not.toContain(
      label("dataQuality.suggestions.acceptLabels", { count: 1 })
    );
    wrapper.unmount();
  });

  it("accepts a label from its row", async () => {
    const { wrapper } = await mountPage();

    buttonByText(label("dataQuality.suggestions.accept"))?.click();
    await flushPromises();

    expect(mocks.updateQualitySuggestion).toHaveBeenCalledWith({
      name: LABEL_ONE,
      decision: QualitySuggestionStatus.ACCEPTED,
    });
    expect(mocks.listQualitySuggestions).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("reloads the jobs panel after a submission and selects what a job settles", async () => {
    mocks.listQualitySuggestions.mockResolvedValue({
      items: queue(),
      nextPageToken: "page-2",
    });
    const { wrapper } = await mountPage("/data-quality/suggestions?draft=1");

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.workbench.description")
    );
    // The panel loads once on mount.
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(1);
    // Move off page one so the completion reset is observable.
    buttonByText(label("common.next"))?.click();
    await flushPromises();
    expect(mocks.listQualitySuggestions).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "page-2" })
    );

    const queued = draftSuggestion(
      "workspaces/ws/qualitySuggestions/draft-3",
      "Queued by the job",
      OTHER_TARGET
    );
    const missing = draftSuggestion(
      "workspaces/ws/qualitySuggestions/draft-4",
      "Queued but filtered out",
      OTHER_TARGET
    );
    const info = vi.spyOn(notify, "info");
    const success = vi.spyOn(notify, "success");

    // The workbench only submits a job; the panel reloads immediately.
    wrapper
      .findComponent({ name: "SuggestionWorkbench" })
      .vm.$emit("submitted", { name: "workspaces/ws/qualitySuggestionJobs/j" });
    await flushPromises();
    expect(mocks.listQualitySuggestionJobs).toHaveBeenCalledTimes(2);

    // When the job settles, its suggestions are the newest rows: page one,
    // select the visible pending ones, and explain any that are not visible.
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [queued],
      nextPageToken: "",
    });
    wrapper
      .findComponent({ name: "SuggestionJobsPanel" })
      .vm.$emit("settled", [queued.name, missing.name]);
    await flushPromises();

    expect(mocks.listQualitySuggestions).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "" })
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.selectedCount", { count: 1 })
    );
    expect(info).toHaveBeenCalledWith(
      label("dataQuality.suggestions.queuedPartial", {
        selected: 1,
        total: 2,
      })
    );
    expect(success).toHaveBeenCalledWith(
      label("dataQuality.suggestions.jobs.settledToast", { count: 2 })
    );
    info.mockRestore();
    success.mockRestore();
    wrapper.unmount();
  });

  it("refreshes the current queue when a job settles without suggestions", async () => {
    const { wrapper } = await mountPage();

    wrapper
      .findComponent({ name: "SuggestionJobsPanel" })
      .vm.$emit("settled", []);
    await flushPromises();

    expect(mocks.listQualitySuggestions).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).not.toContain(
      label("dataQuality.suggestions.selectedCount", { count: 1 })
    );
    wrapper.unmount();
  });

  it("opens the workbench from the deep link even when the LLM switch is off", async () => {
    mocks.getWorkspaceProfileSetting.mockResolvedValue({
      dataQuality: { enableLlmAnalysis: false },
    });
    const { wrapper } = await mountPage("/data-quality/suggestions?draft=1");
    await flushPromises();

    // The deep link is never a dead end: the workbench explains why drafting is
    // unavailable and points at the settings.
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.workbench.description")
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.llm.disabledHint")
    );
    const generate = [
      ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
    ].find((button) =>
      button.textContent?.includes(
        label("dataQuality.suggestions.workbench.submit")
      )
    ) as HTMLButtonElement | undefined;
    expect(generate?.disabled).toBe(true);
    wrapper.unmount();
  });

  it("opens the detail drawer and decides a label there", async () => {
    const { wrapper } = await mountPage();

    reviewButton(2)?.click();
    await flushPromises();
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.detailTitle")
    );

    dialogButtonByText(label("dataQuality.suggestions.reject"))?.click();
    await flushPromises();
    expect(mocks.updateQualitySuggestion).toHaveBeenCalledWith(
      expect.objectContaining({
        name: LABEL_ONE,
        decision: QualitySuggestionStatus.REJECTED,
      })
    );
    wrapper.unmount();
  });

  it("explains an empty pending queue and lets the reader widen the filter", async () => {
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.pendingEmptyTitle")
    );

    buttonByText(label("dataQuality.suggestions.viewAllStatuses"))?.click();
    await flushPromises();

    expect(mocks.listQualitySuggestions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        status: QualitySuggestionStatus.UNSPECIFIED,
      })
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.noFiltered")
    );

    buttonByText(label("common.filter.clear"))?.click();
    await flushPromises();
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.pendingEmptyTitle")
    );
    wrapper.unmount();
  });

  it("shows a filtered empty state with a clear action", async () => {
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    const { wrapper } = await mountPage();

    // Widening to every status is a non-default filter; clearing it returns to
    // the page's own pending view.
    buttonByText(label("dataQuality.suggestions.viewAllStatuses"))?.click();
    await flushPromises();
    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.noFiltered")
    );

    buttonByText(label("common.filter.clear"))?.click();
    await flushPromises();
    expect(mocks.listQualitySuggestions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        status: QualitySuggestionStatus.PENDING,
      })
    );
    wrapper.unmount();
  });
});
