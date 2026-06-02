<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.datasets')"
      :description="t('openlineage.datasetsDescription')"
    />

    <div class="grid gap-3 md:grid-cols-3">
      <Card>
        <CardContent class="flex items-center justify-between px-3 py-2">
          <span class="text-sm text-muted-foreground">{{ t("openlineage.visibleDatasets") }}</span>
          <span class="text-xl font-semibold">{{ filteredDatasets.length }}</span>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="flex items-center justify-between px-3 py-2">
          <span class="text-sm text-muted-foreground">{{ t("openlineage.internalDatasets") }}</span>
          <span class="text-xl font-semibold">{{ internalDatasetCount }}</span>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="flex items-center justify-between px-3 py-2">
          <span class="text-sm text-muted-foreground">{{ t("openlineage.columnLineageDatasets") }}</span>
          <span class="text-xl font-semibold">{{ columnLineageDatasetCount }}</span>
        </CardContent>
      </Card>
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
        <div v-if="isLoading" class="p-8 flex justify-center">
          <AppLoading />
        </div>
        <div
          v-else-if="filteredDatasets.length === 0"
          class="p-8 text-center text-muted-foreground"
        >
          <Database class="mx-auto mb-4 h-12 w-12 text-muted-foreground/50" />
          <p>{{ t("openlineage.noDatasets") }}</p>
        </div>
        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("openlineageSettings.namespace") }}</TableHead>
              <TableHead>{{ t("openlineage.datasetName") }}</TableHead>
              <TableHead>{{ t("openlineage.datasetType") }}</TableHead>
              <TableHead>{{ t("openlineage.resolvedTarget") }}</TableHead>
              <TableHead>{{ t("openlineage.lastSeen") }}</TableHead>
              <TableHead>{{ t("openlineage.sourceJobsCount") }}</TableHead>
              <TableHead>{{ t("openlineage.targetJobsCount") }}</TableHead>
              <TableHead>{{ t("openlineage.supportsColumnLineage") }}</TableHead>
              <TableHead>{{ t("openlineage.scope") }}</TableHead>
              <TableHead class="text-right">{{ t("openlineageSettings.actions") }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="dataset in filteredDatasets" :key="datasetRowKey(dataset)">
              <TableCell class="font-mono text-sm">{{ dataset.namespace }}</TableCell>
              <TableCell>
                <button
                  class="text-left font-medium text-primary hover:underline"
                  type="button"
                  @click="openDatasetDetail(dataset)"
                >
                  {{ dataset.name }}
                </button>
              </TableCell>
              <TableCell>{{ dataset.datasetType || "-" }}</TableCell>
              <TableCell>
                <span v-if="dataset.resolvedTarget" class="font-mono text-sm">
                  {{ dataset.resolvedTarget }}
                </span>
                <span v-else class="text-muted-foreground">-</span>
              </TableCell>
              <TableCell>{{ formatTimestamp(dataset.lastSeen) }}</TableCell>
              <TableCell>{{ dataset.sourceJobCount }}</TableCell>
              <TableCell>{{ dataset.targetJobCount }}</TableCell>
              <TableCell>
                <Badge :variant="dataset.supportsColumnLineage ? 'success' : 'secondary'">
                  {{ dataset.supportsColumnLineage ? t("openlineage.yes") : t("openlineage.no") }}
                </Badge>
              </TableCell>
              <TableCell>
                <Badge :variant="dataset.internal ? 'default' : 'outline'">
                  {{ dataset.internal ? t("openlineage.internal") : t("openlineage.external") }}
                </Badge>
              </TableCell>
              <TableCell class="text-right">
                <div class="flex justify-end gap-2">
                  <Button variant="ghost" size="sm" @click="openDatasetDetail(dataset)">
                    {{ t("openlineageSettings.viewDetail") }}
                  </Button>
                  <Button variant="ghost" size="sm" @click="openGraph(dataset)">
                    {{ t("openlineage.openGraph") }}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    :disabled="!dataset.supportsColumnLineage"
                    @click="openColumnLineage(dataset)"
                  >
                    {{ t("openlineage.openColumnLineage") }}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    :disabled="!dataset.internal"
                    @click="openMetadata(dataset)"
                  >
                    {{ t("openlineage.openMetadata") }}
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
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
import { Database } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listOpenLineageDatasets } from "@/api/openlineage";
import type { ActiveFilter } from "@/components/common/AdvancedSearchBar.vue";
import AdvancedSearchBar from "@/components/common/AdvancedSearchBar.vue";
import AppLoading from "@/components/common/AppLoading.vue";
import OpenLineageDatasetDetailDrawer from "@/components/openlineage/OpenLineageDatasetDetailDrawer.vue";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
import { toGuidPath } from "@/lib/openlineage";
import type { OpenLineageDatasetResource } from "@/types/proto-es/v1/openlineage_service_pb";

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
  if (!ts?.seconds) return "-";
  const date = new Date(Number(ts.seconds) * 1000);
  return new Intl.DateTimeFormat(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function openGraph(dataset: OpenLineageDatasetResource) {
  router.push({
    name: "LineageGraph",
    params: { guid: dataset.guid },
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
    params: { guid: toGuidPath(dataset.guid) },
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
    params: { guid: toGuidPath(dataset.guid) },
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