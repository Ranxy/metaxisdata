import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import UserMenu from "./UserMenu.vue";

// The dropdown's content is teleported out of the component and only rendered
// while the menu is open, so the assertions read the document, not the wrapper.
async function openUserMenu() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push("/");
  await router.isReady();
  const wrapper = mount(UserMenu, {
    attachTo: document.body,
    global: { plugins: [router, i18n] },
  });
  await wrapper.find("button").trigger("click");
  await new Promise((resolve) => setTimeout(resolve, 0));
  return wrapper;
}

describe("UserMenu", () => {
  beforeEach(() => {
    localStorage.clear();
    setActivePinia(createPinia());
  });

  afterEach(() => {
    document.body.innerHTML = "";
    vi.unstubAllGlobals();
  });

  it("shows the server's version, commit and build time", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({
              version: "v1.2.3",
              git_commit: "abcdef0123456789abcdef0123456789abcdef01",
              build_time: "2025-10-09T00:00:00Z",
            }),
        })
      )
    );

    const wrapper = await openUserMenu();
    const menu = document.body.textContent ?? "";

    expect(menu).toContain("Version: v1.2.3");
    expect(menu).toContain("Commit: abcdef01");
    expect(menu).toContain("Build time: 2025-10-09T00:00:00Z");
    wrapper.unmount();
  });

  it("renders no build metadata when the server does not answer", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new Error("offline")))
    );

    const wrapper = await openUserMenu();

    expect(document.body.textContent ?? "").not.toContain("Version:");
    wrapper.unmount();
  });
});
