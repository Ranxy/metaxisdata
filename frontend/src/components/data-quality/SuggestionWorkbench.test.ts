import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityRuleConfigSchema,
  QualitySuggestionJobSchema,
  QualitySuggestionJobStatus,
  QualitySuggestionJobTargetSchema,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  QualityTargetSchema,
  RowCountConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import SuggestionWorkbench from "./SuggestionWorkbench.vue";

const mocks = vi.hoisted(() => ({
  suggestQualityRule: vi.fn(),
  batchUpdateQualitySuggestions: vi.fn(),
  createQualitySuggestionJob: vi.fn(),
  listMetadata: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  suggestQualityRule: mocks.suggestQualityRule,
  batchUpdateQualitySuggestions: mocks.batchUpdateQualitySuggestions,
  createQualitySuggestionJob: mocks.createQualitySuggestionJob,
}));

vi.mock("@/api/database", () => ({
  listMetadata: mocks.listMetadata,
}));

const PERMISSIONS = ["metaxisdata.dataQuality.suggest"];
const TABLE = "inst1;db1;public;orders";
const OTHER_TABLE = "inst1;db1;public;customers";
const SUGGESTION = "workspaces/ws/qualitySuggestions/draft-1";

function draftSuggestion(name = SUGGESTION, title = "Orders should have rows") {
  return create(QualitySuggestionSchema, {
    name,
    kind: QualitySuggestionKind.RULE_DRAFT,
    status: QualitySuggestionStatus.PENDING,
    source: QualitySuggestionSource.NATURAL_LANGUAGE,
    target: create(QualityTargetSchema, {
      metaGuid: TABLE,
      metaType: MetaType.TABLE,
    }),
    payload: {
      payload: {
        case: "rule",
        value: {
          title,
          rationale: "The fact table.",
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

function jobFixture(targets: string[] = [TABLE, OTHER_TABLE]) {
  return create(QualitySuggestionJobSchema, {
    name: "workspaces/ws/qualitySuggestionJobs/job-1",
    status: QualitySuggestionJobStatus.PENDING,
    intent: "Keep the tables healthy",
    language: "en-US",
    targetCount: targets.length,
    completedTargetCount: 0,
    suggestionCount: 0,
    targets: targets.map((guid) =>
      create(QualitySuggestionJobTargetSchema, {
        target: create(QualityTargetSchema, {
          metaGuid: guid,
          metaType: MetaType.TABLE,
          path: {
            instance: "inst1",
            database: "db1",
            schema: "public",
            object: guid.split(";")[3],
          },
        }),
      })
    ),
  });
}

async function mountWorkbench(
  options: {
    fixedTarget?: {
      tableGuid: string;
      metaType: MetaType;
      columns: string[];
    };
    llmEnabled?: boolean;
  } = {}
) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/data-quality/suggestions",
        name: "DataQualitySuggestions",
        component: { template: "<div />" },
      },
      {
        path: "/settings/data-quality",
        name: "DataQualitySettings",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/data-quality/suggestions");
  await router.isReady();
  return mount(SuggestionWorkbench, {
    attachTo: document.body,
    props: {
      open: true,
      llmEnabled: options.llmEnabled ?? true,
      fixedTarget: options.fixedTarget,
    },
    global: { plugins: [i18n, pinia, router] },
  });
}

function setIntent(value: string) {
  const element = document.getElementById(
    "dq-workbench-intent"
  ) as HTMLTextAreaElement;
  Object.getOwnPropertyDescriptor(
    HTMLTextAreaElement.prototype,
    "value"
  )?.set?.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

function buttonByText(text: string): HTMLElement | undefined {
  return [...document.body.querySelectorAll<HTMLElement>("button")].find(
    (button) => button.textContent?.includes(text)
  );
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function pickerSelection(guid: string, object: string) {
  return {
    guid,
    metaType: MetaType.TABLE,
    path: {
      instance: "inst1",
      database: "db1",
      schema: "public",
      object,
    },
    columns: [],
  };
}

describe("SuggestionWorkbench", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.suggestQualityRule.mockResolvedValue([draftSuggestion()]);
    mocks.batchUpdateQualitySuggestions.mockResolvedValue([
      { suggestion: draftSuggestion(), error: "", name: SUGGESTION },
    ]);
    mocks.createQualitySuggestionJob.mockResolvedValue(jobFixture());
    mocks.listMetadata.mockResolvedValue({
      typesStoredMetadata: [
        {
          metaType: MetaType.COLUMN,
          list: [
            {
              guid: `${TABLE};id`,
              type: { case: "columnMetadata", value: { name: "id" } },
            },
            {
              guid: `${TABLE};amount`,
              type: { case: "columnMetadata", value: { name: "amount" } },
            },
          ],
        },
      ],
    });
  });

  it("page mode submits one durable job instead of looping in the browser", async () => {
    const wrapper = await mountWorkbench();
    await flushPromises();

    wrapper
      .findComponent({ name: "TargetPicker" })
      .vm.$emit("selectMany", [
        pickerSelection(TABLE, "orders"),
        pickerSelection(OTHER_TABLE, "customers"),
      ]);
    await flushPromises();

    setIntent("Keep the tables healthy");
    await flushPromises();
    buttonByText(label("dataQuality.suggestions.workbench.submit"))?.click();
    await flushPromises();

    expect(mocks.suggestQualityRule).not.toHaveBeenCalled();
    expect(mocks.createQualitySuggestionJob).toHaveBeenCalledTimes(1);
    const request = mocks.createQualitySuggestionJob.mock.calls[0]?.[0] as {
      intent: string;
      language: string;
      targets: Array<{ metaGuid: string; columns: string[] }>;
    };
    expect(request.intent).toBe("Keep the tables healthy");
    expect(request.language).toBe("en-US");
    // Several targets cannot share one column checklist.
    expect(request.targets.map((target) => target.metaGuid)).toEqual([
      TABLE,
      OTHER_TABLE,
    ]);
    expect(request.targets.every((target) => target.columns.length === 0)).toBe(
      true
    );

    // Nothing is in flight locally, so the dialog only confirms and may close.
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.workbench.submittedTitle")
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.workbench.submittedHint")
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.workbench.submittedTargets", { count: 2 })
    );
    const submitted = wrapper.emitted("submitted");
    expect(submitted).toHaveLength(1);
    expect((submitted?.[0]?.[0] as { name?: string } | undefined)?.name).toBe(
      "workspaces/ws/qualitySuggestionJobs/job-1"
    );

    buttonByText(label("dataQuality.suggestions.workbench.finish"))?.click();
    await flushPromises();
    expect(wrapper.emitted("update:open")).toContainEqual([false]);
    wrapper.unmount();
  });

  it("page mode sends the selected columns for a single target", async () => {
    const wrapper = await mountWorkbench();
    await flushPromises();

    wrapper
      .findComponent({ name: "TargetPicker" })
      .vm.$emit("select", pickerSelection(TABLE, "orders"));
    await flushPromises();

    setIntent("Orders must have rows");
    await flushPromises();
    buttonByText(label("dataQuality.suggestions.workbench.submit"))?.click();
    await flushPromises();

    const request = mocks.createQualitySuggestionJob.mock.calls[0]?.[0] as {
      targets: Array<{ metaGuid: string; columns: string[] }>;
    };
    expect(request.targets[0]?.metaGuid).toBe(TABLE);
    expect(request.targets[0]?.columns).toEqual(["id", "amount"]);
    wrapper.unmount();
  });

  it("page mode reports a failed submission and emits nothing", async () => {
    const { ConnectError, Code } = await import("@connectrpc/connect");
    mocks.createQualitySuggestionJob.mockRejectedValue(
      new ConnectError("quota exceeded", Code.ResourceExhausted)
    );
    const wrapper = await mountWorkbench();
    await flushPromises();

    wrapper
      .findComponent({ name: "TargetPicker" })
      .vm.$emit("select", pickerSelection(TABLE, "orders"));
    setIntent("Orders must have rows");
    await flushPromises();
    buttonByText(label("dataQuality.suggestions.workbench.submit"))?.click();
    await flushPromises();

    expect(wrapper.emitted("submitted")).toBeUndefined();
    expect(document.body.textContent).toContain(
      label("dataQuality.llm.errors.budget")
    );
    wrapper.unmount();
  });

  it("drafts once for a fixed editor target and prefills a result", async () => {
    const wrapper = await mountWorkbench({
      fixedTarget: {
        tableGuid: TABLE,
        metaType: MetaType.TABLE,
        columns: ["id", "amount"],
      },
    });
    await flushPromises();

    setIntent("Orders must have rows");
    await flushPromises();
    buttonByText(label("dataQuality.llm.draft.generate"))?.click();
    await flushPromises();

    expect(mocks.createQualitySuggestionJob).not.toHaveBeenCalled();
    expect(mocks.suggestQualityRule).toHaveBeenCalledWith({
      tableGuid: TABLE,
      metaType: MetaType.TABLE,
      columns: ["id", "amount"],
      intent: "Orders must have rows",
      language: "en-US",
    });
    expect(document.body.textContent).toContain("Orders should have rows");

    buttonByText(label("dataQuality.llm.draft.openInEditor"))?.click();
    await flushPromises();

    expect(wrapper.emitted("accepted")).toHaveLength(1);
    expect(wrapper.emitted("update:open")).toContainEqual([false]);
    wrapper.unmount();
  });

  it("rejects an editor-mode result instead of creating anything", async () => {
    const wrapper = await mountWorkbench({
      fixedTarget: {
        tableGuid: TABLE,
        metaType: MetaType.TABLE,
        columns: ["id"],
      },
    });
    await flushPromises();
    setIntent("Orders must have rows");
    await flushPromises();
    buttonByText(label("dataQuality.llm.draft.generate"))?.click();
    await flushPromises();

    buttonByText(label("dataQuality.suggestions.reject"))?.click();
    await flushPromises();

    expect(mocks.batchUpdateQualitySuggestions).toHaveBeenCalledWith([
      { name: SUGGESTION, decision: QualitySuggestionStatus.REJECTED },
    ]);
    expect(document.body.textContent).not.toContain("Orders should have rows");
    wrapper.unmount();
  });

  it("disables drafting when the workspace switch is off", async () => {
    const wrapper = await mountWorkbench({ llmEnabled: false });
    await flushPromises();

    setIntent("Orders must have rows");
    await flushPromises();
    const generate = buttonByText(
      label("dataQuality.suggestions.workbench.submit")
    ) as HTMLButtonElement | undefined;
    expect(generate?.disabled).toBe(true);
    expect(document.body.textContent).toContain(
      label("dataQuality.llm.disabledHint")
    );
    wrapper.unmount();
  });
});
