<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.datasets')"
      :description="t('openlineage.datasetsDescription')"
    />

    <div class="grid gap-4 md:grid-cols-3">
      <Card>
        <CardContent class="p-5">
          <div class="text-sm text-muted-foreground">{{ t("openlineage.visibleDatasets") }}</div>
          <div class="mt-2 text-2xl font-semibold">{{ filteredDatasets.length }}</div>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="p-5">
          <div class="text-sm text-muted-foreground">{{ t("openlineage.internalDatasets") }}</div>
          <div class="mt-2 text-2xl font-semibold">{{ internalDatasetCount }}</div>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="p-5">
          <div class="text-sm text-muted-foreground">{{ t("openlineage.columnLineageDatasets") }}</div>
          <div class="mt-2 text-2xl font-semibold">{{ columnLineageDatasetCount }}</div>
        </CardContent>
      </Card>
    </div>

    <Card>
      <CardContent class="flex flex-col gap-4 pt-6 xl:flex-row xl:items-end">
        <div class="flex-1 space-y-2">
          <Label for="openlineage-dataset-search">{{ t("openlineage.search") }}</Label>
          <Input
            id="openlineage-dataset-search"
            v-model="searchTerm"
            :placeholder="t('openlineage.searchDatasetsPlaceholder')"
          />
        </div>

        <div class="space-y-2 xl:w-64">
          <Label>{{ t("openlineageSettings.namespace") }}</Label>
          <Select v-model="selectedNamespace">
            <SelectTrigger>
              <SelectValue :placeholder="t('openlineage.namespaceAll')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{{ t("openlineage.namespaceAll") }}</SelectItem>
              <SelectItem v-for="namespace in namespaces" :key="namespace" :value="namespace">
                {{ namespace }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="space-y-2 xl:w-56">
          <Label>{{ t("openlineageSettings.integration") }}</Label>
          <Select v-model="selectedIntegration">
            <SelectTrigger>
              <SelectValue :placeholder="t('openlineage.integrationAll')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{{ t("openlineage.integrationAll") }}</SelectItem>
              <SelectItem
                v-for="integration in integrations"
                :key="integration"
                :value="integration"
              >
                {{ integration }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="space-y-2 xl:w-56">
          <Label>{{ t("openlineageSettings.sourceLabel") }}</Label>
          <Select v-model="selectedSource">
            <SelectTrigger>
              <SelectValue :placeholder="t('openlineage.sourceAll')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{{ t("openlineage.sourceAll") }}</SelectItem>
              <SelectItem v-for="source in sources" :key="source" :value="source">
                {{ source }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="space-y-2 xl:w-56">
          <Label>{{ t("openlineage.datasetScope") }}</Label>
          <Select v-model="datasetScope">
            <SelectTrigger>
              <SelectValue :placeholder="t('openlineage.datasetScopeAll')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{{ t("openlineage.datasetScopeAll") }}</SelectItem>
              <SelectItem value="internal">{{ t("openlineage.internalOnly") }}</SelectItem>
              <SelectItem value="external">{{ t("openlineage.externalOnly") }}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex items-center gap-2 xl:pb-2">
          <Checkbox
            id="datasets-column-lineage-only"
            :checked="columnLineageOnly"
            @update:checked="columnLineageOnly = $event === true"
          />
          <Label for="datasets-column-lineage-only" class="cursor-pointer text-sm">
            {{ t("openlineage.onlyColumnLineage") }}
          </Label>
        </div>

        <Button variant="outline" @click="resetFilters">
          {{ t("openlineage.clearFilters") }}
        </Button>
      </CardContent>
    </Card>

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
import AppLoading from "@/components/common/AppLoading.vue";
import OpenLineageDatasetDetailDrawer from "@/components/openlineage/OpenLineageDatasetDetailDrawer.vue";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
const searchTerm = ref(
  typeof route.query.search === "string" ? route.query.search : ""
);
const selectedNamespace = ref(
  typeof route.query.namespace === "string" ? route.query.namespace : "all"
);
const selectedIntegration = ref(
  typeof route.query.integration === "string" ? route.query.integration : "all"
);
const selectedSource = ref(
  typeof route.query.source === "string" ? route.query.source : "all"
);
const datasetScope = ref(
  typeof route.query.scope === "string" ? route.query.scope : "all"
);
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

const filteredDatasets = computed(() => {
  const query = searchTerm.value.trim().toLowerCase();

  return datasets.value.filter((dataset) => {
    if (
      selectedNamespace.value !== "all" &&
      dataset.namespace !== selectedNamespace.value
    ) {
      return false;
    }

    if (
      selectedIntegration.value !== "all" &&
      !dataset.integrations.includes(selectedIntegration.value)
    ) {
      return false;
    }

    if (
      selectedSource.value !== "all" &&
      !dataset.sources.includes(selectedSource.value)
    ) {
      return false;
    }

    if (datasetScope.value === "internal" && !dataset.internal) {
      return false;
    }

    if (datasetScope.value === "external" && dataset.internal) {
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
  searchTerm.value = "";
  selectedNamespace.value = "all";
  selectedIntegration.value = "all";
  selectedSource.value = "all";
  datasetScope.value = "all";
  columnLineageOnly.value = false;
}

function datasetRowKey(dataset: OpenLineageDatasetResource): string {
  return `${dataset.namespace}\u0000${dataset.name}`;
}

watch(
  [
    searchTerm,
    selectedNamespace,
    selectedIntegration,
    selectedSource,
    datasetScope,
    columnLineageOnly,
  ],
  () => {
    const nextQuery: Record<string, string> = {};

    if (searchTerm.value.trim()) {
      nextQuery.search = searchTerm.value.trim();
    }
    if (selectedNamespace.value !== "all") {
      nextQuery.namespace = selectedNamespace.value;
    }
    if (selectedIntegration.value !== "all") {
      nextQuery.integration = selectedIntegration.value;
    }
    if (selectedSource.value !== "all") {
      nextQuery.source = selectedSource.value;
    }
    if (datasetScope.value !== "all") {
      nextQuery.scope = datasetScope.value;
    }
    if (columnLineageOnly.value) {
      nextQuery.columnLineageOnly = "true";
    }

    router.replace({ query: nextQuery });
  }
);

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