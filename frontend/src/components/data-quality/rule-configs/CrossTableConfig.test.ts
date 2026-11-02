import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import RuleFilterEditor from "@/components/data-quality/RuleFilterEditor.vue";
import TargetPicker from "@/components/data-quality/TargetPicker.vue";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import {
  QualityAggregateFunction,
  QualityFilterOperator,
  QualityRuleType,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { TargetSelection } from "@/utils/dataQualityForm";
import { emptyConfig, type RuleConfigForm } from "@/utils/dataQualityForm";
import CrossTableConfig from "./CrossTableConfig.vue";

const mocks = vi.hoisted(() => ({
  getQualityTarget: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  getQualityTarget: mocks.getQualityTarget,
}));

const LEFT_COLUMNS = [{ name: "amount", type: "numeric", nullable: false }];

const REFERENCE_COLUMNS = [
  { name: "total", type: "numeric", nullable: false },
  { name: "status", type: "text", nullable: true },
];

const REFERENCE: TargetSelection = {
  guid: "inst2;db2;public;orders_ref",
  metaType: MetaType.VIEW,
  path: {
    instance: "inst2",
    database: "db2",
    schema: "public",
    object: "orders_ref",
  },
  columns: [],
};

const OTHER_REFERENCE: TargetSelection = {
  guid: "inst3;warehouse;main;ledger",
  metaType: MetaType.EXTERNAL_TABLE,
  path: {
    instance: "inst3",
    database: "warehouse",
    schema: "main",
    object: "ledger",
  },
  columns: [],
};

function referenceColumnsResponse(columns = REFERENCE_COLUMNS) {
  return { targetColumns: columns };
}

function mountConfig(config: Partial<RuleConfigForm> = {}) {
  const pinia = createPinia();
  setActivePinia(pinia);
  const model = { ...emptyConfig(), ...config };
  const wrapper = mount(CrossTableConfig, {
    props: {
      ruleType: QualityRuleType.CROSS_TABLE,
      modelValue: model,
      columnDetails: LEFT_COLUMNS,
    },
    global: { plugins: [i18n, pinia] },
  });
  return { wrapper, model };
}

/** The column pickers by the label they render. */
function columnSelect(wrapper: ReturnType<typeof mount>, labelKey: string) {
  return wrapper
    .findAllComponents(ColumnSelect)
    .find((select) => select.props("label") === i18n.global.t(labelKey));
}

beforeEach(() => {
  mocks.getQualityTarget.mockReset();
  mocks.getQualityTarget.mockResolvedValue(referenceColumnsResponse());
});

describe("CrossTableConfig", () => {
  it("requires the reference table before enabling the reference side", () => {
    const { wrapper } = mountConfig();
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.referenceTablePlaceholder")
    );
    const rightColumn = columnSelect(
      wrapper,
      "dataQuality.editor.referenceColumn"
    );
    expect(rightColumn?.props("columns")).toEqual([]);
    expect(rightColumn?.props("disabled")).toBe(true);
    expect(wrapper.findComponent(RuleFilterEditor).props("disabled")).toBe(
      true
    );
    expect(mocks.getQualityTarget).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("reads the reference table's columns after one is picked", async () => {
    const { wrapper, model } = mountConfig({
      rightAggFunction: QualityAggregateFunction.SUM,
    });
    wrapper.findComponent(TargetPicker).vm.$emit("select", REFERENCE);
    await flushPromises();

    expect(mocks.getQualityTarget).toHaveBeenCalledWith({
      targetGuid: REFERENCE.guid,
      metaType: REFERENCE.metaType,
    });
    expect(model.referenceGuid).toBe(REFERENCE.guid);
    expect(model.referenceMetaType).toBe(REFERENCE.metaType);
    expect(model.referencePath).toEqual(REFERENCE.path);
    expect(wrapper.text()).toContain("inst2 > db2 > public > orders_ref");
    expect(wrapper.findComponent(RuleFilterEditor).props("disabled")).toBe(
      false
    );
    const rightColumn = columnSelect(
      wrapper,
      "dataQuality.editor.referenceColumn"
    );
    expect(rightColumn?.props("disabled")).toBe(false);
    expect(
      (rightColumn?.props("columns") as Array<{ name: string }>).map(
        (column) => column.name
      )
    ).toEqual(["total", "status"]);
    // The reference filter edits the config arm through the same editor the
    // check's own filter uses, with the reference table's columns.
    expect(wrapper.findComponent(RuleFilterEditor).props("columns")).toEqual(
      REFERENCE_COLUMNS
    );
    const addFilter = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("dataQuality.editor.addFilter"))
      );
    expect(addFilter).toBeTruthy();
    await addFilter?.trigger("click");
    expect(model.rightFilter).toHaveLength(1);
    wrapper.unmount();
  });

  it("clears the right column and the reference filter when the reference changes", async () => {
    const { wrapper, model } = mountConfig({
      referenceGuid: REFERENCE.guid,
      referenceMetaType: REFERENCE.metaType,
      referencePath: REFERENCE.path,
      rightAggFunction: QualityAggregateFunction.SUM,
      rightAggColumn: "total",
      rightFilter: [
        {
          column: "status",
          op: QualityFilterOperator.EQ,
          values: "paid",
        },
      ],
    });
    await flushPromises();
    expect(model.rightAggColumn).toBe("total");

    wrapper.findComponent(TargetPicker).vm.$emit("select", OTHER_REFERENCE);
    await flushPromises();

    expect(model.referenceGuid).toBe(OTHER_REFERENCE.guid);
    expect(model.rightAggColumn).toBe("");
    expect(model.rightFilter).toEqual([]);
    expect(mocks.getQualityTarget).toHaveBeenLastCalledWith({
      targetGuid: OTHER_REFERENCE.guid,
      metaType: OTHER_REFERENCE.metaType,
    });
    wrapper.unmount();
  });

  it("clears the reference and its right-side assertions on request", async () => {
    const { wrapper, model } = mountConfig({
      referenceGuid: REFERENCE.guid,
      referenceMetaType: REFERENCE.metaType,
      referencePath: REFERENCE.path,
      rightAggFunction: QualityAggregateFunction.SUM,
      rightAggColumn: "total",
      rightFilter: [
        { column: "status", op: QualityFilterOperator.EQ, values: "paid" },
      ],
    });
    await flushPromises();

    const clear = wrapper.find(
      `button[aria-label="${i18n.global.t("dataQuality.editor.referenceTableClear")}"]`
    );
    expect(clear.exists()).toBe(true);
    await clear.trigger("click");

    expect(model.referenceGuid).toBe("");
    expect(model.referencePath).toEqual({
      instance: "",
      database: "",
      schema: "",
      object: "",
    });
    expect(model.rightAggColumn).toBe("");
    expect(model.rightFilter).toEqual([]);
    wrapper.unmount();
  });

  it("keeps the editors usable when the reference columns cannot be read", async () => {
    mocks.getQualityTarget.mockRejectedValueOnce(new Error("boom"));
    const { wrapper } = mountConfig({
      referenceGuid: REFERENCE.guid,
      referenceMetaType: REFERENCE.metaType,
      referencePath: REFERENCE.path,
    });
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.referenceColumnsLoadFailed")
    );
    expect(wrapper.text()).toContain(i18n.global.t("common.retry"));
    wrapper.unmount();
  });

  it("drops the right column when the reference aggregate becomes COUNT", async () => {
    // COUNT reads the row count and takes no column, so the field is cleared to
    // keep the form and the compiled request in agreement.
    const { wrapper, model } = mountConfig({
      referenceGuid: REFERENCE.guid,
      referenceMetaType: REFERENCE.metaType,
      referencePath: REFERENCE.path,
      rightAggFunction: QualityAggregateFunction.SUM,
      rightAggColumn: "total",
    });
    await flushPromises();

    const selects = wrapper.findAllComponents(Select);
    // The first aggregate is the target side, the second the reference side.
    selects[1]?.vm.$emit(
      "update:modelValue",
      String(QualityAggregateFunction.COUNT)
    );
    await flushPromises();

    expect(model.rightAggFunction).toBe(QualityAggregateFunction.COUNT);
    expect(model.rightAggColumn).toBe("");
    wrapper.unmount();
  });
});
