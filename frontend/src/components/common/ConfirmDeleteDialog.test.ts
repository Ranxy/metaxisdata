import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import ConfirmDeleteDialog from "./ConfirmDeleteDialog.vue";

function mountDialog(props: Record<string, unknown> = {}) {
  return mount(ConfirmDeleteDialog, {
    props: {
      modelValue: true,
      title: "Delete instance",
      message: "This cannot be undone.",
      ...props,
    },
    global: { plugins: [i18n] },
    // Radix renders the content into a portal on the body.
    attachTo: document.body,
  });
}

function buttonByText(text: string): HTMLButtonElement {
  const button = Array.from(document.body.querySelectorAll("button")).find(
    (candidate) => candidate.textContent?.trim() === text
  );
  if (!button) {
    throw new Error(`no button labelled ${text}`);
  }
  return button as HTMLButtonElement;
}

describe("ConfirmDeleteDialog", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("shows the operation, its wording and the target", async () => {
    mountDialog({ itemName: "instances/prod" });
    await flushPromises();

    expect(document.body.textContent).toContain("Delete instance");
    expect(document.body.textContent).toContain("This cannot be undone.");
    expect(document.body.textContent).toContain("instances/prod");
  });

  it("emits confirm from the destructive button", async () => {
    const wrapper = mountDialog();
    await flushPromises();

    buttonByText("Delete").click();
    await flushPromises();

    expect(wrapper.emitted("confirm")).toHaveLength(1);
  });

  it("closes through cancel and through the dialog itself", async () => {
    const wrapper = mountDialog();
    await flushPromises();

    buttonByText("Cancel").click();
    await flushPromises();

    expect(wrapper.emitted("update:modelValue")).toEqual([[false]]);
  });

  it("keeps the destructive button disabled while the request runs", async () => {
    mountDialog({ loading: true });
    await flushPromises();

    expect(buttonByText("Delete").disabled).toBe(true);
  });
});
