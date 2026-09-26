<template>
  <div class="space-y-4">
    <PageHeader :title="t('lineageGraph.title')">
      <template #title-extra>
        <Badge
          variant="outline"
          class="max-w-64 truncate"
        >
          {{ focusAssetLabel }}
        </Badge>
      </template>
      <template #actions>
        <Select v-model="expandDepth">
          <SelectTrigger class="w-28">
            <SelectValue :placeholder="t('lineageGraph.expandDepth')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="1">{{ t("lineageGraph.depth1") }}</SelectItem>
            <SelectItem value="2">{{ t("lineageGraph.depth2") }}</SelectItem>
            <SelectItem value="3">{{ t("lineageGraph.depth3") }}</SelectItem>
          </SelectContent>
        </Select>
        <Button v-if="hasExpandedBeyondRoot" variant="outline" size="sm" @click="handleReset">
          <RotateCcw class="size-4 mr-1" />
          {{ t("lineageGraph.reset") }}
        </Button>
        <Button variant="outline" size="sm" @click="handleFitView">
          <Maximize2 class="size-4 mr-1" />
          {{ t("lineageGraph.fitView") }}
        </Button>
        <Button variant="outline" size="sm" @click="handleBackToMetadata">
          <ArrowLeft class="size-4 mr-1" />
          {{ t("lineageGraph.backToMetadata") }}
        </Button>
      </template>
    </PageHeader>

    <Card v-if="openLineageSources.length > 0">
      <CardContent class="pt-6 space-y-3">
        <div>
          <h2 class="text-sm font-semibold tracking-tight">
            {{ t("lineageGraph.openlineageSources") }}
          </h2>
          <p class="text-sm text-muted-foreground">
            {{
              selectedColumnGuid && selectedColumnName
                ? t("lineageGraph.openlineageSourcesFiltered")
                : t("lineageGraph.openlineageSourcesDescription")
            }}
          </p>
        </div>
        <div class="flex flex-wrap gap-2">
          <Button
            v-for="source in openLineageSources"
            :key="source.guid"
            variant="outline"
            size="sm"
            @click="openOpenLineageRun(source.guid)"
          >
            {{ source.label }}
          </Button>
        </div>
      </CardContent>
    </Card>

    <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
      <Card class="relative overflow-hidden" style="height: calc(100vh - 12rem)">
        <div v-if="initialLoading" class="absolute inset-0 z-10 flex items-center justify-center bg-background/80">
          <AppLoading />
        </div>

        <VueFlow
          :nodes="nodes"
          :edges="edges"
          :default-viewport="{ x: 0, y: 0, zoom: 0.8 }"
          :min-zoom="0.1"
          :max-zoom="2"
          fit-view-on-init
        >
          <Background />
          <Controls />
          <MiniMap />

          <template #node-lineage="nodeProps">
            <LineageNode
              :data="nodeProps.data"
              @expand="handleExpandNode"
              @select-node="handleSelectNode"
              @select-column="handleSelectColumn"
              @toggle-fields="handleToggleFields"
            />
          </template>
        </VueFlow>
      </Card>

      <Card v-if="selectedNodeSummary" class="overflow-hidden xl:h-[calc(100vh-12rem)]">
        <CardContent class="flex h-full flex-col p-0">
          <div class="flex items-start justify-between gap-3 border-b px-5 py-4">
            <div class="space-y-1">
              <div class="text-xs uppercase tracking-wide text-muted-foreground">
                {{ t("common.details") }}
              </div>
              <h2 class="text-lg font-semibold leading-tight">
                {{ selectedNodeSummary.label }}
              </h2>
              <p class="break-all text-xs text-muted-foreground">
                {{ selectedNodeSummary.guid }}
              </p>
            </div>
            <Button variant="ghost" size="sm" @click="closeSelectedNode">
              {{ t("lineageGraph.closeDetail") }}
            </Button>
          </div>

          <div class="flex-1 space-y-6 overflow-y-auto px-5 py-4">
            <section class="space-y-3">
              <div class="flex flex-wrap gap-2">
                <Badge :variant="selectedNodeSummary.isExternal ? 'outline' : 'default'">
                  {{ selectedNodeSummary.isExternal ? t("openlineage.external") : t("openlineage.internal") }}
                </Badge>
                <Badge variant="outline">
                  {{ selectedNodeSummary.metaTypeLabel }}
                </Badge>
                <Badge v-if="selectedNodeSummary.isRoot" variant="secondary">
                  {{ t("lineageGraph.rootNode") }}
                </Badge>
              </div>

              <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("lineageGraph.shortPath") }}</div>
                  <div class="mt-1 break-all text-sm font-medium">{{ selectedNodeSummary.shortPath }}</div>
                </div>
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("lineageGraph.fieldCount") }}</div>
                  <div class="mt-1 text-sm font-medium">{{ selectedNodeSummary.columns.length }}</div>
                </div>
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("lineageGraph.upstreamObjects") }}</div>
                  <div class="mt-1 text-sm font-medium">{{ selectedNodeSummary.upstreamCount }}</div>
                </div>
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("lineageGraph.downstreamObjects") }}</div>
                  <div class="mt-1 text-sm font-medium">{{ selectedNodeSummary.downstreamCount }}</div>
                </div>
                <div v-if="selectedNodeSummary.isExternal" class="rounded-md border p-3 sm:col-span-2 xl:col-span-1">
                  <div class="text-xs text-muted-foreground">{{ t("openlineageSettings.namespace") }}</div>
                  <div class="mt-1 break-all text-sm font-medium">{{ selectedNodeSummary.externalNamespace || "-" }}</div>
                </div>
                <div v-if="selectedNodeSummary.isExternal" class="rounded-md border p-3 sm:col-span-2 xl:col-span-1">
                  <div class="text-xs text-muted-foreground">{{ t("openlineage.datasetType") }}</div>
                  <div class="mt-1 text-sm font-medium">{{ selectedNodeSummary.externalDatasetType || "-" }}</div>
                </div>
                <div v-if="selectedColumnContext" class="rounded-md border p-3 sm:col-span-2 xl:col-span-1">
                  <div class="text-xs text-muted-foreground">{{ t("lineageGraph.selectedField") }}</div>
                  <div class="mt-1 text-sm font-medium">{{ selectedColumnContext }}</div>
                </div>
              </div>

              <div class="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" @click="refocusOnSelectedNode">
                  {{ t("lineageGraph.refocusGraph") }}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  :disabled="!canOpenSelectedNodeColumnLineage"
                  @click="openSelectedNodeColumnLineage"
                >
                  {{ t("lineageGraph.openColumnLineage") }}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  :disabled="selectedNodeSummary.isExternal"
                  @click="openSelectedNodeMetadata"
                >
                  {{ t("openlineage.openMetadata") }}
                </Button>
              </div>
            </section>

            <section class="space-y-3">
              <div>
                <h3 class="text-sm font-semibold">{{ t("lineageGraph.relatedRuns") }}</h3>
                <p class="text-sm text-muted-foreground">
                  {{
                    selectedColumnContext
                      ? t("lineageGraph.relatedRunsFiltered")
                      : t("lineageGraph.relatedRunsDescription")
                  }}
                </p>
              </div>

              <div v-if="selectedNodeRelatedRuns.length === 0" class="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
                {{ t("lineageGraph.noRelatedRuns") }}
              </div>

              <div v-else class="space-y-2">
                <Button
                  v-for="run in selectedNodeRelatedRuns"
                  :key="run.guid"
                  variant="outline"
                  class="h-auto w-full justify-start px-3 py-3 text-left"
                  @click="openOpenLineageRun(run.guid)"
                >
                  <div class="space-y-1">
                    <div class="font-medium">{{ run.label }}</div>
                    <div class="text-xs text-muted-foreground">
                      {{ run.updatedAtLabel }}
                    </div>
                  </div>
                </Button>
              </div>
            </section>
          </div>
        </CardContent>
      </Card>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Background } from "@vue-flow/background";
import { Controls } from "@vue-flow/controls";
import { type Edge, type Node, useVueFlow, VueFlow } from "@vue-flow/core";
import { MiniMap } from "@vue-flow/minimap";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import "@vue-flow/core/dist/style.css";
import "@vue-flow/core/dist/theme-default.css";
import "@vue-flow/controls/dist/style.css";
import "@vue-flow/minimap/dist/style.css";
import { ArrowLeft, Maximize2, RotateCcw } from "lucide-vue-next";
import { getLineage } from "@/api/lineage";
import AppLoading from "@/components/common/AppLoading.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import type { LineageNodeData } from "@/components/lineage/LineageNode.vue";
import LineageNode from "@/components/lineage/LineageNode.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  assignLayers,
  buildLineageEdges,
  type ColumnFilter,
  layoutNodes,
  nodeHeight,
} from "@/lib/lineageGraph";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type {
  ExternalDatasetInfo,
  LineageRelation,
} from "@/types/proto-es/v1/lineage_service_pb";
import { LineageType } from "@/types/proto-es/v1/lineage_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { extractErrorMessage } from "@/utils/error";
import { guidToRouteParams, routeParamToGuid } from "@/utils/guid";
import { metaTypeLabel, parseMetaType } from "@/utils/metaType";

const EXTERNAL_PREFIX = "external:";

function isExternalGuid(guid: string): boolean {
  return guid.startsWith(EXTERNAL_PREFIX);
}

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { fitView, getNodes } = useVueFlow();

const OPENLINEAGE_META_TYPE = 100;

type LineageDirection = "upstream" | "downstream";

interface NodeLineageData {
  upstream: LineageRelation[];
  downstream: LineageRelation[];
  upstreamLoaded: boolean;
  downstreamLoaded: boolean;
}

type NodeRunSummary = {
  guid: string;
  label: string;
  updatedAt?: Timestamp;
  updatedAtLabel: string;
};

const nodes = ref<Node[]>([]);
const edges = ref<Edge[]>([]);
const initialLoading = ref(true);

const expandedGuids = ref<Set<string>>(new Set());
const nodeDataMap = ref<Map<string, NodeLineageData>>(new Map());
const expandDepth = ref("1");

const focusAssetLabel = computed(() => {
  return formatGuidLabel(currentGuid.value);
});

// Track actual MetaType per guid, derived from lineage relation sourceType/targetType
const guidMetaTypeMap = ref<Map<string, MetaType>>(new Map());

// External dataset info cache: guid -> ExternalDatasetInfo
const externalDatasetMap = ref<Map<string, ExternalDatasetInfo>>(new Map());

// Snapshot of initial state for reset
let initialExpandedGuids = new Set<string>();
let initialNodeDataMap = new Map<string, NodeLineageData>();

// Field-level column selection state
const selectedColumnGuid = ref<string | null>(null);
const selectedColumnName = ref<string | null>(null);
// guid -> Set<column> for columns that should be highlighted on neighbour nodes
const highlightedColumnsMap = ref<Map<string, Set<string>>>(new Map());

// Track which nodes have their fields panel visible (for layout height calculation)
const fieldsVisibleGuids = ref<Set<string>>(new Set());

const hasExpandedBeyondRoot = computed(() => {
  return expandedGuids.value.size > initialExpandedGuids.size;
});

const selectedNodeGuid = computed(() => {
  const node = route.query.node;
  return typeof node === "string" && node.length > 0 ? node : null;
});

const currentGuid = computed(() => routeParamToGuid(route.params.guid));

const currentMetaType = computed(
  () => parseMetaType(route.query.metaType) ?? MetaType.TABLE
);

const openLineageSources = computed(() => {
  const sources = new Map<string, { guid: string; label: string }>();

  for (const [guid, data] of nodeDataMap.value) {
    for (const rel of [...data.upstream, ...data.downstream]) {
      if (Number(rel.metaType) !== OPENLINEAGE_META_TYPE || !rel.metaGuid) {
        continue;
      }

      if (
        selectedColumnGuid.value &&
        selectedColumnName.value &&
        !relationMatchesSelectedColumn(rel)
      ) {
        continue;
      }

      if (!sources.has(rel.metaGuid)) {
        sources.set(rel.metaGuid, {
          guid: rel.metaGuid,
          label: formatOpenLineageRunLabel(rel.metaGuid),
        });
      }
    }

    if (!nodeDataMap.value.has(guid)) {
      continue;
    }
  }

  return Array.from(sources.values());
});

const selectedNodeSummary = computed(() => {
  if (!selectedNodeGuid.value) {
    return null;
  }

  const lineageData = nodeDataMap.value.get(selectedNodeGuid.value);
  if (!lineageData && selectedNodeGuid.value !== currentGuid.value) {
    return null;
  }

  const externalInfo = externalDatasetMap.value.get(selectedNodeGuid.value);
  // The type is resolved from the lineage relations (or the route), never
  // guessed from the number of GUID segments: a MySQL schema is an empty
  // segment, so the segment count does not name the type.
  const metaTypeValue =
    guidMetaTypeMap.value.get(selectedNodeGuid.value) ?? currentMetaType.value;

  return {
    guid: selectedNodeGuid.value,
    label: formatGuidLabel(selectedNodeGuid.value),
    shortPath: formatGuidShort(selectedNodeGuid.value),
    isRoot: selectedNodeGuid.value === currentGuid.value,
    isExternal: isExternalGuid(selectedNodeGuid.value),
    metaTypeValue,
    metaTypeLabel: metaTypeLabel(metaTypeValue, t),
    upstreamCount: lineageData?.upstream.length ?? 0,
    downstreamCount: lineageData?.downstream.length ?? 0,
    columns: collectColumnsForGuid(selectedNodeGuid.value),
    externalNamespace: externalInfo?.namespace ?? "",
    externalDatasetType: externalInfo?.datasetType ?? "",
  };
});

const selectedColumnContext = computed(() => {
  if (
    selectedColumnGuid.value !== selectedNodeGuid.value ||
    !selectedColumnName.value
  ) {
    return null;
  }
  return selectedColumnName.value;
});

const canOpenSelectedNodeColumnLineage = computed(() => {
  if (!selectedNodeSummary.value) {
    return false;
  }

  return (
    !selectedNodeSummary.value.isExternal &&
    selectedNodeSummary.value.columns.length > 0
  );
});

const selectedNodeRelatedRuns = computed<NodeRunSummary[]>(() => {
  if (!selectedNodeGuid.value) {
    return [];
  }

  const lineageData = getNodeLineageData(selectedNodeGuid.value);
  const runs = new Map<string, NodeRunSummary>();
  const addRelation = (relation: LineageRelation) => {
    if (
      Number(relation.metaType) !== OPENLINEAGE_META_TYPE ||
      !relation.metaGuid
    ) {
      return;
    }

    if (
      selectedColumnContext.value &&
      !relationMatchesSelectedColumn(relation)
    ) {
      return;
    }

    const existing = runs.get(relation.metaGuid);
    if (
      !existing ||
      compareTimestamps(relation.updatedAt, existing.updatedAt) < 0
    ) {
      runs.set(relation.metaGuid, {
        guid: relation.metaGuid,
        label: formatOpenLineageRunLabel(relation.metaGuid),
        updatedAt: relation.updatedAt,
        updatedAtLabel: formatTimestamp(relation.updatedAt),
      });
    }
  };

  for (const relation of lineageData.upstream) {
    addRelation(relation);
  }
  for (const relation of lineageData.downstream) {
    addRelation(relation);
  }

  return Array.from(runs.values()).sort((left, right) =>
    compareTimestamps(right.updatedAt, left.updatedAt)
  );
});

function formatGuidShort(guid: string): string {
  if (!guid) return "";
  if (isExternalGuid(guid)) {
    const ext = externalDatasetMap.value.get(guid);
    if (ext) return `${ext.namespace} / ${ext.name}`;
    return guid.substring(EXTERNAL_PREFIX.length);
  }
  const segments = guid.split(";").filter(Boolean);
  if (segments.length === 0) return guid;
  return segments.slice(-3).join(".");
}

function formatGuidLabel(guid: string): string {
  if (!guid) return "";
  if (isExternalGuid(guid)) {
    const ext = externalDatasetMap.value.get(guid);
    if (ext) {
      const nameParts = ext.name.split(".");
      return nameParts[nameParts.length - 1] || ext.name;
    }
    const parts = guid.substring(EXTERNAL_PREFIX.length).split(":");
    return parts[parts.length - 1] || guid;
  }
  const segments = guid.split(";").filter(Boolean);
  return segments[segments.length - 1] || guid;
}

function guidToMetaType(guid: string): string {
  if (isExternalGuid(guid)) return "external";
  const segments = guid.split(";").filter(Boolean);
  if (segments.length <= 1) return "instance";
  if (segments.length === 2) return "database";
  if (segments.length === 3) return "schema";
  return "table";
}

const columnFilter = computed<ColumnFilter | null>(() =>
  selectedColumnGuid.value && selectedColumnName.value
    ? { guid: selectedColumnGuid.value, column: selectedColumnName.value }
    : null
);

function relationMatchesSelectedColumn(rel: LineageRelation): boolean {
  if (!selectedColumnGuid.value || !selectedColumnName.value) {
    return true;
  }
  return (
    (rel.sourceGuid === selectedColumnGuid.value &&
      rel.sourceColumn === selectedColumnName.value) ||
    (rel.targetGuid === selectedColumnGuid.value &&
      rel.targetColumn === selectedColumnName.value)
  );
}

function formatOpenLineageRunLabel(guid: string): string {
  const prefix = "openlineage:run:";
  if (!guid.startsWith(prefix)) {
    return guid;
  }

  const segments = guid
    .substring(prefix.length)
    .split(":")
    .map((segment) => decodeURIComponent(segment));

  if (segments.length >= 3) {
    const runID = segments[segments.length - 1];
    const jobName = segments[segments.length - 2];
    return `${jobName} · ${runID}`;
  }

  return segments.join(" · ") || guid;
}

function formatTimestamp(ts: Timestamp | undefined): string {
  return formatDateTime(ts, locale.value);
}

function compareTimestamps(
  left: Timestamp | undefined,
  right: Timestamp | undefined
): number {
  const leftSeconds = Number(left?.seconds ?? 0);
  const rightSeconds = Number(right?.seconds ?? 0);
  if (leftSeconds === rightSeconds) {
    return 0;
  }
  return leftSeconds > rightSeconds ? 1 : -1;
}

function setSelectedNode(guid: string | null) {
  const nextQuery = { ...route.query };
  if (guid) {
    nextQuery.node = guid;
  } else {
    delete nextQuery.node;
  }
  router.replace({ query: nextQuery });
}

function openOpenLineageRun(guid: string) {
  router.push({
    name: "OpenLineageRunDetail",
    params: { guid },
    query: { from: route.fullPath },
  });
}

function openSelectedNodeMetadata() {
  if (!selectedNodeSummary.value || selectedNodeSummary.value.isExternal) {
    return;
  }

  router.push({
    name: "MetadataDetail",
    params: { guid: guidToRouteParams(selectedNodeSummary.value.guid) },
    query: {
      metaType: String(selectedNodeSummary.value.metaTypeValue),
      from: route.fullPath,
    },
  });
}

function refocusOnSelectedNode() {
  if (!selectedNodeSummary.value) {
    return;
  }

  const nextQuery: Record<string, string> = {
    metaType: String(selectedNodeSummary.value.metaTypeValue),
  };
  if (typeof route.query.from === "string" && route.query.from.length > 0) {
    nextQuery.from = route.query.from;
  }

  router.push({
    name: "LineageGraph",
    params: { guid: guidToRouteParams(selectedNodeSummary.value.guid) },
    query: nextQuery,
  });
}

function openSelectedNodeColumnLineage() {
  if (!selectedNodeSummary.value || !canOpenSelectedNodeColumnLineage.value) {
    return;
  }

  const query: Record<string, string> = {
    metaType: String(selectedNodeSummary.value.metaTypeValue),
    from: route.fullPath,
  };
  if (selectedColumnContext.value) {
    query.column = selectedColumnContext.value;
  }

  router.push({
    name: "OpenLineageColumnLineage",
    params: { guid: guidToRouteParams(selectedNodeSummary.value.guid) },
    query,
  });
}

function createEmptyNodeLineageData(): NodeLineageData {
  return {
    upstream: [],
    downstream: [],
    upstreamLoaded: false,
    downstreamLoaded: false,
  };
}

function getNodeLineageData(guid: string): NodeLineageData {
  return nodeDataMap.value.get(guid) ?? createEmptyNodeLineageData();
}

function isDirectionLoaded(
  data: NodeLineageData,
  lineageType: LineageType
): boolean {
  switch (lineageType) {
    case LineageType.SOURCE:
      return data.upstreamLoaded;
    case LineageType.TARGET:
      return data.downstreamLoaded;
    default:
      return data.upstreamLoaded && data.downstreamLoaded;
  }
}

function directionToLineageType(direction: LineageDirection): LineageType {
  return direction === "upstream" ? LineageType.SOURCE : LineageType.TARGET;
}

async function fetchLineageForGuid(
  guid: string,
  lineageType: LineageType = LineageType.LINEAGE_TYPE_UNSPECIFIED
): Promise<NodeLineageData> {
  const existingData = getNodeLineageData(guid);
  if (isDirectionLoaded(existingData, lineageType)) {
    return existingData;
  }

  const metaType = guidMetaTypeMap.value.get(guid) ?? currentMetaType.value;

  try {
    const response = await getLineage({
      guid,
      metaType,
      lineageType,
    });
    const data: NodeLineageData = {
      upstream:
        lineageType === LineageType.TARGET
          ? existingData.upstream
          : response.relationsSource,
      downstream:
        lineageType === LineageType.SOURCE
          ? existingData.downstream
          : response.relationsTarget,
      upstreamLoaded:
        lineageType === LineageType.TARGET ? existingData.upstreamLoaded : true,
      downstreamLoaded:
        lineageType === LineageType.SOURCE
          ? existingData.downstreamLoaded
          : true,
    };
    nodeDataMap.value.set(guid, data);

    // Store external dataset info from response
    for (const ext of response.externalDatasets) {
      if (ext.guid && !externalDatasetMap.value.has(ext.guid)) {
        externalDatasetMap.value.set(ext.guid, ext);
      }
    }

    // Record metaType for all guids referenced in the relations
    for (const rel of data.upstream) {
      if (rel.sourceType && !guidMetaTypeMap.value.has(rel.sourceGuid)) {
        guidMetaTypeMap.value.set(rel.sourceGuid, rel.sourceType);
      }
      if (rel.targetType && !guidMetaTypeMap.value.has(rel.targetGuid)) {
        guidMetaTypeMap.value.set(rel.targetGuid, rel.targetType);
      }
    }
    for (const rel of data.downstream) {
      if (rel.sourceType && !guidMetaTypeMap.value.has(rel.sourceGuid)) {
        guidMetaTypeMap.value.set(rel.sourceGuid, rel.sourceType);
      }
      if (rel.targetType && !guidMetaTypeMap.value.has(rel.targetGuid)) {
        guidMetaTypeMap.value.set(rel.targetGuid, rel.targetType);
      }
    }

    return data;
  } catch (e) {
    console.error(
      `Failed to fetch lineage for ${guid}:`,
      extractErrorMessage(e)
    );
    const empty: NodeLineageData = {
      upstream: lineageType === LineageType.TARGET ? existingData.upstream : [],
      downstream:
        lineageType === LineageType.SOURCE ? existingData.downstream : [],
      upstreamLoaded:
        lineageType === LineageType.TARGET ? existingData.upstreamLoaded : true,
      downstreamLoaded:
        lineageType === LineageType.SOURCE
          ? existingData.downstreamLoaded
          : true,
    };
    nodeDataMap.value.set(guid, empty);
    return empty;
  }
}

// Collect unique columns for a given guid from all lineage relations
function collectColumnsForGuid(guid: string): string[] {
  const columns = new Set<string>();
  for (const [, data] of nodeDataMap.value) {
    for (const rel of data.upstream) {
      if (rel.sourceGuid === guid && rel.sourceColumn)
        columns.add(rel.sourceColumn);
      if (rel.targetGuid === guid && rel.targetColumn)
        columns.add(rel.targetColumn);
    }
    for (const rel of data.downstream) {
      if (rel.sourceGuid === guid && rel.sourceColumn)
        columns.add(rel.sourceColumn);
      if (rel.targetGuid === guid && rel.targetColumn)
        columns.add(rel.targetColumn);
    }
  }
  return Array.from(columns).sort();
}

function nodeDataFor(guid: string): LineageNodeData {
  const data = nodeDataMap.value.get(guid);
  return {
    guid,
    label: formatGuidLabel(guid),
    shortPath: formatGuidShort(guid),
    isRoot: guid === currentGuid.value,
    upstreamLoaded: data?.upstreamLoaded ?? false,
    downstreamLoaded: data?.downstreamLoaded ?? false,
    upstreamCount: data?.upstream.length ?? 0,
    downstreamCount: data?.downstream.length ?? 0,
    metaType: guidToMetaType(guid),
    columns: collectColumnsForGuid(guid),
    selectedColumn:
      selectedColumnGuid.value === guid ? selectedColumnName.value : null,
    highlightedColumns: highlightedColumnsMap.value.get(guid) ?? new Set(),
  };
}

/** Rebuilds every node from scratch, which also re-runs the layer layout. */
function rebuildGraph() {
  const layers = assignLayers(currentGuid.value, nodeDataMap.value);
  const positions = layoutNodes(layers, (guid) =>
    nodeHeight(
      collectColumnsForGuid(guid).length,
      fieldsVisibleGuids.value.has(guid)
    )
  );

  const nodeMap = new Map<string, Node>();
  for (const [guid, position] of positions) {
    nodeMap.set(guid, {
      id: guid,
      type: "lineage",
      position,
      data: nodeDataFor(guid),
    });
  }

  nodes.value = Array.from(nodeMap.values());
  edges.value = buildLineageEdges(nodeDataMap.value, {
    validNodeIds: new Set(nodeMap.keys()),
    columnFilter: columnFilter.value,
  });
}

/** Refreshes node data and edges, keeping positions the user may have dragged. */
function updateGraphState() {
  const currentPositions = new Map<string, { x: number; y: number }>();
  for (const node of getNodes.value) {
    currentPositions.set(node.id, { ...node.position });
  }

  nodes.value = nodes.value.map((node) => ({
    ...node,
    position: currentPositions.get(node.id) ?? node.position,
    data: nodeDataFor(node.id),
  }));
  edges.value = buildLineageEdges(nodeDataMap.value, {
    validNodeIds: new Set(nodes.value.map((node) => node.id)),
    columnFilter: columnFilter.value,
  });
}

function clearColumnSelection() {
  selectedColumnGuid.value = null;
  selectedColumnName.value = null;
  highlightedColumnsMap.value.clear();
  updateGraphState();
}

async function handleSelectNode(guid: string) {
  if (selectedNodeGuid.value === guid) {
    closeSelectedNode();
    return;
  }
  setSelectedNode(guid);

  // Pre-fetch lineage data for non-root nodes so the drawer shows
  // accurate upstream/downstream counts and related runs immediately,
  // without expanding the graph (the user can do that via "Expand").
  if (guid !== currentGuid.value && !nodeDataMap.value.has(guid)) {
    await fetchLineageForGuid(guid);
  }
}

function closeSelectedNode() {
  setSelectedNode(null);
}

function handleToggleFields(guid: string, visible: boolean) {
  if (visible) {
    fieldsVisibleGuids.value.add(guid);
  } else {
    fieldsVisibleGuids.value.delete(guid);
    clearColumnSelection();
  }
}

function handleSelectColumn(guid: string, column: string) {
  if (selectedNodeGuid.value !== guid) {
    setSelectedNode(guid);
  }

  // Toggle off if same column clicked again
  if (
    selectedColumnGuid.value === guid &&
    selectedColumnName.value === column
  ) {
    clearColumnSelection();
    return;
  }

  selectedColumnGuid.value = guid;
  selectedColumnName.value = column;

  // Find related columns on neighbouring nodes
  const highlighted = new Map<string, Set<string>>();

  for (const [nodeGuid, data] of nodeDataMap.value) {
    for (const rel of data.upstream) {
      if (rel.targetGuid === guid && rel.targetColumn === column) {
        if (!highlighted.has(rel.sourceGuid))
          highlighted.set(rel.sourceGuid, new Set());
        highlighted.get(rel.sourceGuid)!.add(rel.sourceColumn);
      }
      if (rel.sourceGuid === guid && rel.sourceColumn === column) {
        if (!highlighted.has(nodeGuid)) highlighted.set(nodeGuid, new Set());
        highlighted.get(nodeGuid)!.add(rel.targetColumn);
      }
    }
    for (const rel of data.downstream) {
      if (rel.sourceGuid === guid && rel.sourceColumn === column) {
        if (!highlighted.has(rel.targetGuid))
          highlighted.set(rel.targetGuid, new Set());
        highlighted.get(rel.targetGuid)!.add(rel.targetColumn);
      }
      if (rel.targetGuid === guid && rel.targetColumn === column) {
        if (!highlighted.has(nodeGuid)) highlighted.set(nodeGuid, new Set());
        highlighted.get(nodeGuid)!.add(rel.sourceColumn);
      }
    }
  }

  highlightedColumnsMap.value = highlighted;
  updateGraphState();
}

async function handleExpandNode(guid: string, direction: LineageDirection) {
  const lineageType = directionToLineageType(direction);
  if (isDirectionLoaded(getNodeLineageData(guid), lineageType)) return;

  expandedGuids.value.add(guid);

  await fetchLineageForGuid(guid, lineageType);
  rebuildGraph();

  setTimeout(() => fitView({ duration: 300 }), 50);
}

function handleReset() {
  // Restore the initial snapshot
  expandedGuids.value = new Set(initialExpandedGuids);
  nodeDataMap.value = new Map(
    Array.from(initialNodeDataMap.entries()).map(([k, v]) => [k, { ...v }])
  );
  selectedColumnGuid.value = null;
  selectedColumnName.value = null;
  highlightedColumnsMap.value.clear();
  fieldsVisibleGuids.value.clear();
  rebuildGraph();
  if (selectedNodeGuid.value && selectedNodeGuid.value !== currentGuid.value) {
    closeSelectedNode();
  }
  setTimeout(() => fitView({ duration: 300 }), 50);
}

function handleFitView() {
  fitView({ duration: 300 });
}

function handleBackToMetadata() {
  const from = route.query.from;
  if (typeof from === "string" && from.length > 0) {
    router.push(from);
    return;
  }

  if (!currentGuid.value) {
    router.push({ name: "MetadataBrowser" });
    return;
  }
  router.push({
    name: "MetadataDetail",
    params: { guid: guidToRouteParams(currentGuid.value) },
    query: { metaType: String(currentMetaType.value) },
  });
}

function saveInitialSnapshot() {
  initialExpandedGuids = new Set(expandedGuids.value);
  initialNodeDataMap = new Map(
    Array.from(nodeDataMap.value.entries()).map(([k, v]) => [k, { ...v }])
  );
}

function syncSelectedNodeVisibility() {
  if (
    selectedNodeGuid.value &&
    !nodes.value.some((node) => node.id === selectedNodeGuid.value)
  ) {
    closeSelectedNode();
  }
}

async function initializeGraph() {
  if (!currentGuid.value) return;

  initialLoading.value = true;
  expandedGuids.value.clear();
  nodeDataMap.value.clear();
  guidMetaTypeMap.value.clear();
  selectedColumnGuid.value = null;
  selectedColumnName.value = null;
  highlightedColumnsMap.value.clear();

  expandedGuids.value.add(currentGuid.value);
  guidMetaTypeMap.value.set(currentGuid.value, currentMetaType.value);
  await fetchLineageForGuid(currentGuid.value);
  rebuildGraph();
  syncSelectedNodeVisibility();
  saveInitialSnapshot();

  initialLoading.value = false;
  setTimeout(() => fitView({ duration: 300 }), 100);
}

onMounted(() => {
  initializeGraph();
});

watch(currentGuid, () => {
  initializeGraph();
});

watch(nodes, () => {
  syncSelectedNodeVisibility();
});
</script>
