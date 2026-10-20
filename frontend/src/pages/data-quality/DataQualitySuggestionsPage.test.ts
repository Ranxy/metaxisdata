import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityRuleConfigSchema,
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
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualitySuggestions: mocks.listQualitySuggestions,
  updateQualitySuggestion: mocks.updateQualitySuggestion,
}));

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.dataQuality.suggest",
];

const RULE_SUGGESTION = "workspaces/ws/qualitySuggestions/draft-1";
const LABEL_SUGGESTION = "workspaces/ws/qualitySuggestions/label-1";
const Dummy = { template: "<div />" };

function draftSuggestion() {
  return create(QualitySuggestionSchema, {
    name: RULE_SUGGESTION,
    kind: QualitySuggestionKind.RULE_DRAFT,
    source: QualitySuggestionSource.NATURAL_LANGUAGE,
    status: QualitySuggestionStatus.PENDING,
    provider: "openai",
    model: "gpt-test",
    target: create(QualityTargetSchema, {
      metaGuid: "inst1;db1;public;orders",
      metaType: MetaType.TABLE,
    }),
    payload: {
      payload: {
        case: "rule",
        value: {
          title: "Orders should have rows",
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

function labelSuggestion() {
  return create(QualitySuggestionSchema, {
    name: LABEL_SUGGESTION,
    kind: QualitySuggestionKind.LABEL,
    source: QualitySuggestionSource.PROFILE,
    status: QualitySuggestionStatus.PENDING,
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

async function mountPage() {
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
        path: "/data-quality/rules",
        name: "DataQualityRules",
        component: Dummy,
      },
    ],
  });
  await router.push("/data-quality/suggestions");
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
  return [...document.body.querySelectorAll("button")].find((button) =>
    button.textContent?.includes(text)
  ) as HTMLElement | undefined;
}

function reviewButton(index = 0): HTMLElement | undefined {
  return document.body.querySelectorAll<HTMLElement>('[aria-label="Review"]')[
    index
  ];
}

describe("DataQualitySuggestionsPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [draftSuggestion(), labelSuggestion()],
      nextPageToken: "",
    });
    mocks.updateQualitySuggestion.mockImplementation(async (options) =>
      create(QualitySuggestionSchema, { name: options.name })
    );
  });

  it("lists the queue with its kinds, sources and states", async () => {
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain("Orders should have rows");
    expect(wrapper.text()).toContain("email · Column label");
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.suggestionStatus.pending")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.suggestionKind.ruleDraft")
    );
    wrapper.unmount();
  });

  it("opens a rule draft in the editor without accepting it", async () => {
    const { wrapper, router } = await mountPage();
    reviewButton(0)?.click();
    await flushPromises();

    const open = buttonByText(
      i18n.global.t("dataQuality.suggestions.openInEditor")
    );
    expect(open).toBeTruthy();
    open?.click();
    await flushPromises();

    // Reviewing a draft is not a decision: the suggestion stays pending until
    // the editor creates a rule from it.
    expect(mocks.updateQualitySuggestion).not.toHaveBeenCalled();
    expect(router.currentRoute.value.name).toBe("DataQualityRuleNew");
    expect(router.currentRoute.value.query.suggestion).toBe(RULE_SUGGESTION);
    wrapper.unmount();
  });

  it("rejects a label suggestion and refreshes the queue", async () => {
    const { wrapper } = await mountPage();
    reviewButton(1)?.click();
    await flushPromises();

    const reject = buttonByText(
      i18n.global.t("dataQuality.suggestions.reject")
    );
    expect(reject).toBeTruthy();
    reject?.click();
    await flushPromises();

    expect(mocks.updateQualitySuggestion).toHaveBeenCalledWith(
      expect.objectContaining({
        name: LABEL_SUGGESTION,
        decision: QualitySuggestionStatus.REJECTED,
      })
    );
    expect(mocks.listQualitySuggestions).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });
});
