<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.jobs')"
      :description="t('openlineage.jobsDescription')"
    />

    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <!-- Counted by the server across the whole registry: the table shows one
           page at a time, so anything derived from the rows on screen would be a
           summary of one page. -->
      <StatCard
        :label="t('openlineage.jobsTotal')"
        :value="totalJobCount"
      />
      <StatCard
        :label="t('openlineage.runsTotal')"
        :value="totalRunCount"
      />
      <StatCard
        :label="t('openlineage.activeNamespaces')"
        :value="namespaceCount"
      />
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <AdvancedSearchBar
        ref="searchBar"
        class="min-w-0 flex-1"
        :filter-categories="filterCategories"
        :initial-filters="initialFilters"
        :search-placeholder="t('openlineage.searchJobsPlaceholder')"
        @update:filters="handleFiltersUpdate"
      />
      <div class="flex items-center gap-2">
        <Checkbox
          id="jobs-lineage-only"
          :checked="lineageOnly"
          @update:checked="lineageOnly = $event === true"
        />
        <Label for="jobs-lineage-only" class="cursor-pointer text-sm whitespace-nowrap">
          {{ t("openlineage.onlyLineage") }}
        </Label>
      </div>
      <Button variant="outline" size="sm" @click="resetFilters">
        {{ t("openlineage.clearFilters") }}
      </Button>
    </div>

    <Card>
      <CardContent class="pt-6">
        <PageState :loading="isLoading">
          <EmptyState
            v-if="tasks.length === 0"
            :icon="ScrollText"
            :title="t('openlineageSettings.noTasks')"
          />
          <!-- Ten columns of long values overflowed the card by 315px. The
               lineage count now shares the run-count cell, the coverage badge
               is gone (it only restated that count), the row actions collapse
               into a menu, and job type is left to the filter menu — it is a
               low-cardinality constant in practice and cost a whole column.
               `whitespace-nowrap` keeps Chinese labels from stacking one
               character per line. -->
          <Table
            v-else
            class="[&_th]:whitespace-nowrap"
          >
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("openlineageSettings.namespace") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.jobName") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.integration") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.latestEventTime") }}</TableHead>
                <TableHead>{{ t("openlineage.latestRunStatus") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.runCount") }}</TableHead>
                <TableHead class="sticky right-0 bg-background text-right">
                  {{ t("openlineageSettings.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <!-- The whole row opens the job: reaching its detail used to mean
                   opening the action menu and picking an item out of it. The
                   name stays a real link, so the row has a visible affordance
                   and one keyboard stop. -->
              <TableRow
                v-for="task in tasks"
                :key="task.guid"
                class="cursor-pointer"
                @click="handleRowClick(task.guid)"
              >
                <TableCell class="font-mono text-sm">{{ task.jobNamespace }}</TableCell>
                <TableCell>
                  <RouterLink
                    class="block max-w-64 truncate font-medium text-primary hover:underline"
                    :title="task.jobName"
                    :to="{
                      name: 'OpenLineageTaskDetail',
                      params: { guid: task.guid },
                      query: { from: route.fullPath },
                    }"
                    @click.stop
                  >
                    {{ task.jobName }}
                  </RouterLink>
                </TableCell>
                <TableCell>{{ task.integration || "-" }}</TableCell>
                <TableCell class="whitespace-nowrap">{{ formatTimestamp(task.latestEventTime) }}</TableCell>
                <TableCell>
                  <Badge :variant="openLineageStatusVariant(task.latestEventType)">
                    {{ task.latestEventType || "-" }}
                  </Badge>
                </TableCell>
                <TableCell class="whitespace-nowrap">
                  {{ task.runCount }}
                  <span class="text-muted-foreground">
                    / {{ task.lineageRunCount }} {{ t("openlineage.lineageEvents") }}
                  </span>
                </TableCell>
                <TableCell
                  class="sticky right-0 bg-background text-right"
                  @click.stop
                >
                  <DropdownMenu>
                    <DropdownMenuTrigger as-child>
                      <Button
                        variant="ghost"
                        size="icon"
                        class="h-8 w-8"
                        :title="t('openlineage.moreActions')"
                        :aria-label="t('openlineage.moreActions')"
                      >
                        <MoreHorizontal class="h-4 w-4 text-muted-foreground" />
                      </Button>
                    </DropdownMenuTrigger>
                    <!-- Detail is not listed: the row and the name already open
                         it, so this menu is only the other destinations. -->
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem @select="openGraph(task.guid)">
                        <Network class="mr-2 h-4 w-4" />
                        {{ t("openlineage.openGraph") }}
                      </DropdownMenuItem>
                      <DropdownMenuItem @select="openEvents(task)">
                        <Files class="mr-2 h-4 w-4" />
                        {{ t("openlineage.openEvents") }}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </PageState>

        <TablePager
          v-model:page-size="pageSize"
          :has-previous="hasPrevious"
          :has-next="hasNext"
          :disabled="isLoading"
          @previous="goPrevious"
          @next="goNext"
        />
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Files, MoreHorizontal, Network, ScrollText } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute, useRouter } from "vue-router";
import {
  listOpenLineageFilterOptions,
  listOpenLineageTasks,
} from "@/api/openlineage";
import type {
  ActiveFilter,
  FilterCategory,
} from "@/components/common/AdvancedSearchBar.vue";
import AdvancedSearchBar from "@/components/common/AdvancedSearchBar.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import StatCard from "@/components/common/StatCard.vue";
import TablePager from "@/components/common/TablePager.vue";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import type {
  ListOpenLineageFilterOptionsResponse,
  OpenLineageFilterOption,
  OpenLineageTask,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";
import { openLineageStatusVariant } from "@/utils/openlineageStatus";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();

const pageSize = ref(50);
const lineageOnly = ref(route.query.lineageOnly !== "false");
const facets = ref<ListOpenLineageFilterOptionsResponse | null>(null);
const searchBar = ref<InstanceType<typeof AdvancedSearchBar> | null>(null);

/**
 * A reload or a shared link carries whatever the filters last wrote. Reading
 * them back is what makes the URL the filter's home instead of a write-only log.
 */
function queryFilters(): ActiveFilter[] {
  const spec: Array<[string, string, string]> = [
    ["name", "search", t("common.filter.name")],
    ["namespace", "namespace", t("openlineageSettings.namespace")],
    ["jobType", "jobType", t("openlineageSettings.jobType")],
  ];
  return spec.flatMap(([type, key, label]) => {
    const value = route.query[key];
    return typeof value === "string" && value
      ? [{ id: `query-${key}`, type, label, value, displayValue: value }]
      : [];
  });
}

const initialFilters = queryFilters();
const activeFilters = ref<ActiveFilter[]>(initialFilters);

function filterValue(type: string): string {
  return (
    activeFilters.value.find((filter) => filter.type === type)?.value ?? ""
  );
}

// The filter menus read their values from the server: the table takes one page
// at a time, so a menu built from the rows on screen would offer only the
// namespaces that happen to be on that page. The summary cards read the same
// answer, so it is fetched once and kept.
async function loadFacets(): Promise<ListOpenLineageFilterOptionsResponse | null> {
  if (!facets.value) {
    try {
      facets.value = await listOpenLineageFilterOptions();
    } catch (error) {
      // The menus and the summary cards are an enhancement; a failure must not
      // take the table down with it. Nothing is cached, so the next menu open
      // retries.
      handleError(error);
    }
  }
  return facets.value;
}

function asFilterOptions(values: OpenLineageFilterOption[]) {
  return values.map((value) => ({ value: value.value, label: value.value }));
}

const filterCategories = computed<FilterCategory[]>(() => [
  {
    type: "namespace",
    label: t("openlineageSettings.namespace"),
    icon: "📦",
    options: async () =>
      asFilterOptions((await loadFacets())?.jobNamespaces ?? []),
  },
  {
    type: "jobType",
    label: t("openlineageSettings.jobType"),
    icon: "⚙️",
    options: async () => asFilterOptions((await loadFacets())?.jobTypes ?? []),
  },
]);

const {
  items: tasks,
  isLoading,
  hasNext,
  hasPrevious,
  reset,
  goNext,
  goPrevious,
} = usePagedFetch<OpenLineageTask>({
  fetchPage: async (pageToken, signal) => {
    const response = await listOpenLineageTasks({
      pageSize: pageSize.value,
      pageToken,
      search: filterValue("name"),
      jobNamespace: filterValue("namespace"),
      jobType: filterValue("jobType"),
      lineageOnly: lineageOnly.value,
      signal,
    });
    return { items: response.tasks, nextPageToken: response.nextPageToken };
  },
  onError: handleError,
});

const totalJobCount = computed(() => Number(facets.value?.totalJobs ?? 0));
const totalRunCount = computed(() => Number(facets.value?.totalRuns ?? 0));
const namespaceCount = computed(() =>
  Number(facets.value?.totalJobNamespaces ?? 0)
);

function handleFiltersUpdate(filters: ActiveFilter[]) {
  activeFilters.value = filters;
  const nextQuery: Record<string, string> = {};

  const nameFilter = filters.find((filter) => filter.type === "name");
  if (nameFilter?.value) {
    nextQuery.search = nameFilter.value;
  }
  const namespaceFilter = filters.find((filter) => filter.type === "namespace");
  if (namespaceFilter?.value) {
    nextQuery.namespace = namespaceFilter.value;
  }
  const jobTypeFilter = filters.find((filter) => filter.type === "jobType");
  if (jobTypeFilter?.value) {
    nextQuery.jobType = jobTypeFilter.value;
  }

  router.replace({ query: nextQuery });
  void reset();
}

function resetFilters() {
  // The bar owns the pills and the search box; clearing it emits an empty filter
  // list, and that handler is what rewrites the URL and restarts the walk.
  lineageOnly.value = true;
  searchBar.value?.clear();
}

// A filter, the lineage toggle and the page size each restart the walk at its
// first page: a cursor belongs to the query that produced it.
watch([lineageOnly, pageSize], () => {
  void reset();
});

function formatTimestamp(ts: Timestamp | undefined): string {
  return formatDateTime(ts, locale.value);
}

function openDetail(guid: string) {
  router.push({
    name: "OpenLineageTaskDetail",
    params: { guid },
    query: { from: route.fullPath },
  });
}

/**
 * A click anywhere in the row opens the job. A click that ends a text selection
 * is someone copying an identifier out of the table, not asking to navigate, so
 * it is left alone.
 */
function handleRowClick(guid: string) {
  if (window.getSelection()?.toString()) {
    return;
  }
  openDetail(guid);
}

function openGraph(guid: string) {
  router.push({
    name: "LineageGraph",
    params: { guid: guidToRouteParams(guid) },
    query: {
      metaType: "100",
      from: route.fullPath,
    },
  });
}

function openEvents(task: OpenLineageTask) {
  router.push({
    name: "OpenLineageEvents",
    query: {
      search: task.jobName,
      namespace: task.jobNamespace,
      from: route.fullPath,
    },
  });
}

onMounted(async () => {
  void reset();
  void loadFacets();
});
</script>