<template>
  <div class="space-y-6">
    <!-- Page Header -->
    <div class="flex items-center gap-4">
      <Button
        variant="ghost"
        size="icon"
        @click="goBack"
      >
        <ArrowLeft class="h-5 w-5" />
      </Button>
      <div class="flex-1">
        <h1 class="text-2xl font-bold tracking-tight">
          {{ instance?.title || instanceId }}
        </h1>
        <p class="text-muted-foreground">
          {{ t("instanceDetail.description") }}
        </p>
      </div>
    </div>

    <!-- Instance Info Card -->
    <Card v-if="isLoadingInstance">
      <CardHeader>
        <CardTitle>{{ t("instanceDetail.instanceInfo") }}</CardTitle>
      </CardHeader>
      <CardContent>
        <div class="p-8 flex justify-center">
          <AppLoading />
        </div>
      </CardContent>
    </Card>

    <Card v-else-if="instance">
      <CardHeader>
        <CardTitle>{{ t("instanceDetail.instanceInfo") }}</CardTitle>
      </CardHeader>
      <CardContent>
        <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
          <div>
            <p class="text-sm text-muted-foreground">
              {{ t("instanceManagement.engine") }}
            </p>
            <p class="font-medium">
              {{ getEngineLabel(instance.engine) }}
            </p>
          </div>
          <div>
            <p class="text-sm text-muted-foreground">
              {{ t("instanceManagement.host") }}
            </p>
            <p class="font-medium">
              {{ getHostInfo(instance) }}
            </p>
          </div>
          <div>
            <p class="text-sm text-muted-foreground">
              {{ t("instanceManagement.environment") }}
            </p>
            <p class="font-medium">
              {{ getEnvironmentId(instance.environment) }}
            </p>
          </div>
          <div>
            <p class="text-sm text-muted-foreground">
              {{ t("instanceManagement.status") }}
            </p>
            <Badge :variant="instance.activation ? 'success' : 'secondary'">
              {{ instance.activation ? t("instanceManagement.active") : t("instanceManagement.inactive") }}
            </Badge>
          </div>
        </div>
      </CardContent>
    </Card>

    <!-- Databases Section -->
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <CardTitle>{{ t("instanceDetail.databases") }}</CardTitle>
          <div class="text-sm text-muted-foreground">
            {{ t("instanceDetail.totalDatabases", { count: databases.length }) }}
          </div>
        </div>
      </CardHeader>

      <!-- Loading State -->
      <div
        v-if="isLoadingDatabases"
        class="p-8 flex justify-center"
      >
        <AppLoading />
      </div>

      <!-- Error State -->
      <div
        v-else-if="databaseError"
        class="p-8 text-center text-destructive"
      >
        {{ databaseError }}
      </div>

      <!-- Empty State -->
      <div
        v-else-if="databases.length === 0"
        class="p-8 text-center text-muted-foreground"
      >
        <Database class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
        <p>{{ t("instanceDetail.noDatabases") }}</p>
      </div>

      <!-- Databases List -->
      <Table v-else>
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("instanceDetail.databaseName") }}</TableHead>
            <TableHead>{{ t("instanceDetail.project") }}</TableHead>
            <TableHead>{{ t("instanceDetail.environment") }}</TableHead>
            <TableHead>{{ t("instanceDetail.schemaVersion") }}</TableHead>
            <TableHead>{{ t("instanceDetail.lastSync") }}</TableHead>
            <TableHead>{{ t("instanceDetail.status") }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="database in databases"
            :key="database.name"
          >
            <TableCell>
              <div class="flex items-center">
                <Database class="h-4 w-4 mr-2 text-muted-foreground" />
                <div>
                  <div class="font-medium">
                    {{ getDatabaseName(database.name) }}
                  </div>
                  <div class="text-xs text-muted-foreground">
                    {{ database.name }}
                  </div>
                </div>
              </div>
            </TableCell>
            <TableCell class="text-muted-foreground">
              {{ getProjectId(database.project) }}
            </TableCell>
            <TableCell class="text-muted-foreground">
              {{ getEnvironmentId(database.effectiveEnvironment) }}
            </TableCell>
            <TableCell class="text-muted-foreground">
              {{ database.schemaVersion || "-" }}
            </TableCell>
            <TableCell class="text-muted-foreground">
              {{ formatLastSync(database.successfulSyncTime) }}
            </TableCell>
            <TableCell>
              <Badge
                :variant="database.state === State.ACTIVE ? 'success' : 'secondary'"
              >
                {{ getStateLabel(database.state) }}
              </Badge>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </Card>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { ArrowLeft, Database } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listDatabases } from "@/api/database";
import { listInstances } from "@/api/instance";
import AppLoading from "@/components/common/AppLoading.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { Engine, State } from "@/types/proto-es/v1/common_pb";
import type { Database as DatabaseType } from "@/types/proto-es/v1/database_service_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();

// State
const instanceId = computed(() => route.params.instanceId as string);
const instance = ref<Instance | null>(null);
const databases = ref<DatabaseType[]>([]);
const isLoadingInstance = ref(false);
const isLoadingDatabases = ref(false);
const databaseError = ref<string | null>(null);

// Methods
function goBack() {
  router.push({ name: "InstanceManagement" });
}

function getDatabaseName(name: string): string {
  // Format: instances/{instance}/databases/{database} -> return database
  const parts = name.split("/");
  return parts[parts.length - 1] || name;
}

function getProjectId(project: string): string {
  // Format: projects/{id} -> return id
  if (!project) return "-";
  return project.replace("projects/", "");
}

function getEnvironmentId(environment: string): string {
  // Format: environments/{id} -> return id
  if (!environment) return "-";
  return environment.replace("environments/", "");
}

function getHostInfo(instance: Instance): string {
  const adminDataSource = instance.dataSources.find((ds) => ds.type === 1); // ADMIN type
  if (adminDataSource) {
    const port = adminDataSource.port ? `:${adminDataSource.port}` : "";
    return `${adminDataSource.host}${port}`;
  }
  return "-";
}

function getEngineLabel(engine: Engine): string {
  const engineLabels: Record<number, string> = {
    [Engine.ENGINE_UNSPECIFIED]: "Unknown",
    [Engine.CLICKHOUSE]: "ClickHouse",
    [Engine.MYSQL]: "MySQL",
    [Engine.POSTGRES]: "PostgreSQL",
    [Engine.SNOWFLAKE]: "Snowflake",
    [Engine.SQLITE]: "SQLite",
    [Engine.TIDB]: "TiDB",
    [Engine.MONGODB]: "MongoDB",
    [Engine.REDIS]: "Redis",
    [Engine.ORACLE]: "Oracle",
    [Engine.SPANNER]: "Spanner",
    [Engine.MSSQL]: "SQL Server",
    [Engine.REDSHIFT]: "Redshift",
    [Engine.MARIADB]: "MariaDB",
    [Engine.OCEANBASE]: "OceanBase",
    [Engine.STARROCKS]: "StarRocks",
    [Engine.DORIS]: "Doris",
    [Engine.HIVE]: "Hive",
    [Engine.ELASTICSEARCH]: "Elasticsearch",
    [Engine.BIGQUERY]: "BigQuery",
    [Engine.DYNAMODB]: "DynamoDB",
    [Engine.DATABRICKS]: "Databricks",
    [Engine.COCKROACHDB]: "CockroachDB",
    [Engine.COSMOSDB]: "CosmosDB",
    [Engine.TRINO]: "Trino",
    [Engine.CASSANDRA]: "Cassandra",
  };
  return engineLabels[engine] || "Unknown";
}

function getStateLabel(state: State): string {
  const stateLabels: Record<number, string> = {
    [State.STATE_UNSPECIFIED]: t("instanceDetail.stateUnspecified"),
    [State.ACTIVE]: t("instanceDetail.stateActive"),
    [State.DELETED]: t("instanceDetail.stateDeleted"),
  };
  return stateLabels[state] || "Unknown";
}

function formatLastSync(timestamp: Timestamp | undefined): string {
  if (!timestamp?.seconds) return "-";
  const d = new Date(Number(timestamp.seconds) * 1000);
  const formatter = new Intl.DateTimeFormat(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });

  return formatter.format(d);
}

async function fetchInstance() {
  isLoadingInstance.value = true;
  try {
    const response = await listInstances({
      pageSize: 100,
      showDeleted: false,
      filter: `resource_id == "${instanceId.value}"`,
    });
    if (response.instances.length > 0) {
      instance.value = response.instances[0];
    } else {
      handleError(
        new Error("Instance not found"),
        t("instanceDetail.fetchInstanceError")
      );
      router.push({ name: "InstanceManagement" });
    }
  } catch (e) {
    handleError(e, t("instanceDetail.fetchInstanceError"));
    router.push({ name: "InstanceManagement" });
  } finally {
    isLoadingInstance.value = false;
  }
}

async function fetchDatabases() {
  isLoadingDatabases.value = true;
  databaseError.value = null;
  try {
    const response = await listDatabases({
      parent: `instances/${instanceId.value}`,
      pageSize: 100,
      showDeleted: false,
    });
    databases.value = response.databases;
  } catch (e) {
    databaseError.value = t("instanceDetail.fetchDatabasesError");
    console.error("Failed to fetch databases:", e);
  } finally {
    isLoadingDatabases.value = false;
  }
}

// Lifecycle
onMounted(async () => {
  await fetchInstance();
  await fetchDatabases();
});
</script>
