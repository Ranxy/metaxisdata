import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import UserSelect from "@/components/common/UserSelect.vue";
import RuleConfigForm from "@/components/data-quality/RuleConfigForm.vue";
import RuleFilterEditor from "@/components/data-quality/RuleFilterEditor.vue";
import { Select } from "@/components/ui/select";
import { notify } from "@/lib/notify";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import { useInstanceStore } from "@/store/modules/instance";
import { Engine } from "@/types/proto-es/v1/common_pb";
import {
  QualityComparison,
  QualityEmptyBehavior,
  QualityFilterOperator,
  type QualityRule,
  QualityRuleConfigSchema,
  QualityRuleSamplingSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleTrendSchema,
  QualityRuleType,
  QualitySeverity,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
  QualityThresholdSuggestionMethod,
  QualityThresholdSuggestionSchema,
  QualityThresholdsSchema,
  QualityTrendUnit,
  RowCountChangeConfigSchema,
  RowCountConfigSchema,
  UniqueConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { InstanceSchema } from "@/types/proto-es/v1/instance_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import {
  qualityRuleTypeDescriptionKey,
  qualityRuleTypeKey,
} from "@/utils/dataQuality";
import type { TargetColumnDetail } from "@/utils/dataQualityForm";
import TargetRuleComposer from "./TargetRuleComposer.vue";

// Radix opens a select on pointerdown and calls pointer-capture APIs that jsdom
// does not implement.
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.setPointerCapture ??= () => {};
Element.prototype.releasePointerCapture ??= () => {};

const mocks = vi.hoisted(() => ({
  createQualityRule: vi.fn(),
  updateQualityRule: vi.fn(),
  previewQualityRule: vi.fn(),
  getQualityRuleTrend: vi.fn(),
  listAllMetadata: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  createQualityRule: mocks.createQualityRule,
  updateQualityRule: mocks.updateQualityRule,
  previewQualityRule: mocks.previewQualityRule,
  getQualityRuleTrend: mocks.getQualityRuleTrend,
}));

vi.mock("@/api/database", () => ({
  listAllMetadata: mocks.listAllMetadata,
}));

const TABLE = "inst1;db1;public;orders";
const CREATED = "workspaces/ws/qualityRules/rule-1";
const SUGGESTION = "workspaces/ws/qualitySuggestions/draft-1";
const TARGET_LABEL = "prod · shop · public · orders";

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.dataQuality.rules.update",
  "metaxisdata.dataQuality.rules.run",
];

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

/** Every control in the dialog lives in a portal on `document.body`. */
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

/** The check-type entries of the left catalogue, in render order. */
function typeButtons(): HTMLElement[] {
  return [
    ...document.body.querySelectorAll<HTMLElement>(
      '[role="dialog"] [data-rule-type]'
    ),
  ];
}

/** The check name a catalogue entry renders as (the description is beneath it). */
function typeButtonLabel(button: HTMLElement): string {
  return button.querySelector("span")?.textContent?.trim() ?? "";
}

/** The catalogue entry a check type renders as. */
function typeButton(ruleType: QualityRuleType): HTMLElement | undefined {
  return (
    document.body.querySelector<HTMLElement>(
      `[role="dialog"] [data-rule-type="${ruleType}"]`
    ) ?? undefined
  );
}

/** A stored rule as the page hands it to the composer for editing. */
function ruleFixture(
  options: {
    title?: string;
    ruleType?: QualityRuleType;
    state?: QualityRuleState;
    owner?: string;
    tags?: string[];
    minRows?: bigint;
    column?: string;
    config?: QualityRule["config"];
  } = {}
): QualityRule {
  const ruleType = options.ruleType ?? QualityRuleType.ROW_COUNT;
  const config =
    ruleType === QualityRuleType.UNIQUE
      ? {
          case: "unique" as const,
          value: create(UniqueConfigSchema, {
            column: options.column ?? "email",
          }),
        }
      : {
          case: "rowCount" as const,
          value: create(RowCountConfigSchema, {
            minRows: options.minRows ?? 1n,
          }),
        };
  return create(QualityRuleSchema, {
    name: CREATED,
    title: options.title ?? "Orders have rows",
    description: "The fact table should not go empty.",
    ruleType,
    state: options.state ?? QualityRuleState.ENABLED,
    severity: QualitySeverity.ERROR,
    owner: options.owner ?? "",
    tags: options.tags ?? [],
    target: create(QualityTargetSchema, {
      metaGuid: TABLE,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "inst1",
        database: "db1",
        schema: "public",
        object: "orders",
      }),
      // The API records the columns the config reads on the target snapshot, so
      // the fixture does too: a missing list would look like a target change.
      columns:
        ruleType === QualityRuleType.UNIQUE ? [options.column ?? "email"] : [],
    }),
    config: options.config ?? create(QualityRuleConfigSchema, { config }),
  });
}

/** A trend whose suggestion offers the full two-sided row-count range. */
function suggestedTrendFixture() {
  return create(QualityRuleTrendSchema, {
    rule: CREATED,
    unit: QualityTrendUnit.COUNT,
    points: [],
    suggestion: create(QualityThresholdSuggestionSchema, {
      sampleSize: 12,
      method: QualityThresholdSuggestionMethod.OBSERVED_RANGE,
      thresholds: create(QualityThresholdsSchema, {
        comparison: QualityComparison.OUTSIDE_RANGE,
        failValue: 10,
        failValueUpper: 100,
        warnValue: 20,
        warnValueUpper: 80,
      }),
    }),
  });
}

/** An AI draft for a row-count check on the same target. */
function suggestionFixture() {
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
          title: "Orders must have rows",
          rationale: "The nightly load can leave the table empty.",
          confidence: 0.8,
          severity: QualitySeverity.WARNING,
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

async function mountComposer(
  options: {
    metaType?: MetaType;
    permissions?: string[];
    defaultSeverity?: QualitySeverity;
    rule?: QualityRule;
    suggestion?: ReturnType<typeof suggestionFixture>;
    scope?: "dataset" | "column";
    initialColumn?: string;
    columnDetails?: TargetColumnDetail[];
    engine?: Engine;
  } = {}
) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, {
    permissions: options.permissions ?? PERMISSIONS,
  });
  // The editor reads the target's engine from the shared instance cache; seed it
  // so the dialog never reaches the API for a list the workspace already has.
  const instanceStore = useInstanceStore();
  instanceStore.instances = [
    create(InstanceSchema, {
      name: "instances/inst1",
      engine: options.engine ?? Engine.POSTGRES,
    }),
  ];
  instanceStore.loaded = true;
  const wrapper = mount(TargetRuleComposer, {
    attachTo: document.body,
    props: {
      open: true,
      targetGuid: TABLE,
      metaType: options.metaType ?? MetaType.TABLE,
      targetLabel: TARGET_LABEL,
      defaultSeverity: options.defaultSeverity,
      rule: options.rule,
      suggestion: options.suggestion,
      scope: options.scope,
      initialColumn: options.initialColumn,
      columnDetails: options.columnDetails,
    },
    global: { plugins: [i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

describe("TargetRuleComposer", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.listAllMetadata.mockResolvedValue([
      {
        guid: `${TABLE};email`,
        type: {
          case: "columnMetadata",
          value: { name: "email", type: "text", nullable: true },
        },
      },
    ]);
    mocks.previewQualityRule.mockResolvedValue({
      compiledSql: "SELECT count(*) FROM orders",
      referencedColumns: [],
      parameters: [],
    });
    mocks.getQualityRuleTrend.mockResolvedValue(suggestedTrendFixture());
    mocks.createQualityRule.mockResolvedValue(
      create(QualityRuleSchema, {
        name: CREATED,
        title: "Orders have rows",
        ruleType: QualityRuleType.ROW_COUNT,
        state: QualityRuleState.ENABLED,
      })
    );
  });

  it("names the target it is fixed to and loads its columns", async () => {
    const wrapper = await mountComposer();

    // The title carries the page's label, not a target the reader must pick.
    expect(document.body.textContent).toContain(
      label("dataQuality.composer.title", { target: TARGET_LABEL })
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.composer.description")
    );
    // Column details reach the config form's pickers; a failed read is silent.
    expect(mocks.listAllMetadata).toHaveBeenCalledWith({
      parentGuid: TABLE,
      metaType: MetaType.COLUMN,
    });
    wrapper.unmount();
  });

  it("lists every allowed check in its catalogue section", async () => {
    const wrapper = await mountComposer();
    await nextTick();

    const buttons = typeButtons();
    expect(buttons).toHaveLength(18);
    expect(buttons.map(typeButtonLabel)).toContain(
      label(qualityRuleTypeKey(QualityRuleType.ROW_COUNT))
    );
    // Each entry explains what the check observes.
    expect(document.body.textContent).toContain(
      label(qualityRuleTypeDescriptionKey(QualityRuleType.ROW_COUNT))
    );
    // All four sections have a member on a plain table.
    for (const group of [
      "commonGroup",
      "datasetGroup",
      "columnGroup",
      "advancedGroup",
    ]) {
      expect(document.body.textContent).toContain(
        label(`dataQuality.composer.${group}`)
      );
    }
    // The default check is the one the catalogue marks as selected.
    expect(
      typeButton(QualityRuleType.ROW_COUNT)?.getAttribute("aria-pressed")
    ).toBe("true");
    wrapper.unmount();
  });

  it("offers only the checks a view can watch", async () => {
    const wrapper = await mountComposer({ metaType: MetaType.VIEW });
    await nextTick();

    const buttons = typeButtons();
    expect(buttons).toHaveLength(5);
    expect(new Set(buttons.map(typeButtonLabel))).toEqual(
      new Set(
        [
          QualityRuleType.ROW_COUNT,
          QualityRuleType.ROW_COUNT_CHANGE,
          QualityRuleType.FRESHNESS,
          QualityRuleType.CUSTOM_SQL,
          QualityRuleType.SCHEMA_DRIFT,
        ].map((ruleType) => label(qualityRuleTypeKey(ruleType)))
      )
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.editor.ruleTypeViewHint")
    );
    // The sections with no member for a view are not rendered.
    expect(document.body.textContent).not.toContain(
      label("dataQuality.composer.columnGroup")
    );
    expect(document.body.textContent).not.toContain(
      label("dataQuality.composer.advancedGroup")
    );
    wrapper.unmount();
  });

  it("creates the rule on the fixed target and reports it to the page", async () => {
    const created = create(QualityRuleSchema, {
      name: CREATED,
      title: "Orders have rows",
      ruleType: QualityRuleType.ROW_COUNT,
      state: QualityRuleState.ENABLED,
    });
    mocks.createQualityRule.mockResolvedValue(created);
    const wrapper = await mountComposer();

    setInput("#dq-composer-title", "Orders have rows");
    setInput("#dq-field-minRows", "1");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    // Saving is two-step on purpose: the dry run compiles the rule first.
    expect(mocks.previewQualityRule).toHaveBeenCalledTimes(1);
    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    const [rule] = mocks.createQualityRule.mock.calls[0] as [QualityRule];
    expect(rule.title).toBe("Orders have rows");
    expect(rule.target?.metaGuid).toBe(TABLE);
    expect(rule.target?.metaType).toBe(MetaType.TABLE);
    expect(rule.state).toBe(QualityRuleState.ENABLED);
    expect(rule.config?.config.case).toBe("rowCount");

    // The page renders the created rule in place, so the dialog reports it.
    expect(wrapper.emitted("saved")).toEqual([[created]]);
    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    wrapper.unmount();
  });

  it("starts a new rule from the contract's default severity", async () => {
    const wrapper = await mountComposer({
      defaultSeverity: QualitySeverity.ERROR,
    });

    setInput("#dq-composer-title", "Orders have rows");
    setInput("#dq-field-minRows", "1");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    // The severity select's label only renders once its menu opens, so the saved
    // rule is the observable proof that the contract default applied.
    const [rule] = mocks.createQualityRule.mock.calls[0] as [QualityRule];
    expect(rule.severity).toBe(QualitySeverity.ERROR);
    // Scheduling belongs to the target, so the rule carries none.
    expect(rule).not.toHaveProperty("scheduleInterval");
    wrapper.unmount();
  });

  it("seeds a column check from the column the workspace named", async () => {
    const wrapper = await mountComposer({
      scope: "column",
      initialColumn: "email",
    });
    await nextTick();

    expect(
      typeButton(QualityRuleType.NULL_PERCENT)?.getAttribute("aria-pressed")
    ).toBe("true");
    // The named column reached the null-rate config, so the dialog opens on a
    // check the reader can save without retyping the column.
    setInput("#dq-composer-title", "Emails are present");
    setInput("#dq-field-maxNullPct", "5");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    const [rule] = mocks.createQualityRule.mock.calls[0] as [QualityRule];
    const config = rule.config?.config;
    expect(config?.case).toBe("nullPercent");
    expect((config?.value as { column?: string } | undefined)?.column).toBe(
      "email"
    );
    wrapper.unmount();
  });

  it("starts a dataset check from the dataset scope", async () => {
    const wrapper = await mountComposer({ scope: "dataset" });
    await nextTick();

    expect(
      typeButton(QualityRuleType.ROW_COUNT)?.getAttribute("aria-pressed")
    ).toBe("true");
    wrapper.unmount();
  });

  it("carries the named column across a check-type change", async () => {
    const wrapper = await mountComposer({
      scope: "column",
      initialColumn: "email",
    });
    await nextTick();

    // Switching to another single-column check keeps the column the row named.
    typeButton(QualityRuleType.UNIQUE)?.click();
    await flushPromises();
    setInput("#dq-composer-title", "Emails are unique");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    const [rule] = mocks.createQualityRule.mock.calls[0] as [QualityRule];
    const config = rule.config?.config;
    expect(config?.case).toBe("unique");
    expect((config?.value as { column?: string } | undefined)?.column).toBe(
      "email"
    );
    wrapper.unmount();
  });

  it("prefills from an AI draft and accepts it when creating", async () => {
    const wrapper = await mountComposer({ suggestion: suggestionFixture() });
    await nextTick();

    // The draft's title and check type are the form's.
    expect(
      document.querySelector<HTMLInputElement>("#dq-composer-title")?.value
    ).toBe("Orders must have rows");
    expect(
      typeButton(QualityRuleType.ROW_COUNT)?.getAttribute("aria-pressed")
    ).toBe("true");

    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    // One request creates the rule and accepts the suggestion it came from.
    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    const [rule, suggestionName] = mocks.createQualityRule.mock.calls[0] as [
      QualityRule,
      string,
    ];
    expect(suggestionName).toBe(SUGGESTION);
    expect(rule.title).toBe("Orders must have rows");
    expect(rule.target?.metaGuid).toBe(TABLE);
    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    wrapper.unmount();
  });

  it("skips the dry run when the caller may not run rules", async () => {
    const wrapper = await mountComposer({
      permissions: ["metaxisdata.dataQuality.rules.create"],
    });

    setInput("#dq-composer-title", "Orders have rows");
    setInput("#dq-field-minRows", "1");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    expect(mocks.previewQualityRule).not.toHaveBeenCalled();
    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("reports an incomplete form in place and sends nothing", async () => {
    const wrapper = await mountComposer();

    // A blank rule has no title and no row bound.
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    expect(mocks.previewQualityRule).not.toHaveBeenCalled();
    expect(mocks.createQualityRule).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain(
      label("dataQuality.editor.validationTitle")
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.editor.validation.titleRequired")
    );
    wrapper.unmount();
  });

  it("shows the server's rejection in place and keeps the dialog open", async () => {
    mocks.createQualityRule.mockRejectedValue(
      new ConnectError('column "nope" does not exist', Code.InvalidArgument)
    );
    const wrapper = await mountComposer();

    setInput("#dq-composer-title", "Orders have rows");
    setInput("#dq-field-minRows", "1");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.editor.saveFailed")
    );
    expect(document.body.textContent).toContain('column "nope" does not exist');
    // A rejected create is not a save: nothing is reported and the form stays.
    expect(wrapper.emitted("saved")).toBeUndefined();
    expect(wrapper.emitted("update:open")).toBeUndefined();
    wrapper.unmount();
  });

  it("edits the rule in place and sends only the changed fields", async () => {
    const original = ruleFixture();
    const updated = ruleFixture({ title: "Orders have rows, revised" });
    mocks.updateQualityRule.mockResolvedValue(updated);
    const wrapper = await mountComposer({ rule: original });

    // The dialog is fixed to the rule rather than to a blank create form.
    expect(document.body.textContent).toContain(
      label("dataQuality.composer.editTitle", { title: "Orders have rows" })
    );
    expect(dialogButton(label("dataQuality.editor.saveChanges"))).toBeDefined();

    setInput("#dq-composer-title", "Orders have rows, revised");
    setInput("#dq-field-minRows", "5");
    await flushPromises();
    dialogButton(label("dataQuality.editor.saveChanges"))?.click();
    await flushPromises();

    // The dry run still gates the write, exactly as it does for a new rule.
    expect(mocks.previewQualityRule).toHaveBeenCalledTimes(1);
    expect(mocks.updateQualityRule).toHaveBeenCalledTimes(1);
    const [rule, mask] = mocks.updateQualityRule.mock.calls[0] as [
      QualityRule,
      string[],
    ];
    expect(rule.name).toBe(original.name);
    expect(rule.title).toBe("Orders have rows, revised");
    expect(
      (rule.config?.config.value as { minRows?: bigint } | undefined)?.minRows
    ).toBe(5n);
    // Only what changed travels; target and state stay off the mask.
    expect(mask).toContain("title");
    expect(mask).toContain("config");
    expect(mask).not.toContain("target");
    expect(mask).not.toContain("state");

    expect(wrapper.emitted("saved")).toEqual([[updated]]);
    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    wrapper.unmount();
  });

  it("saves the filter, owner, tags and empty behaviour from the details", async () => {
    const original = ruleFixture({ ruleType: QualityRuleType.UNIQUE });
    mocks.updateQualityRule.mockResolvedValue(
      ruleFixture({ ruleType: QualityRuleType.UNIQUE })
    );
    const wrapper = await mountComposer({ rule: original });
    await nextTick();

    wrapper
      .findComponent(UserSelect)
      .vm.$emit("update:modelValue", "users/alice");
    wrapper
      .findComponent(RuleFilterEditor)
      .vm.$emit("update:modelValue", [
        { column: "region", op: QualityFilterOperator.EQ, values: "EU" },
      ]);
    const onEmpty = wrapper
      .findAllComponents(Select)
      .find((candidate) => candidate.find("#dq-composer-on-empty").exists());
    onEmpty?.vm.$emit("update:modelValue", String(QualityEmptyBehavior.FAIL));
    setInput("#dq-composer-tags", "nightly, core");
    await flushPromises();

    dialogButton(label("dataQuality.editor.saveChanges"))?.click();
    await flushPromises();

    expect(mocks.updateQualityRule).toHaveBeenCalledTimes(1);
    const [rule, mask] = mocks.updateQualityRule.mock.calls[0] as [
      QualityRule,
      string[],
    ];
    expect(rule.owner).toBe("users/alice");
    expect(rule.tags).toEqual(["nightly", "core"]);
    expect(rule.config?.onEmpty).toBe(QualityEmptyBehavior.FAIL);
    expect(rule.config?.filter?.clauses).toHaveLength(1);
    expect(rule.config?.filter?.clauses[0]?.column).toBe("region");
    expect(mask).toContain("owner");
    expect(mask).toContain("tags");
    expect(mask).toContain("config");
    wrapper.unmount();
  });

  it("saves the execution settings from the details", async () => {
    // A sampleable check type: a row count describes the whole table and has no
    // sampling control.
    const original = ruleFixture({ ruleType: QualityRuleType.UNIQUE });
    mocks.updateQualityRule.mockResolvedValue(
      ruleFixture({ ruleType: QualityRuleType.UNIQUE })
    );
    const wrapper = await mountComposer({ rule: original });
    await nextTick();

    setInput("#dq-config-timeout", "120");
    wrapper
      .findAllComponents(Select)
      .find((candidate) => candidate.find("#dq-config-sampling").exists())
      ?.vm.$emit("update:modelValue", "maxRows");
    await nextTick();
    setInput("#dq-config-sample-max-rows", "1000");
    await flushPromises();

    dialogButton(label("dataQuality.editor.saveChanges"))?.click();
    await flushPromises();

    expect(mocks.updateQualityRule).toHaveBeenCalledTimes(1);
    const [rule, mask] = mocks.updateQualityRule.mock.calls[0] as [
      QualityRule,
      string[],
    ];
    expect(rule.config?.timeoutSeconds).toBe(120);
    expect(rule.config?.sampling?.method.case).toBe("maxRows");
    expect(rule.config?.sampling?.method.value).toBe(1000n);
    expect(mask).toEqual(["config"]);
    wrapper.unmount();
  });

  it("keeps a stored percentage on a target that cannot express it", async () => {
    // MySQL has no sampling clause; the editor shows the method selected but
    // refuses the save until the reader picks another one.
    const stored = ruleFixture({ ruleType: QualityRuleType.UNIQUE });
    if (stored.config) {
      stored.config.sampling = create(QualityRuleSamplingSchema, {
        method: { case: "percent", value: 25 },
      });
    }
    const wrapper = await mountComposer({
      rule: stored,
      engine: Engine.MYSQL,
    });
    await nextTick();

    expect(document.body.textContent).toContain(
      label("dataQuality.editor.samplePercentUnsupported")
    );
    dialogButton(label("dataQuality.editor.saveChanges"))?.click();
    await flushPromises();
    expect(mocks.updateQualityRule).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("keeps the state, owner and tags a rule was saved with", async () => {
    const original = ruleFixture({
      state: QualityRuleState.PAUSED,
      owner: "users/alice",
      tags: ["nightly"],
    });
    mocks.updateQualityRule.mockResolvedValue(
      ruleFixture({ title: "Renamed", state: QualityRuleState.PAUSED })
    );
    const wrapper = await mountComposer({ rule: original });

    setInput("#dq-composer-title", "Renamed");
    await flushPromises();
    dialogButton(label("dataQuality.editor.saveChanges"))?.click();
    await flushPromises();

    const [rule, mask] = mocks.updateQualityRule.mock.calls[0] as [
      QualityRule,
      string[],
    ];
    expect(rule.state).toBe(QualityRuleState.PAUSED);
    expect(rule.owner).toBe("users/alice");
    expect(rule.tags).toEqual(["nightly"]);
    // A rename is a rename: nothing else is named by the mask.
    expect(mask).toEqual(["title"]);
    wrapper.unmount();
  });

  it("closes without a request when an edit changes nothing", async () => {
    const wrapper = await mountComposer({ rule: ruleFixture() });

    dialogButton(label("dataQuality.editor.saveChanges"))?.click();
    await flushPromises();

    expect(mocks.previewQualityRule).not.toHaveBeenCalled();
    expect(mocks.updateQualityRule).not.toHaveBeenCalled();
    expect(wrapper.emitted("saved")).toBeUndefined();
    // The dialog still closes: there is nothing to save, not an error.
    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    wrapper.unmount();
  });

  it("uses the columns the page hands down without a metadata read", async () => {
    const provided: TargetColumnDetail[] = [
      { name: "email", type: "text", nullable: true },
      { name: "region", type: "text", nullable: false },
    ];
    const wrapper = await mountComposer({
      scope: "column",
      initialColumn: "email",
      columnDetails: provided,
    });
    await nextTick();

    // The workspace already read the columns; the dialog must not ask again (a
    // VIEW would come back with none and lose the pickers).
    expect(mocks.listAllMetadata).not.toHaveBeenCalled();
    expect(
      wrapper.findComponent(RuleConfigForm).props("columnDetails")
    ).toEqual(provided);
    // The column picker of the check offers exactly the handed-down columns.
    expect(wrapper.findComponent(ColumnSelect).props("columns")).toEqual(
      provided
    );
    wrapper.unmount();
  });

  it("applies columns that arrive after the dialog opened", async () => {
    // The metadata read finds no registry column rows, like a view or an
    // external table; the workspace's list arrives a moment later.
    mocks.listAllMetadata.mockResolvedValue([]);
    const wrapper = await mountComposer();
    await flushPromises();
    expect(
      wrapper.findComponent(RuleConfigForm).props("columnDetails")
    ).toEqual([]);

    const provided: TargetColumnDetail[] = [
      { name: "region", type: "text", nullable: true },
    ];
    await wrapper.setProps({ columnDetails: provided });
    await flushPromises();

    expect(
      wrapper.findComponent(RuleConfigForm).props("columnDetails")
    ).toEqual(provided);
    wrapper.unmount();
  });

  it("falls back to the metadata read when no columns are handed down", async () => {
    const wrapper = await mountComposer();
    await flushPromises();

    expect(mocks.listAllMetadata).toHaveBeenCalledWith({
      parentGuid: TABLE,
      metaType: MetaType.COLUMN,
    });
    expect(
      wrapper.findComponent(RuleConfigForm).props("columnDetails")
    ).toEqual([{ name: "email", type: "text", nullable: true }]);
    wrapper.unmount();
  });

  it("falls back to the metadata read for an empty hand-down", async () => {
    const wrapper = await mountComposer({ columnDetails: [] });
    await flushPromises();

    expect(mocks.listAllMetadata).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });
  it("uses the saved check's trend to suggest thresholds, without saving", async () => {
    const success = vi.spyOn(notify, "success");
    const wrapper = await mountComposer({ rule: ruleFixture() });
    await flushPromises();

    // Editing asks for the check's own history; creating never does.
    expect(mocks.getQualityRuleTrend).toHaveBeenCalledWith(CREATED, {
      limit: 30,
      signal: expect.any(AbortSignal),
    });
    expect(document.body.textContent).toContain(
      label("dataQuality.trend.suggestion.title")
    );
    // Reading the suggestion is not applying it: the saved bound is still in
    // the form until the reader asks for the new one.
    expect(
      document.querySelector<HTMLInputElement>("#dq-field-minRows")?.value
    ).toBe("1");

    dialogButton(label("dataQuality.trend.suggestion.apply"))?.click();
    await flushPromises();

    // The suggestion only fills the form: both bounds are now typed in, and
    // nothing has been written to the server yet.
    expect(
      document.querySelector<HTMLInputElement>("#dq-field-minRows")?.value
    ).toBe("10");
    expect(
      document.querySelector<HTMLInputElement>("#dq-field-maxRows")?.value
    ).toBe("100");
    expect(mocks.updateQualityRule).not.toHaveBeenCalled();
    expect(success).toHaveBeenCalledWith(
      label("dataQuality.trend.suggestion.applied")
    );
    success.mockRestore();
    wrapper.unmount();
  });

  it("disables the suggestion when the edited form changes the measurement unit", async () => {
    const wrapper = await mountComposer({ rule: ruleFixture() });
    await flushPromises();

    // Switching the check type makes the saved history's unit unrelated to the
    // form being edited, so the panel refuses to copy its numbers in.
    typeButton(QualityRuleType.UNIQUE)?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.trend.suggestion.unitChanged")
    );
    const button = dialogButton(label("dataQuality.trend.suggestion.apply")) as
      | HTMLButtonElement
      | undefined;
    expect(button?.disabled).toBe(true);
    wrapper.unmount();
  });

  it("keeps an existing count-change check's saved empty behaviour", async () => {
    const original = ruleFixture({
      ruleType: QualityRuleType.ROW_COUNT_CHANGE,
      config: create(QualityRuleConfigSchema, {
        onEmpty: QualityEmptyBehavior.NOT_EVALUATED,
        config: {
          case: "rowCountChange",
          value: create(RowCountChangeConfigSchema, { maxIncreasePct: 10 }),
        },
      }),
    });
    const wrapper = await mountComposer({ rule: original });

    // The new-check default is PASS, but an edit must not rewrite what the check
    // already saved: renaming it sends the stored behaviour back unchanged.
    mocks.updateQualityRule.mockResolvedValue(original);
    setInput("#dq-composer-title", "Renamed");
    await flushPromises();
    dialogButton(label("dataQuality.editor.saveChanges"))?.click();
    await flushPromises();

    expect(mocks.updateQualityRule).toHaveBeenCalledTimes(1);
    const [saved] = mocks.updateQualityRule.mock.calls[0] as [QualityRule];
    expect(saved.config?.onEmpty).toBe(QualityEmptyBehavior.NOT_EVALUATED);
    wrapper.unmount();
  });

  it("starts a new count-change check with on_empty pass and explains why", async () => {
    const wrapper = await mountComposer();
    await nextTick();

    typeButton(QualityRuleType.ROW_COUNT_CHANGE)?.click();
    await flushPromises();

    // A count-change check's first run only records a baseline; the editor says
    // so next to the empty-result control instead of leaving the reader to
    // discover it from a NOT_EVALUATED verdict.
    expect(document.body.textContent).toContain(
      label("dataQuality.editor.onEmptyRowCountChangeHint")
    );

    setInput("#dq-composer-title", "Row count stays stable");
    setInput("#dq-field-maxIncreasePct", "10");
    await flushPromises();
    dialogButton(label("dataQuality.editor.create"))?.click();
    await flushPromises();

    expect(mocks.createQualityRule).toHaveBeenCalledTimes(1);
    const [rule] = mocks.createQualityRule.mock.calls[0] as [QualityRule];
    expect(rule.config?.config.case).toBe("rowCountChange");
    expect(rule.config?.onEmpty).toBe(QualityEmptyBehavior.PASS);
    wrapper.unmount();
  });
});
