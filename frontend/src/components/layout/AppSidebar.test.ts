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

describe("AppSidebar sections", () => {
  beforeEach(() => {
    localStorage.clear();
    setActivePinia(createPinia());
    useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  });

  it("renders every section expanded by default", async () => {
    const { wrapper } = await mountSidebar();

    expect(sectionTrigger(wrapper, "Data Sources").exists()).toBe(true);
    expect(wrapper.text()).toContain("Metadata Browser");
    expect(wrapper.text()).toContain("Audit Logs");
  });

  it("collapses a section, hides its entries and remembers the choice", async () => {
    const { wrapper } = await mountSidebar();
    const appStore = useAppStore();

    await sectionTrigger(wrapper, "Settings").trigger("click");
    await wrapper.vm.$nextTick();

    expect(appStore.collapsedSections).toEqual(["settings"]);
    expect(wrapper.text()).not.toContain("Audit Logs");
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}")).toMatchObject(
      { collapsedSections: ["settings"] }
    );

    await sectionTrigger(wrapper, "Settings").trigger("click");
    await wrapper.vm.$nextTick();

    expect(appStore.collapsedSections).toEqual([]);
    expect(wrapper.text()).toContain("Audit Logs");
  });

  it("reopens a collapsed section when navigation lands inside it", async () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ collapsedSections: ["settings"] })
    );
    const { wrapper } = await mountSidebar("/settings/users");

    expect(useAppStore().collapsedSections).toEqual([]);
    expect(wrapper.text()).toContain("Audit Logs");
  });

  it("keeps a collapsed section closed while its entries stay unreachable", async () => {
    const { wrapper } = await mountSidebar("/");
    const appStore = useAppStore();

    await sectionTrigger(wrapper, "OpenLineage").trigger("click");
    await wrapper.vm.$nextTick();

    expect(appStore.collapsedSections).toEqual(["openlineage"]);
    expect(wrapper.text()).not.toContain("Datasets");
  });
});
