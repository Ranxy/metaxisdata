import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import { Engine } from "@/types/proto-es/v1/common_pb";
import { DatabaseSchema } from "@/types/proto-es/v1/database_service_pb";
import {
  InstanceResourceSchema,
  InstanceSchema,
} from "@/types/proto-es/v1/instance_service_pb";
import {
  OpenLineageDatasetResourceSchema,
  OpenLineageRunSchema,
  OpenLineageTaskSchema,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import HomePage from "./HomePage.vue";

const mocks = vi.hoisted(() => ({
  listInstances: vi.fn(),
  listDatabases: vi.fn(),
  listOpenLineageTasks: vi.fn(),
  listOpenLineageDatasets: vi.fn(),
  listOpenLineageRuns: vi.fn(),
  listEnvironments: vi.fn(),
}));

vi.mock("@/api/instance", () => ({ listInstances: mocks.listInstances }));
vi.mock("@/api/database", () => ({ listDatabases: mocks.listDatabases }));
vi.mock("@/api/openlineage", () => ({
  listOpenLineageTasks: mocks.listOpenLineageTasks,
  listOpenLineageDatasets: mocks.listOpenLineageDatasets,
  listOpenLineageRuns: mocks.listOpenLineageRuns,
}));
vi.mock("@/api/environment", () => ({
  listEnvironments: mocks.listEnvironments,
  environmentId: (name: string) => name.replace("environments/", ""),
  environmentName: (id: string) => `environments/${id}`,
  createEnvironment: vi.fn(),
  updateEnvironment: vi.fn(),
  deleteEnvironment: vi.fn(),
}));

const PERMISSIONS = [
  "metaxisdata.instances.list",
  "metaxisdata.databases.list",
  "metaxisdata.databases.read",
  "metaxisdata.manualSqls.list",
  "metaxisdata.openlineage.read",
  "metaxisdata.openlineage.namespaceMappings.list",
  "metaxisdata.explainSql.explain",
  "metaxisdata.llm.profiles.list",
  "metaxisdata.users.list",
  "metaxisdata.auditLogs.search",
];

const Dummy = { template: "<div />" };

async function mountHome() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      { path: "/instances", name: "InstanceManagement", component: Dummy },
      {
        path: "/instances/:instanceId",
        name: "InstanceDetail",
        component: Dummy,
      },
      { path: "/databases", name: "DatabaseManagement", component: Dummy },
      { path: "/metadata", name: "MetadataBrowser", component: Dummy },
      { path: "/explain-sql", name: "ExplainSQL", component: Dummy },
      { path: "/manual-sql", name: "ManualSQLManagement", component: Dummy },
      {
        path: "/settings/openlineage",
        name: "OpenLineageSettings",
        component: Dummy,
      },
      {
        path: "/settings/llm-providers",
        name: "LLMProviderManagement",
        component: Dummy,
      },
      { path: "/settings/users", name: "UserManagement", component: Dummy },
      { path: "/settings/audit-logs", name: "AuditLogs", component: Dummy },
      { path: "/openlineage/jobs", name: "OpenLineageTasks", component: Dummy },
      {
        path: "/openlineage/datasets",
        name: "OpenLineageDatasets",
        component: Dummy,
      },
      {
        path: "/openlineage/events",
        name: "OpenLineageEvents",
        component: Dummy,
      },
      {
        path: "/openlineage/events/:guid",
        name: "OpenLineageRunDetail",
        component: Dummy,
      },
    ],
  });
  await router.push("/");
  await router.isReady();

  const wrapper = mount(HomePage, {
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return wrapper;
}

describe("HomePage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    setActivePinia(createPinia());
    useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });

    mocks.listEnvironments.mockResolvedValue({ environments: [] });
    mocks.listInstances.mockResolvedValue({
      instances: [],
      nextPageToken: "",
    });
    mocks.listDatabases.mockResolvedValue({
      databases: [],
      nextPageToken: "",
    });
    mocks.listOpenLineageTasks.mockResolvedValue({
      tasks: [],
      nextPageToken: "",
    });
    mocks.listOpenLineageDatasets.mockResolvedValue({
      datasets: [],
      nextPageToken: "",
    });
    mocks.listOpenLineageRuns.mockResolvedValue({
      runs: [],
      nextPageToken: "",
    });
  });

  it("guides a fresh workspace instead of showing empty tables", async () => {
    const wrapper = await mountHome();

    expect(wrapper.text()).toContain("Get started");
    expect(wrapper.text()).toContain("Connect a data source");
    // No problems to report and no OpenLineage evidence yet.
    expect(wrapper.text()).not.toContain("Needs attention");
    expect(wrapper.text()).toContain("No OpenLineage runs found");
  });

  it("shows real counts, sync problems and recent activity", async () => {
    mocks.listInstances.mockResolvedValue({
      instances: [
        create(InstanceSchema, {
          name: "instances/prod",
          title: "Prod",
          engine: Engine.MYSQL,
          activation: true,
          environment: "environments/prod",
          lastSyncTime: { seconds: 1_700_000_000n, nanos: 0 },
        }),
      ],
      nextPageToken: "",
    });
    mocks.listDatabases.mockResolvedValue({
      databases: [
        create(DatabaseSchema, {
          name: "instances/prod/databases/shop",
          drifted: true,
          instanceResource: create(InstanceResourceSchema, {
            name: "instances/prod",
          }),
        }),
        create(DatabaseSchema, {
          name: "instances/prod/databases/billing",
          instanceResource: create(InstanceResourceSchema, {
            name: "instances/prod",
          }),
        }),
      ],
      nextPageToken: "",
    });
    mocks.listOpenLineageTasks.mockResolvedValue({
      tasks: [
        create(OpenLineageTaskSchema, {
          jobName: "load_orders",
          jobNamespace: "airflow",
          lineageRunCount: 3,
        }),
      ],
      nextPageToken: "",
    });
    mocks.listOpenLineageDatasets.mockResolvedValue({
      datasets: [create(OpenLineageDatasetResourceSchema, { internal: true })],
      nextPageToken: "",
    });
    mocks.listOpenLineageRuns.mockResolvedValue({
      runs: [
        create(OpenLineageRunSchema, {
          guid: "run-1",
          jobName: "load_orders",
          eventType: "COMPLETE",
          hasLineage: true,
          eventTime: { seconds: 1_700_000_000n, nanos: 0 },
        }),
      ],
      nextPageToken: "",
    });

    const wrapper = await mountHome();
    const text = wrapper.text();

    expect(text).not.toContain("Get started");
    expect(text).toContain("Needs attention");
    expect(text).toContain("1 drifted");
    expect(text).toContain("1 database(s) drifted from the source schema");
    expect(text).toContain("Prod");
    expect(text).toContain("MySQL");
    expect(text).toContain("load_orders");
    expect(text).toContain("COMPLETE");
  });

  it("keeps the rest of the page when one section fails", async () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});
    mocks.listOpenLineageTasks.mockRejectedValue(new Error("boom"));

    const wrapper = await mountHome();

    expect(wrapper.text()).toContain("Part of the overview failed to load");
    // The sections that did load are still rendered.
    expect(wrapper.text()).toContain("Quick actions");
    consoleError.mockRestore();
  });
});
