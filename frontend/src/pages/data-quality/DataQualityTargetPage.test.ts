import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import QualityCheckDrawer from "@/components/data-quality/QualityCheckDrawer.vue";
import TargetContractCard from "@/components/data-quality/TargetContractCard.vue";
import TargetDraftCard from "@/components/data-quality/TargetDraftCard.vue";
import TargetRuleComposer from "@/components/data-quality/TargetRuleComposer.vue";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  GetQualityTargetResponseSchema,
  NullPercentConfigSchema,
  type QualityContract,
  QualityContractSchema,
  type QualityIssue,
  QualityIssueSchema,
  QualityIssueStatus,
  type QualityRule,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityStatus,
  type QualitySuggestion,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
  RowCountConfigSchema,
  UniqueCombinationConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityTargetPage from "./DataQualityTargetPage.vue";

// Radix opens a select on pointerdown and calls pointer-capture APIs that jsdom
// does not implement.
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.setPointerCapture ??= () => {};
Element.prototype.releasePointerCapture ??= () => {};

const mocks = vi.hoisted(() => ({
  getQualityTarget: vi.fn(),
  getQualityContract: vi.fn(),
  getQualityRule: vi.fn(),
  listQualityExecutions: vi.fn(),
  deleteQualityRule: vi.fn(),
  listQualitySuggestions: vi.fn(),
  runQualityRules: vi.fn(),
  createQualityRule: vi.fn(),
  updateQualityRule: vi.fn(),
  previewQualityRule: vi.fn(),
  listAllMetadata: vi.fn(),
  updateQualitySuggestion: vi.fn(),
  suggestQualityRule: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityTarget: mocks.getQualityTarget,
  getQualityContract: mocks.getQualityContract,
  getQualityRule: mocks.getQualityRule,
  listQualityExecutions: mocks.listQualityExecutions,
  deleteQualityRule: mocks.deleteQualityRule,
  listQualitySuggestions: mocks.listQualitySuggestions,
  runQualityRules: mocks.runQualityRules,
  createQualityRule: mocks.createQualityRule,
  updateQualityRule: mocks.updateQualityRule,
  previewQualityRule: mocks.previewQualityRule,
  updateQualitySuggestion: mocks.updateQualitySuggestion,
  suggestQualityRule: mocks.suggestQualityRule,
}));

// The inline composer lists the target's columns through this API.
vi.mock("@/api/database", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/database")>()),
  listAllMetadata: mocks.listAllMetadata,
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
  "metaxisdata.dataQuality.rules.delete",
  "metaxisdata.dataQuality.suggest",
  "metaxisdata.dataQuality.issues.list",
  "metaxisdata.dataQuality.issues.get",
];

function target(column = "", metaType = MetaType.TABLE) {
  return create(QualityTargetSchema, {
    metaGuid: TARGET,
    metaType,
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
    scheduleInterval: { seconds: 3600n, nanos: 0 },
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

/** The target's contract: a published version that has drifted from the rules. */
function contractFixture(
  options: {
    version?: number;
    changes?: boolean;
    scheduleSeconds?: bigint;
    severity?: QualitySeverity;
  } = {}
): QualityContract {
  return create(QualityContractSchema, {
    publishedVersion: options.version ?? 2,
    publishedAt: timestampFromDate(new Date()),
    publishedBy: "users/alice",
    hasUnpublishedChanges: options.changes ?? true,
    checkCount: 3,
    changeSummary: { added: 1, modified: 1, removed: 0 },
    settings: {
      defaultScheduleIntervalSeconds: options.scheduleSeconds ?? 0n,
      defaultSeverity: options.severity ?? QualitySeverity.UNSPECIFIED,
    },
  });
}

function summaryFixture(
  options: {
    metaType?: MetaType;
    ruleCount?: number;
    enabledRuleCount?: number;
    openIssueCount?: number;
    pendingDraftCount?: number;
    schemaChanged?: boolean;
    withPath?: boolean;
  } = {}
): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, {
      metaGuid: TARGET,
      metaType: options.metaType ?? MetaType.TABLE,
      path:
        options.withPath === false
          ? undefined
          : create(QualityTargetPathSchema, {
              instance: "prod",
              database: "shop",
              schema: "public",
              object: "orders",
            }),
    }),
    ruleCount: options.ruleCount ?? 3,
    enabledRuleCount: options.enabledRuleCount ?? options.ruleCount ?? 3,
    pausedRuleCount: 0,
    columnCount: 1,
    openIssueCount: options.openIssueCount ?? 0,
    pendingDraftCount: options.pendingDraftCount ?? 0,
    pendingLabelCount: 0,
    worstStatus: QualityStatus.ERROR,
    lastRunTime: timestampFromDate(new Date()),
    schemaChanged: options.schemaChanged ?? false,
  });
}

function response(
  options: {
    summary?: QualityTargetSummary;
    rules?: QualityRule[];
    issues?: QualityIssue[];
    orphanedReason?: string;
  } = {}
) {
  return create(GetQualityTargetResponseSchema, {
    summary: options.summary ?? summaryFixture(),
    qualityRules: options.rules ?? rulesFixture(),
    openIssues: options.issues ?? [],
    orphanedReason: options.orphanedReason ?? "",
  });
}

/** The pending-draft queue the suggest-capable reader loads separately. */
function draftPage(suggestions: QualitySuggestion[]) {
  return { items: suggestions, nextPageToken: "" };
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
                    value: create(NullPercentConfigSchema, {
                      column,
                      maxNullPct: 0.01,
                    }),
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
  path = `/data-quality/targets/${TARGET.split(";").join("/")}`,
  permissions = PERMISSIONS
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
        path: "/data-quality/executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: Dummy,
      },
      {
        path: "/metadata/:guid+",
        name: "MetadataDetail",
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
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(DataQualityTargetPage, {
    attachTo: document.body,
    global: {
      plugins: [router, i18n, pinia],
      stubs: { QualityExplainDrawer: true },
    },
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

function confirmDialog(): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"]'),
  ].find((dialog) =>
    dialog.textContent?.includes(label("dataQuality.deleteRuleTitle"))
  );
}

function dialogButton(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.trim() === text);
}

function setInput(selector: string, value: string) {
  const element = document.querySelector(selector) as HTMLInputElement;
  Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value"
  )?.set?.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

const scrollSpy = vi.fn();

describe("DataQualityTargetPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    Element.prototype.scrollIntoView = scrollSpy;
    mocks.getQualityTarget.mockResolvedValue(response());
    mocks.getQualityContract.mockResolvedValue(contractFixture());
    mocks.getQualityRule.mockResolvedValue(rulesFixture()[0]);
    mocks.listQualityExecutions.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    mocks.deleteQualityRule.mockResolvedValue(undefined);
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    mocks.runQualityRules.mockResolvedValue([]);
    mocks.previewQualityRule.mockResolvedValue({
      compiledSql: "SELECT count(*) FROM orders",
      referencedColumns: [],
      parameters: [],
    });
    mocks.listAllMetadata.mockResolvedValue([]);
    mocks.updateQualityRule.mockResolvedValue(
      create(QualityRuleSchema, {
        name: RULE_DATASET,
        title: "Row count floor, revised",
        ruleType: QualityRuleType.ROW_COUNT,
        target: target(),
        severity: QualitySeverity.ERROR,
        state: QualityRuleState.ENABLED,
        scheduleInterval: { seconds: 3600n, nanos: 0 },
        config: create(QualityRuleConfigSchema, {
          config: {
            case: "rowCount",
            value: create(RowCountConfigSchema, { minRows: 5n }),
          },
        }),
      })
    );
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

  it("reads the target's whole workspace in one call and groups its rules", async () => {
    mocks.getQualityTarget.mockResolvedValue(
      response({
        summary: summaryFixture({ openIssueCount: 1 }),
        issues: [issueFixture()],
      })
    );
    const { wrapper } = await mountPage();

    // One aggregate read; the server derives the meta type from the rules.
    expect(mocks.getQualityTarget).toHaveBeenCalledWith({
      targetGuid: TARGET,
      metaType: MetaType.UNSPECIFIED,
    });
    // Drafts are a separate read because they need the suggest permission.
    expect(mocks.listQualitySuggestions).toHaveBeenCalledWith({
      targetGuid: TARGET,
      status: QualitySuggestionStatus.PENDING,
      pageSize: 1000,
    });

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
    expect(text).toContain(label("dataQuality.workspace.kindTable"));

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
    mocks.getQualityTarget.mockResolvedValue(
      response({ summary: summaryFixture({ pendingDraftCount: 1 }) })
    );
    mocks.listQualitySuggestions.mockResolvedValue(
      draftPage([
        draftSuggestion(DRAFT_EMAIL, "Emails must not be empty", "email"),
      ])
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryDrafts", { count: 1 })
    );

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
    // the returned rows carry the transient highlight. The header count follows
    // the local queue without re-reading the workspace.
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
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryDrafts", { count: 2 })
    );
    expect(mocks.getQualityTarget).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("creates a rule from a draft and adjusts the summary counts in place", async () => {
    mocks.getQualityTarget.mockResolvedValue(
      response({ summary: summaryFixture({ pendingDraftCount: 1 }) })
    );
    mocks.listQualitySuggestions.mockResolvedValue(
      draftPage([
        draftSuggestion(DRAFT_EMAIL, "Emails must not be empty", "email"),
      ])
    );
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

    // The draft card is gone, the created rule is rendered and the header counts
    // moved with it; nothing re-reads the workspace.
    expect(wrapper.findAllComponents(TargetDraftCard)).toHaveLength(0);
    expect(wrapper.text()).toContain("Created from draft");
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryRules", { count: 4 })
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryDrafts", { count: 0 })
    );
    expect(mocks.getQualityTarget).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("drops a rejected draft and decrements the draft count", async () => {
    mocks.getQualityTarget.mockResolvedValue(
      response({ summary: summaryFixture({ pendingDraftCount: 1 }) })
    );
    mocks.listQualitySuggestions.mockResolvedValue(
      draftPage([
        draftSuggestion(DRAFT_EMAIL, "Emails must not be empty", "email"),
      ])
    );
    const { wrapper } = await mountPage();

    buttonByText(label("dataQuality.workspace.draftIgnore"))?.click();
    await flushPromises();

    expect(mocks.updateQualitySuggestion).toHaveBeenCalledTimes(1);
    expect(wrapper.findAllComponents(TargetDraftCard)).toHaveLength(0);
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryDrafts", { count: 0 })
    );
    expect(mocks.getQualityTarget).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("shows the page error when the aggregate read fails, and retries", async () => {
    mocks.getQualityTarget.mockRejectedValue(
      new ConnectError("target down", Code.Internal)
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.rulesLoadFailed")
    );
    const retry = buttonByText(label("common.refresh"));
    expect(retry).toBeDefined();
    retry?.click();
    await flushPromises();
    expect(mocks.getQualityTarget).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("explains an empty target whose metadata still resolves", async () => {
    mocks.getQualityTarget.mockResolvedValue(
      response({
        summary: summaryFixture({ ruleCount: 0, withPath: true }),
        rules: [],
      })
    );
    const { wrapper } = await mountPage();

    // The summary's registry path still names the target and its kind.
    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).toContain(label("dataQuality.workspace.kindTable"));
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
    // The empty state's next step opens the inline composer for this target.
    expect(buttonByText(label("dataQuality.workspace.addRule"))).toBeDefined();
    wrapper.unmount();
  });

  it("survives a response without a summary", async () => {
    mocks.getQualityTarget.mockResolvedValue(
      create(GetQualityTargetResponseSchema, {
        qualityRules: [],
        openIssues: [],
        orphanedReason: "",
      })
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.noRulesTitle")
    );
    expect(wrapper.text()).not.toContain(
      label("dataQuality.workspace.rulesLoadFailed")
    );
    wrapper.unmount();
  });

  it("flags an orphaned target whose schema changed", async () => {
    mocks.getQualityTarget.mockResolvedValue(
      response({
        summary: summaryFixture({ schemaChanged: true }),
        orphanedReason: "table dropped",
      })
    );
    const { wrapper } = await mountPage();

    const text = wrapper.text();
    expect(text).toContain(label("dataQuality.orphaned"));
    expect(text).toContain(label("dataQuality.targetChanged"));
    expect(text).toContain(label("dataQuality.workspace.targetMissingTitle"));
    expect(text).toContain(label("dataQuality.workspace.targetMissingHint"));
    wrapper.unmount();
  });

  it("reads the target as the meta type the route names", async () => {
    const { wrapper } = await mountPage(
      `/data-quality/targets/${TARGET.split(";").join("/")}?metaType=${MetaType.VIEW}`
    );

    // The query wins over the summary, so the page names the target as a view.
    expect(mocks.getQualityTarget).toHaveBeenCalledWith({
      targetGuid: TARGET,
      metaType: MetaType.VIEW,
    });
    expect(wrapper.text()).toContain(label("dataQuality.workspace.kindView"));
    wrapper.unmount();
  });

  it("falls back to the first rule's meta type when the summary has none", async () => {
    mocks.getQualityTarget.mockResolvedValue(
      response({
        summary: summaryFixture({ metaType: MetaType.UNSPECIFIED }),
      })
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(label("dataQuality.workspace.kindTable"));
    wrapper.unmount();
  });

  it("still degrades the drafts region when the refresh after a failed decision fails", async () => {
    mocks.getQualityTarget.mockResolvedValue(
      response({ summary: summaryFixture({ pendingDraftCount: 1 }) })
    );
    mocks.updateQualitySuggestion.mockRejectedValue(
      new ConnectError("decision down", Code.Internal)
    );
    mocks.listQualitySuggestions
      .mockResolvedValueOnce(
        draftPage([
          draftSuggestion(DRAFT_EMAIL, "Emails must not be empty", "email"),
        ])
      )
      .mockRejectedValue(new ConnectError("drafts down", Code.Internal));
    const { wrapper } = await mountPage();

    buttonByText(label("dataQuality.workspace.draftIgnore"))?.click();
    await flushPromises();

    expect(wrapper.text()).toContain("Row count floor");
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.draftsLoadFailed")
    );
    wrapper.unmount();
  });

  it("creates a rule inline and renders it without re-reading the target", async () => {
    const created = create(QualityRuleSchema, {
      name: RULE_CREATED,
      title: "Orders stay fresh",
      ruleType: QualityRuleType.ROW_COUNT,
      target: target(),
      severity: QualitySeverity.WARNING,
      state: QualityRuleState.ENABLED,
    });
    mocks.createQualityRule.mockResolvedValue(created);
    const { wrapper } = await mountPage();

    // The header's New rule opens the composer instead of leaving the page.
    buttonByText(label("dataQuality.workspace.addRule"))?.click();
    await flushPromises();
    expect(document.body.textContent).toContain(
      label("dataQuality.composer.title", { target: "orders" })
    );

    setInput("#dq-composer-title", "Orders stay fresh");
    setInput("#dq-field-minRows", "1");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    // The request carries the target the page was already showing.
    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    const [createdRule] = mocks.createQualityRule.mock.calls[0] as [
      QualityRule,
    ];
    expect(createdRule.target?.metaGuid).toBe(TARGET);

    // The rule joins the page where it belongs and the count moves; the page
    // does not re-read the workspace.
    expect(wrapper.text()).toContain("Orders stay fresh");
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryRules", { count: 4 })
    );
    expect(mocks.getQualityTarget).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("does not read the draft queue without the suggest permission", async () => {
    const { wrapper } = await mountPage(
      `/data-quality/targets/${TARGET.split(";").join("/")}`,
      PERMISSIONS.filter(
        (permission) => permission !== "metaxisdata.dataQuality.suggest"
      )
    );

    // The aggregate read needs only the rules permission; a reader without the
    // suggest permission never asks for the queue, so no draft request is made.
    expect(mocks.getQualityTarget).toHaveBeenCalledTimes(1);
    expect(mocks.listQualitySuggestions).not.toHaveBeenCalled();
    expect(wrapper.findAllComponents(TargetDraftCard)).toHaveLength(0);
    expect(wrapper.text()).toContain("Row count floor");
    wrapper.unmount();
  });

  it("reads the contract beside the workspace and shows its version", async () => {
    const { wrapper } = await mountPage();

    // Both reads start together: the contract is a projection, not part of the
    // workspace payload.
    expect(mocks.getQualityTarget).toHaveBeenCalledTimes(1);
    expect(mocks.getQualityContract).toHaveBeenCalledWith({
      targetGuid: TARGET,
      metaType: MetaType.UNSPECIFIED,
    });
    const text = wrapper.text();
    expect(text).toContain(
      label("dataQuality.contract.statePublished", { version: 2 })
    );
    // One added and one changed rule are the two unpublished changes.
    expect(text).toContain(
      label("dataQuality.contract.unpublishedChanges", { count: 2 })
    );
    wrapper.unmount();
  });

  it("keeps the workspace when only the contract read fails", async () => {
    mocks.getQualityContract.mockRejectedValue(
      new ConnectError("contract down", Code.Internal)
    );
    const { wrapper } = await mountPage();

    // The rules are the page; a failed contract read degrades one strip.
    expect(wrapper.text()).toContain("Row count floor");
    expect(wrapper.text()).toContain(label("dataQuality.contract.loadFailed"));

    buttonByText(label("common.retry"))?.click();
    await flushPromises();
    expect(mocks.getQualityContract).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("hands the contract's defaults to the inline composer", async () => {
    mocks.getQualityContract.mockResolvedValue(
      contractFixture({
        scheduleSeconds: 6n * 3600n,
        severity: QualitySeverity.ERROR,
      })
    );
    const { wrapper } = await mountPage();

    const composer = wrapper.findComponent(TargetRuleComposer);
    expect(composer.props("defaultScheduleSeconds")).toBe(21600);
    expect(composer.props("defaultSeverity")).toBe(QualitySeverity.ERROR);
    wrapper.unmount();
  });

  it("shows the contract a publish or a settings save returned", async () => {
    const { wrapper } = await mountPage();
    const published = contractFixture({ version: 3, changes: false });

    wrapper.findComponent(TargetContractCard).vm.$emit("published", published);
    await flushPromises();
    expect(wrapper.findComponent(TargetContractCard).props("contract")).toEqual(
      published
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.contract.statePublished", { version: 3 })
    );

    const updated = contractFixture({ version: 3, changes: true });
    wrapper.findComponent(TargetContractCard).vm.$emit("updated", updated);
    await flushPromises();
    expect(wrapper.findComponent(TargetContractCard).props("contract")).toEqual(
      updated
    );
    // Local state only: neither event re-reads the contract.
    expect(mocks.getQualityContract).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("edits a rule inline and replaces its row without a re-read", async () => {
    const { wrapper } = await mountPage();
    const row = wrapper.find("#dq-rule-rule-dataset");
    expect(row.exists()).toBe(true);

    // The row's pencil opens the workspace composer, fixed to that rule.
    await row
      .find(`[aria-label="${label("dataQuality.editRule")}"]`)
      .trigger("click");
    await flushPromises();
    expect(document.body.textContent).toContain(
      label("dataQuality.composer.editTitle", { title: "Row count floor" })
    );

    setInput("#dq-composer-title", "Row count floor, revised");
    setInput("#dq-field-minRows", "5");
    await flushPromises();
    dialogButton(label("dataQuality.editor.saveChanges"))?.click();
    await flushPromises();

    expect(mocks.updateQualityRule).toHaveBeenCalledTimes(1);
    const [rule, mask] = mocks.updateQualityRule.mock.calls[0] as [
      QualityRule,
      string[],
    ];
    expect(rule.name).toBe(RULE_DATASET);
    expect(rule.title).toBe("Row count floor, revised");
    // The row's own target and state are untouched, so the mask names neither.
    expect(mask).toContain("title");
    expect(mask).toContain("config");
    expect(mask).not.toContain("target");
    expect(mask).not.toContain("state");

    // The returned rule replaces its row in place; the counts and the read stand.
    expect(wrapper.find("#dq-rule-rule-dataset").text()).toContain(
      "Row count floor, revised"
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryRules", { count: 3 })
    );
    expect(mocks.getQualityTarget).toHaveBeenCalledTimes(1);
    expect(wrapper.findComponent(TargetRuleComposer).props("open")).toBe(false);
    wrapper.unmount();
  });

  it("highlights and scrolls to the rule the query names", async () => {
    const { wrapper } = await mountPage(
      `/data-quality/targets/${TARGET.split(";").join("/")}?highlight=${encodeURIComponent(RULE_DATASET)}`
    );
    await flushPromises();

    expect(wrapper.find("#dq-rule-rule-dataset").classes()).toContain("ring-2");
    // Only the named row is marked.
    expect(wrapper.find("#dq-rule-rule-multi").classes()).not.toContain(
      "ring-2"
    );
    expect(scrollSpy).toHaveBeenCalledTimes(1);
    expect(scrollSpy).toHaveBeenCalledWith({ block: "center" });
    wrapper.unmount();
  });

  it("ignores a highlight that names no row on this target", async () => {
    const { wrapper } = await mountPage(
      `/data-quality/targets/${TARGET.split(";").join("/")}?highlight=${encodeURIComponent("workspaces/ws/qualityRules/elsewhere")}`
    );
    await flushPromises();

    expect(scrollSpy).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("Row count floor");
    wrapper.unmount();
  });

  it("opens the check side sheet from a row and closes it", async () => {
    const { wrapper } = await mountPage();
    const row = wrapper.find("#dq-rule-rule-dataset");

    await row.find("button").trigger("click");
    await flushPromises();

    expect(mocks.getQualityRule).toHaveBeenCalledWith(RULE_DATASET);
    expect(document.body.textContent).toContain(
      label("dataQuality.ruleDefinition")
    );
    expect(document.body.textContent).toContain("Row count floor");

    dialogButton(label("common.close"))?.click();
    await flushPromises();
    expect(document.body.textContent).not.toContain(
      label("dataQuality.ruleDefinition")
    );
    wrapper.unmount();
  });

  it("opens the drawer named by the check query and clears it on close", async () => {
    const { wrapper, router } = await mountPage(
      `/data-quality/targets/${TARGET.split(";").join("/")}?metaType=${MetaType.TABLE}&check=${encodeURIComponent(RULE_DATASET)}`
    );

    expect(mocks.getQualityRule).toHaveBeenCalledWith(RULE_DATASET);
    expect(document.body.textContent).toContain(
      label("dataQuality.ruleDefinition")
    );

    dialogButton(label("common.close"))?.click();
    await flushPromises();
    // The other query keys survive; only the check is removed.
    expect(router.currentRoute.value.query.check).toBeUndefined();
    expect(router.currentRoute.value.query.metaType).toBe(
      String(MetaType.TABLE)
    );
    wrapper.unmount();
  });

  it("drops a check query that names no rule on this target", async () => {
    const { wrapper, router } = await mountPage(
      `/data-quality/targets/${TARGET.split(";").join("/")}?check=${encodeURIComponent("workspaces/ws/qualityRules/elsewhere")}`
    );
    await flushPromises();

    expect(mocks.getQualityRule).not.toHaveBeenCalled();
    expect(router.currentRoute.value.query.check).toBeUndefined();
    expect(wrapper.text()).toContain("Row count floor");
    wrapper.unmount();
  });

  it("edits a check from its side sheet in the workspace composer", async () => {
    const { wrapper } = await mountPage();
    await wrapper.find("#dq-rule-rule-dataset").find("button").trigger("click");
    await flushPromises();

    buttonByText(label("dataQuality.editRule"))?.click();
    await flushPromises();

    // The sheet closes and the composer opens fixed to that rule.
    expect(document.body.textContent).not.toContain(
      label("dataQuality.ruleDefinition")
    );
    const composer = wrapper.findComponent(TargetRuleComposer);
    expect(composer.props("open")).toBe(true);
    expect(composer.props("rule")?.name).toBe(RULE_DATASET);
    wrapper.unmount();
  });

  it("keeps the row and counts in sync when the drawer reports a change", async () => {
    const { wrapper } = await mountPage();

    const changed = create(QualityRuleSchema, {
      name: RULE_DATASET,
      title: "Row count floor, paused",
      ruleType: QualityRuleType.ROW_COUNT,
      target: target(),
      severity: QualitySeverity.ERROR,
      state: QualityRuleState.PAUSED,
      scheduleInterval: { seconds: 3600n, nanos: 0 },
    });
    wrapper.findComponent(QualityCheckDrawer).vm.$emit("changed", changed);
    await flushPromises();

    expect(wrapper.find("#dq-rule-rule-dataset").text()).toContain(
      "Row count floor, paused"
    );
    // Pausing moves one rule from enabled to paused without re-reading.
    expect(mocks.getQualityTarget).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("drops a deleted check's row and decrements the summary", async () => {
    const { wrapper } = await mountPage();
    expect(wrapper.find("#dq-rule-rule-dataset").exists()).toBe(true);

    wrapper.findComponent(QualityCheckDrawer).vm.$emit("deleted", RULE_DATASET);
    await flushPromises();

    expect(wrapper.find("#dq-rule-rule-dataset").exists()).toBe(false);
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryRules", { count: 2 })
    );
    expect(mocks.getQualityTarget).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("archives a check from the drawer after the confirmation", async () => {
    const { wrapper } = await mountPage();
    await wrapper.find("#dq-rule-rule-dataset").find("button").trigger("click");
    await flushPromises();

    buttonByText(label("common.delete"))?.click();
    await flushPromises();
    const dialog = confirmDialog();
    expect(dialog).toBeDefined();
    dialog?.querySelectorAll<HTMLElement>("button").forEach((button) => {
      if (button.textContent?.trim() === label("common.delete")) {
        button.click();
      }
    });
    await flushPromises();

    expect(mocks.deleteQualityRule).toHaveBeenCalledWith(RULE_DATASET);
    expect(wrapper.find("#dq-rule-rule-dataset").exists()).toBe(false);
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryRules", { count: 2 })
    );
    wrapper.unmount();
  });

  it("adjusts a draft in the workspace composer and accepts it on save", async () => {
    mocks.getQualityTarget.mockResolvedValue(
      response({ summary: summaryFixture({ pendingDraftCount: 1 }) })
    );
    mocks.listQualitySuggestions.mockResolvedValue(
      draftPage([
        draftSuggestion(DRAFT_EMAIL, "Emails must not be empty", "email"),
      ])
    );
    const { wrapper } = await mountPage();

    buttonByText(label("dataQuality.workspace.draftEdit"))?.click();
    await flushPromises();

    // The composer receives the draft, prefilled rather than blank.
    const composer = wrapper.findComponent(TargetRuleComposer);
    expect(composer.props("open")).toBe(true);
    expect(composer.props("suggestion")?.name).toBe(DRAFT_EMAIL);
    const titleInput =
      document.querySelector<HTMLInputElement>("#dq-composer-title");
    expect(titleInput?.value).toBe("Emails must not be empty");

    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    // Saving accepts the draft with the rule in one request.
    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    const [, suggestionName] = mocks.createQualityRule.mock.calls[0] as [
      QualityRule,
      string,
    ];
    expect(suggestionName).toBe(DRAFT_EMAIL);
    expect(wrapper.findAllComponents(TargetDraftCard)).toHaveLength(0);
    expect(wrapper.text()).toContain("Created from draft");
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryDrafts", { count: 0 })
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.workspace.summaryRules", { count: 4 })
    );
    wrapper.unmount();
  });

  it("opens the check dialog when the directory sends add=1", async () => {
    const { wrapper } = await mountPage(
      `/data-quality/targets/${TARGET.split(";").join("/")}?metaType=${MetaType.TABLE}&add=1`
    );

    expect(wrapper.findComponent(TargetRuleComposer).props("open")).toBe(true);
    expect(wrapper.vm.$route.query.add).toBeUndefined();
    wrapper.unmount();
  });
});
