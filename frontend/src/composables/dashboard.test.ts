import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAuthStore } from "@/store/modules/auth";
import {
  type Database,
  DatabaseSchema,
} from "@/types/proto-es/v1/database_service_pb";
import {
  type Instance,
  InstanceResourceSchema,
  InstanceSchema,
} from "@/types/proto-es/v1/instance_service_pb";
import {
  OpenLineageDatasetResourceSchema,
  OpenLineageTaskSchema,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import {
  countDatabasesByInstance,
  instanceIDOfDatabase,
  summarizeEstate,
  summarizeOpenLineage,
  useDashboard,
} from "./dashboard";

const mocks = vi.hoisted(() => ({
  listInstances: vi.fn(),
  listDatabases: vi.fn(),
  listOpenLineageTasks: vi.fn(),
  listOpenLineageDatasets: vi.fn(),
  listOpenLineageRuns: vi.fn(),
}));

vi.mock("@/api/instance", () => ({ listInstances: mocks.listInstances }));
vi.mock("@/api/database", () => ({ listDatabases: mocks.listDatabases }));
vi.mock("@/api/openlineage", () => ({
  listOpenLineageTasks: mocks.listOpenLineageTasks,
  listOpenLineageDatasets: mocks.listOpenLineageDatasets,
  listOpenLineageRuns: mocks.listOpenLineageRuns,
}));

function instance(
  id: string,
  options: { activation?: boolean; lastSyncSeconds?: number } = {}
): Instance {
  return create(InstanceSchema, {
    name: `instances/${id}`,
    activation: options.activation ?? true,
    lastSyncTime: options.lastSyncSeconds
      ? { seconds: BigInt(options.lastSyncSeconds), nanos: 0 }
      : undefined,
  });
}

function database(
  instanceID: string,
  name: string,
  options: { drifted?: boolean; syncSeconds?: number } = {}
): Database {
  return create(DatabaseSchema, {
    name: `instances/${instanceID}/databases/${name}`,
    drifted: options.drifted ?? false,
    successfulSyncTime: options.syncSeconds
      ? { seconds: BigInt(options.syncSeconds), nanos: 0 }
      : undefined,
    instanceResource: create(InstanceResourceSchema, {
      name: `instances/${instanceID}`,
    }),
  });
}

describe("summarizeEstate", () => {
  it("counts instances, databases and the states worth flagging", () => {
    const summary = summarizeEstate(
      [
        instance("prod", { lastSyncSeconds: 1_700_000_000 }),
        instance("staging", { activation: false }),
        instance("dev"),
      ],
      [
        database("prod", "shop", { syncSeconds: 1_700_000_000 }),
        database("prod", "billing", { drifted: true }),
        database("dev", "sandbox", { syncSeconds: 1_700_000_000 }),
      ]
    );

    expect(summary).toEqual({
      instanceCount: 3,
      activeInstanceCount: 2,
      inactiveInstanceCount: 1,
      // Only the active "dev" instance has never synced; the inactive one is
      // expected not to have synced.
      neverSyncedInstanceCount: 1,
      databaseCount: 3,
      driftedDatabaseCount: 1,
      neverSyncedDatabaseCount: 1,
    });
  });

  it("treats a zero timestamp as never synced", () => {
    const summary = summarizeEstate([instance("zero")], []);
    expect(summary.neverSyncedInstanceCount).toBe(1);
  });
});

describe("countDatabasesByInstance", () => {
  it("groups databases by the instance they belong to", () => {
    const counts = countDatabasesByInstance([
      database("prod", "shop"),
      database("prod", "billing"),
      database("dev", "sandbox"),
    ]);

    expect(counts).toEqual({ prod: 2, dev: 1 });
  });

  it("falls back to the resource name when the instance resource is absent", () => {
    const withoutResource = create(DatabaseSchema, {
      name: "instances/legacy/databases/old",
    });

    expect(instanceIDOfDatabase(withoutResource)).toBe("legacy");
    expect(countDatabasesByInstance([withoutResource])).toEqual({ legacy: 1 });
  });

  it("ignores a database whose instance cannot be derived", () => {
    const nameless = create(DatabaseSchema, { name: "no-slashes" });

    expect(instanceIDOfDatabase(nameless)).toBe("");
    expect(countDatabasesByInstance([nameless])).toEqual({});
  });
});

describe("summarizeOpenLineage", () => {
  it("counts jobs, lineage-ready jobs, namespaces and dataset flags", () => {
    const summary = summarizeOpenLineage(
      [
        create(OpenLineageTaskSchema, {
          jobNamespace: "airflow",
          lineageRunCount: 2,
        }),
        create(OpenLineageTaskSchema, {
          jobNamespace: "airflow",
          lineageRunCount: 0,
        }),
        create(OpenLineageTaskSchema, {
          jobNamespace: "dbt",
          lineageRunCount: 5,
        }),
      ],
      [
        create(OpenLineageDatasetResourceSchema, {
          internal: true,
          supportsColumnLineage: true,
        }),
        create(OpenLineageDatasetResourceSchema, {
          internal: false,
          supportsColumnLineage: false,
        }),
      ]
    );

    expect(summary).toEqual({
      jobCount: 3,
      lineageReadyJobCount: 2,
      namespaceCount: 2,
      datasetCount: 2,
      internalDatasetCount: 1,
      columnLineageDatasetCount: 1,
    });
  });

  it("returns zeroes for an empty estate", () => {
    expect(summarizeOpenLineage([], [])).toEqual({
      jobCount: 0,
      lineageReadyJobCount: 0,
      namespaceCount: 0,
      datasetCount: 0,
      internalDatasetCount: 0,
      columnLineageDatasetCount: 0,
    });
  });
});

const PERMISSIONS = [
  "metaxisdata.instances.list",
  "metaxisdata.databases.list",
  "metaxisdata.openlineage.read",
];

/** A fresh store, so one test's permissions never leak into the next. */
function grant(permissions: string[]) {
  setActivePinia(createPinia());
  useAuthStore().user = create(UserSchema, { permissions });
}

describe("useDashboard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listInstances.mockResolvedValue({ instances: [], nextPageToken: "" });
    mocks.listDatabases.mockResolvedValue({ databases: [], nextPageToken: "" });
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

  it("loads every permitted section and stamps the load time", async () => {
    grant(PERMISSIONS);
    const dashboard = useDashboard();

    await dashboard.load();

    expect(dashboard.failedSections.value).toEqual([]);
    expect(dashboard.isLoading.value).toBe(false);
    expect(dashboard.lastUpdated.value).toBeInstanceOf(Date);
    expect(mocks.listInstances).toHaveBeenCalledTimes(1);
    expect(mocks.listDatabases).toHaveBeenCalledTimes(1);
    expect(mocks.listOpenLineageTasks).toHaveBeenCalledTimes(1);
  });

  it("keeps the sections that loaded and names the ones that failed", async () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});
    grant(PERMISSIONS);
    mocks.listInstances.mockRejectedValue(new Error("no sources"));
    mocks.listDatabases.mockRejectedValue(new Error("no databases"));
    const dashboard = useDashboard();

    await dashboard.load();

    expect(dashboard.failedSections.value).toEqual(["instances", "databases"]);
    expect(dashboard.lastUpdated.value).toBeInstanceOf(Date);
    expect(mocks.listOpenLineageTasks).toHaveBeenCalledTimes(1);
    consoleError.mockRestore();
  });

  it("asks for nothing the caller may not read", async () => {
    grant([]);
    const dashboard = useDashboard();

    await dashboard.load();

    expect(mocks.listInstances).not.toHaveBeenCalled();
    expect(mocks.listDatabases).not.toHaveBeenCalled();
    expect(mocks.listOpenLineageTasks).not.toHaveBeenCalled();
    expect(dashboard.failedSections.value).toEqual([]);
    expect(dashboard.isLoading.value).toBe(false);
  });
});
