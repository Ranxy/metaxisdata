import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Select, SelectItem } from "@/components/ui/select";
import { i18n } from "@/locales";
import { Engine } from "@/types/proto-es/v1/common_pb";
import { QualityRuleType } from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  emptyConfig,
  MAX_SAMPLE_MAX_ROWS,
  type RuleConfigForm,
  type SampleMethod,
} from "@/utils/dataQualityForm";
import RuleExecutionFields from "./RuleExecutionFields.vue";

// Radix opens a select on pointerdown and calls pointer-capture APIs that jsdom
// does not implement.
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.setPointerCapture ??= () => {};
Element.prototype.releasePointerCapture ??= () => {};

function label(key: string): string {
  return i18n.global.t(key);
}

function mountFields(
  options: {
    ruleType?: QualityRuleType;
    engine?: Engine;
    metaType?: MetaType;
    config?: Partial<RuleConfigForm>;
  } = {}
) {
  const model: RuleConfigForm = { ...emptyConfig(), ...options.config };
  const wrapper = mount(RuleExecutionFields, {
    props: {
      ruleType: options.ruleType ?? QualityRuleType.UNIQUE,
      engine: options.engine ?? Engine.POSTGRES,
      metaType: options.metaType ?? MetaType.TABLE,
      modelValue: model,
    },
    global: { plugins: [i18n] },
  });
  return { wrapper, model };
}

function samplingSelect(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAllComponents(Select)
    .find((candidate) => candidate.find("#dq-config-sampling").exists());
}

/** The method options the select renders, in order, once it is open. */
async function methodOptions(wrapper: ReturnType<typeof mount>) {
  await wrapper.find("#dq-config-sampling").trigger("pointerdown");
  await flushPromises();
  return wrapper.findAllComponents(SelectItem).map((item) => ({
    value: String(item.props("value")),
    disabled: item.props("disabled") === true,
  }));
}

function fieldLabels(wrapper: ReturnType<typeof mount>): string[] {
  return wrapper
    .findAllComponents(FormField)
    .map((field) => String(field.props("label")));
}

describe("RuleExecutionFields", () => {
  it("edits the query timeout on its own", async () => {
    const { wrapper, model } = mountFields();
    expect(fieldLabels(wrapper)).toContain(
      label("dataQuality.editor.timeoutSeconds")
    );

    await wrapper.find("#dq-config-timeout").setValue("120");
    expect(model.timeoutSeconds).toBe("120");
    // The check's schedule belongs to the target, so the copy says so.
    expect(wrapper.text()).toContain(label("dataQuality.editor.executionHint"));
    wrapper.unmount();
  });

  it("offers no sampling by default and keeps it that way", () => {
    const { wrapper, model } = mountFields();
    expect(model.sampleMethod).toBe("none");
    expect(wrapper.text()).toContain(label("dataQuality.editor.sampling"));
    expect(wrapper.find("#dq-config-sampling").exists()).toBe(true);
    // With no sampling chosen there is no amount to enter.
    expect(wrapper.find("#dq-config-sample-percent").exists()).toBe(false);
    expect(wrapper.find("#dq-config-sample-max-rows").exists()).toBe(false);
    wrapper.unmount();
  });

  it("offers every method a table on a sampling-capable engine can express", async () => {
    const { wrapper } = mountFields({ engine: Engine.POSTGRES });

    expect(await methodOptions(wrapper)).toEqual([
      { value: "none", disabled: false },
      { value: "percent", disabled: false },
      { value: "maxRows", disabled: false },
    ]);
    wrapper.unmount();
  });

  it("leaves the percentage out for an engine without a sampling clause", async () => {
    const { wrapper } = mountFields({ engine: Engine.MYSQL });

    expect(await methodOptions(wrapper)).toEqual([
      { value: "none", disabled: false },
      { value: "maxRows", disabled: false },
    ]);
    wrapper.unmount();
  });

  it("leaves the percentage out for a view", async () => {
    const { wrapper } = mountFields({ metaType: MetaType.VIEW });

    expect(await methodOptions(wrapper)).toEqual([
      { value: "none", disabled: false },
      { value: "maxRows", disabled: false },
    ]);
    wrapper.unmount();
  });

  it("shows the amount input of the chosen method", async () => {
    const percent = mountFields({ config: { sampleMethod: "percent" } });
    expect(percent.wrapper.find("#dq-config-sample-percent").exists()).toBe(
      true
    );
    expect(percent.wrapper.find("#dq-config-sample-max-rows").exists()).toBe(
      false
    );
    expect(percent.wrapper.text()).toContain(
      label("dataQuality.editor.samplePercentHint")
    );
    expect(percent.wrapper.text()).toContain("%");

    await percent.wrapper.find("#dq-config-sample-percent").setValue("25");
    expect(percent.model.samplePercent).toBe("25");
    percent.wrapper.unmount();

    const maxRows = mountFields({
      config: { sampleMethod: "maxRows", sampleMaxRows: "1000" },
    });
    expect(maxRows.wrapper.find("#dq-config-sample-max-rows").exists()).toBe(
      true
    );
    expect(maxRows.wrapper.text()).toContain(
      label("dataQuality.editor.sampleMaxRowsHint")
    );
    expect(
      maxRows.wrapper.find("#dq-config-sample-max-rows").attributes("max")
    ).toBe(String(MAX_SAMPLE_MAX_ROWS));
    maxRows.wrapper.unmount();
  });

  it("switches the method without converting an amount", async () => {
    const { wrapper, model } = mountFields();
    const methods = await methodOptions(wrapper);

    // The select's own event is what the editor wires to the form.
    samplingSelect(wrapper)?.vm.$emit(
      "update:modelValue",
      methods[1]?.value as SampleMethod
    );
    await flushPromises();

    expect(model.sampleMethod).toBe("percent");
    expect(model.sampleMaxRows).toBe("");
    wrapper.unmount();
  });

  it("keeps a stored unsupported percentage selected but not editable", async () => {
    const { wrapper, model } = mountFields({
      engine: Engine.MYSQL,
      config: { sampleMethod: "percent", samplePercent: "25" },
    });

    expect(model.sampleMethod).toBe("percent");
    expect(await methodOptions(wrapper)).toEqual([
      { value: "none", disabled: false },
      { value: "percent", disabled: true },
      { value: "maxRows", disabled: false },
    ]);
    expect(wrapper.text()).toContain(
      label("dataQuality.editor.samplePercentUnsupported")
    );
    expect(
      (wrapper.find("#dq-config-sample-percent").element as HTMLInputElement)
        .disabled
    ).toBe(true);
    wrapper.unmount();
  });

  it("does not explain an unavailable percentage that is not chosen", () => {
    const { wrapper } = mountFields({
      engine: Engine.MYSQL,
      config: { sampleMethod: "none" },
    });

    expect(wrapper.text()).not.toContain(
      label("dataQuality.editor.samplePercentUnsupported")
    );
    wrapper.unmount();
  });

  it("hides the whole execution group for a check that reads the registry", () => {
    // A metadata check runs no target query and has nothing to sample, so the
    // group would only promise limits that do not exist.
    const schemaDrift = mountFields({
      ruleType: QualityRuleType.SCHEMA_DRIFT,
      config: { sampleMethod: "none" },
    });
    expect(schemaDrift.wrapper.find("#dq-config-sampling").exists()).toBe(
      false
    );
    expect(schemaDrift.wrapper.find("#dq-config-timeout").exists()).toBe(false);
    expect(schemaDrift.wrapper.text()).not.toContain(
      label("dataQuality.editor.executionTitle")
    );
    schemaDrift.wrapper.unmount();

    // A stored method stays visible so it can be inspected or cleared, but the
    // query budget still does not exist for this check.
    const stored = mountFields({
      ruleType: QualityRuleType.SCHEMA_DRIFT,
      config: { sampleMethod: "maxRows", sampleMaxRows: "10" },
    });
    expect(stored.wrapper.find("#dq-config-sampling").exists()).toBe(true);
    expect(stored.wrapper.find("#dq-config-timeout").exists()).toBe(false);
    stored.wrapper.unmount();
  });

  it("hides sampling for a rule type that cannot be sampled", () => {
    for (const ruleType of [
      QualityRuleType.ROW_COUNT,
      QualityRuleType.ROW_COUNT_CHANGE,
      QualityRuleType.FRESHNESS,
      QualityRuleType.CUSTOM_SQL,
      QualityRuleType.CEL_EXPRESSION,
      QualityRuleType.REFERENTIAL_INTEGRITY,
      QualityRuleType.CROSS_TABLE,
    ]) {
      const { wrapper } = mountFields({ ruleType });
      expect(wrapper.find("#dq-config-sampling").exists()).toBe(false);
      // The timeout stays available for every check.
      expect(wrapper.find("#dq-config-timeout").exists()).toBe(true);
      wrapper.unmount();
    }

    // A stored method survives on such a type so it can be inspected or cleared.
    const stored = mountFields({
      ruleType: QualityRuleType.ROW_COUNT,
      config: { sampleMethod: "maxRows", sampleMaxRows: "10" },
    });
    expect(stored.wrapper.find("#dq-config-sampling").exists()).toBe(true);
    stored.wrapper.unmount();
  });

  it("describes the guarantee and the cost, never the SQL mechanism", () => {
    for (const config of [
      { sampleMethod: "percent" as const, samplePercent: "10" },
      { sampleMethod: "maxRows" as const, sampleMaxRows: "1000" },
    ]) {
      const { wrapper } = mountFields({ config });
      const text = wrapper.text();
      expect(text).not.toMatch(/TABLESAMPLE|\bLIMIT\b|TOP \d|derived table/);
      expect(text).toContain(
        label(
          config.sampleMethod === "percent"
            ? "dataQuality.editor.samplePercentHint"
            : "dataQuality.editor.sampleMaxRowsHint"
        )
      );
      wrapper.unmount();
    }
  });
});
