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

    <!-- No inner padding: the table is the card's content, so its header band
         reaches the card edges like every other list page. -->
    <Card>
      <CardContent class="p-0">
        <PageState
          :loading="isLoading"
          :error="error"
        >
          <!-- Empty State -->
          <EmptyState
            v-if="databases.length === 0"
            :icon="Database"
            :title="t('databaseManagement.noDatabases')"
          >
            <Button
              variant="outline"
              size="sm"
              asChild
            >
              <RouterLink :to="{ name: 'InstanceManagement' }">
                {{ t("instanceManagement.title") }}
              </RouterLink>
            </Button>
          </EmptyState>

          <!-- Databases List -->
          <div v-else>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t("databaseManagement.database") }}</TableHead>
                  <TableHead class="hidden md:table-cell">
                    {{ t("databaseManagement.instance") }}
                  </TableHead>
                  <TableHead class="hidden md:table-cell">
                    {{ t("databaseManagement.engine") }}
                  </TableHead>
                  <TableHead
                    v-if="hasEnvironment"
                    class="hidden md:table-cell"
                  >
                    {{ t("databaseManagement.environment") }}
                  </TableHead>
                  <TableHead class="hidden md:table-cell">
                    {{ t("databaseManagement.lastSync") }}
                  </TableHead>
                  <TableHead class="hidden md:table-cell">
                    {{ t("databaseManagement.status") }}
                  </TableHead>
                  <TableHead class="w-12 text-right md:w-36">
                    <span class="sr-only md:not-sr-only">{{ t("databaseManagement.actions") }}</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow
                  v-for="database in databases"
                  :key="database.name"
                >
                  <TableCell>
                    <div class="flex items-center">
                      <Database class="mr-2 h-5 w-5 shrink-0 text-muted-foreground" />
                      <div class="min-w-0">
                        <RouterLink
                          :to="metadataLink(database.name)"
                          class="block max-w-[9rem] truncate font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 md:max-w-none"
                        >
                          {{ getDatabaseName(database.name) }}
                        </RouterLink>
                        <div
                          v-if="database.drifted"
                          class="text-xs text-warning"
                        >
                          {{ t("databaseManagement.drifted") }}
                        </div>
                        <!-- Below md the instance and status columns collapse in here. -->
                        <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 md:hidden">
                          <Badge :variant="stateBadgeVariant(database.state)">
                            {{ stateLabel(database.state, t) }}
                          </Badge>
                          <span class="max-w-[9rem] truncate text-xs text-muted-foreground">
                            {{ database.instanceResource?.title || getInstanceName(database.name) }}
                          </span>
                        </div>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell class="hidden md:table-cell">
                    <div class="min-w-0">
                      <div class="truncate font-medium">
                        {{ database.instanceResource?.title || getInstanceName(database.name) }}
                      </div>
                      <div
                        v-if="database.instanceResource?.title"
                        class="truncate text-xs text-muted-foreground"
                      >
                        {{ getInstanceName(database.name) }}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell class="hidden md:table-cell">
                    <Badge
                      variant="secondary"
                      :class="engineBadgeClass(database.instanceResource?.engine)"
                    >
                      {{ engineLabel(database.instanceResource?.engine) }}
                    </Badge>
                  </TableCell>
                  <TableCell
                    v-if="hasEnvironment"
                    class="hidden md:table-cell"
                  >
                    <EnvironmentLabel
                      :environment="database.effectiveEnvironment"
                    />
                  </TableCell>
                  <TableCell
                    class="hidden whitespace-nowrap text-muted-foreground md:table-cell"
                    :title="formatLastSyncTitle(database.successfulSyncTime)"
                  >
                    {{ formatLastSync(database.successfulSyncTime) }}
                  </TableCell>
                  <TableCell class="hidden md:table-cell">
                    <Badge :variant="stateBadgeVariant(database.state)">
                      {{ stateLabel(database.state, t) }}
                    </Badge>
                  </TableCell>
                  <TableCell class="text-right">
                    <Button
                      v-if="canSync"
                      class="justify-center md:w-28"
                      variant="outline"
                      size="sm"
                      :disabled="isDatabaseSyncing(database.name)"
                      :aria-label="t('databaseManagement.sync')"
                      :title="t('databaseManagement.sync')"
                      @click.stop="handleSyncDatabase(database.name)"
                    >
                      <Loader2
                        v-if="isDatabaseSyncing(database.name)"
                        class="h-4 w-4 animate-spin md:mr-2"
                      />
                      <RefreshCw
                        v-else
                        class="h-4 w-4 md:hidden"
                      />
                      <span class="hidden md:inline">
                        {{ isDatabaseSyncing(database.name) ? t("databaseManagement.syncing") : t("databaseManagement.sync") }}
                      </span>
                    </Button>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>

            <!-- The pager footer keeps the card's content inset. -->
            <div class="px-6 pb-6">
              <TablePager
                v-model:page-size="pageSize"
                :has-previous="hasPrevious"
                :has-next="hasNext"
                :disabled="isLoading"
                @previous="goToPreviousPage"
                @next="goToNextPage"
              />
            </div>
          </div>
        </PageState>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Database, Loader2, RefreshCw } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
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
import CardContent from "@/components/ui/card/CardContent.vue";
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
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { engineBadgeClass, engineLabel } from "@/utils/engine";
import { guidSegmentsToRouteParams } from "@/utils/guid";
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

// The Environment catalog is empty until an administrator creates entries, so
// the column would otherwise render a muted dash in every row. It comes back the
// moment any loaded database actually carries an environment.
const hasEnvironment = computed(() =>
  databases.value.some((database) => !!database.effectiveEnvironment)
);

// A database is a metadata object in its own right; its table row is the
// keyboard-reachable way into the browser (the row itself is not a link).
function metadataLink(fullName: string): {
  name: string;
  params: { guid: string[] };
} {
  const instanceId = getInstanceName(fullName);
  const databaseName = getDatabaseName(fullName);
  return {
    name: "MetadataDetail",
    params: {
      guid: guidSegmentsToRouteParams([instanceId, databaseName]),
    },
  };
}

function formatLastSync(timestamp: Timestamp | undefined): string {
  return formatRelativeTime(timestamp, locale.value);
}

function formatLastSyncTitle(timestamp: Timestamp | undefined): string {
  return formatDateTime(timestamp, locale.value);
}

onMounted(async () => {
  await Promise.all([
    instanceStore.ensureLoaded(),
    resetDatabasePage(),
    environmentStore.ensureLoaded(),
  ]);
});
</script>
