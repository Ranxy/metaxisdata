import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import FormField from "@/components/ui/form-field/FormField.vue";
import { i18n } from "@/locales";
import {
  QualityAggregateFunction,
  QualityRuleType,
  QualityThresholdMetric,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  emptyConfig,
  type RuleConfigForm as RuleConfigState,
  THRESHOLD_METRIC_RULE_TYPES,
} from "@/utils/dataQualityForm";
import RuleConfigForm from "./RuleConfigForm.vue";

function label(key: string): string {
  return i18n.global.t(key);
}

function mountForm(
  ruleType: QualityRuleType,
  config: Partial<RuleConfigState> = {}
) {
  return mount(RuleConfigForm, {
    props: {
      ruleType,
      modelValue: { ...emptyConfig(), ...config },
      thresholdMetric: QualityThresholdMetric.COUNT,
    },
    global: { plugins: [i18n] },
  });
}

const THRESHOLD_LABEL = () => label("dataQuality.editor.thresholdMetric");

const NON_METRIC_TYPES = [
  QualityRuleType.ROW_COUNT,
  QualityRuleType.ROW_COUNT_CHANGE,
  QualityRuleType.NULL_PERCENT,
  QualityRuleType.FRESHNESS,
  QualityRuleType.SCHEMA_DRIFT,
  QualityRuleType.AGGREGATE,
  QualityRuleType.CROSS_TABLE,
  QualityRuleType.CUSTOM_SQL,
];

describe("RuleConfigForm", () => {
  for (const ruleType of THRESHOLD_METRIC_RULE_TYPES) {
    it(`shows the threshold unit select for ${QualityRuleType[ruleType]}`, () => {
      const wrapper = mountForm(ruleType);
      expect(wrapper.text()).toContain(THRESHOLD_LABEL());
      wrapper.unmount();
    });
  }

  // The unit is only meaningful where the rule has a total-rows denominator;
  // the server rejects PERCENT on every other type.
  for (const ruleType of NON_METRIC_TYPES) {
    it(`hides the threshold unit select for ${QualityRuleType[ruleType]}`, () => {
      const wrapper = mountForm(ruleType);
      expect(wrapper.text()).not.toContain(THRESHOLD_LABEL());
      wrapper.unmount();
    });
  }

  it("keeps custom SQL thresholds but hides the unit that has no denominator", () => {
    const wrapper = mountForm(QualityRuleType.CUSTOM_SQL);
    expect(wrapper.find("#dq-config-sql").exists()).toBe(true);
    expect(wrapper.text()).not.toContain(THRESHOLD_LABEL());
    const fields = wrapper
      .findAllComponents(FormField)
      .map((field) => field.props("label"));
    expect(fields).toContain(label("dataQuality.editor.failThreshold"));
    expect(fields).toContain(label("dataQuality.editor.warnThreshold"));
    wrapper.unmount();
  });

  it("renders the cross-table failure and warning thresholds", () => {
    const wrapper = mountForm(QualityRuleType.CROSS_TABLE, {
      rightTable: "public.orders_ref",
      leftAggFunction: QualityAggregateFunction.SUM,
      leftAggColumn: "amount",
    });
    const fields = wrapper
      .findAllComponents(FormField)
      .map((field) => field.props("label"));
    expect(fields).toContain(label("dataQuality.editor.failThreshold"));
    expect(fields).toContain(label("dataQuality.editor.warnThreshold"));
    expect(fields).toContain(label("dataQuality.editor.tolerance"));
    expect(wrapper.find("#dq-field-failThreshold").exists()).toBe(true);
    expect(wrapper.find("#dq-field-warnThreshold").exists()).toBe(true);
    expect(wrapper.text()).not.toContain(THRESHOLD_LABEL());
    wrapper.unmount();
  });

  describe("schema-drift snapshot rows", () => {
    const TARGET_COLUMNS = [
      { name: "id", type: "bigint", nullable: false },
      { name: "email", type: "text", nullable: true },
    ];

    function mountSchemaDrift(
      expectedColumns: Array<{
        name: string;
        type: string;
        nullable?: boolean;
      }>
    ) {
      const model = { ...emptyConfig(), expectedColumns };
      const wrapper = mount(RuleConfigForm, {
        props: {
          ruleType: QualityRuleType.SCHEMA_DRIFT,
          modelValue: model,
          thresholdMetric: QualityThresholdMetric.COUNT,
          columnDetails: TARGET_COLUMNS,
        },
        global: { plugins: [i18n] },
      });
      return { wrapper, model };
    }

    function nameInput(wrapper: ReturnType<typeof mount>) {
      return wrapper.find(
        `input[aria-label="${label("dataQuality.editor.columnName")}"]`
      );
    }

    function nameInputs(wrapper: ReturnType<typeof mount>) {
      return wrapper.findAll(
        `input[aria-label="${label("dataQuality.editor.columnName")}"]`
      );
    }

    function typeInput(wrapper: ReturnType<typeof mount>) {
      return wrapper.find(
        `input[aria-label="${label("dataQuality.editor.columnType")}"]`
      );
    }

    function rowCheckbox(wrapper: ReturnType<typeof mount>) {
      return wrapper.find('[role="checkbox"]');
    }

    /** The commit point the component resolves a manual row on. */
    async function commitName(wrapper: ReturnType<typeof mount>) {
      await nameInput(wrapper).trigger("blur");
    }

    it("shows an unset nullability as unchecked and only writes it on toggle", async () => {
      const { wrapper, model } = mountSchemaDrift([
        { name: "id", type: "bigint" },
      ]);
      expect(model.expectedColumns?.[0]?.nullable).toBeUndefined();
      expect(rowCheckbox(wrapper).attributes("aria-checked")).toBe("false");
      expect(wrapper.text()).toContain(
        label("dataQuality.editor.nullableHint")
      );

      await rowCheckbox(wrapper).trigger("click");
      expect(model.expectedColumns?.[0]?.nullable).toBe(true);
      wrapper.unmount();
    });

    it("fills a manual row's type and nullability from a matching target column", async () => {
      const { wrapper, model } = mountSchemaDrift([{ name: "", type: "" }]);
      await nameInput(wrapper).setValue("email");
      // Typing alone resolves nothing; blur is the commit point.
      expect(model.expectedColumns?.[0]?.type).toBe("");
      await commitName(wrapper);

      expect(model.expectedColumns?.[0]).toEqual({
        name: "email",
        type: "text",
        nullable: true,
      });
      expect(rowCheckbox(wrapper).attributes("aria-checked")).toBe("true");
      wrapper.unmount();
    });

    it("keeps a type the reader already typed when a name matches", async () => {
      const { wrapper, model } = mountSchemaDrift([
        { name: "", type: "custom_type" },
      ]);
      await nameInput(wrapper).setValue("id");
      await commitName(wrapper);

      expect(model.expectedColumns?.[0]?.type).toBe("custom_type");
      expect(model.expectedColumns?.[0]?.nullable).toBe(false);
      wrapper.unmount();
    });

    it("clears auto-filled values when the final name matches nothing", async () => {
      const { wrapper, model } = mountSchemaDrift([{ name: "", type: "" }]);
      await nameInput(wrapper).setValue("id");
      await commitName(wrapper);
      expect(model.expectedColumns?.[0]?.type).toBe("bigint");
      expect(model.expectedColumns?.[0]?.nullable).toBe(false);

      await nameInput(wrapper).setValue("idx");
      await commitName(wrapper);
      // `idx` matches no column, so the transient `id` expectations are gone.
      expect(model.expectedColumns?.[0]?.type).toBe("");
      expect(model.expectedColumns?.[0]?.nullable).toBeUndefined();
      wrapper.unmount();
    });

    it("keeps values the reader typed or toggled when the name matches nothing", async () => {
      const { wrapper, model } = mountSchemaDrift([{ name: "", type: "" }]);
      await nameInput(wrapper).setValue("id");
      await commitName(wrapper);

      await typeInput(wrapper).setValue("custom_type");
      await rowCheckbox(wrapper).trigger("click");
      await nameInput(wrapper).setValue("gone");
      await commitName(wrapper);

      expect(model.expectedColumns?.[0]?.type).toBe("custom_type");
      expect(model.expectedColumns?.[0]?.nullable).toBe(true);
      wrapper.unmount();
    });

    it("never overwrites a manual nullability toggle on a later name match", async () => {
      const { wrapper, model } = mountSchemaDrift([{ name: "", type: "" }]);
      await nameInput(wrapper).setValue("email");
      await commitName(wrapper);
      // The target says email is nullable; the reader says it is not.
      expect(model.expectedColumns?.[0]?.nullable).toBe(true);
      await rowCheckbox(wrapper).trigger("click");
      expect(model.expectedColumns?.[0]?.nullable).toBe(false);

      // Committing the same matching name again must not restore the target's bit.
      await nameInput(wrapper).setValue("email");
      await commitName(wrapper);
      expect(model.expectedColumns?.[0]?.nullable).toBe(false);
      wrapper.unmount();
    });

    it("does not clear values that fill-from-target recorded", async () => {
      const { wrapper, model } = mountSchemaDrift([]);
      const fill = wrapper
        .findAll("button")
        .find((button) =>
          button.text().includes(label("dataQuality.editor.fillFromTarget"))
        );
      expect(fill).toBeTruthy();
      await fill?.trigger("click");
      expect(model.expectedColumns).toHaveLength(2);

      const second = nameInputs(wrapper)[1];
      await second?.setValue("gone");
      await second?.trigger("blur");

      // Values the fill recorded are not auto-fill bookkeeping, so they survive.
      expect(model.expectedColumns?.[1]?.type).toBe("text");
      expect(model.expectedColumns?.[1]?.nullable).toBe(true);
      wrapper.unmount();
    });
  });
});
