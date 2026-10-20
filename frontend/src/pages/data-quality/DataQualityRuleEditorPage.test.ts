import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleType,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  QualityTargetSchema,
  RowCountConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { emptyRuleForm, formToRule } from "@/utils/dataQualityForm";
import DataQualityRuleEditorPage from "./DataQualityRuleEditorPage.vue";

const mocks = vi.hoisted(() => ({
  createQualityRule: vi.fn(),
  findQualitySuggestion: vi.fn(),
  getQualityRule: vi.fn(),
  previewQualityRule: vi.fn(),
  updateQualityRule: vi.fn(),
  listMetadata: vi.fn(),
  listAllInstances: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  createQualityRule: mocks.createQualityRule,
  findQualitySuggestion: mocks.findQualitySuggestion,
  getQualityRule: mocks.getQualityRule,
  previewQualityRule: mocks.previewQualityRule,
  updateQualityRule: mocks.updateQualityRule,
}));

vi.mock("@/api/database", () => ({
  listMetadata: mocks.listMetadata,
}));

vi.mock("@/api/instance", () => ({
  listAllInstances: mocks.listAllInstances,
}));

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.dataQuality.rules.update",
  "metaxisdata.dataQuality.rules.list",
  // Preview is a `rules.run` permission on the service.
  "metaxisdata.dataQuality.rules.run",
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
        path: "/data-quality/suggestions",
        name: "DataQualitySuggestions",
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
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
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

describe("DataQualityRuleEditorPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.createQualityRule.mockResolvedValue(create(QualityRuleSchema, {}));
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
    expect(router.currentRoute.value.name).toBe("DataQualityRules");
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
    await fieldInput(wrapper, label("dataQuality.editor.column"))?.setValue(
      "amount"
    );
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
    mocks.findQualitySuggestion.mockResolvedValue(
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

    expect(mocks.findQualitySuggestion).toHaveBeenCalledWith(
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
    await actionButton(
      wrapper,
      label("dataQuality.editor.saveChanges")
    ).trigger("click");
    await flushPromises();

    expect(mocks.updateQualityRule).toHaveBeenCalledTimes(1);
    const [, mask] = mocks.updateQualityRule.mock.calls[0] ?? [];
    expect(mask).toEqual(["title"]);
  });
});
