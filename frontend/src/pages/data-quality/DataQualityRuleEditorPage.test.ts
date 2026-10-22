import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import RuleConfigForm from "@/components/data-quality/RuleConfigForm.vue";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityAggregateFunction,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleType,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  QualityTargetSchema,
  QualityThresholdMetric,
  RowCountConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { InstanceSchema } from "@/types/proto-es/v1/instance_service_pb";
import {
  DataQualitySettingSchema,
  WorkspaceProfileSettingSchema,
} from "@/types/proto-es/v1/setting_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { qualityRuleTypeKey } from "@/utils/dataQuality";
import { emptyRuleForm, formToRule } from "@/utils/dataQualityForm";
import DataQualityRuleEditorPage from "./DataQualityRuleEditorPage.vue";

// Radix opens a select on pointerdown and calls pointer-capture APIs that jsdom
// does not implement.
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.setPointerCapture ??= () => {};
Element.prototype.releasePointerCapture ??= () => {};

const mocks = vi.hoisted(() => ({
  createQualityRule: vi.fn(),
  getQualitySuggestion: vi.fn(),
  getQualityRule: vi.fn(),
  previewQualityRule: vi.fn(),
  updateQualityRule: vi.fn(),
  getWorkspaceProfileSetting: vi.fn(),
  listMetadata: vi.fn(),
  listAllInstances: vi.fn(),
  getInstance: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  createQualityRule: mocks.createQualityRule,
  getQualitySuggestion: mocks.getQualitySuggestion,
  getQualityRule: mocks.getQualityRule,
  previewQualityRule: mocks.previewQualityRule,
  updateQualityRule: mocks.updateQualityRule,
}));

vi.mock("@/api/setting", () => ({
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
}));

vi.mock("@/api/database", () => ({
  listMetadata: mocks.listMetadata,
}));

vi.mock("@/api/instance", () => ({
  listAllInstances: mocks.listAllInstances,
  getInstance: mocks.getInstance,
}));

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.dataQuality.rules.update",
  "metaxisdata.dataQuality.rules.list",
  // Preview is a `rules.run` permission on the service.
  "metaxisdata.dataQuality.rules.run",
  "metaxisdata.dataQuality.suggest",
];

const PREFILL = "inst1;db1;public;orders";
const PREVIEW = {
  compiledSql: "SELECT COUNT(*) FROM public.orders",
  referencedColumns: ["id"],
  parameters: [],
};

function label(key: string): string {
  return i18n.global.t(key);
}

async function mountEditor(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/data-quality/rules/new",
        name: "DataQualityRuleNew",
        component: DataQualityRuleEditorPage,
      },
      {
        path: "/data-quality/rules/:ruleId+/edit",
        name: "DataQualityRuleEdit",
        component: DataQualityRuleEditorPage,
      },
      {
        path: "/data-quality/rules",
        name: "DataQualityRules",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleDetail",
        component: { template: "<div />" },
      },
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
      {
        path: "/instances/:instanceId",
        name: "InstanceDetail",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push(path);
  await router.isReady();

  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });

  const wrapper = mount(DataQualityRuleEditorPage, {
    attachTo: document.body,
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

/** Pick a column in the searchable picker whose label matches `text`. */
function chooseColumn(
  wrapper: ReturnType<typeof mount>,
  text: string,
  value: string
) {
  const select = wrapper
    .findAllComponents(ColumnSelect)
    .find((candidate) => candidate.props("label") === text);
  return select?.vm.$emit("update:modelValue", value);
}

/** The input of the form field whose visible label matches `text`. */
function fieldInput(
  wrapper: ReturnType<typeof mount>,
  text: string
): ReturnType<ReturnType<typeof mount>["find"]> | undefined {
  const field = wrapper
    .findAllComponents(FormField)
    .find((candidate) => candidate.props("label") === text);
  return field?.find("input");
}

function actionButton(
  wrapper: ReturnType<typeof mount>,
  text: string
): ReturnType<ReturnType<typeof mount>["find"]> {
  const button = wrapper
    .findAll("button")
    .find((candidate) => candidate.text().includes(text));
  expect(button, `no button labelled ${text}`).toBeTruthy();
  return button as ReturnType<ReturnType<typeof mount>["find"]>;
}

/** Open the rule-type select and return the portalled option elements. */
async function openRuleTypeSelect(
  wrapper: ReturnType<typeof mount>
): Promise<HTMLElement[]> {
  const trigger = wrapper.find("#dq-rule-type");
  expect(trigger.exists(), "no rule-type trigger").toBe(true);
  await trigger.trigger("pointerdown");
  await nextTick();
  await nextTick();
  return Array.from(
    document.body.querySelectorAll<HTMLElement>('[role="option"]')
  );
}

/** The trimmed text of each option, in the order it renders. */
function optionLabels(options: HTMLElement[]): string[] {
  return options.map((option) => option.textContent?.trim() ?? "");
}

describe("DataQualityRuleEditorPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.createQualityRule.mockResolvedValue(
      create(QualityRuleSchema, { name: "workspaces/ws/qualityRules/rule-1" })
    );
    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        dataQuality: create(DataQualitySettingSchema, {
          enableLlmAnalysis: true,
        }),
      })
    );
    mocks.previewQualityRule.mockResolvedValue(PREVIEW);
    mocks.listMetadata.mockResolvedValue({
      typesStoredMetadata: [
        {
          metaType: MetaType.COLUMN,
          list: [
            {
              guid: `${PREFILL};id`,
              type: {
                case: "columnMetadata",
                value: { name: "id", type: "bigint", nullable: false },
              },
            },
          ],
        },
      ],
    });
    mocks.listAllInstances.mockResolvedValue([]);
    mocks.getInstance.mockResolvedValue(
      create(InstanceSchema, {
        name: "instances/inst1",
        dataSources: [
          {
            name: "instances/inst1/dataSources/readonly",
            type: 2,
          },
        ],
      })
    );
  });

  it("enables Draft with AI only with the switch and the permission", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    const draft = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(label("dataQuality.llm.draft.openButton"))
      );
    expect(draft).toBeTruthy();
    expect(draft?.attributes("disabled")).toBeUndefined();

    mocks.getWorkspaceProfileSetting.mockResolvedValue(
      create(WorkspaceProfileSettingSchema, {
        dataQuality: create(DataQualitySettingSchema, {
          enableLlmAnalysis: false,
        }),
      })
    );
    const { wrapper: disabled } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    const disabledDraft = disabled
      .findAll("button")
      .find((button) =>
        button.text().includes(label("dataQuality.llm.draft.openButton"))
      );
    expect(disabledDraft?.attributes("disabled")).toBeDefined();
    expect(disabled.text()).toContain(label("dataQuality.llm.disabledHint"));
  });

  it("prefills the target from the query so an asset page can link here", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );

    expect(wrapper.text()).toContain("inst1 · db1");
    expect(mocks.getQualityRule).not.toHaveBeenCalled();
  });

  // A rule type is a different config arm; switching must not carry the old
  // arm's values into the new one.
  it("resets the config when the rule type changes", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    const selects = wrapper.findAllComponents(Select);
    await selects[0]?.vm.$emit(
      "update:modelValue",
      String(QualityRuleType.CUSTOM_SQL)
    );
    await nextTick();

    const sql = wrapper.find("#dq-config-sql");
    expect(sql.exists()).toBe(true);
    await sql.setValue("SELECT 1");

    await selects[0]?.vm.$emit(
      "update:modelValue",
      String(QualityRuleType.ROW_COUNT)
    );
    await nextTick();
    expect(wrapper.find("#dq-config-sql").exists()).toBe(false);
    expect(wrapper.text()).toContain(label("dataQuality.editor.minRows"));

    await selects[0]?.vm.$emit(
      "update:modelValue",
      String(QualityRuleType.CUSTOM_SQL)
    );
    await nextTick();
    expect(
      (wrapper.find("#dq-config-sql").element as HTMLTextAreaElement).value
    ).toBe("");
  });

  it("offers every rule type in labelled groups for an unfiltered target", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    const options = await openRuleTypeSelect(wrapper);
    const labels = optionLabels(options);
    const expected = Object.values(QualityRuleType)
      .filter(
        (value): value is QualityRuleType =>
          typeof value === "number" && value !== QualityRuleType.UNSPECIFIED
      )
      .map((ruleType) => label(qualityRuleTypeKey(ruleType)));

    expect(options).toHaveLength(18);
    expect(new Set(labels)).toEqual(new Set(expected));
    expect(labels).not.toContain(label("dataQuality.ruleType.unspecified"));
    // The four family headings the select renders above the options.
    expect(document.body.querySelectorAll('[role="group"]')).toHaveLength(4);
    wrapper.unmount();
  });

  it("hides the types a view cannot watch and explains why", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=5`
    );
    await flushPromises();

    expect(wrapper.text()).toContain(
      label("dataQuality.editor.ruleTypeViewHint")
    );
    const options = await openRuleTypeSelect(wrapper);
    const labels = optionLabels(options);
    expect(options).toHaveLength(5);
    expect(new Set(labels)).toEqual(
      new Set([
        label("dataQuality.ruleType.rowCount"),
        label("dataQuality.ruleType.rowCountChange"),
        label("dataQuality.ruleType.freshness"),
        label("dataQuality.ruleType.customSql"),
        label("dataQuality.ruleType.schemaDrift"),
      ])
    );
    expect(labels).not.toContain(label("dataQuality.ruleType.unique"));
    expect(labels).not.toContain(label("dataQuality.ruleType.aggregate"));
    wrapper.unmount();
  });

  it("shows the selected rule type's description", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.ruleTypeDescription.rowCount")
    );

    await wrapper
      .findAllComponents(Select)[0]
      ?.vm.$emit("update:modelValue", String(QualityRuleType.AGGREGATE));
    await nextTick();
    expect(wrapper.text()).toContain(
      label("dataQuality.ruleTypeDescription.aggregate")
    );
    expect(wrapper.text()).not.toContain(
      label("dataQuality.ruleTypeDescription.rowCount")
    );
    wrapper.unmount();
  });

  it("resets a percentage threshold when the type no longer reads one", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    await wrapper
      .findAllComponents(Select)[0]
      ?.vm.$emit("update:modelValue", String(QualityRuleType.UNIQUE));
    await nextTick();

    wrapper
      .findComponent(RuleConfigForm)
      .vm.$emit("update:thresholdMetric", QualityThresholdMetric.PERCENT);
    await nextTick();
    expect(wrapper.findComponent(RuleConfigForm).props("thresholdMetric")).toBe(
      QualityThresholdMetric.PERCENT
    );
    expect(wrapper.find("#dq-rule-on-empty").exists()).toBe(true);

    await wrapper
      .findAllComponents(Select)[0]
      ?.vm.$emit("update:modelValue", String(QualityRuleType.ROW_COUNT));
    await nextTick();
    expect(wrapper.findComponent(RuleConfigForm).props("thresholdMetric")).toBe(
      QualityThresholdMetric.COUNT
    );
    // on_empty cannot change a row-count verdict, so its select is hidden.
    expect(wrapper.find("#dq-rule-on-empty").exists()).toBe(false);
    wrapper.unmount();
  });

  it("blocks Dry run on a missing required field without calling the server", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    await fieldInput(wrapper, label("dataQuality.editor.title"))?.setValue(
      "Row count"
    );

    await actionButton(wrapper, label("dataQuality.editor.dryRun")).trigger(
      "click"
    );
    await flushPromises();
    await nextTick();

    expect(mocks.previewQualityRule).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain(
      label("dataQuality.editor.missingRequiredTitle")
    );
    // The one-of message is shown both at the top and under the field.
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.schema.atLeastOne", {
        fields: [
          label("dataQuality.editor.minRows"),
          label("dataQuality.editor.maxRows"),
          label("dataQuality.editor.exactRows"),
        ].join(" / "),
      })
    );
    expect(wrapper.find("#dq-field-minRows").exists()).toBe(true);
    expect(document.activeElement?.id).toBe("dq-field-minRows");
  });

  it("runs the dry run once the required fields are filled", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    await fieldInput(wrapper, label("dataQuality.editor.title"))?.setValue(
      "Row count"
    );
    await fieldInput(wrapper, label("dataQuality.editor.minRows"))?.setValue(
      "1"
    );
    await flushPromises();
    await actionButton(wrapper, label("dataQuality.editor.dryRun")).trigger(
      "click"
    );
    await flushPromises();

    expect(mocks.previewQualityRule).toHaveBeenCalledTimes(1);
  });

  it("renders the compiled SQL and parameters of a failed dry run", async () => {
    mocks.previewQualityRule.mockRejectedValue(
      new ConnectError(
        "the rule's compiled query failed the dry run: expected 2 arguments, got 0 (compiled SQL: SELECT COUNT(*) FROM orders WHERE created_at > $1; parameters: [2026-01-01])",
        Code.InvalidArgument
      )
    );
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    await fieldInput(wrapper, label("dataQuality.editor.title"))?.setValue(
      "Row count"
    );
    await fieldInput(wrapper, label("dataQuality.editor.minRows"))?.setValue(
      "1"
    );
    await actionButton(wrapper, label("dataQuality.editor.dryRun")).trigger(
      "click"
    );
    await flushPromises();

    expect(wrapper.text()).toContain(
      "the rule's compiled query failed the dry run: expected 2 arguments, got 0"
    );
    expect(wrapper.text()).toContain("SELECT COUNT(*) FROM orders");
    expect(wrapper.text()).toContain("[2026-01-01]");
    expect(wrapper.text()).toContain(label("dataQuality.editor.compiledQuery"));
    expect(mocks.createQualityRule).not.toHaveBeenCalled();
  });

  it("shows the server's compile error inline and does not save", async () => {
    mocks.previewQualityRule.mockRejectedValue(
      new ConnectError("the filter column does not exist", Code.InvalidArgument)
    );
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    await fieldInput(wrapper, label("dataQuality.editor.title"))?.setValue(
      "Row count"
    );
    await fieldInput(wrapper, label("dataQuality.editor.minRows"))?.setValue(
      "1"
    );

    await actionButton(wrapper, label("dataQuality.editor.dryRun")).trigger(
      "click"
    );
    await flushPromises();

    expect(wrapper.text()).toContain("the filter column does not exist");
    expect(mocks.createQualityRule).not.toHaveBeenCalled();
  });

  it("marks a schema-required config field and its one-of hint", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    const selects = wrapper.findAllComponents(Select);
    await selects[0]?.vm.$emit(
      "update:modelValue",
      String(QualityRuleType.UNIQUE)
    );
    await nextTick();

    expect(wrapper.find("#dq-field-column").attributes("aria-required")).toBe(
      "true"
    );
    expect(wrapper.find('label[for="dq-field-column"]').text()).toContain("*");
  });

  it("creates the rule with the selected target and config", async () => {
    const { wrapper, router } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    await fieldInput(wrapper, label("dataQuality.editor.title"))?.setValue(
      "Row count"
    );
    await fieldInput(wrapper, label("dataQuality.editor.minRows"))?.setValue(
      "10"
    );

    await actionButton(wrapper, label("dataQuality.editor.create")).trigger(
      "click"
    );
    await flushPromises();

    expect(mocks.previewQualityRule).toHaveBeenCalledTimes(1);
    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    const rule = mocks.createQualityRule.mock.calls[0]?.[0];
    expect(rule.title).toBe("Row count");
    expect(rule.ruleType).toBe(QualityRuleType.ROW_COUNT);
    expect(rule.config.config.case).toBe("rowCount");
    expect(rule.config.config.value.minRows).toBe(10n);
    expect(rule.scheduleInterval.seconds).toBe(3_600n);
    expect(rule.target.metaGuid).toBe(PREFILL);
    expect(rule.target.metaType).toBe(MetaType.TABLE);
    expect(router.currentRoute.value.name).toBe("DataQualityRuleDetail");
    expect(router.currentRoute.value.params.ruleId).toEqual([
      "workspaces",
      "ws",
      "qualityRules",
      "rule-1",
    ]);
  });

  it("creates an aggregate rule with its bounds", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    const selects = wrapper.findAllComponents(Select);
    await selects[0]?.vm.$emit(
      "update:modelValue",
      String(QualityRuleType.AGGREGATE)
    );
    await nextTick();

    await fieldInput(wrapper, label("dataQuality.editor.title"))?.setValue(
      "Average amount"
    );
    chooseColumn(wrapper, label("dataQuality.editor.column"), "amount");
    await nextTick();
    await fieldInput(wrapper, label("dataQuality.editor.minValue"))?.setValue(
      "0"
    );
    await fieldInput(wrapper, label("dataQuality.editor.maxValue"))?.setValue(
      "100"
    );

    await actionButton(wrapper, label("dataQuality.editor.create")).trigger(
      "click"
    );
    await flushPromises();

    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    const rule = mocks.createQualityRule.mock.calls[0]?.[0];
    expect(rule.ruleType).toBe(QualityRuleType.AGGREGATE);
    expect(rule.config.config.case).toBe("aggregate");
    expect(rule.config.config.value.column).toBe("amount");
    expect(rule.config.config.value.min).toBe(0);
    expect(rule.config.config.value.max).toBe(100);
  });

  it("fills a schema-drift snapshot from the target's columns", async () => {
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    const selects = wrapper.findAllComponents(Select);
    await selects[0]?.vm.$emit(
      "update:modelValue",
      String(QualityRuleType.SCHEMA_DRIFT)
    );
    await nextTick();

    const fill = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(label("dataQuality.editor.fillFromTarget"))
      );
    expect(fill).toBeTruthy();
    await fill?.trigger("click");
    await nextTick();

    expect(
      wrapper.findAll("input").some((input) => input.element.value === "id")
    ).toBe(true);
    expect(
      wrapper.findAll("input").some((input) => input.element.value === "bigint")
    ).toBe(true);
  });

  it("prefills the form from a suggested rule draft", async () => {
    mocks.getQualitySuggestion.mockResolvedValue(
      create(QualitySuggestionSchema, {
        name: "workspaces/ws/qualitySuggestions/draft-1",
        kind: QualitySuggestionKind.RULE_DRAFT,
        source: QualitySuggestionSource.NATURAL_LANGUAGE,
        status: QualitySuggestionStatus.PENDING,
        target: create(QualityTargetSchema, {
          metaGuid: PREFILL,
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
      })
    );

    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?suggestion=${encodeURIComponent("workspaces/ws/qualitySuggestions/draft-1")}`
    );

    expect(mocks.getQualitySuggestion).toHaveBeenCalledWith(
      "workspaces/ws/qualitySuggestions/draft-1"
    );
    expect(
      (wrapper.find("#dq-rule-title").element as HTMLInputElement).value
    ).toBe("Orders should have rows");
    expect(
      (
        fieldInput(wrapper, label("dataQuality.editor.minRows"))
          ?.element as HTMLInputElement
      ).value
    ).toBe("1");
    expect(wrapper.text()).toContain(
      label("dataQuality.editor.suggestionPrefilled")
    );
    // The target came from the suggestion, not from a query parameter.
    expect(wrapper.text()).toContain(PREFILL);
  });

  it("creates the rule and accepts the source suggestion in one request", async () => {
    mocks.getQualitySuggestion.mockResolvedValue(
      create(QualitySuggestionSchema, {
        name: "workspaces/ws/qualitySuggestions/draft-1",
        kind: QualitySuggestionKind.RULE_DRAFT,
        target: create(QualityTargetSchema, {
          metaGuid: PREFILL,
          metaType: MetaType.TABLE,
        }),
        payload: {
          payload: {
            case: "rule",
            value: {
              title: "Orders should have rows",
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
      })
    );
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?suggestion=${encodeURIComponent("workspaces/ws/qualitySuggestions/draft-1")}`
    );

    await actionButton(wrapper, label("dataQuality.editor.create")).trigger(
      "click"
    );
    await flushPromises();

    // One request both creates the rule and decides the suggestion: the server
    // takes the target from the suggestion and marks it accepted.
    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    expect(mocks.createQualityRule).toHaveBeenCalledWith(
      expect.objectContaining({ title: "Orders should have rows" }),
      "workspaces/ws/qualitySuggestions/draft-1"
    );
    wrapper.unmount();
  });

  it("detaches the source suggestion when the reader picks another target", async () => {
    mocks.getQualitySuggestion.mockResolvedValue(
      create(QualitySuggestionSchema, {
        name: "workspaces/ws/qualitySuggestions/draft-1",
        kind: QualitySuggestionKind.RULE_DRAFT,
        target: create(QualityTargetSchema, {
          metaGuid: PREFILL,
          metaType: MetaType.TABLE,
        }),
        payload: {
          payload: {
            case: "rule",
            value: {
              title: "Orders should have rows",
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
      })
    );
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?suggestion=${encodeURIComponent("workspaces/ws/qualitySuggestions/draft-1")}`
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.editor.suggestionPrefilled")
    );

    const NEW_TARGET = "inst2;db2;public;customers";
    wrapper.findComponent({ name: "TargetPicker" }).vm.$emit("select", {
      guid: NEW_TARGET,
      metaType: MetaType.TABLE,
      path: {
        instance: "inst2",
        database: "db2",
        schema: "public",
        object: "customers",
      },
      columns: [],
    });
    await flushPromises();

    // A draft names one target: once the target changes the save must create a
    // plain rule for the new one, leaving the draft pending for the queue.
    expect(wrapper.text()).not.toContain(
      label("dataQuality.editor.suggestionPrefilled")
    );

    await actionButton(wrapper, label("dataQuality.editor.create")).trigger(
      "click"
    );
    await flushPromises();

    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    const [rule, suggestionName] = mocks.createQualityRule.mock.calls[0] ?? [];
    expect(suggestionName).toBe("");
    expect((rule as { target?: { metaGuid?: string } }).target?.metaGuid).toBe(
      NEW_TARGET
    );
    wrapper.unmount();
  });

  it("leaves the suggestion pending when the editor is cancelled", async () => {
    mocks.getQualitySuggestion.mockResolvedValue(
      create(QualitySuggestionSchema, {
        name: "workspaces/ws/qualitySuggestions/draft-1",
        kind: QualitySuggestionKind.RULE_DRAFT,
        target: create(QualityTargetSchema, {
          metaGuid: PREFILL,
          metaType: MetaType.TABLE,
        }),
        payload: {
          payload: {
            case: "rule",
            value: {
              title: "Orders should have rows",
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
      })
    );
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?suggestion=${encodeURIComponent("workspaces/ws/qualitySuggestions/draft-1")}`
    );

    await actionButton(wrapper, label("common.cancel")).trigger("click");
    await flushPromises();

    // Cancelling creates nothing and decides nothing.
    expect(mocks.createQualityRule).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("warns about a missing READ_ONLY data source on the target card", async () => {
    mocks.getInstance.mockResolvedValue(
      create(InstanceSchema, {
        name: "instances/inst1",
        dataSources: [],
      })
    );
    const { wrapper } = await mountEditor(
      `/data-quality/rules/new?table_guid=${encodeURIComponent(PREFILL)}&meta_type=4`
    );
    await flushPromises();
    await nextTick();

    expect(wrapper.text()).toContain(
      label("dataQuality.editor.readOnlyMissingTitle")
    );
    wrapper.unmount();
  });

  it("updates an edited rule with the minimal mask", async () => {
    const baseForm = emptyRuleForm(QualityRuleType.ROW_COUNT);
    baseForm.title = "Row count";
    baseForm.config.minRows = "10";
    const stored = formToRule(baseForm, {
      target: {
        guid: PREFILL,
        metaType: MetaType.TABLE,
        path: {
          instance: "inst1",
          database: "db1",
          schema: "public",
          object: "orders",
        },
        columns: [],
      },
    });
    stored.name = "workspaces/ws/qualityRules/rule-1";
    mocks.getQualityRule.mockResolvedValue(stored);
    mocks.updateQualityRule.mockResolvedValue(stored);

    const { wrapper } = await mountEditor(
      "/data-quality/rules/workspaces/ws/qualityRules/rule-1/edit"
    );
    expect(mocks.getQualityRule).toHaveBeenCalledWith(
      "workspaces/ws/qualityRules/rule-1"
    );
    expect(
      (wrapper.find("#dq-rule-title").element as HTMLInputElement).value
    ).toBe("Row count");

    await wrapper.find("#dq-rule-title").setValue("Row count on orders");
    await flushPromises();
    await nextTick();
    await actionButton(
      wrapper,
      label("dataQuality.editor.saveChanges")
    ).trigger("click");
    await flushPromises();

    expect(mocks.updateQualityRule).toHaveBeenCalledTimes(1);
    const [, mask] = mocks.updateQualityRule.mock.calls[0] ?? [];
    expect(mask).toEqual(["title"]);
  });

  it("resolves an unset schema-drift nullability from the loaded target columns", async () => {
    const baseForm = emptyRuleForm(QualityRuleType.SCHEMA_DRIFT);
    baseForm.title = "Schema drift";
    baseForm.config.expectedColumns = [
      { name: "id", type: "bigint" },
      { name: "legacy_col", type: "text" },
    ];
    const stored = formToRule(baseForm, {
      target: {
        guid: PREFILL,
        metaType: MetaType.TABLE,
        path: {
          instance: "inst1",
          database: "db1",
          schema: "public",
          object: "orders",
        },
        columns: [],
      },
    });
    stored.name = "workspaces/ws/qualityRules/rule-1";
    mocks.getQualityRule.mockResolvedValue(stored);
    mocks.updateQualityRule.mockResolvedValue(stored);

    const { wrapper } = await mountEditor(
      "/data-quality/rules/workspaces/ws/qualityRules/rule-1/edit"
    );
    await flushPromises();

    const expected = wrapper
      .findComponent(RuleConfigForm)
      .props("modelValue").expectedColumns;
    // The mock's target column "id" is nullable=false.
    expect(expected?.[0]?.nullable).toBe(false);
    // A column the target no longer has stays unjudged instead of inventing a bit.
    expect(expected?.[1]?.nullable).toBeUndefined();

    // Saving without edits writes nothing: the baseline was resolved the same way.
    await actionButton(
      wrapper,
      label("dataQuality.editor.saveChanges")
    ).trigger("click");
    await flushPromises();
    expect(mocks.previewQualityRule).toHaveBeenCalledTimes(1);
    expect(mocks.updateQualityRule).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("keeps a stored type outside a view's options visible instead of rewriting it", async () => {
    const baseForm = emptyRuleForm(QualityRuleType.AGGREGATE);
    baseForm.title = "Average amount";
    baseForm.config.aggregateColumn = "amount";
    baseForm.config.aggregateFunction = QualityAggregateFunction.AVG;
    baseForm.config.aggMin = "0";
    baseForm.config.aggMax = "100";
    const stored = formToRule(baseForm, {
      target: {
        guid: PREFILL,
        metaType: MetaType.VIEW,
        path: {
          instance: "inst1",
          database: "db1",
          schema: "public",
          object: "orders",
        },
        columns: [],
      },
    });
    stored.name = "workspaces/ws/qualityRules/rule-1";
    mocks.getQualityRule.mockResolvedValue(stored);

    const { wrapper } = await mountEditor(
      "/data-quality/rules/workspaces/ws/qualityRules/rule-1/edit"
    );
    await flushPromises();

    // The stored value is kept, not silently rewritten to a view-legal type.
    expect(wrapper.findAllComponents(Select)[0]?.props("modelValue")).toBe(
      String(QualityRuleType.AGGREGATE)
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.editor.ruleTypeViewHint")
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.ruleTypeDescription.aggregate")
    );

    // The fallback entry renders with the five view-legal options.
    const options = await openRuleTypeSelect(wrapper);
    const labels = optionLabels(options);
    expect(options).toHaveLength(6);
    expect(labels).toContain(label("dataQuality.ruleType.aggregate"));
    expect(mocks.updateQualityRule).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
