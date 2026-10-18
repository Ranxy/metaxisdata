import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import SettingsNav from "./SettingsNav.vue";

// The member baseline: read-only, and it reaches the applications page.
const MEMBER_PERMISSIONS = [
  "metaxisdata.instances.list",
  "metaxisdata.databases.list",
  "metaxisdata.applications.get",
  "metaxisdata.applications.list",
];

const ADMINISTRATOR_PERMISSIONS = [
  "metaxisdata.settings.get",
  "metaxisdata.roles.list",
  "metaxisdata.applications.list",
];

// Every entry in the rail, so the rendered order is the full order.
const ALL_PERMISSIONS = [
  "metaxisdata.settings.get",
  "metaxisdata.iam.getPolicy",
  "metaxisdata.users.list",
  "metaxisdata.identityProviders.list",
  "metaxisdata.connectedApps.list",
  "metaxisdata.roles.list",
  "metaxisdata.groups.list",
  "metaxisdata.llm.profiles.list",
  "metaxisdata.applications.list",
  "metaxisdata.openlineage.namespaceMappings.list",
  "metaxisdata.auditLogs.search",
];

async function mountNav() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push("/settings/applications");
  await router.isReady();
  const wrapper = mount(SettingsNav, { global: { plugins: [router, i18n] } });
  return { wrapper };
}

describe("SettingsNav", () => {
  beforeEach(() => {
    localStorage.clear();
    setActivePinia(createPinia());
  });

  it("offers the applications page to a member, who may read applications", async () => {
    useAuthStore().user = create(UserSchema, {
      permissions: MEMBER_PERMISSIONS,
    });
    const { wrapper } = await mountNav();

    expect(wrapper.text()).toContain("Application SQL Ingestion");
    expect(wrapper.text()).not.toContain("Roles");
  });

  it("hides the administration pages from that same member", async () => {
    useAuthStore().user = create(UserSchema, {
      permissions: ADMINISTRATOR_PERMISSIONS,
    });
    const { wrapper } = await mountNav();

    expect(wrapper.text()).toContain("Roles");
    expect(wrapper.text()).not.toContain("Audit Logs");
  });

  it("keeps user management next to the policy page and audit logs last", async () => {
    useAuthStore().user = create(UserSchema, { permissions: ALL_PERMISSIONS });
    const { wrapper } = await mountNav();

    const hrefs = wrapper.findAll("a").map((link) => link.attributes("href"));

    expect(hrefs[hrefs.indexOf("/settings/iam") + 1]).toBe("/settings/users");
    expect(hrefs[hrefs.length - 1]).toBe("/settings/audit-logs");
  });

  // Connected apps is access governance, so it sits with the identity pages and
  // is hidden from a caller who may not see the workspace's connections.
  it("offers connected apps only to a reader of the workspace connections", async () => {
    useAuthStore().user = create(UserSchema, {
      permissions: ["metaxisdata.connectedApps.list"],
    });
    const { wrapper } = await mountNav();

    expect(wrapper.text()).toContain("Connected apps");
    expect(wrapper.text()).not.toContain("Roles");
  });

  it("places connected apps after identity providers and before roles", async () => {
    useAuthStore().user = create(UserSchema, { permissions: ALL_PERMISSIONS });
    const { wrapper } = await mountNav();

    const hrefs = wrapper.findAll("a").map((link) => link.attributes("href"));
    const connectedApps = hrefs.indexOf("/settings/connected-apps");

    expect(connectedApps).toBeGreaterThan(-1);
    expect(hrefs[connectedApps - 1]).toBe("/settings/identity-providers");
    expect(hrefs[connectedApps + 1]).toBe("/settings/roles");
  });
});
