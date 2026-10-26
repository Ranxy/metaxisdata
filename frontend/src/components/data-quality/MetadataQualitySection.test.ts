import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { notify } from "@/lib/notify";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  ColumnProfileSchema,
  ColumnProfileStatsSchema,
  ColumnSemanticLabelSchema,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityIssueSchema,
  QualityIssueStatus,
  QualityObservedKind,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySemanticType,
  QualitySeverity,
  QualityStatus,
  QualitySuggestionKind,
  QualitySuggestionQueueSummarySchema,
  QualitySuggestionSchema,
  QualitySuggestionStatus,
  QualityTargetSchema,
  RowCountConfigSchema,
  SemanticLabelSuggestionSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import MetadataQualitySection from "./MetadataQualitySection.vue";

const mocks = vi.hoisted(() => ({
  listQualityRules: vi.fn(),
  listQualityExecutions: vi.fn(),
  listQualityIssues: vi.fn(),
  listColumnLabels: vi.fn(),
  listColumnProfiles: vi.fn(),
  listQualitySuggestions: vi.fn(),
  profileColumns: vi.fn(),
  suggestColumnLabels: vi.fn(),
  updateColumnLabel: vi.fn(),
  updateQualitySuggestion: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualityRules: mocks.listQualityRules,
  listQualityExecutions: mocks.listQualityExecutions,
  listQualityIssues: mocks.listQualityIssues,
  listColumnLabels: mocks.listColumnLabels,
  listColumnProfiles: mocks.listColumnProfiles,
  listQualitySuggestions: mocks.listQualitySuggestions,
  profileColumns: mocks.profileColumns,
  suggestColumnLabels: mocks.suggestColumnLabels,
  updateColumnLabel: mocks.updateColumnLabel,
  updateQualitySuggestion: mocks.updateQualitySuggestion,
}));

const TABLE = "inst1;db1;public;orders";
const RULE = "workspaces/ws/qualityRules/rule-1";
const EXECUTION = "workspaces/ws/qualityExecutions/exec-1";
const LABEL_SUGGESTION = "workspaces/ws/qualitySuggestions/label-1";
const DRAFT_SUGGESTION = "workspaces/ws/qualitySuggestions/draft-1";
const BASE_TIME = new Date("2026-10-07T09:30:00Z");
const SUGGEST_PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.dataQuality.profile.run",
  "metaxisdata.dataQuality.suggest",
  "metaxisdata.dataQuality.labels.update",
];

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

/** A rule list row; the preview enriches an execution with it. */
function rule(name: string, title: string) {
  return create(QualityRuleSchema, {
    name,
    title,
    ruleType: QualityRuleType.ROW_COUNT,
    severity: QualitySeverity.ERROR,
    state: QualityRuleState.ENABLED,
  });
}

/** A completed run of `ruleName`, `minute` minutes after the base time. */
function executionOf(
  ruleName: string,
  name = EXECUTION,
  options: { outcome?: QualityStatus; minute?: number } = {}
) {
  return create(QualityExecutionSchema, {
    name: `workspaces/ws/qualityExecutions/${name}`,
    rule: ruleName,
    state: QualityExecutionState.COMPLETED,
    outcome: options.outcome ?? QualityStatus.FAIL,
    observedKind: QualityObservedKind.RATIO,
    observedValue: 0.04,
    failedRows: 40n,
    totalRows: 1000n,
    finishTime: timestampFromDate(
      new Date(BASE_TIME.getTime() + (options.minute ?? 0) * 60_000)
    ),
  });
}

/** A pending label proposal for the `email` column. */
function labelSuggestion(
  options: { name?: string; column?: string; rationale?: string } = {}
) {
  const column = options.column ?? "email";
  return create(QualitySuggestionSchema, {
    name: options.name ?? LABEL_SUGGESTION,
    kind: QualitySuggestionKind.LABEL,
    status: QualitySuggestionStatus.PENDING,
    target: create(QualityTargetSchema, {
      metaGuid: TABLE,
      metaType: MetaType.TABLE,
    }),
    payload: {
      payload: {
        case: "label",
        value: create(SemanticLabelSuggestionSchema, {
          column,
          label: QualitySemanticType.EMAIL,
          confidence: 0.85,
          evidence: {
            rationale: options.rationale ?? "Values look like addresses.",
          },
        }),
      },
    },
  });
}

/** A pending rule draft the target workspace can adjust. */
function draftSuggestion(options: { name?: string; title?: string } = {}) {
  return create(QualitySuggestionSchema, {
    name: options.name ?? DRAFT_SUGGESTION,
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
          title: options.title ?? "Orders should have rows",
          rationale: "The table is the fact table for orders.",
          severity: QualitySeverity.ERROR,
          confidence: 0.7,
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

type Mounted = Awaited<ReturnType<typeof mountSection>>;

function queryOf(href: string): URLSearchParams {
  return new URLSearchParams(href.split("?")[1] ?? "");
}

/** The links whose query names a check, i.e. the preview's rows. */
function checkRowLinks(wrapper: Mounted) {
  return wrapper
    .findAll("a")
    .filter((link) => queryOf(link.attributes("href") ?? "").has("check"));
}

async function mountSection(permissions: string[]) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", component: { template: "<div />" } },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/suggestions",
        name: "DataQualitySuggestions",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/");
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(MetadataQualitySection, {
    props: {
      tableGuid: TABLE,
      metaType: MetaType.TABLE,
      columns: [
        {
          name: "id",
          type: "bigint",
          nullable: false,
          position: 1,
          $typeName: "metaxisdata.v1.ColumnMetadata",
        } as never,
        {
          name: "email",
          type: "text",
          nullable: true,
          position: 2,
          $typeName: "metaxisdata.v1.ColumnMetadata",
        } as never,
      ],
    },
    global: { plugins: [i18n, pinia, router] },
  });
  await flushPromises();
  return wrapper;
}

describe("MetadataQualitySection", () => {
  beforeEach(() => {
    localStorage.clear();
    document.body.innerHTML = "";
    vi.clearAllMocks();
    mocks.listQualityRules.mockResolvedValue({
      items: [
        create(QualityRuleSchema, {
          name: RULE,
          title: "Null rate on orders.id",
          ruleType: QualityRuleType.NULL_PERCENT,
          severity: QualitySeverity.ERROR,
          state: QualityRuleState.ENABLED,
          lastResult: { status: QualityStatus.FAIL },
        }),
      ],
      nextPageToken: "",
    });
    mocks.listQualityExecutions.mockResolvedValue({
      items: [executionOf(RULE)],
      nextPageToken: "",
    });
    mocks.listQualityIssues.mockResolvedValue({
      items: [
        create(QualityIssueSchema, {
          name: "workspaces/ws/qualityIssues/i1",
          rule: RULE,
          status: QualityIssueStatus.ACKNOWLEDGED,
          severity: QualitySeverity.ERROR,
        }),
      ],
      nextPageToken: "",
    });
    mocks.listColumnLabels.mockResolvedValue([
      create(ColumnSemanticLabelSchema, {
        tableGuid: TABLE,
        metaType: MetaType.TABLE,
        column: "id",
        label: QualitySemanticType.ID,
        status: 2,
        confidence: 0.9,
      }),
    ]);
    mocks.listColumnProfiles.mockResolvedValue([
      create(ColumnProfileSchema, {
        tableGuid: TABLE,
        metaType: MetaType.TABLE,
        column: "id",
        sampleMethod: "full",
        sampleRows: 1000n,
        profile: create(ColumnProfileStatsSchema, {
          totalRows: 1000n,
          nullCount: 20n,
          distinctCount: 990n,
        }),
      }),
    ]);
    mocks.suggestColumnLabels.mockResolvedValue({
      qualitySuggestions: [],
      summary: create(QualitySuggestionQueueSummarySchema, { created: 2 }),
    });
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    mocks.updateQualitySuggestion.mockResolvedValue(labelSuggestion());
  });

  it("renders the target's counts, newest check result and column facts", async () => {
    const wrapper = await mountSection(["metaxisdata.dataQuality.rules.list"]);

    // The preview names the check by its rule title and shows its latest result.
    expect(wrapper.text()).toContain("Null rate on orders.id");
    expect(wrapper.text()).toContain(label("dataQuality.ruleType.nullPercent"));
    expect(wrapper.text()).toContain(label("dataQuality.severity.error"));
    expect(wrapper.text()).toContain(label("dataQuality.status.fail"));
    expect(wrapper.text()).toContain("4%");
    // The counts survive: checks, open issues, labelled and profiled columns.
    expect(wrapper.text()).toContain(label("dataQuality.asset.ruleCount"));
    expect(wrapper.text()).toContain(label("dataQuality.asset.openIssueCount"));
    expect(wrapper.text()).toContain(label("dataQuality.asset.labeledCount"));
    expect(wrapper.text()).toContain(label("dataQuality.asset.profiledCount"));
    // The accepted label and the profile facts show up per column, and the
    // unlabelled column says so explicitly.
    expect(wrapper.text()).toContain(label("dataQuality.semanticType.id"));
    expect(wrapper.text()).toContain("990");
    expect(wrapper.text()).toContain(label("dataQuality.asset.noLabel"));
    // Read-only caller: no action buttons.
    expect(wrapper.text().includes(label("dataQuality.asset.createRule"))).toBe(
      false
    );
    expect(
      wrapper.text().includes(label("dataQuality.asset.analyzeColumns"))
    ).toBe(false);
    // The queue endpoint needs the suggest permission, so a read-only caller
    // never asks for it.
    expect(mocks.listQualitySuggestions).not.toHaveBeenCalled();
  });

  it("offers the actions its permissions allow, but never a check creator", async () => {
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    expect(wrapper.text()).toContain(label("dataQuality.asset.analyzeColumns"));
    expect(wrapper.text()).toContain(label("dataQuality.asset.suggestLabels"));
    expect(wrapper.find('[aria-label="Clear label"]').exists()).toBe(true);
    // Authoring lives in the target workspace only: no inline create entrance,
    // and no link into the removed rule editor.
    expect(wrapper.text().includes(label("dataQuality.asset.createRule"))).toBe(
      false
    );
    expect(wrapper.find('a[href*="/rules/new"]').exists()).toBe(false);
    expect(wrapper.find('a[href*="suggestion="]').exists()).toBe(false);
  });

  it("links the preview and every check row to the target workspace", async () => {
    const wrapper = await mountSection(["metaxisdata.dataQuality.rules.list"]);

    // The primary entrance opens the workspace for this target and meta type.
    const workspace = wrapper
      .findAll("a")
      .find((link) =>
        link.text().includes(label("dataQuality.asset.openWorkspace"))
      );
    expect(workspace).toBeDefined();
    const workspaceHref = workspace!.attributes("href") ?? "";
    expect(workspaceHref.split("?")[0]).toBe(
      "/data-quality/targets/inst1/db1/public/orders"
    );
    expect(queryOf(workspaceHref).get("metaType")).toBe(String(MetaType.TABLE));

    // A check row carries the check's resource name, not a rule-detail route.
    const [row] = checkRowLinks(wrapper);
    expect(row).toBeDefined();
    expect(row.text()).toContain("Null rate on orders.id");
    const query = queryOf(row.attributes("href") ?? "");
    expect(query.get("check")).toBe(RULE);
    expect(query.get("metaType")).toBe(String(MetaType.TABLE));
  });

  it("stays out of the page for a caller without rules.list", async () => {
    const wrapper = await mountSection([]);
    expect(wrapper.text()).toBe("");
    expect(mocks.listQualityRules).not.toHaveBeenCalled();
  });

  it("lists only the newest run of each check and skips unfinished ones", async () => {
    const other = "workspaces/ws/qualityRules/rule-2";
    const running = "workspaces/ws/qualityRules/rule-3";
    mocks.listQualityRules.mockResolvedValue({
      items: [
        rule(RULE, "Null rate on orders.id"),
        rule(other, "Orders have rows"),
        rule(running, "Orders are fresh"),
      ],
      nextPageToken: "",
    });
    mocks.listQualityExecutions.mockResolvedValue({
      items: [
        executionOf(other, "exec-other-newest", { minute: 3 }),
        executionOf(RULE, "exec-this", { minute: 2 }),
        executionOf(other, "exec-other-older", { minute: 1 }),
        create(QualityExecutionSchema, {
          name: "workspaces/ws/qualityExecutions/exec-running",
          rule: running,
          state: QualityExecutionState.RUNNING,
          outcome: QualityStatus.UNSPECIFIED,
        }),
      ],
      nextPageToken: "",
    });
    const wrapper = await mountSection(["metaxisdata.dataQuality.rules.list"]);

    const rows = checkRowLinks(wrapper);
    // The older run of the same check is folded into its newest run, and the
    // still-running check has no verdict yet.
    expect(rows).toHaveLength(2);
    expect(rows[0].text()).toContain("Orders have rows");
    expect(rows[1].text()).toContain("Null rate on orders.id");
    expect(wrapper.text()).not.toContain("Orders are fresh");
  });

  it("caps the checks preview at five rows", async () => {
    const names = Array.from(
      { length: 8 },
      (_, index) => `workspaces/ws/qualityRules/rule-${index}`
    );
    mocks.listQualityRules.mockResolvedValue({
      items: names.map((name, index) => rule(name, `Check ${index}`)),
      nextPageToken: "",
    });
    mocks.listQualityExecutions.mockResolvedValue({
      // Newest first, as the API returns them: rule-7 ran most recently.
      items: names
        .map((name, index) =>
          executionOf(name, `exec-${index}`, { minute: index })
        )
        .reverse(),
      nextPageToken: "",
    });
    const wrapper = await mountSection(["metaxisdata.dataQuality.rules.list"]);

    expect(checkRowLinks(wrapper)).toHaveLength(5);
    expect(wrapper.text()).toContain("Check 7");
    expect(wrapper.text()).not.toContain("Check 2");
  });

  it("explains an empty target with the existing empty states", async () => {
    mocks.listQualityRules.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    mocks.listQualityExecutions.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    const noChecks = await mountSection(["metaxisdata.dataQuality.rules.list"]);
    expect(noChecks.text()).toContain(label("dataQuality.asset.noRules"));
    noChecks.unmount();

    mocks.listQualityRules.mockResolvedValue({
      items: [rule(RULE, "Null rate on orders.id")],
      nextPageToken: "",
    });
    const neverRun = await mountSection(["metaxisdata.dataQuality.rules.list"]);
    expect(neverRun.text()).toContain(label("dataQuality.asset.noExecutions"));
  });

  it("toasts what the label pass created, refreshed and skipped", async () => {
    mocks.suggestColumnLabels.mockResolvedValue({
      qualitySuggestions: [],
      summary: create(QualitySuggestionQueueSummarySchema, {
        created: 2,
        refreshed: 1,
        skippedExistingFact: 3,
      }),
    });
    const success = vi.spyOn(notify, "success");
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    await wrapper
      .findAll("button")
      .find((item) =>
        item.text().includes(label("dataQuality.asset.suggestLabels"))
      )
      ?.trigger("click");
    await flushPromises();

    // The default action skips columns that already have a fact or a proposal.
    expect(mocks.suggestColumnLabels).toHaveBeenCalledWith({
      tableGuid: TABLE,
      metaType: MetaType.TABLE,
      columns: ["id", "email"],
      language: "en-US",
      forceRegenerate: false,
    });
    expect(success).toHaveBeenCalledWith(
      [
        label("dataQuality.suggestionSummary.created", { count: 2 }),
        label("dataQuality.suggestionSummary.refreshed", { count: 1 }),
        label("dataQuality.suggestionSummary.skipped", { count: 3 }),
      ].join(" · ")
    );
    success.mockRestore();
    wrapper.unmount();
  });

  it("explains why a label pass queued nothing", async () => {
    mocks.suggestColumnLabels.mockResolvedValue({
      qualitySuggestions: [],
      summary: create(QualitySuggestionQueueSummarySchema, {
        skippedExistingFact: 2,
      }),
    });
    const info = vi.spyOn(notify, "info");
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    await wrapper
      .findAll("button")
      .find((item) =>
        item.text().includes(label("dataQuality.asset.suggestLabels"))
      )
      ?.trigger("click");
    await flushPromises();

    expect(info).toHaveBeenCalledWith(
      label("dataQuality.suggestionSummary.emptyLabelExistingFact", {
        count: 2,
      })
    );
    info.mockRestore();
    wrapper.unmount();
  });

  it("offers a forced regeneration as the split action's second choice", async () => {
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    const trigger = wrapper.find(
      `[aria-label="${label("dataQuality.asset.suggestRegenerate")}"]`
    );
    expect(trigger.exists()).toBe(true);
    await trigger.trigger("click");
    await flushPromises();

    const item = document.body.querySelector<HTMLElement>('[role="menuitem"]');
    // The secondary action says it may refresh existing proposals.
    expect(item?.textContent).toContain(
      label("dataQuality.asset.suggestRegenerateHint")
    );
    item?.click();
    await flushPromises();

    expect(mocks.suggestColumnLabels).toHaveBeenCalledWith(
      expect.objectContaining({ forceRegenerate: true })
    );
    wrapper.unmount();
  });

  it("shows the table's pending label suggestions, and only when there are some", async () => {
    const empty = await mountSection(SUGGEST_PERMISSIONS);
    expect(empty.text()).not.toContain(
      label("dataQuality.asset.pendingSuggestionsTitle")
    );
    expect(mocks.listQualitySuggestions).toHaveBeenCalledWith({
      targetGuid: TABLE,
      status: QualitySuggestionStatus.PENDING,
      pageSize: 100,
    });
    empty.unmount();

    mocks.listQualitySuggestions.mockResolvedValue({
      items: [labelSuggestion()],
      nextPageToken: "",
    });
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    expect(wrapper.text()).toContain(
      label("dataQuality.asset.pendingSuggestionsTitle")
    );
    // The row names the column, the proposed label, its confidence and why.
    expect(wrapper.text()).toContain("email");
    expect(wrapper.text()).toContain(label("dataQuality.semanticType.email"));
    expect(wrapper.text()).toContain("85.0%");
    expect(wrapper.text()).toContain("Values look like addresses.");
    wrapper.unmount();
  });

  it("sends a check draft to the target workspace instead of the rule editor", async () => {
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [draftSuggestion()],
      nextPageToken: "",
    });
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    expect(wrapper.text()).toContain(
      label("dataQuality.asset.pendingSuggestionsTitle")
    );
    expect(wrapper.text()).toContain("Orders should have rows");
    expect(wrapper.text()).toContain(label("dataQuality.ruleType.rowCount"));
    expect(wrapper.text()).toContain(label("dataQuality.severity.error"));
    // The draft is examined in the workspace, where the inline composer lives.
    const openTarget = wrapper
      .findAll("a")
      .find((link) => link.text() === label("dataQuality.checks.openTarget"));
    expect(openTarget).toBeDefined();
    const query = queryOf(openTarget!.attributes("href") ?? "");
    expect(query.has("check")).toBe(false);
    expect(query.get("metaType")).toBe(String(MetaType.TABLE));
    expect(wrapper.find('a[href*="suggestion="]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("previews five pending proposals and points at the center for the rest", async () => {
    mocks.listQualitySuggestions.mockResolvedValue({
      items: Array.from({ length: 6 }, (_, index) =>
        labelSuggestion({
          name: `workspaces/ws/qualitySuggestions/label-${index}`,
          column: `column_${index}`,
          rationale: `Reason ${index}.`,
        })
      ),
      nextPageToken: "",
    });
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    expect(wrapper.text()).toContain("column_0");
    expect(wrapper.text()).toContain("column_4");
    expect(wrapper.text()).not.toContain("column_5");
    expect(wrapper.text()).toContain(
      label("dataQuality.asset.pendingSuggestionsMore", { count: 1 })
    );
    wrapper.unmount();
  });

  it("accepts a label suggestion in place and refreshes the labels", async () => {
    mocks.listQualitySuggestions
      .mockResolvedValueOnce({
        items: [labelSuggestion()],
        nextPageToken: "",
      })
      .mockResolvedValue({ items: [], nextPageToken: "" });
    const success = vi.spyOn(notify, "success");
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);
    mocks.listColumnLabels.mockClear();

    await wrapper
      .findAll("button")
      .find((item) => item.text() === label("dataQuality.suggestions.accept"))
      ?.trigger("click");
    await flushPromises();

    expect(mocks.updateQualitySuggestion).toHaveBeenCalledWith({
      name: LABEL_SUGGESTION,
      decision: QualitySuggestionStatus.ACCEPTED,
    });
    expect(success).toHaveBeenCalledWith(
      label("dataQuality.suggestions.accepted")
    );
    // Accepting writes the label server-side, so the table reloads it.
    expect(mocks.listColumnLabels).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).not.toContain(
      label("dataQuality.asset.pendingSuggestionsTitle")
    );
    success.mockRestore();
    wrapper.unmount();
  });

  it("rejects a label suggestion without touching the labels", async () => {
    mocks.listQualitySuggestions
      .mockResolvedValueOnce({
        items: [labelSuggestion()],
        nextPageToken: "",
      })
      .mockResolvedValue({ items: [], nextPageToken: "" });
    const success = vi.spyOn(notify, "success");
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);
    mocks.listColumnLabels.mockClear();

    await wrapper
      .findAll("button")
      .find((item) => item.text() === label("dataQuality.suggestions.reject"))
      ?.trigger("click");
    await flushPromises();

    expect(mocks.updateQualitySuggestion).toHaveBeenCalledWith({
      name: LABEL_SUGGESTION,
      decision: QualitySuggestionStatus.REJECTED,
    });
    expect(success).toHaveBeenCalledWith(
      label("dataQuality.suggestions.rejected")
    );
    expect(mocks.listColumnLabels).not.toHaveBeenCalled();
    success.mockRestore();
    wrapper.unmount();
  });

  it("shows proposals from the pass it just ran without leaving the tab", async () => {
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);
    expect(wrapper.text()).not.toContain(
      label("dataQuality.asset.pendingSuggestionsTitle")
    );

    mocks.suggestColumnLabels.mockResolvedValue({
      qualitySuggestions: [labelSuggestion()],
      summary: create(QualitySuggestionQueueSummarySchema, { created: 1 }),
    });
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [labelSuggestion()],
      nextPageToken: "",
    });

    await wrapper
      .findAll("button")
      .find((item) =>
        item.text().includes(label("dataQuality.asset.suggestLabels"))
      )
      ?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(
      label("dataQuality.asset.pendingSuggestionsTitle")
    );
    expect(wrapper.text()).toContain(label("dataQuality.semanticType.email"));
    wrapper.unmount();
  });

  it("reports a queue load failure instead of pretending the queue is empty", async () => {
    mocks.listQualitySuggestions.mockRejectedValue(new Error("offline"));
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    // The rest of the tab still renders from its own independent loads.
    expect(wrapper.text()).toContain("Null rate on orders.id");
    expect(wrapper.text()).toContain(
      label("dataQuality.asset.pendingSuggestionsLoadFailed")
    );
    expect(wrapper.text()).not.toContain(
      label("dataQuality.asset.pendingSuggestionsTitle")
    );
    wrapper.unmount();
  });

  it("marks a full queue page as truncated instead of understating it", async () => {
    mocks.listQualitySuggestions.mockResolvedValue({
      items: Array.from({ length: 6 }, (_, index) =>
        labelSuggestion({
          name: `workspaces/ws/qualitySuggestions/label-${index}`,
          column: `column_${index}`,
        })
      ),
      nextPageToken: "next",
    });
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    // The badge and the footer are lower bounds once the first page is full.
    expect(wrapper.text()).toContain("6+");
    expect(wrapper.text()).toContain(
      label("dataQuality.asset.pendingSuggestionsMore", { count: "1+" })
    );
    wrapper.unmount();
  });

  it("previews label proposals before check drafts", async () => {
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [
        draftSuggestion({ name: "draft-0", title: "Draft zero" }),
        draftSuggestion({ name: "draft-1", title: "Draft one" }),
        draftSuggestion({ name: "draft-2", title: "Draft two" }),
        labelSuggestion({ name: "label-0", column: "column_0" }),
        labelSuggestion({ name: "label-1", column: "column_1" }),
        labelSuggestion({ name: "label-2", column: "column_2" }),
      ],
      nextPageToken: "",
    });
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    const text = wrapper.text();
    // The three labels come first, so two draft slots remain in the preview.
    expect(text.indexOf("column_0")).toBeLessThan(text.indexOf("Draft zero"));
    expect(text).toContain("Draft one");
    expect(text).not.toContain("Draft two");
    expect(text).toContain(
      label("dataQuality.asset.pendingSuggestionsMore", { count: 1 })
    );
    wrapper.unmount();
  });

  it("rejects a check draft from the card", async () => {
    mocks.listQualitySuggestions
      .mockResolvedValueOnce({
        items: [draftSuggestion()],
        nextPageToken: "",
      })
      .mockResolvedValue({ items: [], nextPageToken: "" });
    const success = vi.spyOn(notify, "success");
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    await wrapper
      .findAll("button")
      .find((item) => item.text() === label("dataQuality.suggestions.reject"))
      ?.trigger("click");
    await flushPromises();

    expect(mocks.updateQualitySuggestion).toHaveBeenCalledWith({
      name: DRAFT_SUGGESTION,
      decision: QualitySuggestionStatus.REJECTED,
    });
    expect(success).toHaveBeenCalledWith(
      label("dataQuality.suggestions.rejected")
    );
    expect(wrapper.text()).not.toContain(
      label("dataQuality.asset.pendingSuggestionsTitle")
    );
    success.mockRestore();
    wrapper.unmount();
  });

  it("explains a failed decision in the card and reconciles the queue", async () => {
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [labelSuggestion()],
      nextPageToken: "",
    });
    mocks.updateQualitySuggestion.mockRejectedValue(
      new Error("already decided")
    );
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    await wrapper
      .findAll("button")
      .find((item) => item.text() === label("dataQuality.suggestions.accept"))
      ?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(
      label("dataQuality.suggestions.decideError")
    );
    // The row may already be decided elsewhere; the card re-reads the queue.
    expect(mocks.listQualitySuggestions).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("submits one decision when accept is clicked twice", async () => {
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [labelSuggestion()],
      nextPageToken: "",
    });
    let resolveUpdate: (value: unknown) => void = () => {};
    mocks.updateQualitySuggestion.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveUpdate = resolve;
        })
    );
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);
    const accept = wrapper
      .findAll("button")
      .find((item) => item.text() === label("dataQuality.suggestions.accept"));

    await accept?.trigger("click");
    // The row is disabled while the decision is in flight, so a second click
    // cannot start a second request.
    expect(accept?.attributes("disabled")).toBeDefined();
    await accept?.trigger("click");
    expect(mocks.updateQualitySuggestion).toHaveBeenCalledTimes(1);

    resolveUpdate(labelSuggestion());
    await flushPromises();
    wrapper.unmount();
  });
});
