import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
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
import { qualityRuleTypeKey } from "@/utils/dataQuality";
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

/** A percent-null check: a type where on_empty does change the verdict. */
function nullPercentRule(name = NULL_RULE) {
  return create(QualityRuleSchema, {
    name,
    title: "Email is never null",
    ruleType: RuleType.NULL_PERCENT,
    severity: QualitySeverity.ERROR,
    state: QualityRuleState.ENABLED,
    revision: 3,
    config: create(QualityRuleConfigSchema, {
      thresholdMetric: QualityThresholdMetric.COUNT,
      onEmpty: QualityEmptyBehavior.NOT_EVALUATED,
      config: {
        case: "nullPercent",
        value: create(NullPercentConfigSchema, {
          column: "email",
          maxNullPct: 5,
        }),
      },
    }),
  });
}

/** A regex check: one of the threshold-metric rule types. */
function regexRule(name = REGEX_RULE, revision = 7) {
  return create(QualityRuleSchema, {
    name,
    title: "Email matches the pattern",
    ruleType: RuleType.REGEX,
    severity: QualitySeverity.WARNING,
    state: QualityRuleState.PAUSED,
    revision,
    config: create(QualityRuleConfigSchema, {
      thresholdMetric: QualityThresholdMetric.PERCENT,
      onEmpty: QualityEmptyBehavior.PASS,
      config: {
        case: "regex",
        value: create(RegexConfigSchema, {
          column: "email",
          pattern: "^.+@.+$",
          failThreshold: 1,
          warnThreshold: 0.5,
        }),
      },
    }),
  });
}

/** A row-count check: on_empty is irrelevant and thresholds are not shared. */
function rowCountRule(name = ROW_COUNT_RULE) {
  return create(QualityRuleSchema, {
    name,
    title: "Orders have rows",
    ruleType: RuleType.ROW_COUNT,
    severity: QualitySeverity.ERROR,
    state: QualityRuleState.ENABLED,
    revision: 2,
    config: create(QualityRuleConfigSchema, {
      onEmpty: QualityEmptyBehavior.NOT_EVALUATED,
      config: {
        case: "rowCount",
        value: create(RowCountConfigSchema, { minRows: 1n }),
      },
    }),
  });
}

/** The picker is tested separately; this stub reports a member or the clears value. */
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

async function mountDialog(rules = [nullPercentRule(), regexRule()]) {
  const pinia = createPinia();
  setActivePinia(pinia);
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

  it("sends a narrow severity mask with every check's current revision", async () => {
    await mountDialog();

    dialogButtonByText(
      label("dataQuality.checks.batchEdit.submit", { count: 2 })
    )?.click();
    await flushPromises();

    expect(mocks.batchUpdateQualityRules).toHaveBeenCalledTimes(1);
    const requests = lastRequests();
    expect(requests).toHaveLength(2);
    expect(requests.map((request) => request.rule.name)).toEqual([
      NULL_RULE,
      REGEX_RULE,
    ]);
    expect(requests[0]?.rule.revision).toBe(3);
    expect(requests[1]?.rule.revision).toBe(7);
    expect(requests[0]?.rule.severity).toBe(QualitySeverity.WARNING);
    expect(requests[0]?.updateMask.paths).toEqual(["severity"]);
    // Only the field and the identity revision travel, not the whole definition.
    expect(requests[0]?.rule.config).toBeUndefined();
  });

  it("sends dimension, state and owner as their own mask", async () => {
    await mountDialog();
    await pick("dq-batch-edit-field", "dimension");
    await pick("dq-batch-edit-dimension", String(QualityDimension.ACCURACY));
    dialogButtonByText(
      label("dataQuality.checks.batchEdit.submit", { count: 2 })
    )?.click();
    await flushPromises();
    expect(lastRequests()[0]?.rule.dimension).toBe(QualityDimension.ACCURACY);
    expect(lastRequests()[0]?.updateMask.paths).toEqual(["dimension"]);
    wrapper?.unmount();
    document.body.innerHTML = "";

    mocks.batchUpdateQualityRules.mockClear();
    await mountDialog();
    await pick("dq-batch-edit-field", "state");
    await pick("dq-batch-edit-state", String(QualityRuleState.PAUSED));
    dialogButtonByText(
      label("dataQuality.checks.batchEdit.submit", { count: 2 })
    )?.click();
    await flushPromises();
    expect(lastRequests()[0]?.rule.state).toBe(QualityRuleState.PAUSED);
    expect(lastRequests()[0]?.updateMask.paths).toEqual(["state"]);
    wrapper?.unmount();
    document.body.innerHTML = "";

    mocks.batchUpdateQualityRules.mockClear();
    await mountDialog();
    await pick("dq-batch-edit-field", "owner");
    // Nothing picked yet: the empty model never means "clear every owner".
    expect(
      dialogButtonByText(
        label("dataQuality.checks.batchEdit.submit", { count: 2 })
      )?.hasAttribute("disabled")
    ).toBe(true);
    await document.querySelector<HTMLElement>(".owner-member-stub")?.click();
    await flushPromises();
    dialogButtonByText(
      label("dataQuality.checks.batchEdit.submit", { count: 2 })
    )?.click();
    await flushPromises();
    expect(lastRequests()[0]?.rule.owner).toBe("users/101");
    expect(lastRequests()[0]?.updateMask.paths).toEqual(["owner"]);
    wrapper?.unmount();
    document.body.innerHTML = "";

    // The explicit "unassigned" choice is the only way to clear an owner.
    mocks.batchUpdateQualityRules.mockClear();
    await mountDialog();
    await pick("dq-batch-edit-field", "owner");
    await document
      .querySelector<HTMLElement>(".owner-unassigned-stub")
      ?.click();
    await flushPromises();
    dialogButtonByText(
      label("dataQuality.checks.batchEdit.submit", { count: 2 })
    )?.click();
    await flushPromises();
    expect(lastRequests()[0]?.rule.owner).toBe("");
    expect(lastRequests()[0]?.updateMask.paths).toEqual(["owner"]);
  });

  it("rebuilds the config for on_empty and leaves inapplicable checks alone", async () => {
    await mountDialog([nullPercentRule(), rowCountRule()]);
    await pick("dq-batch-edit-field", "on_empty");

    // The row-count check is reported as not applicable before submitting.
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.notApplicableTitle")
    );
    expect(document.body.textContent).toContain("Orders have rows");
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.notApplicable", { count: 1 })
    );

    await pick("dq-batch-edit-on-empty", String(QualityEmptyBehavior.FAIL));
    dialogButtonByText(
      label("dataQuality.checks.batchEdit.submit", { count: 1 })
    )?.click();
    await flushPromises();

    const requests = lastRequests();
    expect(requests).toHaveLength(1);
    expect(requests[0]?.rule.name).toBe(NULL_RULE);
    expect(requests[0]?.rule.revision).toBe(3);
    expect(requests[0]?.updateMask.paths).toEqual(["config"]);
    expect(requests[0]?.rule.config?.onEmpty).toBe(QualityEmptyBehavior.FAIL);
    // The rest of the definition survives the config rebuild.
    expect(requests[0]?.rule.config?.config?.case).toBe("nullPercent");
    expect(requests[0]?.rule.config?.config?.value).toMatchObject({
      column: "email",
    });
    // The skipped rule stays visible in the result summary too.
    expect(document.body.textContent).toContain("Orders have rows");
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.notApplicable", { count: 1 })
    );
  });

  it("rebuilds the config for a threshold shared by one type", async () => {
    await mountDialog([regexRule(), regexRule(REGEX_RULE_TWO, 4)]);
    await pick("dq-batch-edit-field", "threshold");
    await pick("dq-batch-edit-metric", String(QualityThresholdMetric.PERCENT));
    setInput("#dq-batch-edit-fail-threshold", "2.5");
    setInput("#dq-batch-edit-warn-threshold", "1");
    await nextTick();
    await flushPromises();

    dialogButtonByText(
      label("dataQuality.checks.batchEdit.submit", { count: 2 })
    )?.click();
    await flushPromises();

    const requests = lastRequests();
    expect(requests).toHaveLength(2);
    expect(requests.map((request) => request.rule.revision)).toEqual([7, 4]);
    for (const request of requests) {
      expect(request.updateMask.paths).toEqual(["config"]);
      expect(request.rule.config?.thresholdMetric).toBe(
        QualityThresholdMetric.PERCENT
      );
      expect(request.rule.config?.config?.case).toBe("regex");
      expect(request.rule.config?.config?.value).toMatchObject({
        failThreshold: 2.5,
        warnThreshold: 1,
        column: "email",
      });
    }
  });

  it("disables threshold editing when the types differ or lack a threshold", async () => {
    // Mixed types: no single family to share.
    await mountDialog([nullPercentRule(), regexRule()]);
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.thresholdHint")
    );
    // The option is disabled in the selector, and the guard still refuses a
    // request if a caller sets the field some other way.
    await pick("dq-batch-edit-field", "threshold");
    const submit = dialogButtonByText(
      label("dataQuality.checks.batchEdit.submit", { count: 2 })
    );
    expect(submit?.hasAttribute("disabled")).toBe(true);
    wrapper?.unmount();
    document.body.innerHTML = "";

    // One shared type without a shared fail/warn threshold.
    await mountDialog([rowCountRule(), rowCountRule("rule-2")]);
    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.thresholdTypeHint", {
        type: label(qualityRuleTypeKey(RuleType.ROW_COUNT)),
      })
    );
  });

  it("reports per-item failures and keeps the failed check selected", async () => {
    mocks.batchUpdateQualityRules.mockResolvedValue([
      { rule: nullPercentRule(), error: "", name: NULL_RULE },
      {
        rule: undefined,
        error: "revision mismatch: expected 7, got 8",
        name: REGEX_RULE,
      },
    ]);
    await mountDialog();

    dialogButtonByText(
      label("dataQuality.checks.batchEdit.submit", { count: 2 })
    )?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.partial", {
        succeeded: 1,
        failed: 1,
      })
    );
    expect(document.body.textContent).toContain(
      "revision mismatch: expected 7, got 8"
    );
    // The failure is labelled from the echoed rule name, not the request index.
    expect(document.body.textContent).toContain("Email matches the pattern");

    dialogButtonByText(label("common.close"))?.click();
    await flushPromises();
    expect(wrapper?.emitted("done")).toContainEqual([[NULL_RULE]]);
  });

  it("keeps a whole-call failure next to the form", async () => {
    mocks.batchUpdateQualityRules.mockRejectedValue(new Error("network down"));
    await mountDialog();

    dialogButtonByText(
      label("dataQuality.checks.batchEdit.submit", { count: 2 })
    )?.click();
    await flushPromises();

    expect(document.body.textContent).toContain(
      label("dataQuality.checks.batchEdit.failed")
    );
    // Nothing was updated, so closing reports no success and the page keeps
    // its selection.
    dialogButtonByText(label("common.cancel"))?.click();
    await flushPromises();
    expect(wrapper?.emitted("done")).toBeUndefined();
  });
});
