import { flushPromises, mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import AuditLogsPage from "./AuditLogsPage.vue";

const mocks = vi.hoisted(() => ({
  listAuditLogs: vi.fn(),
  batchGetUsers: vi.fn(),
  listAllUsers: vi.fn(),
}));

vi.mock("@/api/audit", () => ({ listAuditLogs: mocks.listAuditLogs }));
vi.mock("@/api/user", () => ({
  batchGetUsers: mocks.batchGetUsers,
  listAllUsers: mocks.listAllUsers,
}));

// The shared search bar loads the workspace environments on mount.
vi.mock("@/api/environment", () => ({
  listAllEnvironments: vi.fn().mockResolvedValue([]),
  environmentId: (name: string) => name,
}));

async function mountPage() {
  const wrapper = mount(AuditLogsPage, {
    global: { plugins: [i18n, createPinia()] },
  });
  await flushPromises();
  return wrapper;
}

function lastFilter(): string {
  const calls = mocks.listAuditLogs.mock.calls;
  return calls[calls.length - 1]?.[0]?.filter ?? "";
}

function searchInput(wrapper: Awaited<ReturnType<typeof mountPage>>) {
  const input = wrapper.find('input[type="text"]').element as HTMLInputElement;
  return input;
}

describe("AuditLogsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listAuditLogs.mockResolvedValue({
      auditLogs: [],
      nextPageToken: "",
    });
    mocks.batchGetUsers.mockResolvedValue({ users: [] });
    mocks.listAllUsers.mockResolvedValue([]);
  });

  it("loads the first page with no method filter", async () => {
    await mountPage();

    expect(mocks.listAuditLogs).toHaveBeenCalledWith(
      expect.objectContaining({
        filter: expect.not.stringContaining("method.matches"),
      })
    );
  });

  // The bar's text box is a plain search input; typing restarts the walk with a
  // method substring filter rather than opening a filter-type menu.
  it("searches the event by the text in the box", async () => {
    const wrapper = await mountPage();
    mocks.listAuditLogs.mockClear();

    await wrapper.find('input[type="text"]').setValue("Login");
    await new Promise((resolve) => setTimeout(resolve, 300));
    await flushPromises();

    expect(lastFilter()).toContain('method.matches("Login")');
  });

  it("drops the filter when the bar is cleared", async () => {
    const wrapper = await mountPage();
    await wrapper.find('input[type="text"]').setValue("Login");
    await new Promise((resolve) => setTimeout(resolve, 300));
    await flushPromises();
    mocks.listAuditLogs.mockClear();

    const clearButton = wrapper
      .findAll("button")
      .find((button) => button.text() === "Clear filters");
    await clearButton?.trigger("click");
    await new Promise((resolve) => setTimeout(resolve, 300));
    await flushPromises();

    expect(searchInput(wrapper).value).toBe("");
    expect(lastFilter()).not.toContain("method.matches");
  });
});
