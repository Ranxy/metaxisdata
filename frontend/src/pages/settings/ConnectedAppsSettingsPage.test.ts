import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AdvancedSearchBar from "@/components/common/AdvancedSearchBar.vue";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  ConnectedApp_State,
  ConnectedAppSchema,
} from "@/types/proto-es/v1/connected_app_service_pb";
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

function app() {
  return create(ConnectedAppSchema, {
    name: "connectedApps/7",
    clientId: "client-abc",
    clientName: "Claude Code",
    user: "users/1",
    userEmail: "ada@example.com",
    createTime: timestampFromDate(new Date("2026-10-01T09:00:00Z")),
    expireTime: timestampFromDate(new Date("2026-11-01T09:00:00Z")),
    state: ConnectedApp_State.ACTIVE,
  });
}

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
      connectedApps: [app()],
      nextPageToken: "",
    });
    mocks.listAllUsers.mockResolvedValue([]);
  });

  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("is the administrator's workspace-wide view, with the user column", async () => {
    const wrapper = mount(ConnectedAppsSettingsPage, {
      global: { plugins: [i18n] },
    });
    await flushPromises();

    expect(wrapper.text()).toContain("Connected apps");
    expect(wrapper.text()).toContain(
      "Every MCP client authorized to act on behalf of a user"
    );
    // The administrator's own name must not pin the query.
    expect(mocks.listConnectedApps).toHaveBeenCalledWith(
      expect.objectContaining({ user: "" })
    );
    expect(wrapper.text()).toContain("User");
    expect(wrapper.text()).toContain("ada@example.com");
    expect(wrapper.findComponent(AdvancedSearchBar).exists()).toBe(true);
  });

  it("reloads the current page from the header refresh button", async () => {
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
