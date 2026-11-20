import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { QualityRuleType } from "@/types/proto-es/v1/data_quality_service_pb";
import {
  emptyConfig,
  type RuleConfigForm,
  type TargetColumnDetail,
} from "@/utils/dataQualityForm";
import AcceptedValuesConfig from "./AcceptedValuesConfig.vue";

const COLUMNS: TargetColumnDetail[] = [
  { name: "status", type: "text", nullable: false },
  { name: "status_code", type: "varchar", nullable: true },
];

function mountConfig(
  config: Partial<RuleConfigForm> = {},
  columns: TargetColumnDetail[] = COLUMNS
) {
  const model: RuleConfigForm = {
    ...emptyConfig(),
    column: "status",
    values: "open, closed",
    ...config,
  };
  const wrapper = mount(AcceptedValuesConfig, {
    props: {
      ruleType: QualityRuleType.ACCEPTED_VALUES,
      modelValue: model,
      columnDetails: columns,
      fieldErrors: {},
    },
    global: { plugins: [i18n] },
  });
  return { wrapper, model };
}

function sourceSelect(wrapper: ReturnType<typeof mountConfig>["wrapper"]) {
  return wrapper
    .findAllComponents(Select)
    .find((candidate) => candidate.find("#dq-field-acceptedSource").exists());
}

describe("AcceptedValuesConfig sources", () => {
  it("starts on the fixed list and shows the values input", () => {
    const { wrapper, model } = mountConfig();
    expect(model.acceptedSource).toBe("list");
    expect(wrapper.find("#dq-field-values").exists()).toBe(true);
    expect(wrapper.find("#dq-field-acceptedField").exists()).toBe(false);
    expect(wrapper.find("#dq-field-acceptedQuery").exists()).toBe(false);
    wrapper.unmount();
  });

  it("switches to the reference column and filters out the checked column", async () => {
    const { wrapper, model } = mountConfig();
    sourceSelect(wrapper)?.vm.$emit("update:modelValue", "field");
    await wrapper.vm.$nextTick();

    expect(model.acceptedSource).toBe("field");
    expect(wrapper.find("#dq-field-values").exists()).toBe(false);
    // The second picker is the reference column; the first is the checked one.
    const picker = wrapper.findAllComponents(ColumnSelect)[1];
    expect(picker?.exists()).toBe(true);
    expect(picker?.props("label")).toBe(
      i18n.global.t("dataQuality.editor.acceptedField")
    );
    // The checked column cannot be the reference column.
    expect(
      (picker?.props("columns") as Array<{ name: string }>).map(
        (column) => column.name
      )
    ).toEqual(["status_code"]);

    picker?.vm.$emit("update:modelValue", "status_code");
    await wrapper.vm.$nextTick();
    expect(model.acceptedField).toBe("status_code");
    wrapper.unmount();
  });

  it("switches to a query and shows its statement and value column", async () => {
    const { wrapper, model } = mountConfig();
    sourceSelect(wrapper)?.vm.$emit("update:modelValue", "query");
    await wrapper.vm.$nextTick();

    expect(model.acceptedSource).toBe("query");
    expect(wrapper.find("#dq-field-values").exists()).toBe(false);
    expect(wrapper.find("#dq-field-acceptedField").exists()).toBe(false);
    const query = wrapper.find<HTMLTextAreaElement>("#dq-field-acceptedQuery");
    expect(query.exists()).toBe(true);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.acceptedQuery")
    );
    // The value column is a plain text field beside the statement.
    const fields = wrapper
      .findAllComponents(FormField)
      .map((field) => field.props("label"));
    expect(fields).toContain(
      i18n.global.t("dataQuality.editor.acceptedQueryColumn")
    );
    wrapper.unmount();
  });

  it("renders the source-specific error under its own control", async () => {
    const { wrapper } = mountConfig({
      acceptedSource: "field",
      acceptedField: "status",
    });
    await wrapper.setProps({
      fieldErrors: {
        acceptedField:
          "The reference column must differ from the checked column.",
      },
    });
    expect(wrapper.text()).toContain(
      "The reference column must differ from the checked column."
    );
    wrapper.unmount();
  });
});

describe("AcceptedValuesConfig enum labels", () => {
  it("offers a declared enum column's labels as one-click accepted values", async () => {
    const { wrapper, model } = mountConfig(
      { column: "status", values: "active" },
      [
        {
          name: "status",
          type: "public.product_status",
          nullable: false,
          enumValues: ["active", "inactive", "archived", "draft"],
        },
      ]
    );
    const draft = wrapper
      .findAll("button")
      .find((button) => button.text() === "draft");
    expect(draft?.exists()).toBe(true);
    await draft?.trigger("click");
    expect(model.values).toBe("active, draft");
    // An already accepted label is not offered twice.
    const applied = wrapper
      .findAll("button")
      .find((button) => button.text() === "draft");
    expect(applied?.attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("shows no labels for a column that is not an enum", () => {
    const { wrapper } = mountConfig();
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.editor.enumLabelsHint")
    );
    wrapper.unmount();
  });
});

describe("AcceptedValuesConfig enum label constraints", () => {
  it("explains a label it cannot put in the comma-separated field", async () => {
    const { wrapper } = mountConfig({ column: "status", values: "" }, [
      {
        name: "status",
        type: "public.product_status",
        nullable: false,
        enumValues: ["active", " padded ", "two,values"],
      },
    ]);
    const padded = wrapper
      .findAll("button")
      .find((button) => button.text() === "padded");
    expect(padded?.attributes("disabled")).toBeDefined();
    expect(padded?.attributes("title")).toBe(
      i18n.global.t("dataQuality.editor.enumLabelsUnsupported")
    );
    wrapper.unmount();
  });

  it("marks an already accepted label as such", () => {
    const { wrapper } = mountConfig({ column: "status", values: "active" }, [
      {
        name: "status",
        type: "public.product_status",
        nullable: false,
        enumValues: ["active"],
      },
    ]);
    const active = wrapper
      .findAll("button")
      .find((button) => button.text() === "active");
    expect(active?.attributes("disabled")).toBeDefined();
    expect(active?.attributes("title")).toBe(
      i18n.global.t("dataQuality.editor.enumLabelsAlready")
    );
    wrapper.unmount();
  });
});
