<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.datasets')"
      :description="t('openlineage.datasetsDescription')"
    />

    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <StatCard
        :label="t('openlineage.visibleDatasets')"
        :value="filteredDatasets.length"
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
            v-if="filteredDatasets.length === 0"
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
              <TableRow v-for="dataset in filteredDatasets" :key="datasetRowKey(dataset)">
                <TableCell>
                  <button
                    class="block max-w-64 truncate text-left font-medium text-primary hover:underline"
                    type="button"
                    :title="dataset.name"
                    @click="openDatasetDetail(dataset)"
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
                <TableCell class="sticky right-0 bg-background text-right">
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
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem @select="openDatasetDetail(dataset)">
                        <Info class="mr-2 h-4 w-4" />
                        {{ t("openlineageSettings.viewDetail") }}
                      </DropdownMenuItem>
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
  Info,
  MoreHorizontal,
  Network,
  Table2,
} from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listOpenLineageDatasets } from "@/api/openlineage";
import type { ActiveFilter } from "@/components/common/AdvancedSearchBar.vue";
import AdvancedSearchBar from "@/components/common/AdvancedSearchBar.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import StatCard from "@/components/common/StatCard.vue";
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
import type { OpenLineageDatasetResource } from "@/types/proto-es/v1/openlineage_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();

const isLoading = ref(false);
const datasets = ref<OpenLineageDatasetResource[]>([]);
const selectedDataset = ref<OpenLineageDatasetResource | null>(null);
const isDetailDrawerOpen = ref(false);
const activeFilters = ref<ActiveFilter[]>([]);
const columnLineageOnly = ref(route.query.columnLineageOnly === "true");

const namespaces = computed(() => {
  return Array.from(
    new Set(datasets.value.map((dataset) => dataset.namespace).filter(Boolean))
  ).sort((left, right) => left.localeCompare(right));
});

const integrations = computed(() => {
  return Array.from(
    new Set(
      datasets.value.flatMap((dataset) => dataset.integrations).filter(Boolean)
    )
  ).sort((left, right) => left.localeCompare(right));
});

const sources = computed(() => {
  return Array.from(
    new Set(
      datasets.value.flatMap((dataset) => dataset.sources).filter(Boolean)
    )
  ).sort((left, right) => left.localeCompare(right));
});

const filterCategories = computed(() => {
  return [
    {
      type: "namespace",
      label: t("openlineageSettings.namespace"),
      icon: "📦",
      options: namespaces.value.map((ns) => ({ value: ns, label: ns })),
    },
    {
      type: "integration",
      label: t("openlineageSettings.integration"),
      icon: "🔌",
      options: integrations.value.map((i) => ({ value: i, label: i })),
    },
    {
      type: "source",
      label: t("openlineageSettings.sourceLabel"),
      icon: "📡",
      options: sources.value.map((s) => ({ value: s, label: s })),
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
  ].filter((cat) => cat.options.length > 0);
});

function handleFiltersUpdate(filters: ActiveFilter[]) {
  activeFilters.value = filters;
  const nextQuery: Record<string, string> = {};

  const nameFilter = filters.find((f) => f.type === "name");
  if (nameFilter?.value) {
    nextQuery.search = nameFilter.value;
  }
  const nsFilter = filters.find((f) => f.type === "namespace");
  if (nsFilter?.value) {
    nextQuery.namespace = nsFilter.value;
  }
  const intFilter = filters.find((f) => f.type === "integration");
  if (intFilter?.value) {
    nextQuery.integration = intFilter.value;
  }
  const srcFilter = filters.find((f) => f.type === "source");
  if (srcFilter?.value) {
    nextQuery.source = srcFilter.value;
  }
  const scopeFilter = filters.find((f) => f.type === "scope");
  if (scopeFilter?.value) {
    nextQuery.scope = scopeFilter.value;
  }

  router.replace({ query: nextQuery });
}

const filteredDatasets = computed(() => {
  const nameFilter =
    activeFilters.value.find((f) => f.type === "name")?.value ?? "";
  const nsFilter =
    activeFilters.value.find((f) => f.type === "namespace")?.value ?? "";
  const intFilter =
    activeFilters.value.find((f) => f.type === "integration")?.value ?? "";
  const srcFilter =
    activeFilters.value.find((f) => f.type === "source")?.value ?? "";
  const scopeFilter =
    activeFilters.value.find((f) => f.type === "scope")?.value ?? "";

  const query = nameFilter.toLowerCase();

  return datasets.value.filter((dataset) => {
    if (nsFilter && dataset.namespace !== nsFilter) {
      return false;
    }
    if (intFilter && !dataset.integrations.includes(intFilter)) {
      return false;
    }
    if (srcFilter && !dataset.sources.includes(srcFilter)) {
      return false;
    }
    if (scopeFilter === "internal" && !dataset.internal) {
      return false;
    }
    if (scopeFilter === "external" && dataset.internal) {
      return false;
    }
    if (columnLineageOnly.value && !dataset.supportsColumnLineage) {
      return false;
    }
    if (!query) {
      return true;
    }
    const haystack = [
      dataset.name,
      dataset.namespace,
      dataset.datasetType,
      dataset.resolvedTarget,
      dataset.integrations.join(" "),
      dataset.sources.join(" "),
    ]
      .join(" ")
      .toLowerCase();
    return haystack.includes(query);
  });
});

const internalDatasetCount = computed(() => {
  return filteredDatasets.value.filter((dataset) => dataset.internal).length;
});

const columnLineageDatasetCount = computed(() => {
  return filteredDatasets.value.filter(
    (dataset) => dataset.supportsColumnLineage
  ).length;
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

function resetFilters() {
  activeFilters.value = [];
  columnLineageOnly.value = false;
  router.replace({ query: {} });
}

function datasetRowKey(dataset: OpenLineageDatasetResource): string {
  return `${dataset.namespace}\u0000${dataset.name}`;
}

watch([columnLineageOnly], () => {
  const nextQuery = { ...route.query };
  if (columnLineageOnly.value) {
    nextQuery.columnLineageOnly = "true";
  } else {
    delete nextQuery.columnLineageOnly;
  }
  router.replace({ query: nextQuery });
});

async function fetchDatasets() {
  isLoading.value = true;
  try {
    const response = await listOpenLineageDatasets({ pageSize: 500 });
    datasets.value = response.datasets;
  } catch (error) {
    handleError(error);
  } finally {
    isLoading.value = false;
  }
}

onMounted(() => {
  fetchDatasets();
});
</script>