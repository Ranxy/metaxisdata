import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAppStore } from "@/store/modules/app";
import { useAuthStore } from "@/store/modules/auth";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import AppSidebar from "./AppSidebar.vue";

const STORAGE_KEY = "metaxisdata-app-state";

// Every permission the sidebar gates an entry on, so no section is filtered out.
const PERMISSIONS = [
  "metaxisdata.explainSql.explain",
  "metaxisdata.instances.list",
  "metaxisdata.databases.list",
  "metaxisdata.databases.read",
  "metaxisdata.manualSqls.list",
  "metaxisdata.openlineage.read",
  "metaxisdata.openlineage.namespaceMappings.list",
  "metaxisdata.settings.get",
  "metaxisdata.iam.getPolicy",
  "metaxisdata.roles.list",
  "metaxisdata.groups.list",
  "metaxisdata.users.list",
  "metaxisdata.auditLogs.search",
  "metaxisdata.llm.profiles.list",
];

// The discovery/read baseline: no administration permission at all.
const MEMBER_PERMISSIONS = [
  "metaxisdata.explainSql.explain",
  "metaxisdata.instances.list",
  "metaxisdata.databases.list",
  "metaxisdata.databases.read",
  "metaxisdata.manualSqls.list",
  "metaxisdata.openlineage.read",
];

async function mountSidebar(path = "/") {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push(path);
  await router.isReady();
  const wrapper = mount(AppSidebar, { global: { plugins: [router, i18n] } });
  return { wrapper, router };
}

function sectionTrigger(wrapper: ReturnType<typeof mount>, label: string) {
  const trigger = wrapper
    .findAll("button")
    .find((button) => button.text().includes(label));
  if (!trigger) {
    throw new Error(`no section trigger labelled ${label}`);
  }
  return trigger;
}

describe("AppSidebar", () => {
  beforeEach(() => {
    localStorage.clear();
    setActivePinia(createPinia());
    useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  });

  it("shows only the top-level entries while every section is closed", async () => {
    const { wrapper } = await mountSidebar();

    expect(wrapper.text()).toContain("Data Sources");
    expect(wrapper.text()).toContain("OpenLineage");
    expect(wrapper.text()).toContain("Settings");
    // Section children stay hidden until the section or a route opens them.
    expect(wrapper.text()).not.toContain("Metadata Browser");
    expect(wrapper.text()).not.toContain("Audit Logs");
  });

  it("collapses a section, hides its entries and remembers the choice", async () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ collapsedSections: [] })
    );
    const { wrapper } = await mountSidebar();
    const appStore = useAppStore();

    expect(wrapper.text()).toContain("Metadata Browser");

    await sectionTrigger(wrapper, "Data Sources").trigger("click");
    await wrapper.vm.$nextTick();

    expect(appStore.collapsedSections).toContain("datasource");
    expect(wrapper.text()).not.toContain("Metadata Browser");
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}")).toMatchObject(
      { collapsedSections: ["datasource"] }
    );

    await sectionTrigger(wrapper, "Data Sources").trigger("click");
    await wrapper.vm.$nextTick();

    expect(appStore.collapsedSections).not.toContain("datasource");
    expect(wrapper.text()).toContain("Metadata Browser");
  });

  it("reopens a collapsed section when navigation lands inside it", async () => {
    const { wrapper } = await mountSidebar("/metadata");

    expect(useAppStore().collapsedSections).not.toContain("datasource");
    expect(wrapper.text()).toContain("Metadata Browser");
  });

  it("keeps a collapsed section closed while its entries stay unreachable", async () => {
    const { wrapper } = await mountSidebar("/");
    const appStore = useAppStore();

    // OpenLineage starts collapsed, so the first click opens it and the second
    // closes it again.
    await sectionTrigger(wrapper, "OpenLineage").trigger("click");
    await wrapper.vm.$nextTick();
    expect(appStore.collapsedSections).not.toContain("openlineage");
    expect(wrapper.text()).toContain("Datasets");

    await sectionTrigger(wrapper, "OpenLineage").trigger("click");
    await wrapper.vm.$nextTick();

    expect(appStore.collapsedSections).toContain("openlineage");
    expect(wrapper.text()).not.toContain("Datasets");
  });

  it("renders Settings as a single entry into the settings section", async () => {
    const { wrapper } = await mountSidebar();
    const link = wrapper
      .findAll("a")
      .find((anchor) => anchor.text().includes("Settings"));

    expect(link?.attributes("href")).toBe("/settings");
  });

  it("hides Settings from a caller without any administration permission", async () => {
    useAuthStore().user = create(UserSchema, {
      permissions: MEMBER_PERMISSIONS,
    });
    const { wrapper } = await mountSidebar();

    expect(wrapper.text()).not.toContain("Settings");
  });

  it("dismisses the mobile drawer after navigating", async () => {
    const { wrapper, router } = await mountSidebar();
    const appStore = useAppStore();
    appStore.setMobileNavOpen(true);
    await wrapper.vm.$nextTick();

    await router.push("/instances");
    await wrapper.vm.$nextTick();

    expect(appStore.mobileNavOpen).toBe(false);
  });
});
