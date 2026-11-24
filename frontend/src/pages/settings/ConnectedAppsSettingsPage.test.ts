import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import ConnectedAppsSettingsPage from "./ConnectedAppsSettingsPage.vue";

const mocks = vi.hoisted(() => ({
  listConnectedApps: vi.fn(),
  revokeConnectedApp: vi.fn(),
  listAllUsers: vi.fn(),
}));

vi.mock("@/api/connected-app", () => ({
  listConnectedApps: mocks.listConnectedApps,
  revokeConnectedApp: mocks.revokeConnectedApp,
}));
vi.mock("@/api/user", () => ({ listAllUsers: mocks.listAllUsers }));
vi.mock("@/api/environment", () => ({
  listAllEnvironments: vi.fn().mockResolvedValue([]),
  environmentId: (name: string) => name,
}));
vi.mock("@/lib/notify", () => ({
  notify: { success: vi.fn(), error: vi.fn() },
}));

describe("ConnectedAppsSettingsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    i18n.global.locale.value = "en-US";
    setActivePinia(createPinia());
    useAuthStore().user = create(UserSchema, {
      name: "users/9",
      permissions: ["metaxisdata.connectedApps.list"],
    });
    mocks.listConnectedApps.mockResolvedValue({
      connectedApps: [],
      nextPageToken: "",
    });
  });

  afterEach(() => {
    document.body.innerHTML = "";
  });

  // The admin/self scope and its columns are the section's contract; this page
  // only owns the header that wraps it.
  it("reloads the section from the header refresh button", async () => {
    const wrapper = mount(ConnectedAppsSettingsPage, {
      global: { plugins: [i18n] },
    });
    await flushPromises();
    mocks.listConnectedApps.mockClear();

    const refresh = wrapper
      .findAll("button")
      .find((candidate) => candidate.text() === "Refresh");
    await refresh?.trigger("click");
    await flushPromises();

    expect(mocks.listConnectedApps).toHaveBeenCalledTimes(1);
  });
});
