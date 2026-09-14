import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { computed, ref } from "vue";
import { listDatabases } from "@/api/database";
import { listInstances } from "@/api/instance";
import {
  listOpenLineageDatasets,
  listOpenLineageRuns,
  listOpenLineageTasks,
} from "@/api/openlineage";
import { useAuthStore } from "@/store/modules/auth";
import type { Database } from "@/types/proto-es/v1/database_service_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";
import type {
  OpenLineageDatasetResource,
  OpenLineageRun,
  OpenLineageTask,
} from "@/types/proto-es/v1/openlineage_service_pb";

/**
 * The largest page the list RPCs accept. A response that fills the page means
 * more rows exist, so the dashboard reports the count as "at least this many"
 * rather than walking every page of a large workspace.
 */
const PAGE_SIZE = 1000;

/** How many OpenLineage runs the "recent activity" table shows. */
const RECENT_RUN_LIMIT = 8;

/** The parent that lists every database in the workspace, not one instance. */
const WORKSPACE_PARENT = "workspaces/-";

export interface EstateSummary {
  instanceCount: number;
  activeInstanceCount: number;
  inactiveInstanceCount: number;
  /** Active instances that have never completed a sync. */
  neverSyncedInstanceCount: number;
  databaseCount: number;
  driftedDatabaseCount: number;
  /** Databases that have never completed a sync. */
  neverSyncedDatabaseCount: number;
}

export interface OpenLineageSummary {
  jobCount: number;
  /** Jobs that produced at least one run carrying lineage. */
  lineageReadyJobCount: number;
  namespaceCount: number;
  datasetCount: number;
  internalDatasetCount: number;
  columnLineageDatasetCount: number;
}

function hasTimestamp(timestamp: Timestamp | undefined): boolean {
  return timestamp !== undefined && timestamp.seconds > 0n;
}

/** The last segment of a resource name. */
function resourceID(name: string): string {
  return name.split("/").pop() ?? name;
}

/** The instance ID a database belongs to, from its embedded instance resource. */
export function instanceIDOfDatabase(database: Database): string {
  const instanceName = database.instanceResource?.name;
  if (instanceName) {
    return resourceID(instanceName);
  }
  // The database's own name is instances/{instance}/databases/{database}.
  return database.name.split("/")[1] ?? "";
}

export function summarizeEstate(
  instances: Instance[],
  databases: Database[]
): EstateSummary {
  const activeInstances = instances.filter((instance) => instance.activation);
  return {
    instanceCount: instances.length,
    activeInstanceCount: activeInstances.length,
    inactiveInstanceCount: instances.length - activeInstances.length,
    neverSyncedInstanceCount: activeInstances.filter(
      (instance) => !hasTimestamp(instance.lastSyncTime)
    ).length,
    databaseCount: databases.length,
    driftedDatabaseCount: databases.filter((database) => database.drifted)
      .length,
    neverSyncedDatabaseCount: databases.filter(
      (database) => !hasTimestamp(database.successfulSyncTime)
    ).length,
  };
}

/** Database count per instance resource ID, for the data source table. */
export function countDatabasesByInstance(
  databases: Database[]
): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const database of databases) {
    const instanceID = instanceIDOfDatabase(database);
    if (!instanceID) {
      continue;
    }
    counts[instanceID] = (counts[instanceID] ?? 0) + 1;
  }
  return counts;
}

export function summarizeOpenLineage(
  tasks: OpenLineageTask[],
  datasets: OpenLineageDatasetResource[]
): OpenLineageSummary {
  const namespaces = new Set(
    tasks.map((task) => task.jobNamespace).filter(Boolean)
  );
  return {
    jobCount: tasks.length,
    lineageReadyJobCount: tasks.filter((task) => task.lineageRunCount > 0)
      .length,
    namespaceCount: namespaces.size,
    datasetCount: datasets.length,
    internalDatasetCount: datasets.filter((dataset) => dataset.internal).length,
    columnLineageDatasetCount: datasets.filter(
      (dataset) => dataset.supportsColumnLineage
    ).length,
  };
}

/**
 * Loads the workspace-wide numbers the landing page shows.
 *
 * Every list is capped at one page: the dashboard is a starting point, not a
 * report, and walking thousands of rows to show an exact figure would make the
 * first paint slow. Each section is fetched only when the caller holds its
 * permission, and a failing section leaves the rest of the page intact.
 */
export function useDashboard() {
  const authStore = useAuthStore();

  const instances = ref<Instance[]>([]);
  const databases = ref<Database[]>([]);
  const tasks = ref<OpenLineageTask[]>([]);
  const datasets = ref<OpenLineageDatasetResource[]>([]);
  const recentRuns = ref<OpenLineageRun[]>([]);

  const isLoading = ref(false);
  const failedSections = ref<string[]>([]);
  const lastUpdated = ref<Date | null>(null);
  const instancesTruncated = ref(false);
  const databasesTruncated = ref(false);
  const jobsTruncated = ref(false);
  const datasetsTruncated = ref(false);

  const canViewInstances = computed(() =>
    authStore.hasPermission("metaxisdata.instances.list")
  );
  const canViewDatabases = computed(() =>
    authStore.hasPermission("metaxisdata.databases.list")
  );
  const canViewOpenLineage = computed(() =>
    authStore.hasPermission("metaxisdata.openlineage.read")
  );

  const estate = computed(() =>
    summarizeEstate(instances.value, databases.value)
  );
  const openLineage = computed(() =>
    summarizeOpenLineage(tasks.value, datasets.value)
  );
  const databasesPerInstance = computed(() =>
    countDatabasesByInstance(databases.value)
  );

  async function loadInstances() {
    try {
      const response = await listInstances({ pageSize: PAGE_SIZE });
      instances.value = response.instances;
      instancesTruncated.value = response.nextPageToken !== "";
    } catch (error) {
      console.error("Failed to load data sources for the dashboard:", error);
      failedSections.value.push("instances");
    }
  }

  async function loadDatabases() {
    try {
      const response = await listDatabases({
        parent: WORKSPACE_PARENT,
        pageSize: PAGE_SIZE,
      });
      databases.value = response.databases;
      databasesTruncated.value = response.nextPageToken !== "";
    } catch (error) {
      console.error("Failed to load databases for the dashboard:", error);
      failedSections.value.push("databases");
    }
  }

  async function loadOpenLineage() {
    try {
      const [taskResponse, datasetResponse, runResponse] = await Promise.all([
        listOpenLineageTasks({
          pageSize: PAGE_SIZE,
          jobType: "",
          lineageOnly: false,
        }),
        listOpenLineageDatasets({ pageSize: PAGE_SIZE }),
        listOpenLineageRuns({ pageSize: RECENT_RUN_LIMIT }),
      ]);
      tasks.value = taskResponse.tasks;
      jobsTruncated.value = taskResponse.nextPageToken !== "";
      datasets.value = datasetResponse.datasets;
      datasetsTruncated.value = datasetResponse.nextPageToken !== "";
      recentRuns.value = runResponse.runs;
    } catch (error) {
      console.error(
        "Failed to load OpenLineage data for the dashboard:",
        error
      );
      failedSections.value.push("openlineage");
    }
  }

  async function load() {
    isLoading.value = true;
    failedSections.value = [];
    const calls: Promise<void>[] = [];
    if (canViewInstances.value) {
      calls.push(loadInstances());
    }
    if (canViewDatabases.value) {
      calls.push(loadDatabases());
    }
    if (canViewOpenLineage.value) {
      calls.push(loadOpenLineage());
    }
    await Promise.all(calls);
    lastUpdated.value = new Date();
    isLoading.value = false;
  }

  return {
    instances,
    databases,
    recentRuns,
    estate,
    openLineage,
    databasesPerInstance,
    isLoading,
    failedSections,
    lastUpdated,
    instancesTruncated,
    databasesTruncated,
    jobsTruncated,
    datasetsTruncated,
    canViewInstances,
    canViewDatabases,
    canViewOpenLineage,
    load,
  };
}
