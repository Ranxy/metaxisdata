import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  QualityTargetSchema,
  SemanticLabelSuggestionSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import BatchDecideSuggestionsDialog from "./BatchDecideSuggestionsDialog.vue";

const mocks = vi.hoisted(() => ({
  batchUpdateQualitySuggestions: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  batchUpdateQualitySuggestions: mocks.batchUpdateQualitySuggestions,
}));

const TABLE = "inst1;db1;public;customers";
const LABEL = "workspaces/ws/qualitySuggestions/label-1";
const DRAFT = "workspaces/ws/qualitySuggestions/draft-1";

function labelSuggestion() {
  return create(QualitySuggestionSchema, {
    name: LABEL,
    kind: QualitySuggestionKind.LABEL,
    status: QualitySuggestionStatus.PENDING,
    source: QualitySuggestionSource.PROFILE,
    target: create(QualityTargetSchema, {
      metaGuid: TABLE,
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

function draftSuggestion() {
  return create(QualitySuggestionSchema, {
    name: DRAFT,
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
        value: { title: "Orders should have rows", severity: 2 },
      },
    },
  });
}

function mountDialog(
  suggestions = [labelSuggestion(), draftSuggestion()],
  decision = QualitySuggestionStatus.ACCEPTED
) {
  const pinia = createPinia();
  setActivePinia(pinia);
  return mount(BatchDecideSuggestionsDialog, {
    attachTo: document.body,
    props: { open: true, suggestions, decision },
    global: { plugins: [i18n, pinia] },
  });
}

function buttonByText(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.includes(text));
}

function setNote(value: string) {
  const element = document.getElementById(
    "dq-batch-decide-note"
  ) as HTMLTextAreaElement;
  Object.getOwnPropertyDescriptor(
    HTMLTextAreaElement.prototype,
    "value"
  )?.set?.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

describe("BatchDecideSuggestionsDialog", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.batchUpdateQualitySuggestions.mockResolvedValue([
      { suggestion: labelSuggestion(), error: "" },
      { suggestion: undefined, error: "suggestion already decided" },
    ]);
  });

  it("accepts labels with a note and reports the per-item failure", async () => {
    const wrapper = await mountDialog();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.batchDecide.acceptLabelsHint")
    );

    setNote("Looks like an email column");
    await flushPromises();
    buttonByText(label("dataQuality.suggestions.accept"))?.click();
    await flushPromises();

    expect(mocks.batchUpdateQualitySuggestions).toHaveBeenCalledWith([
      {
        name: LABEL,
        decision: QualitySuggestionStatus.ACCEPTED,
        note: "Looks like an email column",
      },
      {
        name: DRAFT,
        decision: QualitySuggestionStatus.ACCEPTED,
        note: "Looks like an email column",
      },
    ]);
    expect(document.body.textContent).toContain("suggestion already decided");
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.batchDecide.partial", {
        succeeded: 1,
        failed: 1,
      })
    );
    // The page refreshes for the suggestion that was actually decided.
    expect(wrapper.emitted("done")).toContainEqual([[LABEL]]);
    wrapper.unmount();
  });

  it("rejects in bulk and closes after every item succeeded", async () => {
    mocks.batchUpdateQualitySuggestions.mockResolvedValue([
      { suggestion: labelSuggestion(), error: "" },
      { suggestion: draftSuggestion(), error: "" },
    ]);
    const wrapper = await mountDialog(
      [labelSuggestion(), draftSuggestion()],
      QualitySuggestionStatus.REJECTED
    );
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.batchDecide.rejectHint")
    );

    buttonByText(label("dataQuality.suggestions.reject"))?.click();
    await flushPromises();

    expect(mocks.batchUpdateQualitySuggestions).toHaveBeenCalledWith([
      { name: LABEL, decision: QualitySuggestionStatus.REJECTED, note: "" },
      { name: DRAFT, decision: QualitySuggestionStatus.REJECTED, note: "" },
    ]);
    expect(wrapper.emitted("done")).toContainEqual([[LABEL, DRAFT]]);
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.batchDecide.success", { count: 2 })
    );

    buttonByText(label("common.close"))?.click();
    await flushPromises();
    expect(wrapper.emitted("update:open")).toContainEqual([false]);
    wrapper.unmount();
  });
});
