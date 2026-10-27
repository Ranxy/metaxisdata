import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import RuleConfigForm from "@/components/data-quality/RuleConfigForm.vue";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityDimension,
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
import BatchCreateRulesDialog from "./BatchCreateRulesDialog.vue";

// Radix opens a select on pointerdown and calls pointer-capture APIs that jsdom
// does not implement.
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.setPointerCapture ??= () => {};
Element.prototype.releasePointerCapture ??= () => {};

const mocks = vi.hoisted(() => ({
  batchCreateQualityRules: vi.fn(),
  listAllMetadata: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  batchCreateQualityRules: mocks.batchCreateQualityRules,
}));

vi.mock("@/api/database", () => ({
  listAllMetadata: mocks.listAllMetadata,
}));

const TABLE = "inst1;db1;public;orders";
const DRAFT_ONE = "workspaces/ws/qualitySuggestions/draft-1";
const DRAFT_TWO = "workspaces/ws/qualitySuggestions/draft-2";

function draftSuggestion(name: string, title: string) {
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

function labelSuggestion() {
  return create(QualitySuggestionSchema, {
    name: "workspaces/ws/qualitySuggestions/label-1",
    kind: QualitySuggestionKind.LABEL,
    status: QualitySuggestionStatus.PENDING,
    target: create(QualityTargetSchema, {
      metaGuid: TABLE,
      metaType: MetaType.TABLE,
    }),
    payload: {
      payload: {
        case: "label",
        value: create(SemanticLabelSuggestionSchema, { column: "email" }),
      },
    },
  });
}

async function mountDialog(
  suggestions = [
    draftSuggestion(DRAFT_ONE, "Orders should have rows"),
    draftSuggestion(DRAFT_TWO, "Orders must stay fresh"),
  ]
) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: [] });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/data-quality/suggestions",
        name: "DataQualitySuggestions",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/data-quality/suggestions");
  await router.isReady();
  return mount(BatchCreateRulesDialog, {
    attachTo: document.body,
    props: { open: true, suggestions },
    global: { plugins: [i18n, pinia, router] },
  });
}

function buttonByText(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.includes(text));
}

function setInput(selector: string, value: string) {
  const element = document.querySelector(selector) as HTMLInputElement;
  Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value"
  )?.set?.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

describe("BatchCreateRulesDialog", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.listAllMetadata.mockResolvedValue([
      {
        guid: `${TABLE};id`,
        type: {
          case: "columnMetadata",
          value: { name: "id", type: "bigint", nullable: false },
        },
      },
      {
        guid: `${TABLE};amount`,
        type: {
          case: "columnMetadata",
          value: { name: "amount", type: "numeric", nullable: true },
        },
      },
    ]);
    mocks.batchCreateQualityRules.mockResolvedValue([
      {
        rule: create(QualityRuleSchema, {
          name: "workspaces/ws/qualityRules/rule-1",
          title: "Orders should have rows",
          target: create(QualityTargetSchema, {
            metaGuid: TABLE,
            metaType: MetaType.TABLE,
          }),
        }),
        error: "",
        suggestion: DRAFT_ONE,
      },
      { rule: undefined, error: "compile failed", suggestion: DRAFT_TWO },
    ]);
  });

  it("sends the common overrides and per-row edits in one batch", async () => {
    const wrapper = await mountDialog();
    await flushPromises();

    // Expand the first row and rename it.
    const expand = buttonByText(
      label("dataQuality.suggestions.batchCreate.expand")
    );
    expect(expand?.getAttribute("aria-expanded")).toBe("false");
    expand?.click();
    await flushPromises();
    expect(
      buttonByText(
        label("dataQuality.suggestions.batchCreate.collapse")
      )?.getAttribute("aria-expanded")
    ).toBe("true");
    // The whole column list reaches the row's config form, not one page of it.
    expect(mocks.listAllMetadata).toHaveBeenCalledWith({
      parentGuid: TABLE,
      metaType: MetaType.COLUMN,
    });
    expect(
      wrapper.findAllComponents(RuleConfigForm)[0]?.props("columnDetails")
    ).toEqual([
      { name: "id", type: "bigint", nullable: false },
      { name: "amount", type: "numeric", nullable: true },
    ]);
    setInput("#dq-batch-title-0", "Renamed rule");
    await flushPromises();

    // Common severity: applied to every row, overriding the draft's own value.
    const severitySelect = wrapper
      .findAllComponents(Select)
      .find((candidate) => candidate.find("#dq-batch-severity").exists());
    expect(severitySelect).toBeTruthy();
    severitySelect?.vm.$emit("update:modelValue", "1");
    await nextTick();

    // Common dimension, applied to every row like the severity.
    const dimensionSelect = wrapper
      .findAllComponents(Select)
      .find((candidate) => candidate.find("#dq-batch-dimension").exists());
    expect(dimensionSelect).toBeTruthy();
    dimensionSelect?.vm.$emit(
      "update:modelValue",
      String(QualityDimension.ACCURACY)
    );
    await nextTick();
    await flushPromises();

    buttonByText(
      label("dataQuality.suggestions.batchCreate.submit", { count: 2 })
    )?.click();
    await flushPromises();

    expect(mocks.batchCreateQualityRules).toHaveBeenCalledTimes(1);
    const items = mocks.batchCreateQualityRules.mock.calls[0]?.[0] as Array<{
      rule: {
        title: string;
        severity: number;
        dimension: number;
        owner: string;
        scheduleInterval?: unknown;
        target?: { metaGuid?: string };
      };
      suggestion: string;
    }>;
    expect(items).toHaveLength(2);
    expect(items.map((item) => item.suggestion)).toEqual([
      DRAFT_ONE,
      DRAFT_TWO,
    ]);
    // The row edit wins for its own title; the target always comes from the
    // suggestion.
    expect(items[0]?.rule.title).toBe("Renamed rule");
    expect(items[1]?.rule.title).toBe("Orders must stay fresh");
    expect(items[0]?.rule.target?.metaGuid).toBe(TABLE);
    // The common severity applies (1 = WARNING) and the interval stays unset so
    // the server applies the workspace default.
    expect(items[0]?.rule.severity).toBe(1);
    expect(items[1]?.rule.severity).toBe(1);
    expect(items[0]?.rule.scheduleInterval).toBeUndefined();
    expect(items[0]?.rule.owner).toBe("");
    expect(items[0]?.rule.dimension).toBe(QualityDimension.ACCURACY);
    expect(items[1]?.rule.dimension).toBe(QualityDimension.ACCURACY);

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.batchCreate.partial", {
        succeeded: 1,
        failed: 1,
      })
    );
    expect(document.body.textContent).toContain("compile failed");
    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.batchCreate.createdRules")
    );

    buttonByText(label("common.close"))?.click();
    await flushPromises();
    expect(wrapper.emitted("done")).toContainEqual([[DRAFT_ONE]]);
    wrapper.unmount();
  });

  it("creates the convertible rows when one draft cannot become a rule", async () => {
    mocks.batchCreateQualityRules.mockResolvedValue([
      {
        rule: create(QualityRuleSchema, {
          name: "workspaces/ws/qualityRules/rule-1",
          title: "Orders should have rows",
        }),
        error: "",
        suggestion: DRAFT_ONE,
      },
    ]);
    const wrapper = await mountDialog([
      draftSuggestion(DRAFT_ONE, "Orders should have rows"),
      labelSuggestion(),
    ]);
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.suggestions.batchCreate.unsupported", { count: 1 })
    );

    buttonByText(
      label("dataQuality.suggestions.batchCreate.submit", { count: 1 })
    )?.click();
    await flushPromises();

    const items = mocks.batchCreateQualityRules.mock.calls[0]?.[0] as Array<{
      suggestion: string;
    }>;
    expect(items).toHaveLength(1);
    expect(items[0]?.suggestion).toBe(DRAFT_ONE);

    buttonByText(label("common.close"))?.click();
    await flushPromises();
    expect(wrapper.emitted("done")).toContainEqual([[DRAFT_ONE]]);
    wrapper.unmount();
  });

  it("keeps the draft's severity and interval when the common settings say so", async () => {
    const wrapper = await mountDialog([draftSuggestion(DRAFT_ONE, "Only one")]);
    await flushPromises();

    buttonByText(
      label("dataQuality.suggestions.batchCreate.submit", { count: 1 })
    )?.click();
    await flushPromises();

    const items = mocks.batchCreateQualityRules.mock.calls[0]?.[0] as Array<{
      rule: { severity: number; scheduleInterval?: unknown };
    }>;
    expect(items[0]?.rule.severity).toBe(2);
    expect(items[0]?.rule.scheduleInterval).toBeUndefined();
    wrapper.unmount();
  });

  it("links a created rule to its target workspace with the check selected", async () => {
    const wrapper = await mountDialog();
    await flushPromises();

    buttonByText(
      label("dataQuality.suggestions.batchCreate.submit", { count: 2 })
    )?.click();
    await flushPromises();

    const link = document.body.querySelector<HTMLAnchorElement>(
      'a[href*="/data-quality/targets/"]'
    );
    expect(link).toBeTruthy();
    const href = decodeURIComponent(link?.getAttribute("href") ?? "");
    expect(href).toContain("/data-quality/targets/inst1/db1/public/orders");
    expect(href).toContain("metaType=4");
    expect(href).toContain("check=workspaces/ws/qualityRules/rule-1");
    wrapper.unmount();
  });
});
