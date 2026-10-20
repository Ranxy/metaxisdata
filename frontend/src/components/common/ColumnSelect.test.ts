import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { nextTick } from "vue";
import { i18n } from "@/locales";
import ColumnSelect from "./ColumnSelect.vue";

const COLUMNS = [
  { name: "id", type: "bigint", nullable: false },
  { name: "email", type: "text", nullable: true },
  { name: "amount", type: "numeric(12,2)", nullable: true },
];

function mountSelect(modelValue: string | string[], multiple = false) {
  return mount(ColumnSelect, {
    attachTo: document.body,
    props: { modelValue, columns: COLUMNS, multiple, label: "Column" },
    global: { plugins: [i18n] },
  });
}

function trigger(): HTMLElement {
  return document.body.querySelector<HTMLElement>(
    "button[type='button']"
  ) as HTMLElement;
}

describe("ColumnSelect", () => {
  beforeEach(() => {
    localStorage.clear();
    document.body.innerHTML = "";
  });

  it("shows the selected column with its type badge", async () => {
    const wrapper = mountSelect("id");
    expect(wrapper.text()).toContain("id");
    expect(wrapper.text()).toContain("bigint");
    wrapper.unmount();
  });

  it("searches and selects one column", async () => {
    const wrapper = mountSelect("");
    trigger()?.click();
    await flushPromises();

    const search =
      document.body.querySelector<HTMLInputElement>("input[placeholder]");
    expect(search).toBeTruthy();
    Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      "value"
    )?.set?.call(search, "mail");
    search?.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();
    await flushPromises();
    const options = [
      ...document.body.querySelectorAll<HTMLElement>('[role="option"]'),
    ];
    expect(options.map((option) => option.textContent?.trim())).toEqual([
      "emailtext",
    ]);

    options[0]?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flushPromises();

    expect(wrapper.emitted("update:modelValue")).toContainEqual(["email"]);
    wrapper.unmount();
  });

  it("toggles multiple columns and clears them", async () => {
    const wrapper = mountSelect(["id"], true);
    trigger()?.click();
    await flushPromises();

    const email = [
      ...document.body.querySelectorAll<HTMLElement>('[role="option"]'),
    ].find((option) => option.textContent?.includes("email"));
    email?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flushPromises();
    expect(wrapper.emitted("update:modelValue")).toContainEqual([
      ["id", "email"],
    ]);

    const clear = document.body.querySelector<HTMLElement>(
      '[aria-label="Clear column"]'
    );
    clear?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flushPromises();
    expect(wrapper.emitted("update:modelValue")).toContainEqual([[]]);
    wrapper.unmount();
  });

  it("renders an empty state when the target has no columns", async () => {
    const wrapper = mount(ColumnSelect, {
      attachTo: document.body,
      props: { modelValue: "", columns: [] },
      global: { plugins: [i18n] },
    });
    expect(wrapper.text()).toContain(i18n.global.t("columnSelect.placeholder"));
    wrapper.unmount();
  });
});
