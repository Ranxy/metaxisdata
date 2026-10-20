import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import UserSelect from "./UserSelect.vue";

const mocks = vi.hoisted(() => ({
  listAllUsers: vi.fn(),
}));

vi.mock("@/api/user", () => ({
  listAllUsers: mocks.listAllUsers,
}));

const USERS = [
  create(UserSchema, {
    name: "users/101",
    email: "alice@example.com",
    title: "Data steward",
  }),
  create(UserSchema, {
    name: "users/102",
    email: "bob@example.com",
    title: "Analyst",
  }),
];

async function mountSelect(permissions: string[], modelValue = "") {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(UserSelect, {
    attachTo: document.body,
    props: { modelValue, label: "Owner" },
    global: { plugins: [i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

describe("UserSelect", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.listAllUsers.mockResolvedValue(USERS);
  });

  it("searches by email and title, and emits the selected resource name", async () => {
    const wrapper = await mountSelect(["metaxisdata.users.list"]);
    const triggerButton = document.body.querySelector<HTMLElement>(
      'button[type="button"]'
    );
    triggerButton?.click();
    await flushPromises();

    const search = document.body.querySelector<HTMLInputElement>(
      'input[placeholder="Search users"]'
    );
    expect(search).toBeTruthy();
    Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      "value"
    )?.set?.call(search, "bob");
    search?.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();
    await flushPromises();

    const items = [
      ...document.body.querySelectorAll<HTMLElement>('[role="option"]'),
    ];
    expect(items.map((item) => item.textContent?.trim())).toEqual([
      "bob@example.comAnalyst",
    ]);
    items[0]?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flushPromises();

    expect(wrapper.emitted("update:modelValue")).toContainEqual(["users/102"]);
    wrapper.unmount();
  });

  it("clears the owner", async () => {
    const wrapper = await mountSelect(["metaxisdata.users.list"], "users/101");
    const clear = document.body.querySelector<HTMLElement>(
      '[aria-label="Clear owner"]'
    );
    expect(clear).toBeTruthy();
    clear?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flushPromises();

    expect(wrapper.emitted("update:modelValue")).toContainEqual([""]);
    wrapper.unmount();
  });

  it("degrades to a read-only value without users.list", async () => {
    const wrapper = await mountSelect([], "users/101");

    expect(mocks.listAllUsers).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain(
      i18n.global.t("userSelect.permissionHint")
    );
    expect(wrapper.find("button").exists()).toBe(false);
    wrapper.unmount();
  });
});
