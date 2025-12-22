<template>
  <div class="space-y-6">
    <!-- Page Header -->
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ t("databaseManagement.title") }}
        </h1>
        <p class="text-muted-foreground">
          {{ t("databaseManagement.description") }}
        </p>
      </div>
    </div>

    <!-- Advanced Search Bar -->
    <AdvancedSearchBar
      :instances="instances"
      :engine-options="engineOptions"
      @update:filters="handleFiltersUpdate"
    />

    <!-- Databases Table -->
    <Card>
      <!-- Loading State -->
      <div
        v-if="isLoading"
        class="p-8 flex justify-center"
      >
        <AppLoading />
      </div>

      <!-- Error State -->
      <div
        v-else-if="error"
        class="p-8 text-center text-destructive"
      >
        {{ error }}
      </div>

      <!-- Empty State -->
      <div
        v-else-if="databases.length === 0"
        class="p-8 text-center text-muted-foreground"
      >
        <Database class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
        <p>{{ t("databaseManagement.noDatabases") }}</p>
      </div>

      <!-- Databases List -->
      <div v-else>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("databaseManagement.database") }}</TableHead>
              <TableHead>{{ t("databaseManagement.instance") }}</TableHead>
              <TableHead>{{ t("databaseManagement.engine") }}</TableHead>
              <TableHead>{{ t("databaseManagement.environment") }}</TableHead>
              <TableHead>{{ t("databaseManagement.project") }}</TableHead>
              <TableHead>{{ t("databaseManagement.lastSync") }}</TableHead>
              <TableHead>{{ t("databaseManagement.status") }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="database in databases"
              :key="database.name"
              class="cursor-pointer hover:bg-muted/50"
            >
              <TableCell>
                <div class="flex items-center">
                  <Database class="h-5 w-5 mr-2 text-muted-foreground" />
                  <div>
                    <div class="font-medium">
                      {{ getDatabaseName(database.name) }}
                    </div>
                    <div
                      v-if="database.drifted"
                      class="text-xs text-orange-600"
                    >
                      {{ t("databaseManagement.drifted") }}
                    </div>
                  </div>
                </div>
              </TableCell>
              <TableCell>
                <div>
                  <div class="font-medium">
                    {{ database.instanceResource?.title || getInstanceName(database.name) }}
                  </div>
                  <div class="text-xs text-muted-foreground">
                    {{ getInstanceName(database.name) }}
                  </div>
                </div>
              </TableCell>
              <TableCell>
                <Badge :variant="getEngineBadgeVariant(database.instanceResource?.engine)">
                  {{ getEngineLabel(database.instanceResource?.engine) }}
                </Badge>
              </TableCell>
              <TableCell>
                <Badge
                  v-if="database.effectiveEnvironment"
                  :variant="getEnvironmentVariant(database.effectiveEnvironment)"
                >
                  {{ getEnvironmentLabel(database.effectiveEnvironment) }}
                </Badge>
                <span
                  v-else
                  class="text-muted-foreground"
                >-</span>
              </TableCell>
              <TableCell>
                <span v-if="database.project">
                  {{ getProjectName(database.project) }}
                </span>
                <span
                  v-else
                  class="text-muted-foreground"
                >
                  {{ t("databaseManagement.unassigned") }}
                </span>
              </TableCell>
              <TableCell>
                <div
                  v-if="database.successfulSyncTime"
                  class="text-sm"
                >
                  {{ formatLastSync(database.successfulSyncTime) }}
                </div>
                <span
                  v-else
                  class="text-muted-foreground"
                >-</span>
              </TableCell>
              <TableCell>
                <Badge :variant="getStateBadgeVariant(database.state)">
                  {{ getStateLabel(database.state) }}
                </Badge>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>

        <!-- Pagination -->
        <div
          v-if="hasNextPage || hasPreviousPage"
          class="border-t p-4 flex items-center justify-between"
        >
          <div class="text-sm text-muted-foreground">
            {{ t("databaseManagement.showingResults", { total: databases.length }) }}
          </div>
          <div class="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              :disabled="!hasPreviousPage"
              @click="goToPreviousPage"
            >
              {{ t("common.previous") }}
            </Button>
            <Button
              variant="outline"
              size="sm"
              :disabled="!hasNextPage"
              @click="goToNextPage"
            >
              {{ t("common.next") }}
            </Button>
          </div>
        </div>
      </div>
    </Card>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Database } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { toast } from "vue-sonner";
import { listDatabases } from "@/api/database";
import { listInstances } from "@/api/instance";
import type { ActiveFilter } from "@/components/common/AdvancedSearchBar.vue";
import AdvancedSearchBar from "@/components/common/AdvancedSearchBar.vue";
import AppLoading from "@/components/common/AppLoading.vue";
import Badge from "@/components/ui/badge/Badge.vue";
import Button from "@/components/ui/button/Button.vue";
import Card from "@/components/ui/card/Card.vue";
import Table from "@/components/ui/table/Table.vue";
import TableBody from "@/components/ui/table/TableBody.vue";
import TableCell from "@/components/ui/table/TableCell.vue";
import TableHead from "@/components/ui/table/TableHead.vue";
import TableHeader from "@/components/ui/table/TableHeader.vue";
import TableRow from "@/components/ui/table/TableRow.vue";
import { Engine, State } from "@/types/proto-es/v1/common_pb";
import type { Database as DatabaseType } from "@/types/proto-es/v1/database_service_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";

const { t, locale } = useI18n();

const isLoading = ref(false);
const error = ref("");
const databases = ref<DatabaseType[]>([]);
const instances = ref<Instance[]>([]);
const nextPageToken = ref("");
const previousPageTokens = ref<string[]>([]);

const currentFilters = ref<ActiveFilter[]>([]);

const engineOptions = computed(() => [
  { value: "MYSQL", label: "MySQL" },
  { value: "POSTGRES", label: "PostgreSQL" },
  { value: "MONGODB", label: "MongoDB" },
  { value: "REDIS", label: "Redis" },
  { value: "CLICKHOUSE", label: "ClickHouse" },
  { value: "TIDB", label: "TiDB" },
  { value: "ORACLE", label: "Oracle" },
  { value: "MSSQL", label: "SQL Server" },
  { value: "MARIADB", label: "MariaDB" },
  { value: "SQLITE", label: "SQLite" },
]);

const hasNextPage = computed(() => !!nextPageToken.value);
const hasPreviousPage = computed(() => previousPageTokens.value.length > 0);

function escapeCelValue(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

function handleFiltersUpdate(filters: ActiveFilter[]) {
  currentFilters.value = filters;
  previousPageTokens.value = [];
  fetchDatabases();
}

async function fetchDatabases(pageToken = "") {
  isLoading.value = true;
  error.value = "";

  try {
    const filterParts: string[] = [];

    for (const filter of currentFilters.value) {
      if (filter.type === "name" && filter.value) {
        filterParts.push(`name.matches("${escapeCelValue(filter.value)}")`);
      } else if (filter.type === "instance" && filter.value) {
        filterParts.push(`instance == "${escapeCelValue(filter.value)}"`);
      } else if (filter.type === "environment" && filter.value) {
        filterParts.push(`environment == "${escapeCelValue(filter.value)}"`);
      } else if (filter.type === "engine" && filter.value) {
        filterParts.push(`engine == "${escapeCelValue(filter.value)}"`);
      }
    }

    const filterString = filterParts.join(" && ");

    const response = await listDatabases({
      parent: "workspaces/-",
      pageSize: 50,
      pageToken,
      filter: filterString,
      showDeleted: false,
    });

    databases.value = response.databases;
    nextPageToken.value = response.nextPageToken;
  } catch (e: unknown) {
    const errorMessage = e instanceof Error ? e.message : String(e);
    error.value = errorMessage || t("databaseManagement.fetchError");
    toast.error(error.value);
  } finally {
    isLoading.value = false;
  }
}

async function fetchInstances() {
  try {
    const response = await listInstances({ pageSize: 1000 });
    instances.value = response.instances;
  } catch (e: unknown) {
    console.error("Failed to fetch instances:", e);
  }
}

function goToNextPage() {
  if (nextPageToken.value) {
    previousPageTokens.value.push(nextPageToken.value);
    fetchDatabases(nextPageToken.value);
  }
}

function goToPreviousPage() {
  if (previousPageTokens.value.length > 0) {
    const token = previousPageTokens.value.pop() || "";
    fetchDatabases(token);
  }
}

function getDatabaseName(fullName: string): string {
  const parts = fullName.split("/");
  return parts[parts.length - 1] || fullName;
}

function getInstanceName(fullName: string): string {
  const parts = fullName.split("/");
  return parts.length >= 2 ? parts[1] : "";
}

function getProjectName(fullName: string): string {
  const parts = fullName.split("/");
  return parts[parts.length - 1] || fullName;
}

function getEngineLabel(engine?: Engine): string {
  if (!engine) return "Unknown";
  const option = engineOptions.value.find((e) => e.value === Engine[engine]);
  return option?.label || Engine[engine];
}

function getEngineBadgeVariant(
  _engine?: Engine
): "default" | "secondary" | "outline" {
  return "secondary";
}

function getEnvironmentLabel(environment: string): string {
  const parts = environment.split("/");
  return parts[parts.length - 1] || environment;
}

function getEnvironmentVariant(
  environment: string
): "default" | "secondary" | "destructive" | "outline" {
  const env = environment.toLowerCase();
  if (env.includes("prod")) return "destructive";
  if (env.includes("staging")) return "default";
  if (env.includes("test")) return "secondary";
  return "outline";
}

function getStateLabel(state: State): string {
  switch (state) {
    case State.ACTIVE:
      return t("databaseManagement.stateActive");
    case State.DELETED:
      return t("databaseManagement.stateDeleted");
    default:
      return t("databaseManagement.stateUnknown");
  }
}

function getStateBadgeVariant(
  state: State
): "default" | "secondary" | "destructive" | "outline" {
  switch (state) {
    case State.ACTIVE:
      return "default";
    case State.DELETED:
      return "destructive";
    default:
      return "secondary";
  }
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

onMounted(async () => {
  await Promise.all([fetchInstances(), fetchDatabases()]);
});
</script>
