<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.datasets')"
      :description="t('openlineage.datasetsDescription')"
    />

    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <StatCard
        :label="t('openlineage.visibleDatasets')"
        :value="datasets.length"
      />
      <StatCard
        :label="t('openlineage.internalDatasets')"
        :value="internalDatasetCount"
      />
      <StatCard
        :label="t('openlineage.columnLineageDatasets')"
        :value="columnLineageDatasetCount"
      />
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <AdvancedSearchBar
        class="min-w-0 flex-1"
        :filter-categories="filterCategories"
        :search-placeholder="t('openlineage.searchDatasetsPlaceholder')"
        @update:filters="handleFiltersUpdate"
      />
      <div class="flex items-center gap-2">
        <Checkbox
          id="datasets-column-lineage-only"
          :checked="columnLineageOnly"
          @update:checked="columnLineageOnly = $event === true"
        />
        <Label for="datasets-column-lineage-only" class="cursor-pointer text-sm whitespace-nowrap">
          {{ t("openlineage.onlyColumnLineage") }}
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
            v-if="datasets.length === 0"
            :icon="Database"
            :title="t('openlineage.noDatasets')"
          />
          <!-- Ten columns of long values overflowed the card by 740px — the four
               action buttons alone took 524px of it. The namespace is now the
               dataset's second line, resolved target and the column-lineage
               badge live in the detail drawer, the two job counts share a cell
               and the row actions collapse into a menu.
               `whitespace-nowrap` keeps Chinese labels from stacking one
               character per line. -->
          <Table
            v-else
            class="[&_th]:whitespace-nowrap"
          >
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("openlineage.datasetName") }}</TableHead>
                <TableHead>{{ t("openlineage.datasetType") }}</TableHead>
                <TableHead>{{ t("openlineage.lastSeen") }}</TableHead>
                <TableHead>{{ t("openlineage.sourceTargetJobs") }}</TableHead>
                <TableHead>{{ t("openlineage.scope") }}</TableHead>
                <TableHead class="sticky right-0 bg-background text-right">
                  {{ t("openlineageSettings.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <!-- The whole row opens the dataset detail drawer. Only the name
                   used to be clickable, and the drawer was also reachable by
                   opening the action menu and picking an item out of it. -->
              <TableRow
                v-for="dataset in datasets"
                :key="datasetRowKey(dataset)"
                class="cursor-pointer"
                @click="handleRowClick(dataset)"
              >
                <TableCell>
                  <button
                    class="block max-w-64 truncate text-left font-medium text-primary hover:underline"
                    type="button"
                    :title="dataset.name"
                    @click.stop="openDatasetDetail(dataset)"
                  >
                    {{ dataset.name }}
                  </button>
                  <div
                    class="max-w-64 truncate font-mono text-xs text-muted-foreground"
                    :title="dataset.namespace"
                  >
                    {{ dataset.namespace }}
                  </div>
                </TableCell>
                <TableCell>{{ dataset.datasetType || "-" }}</TableCell>
                <TableCell class="whitespace-nowrap">{{ formatTimestamp(dataset.lastSeen) }}</TableCell>
                <TableCell
                  class="whitespace-nowrap"
                  :title="`${t('openlineage.sourceJobsCount')} ${dataset.sourceJobCount} · ${t('openlineage.targetJobsCount')} ${dataset.targetJobCount}`"
                >
                  {{ dataset.sourceJobCount }} / {{ dataset.targetJobCount }}
                </TableCell>
                <TableCell>
                  <Badge :variant="dataset.internal ? 'default' : 'outline'">
                    {{ dataset.internal ? t("openlineage.internal") : t("openlineage.external") }}
                  </Badge>
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
                      <DropdownMenuItem @select="openGraph(dataset)">
                        <Network class="mr-2 h-4 w-4" />
                        {{ t("openlineage.openGraph") }}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        :disabled="!dataset.supportsColumnLineage"
                        @select="openColumnLineage(dataset)"
                      >
                        <GitBranch class="mr-2 h-4 w-4" />
                        {{ t("openlineage.openColumnLineage") }}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        :disabled="!dataset.internal"
                        @select="openMetadata(dataset)"
                      >
                        <Table2 class="mr-2 h-4 w-4" />
                        {{ t("openlineage.openMetadata") }}
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

    <OpenLineageDatasetDetailDrawer
      v-model="isDetailDrawerOpen"
      :dataset="selectedDataset"
    />
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  Database,
  GitBranch,
  MoreHorizontal,
  Network,
  Table2,
} from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import {
  listOpenLineageDatasets,
  listOpenLineageFilterOptions,
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
import OpenLineageDatasetDetailDrawer from "@/components/openlineage/OpenLineageDatasetDetailDrawer.vue";
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
import {
  type ListOpenLineageFilterOptionsResponse,
  type OpenLineageDatasetResource,
  OpenLineageDatasetScope,
  type OpenLineageFilterOption,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();

const pageSize = ref(50);
const activeFilters = ref<ActiveFilter[]>([]);
const columnLineageOnly = ref(route.query.columnLineageOnly === "true");
const selectedDataset = ref<OpenLineageDatasetResource | null>(null);
const isDetailDrawerOpen = ref(false);
const facets = ref<ListOpenLineageFilterOptionsResponse | null>(null);

function filterValue(type: string): string {
  return (
    activeFilters.value.find((filter) => filter.type === type)?.value ?? ""
  );
}

function scopeValue(): OpenLineageDatasetScope {
  switch (filterValue("scope")) {
    case "internal":
      return OpenLineageDatasetScope.OPENLINEAGE_DATASET_SCOPE_INTERNAL;
    case "external":
      return OpenLineageDatasetScope.OPENLINEAGE_DATASET_SCOPE_EXTERNAL;
    default:
      return OpenLineageDatasetScope.OPENLINEAGE_DATASET_SCOPE_ALL;
  }
}

// The server owns the dataset aggregate, so the filter menus ask it for the
// values instead of reading them off the page of rows on screen.
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
      asFilterOptions((await loadFacets())?.datasetNamespaces ?? []),
  },
  {
    type: "integration",
    label: t("openlineageSettings.integration"),
    icon: "🔌",
    options: async () =>
      asFilterOptions((await loadFacets())?.integrations ?? []),
  },
  {
    type: "source",
    label: t("openlineageSettings.sourceLabel"),
    icon: "📡",
    options: async () => asFilterOptions((await loadFacets())?.sources ?? []),
  },
  {
    type: "scope",
    label: t("openlineage.datasetScope"),
    icon: "🏷️",
    options: [
      { value: "internal", label: t("openlineage.internalOnly") },
      { value: "external", label: t("openlineage.externalOnly") },
    ],
  },
]);

const {
  items: datasets,
  isLoading,
  hasNext,
  hasPrevious,
  reset,
  goNext,
  goPrevious,
} = usePagedFetch<OpenLineageDatasetResource>({
  fetchPage: async (pageToken, signal) => {
    const response = await listOpenLineageDatasets({
      pageSize: pageSize.value,
      pageToken,
      search: filterValue("name"),
      namespace: filterValue("namespace"),
      integration: filterValue("integration"),
      source: filterValue("source"),
      datasetScope: scopeValue(),
      columnLineageOnly: columnLineageOnly.value,
      signal,
    });
    return { items: response.datasets, nextPageToken: response.nextPageToken };
  },
  onError: handleError,
});

// These cards describe the rows on screen. The dataset aggregate is assembled on
// the server from recent runs, so an exact registry-wide count would mean
// resolving every dataset again; until there is an endpoint for it, the labels
// say which page they count.
const internalDatasetCount = computed(
  () => datasets.value.filter((dataset) => dataset.internal).length
);
const columnLineageDatasetCount = computed(
  () => datasets.value.filter((dataset) => dataset.supportsColumnLineage).length
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
  const integrationFilter = filters.find(
    (filter) => filter.type === "integration"
  );
  if (integrationFilter?.value) {
    nextQuery.integration = integrationFilter.value;
  }
  const sourceFilter = filters.find((filter) => filter.type === "source");
  if (sourceFilter?.value) {
    nextQuery.source = sourceFilter.value;
  }
  const scopeFilter = filters.find((filter) => filter.type === "scope");
  if (scopeFilter?.value) {
    nextQuery.scope = scopeFilter.value;
  }

  router.replace({ query: nextQuery });
  void reset();
}

function resetFilters() {
  activeFilters.value = [];
  const reloadHere = !columnLineageOnly.value;
  columnLineageOnly.value = false;
  router.replace({ query: {} });
  if (reloadHere) {
    void reset();
  }
}

// A filter, the column-lineage toggle and the page size each restart the walk at
// its first page: a cursor belongs to the query that produced it.
watch([columnLineageOnly, pageSize], () => {
  void reset();
});

function formatTimestamp(ts: Timestamp | undefined): string {
  return formatDateTime(ts, locale.value);
}

function openGraph(dataset: OpenLineageDatasetResource) {
  router.push({
    name: "LineageGraph",
    params: { guid: guidToRouteParams(dataset.guid) },
    query: {
      metaType: String(dataset.resolvedMetaType),
      from: route.fullPath,
    },
  });
}

function openDatasetDetail(dataset: OpenLineageDatasetResource) {
  selectedDataset.value = dataset;
  isDetailDrawerOpen.value = true;
}

/**
 * A click anywhere in the row opens the detail drawer. A click that ends a text
 * selection is someone copying an identifier out of the table, not asking to
 * open anything, so it is left alone.
 */
function handleRowClick(dataset: OpenLineageDatasetResource) {
  if (window.getSelection()?.toString()) {
    return;
  }
  openDatasetDetail(dataset);
}

function openMetadata(dataset: OpenLineageDatasetResource) {
  if (!dataset.internal) {
    return;
  }

  router.push({
    name: "MetadataDetail",
    params: { guid: guidToRouteParams(dataset.guid) },
    query: {
      metaType: String(dataset.resolvedMetaType),
      from: route.fullPath,
    },
  });
}

function openColumnLineage(dataset: OpenLineageDatasetResource) {
  if (!dataset.supportsColumnLineage) {
    return;
  }

  router.push({
    name: "OpenLineageColumnLineage",
    params: { guid: guidToRouteParams(dataset.guid) },
    query: {
      metaType: String(dataset.resolvedMetaType),
      from: route.fullPath,
    },
  });
}

function datasetRowKey(dataset: OpenLineageDatasetResource): string {
  return `${dataset.namespace}\u0000${dataset.name}`;
}

onMounted(() => {
  void reset();
  void loadFacets();
});
</script>