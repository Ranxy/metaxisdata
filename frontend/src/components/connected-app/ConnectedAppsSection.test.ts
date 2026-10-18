import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AdvancedSearchBar from "@/components/common/AdvancedSearchBar.vue";
import TablePager from "@/components/common/TablePager.vue";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  type ConnectedApp,
  ConnectedApp_State,
  ConnectedAppSchema,
} from "@/types/proto-es/v1/connected_app_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import ConnectedAppsSection from "./ConnectedAppsSection.vue";

const mocks = vi.hoisted(() => ({
  listConnectedApps: vi.fn(),
  revokeConnectedApp: vi.fn(),
  listAllUsers: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
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
  notify: { success: mocks.success, error: mocks.error },
}));

function app(
  overrides: Partial<Omit<ConnectedApp, "$typeName" | "$unknown">> = {}
): ConnectedApp {
  return create(ConnectedAppSchema, {
    name: "connectedApps/7",
    clientId: "client-abc",
    clientName: "Claude Code",
    user: "users/1",
    userEmail: "ada@example.com",
    createTime: timestampFromDate(new Date("2026-10-01T09:00:00Z")),
    expireTime: timestampFromDate(new Date("2026-11-01T09:00:00Z")),
    state: ConnectedApp_State.ACTIVE,
    ...overrides,
  });
}

function page(apps: ConnectedApp[], nextPageToken = "") {
  return { connectedApps: apps, nextPageToken };
}

async function mountSection(scope: "admin" | "self" = "admin") {
  const wrapper = mount(ConnectedAppsSection, {
    props: { scope },
    attachTo: document.body,
    global: { plugins: [i18n] },
  });
  await flushPromises();
  return wrapper;
}

function lastListCall() {
  const calls = mocks.listConnectedApps.mock.calls;
  return calls[calls.length - 1]?.[0] ?? {};
}

function dialogButton(label: string): HTMLButtonElement | undefined {
  const dialog = document.body.querySelector('[role="dialog"]');
  return Array.from(dialog?.querySelectorAll("button") ?? []).find(
    (button) => button.textContent?.trim() === label
  ) as HTMLButtonElement | undefined;
}

describe("ConnectedAppsSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    i18n.global.locale.value = "en-US";
    setActivePinia(createPinia());
    useAuthStore().user = create(UserSchema, {
      name: "users/1",
      permissions: ["metaxisdata.connectedApps.list", "metaxisdata.users.list"],
    });
    mocks.listConnectedApps.mockResolvedValue(page([]));
    mocks.revokeConnectedApp.mockResolvedValue(app());
    mocks.listAllUsers.mockResolvedValue([]);
  });

  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("loads the first page on mount", async () => {
    await mountSection("admin");

    expect(mocks.listConnectedApps).toHaveBeenCalledWith(
      expect.objectContaining({ pageSize: 50, pageToken: "", user: "" })
    );
  });

  it("pins the self-service page to the signed-in user and hides filters", async () => {
    const wrapper = await mountSection("self");

    expect(mocks.listConnectedApps).toHaveBeenCalledWith(
      expect.objectContaining({ user: "users/1" })
    );
    expect(wrapper.findComponent(AdvancedSearchBar).exists()).toBe(false);
  });

  it("keeps the workspace view for administrators", async () => {
    const wrapper = await mountSection("admin");

    expect(lastListCall().user).toBe("");
    expect(wrapper.findComponent(AdvancedSearchBar).exists()).toBe(true);
  });

  it("explains an empty list differently to each audience", async () => {
    const admin = await mountSection("admin");
    expect(admin.text()).toContain("No MCP client has been authorized");

    const self = await mountSection("self");
    expect(self.text()).toContain("You have not authorized any MCP client");
  });

  it("renders an inline error when the list request fails", async () => {
    mocks.listConnectedApps.mockRejectedValue(
      new ConnectError("boom", Code.Internal)
    );

    const wrapper = await mountSection("admin");

    expect(wrapper.text()).toContain("The server hit an internal error.");
  });

  it("renders the rows the server returned", async () => {
    mocks.listConnectedApps.mockResolvedValue(page([app()]));

    const wrapper = await mountSection("self");

    expect(wrapper.text()).toContain("Claude Code");
    expect(wrapper.text()).toContain("Active");
  });

  it("revokes through the dialog, then reloads the list and toasts", async () => {
    mocks.listConnectedApps
      .mockResolvedValueOnce(page([app()]))
      .mockResolvedValueOnce(
        page([app({ state: ConnectedApp_State.REVOKED })])
      );

    const wrapper = await mountSection("self");
    const rowButton = wrapper
      .findAll("button")
      .find((candidate) => candidate.text() === "Revoke");
    await rowButton?.trigger("click");
    await flushPromises();

    expect(
      document.body.querySelector('[role="dialog"]')?.textContent
    ).toContain("Revoke connected app");

    dialogButton("Revoke")?.click();
    await flushPromises();

    expect(mocks.revokeConnectedApp).toHaveBeenCalledWith("connectedApps/7");
    expect(mocks.listConnectedApps).toHaveBeenCalledTimes(2);
    expect(mocks.success).toHaveBeenCalledWith("Connected app revoked");
  });

  it("keeps the dialog open and toasts when revoking fails", async () => {
    mocks.listConnectedApps.mockResolvedValue(page([app()]));
    mocks.revokeConnectedApp.mockRejectedValue(
      new ConnectError("denied", Code.NotFound)
    );

    const wrapper = await mountSection("self");
    const rowButton = wrapper
      .findAll("button")
      .find((candidate) => candidate.text() === "Revoke");
    await rowButton?.trigger("click");
    await flushPromises();

    dialogButton("Revoke")?.click();
    await flushPromises();

    expect(mocks.error).toHaveBeenCalled();
    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull();
  });

  it("offers the user filter only when the caller may read users", async () => {
    const admin = await mountSection("admin");
    const categories = admin
      .findComponent(AdvancedSearchBar)
      .props("filterCategories") as Array<{ type: string }>;
    expect(categories.map((category) => category.type)).toEqual([
      "user",
      "state",
      "client",
    ]);

    useAuthStore().user = create(UserSchema, {
      name: "users/1",
      permissions: ["metaxisdata.connectedApps.list"],
    });
    const limited = await mountSection("admin");
    const limitedCategories = limited
      .findComponent(AdvancedSearchBar)
      .props("filterCategories") as Array<{ type: string }>;
    expect(limitedCategories.map((category) => category.type)).toEqual([
      "state",
      "client",
    ]);
  });

  it("restarts at the first page when a filter changes", async () => {
    mocks.listConnectedApps
      .mockResolvedValueOnce(page([app()], "10"))
      .mockResolvedValue(page([]));

    const wrapper = await mountSection("admin");
    mocks.listConnectedApps.mockClear();

    wrapper.findComponent(AdvancedSearchBar).vm.$emit("update:filters", [
      {
        id: "1",
        type: "state",
        label: "Status",
        value: "REVOKED",
        displayValue: "Revoked",
      },
    ]);
    await flushPromises();

    expect(lastListCall().filter).toContain('state == "REVOKED"');
    expect(lastListCall().pageToken).toBe("");
  });

  it("sends the search box as a broad client/email match", async () => {
    mocks.listConnectedApps.mockResolvedValue(page([]));

    const wrapper = await mountSection("admin");
    mocks.listConnectedApps.mockClear();

    wrapper.findComponent(AdvancedSearchBar).vm.$emit("update:filters", [
      {
        id: "name-search",
        type: "name",
        label: "Name",
        value: "claude",
        displayValue: "claude",
      },
    ]);
    await flushPromises();
    // The bar emits per keystroke; the page debounces before restarting.
    await new Promise((resolve) => setTimeout(resolve, 300));

    expect(lastListCall().filter).toContain('client.matches("claude")');
    expect(lastListCall().filter).toContain('client_id.matches("claude")');
    expect(lastListCall().filter).toContain('user_email.matches("claude")');
  });

  it("walks pages and restarts when the page size changes", async () => {
    mocks.listConnectedApps
      .mockResolvedValueOnce(page([app()], "10"))
      .mockResolvedValue(page([app()]));

    const wrapper = await mountSection("self");
    await wrapper
      .findAll("button")
      .find((candidate) => candidate.text() === "Next")
      ?.trigger("click");
    await flushPromises();

    expect(lastListCall().pageToken).toBe("10");

    wrapper.findComponent(TablePager).vm.$emit("update:pageSize", 100);
    await flushPromises();

    expect(lastListCall().pageSize).toBe(100);
    expect(lastListCall().pageToken).toBe("");
  });
});
