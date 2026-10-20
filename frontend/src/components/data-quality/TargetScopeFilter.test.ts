import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { defineComponent, h } from "vue";
import { i18n } from "@/locales";
import type { QualityTargetScope } from "@/utils/dataQuality";
import TargetScopeFilter from "./TargetScopeFilter.vue";

const SCOPE: QualityTargetScope = {
  instance: "inst1",
  database: "shop",
  schema: "public",
  table: "",
  label: "Orders > shop > public",
};

/**
 * The dialog owns the browsing interaction (covered by TargetPicker's own
 * tests); this stub only reports that it was opened and emits the picked scope.
 */
const TargetPickerStub = defineComponent({
  name: "TargetPicker",
  props: { open: Boolean, mode: String },
  emits: ["update:open", "selectScope"],
  setup(_props, { emit }) {
    return () =>
      h(
        "button",
        {
          class: "picker-stub",
          type: "button",
          onClick: () => emit("selectScope", SCOPE),
        },
        "pick"
      );
  },
});

function mountFilter(value?: QualityTargetScope) {
  return mount(TargetScopeFilter, {
    props: { modelValue: value },
    global: {
      plugins: [i18n],
      stubs: { TargetPicker: TargetPickerStub },
    },
  });
}

describe("TargetScopeFilter", () => {
  it("shows the placeholder and opens the browser from the trigger", async () => {
    const wrapper = mountFilter();
    const picker = wrapper.findComponent(TargetPickerStub);

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.targetScope.placeholder")
    );
    expect(picker.props("open")).toBe(false);

    await wrapper.findAll("button")[0].trigger("click");

    expect(picker.props("open")).toBe(true);
    expect(picker.props("mode")).toBe("scope");
  });

  it("reports the picked scope", async () => {
    const wrapper = mountFilter();
    await wrapper.find(".picker-stub").trigger("click");

    expect(wrapper.emitted("update:modelValue")?.[0]?.[0]).toEqual(SCOPE);
  });

  it("renders the picked path and lets the reader clear it", async () => {
    const wrapper = mountFilter(SCOPE);

    expect(wrapper.text()).toContain("Orders > shop > public");

    // The icon button carries its label on the title/aria-label, not as text.
    const clearButton = wrapper
      .findAll("button")
      .find(
        (button) =>
          button.attributes("title") ===
          i18n.global.t("dataQuality.targetScope.clear")
      );
    expect(clearButton, "no clear button for a picked scope").toBeTruthy();

    await clearButton?.trigger("click");
    expect(wrapper.emitted("update:modelValue")?.[0]).toEqual([undefined]);
  });
});
