import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  NullPercentConfigSchema,
  QualityDimension,
  QualityEmptyBehavior,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualitySeverity,
  QualityThresholdMetric,
  RegexConfigSchema,
  RowCountConfigSchema,
  QualityRuleType as RuleType,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import BatchEditChecksDialog from "./BatchEditChecksDialog.vue";

const mocks = vi.hoisted(() => ({
  batchUpdateQualityRules: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  batchUpdateQualityRules: mocks.batchUpdateQualityRules,
}));

const NULL_RULE = "workspaces/ws/qualityRules/null-1";
const REGEX_RULE = "workspaces/ws/qualityRules/regex-1";
const REGEX_RULE_TWO = "workspaces/ws/qualityRules/regex-2";
const ROW_COUNT_RULE = "workspaces/ws/qualityRules/row-count-1";

interface RuleOptions {
  name: string;
  type?: RuleType;
  revision?: number;
  severity?: QualitySeverity;
  dimension?: QualityDimension;
  state?: QualityRuleState;
  owner?: string;
  metric?: QualityThresholdMetric;
  onEmpty?: QualityEmptyBehavior;
  fail?: number;
  warn?: number;
  maxNullPct?: number;
}

/** A stored check whose config matches its type, as the directory lists it. */
function makeRule(options: RuleOptions) {
  const type = options.type ?? RuleType.REGEX;
  const shared = {
    thresholdMetric: options.metric ?? QualityThresholdMetric.COUNT,
    onEmpty: options.onEmpty ?? QualityEmptyBehavior.NOT_EVALUATED,
  };
  const config =
    type === RuleType.ROW_COUNT
      ? create(QualityRuleConfigSchema, {
          ...shared,
          config: {
            case: "rowCount",
            value: create(RowCountConfigSchema, { minRows: 1n }),
          },
        })
      : type === RuleType.NULL_PERCENT
        ? create(QualityRuleConfigSchema, {
            ...shared,
            config: {
              case: "nullPercent",
              value: create(NullPercentConfigSchema, {
                column: "email",
                ...(options.maxNullPct === undefined
                  ? {}
                  : { maxNullPct: options.maxNullPct }),
              }),
            },
          })
        : create(QualityRuleConfigSchema, {
            ...shared,
            config: {
              case: "regex",
              value: create(RegexConfigSchema, {
                column: "email",
                pattern: "^.+@.+$",
                ...(options.fail === undefined
                  ? {}
                  : { failThreshold: options.fail }),
                ...(options.warn === undefined
                  ? {}
                  : { warnThreshold: options.warn }),
              }),
            },
          });
  return create(QualityRuleSchema, {
    name: options.name,
    title: options.name.split("/").pop(),
    ruleType: type,
    severity: options.severity ?? QualitySeverity.ERROR,
    dimension: options.dimension ?? QualityDimension.COMPLETENESS,
    state: options.state ?? QualityRuleState.ENABLED,
    owner: options.owner ?? "",
    revision: options.revision ?? 3,
    config,
  });
}

function nullPercentRule(name = NULL_RULE, options: Partial<RuleOptions> = {}) {
  return makeRule({ name, type: RuleType.NULL_PERCENT, ...options });
}

function regexRule(name = REGEX_RULE, options: Partial<RuleOptions> = {}) {
  return makeRule({ name, type: RuleType.REGEX, ...options });
}

function rowCountRule(name = ROW_COUNT_RULE) {
  return makeRule({ name, type: RuleType.ROW_COUNT });
}

/** The picker is tested separately; this stub reports a member or the clear value. */
const UserSelectStub = defineComponent({
  name: "UserSelect",
  props: {
    modelValue: String,
    id: String,
    label: String,
    hint: String,
    placeholder: String,
    noOwnerValue: String,
    noOwnerLabel: String,
    preloadedUsers: Array,
  },
  emits: ["update:modelValue"],
  setup(props, { emit }) {
    return () =>
      h("div", { class: "owner-stub" }, [
        h(
          "button",
          {
            class: "owner-member-stub",
            type: "button",
            onClick: () => emit("update:modelValue", "users/101"),
          },
          "member"
        ),
        h(
          "button",
          {
            class: "owner-unassigned-stub",
            type: "button",
            onClick: () =>
              emit("update:modelValue", props.noOwnerValue ?? "__unassigned__"),
          },
          "unassigned"
        ),
      ]);
  },
});

let wrapper: ReturnType<typeof mount> | undefined;

async function mountDialog(rules: ReturnType<typeof makeRule>[]) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, {
    permissions: ["metaxisdata.users.list"],
  });
  wrapper = mount(BatchEditChecksDialog, {
    attachTo: document.body,
    props: { open: true, rules },
    global: {
      plugins: [i18n, pinia],
      stubs: { UserSelect: UserSelectStub },
    },
  });
  await flushPromises();
  return wrapper;
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function dialogButtonByText(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.includes(text));
}

function submitButton(count = 1): HTMLElement | undefined {
  return dialogButtonByText(
    label("dataQuality.checks.batchEdit.submit", { count })
  );
}

function selectById(id: string) {
  return wrapper
    ?.findAllComponents(Select)
    .find((candidate) => candidate.find(`#${id}`).exists());
}

async function pick(id: string, value: string) {
  selectById(id)?.vm.$emit("update:modelValue", value);
  await nextTick();
  await flushPromises();
}

function setInput(selector: string, value: string) {
  const element = document.querySelector(selector) as HTMLInputElement;
  Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value"
  )?.set?.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

function inputValue(selector: string): string {
  return (document.querySelector(selector) as HTMLInputElement).value;
}

function lastRequests() {
  const calls = mocks.batchUpdateQualityRules.mock.calls;
  return calls[calls.length - 1]?.[0] as Array<{
    rule: {
      name: string;
      revision: number;
      severity: QualitySeverity;
      dimension: QualityDimension;
      state: QualityRuleState;
      owner: string;
      config?: {
        thresholdMetric: QualityThresholdMetric;
        onEmpty: QualityEmptyBehavior;
        config?: { case?: string; value?: Record<string, unknown> };
      };
    };
    updateMask: { paths: string[] };
  }>;
}

describe("BatchEditChecksDialog", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.batchUpdateQualityRules.mockResolvedValue([
      { rule: nullPercentRule(), error: "", name: NULL_RULE },
      { rule: regexRule(), error: "", name: REGEX_RULE },
    ]);
  });

  afterEach(() => {
    wrapper?.unmount();
    wrapper = undefined;
  });

  it("opens with no field chosen and cannot submit", async () => {
    await mountDialog([nullPercentRule(), regexRule()]);

    expect(document.querySelector("#dq-batch-edit-field")).not.toBeNull();
    expect(selectById("dq-batch-edit-field")?.props("modelValue")).toBe("");
    // No value editor is rendered until a field is chosen.
    expect(document.querySelector("#dq-batch-edit-severity")).toBeNull();
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);
    expect(mocks.batchUpdateQualityRules).not.toHaveBeenCalled();
  });

  it("prefills a uniform severity and requires a deliberate pick when mixed", async () => {
    // Mixed: submitting without choosing would silently pick one of the two.
    const mixed = await mountDialog([
      nullPercentRule(NULL_RULE, { severity: QualitySeverity.ERROR }),
      regexRule(REGEX_RULE, { severity: QualitySeverity.WARNING }),
    ]);
    await pick("dq-batch-edit-field", "severity");
    expect(selectById("dq-batch-edit-severity")?.props("modelValue")).toBe("");
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);
    mixed.unmount();
    document.body.innerHTML = "";

    // Uniform: the current severity is prefilled, so one click rewrites the
    // same value instead of downgrading the selection.
    mocks.batchUpdateQualityRules.mockClear();
    await mountDialog([
      nullPercentRule(NULL_RULE, { severity: QualitySeverity.ERROR }),
      regexRule(REGEX_RULE, { severity: QualitySeverity.ERROR }),
    ]);
    await pick("dq-batch-edit-field", "severity");
    expect(selectById("dq-batch-edit-severity")?.props("modelValue")).toBe(
      String(QualitySeverity.ERROR)
    );
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(false);
    submitButton(2)?.click();
    await flushPromises();

    const requests = lastRequests();
    expect(requests).toHaveLength(2);
    expect(requests.map((request) => request.rule.revision)).toEqual([3, 3]);
    expect(requests[0]?.rule.severity).toBe(QualitySeverity.ERROR);
    expect(requests[0]?.updateMask.paths).toEqual(["severity"]);
    // Only the field and the identity revision travel, not the whole definition.
    expect(requests[0]?.rule.config).toBeUndefined();
  });

  it("recomputes the value controls when the field changes", async () => {
    await mountDialog([
      regexRule(REGEX_RULE, {
        severity: QualitySeverity.ERROR,
        dimension: QualityDimension.ACCURACY,
        state: QualityRuleState.PAUSED,
      }),
      regexRule(REGEX_RULE_TWO, {
        severity: QualitySeverity.ERROR,
        dimension: QualityDimension.ACCURACY,
        state: QualityRuleState.PAUSED,
      }),
    ]);
    await pick("dq-batch-edit-field", "severity");
    expect(selectById("dq-batch-edit-severity")?.props("modelValue")).toBe(
      String(QualitySeverity.ERROR)
    );

    await pick("dq-batch-edit-field", "dimension");
    // The previous editor is gone and the new one is prefilled from the rules.
    expect(document.querySelector("#dq-batch-edit-severity")).toBeNull();
    expect(selectById("dq-batch-edit-dimension")?.props("modelValue")).toBe(
      String(QualityDimension.ACCURACY)
    );

    await pick("dq-batch-edit-field", "state");
    expect(selectById("dq-batch-edit-state")?.props("modelValue")).toBe(
      String(QualityRuleState.PAUSED)
    );
  });

  it("writes dimension, state and owner through their own masks", async () => {
    const dimensionDialog = await mountDialog([
      nullPercentRule(NULL_RULE, { dimension: QualityDimension.ACCURACY }),
      regexRule(REGEX_RULE, { dimension: QualityDimension.ACCURACY }),
    ]);
    await pick("dq-batch-edit-field", "dimension");
    submitButton(2)?.click();
    await flushPromises();
    expect(lastRequests()[0]?.rule.dimension).toBe(QualityDimension.ACCURACY);
    expect(lastRequests()[0]?.updateMask.paths).toEqual(["dimension"]);
    dimensionDialog.unmount();
    document.body.innerHTML = "";

    mocks.batchUpdateQualityRules.mockClear();
    const stateDialog = await mountDialog([
      regexRule(REGEX_RULE, { state: QualityRuleState.PAUSED }),
      regexRule(REGEX_RULE_TWO, { state: QualityRuleState.PAUSED }),
    ]);
    await pick("dq-batch-edit-field", "state");
    submitButton(2)?.click();
    await flushPromises();
    expect(lastRequests()[0]?.rule.state).toBe(QualityRuleState.PAUSED);
    expect(lastRequests()[0]?.updateMask.paths).toEqual(["state"]);
    stateDialog.unmount();
    document.body.innerHTML = "";

    // All-unassigned maps to the explicit sentinel, so it may be submitted.
    mocks.batchUpdateQualityRules.mockClear();
    const unassignedDialog = await mountDialog([
      regexRule(REGEX_RULE),
      regexRule(REGEX_RULE_TWO),
    ]);
    await pick("dq-batch-edit-field", "owner");
    expect(wrapper?.findComponent(UserSelectStub).props("modelValue")).toBe(
      "__unassigned__"
    );
    submitButton(2)?.click();
    await flushPromises();
    expect(lastRequests()[0]?.rule.owner).toBe("");
    expect(lastRequests()[0]?.updateMask.paths).toEqual(["owner"]);
    unassignedDialog.unmount();
    document.body.innerHTML = "";

    // Mixed owners stay unset until the operator picks a member explicitly.
    mocks.batchUpdateQualityRules.mockClear();
    await mountDialog([
      regexRule(REGEX_RULE, { owner: "users/101" }),
      regexRule(REGEX_RULE_TWO),
    ]);
    await pick("dq-batch-edit-field", "owner");
    expect(wrapper?.findComponent(UserSelectStub).props("modelValue")).toBe("");
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);
    await document.querySelector<HTMLElement>(".owner-member-stub")?.click();
    await flushPromises();
    submitButton(2)?.click();
    await flushPromises();
    expect(lastRequests()[0]?.rule.owner).toBe("users/101");
  });

  it("rebuilds the config for on_empty and leaves inapplicable checks alone", async () => {
    await mountDialog([nullPercentRule(), rowCountRule()]);
    await pick("dq-batch-edit-field", "on_empty");

    // The single applicable check's current behavior is prefilled.
    expect(selectById("dq-batch-edit-on-empty")?.props("modelValue")).toBe(
      String(QualityEmptyBehavior.NOT_EVALUATED)
    );
    // The row-count check is reported as not applicable before submitting.
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.notApplicableTitle")
    );
    expect(document.body.textContent).toContain("row-count-1");
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.notApplicable", { count: 1 })
    );

    await pick("dq-batch-edit-on-empty", String(QualityEmptyBehavior.FAIL));
    submitButton(1)?.click();
    await flushPromises();

    const requests = lastRequests();
    expect(requests).toHaveLength(1);
    expect(requests[0]?.rule.name).toBe(NULL_RULE);
    expect(requests[0]?.updateMask.paths).toEqual(["config"]);
    expect(requests[0]?.rule.config?.onEmpty).toBe(QualityEmptyBehavior.FAIL);
    // The rest of the definition survives the config rebuild.
    expect(requests[0]?.rule.config?.config?.case).toBe("nullPercent");
    expect(requests[0]?.rule.config?.config?.value).toMatchObject({
      column: "email",
    });
    // The skipped rule stays visible in the result summary too.
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.notApplicable", { count: 1 })
    );
  });

  it("prefills a uniform threshold and keeps blank warn values per rule", async () => {
    const uniform = await mountDialog([
      regexRule(REGEX_RULE, {
        metric: QualityThresholdMetric.PERCENT,
        fail: 1,
        warn: 0.5,
      }),
      regexRule(REGEX_RULE_TWO, {
        metric: QualityThresholdMetric.PERCENT,
        fail: 1,
        warn: 0.5,
        revision: 4,
      }),
    ]);
    await pick("dq-batch-edit-field", "threshold");
    expect(selectById("dq-batch-edit-metric")?.props("modelValue")).toBe(
      String(QualityThresholdMetric.PERCENT)
    );
    expect(inputValue("#dq-batch-edit-fail-threshold")).toBe("1");
    expect(inputValue("#dq-batch-edit-warn-threshold")).toBe("0.5");
    submitButton(2)?.click();
    await flushPromises();
    expect(lastRequests().map((request) => request.rule.revision)).toEqual([
      3, 4,
    ]);
    for (const request of lastRequests()) {
      expect(request.updateMask.paths).toEqual(["config"]);
      expect(request.rule.config?.thresholdMetric).toBe(
        QualityThresholdMetric.PERCENT
      );
      expect(request.rule.config?.config?.value).toMatchObject({
        failThreshold: 1,
        warnThreshold: 0.5,
      });
    }
    uniform.unmount();
    document.body.innerHTML = "";

    // A mixed warning threshold prefills blank, which means "keep each rule's
    // current value" rather than dropping it.
    mocks.batchUpdateQualityRules.mockClear();
    await mountDialog([
      regexRule(REGEX_RULE, {
        metric: QualityThresholdMetric.PERCENT,
        fail: 1,
        warn: 0.5,
      }),
      regexRule(REGEX_RULE_TWO, {
        metric: QualityThresholdMetric.PERCENT,
        fail: 1,
        revision: 4,
      }),
    ]);
    await pick("dq-batch-edit-field", "threshold");
    expect(inputValue("#dq-batch-edit-warn-threshold")).toBe("");
    submitButton(2)?.click();
    await flushPromises();
    const requests = lastRequests();
    expect(requests[0]?.rule.config?.config?.value).toMatchObject({
      failThreshold: 1,
      warnThreshold: 0.5,
    });
    expect(
      requests[1]?.rule.config?.config?.value?.warnThreshold
    ).toBeUndefined();
  });

  it("requires an explicit metric when the selection's metrics differ", async () => {
    await mountDialog([
      regexRule(REGEX_RULE, {
        metric: QualityThresholdMetric.COUNT,
        fail: 1,
      }),
      regexRule(REGEX_RULE_TWO, {
        metric: QualityThresholdMetric.PERCENT,
        fail: 1,
        revision: 4,
      }),
    ]);
    await pick("dq-batch-edit-field", "threshold");
    expect(selectById("dq-batch-edit-metric")?.props("modelValue")).toBe("");
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);

    await pick("dq-batch-edit-metric", String(QualityThresholdMetric.PERCENT));
    // The unit change clears the amounts read in the mixed units, so the
    // operator has to restate the failure threshold.
    expect(inputValue("#dq-batch-edit-fail-threshold")).toBe("");
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);
    setInput("#dq-batch-edit-fail-threshold", "1");
    await nextTick();
    await flushPromises();
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(false);
    submitButton(2)?.click();
    await flushPromises();
    expect(lastRequests()[0]?.rule.config?.thresholdMetric).toBe(
      QualityThresholdMetric.PERCENT
    );
  });

  it("requires an explicit failure threshold when the selection's values differ", async () => {
    await mountDialog([
      regexRule(REGEX_RULE, {
        metric: QualityThresholdMetric.PERCENT,
        fail: 1,
        warn: 0.5,
      }),
      regexRule(REGEX_RULE_TWO, {
        metric: QualityThresholdMetric.PERCENT,
        fail: 2,
        warn: 0.5,
        revision: 4,
      }),
    ]);
    await pick("dq-batch-edit-field", "threshold");
    // The fail values differ, so the operator has to supply the replacement;
    // the hint says why a warn-only edit still needs it.
    expect(inputValue("#dq-batch-edit-fail-threshold")).toBe("");
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.thresholdFailMixedHint")
    );
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);

    setInput("#dq-batch-edit-fail-threshold", "1.5");
    await nextTick();
    await flushPromises();
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(false);
    submitButton(2)?.click();
    await flushPromises();
    for (const request of lastRequests()) {
      expect(request.rule.config?.config?.value).toMatchObject({
        failThreshold: 1.5,
        warnThreshold: 0.5,
      });
    }
  });

  it("drops the prefilled amounts when the metric changes", async () => {
    await mountDialog([
      regexRule(REGEX_RULE, {
        metric: QualityThresholdMetric.PERCENT,
        fail: 1,
        warn: 0.5,
      }),
      regexRule(REGEX_RULE_TWO, {
        metric: QualityThresholdMetric.PERCENT,
        fail: 1,
        warn: 0.5,
        revision: 4,
      }),
    ]);
    await pick("dq-batch-edit-field", "threshold");
    expect(inputValue("#dq-batch-edit-fail-threshold")).toBe("1");
    expect(inputValue("#dq-batch-edit-warn-threshold")).toBe("0.5");

    // A unit change is deliberate; the old numbers must not be reinterpreted.
    await pick("dq-batch-edit-metric", String(QualityThresholdMetric.COUNT));
    expect(inputValue("#dq-batch-edit-fail-threshold")).toBe("");
    expect(inputValue("#dq-batch-edit-warn-threshold")).toBe("");
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);
  });

  it("does not enter threshold editing for mixed or unsupported types", async () => {
    await mountDialog([nullPercentRule(), regexRule(REGEX_RULE)]);
    // The menu item is disabled, but a caller could still force the model; the
    // submit guard must refuse it too.
    await pick("dq-batch-edit-field", "threshold");
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);
  });

  it("clears warning thresholds only when the clear affordance is ticked", async () => {
    await mountDialog([
      regexRule(REGEX_RULE, {
        metric: QualityThresholdMetric.COUNT,
        fail: 1,
        warn: 0.5,
      }),
      regexRule(REGEX_RULE_TWO, {
        metric: QualityThresholdMetric.COUNT,
        fail: 1,
        revision: 4,
      }),
    ]);
    await pick("dq-batch-edit-field", "threshold");
    // A mixed warning prefills blank (= keep); the checkbox is the only clear.
    expect(inputValue("#dq-batch-edit-warn-threshold")).toBe("");

    document.querySelector<HTMLElement>("#dq-batch-edit-clear-warn")?.click();
    await flushPromises();
    expect(
      document
        .querySelector("#dq-batch-edit-warn-threshold")
        ?.hasAttribute("disabled")
    ).toBe(true);
    submitButton(2)?.click();
    await flushPromises();
    for (const request of lastRequests()) {
      expect(request.rule.config?.config?.value?.warnThreshold).toBeUndefined();
    }
  });

  it("validates the threshold against the backend rules", async () => {
    await mountDialog([
      regexRule(REGEX_RULE, {
        metric: QualityThresholdMetric.PERCENT,
        fail: 1,
      }),
      regexRule(REGEX_RULE_TWO, {
        metric: QualityThresholdMetric.PERCENT,
        fail: 1,
        revision: 4,
      }),
    ]);
    await pick("dq-batch-edit-field", "threshold");

    setInput("#dq-batch-edit-fail-threshold", "150");
    await nextTick();
    await flushPromises();
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.thresholdPercentMax")
    );
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);

    setInput("#dq-batch-edit-fail-threshold", "-1");
    await nextTick();
    await flushPromises();
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.thresholdNumberInvalid")
    );
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);

    setInput("#dq-batch-edit-fail-threshold", "1");
    setInput("#dq-batch-edit-warn-threshold", "2");
    await nextTick();
    await flushPromises();
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.thresholdWarnAbove")
    );
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);

    setInput("#dq-batch-edit-fail-threshold", "");
    await nextTick();
    await flushPromises();
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.thresholdRequired")
    );
    expect(submitButton(2)?.hasAttribute("disabled")).toBe(true);
  });

  it("emits one refresh as soon as an all-fail batch returns", async () => {
    // An aborted item: someone else changed the definition first, and the new
    // revision must replace the stale row on the page behind the dialog.
    mocks.batchUpdateQualityRules.mockResolvedValue([
      {
        rule: undefined,
        error: 'aborted: quality rule "null-1" revision changed from 3 to 8',
        name: NULL_RULE,
      },
      {
        rule: undefined,
        error: 'not_found: quality rule "regex-1" not found',
        name: REGEX_RULE,
      },
      {
        rule: undefined,
        error: 'invalid_argument: unknown column "nope"',
        name: REGEX_RULE_TWO,
      },
    ]);
    await mountDialog([
      nullPercentRule(),
      regexRule(REGEX_RULE),
      regexRule(REGEX_RULE_TWO),
    ]);
    await pick("dq-batch-edit-field", "severity");
    submitButton(3)?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.partial", {
        succeeded: 0,
        failed: 3,
      })
    );
    // Raw wire codes become sentences; other codes keep just the server wording.
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.itemChanged")
    );
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.itemGone")
    );
    expect(document.body.textContent).toContain('unknown column "nope"');
    expect(document.body.textContent).not.toContain("invalid_argument:");
    expect(document.body.textContent).not.toContain("aborted:");

    // The refresh event fires with no succeeded names, immediately.
    expect(wrapper?.emitted("done")).toContainEqual([[]]);

    // Closing must not fire it a second time.
    dialogButtonByText(label("common.close"))?.click();
    await flushPromises();
    expect(wrapper?.emitted("done")).toHaveLength(1);
  });

  it("keeps a transport failure next to the form and does not refresh", async () => {
    mocks.batchUpdateQualityRules.mockRejectedValue(new Error("network down"));
    await mountDialog([
      nullPercentRule(),
      regexRule(REGEX_RULE, { severity: QualitySeverity.ERROR }),
    ]);
    await pick("dq-batch-edit-field", "severity");
    submitButton(2)?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.failed")
    );
    expect(wrapper?.emitted("done")).toBeUndefined();
    // The form is still there for a retry.
    expect(document.querySelector("#dq-batch-edit-severity")).not.toBeNull();
  });
});
