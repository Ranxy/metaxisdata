import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import TargetDraftCard from "@/components/data-quality/TargetDraftCard.vue";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  NullPercentConfigSchema,
  QualityIssueSchema,
  QualityIssueStatus,
  type QualityRule,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityStatus,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
  RowCountConfigSchema,
  UniqueCombinationConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityTargetPage from "./DataQualityTargetPage.vue";

const mocks = vi.hoisted(() => ({
  listQualityRules: vi.fn(),
  listQualitySuggestions: vi.fn(),
  listQualityIssues: vi.fn(),
  runQualityRules: vi.fn(),
  createQualityRule: vi.fn(),
  updateQualitySuggestion: vi.fn(),
  suggestQualityRule: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityRules: mocks.listQualityRules,
  listQualitySuggestions: mocks.listQualitySuggestions,
  listQualityIssues: mocks.listQualityIssues,
  runQualityRules: mocks.runQualityRules,
  createQualityRule: mocks.createQualityRule,
  updateQualitySuggestion: mocks.updateQualitySuggestion,
  suggestQualityRule: mocks.suggestQualityRule,
}));

const TARGET = "inst1;db1;public;orders";
const RULE_DATASET = "workspaces/ws/qualityRules/rule-dataset";
const RULE_MULTI = "workspaces/ws/qualityRules/rule-multi";
const RULE_COLUMN = "workspaces/ws/qualityRules/rule-column";
const RULE_CREATED = "workspaces/ws/qualityRules/rule-created";
const DRAFT_EMAIL = "workspaces/ws/qualitySuggestions/draft-email";
const DRAFT_AMOUNT = "workspaces/ws/qualitySuggestions/draft-amount";
const Dummy = { template: "<div />" };

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.get",
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.dataQuality.rules.update",
  "metaxisdata.dataQuality.rules.run",
  "metaxisdata.dataQuality.suggest",
  "metaxisdata.dataQuality.issues.list",
  "metaxisdata.dataQuality.issues.get",
];

function target(column = "") {
  return create(QualityTargetSchema, {
    metaGuid: TARGET,
    metaType: MetaType.TABLE,
    path: create(QualityTargetPathSchema, {
      instance: "prod",
      database: "shop",
      schema: "public",
      object: "orders",
      column,
    }),
    columns: column ? [column] : [],
  });
}

function ruleFixture(options: {
  name: string;
  title: string;
  ruleType: QualityRuleType;
  column?: string;
  config?: QualityRule["config"];
  status?: QualityStatus;
}) {
  return create(QualityRuleSchema, {
    name: options.name,
    title: options.title,
    ruleType: options.ruleType,
    target: target(options.column),
    config: options.config,
    severity: QualitySeverity.ERROR,
    state: QualityRuleState.ENABLED,
    lastResult:
      options.status === undefined
        ? undefined
        : {
            status: options.status,
            observedValue: 4,
            finishTime: timestampFromDate(new Date()),
          },
  });
}

function rulesFixture() {
  return [
    ruleFixture({
      name: RULE_DATASET,
      title: "Row count floor",
      ruleType: QualityRuleType.ROW_COUNT,
      config: create(QualityRuleConfigSchema, {
        config: {
          case: "rowCount",
          value: create(RowCountConfigSchema, { minRows: 1n }),
        },
      }),
      status: QualityStatus.PASS,
    }),
    ruleFixture({
      name: RULE_MULTI,
      title: "Unique order pair",
      ruleType: QualityRuleType.UNIQUE_COMBINATION,
      config: create(QualityRuleConfigSchema, {
        config: {
          case: "uniqueCombination",
          value: create(UniqueCombinationConfigSchema, {
            columns: ["order_id", "line_id"],
          }),
        },
      }),
      status: QualityStatus.FAIL,
    }),
    ruleFixture({
      name: RULE_COLUMN,
      title: "Email must be present",
      ruleType: QualityRuleType.NULL_PERCENT,
      column: "email",
      config: create(QualityRuleConfigSchema, {
        config: {
          case: "nullPercent",
          value: create(NullPercentConfigSchema, { column: "email" }),
        },
      }),
      status: QualityStatus.ERROR,
    }),
  ];
}

function draftSuggestion(
  name: string,
  title: string,
  column: string,
  configCase: "nullPercent" | "rowCount" = "nullPercent"
) {
  return create(QualitySuggestionSchema, {
    name,
    kind: QualitySuggestionKind.RULE_DRAFT,
    status: QualitySuggestionStatus.PENDING,
    target: create(QualityTargetSchema, {
      metaGuid: TARGET,
      metaType: MetaType.TABLE,
    }),
    payload: {
      payload: {
        case: "rule",
        value: {
          title,
          rationale: "The column is read by an important report.",
          confidence: 0.8,
          severity: QualitySeverity.WARNING,
          config:
            configCase === "rowCount"
              ? create(QualityRuleConfigSchema, {
                  config: {
                    case: "rowCount",
                    value: create(RowCountConfigSchema, { minRows: 1n }),
                  },
                })
              : create(QualityRuleConfigSchema, {
                  config: {
                    case: "nullPercent",
                    value: create(NullPercentConfigSchema, { column }),
                  },
                }),
        },
      },
    },
  });
}

function issueFixture() {
  return create(QualityIssueSchema, {
    name: "workspaces/ws/qualityIssues/issue-1",
    rule: RULE_DATASET,
    target: create(QualityTargetSchema, {
      metaGuid: TARGET,
      metaType: MetaType.TABLE,
    }),
    status: QualityIssueStatus.OPEN,
    severity: QualitySeverity.ERROR,
    lastSeenTime: timestampFromDate(new Date()),
  });
}

async function mountPage(
  path = `/data-quality/targets/${TARGET.split(";").join("/")}`
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality",
        name: "DataQualityOverview",
        component: Dummy,
      },
      {
        path: "/data-quality/targets",
        name: "DataQualityTargets",
        component: Dummy,
      },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/new",
        name: "DataQualityRuleNew",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/:ruleId+/edit",
        name: "DataQualityRuleEdit",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleDetail",
        component: Dummy,
      },
      {
        path: "/data-quality/suggestions",
        name: "DataQualitySuggestions",
        component: Dummy,
      },
      {
        path: "/data-quality/issues/:issueId+",
        name: "DataQualityIssueDetail",
        component: Dummy,
      },
      {
        path: "/data-quality/issues",
        name: "DataQualityIssues",
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
  const wrapper = mount(DataQualityTargetPage, {
    attachTo: document.body,
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function buttonByText(text: string): HTMLElement | undefined {
  return [...document.body.querySelectorAll<HTMLElement>("button")].find(
    (button) => button.textContent?.trim() === text
  );
}

function linkByText(text: string): HTMLAnchorElement | undefined {
  return [...document.body.querySelectorAll<HTMLAnchorElement>("a")].find(
    (link) => link.textContent?.trim() === text
  );
}

describe("DataQualityTargetPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.listQualityRules.mockResolvedValue({
      items: rulesFixture(),
      nextPageToken: "",
    });
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    mocks.listQualityIssues.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    mocks.runQualityRules.mockResolvedValue([]);
    mocks.createQualityRule.mockResolvedValue(
      create(QualityRuleSchema, {
        name: RULE_CREATED,
        title: "Created from draft",
        ruleType: QualityRuleType.NULL_PERCENT,
        target: target("email"),
        severity: QualitySeverity.ERROR,
        state: QualityRuleState.ENABLED,
      })
    );
    mocks.updateQualitySuggestion.mockResolvedValue(
      create(QualitySuggestionSchema, {
        name: DRAFT_EMAIL,
        status: QualitySuggestionStatus.REJECTED,
      })
    );
    mocks.suggestQualityRule.mockResolvedValue({
      qualitySuggestions: [
        draftSuggestion(DRAFT_AMOUNT, "Amounts stay in range", "amount"),
      ],
      summary: undefined,
    });
  });

  it("reads the target's rules in dataset, cross-column and per-column sections", async () => {
    mocks.listQualityIssues.mockResolvedValue({
      items: [issueFixture()],
      nextPageToken: "",
    });
    const { wrapper } = await mountPage();

    // All three reads are scoped to this one target.
    expect(mocks.listQualityRules).toHaveBeenCalledWith({
      targetGuid: TARGET,
      pageSize: 1000,
    });
    expect(mocks.listQualitySuggestions).toHaveBeenCalledWith(
      expect.objectContaining({
        targetGuid: TARGET,
        status: QualitySuggestionStatus.PENDING,
        pageSize: 1000,
      })
    );
    expect(mocks.listQualityIssues).toHaveBeenCalledWith(
      expect.objectContaining({ targetGuid: TARGET, pageSize: 1000 })
    );

    const text = wrapper.text();
    // The section order is dataset → cross-column → columns, matching the way a
    // reader narrows from the whole table to one field.
    const datasetAt = text.indexOf(
      label("dataQuality.workspace.datasetRulesTitle")
    );
    const multiAt = text.indexOf(
      label("dataQuality.workspace.crossColumnRulesTitle")
    );
    const columnAt = text.indexOf("email");
    expect(datasetAt).toBeGreaterThanOrEqual(0);
    expect(multiAt).toBeGreaterThan(datasetAt);
    expect(columnAt).toBeGreaterThan(multiAt);
    expect(text).toContain("Row count floor");
    expect(text).toContain("Unique order pair");
    expect(text).toContain("Email must be present");

    // The header answers "is this table okay?" before the rules are scanned.
    expect(text).toContain(
      label("dataQuality.workspace.summaryRules", { count: 3 })
    );
    expect(text).toContain(
      label("dataQuality.workspace.summaryPassing", { count: 1 })
    );
    expect(text).toContain(
      label("dataQuality.workspace.summaryFailing", { count: 2 })
    );
    expect(text).toContain(
      label("dataQuality.workspace.summaryIssues", { count: 1 })
    );
    expect(text).toContain("prod · shop · public");

    // The open issue is named by the rule that opened it, not by its id.
    expect(text).toContain("Row count floor");
    expect(text).toContain(label("dataQuality.workspace.issuesViewAll"));

    // Run all selects the target itself; a single row selects its own rule.
    buttonByText(label("dataQuality.workspace.runAll"))?.click();
    await flushPromises();
    expect(mocks.runQualityRules).toHaveBeenLastCalledWith({
      targetGuid: TARGET,
    });

    document.body
      .querySelector<HTMLElement>(
        `[aria-label="${label("dataQuality.workspace.runRule")}"]`
      )
      ?.click();
    await flushPromises();
    expect(mocks.runQualityRules).toHaveBeenLastCalledWith({
      rules: [RULE_DATASET],
    });
    wrapper.unmount();
  });

  it("renders AI drafts inline and marks the ones a pass just returned", async () => {
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [
        draftSuggestion(DRAFT_EMAIL, "Emails must not be empty", "email"),
      ],
      nextPageToken: "",
    });
    const { wrapper } = await mountPage();

    const existing = wrapper.findAllComponents(TargetDraftCard);
    expect(existing).toHaveLength(1);
    expect(existing[0]?.text()).toContain("Emails must not be empty");
    expect(existing[0]?.text()).toContain(
      label("dataQuality.workspace.draftBadge")
    );
    expect(existing[0]?.text()).toContain(
      label("dataQuality.workspace.draftColumn", { column: "email" })
    );
    // An already queued draft is highlighted by the card's base ring only.
    expect(existing[0]?.classes()).toContain("ring-1");

    const input = document.body.querySelector<HTMLTextAreaElement>("textarea");
    expect(input).not.toBeNull();
    input!.value = "Keep amounts in a sane range";
    input!.dispatchEvent(new Event("input"));
    await flushPromises();
    buttonByText(label("dataQuality.copilot.generate"))?.click();
    await flushPromises();

    // The new draft joins the column section the pass filed it under, and only
    // the returned rows carry the transient highlight.
    const drafts = wrapper.findAllComponents(TargetDraftCard);
    const created = drafts.find((card) =>
      card.text().includes("Amounts stay in range")
    );
    expect(created).toBeDefined();
    expect(created?.classes()).toContain("ring-2");
    expect(created?.classes()).toContain("animate-in");
    expect(
      drafts
        .find((card) => card.text().includes("Emails must not be empty"))
        ?.classes()
    ).toContain("ring-1");
    expect(wrapper.text()).toContain("amount");
    wrapper.unmount();
  });

  it("creates a rule from a draft and keeps the local workspace in sync", async () => {
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [
        draftSuggestion(DRAFT_EMAIL, "Emails must not be empty", "email"),
      ],
      nextPageToken: "",
    });
    const { wrapper } = await mountPage();

    buttonByText(label("dataQuality.workspace.draftAccept"))?.click();
    await flushPromises();

    // The draft becomes a rule in one call: the suggestion name rides along so
    // the server records the decision as a side effect.
    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    const [createdRule, suggestionName] = mocks.createQualityRule.mock
      .calls[0] as [Record<string, unknown>, string];
    expect(suggestionName).toBe(DRAFT_EMAIL);
    expect(createdRule.title).toBe("Emails must not be empty");

    // The draft card is gone and the created rule is rendered without a refetch.
    expect(wrapper.findAllComponents(TargetDraftCard)).toHaveLength(0);
    expect(wrapper.text()).toContain("Created from draft");
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("degrades the drafts and issues regions without losing the rules", async () => {
    mocks.listQualitySuggestions.mockRejectedValue(
      new ConnectError("drafts down", Code.Internal)
    );
    mocks.listQualityIssues.mockRejectedValue(
      new ConnectError("issues down", Code.Internal)
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain("Row count floor");
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.draftsLoadFailed")
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.issuesLoadFailed")
    );
    wrapper.unmount();
  });

  it("keeps the page behind its own error when the rules read fails, and retries", async () => {
    mocks.listQualityRules.mockRejectedValue(
      new ConnectError("rules down", Code.Internal)
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.rulesLoadFailed")
    );
    const retry = buttonByText(label("common.refresh"));
    expect(retry).toBeDefined();
    retry?.click();
    await flushPromises();
    expect(mocks.listQualityRules).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("explains an empty target and offers the first rule", async () => {
    mocks.listQualityRules.mockResolvedValue({ items: [], nextPageToken: "" });
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.noRulesTitle")
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.noRulesHint")
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryRules", { count: 0 })
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.issuesEmpty")
    );
    // The empty state's next step opens the editor on this target.
    expect(linkByText(label("dataQuality.workspace.addRule"))).toBeDefined();
    wrapper.unmount();
  });

  it("reads the target as the meta type the route names", async () => {
    const { wrapper } = await mountPage(
      `/data-quality/targets/${TARGET.split(";").join("/")}?metaType=${MetaType.VIEW}`
    );

    // The GUID carries no object type, so the query decides how the target is
    // named; without it the first rule's own meta type would be used.
    expect(wrapper.text()).toContain(label("dataQuality.workspace.kindView"));
    wrapper.unmount();
  });
});
