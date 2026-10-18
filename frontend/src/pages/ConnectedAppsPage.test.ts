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
      connectedApps: [app()],
      nextPageToken: "",
    });
    mocks.listAllUsers.mockResolvedValue([]);
  });

  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("is the caller's own list, with no user column and no filters", async () => {
    const wrapper = mount(ConnectedAppsPage, {
      global: { plugins: [i18n] },
    });
    await flushPromises();

    expect(wrapper.text()).toContain("Connected apps");
    expect(wrapper.text()).toContain(
      "The MCP clients you authorized to act as you"
    );
    expect(mocks.listConnectedApps).toHaveBeenCalledWith(
      expect.objectContaining({ user: "users/1", filter: "" })
    );
    // The caller's own email is not a column; there is nobody to distinguish.
    expect(wrapper.text()).not.toContain("ada@example.com");
    expect(wrapper.findComponent(AdvancedSearchBar).exists()).toBe(false);
  });
});
