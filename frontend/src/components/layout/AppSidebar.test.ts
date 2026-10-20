import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAppStore } from "@/store/modules/app";
import { useAuthStore } from "@/store/modules/auth";
import { useNotificationStore } from "@/store/modules/notification";
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
  "metaxisdata.applications.list",
  "metaxisdata.applicationSqls.list",
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.runs.list",
  "metaxisdata.dataQuality.issues.list",
  "metaxisdata.dataQuality.issues.get",
  "metaxisdata.dataQuality.suggest",
];

// The member baseline, as backend/store/predefined_roles.go defines it: read-only, and it
// includes reading the applications that report their SQL.
const MEMBER_PERMISSIONS = [
  "metaxisdata.explainSql.explain",
  "metaxisdata.instances.list",
  "metaxisdata.databases.list",
  "metaxisdata.databases.read",
  "metaxisdata.manualSqls.list",
  "metaxisdata.openlineage.read",
  "metaxisdata.applications.get",
  "metaxisdata.applications.list",
  // Quality evidence is part of the baseline, like OpenLineage reads: the
  // section must be reachable by a member even though writing rules is not.
  "metaxisdata.dataQuality.rules.get",
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.runs.get",
  "metaxisdata.dataQuality.runs.list",
  "metaxisdata.dataQuality.issues.get",
  "metaxisdata.dataQuality.issues.list",
  "metaxisdata.dataQuality.explain",
];

// A caller with no permission at all: every section that gates an entry is hidden.
const NO_PERMISSIONS: string[] = [];

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

// The active entry is the one carrying `font-medium`; section headers are
// buttons and the brand link sits outside the nav, so this is the whole set.
function activeHrefs(
  wrapper: ReturnType<typeof mount>
): (string | undefined)[] {
  return wrapper
    .findAll("nav a")
    .filter((anchor) => anchor.classes().includes("font-medium"))
    .map((anchor) => anchor.attributes("href"));
}

describe("AppSidebar", () => {
  beforeEach(() => {
    localStorage.clear();
    setActivePinia(createPinia());
    useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  });

  // The sidebar owns the notification stream, so a mounted one has to be told to let
  // go of it: the test never navigates away, and the connection outlives the wrapper.
  afterEach(() => {
    useNotificationStore().stopStreaming();
  });

  it("keeps the applications page out of the product navigation", async () => {
    const { wrapper } = await mountSidebar();
    const dataTrigger = sectionTrigger(wrapper, "Data");
    await dataTrigger.trigger("click");

    // The data view of the statements is here; the page that registers the applications
    // that report them is in Settings, under its own longer name.
    expect(wrapper.text()).toContain("Application SQL");
    expect(wrapper.text()).not.toContain("Application SQL Ingestion");
  });

  it("shows only the top-level entries while every section is closed", async () => {
    const { wrapper } = await mountSidebar();

    expect(wrapper.text()).toContain("Data");
    expect(wrapper.text()).toContain("OpenLineage");
    expect(wrapper.text()).toContain("Data Quality");
    expect(wrapper.text()).toContain("Settings");
    // Section children stay hidden until the section or a route opens them.
    expect(wrapper.text()).not.toContain("Metadata Browser");
    expect(wrapper.text()).not.toContain("Audit Logs");
    expect(wrapper.text()).not.toContain("Quality Rules");
    expect(wrapper.text()).not.toContain("Issues");
    expect(wrapper.text()).not.toContain("Suggestions");
    expect(wrapper.text()).not.toContain("Overview");
  });

  it("collapses a section, hides its entries and remembers the choice", async () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ collapsedSections: [] })
    );
    const { wrapper } = await mountSidebar();
    const appStore = useAppStore();

    expect(wrapper.text()).toContain("Metadata Browser");

    await sectionTrigger(wrapper, "Data").trigger("click");
    await wrapper.vm.$nextTick();

    expect(appStore.collapsedSections).toContain("datasource");
    expect(wrapper.text()).not.toContain("Metadata Browser");
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}")).toMatchObject(
      { collapsedSections: ["datasource"] }
    );

    await sectionTrigger(wrapper, "Data").trigger("click");
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

  it("shows Settings to a member, whose baseline now reaches the applications page", async () => {
    useAuthStore().user = create(UserSchema, {
      permissions: MEMBER_PERMISSIONS,
    });
    const { wrapper } = await mountSidebar();

    expect(wrapper.text()).toContain("Settings");
  });

  it("hides Settings from a caller without any permission at all", async () => {
    useAuthStore().user = create(UserSchema, {
      permissions: NO_PERMISSIONS,
    });
    const { wrapper } = await mountSidebar();

    expect(wrapper.text()).not.toContain("Settings");
  });

  it("shows the quality section to a member, whose baseline carries its reads", async () => {
    useAuthStore().user = create(UserSchema, {
      permissions: MEMBER_PERMISSIONS,
    });
    const { wrapper } = await mountSidebar();
    await sectionTrigger(wrapper, "Data Quality").trigger("click");

    expect(wrapper.text()).toContain("Data Quality");
    expect(wrapper.text()).toContain("Overview");
    expect(wrapper.text()).toContain("Quality Rules");
    expect(wrapper.text()).toContain("Quality Runs");
    expect(wrapper.text()).toContain("Issues");
  });

  it("hides the quality section from a caller without its permissions", async () => {
    useAuthStore().user = create(UserSchema, {
      permissions: NO_PERMISSIONS,
    });
    const { wrapper } = await mountSidebar();

    expect(wrapper.text()).not.toContain("Data Quality");
  });

  // A custom role may hold only suggestions: the group must still appear, and
  // only the entry it can read.
  it("keeps the quality group for a suggestion-only role", async () => {
    useAuthStore().user = create(UserSchema, {
      permissions: ["metaxisdata.dataQuality.suggest"],
    });
    const { wrapper } = await mountSidebar();
    await sectionTrigger(wrapper, "Data Quality").trigger("click");

    expect(wrapper.text()).toContain("Suggestions");
    expect(wrapper.text()).not.toContain("Quality Rules");
  });

  // A custom role may hold only runs: the group must still appear, because the
  // runs page is the only way to read quality evidence.
  it("keeps the quality group when only one of its entries is allowed", async () => {
    useAuthStore().user = create(UserSchema, {
      permissions: ["metaxisdata.dataQuality.runs.list"],
    });
    const { wrapper } = await mountSidebar();
    await sectionTrigger(wrapper, "Data Quality").trigger("click");

    expect(wrapper.text()).toContain("Quality Runs");
    expect(wrapper.text()).not.toContain("Quality Rules");
    expect(wrapper.text()).not.toContain("Issues");
    expect(wrapper.text()).not.toContain("Suggestions");
    expect(wrapper.text()).not.toContain("Overview");
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

  // The landing page's path prefixes every other quality entry, so it is active
  // only on its own route; the child entries keep prefix matching so their
  // detail routes stay highlighted.
  it("highlights only the quality landing page on its own route", async () => {
    const { wrapper } = await mountSidebar("/data-quality");

    expect(activeHrefs(wrapper)).toEqual(["/data-quality"]);
  });

  it.each([
    "/data-quality/rules",
    "/data-quality/runs",
    "/data-quality/issues",
    "/data-quality/suggestions",
  ])("highlights only the matching quality entry at %s", async (path) => {
    const { wrapper } = await mountSidebar(path);

    expect(activeHrefs(wrapper)).toEqual([path]);
  });

  it("keeps the runs entry active on a run detail route", async () => {
    const { wrapper } = await mountSidebar(
      "/data-quality/runs/workspaces/ws/qualityRuns/abc"
    );

    expect(activeHrefs(wrapper)).toEqual(["/data-quality/runs"]);
  });
});
