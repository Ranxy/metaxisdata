<template>
  <div class="space-y-4">
    <PageHeader :title="t('databaseManagement.title')" />

    <!-- Advanced Search Bar -->
    <AdvancedSearchBar
      :instances="instanceStore.active"
      :engine-options="engineOptions"
      :search-placeholder="t('databaseManagement.searchPlaceholder')"
      @update:filters="handleFiltersUpdate"
    />

    <!-- Databases Table -->
    <Card>
      <PageState
        :loading="isLoading"
        :error="error"
      >
        <!-- Empty State -->
        <EmptyState
          v-if="databases.length === 0"
          :icon="Database"
          :title="t('databaseManagement.noDatabases')"
        />

        <!-- Databases List -->
        <div v-else>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("databaseManagement.database") }}</TableHead>
                <TableHead>{{ t("databaseManagement.instance") }}</TableHead>
                <TableHead>{{ t("databaseManagement.engine") }}</TableHead>
                <TableHead>{{ t("databaseManagement.environment") }}</TableHead>
                <TableHead>{{ t("databaseManagement.lastSync") }}</TableHead>
                <TableHead>{{ t("databaseManagement.status") }}</TableHead>
                <TableHead class="w-36 text-right">{{ t("databaseManagement.actions") }}</TableHead>
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
                    <div
                      v-if="database.instanceResource?.title"
                      class="text-xs text-muted-foreground"
                    >
                      {{ getInstanceName(database.name) }}
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <Badge
                    variant="secondary"
                    :class="engineBadgeClass(database.instanceResource?.engine)"
                  >
                    {{ engineLabel(database.instanceResource?.engine) }}
                  </Badge>
                </TableCell>
                <TableCell>
                  <EnvironmentLabel
                    :environment="database.effectiveEnvironment"
                  />
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
                  <Badge :variant="stateBadgeVariant(database.state)">
                    {{ stateLabel(database.state, t) }}
                  </Badge>
                </TableCell>
                <TableCell class="w-36 text-right">
                  <Button
                    v-if="canSync"
                    class="w-28 justify-center"
                    variant="outline"
                    size="sm"
                    :disabled="isDatabaseSyncing(database.name)"
                    @click.stop="handleSyncDatabase(database.name)"
                  >
                    <Loader2
                      v-if="isDatabaseSyncing(database.name)"
                      class="h-4 w-4 mr-2 animate-spin"
                    />
                    <span>
                      {{ isDatabaseSyncing(database.name) ? t("databaseManagement.syncing") : t("databaseManagement.sync") }}
                    </span>
                  </Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>

          <TablePager
            v-model:page-size="pageSize"
            :has-previous="hasPrevious"
            :has-next="hasNext"
            :disabled="isLoading"
            @previous="goToPreviousPage"
            @next="goToNextPage"
          />
        </div>
      </PageState>
    </Card>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Database, Loader2 } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listDatabases, syncDatabase } from "@/api/database";
import type { ActiveFilter } from "@/components/common/AdvancedSearchBar.vue";
import AdvancedSearchBar from "@/components/common/AdvancedSearchBar.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import EnvironmentLabel from "@/components/common/EnvironmentLabel.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import Badge from "@/components/ui/badge/Badge.vue";
import Button from "@/components/ui/button/Button.vue";
import Card from "@/components/ui/card/Card.vue";
import Table from "@/components/ui/table/Table.vue";
import TableBody from "@/components/ui/table/TableBody.vue";
import TableCell from "@/components/ui/table/TableCell.vue";
import TableHead from "@/components/ui/table/TableHead.vue";
import TableHeader from "@/components/ui/table/TableHeader.vue";
import TableRow from "@/components/ui/table/TableRow.vue";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import { useEnvironmentStore } from "@/store/modules/environment";
import { useInstanceStore } from "@/store/modules/instance";
import type { Database as DatabaseType } from "@/types/proto-es/v1/database_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { engineBadgeClass, engineLabel } from "@/utils/engine";
import { stateBadgeVariant, stateLabel } from "@/utils/state";

const { t, locale } = useI18n();
const { handleError } = useErrorHandler();
const authStore = useAuthStore();
const environmentStore = useEnvironmentStore();
const instanceStore = useInstanceStore();

// Syncing a database rewrites its stored schema, so it needs the sync
// permission; browsing the list only needs databases.list.
const canSync = computed(() =>
  authStore.hasPermission("metaxisdata.databases.sync")
);

const error = ref("");
const syncingDatabases = ref<Record<string, boolean>>({});

const currentFilters = ref<ActiveFilter[]>([]);
const pageSize = ref(50);

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
  { value: "STARROCKS", label: "StarRocks" },
  { value: "DORIS", label: "Doris" },
  { value: "SQLITE", label: "SQLite" },
]);

function escapeCelValue(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

function handleFiltersUpdate(filters: ActiveFilter[]) {
  currentFilters.value = filters;
  void resetDatabasePage();
}

const {
  items: databases,
  isLoading,
  hasNext,
  hasPrevious,
  reset: resetDatabasePage,
  refresh: refreshDatabasePage,
  goNext: goToNextPage,
  goPrevious: goToPreviousPage,
} = usePagedFetch<DatabaseType>({
  fetchPage: async (pageToken, signal) => {
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
      pageSize: pageSize.value,
      pageToken,
      filter: filterString,
      showDeleted: false,
      signal,
    });

    return { items: response.databases, nextPageToken: response.nextPageToken };
  },
  onError: (e) => {
    error.value = handleError(e, "databaseManagement.fetchError");
  },
});

// A bigger page restarts the walk: a cursor belongs to the query that produced it.
watch(pageSize, () => void resetDatabasePage());

function isDatabaseSyncing(name: string): boolean {
  return !!syncingDatabases.value[name];
}

async function handleSyncDatabase(name: string) {
  syncingDatabases.value = {
    ...syncingDatabases.value,
    [name]: true,
  };

  try {
    await syncDatabase(name);
    notify.success(t("databaseManagement.syncSuccess"));
    await refreshDatabasePage();
  } catch (e: unknown) {
    handleError(e, "databaseManagement.syncError");
  } finally {
    const nextSyncingDatabases = { ...syncingDatabases.value };
    delete nextSyncingDatabases[name];
    syncingDatabases.value = nextSyncingDatabases;
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

function formatLastSync(timestamp: Timestamp | undefined): string {
  return formatDateTime(timestamp, locale.value, { seconds: true });
}

onMounted(async () => {
  await Promise.all([
    instanceStore.ensureLoaded(),
    resetDatabasePage(),
    environmentStore.ensureLoaded(),
  ]);
});
</script>
