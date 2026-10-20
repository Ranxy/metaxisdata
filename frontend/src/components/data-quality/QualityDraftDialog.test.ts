import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { i18n } from "@/locales";
import {
  QualityRuleConfigSchema,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionStatus,
  QualityTargetSchema,
  RowCountConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import QualityDraftDialog from "./QualityDraftDialog.vue";

const mocks = vi.hoisted(() => ({
  suggestQualityRule: vi.fn(),
  updateQualitySuggestion: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  suggestQualityRule: mocks.suggestQualityRule,
  updateQualitySuggestion: mocks.updateQualitySuggestion,
}));

const TABLE = "inst1;db1;public;orders";
const SUGGESTION = "workspaces/ws/qualitySuggestions/draft-1";

function draftSuggestion() {
  return create(QualitySuggestionSchema, {
    name: SUGGESTION,
    kind: QualitySuggestionKind.RULE_DRAFT,
    status: QualitySuggestionStatus.PENDING,
    target: create(QualityTargetSchema, {
      metaGuid: TABLE,
      metaType: MetaType.TABLE,
    }),
    payload: {
      payload: {
        case: "rule",
        value: {
          title: "Orders should have rows",
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

function mountDialog() {
  return mount(QualityDraftDialog, {
    attachTo: document.body,
    props: {
      open: true,
      tableGuid: TABLE,
      metaType: MetaType.TABLE,
      columns: ["id", "amount"],
    },
    global: { plugins: [i18n] },
  });
}

/** Dialog content is teleported, so the page wrapper cannot find it. */
function setIntent(value: string) {
  const element = document.getElementById(
    "dq-ai-intent"
  ) as HTMLTextAreaElement;
  Object.getOwnPropertyDescriptor(
    HTMLTextAreaElement.prototype,
    "value"
  )?.set?.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

function buttonByText(text: string): HTMLElement | undefined {
  return [...document.body.querySelectorAll("button")].find((button) =>
    button.textContent?.includes(text)
  ) as HTMLElement | undefined;
}

describe("QualityDraftDialog", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.suggestQualityRule.mockResolvedValue([draftSuggestion()]);
    mocks.updateQualitySuggestion.mockResolvedValue(
      create(QualitySuggestionSchema, { name: SUGGESTION })
    );
  });

  it("submits the intent and the selected columns", async () => {
    const wrapper = mountDialog();
    await flushPromises();

    setIntent("Orders must have rows");
    await nextTick();
    buttonByText(i18n.global.t("dataQuality.llm.draft.generate"))?.click();
    await flushPromises();

    expect(mocks.suggestQualityRule).toHaveBeenCalledWith({
      tableGuid: TABLE,
      metaType: MetaType.TABLE,
      columns: ["id", "amount"],
      intent: "Orders must have rows",
      language: "en-US",
    });
    expect(document.body.textContent).toContain("Orders should have rows");
    expect(document.body.textContent).toContain(
      i18n.global.t("dataQuality.ruleType.rowCount")
    );
    wrapper.unmount();
  });

  it("opens a draft in the editor without accepting it", async () => {
    const wrapper = mountDialog();
    await flushPromises();
    setIntent("Orders must have rows");
    await nextTick();
    buttonByText(i18n.global.t("dataQuality.llm.draft.generate"))?.click();
    await flushPromises();

    buttonByText(i18n.global.t("dataQuality.llm.draft.openInEditor"))?.click();
    await flushPromises();

    expect(mocks.updateQualitySuggestion).not.toHaveBeenCalled();
    expect(wrapper.emitted("accepted")).toHaveLength(1);
    expect(wrapper.emitted("update:open")).toContainEqual([false]);
    wrapper.unmount();
  });

  it("rejects a draft and drops it from the list", async () => {
    const wrapper = mountDialog();
    await flushPromises();
    setIntent("Orders must have rows");
    await nextTick();
    buttonByText(i18n.global.t("dataQuality.llm.draft.generate"))?.click();
    await flushPromises();

    buttonByText(i18n.global.t("dataQuality.suggestions.reject"))?.click();
    await flushPromises();

    expect(mocks.updateQualitySuggestion).toHaveBeenCalledWith(
      expect.objectContaining({
        name: SUGGESTION,
        decision: QualitySuggestionStatus.REJECTED,
      })
    );
    expect(document.body.textContent).not.toContain("Orders should have rows");
    wrapper.unmount();
  });

  it("shows a friendly failure when the model is not configured", async () => {
    const { ConnectError, Code } = await import("@connectrpc/connect");
    mocks.suggestQualityRule.mockRejectedValue(
      new ConnectError("LLM analysis is disabled", Code.FailedPrecondition)
    );
    const wrapper = mountDialog();
    await flushPromises();
    setIntent("Orders must have rows");
    await nextTick();
    buttonByText(i18n.global.t("dataQuality.llm.draft.generate"))?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      i18n.global.t("dataQuality.llm.errors.notConfigured")
    );
    wrapper.unmount();
  });
});
