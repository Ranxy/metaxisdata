import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
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
  listMetadata: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  batchCreateQualityRules: mocks.batchCreateQualityRules,
}));

vi.mock("@/api/database", () => ({
  listMetadata: mocks.listMetadata,
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
        path: "/data-quality/rules/:ruleId+/detail",
        name: "DataQualityRuleDetail",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/data-quality/rules/a/detail");
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
    mocks.listMetadata.mockResolvedValue({
      typesStoredMetadata: [
        {
          metaType: MetaType.COLUMN,
          list: [
            {
              guid: `${TABLE};id`,
              type: {
                case: "columnMetadata",
                value: { name: "id", type: "bigint", nullable: false },
              },
            },
          ],
        },
      ],
    });
    mocks.batchCreateQualityRules.mockResolvedValue([
      {
        rule: create(QualityRuleSchema, {
          name: "workspaces/ws/qualityRules/rule-1",
          title: "Orders should have rows",
        }),
        error: "",
      },
      { rule: undefined, error: "compile failed" },
    ]);
  });

  it("sends the common overrides and per-row edits in one batch", async () => {
    const wrapper = await mountDialog();
    await flushPromises();

    // Expand the first row and rename it.
    buttonByText(label("dataQuality.suggestions.batchCreate.expand"))?.click();
    await flushPromises();
    setInput("#dq-batch-title-0", "Renamed rule");
    await flushPromises();

    // Common severity: every row that was not edited by hand gets it.
    const severityTrigger = document.querySelector(
      "#dq-batch-severity"
    ) as HTMLElement | null;
    expect(severityTrigger).toBeTruthy();
    severityTrigger?.dispatchEvent(
      new PointerEvent("pointerdown", { bubbles: true })
    );
    await nextTick();
    await nextTick();
    const errorOption = [
      ...document.body.querySelectorAll<HTMLElement>('[role="option"]'),
    ].find((option) => option.textContent?.includes("Error"));
    expect(errorOption).toBeTruthy();
    errorOption?.click();
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
    // The common severity applies (2 = ERROR) and the interval stays unset so
    // the server applies the workspace default.
    expect(items[0]?.rule.severity).toBe(2);
    expect(items[1]?.rule.severity).toBe(2);
    expect(items[0]?.rule.scheduleInterval).toBeUndefined();
    expect(items[0]?.rule.owner).toBe("");

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
});
