import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import ConnectedAppsPage from "./ConnectedAppsPage.vue";

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

describe("ConnectedAppsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    i18n.global.locale.value = "en-US";
    setActivePinia(createPinia());
    useAuthStore().user = create(UserSchema, {
      name: "users/1",
      permissions: [],
    });
    mocks.listConnectedApps.mockResolvedValue({
      connectedApps: [],
      nextPageToken: "",
    });
  });

  afterEach(() => {
    document.body.innerHTML = "";
  });

  // The page owns the header's refresh affordance. What the section shows and
  // which scope it queries are the section's contract, owned by its own suite;
  // asserting the header copy here only failed when the copy was edited.
  it("reloads the section from the header refresh button", async () => {
    const wrapper = mount(ConnectedAppsPage, {
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
