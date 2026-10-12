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
});
