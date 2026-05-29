<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ t("lineageGraph.title") }}
        </h1>
      </div>
      <div class="flex items-center gap-2">
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
      </div>
    </div>

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
import type { LineageNodeData } from "@/components/lineage/LineageNode.vue";
import LineageNode from "@/components/lineage/LineageNode.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type {
  ExternalDatasetInfo,
  LineageRelation,
} from "@/types/proto-es/v1/lineage_service_pb";
import { LineageType } from "@/types/proto-es/v1/lineage_service_pb";
import { extractErrorMessage } from "@/utils/error";

const EXTERNAL_PREFIX = "external:";

function isExternalGuid(guid: string): boolean {
  return guid.startsWith(EXTERNAL_PREFIX);
}

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const { fitView, getNodes } = useVueFlow();

const HORIZONTAL_GAP = 280;
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

const currentGuid = computed(() => {
  const guidParam = route.params.guid;
  if (!guidParam) return "";
  const guidStr = Array.isArray(guidParam) ? guidParam.join("/") : guidParam;
  return guidStr
    .split("/")
    .map((s) => decodeURIComponent(s))
    .map((s) => (s === "~" ? "" : s))
    .join(";");
});

const currentMetaType = computed(() => {
  const q = route.query.metaType;
  if (!q) return MetaType.TABLE;
  const raw = Array.isArray(q) ? q[0] : q;
  const value = Number(raw);
  if (!Number.isFinite(value)) return MetaType.TABLE;
  return value as MetaType;
});

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

  return {
    guid: selectedNodeGuid.value,
    label: formatGuidLabel(selectedNodeGuid.value),
    shortPath: formatGuidShort(selectedNodeGuid.value),
    isRoot: selectedNodeGuid.value === currentGuid.value,
    isExternal: isExternalGuid(selectedNodeGuid.value),
    metaTypeValue:
      guidMetaTypeMap.value.get(selectedNodeGuid.value) ??
      currentMetaType.value,
    metaTypeLabel: formatMetaTypeLabel(selectedNodeGuid.value),
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
  if (!ts?.seconds) return "-";
  const date = new Date(Number(ts.seconds) * 1000);
  return new Intl.DateTimeFormat("default", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
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

function formatMetaTypeLabel(guid: string): string {
  const metaType = guidToMetaType(guid);
  switch (metaType) {
    case "materialized_view":
      return "Materialized View";
    case "view":
      return "View";
    case "table":
      return "Table";
    case "schema":
      return "Schema";
    case "database":
      return "Database";
    case "instance":
      return "Instance";
    case "external":
      return "External Dataset";
    default:
      return metaType;
  }
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
    params: { guid: toGuidPath(selectedNodeSummary.value.guid) },
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
    params: { guid: toGuidPath(selectedNodeSummary.value.guid) },
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
    params: { guid: toGuidPath(selectedNodeSummary.value.guid) },
    query,
  });
}

function toGuidPath(guid: string): string {
  return guid
    .split(";")
    .map((s) => (s === "" ? "~" : encodeURIComponent(s)))
    .join("/");
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

function collectRelatedGuids(guid: string): Set<string> {
  const related = new Set<string>();
  const data = nodeDataMap.value.get(guid);
  if (!data) return related;

  for (const rel of data.upstream) {
    related.add(rel.sourceGuid);
  }
  for (const rel of data.downstream) {
    related.add(rel.targetGuid);
  }
  return related;
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

function rebuildGraph() {
  const nodeMap = new Map<string, Node>();
  const edgeList: Edge[] = [];
  const edgeSet = new Set<string>();

  // Collect all nodes and their relationships
  const allGuids = new Set<string>([currentGuid.value]);
  for (const [guid] of nodeDataMap.value) {
    allGuids.add(guid);
    const related = collectRelatedGuids(guid);
    for (const r of related) allGuids.add(r);
  }

  // Build adjacency for layout
  const upstreamOf = new Map<string, Set<string>>();
  const downstreamOf = new Map<string, Set<string>>();

  for (const [guid, data] of nodeDataMap.value) {
    for (const rel of data.upstream) {
      if (!upstreamOf.has(guid)) upstreamOf.set(guid, new Set());
      upstreamOf.get(guid)!.add(rel.sourceGuid);
    }
    for (const rel of data.downstream) {
      if (!downstreamOf.has(guid)) downstreamOf.set(guid, new Set());
      downstreamOf.get(guid)!.add(rel.targetGuid);
    }
  }

  // Assign layers via BFS from root
  const layers = new Map<string, number>();
  layers.set(currentGuid.value, 0);
  const queue = [currentGuid.value];
  let head = 0;

  while (head < queue.length) {
    const current = queue[head++];
    const currentLayer = layers.get(current)!;

    const ups = upstreamOf.get(current);
    if (ups) {
      for (const u of ups) {
        if (!layers.has(u)) {
          layers.set(u, currentLayer - 1);
          queue.push(u);
        }
      }
    }

    const downs = downstreamOf.get(current);
    if (downs) {
      for (const d of downs) {
        if (!layers.has(d)) {
          layers.set(d, currentLayer + 1);
          queue.push(d);
        }
      }
    }
  }

  // For orphan guids that weren't reached by BFS
  for (const guid of allGuids) {
    if (!layers.has(guid)) {
      layers.set(guid, 0);
    }
  }

  // Group by layer
  const layerGroups = new Map<number, string[]>();
  for (const [guid, layer] of layers) {
    if (!layerGroups.has(layer)) layerGroups.set(layer, []);
    layerGroups.get(layer)!.push(guid);
  }

  // Find min layer for offset
  const minLayer = Math.min(...layerGroups.keys());

  // Layout nodes
  for (const [layer, guids] of layerGroups) {
    const x = (layer - minLayer) * HORIZONTAL_GAP;
    let y = 0;
    guids.forEach((guid) => {
      const data = nodeDataMap.value.get(guid);
      const nodeData: LineageNodeData = {
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

      nodeMap.set(guid, {
        id: guid,
        type: "lineage",
        position: { x, y },
        data: nodeData,
      });

      // Advance y by estimated node height to avoid overlapping
      const BASE_NODE_HEIGHT = 120;
      const COLUMN_ROW_HEIGHT = 24;
      const MAX_FIELDS_HEIGHT = 200;
      const GAP = 20;
      let nodeHeight = BASE_NODE_HEIGHT;
      if (fieldsVisibleGuids.value.has(guid)) {
        const cols = nodeData.columns.length;
        nodeHeight += Math.min(cols * COLUMN_ROW_HEIGHT, MAX_FIELDS_HEIGHT);
      }
      y += nodeHeight + GAP;
    });
  }

  // Check if a column is selected for edge filtering
  const hasColumnFilter =
    selectedColumnGuid.value !== null && selectedColumnName.value !== null;

  // Build a set of edge ids that match the selected column's lineage
  const columnEdgeIds = new Set<string>();
  if (hasColumnFilter) {
    for (const [guid, data] of nodeDataMap.value) {
      for (const rel of data.upstream) {
        if (
          (rel.targetGuid === selectedColumnGuid.value &&
            rel.targetColumn === selectedColumnName.value) ||
          (rel.sourceGuid === selectedColumnGuid.value &&
            rel.sourceColumn === selectedColumnName.value)
        ) {
          columnEdgeIds.add(`${rel.sourceGuid}->${guid}`);
        }
      }
      for (const rel of data.downstream) {
        if (
          (rel.sourceGuid === selectedColumnGuid.value &&
            rel.sourceColumn === selectedColumnName.value) ||
          (rel.targetGuid === selectedColumnGuid.value &&
            rel.targetColumn === selectedColumnName.value)
        ) {
          columnEdgeIds.add(`${guid}->${rel.targetGuid}`);
        }
      }
    }
  }

  // Build edges
  for (const [guid, data] of nodeDataMap.value) {
    for (const rel of data.upstream) {
      const edgeId = `${rel.sourceGuid}->${guid}`;
      if (
        !edgeSet.has(edgeId) &&
        nodeMap.has(rel.sourceGuid) &&
        nodeMap.has(guid)
      ) {
        edgeSet.add(edgeId);
        const isHighlighted = hasColumnFilter && columnEdgeIds.has(edgeId);
        const isDimmed = hasColumnFilter && !isHighlighted;
        edgeList.push({
          id: edgeId,
          source: rel.sourceGuid,
          target: guid,
          animated: !isDimmed,
          style: {
            stroke: isDimmed
              ? "hsl(var(--muted-foreground) / 0.2)"
              : isHighlighted
                ? "hsl(var(--primary))"
                : "hsl(var(--primary) / 0.6)",
            strokeWidth: isHighlighted ? 3 : 2,
          },
          label: rel.relationType === 1 ? "" : "T",
          labelStyle: {
            fontSize: "10px",
            fill: "hsl(var(--muted-foreground))",
          },
        });
      }
    }
    for (const rel of data.downstream) {
      const edgeId = `${guid}->${rel.targetGuid}`;
      if (
        !edgeSet.has(edgeId) &&
        nodeMap.has(guid) &&
        nodeMap.has(rel.targetGuid)
      ) {
        edgeSet.add(edgeId);
        const isHighlighted = hasColumnFilter && columnEdgeIds.has(edgeId);
        const isDimmed = hasColumnFilter && !isHighlighted;
        edgeList.push({
          id: edgeId,
          source: guid,
          target: rel.targetGuid,
          animated: !isDimmed,
          style: {
            stroke: isDimmed
              ? "hsl(var(--muted-foreground) / 0.2)"
              : isHighlighted
                ? "hsl(var(--primary))"
                : "hsl(var(--primary) / 0.6)",
            strokeWidth: isHighlighted ? 3 : 2,
          },
          label: rel.relationType === 1 ? "" : "T",
          labelStyle: {
            fontSize: "10px",
            fill: "hsl(var(--muted-foreground))",
          },
        });
      }
    }
  }

  nodes.value = Array.from(nodeMap.values());
  edges.value = edgeList;
}

// Update only node data and edges without recalculating positions.
// This preserves manually-dragged node positions.
function updateGraphState() {
  // Build updated edges
  const edgeList: Edge[] = [];
  const edgeSet = new Set<string>();

  const hasColumnFilter =
    selectedColumnGuid.value !== null && selectedColumnName.value !== null;

  const columnEdgeIds = new Set<string>();
  if (hasColumnFilter) {
    for (const [guid, data] of nodeDataMap.value) {
      for (const rel of data.upstream) {
        if (
          (rel.targetGuid === selectedColumnGuid.value &&
            rel.targetColumn === selectedColumnName.value) ||
          (rel.sourceGuid === selectedColumnGuid.value &&
            rel.sourceColumn === selectedColumnName.value)
        ) {
          columnEdgeIds.add(`${rel.sourceGuid}->${guid}`);
        }
      }
      for (const rel of data.downstream) {
        if (
          (rel.sourceGuid === selectedColumnGuid.value &&
            rel.sourceColumn === selectedColumnName.value) ||
          (rel.targetGuid === selectedColumnGuid.value &&
            rel.targetColumn === selectedColumnName.value)
        ) {
          columnEdgeIds.add(`${guid}->${rel.targetGuid}`);
        }
      }
    }
  }

  for (const [guid, data] of nodeDataMap.value) {
    for (const rel of data.upstream) {
      const edgeId = `${rel.sourceGuid}->${guid}`;
      if (!edgeSet.has(edgeId)) {
        edgeSet.add(edgeId);
        const isHighlighted = hasColumnFilter && columnEdgeIds.has(edgeId);
        const isDimmed = hasColumnFilter && !isHighlighted;
        edgeList.push({
          id: edgeId,
          source: rel.sourceGuid,
          target: guid,
          animated: !isDimmed,
          style: {
            stroke: isDimmed
              ? "hsl(var(--muted-foreground) / 0.2)"
              : isHighlighted
                ? "hsl(var(--primary))"
                : "hsl(var(--primary) / 0.6)",
            strokeWidth: isHighlighted ? 3 : 2,
          },
          label: rel.relationType === 1 ? "" : "T",
          labelStyle: {
            fontSize: "10px",
            fill: "hsl(var(--muted-foreground))",
          },
        });
      }
    }
    for (const rel of data.downstream) {
      const edgeId = `${guid}->${rel.targetGuid}`;
      if (!edgeSet.has(edgeId)) {
        edgeSet.add(edgeId);
        const isHighlighted = hasColumnFilter && columnEdgeIds.has(edgeId);
        const isDimmed = hasColumnFilter && !isHighlighted;
        edgeList.push({
          id: edgeId,
          source: guid,
          target: rel.targetGuid,
          animated: !isDimmed,
          style: {
            stroke: isDimmed
              ? "hsl(var(--muted-foreground) / 0.2)"
              : isHighlighted
                ? "hsl(var(--primary))"
                : "hsl(var(--primary) / 0.6)",
            strokeWidth: isHighlighted ? 3 : 2,
          },
          label: rel.relationType === 1 ? "" : "T",
          labelStyle: {
            fontSize: "10px",
            fill: "hsl(var(--muted-foreground))",
          },
        });
      }
    }
  }

  edges.value = edgeList;

  // Read current positions from VueFlow's internal store (reflects drag positions)
  const currentPositions = new Map<string, { x: number; y: number }>();
  for (const n of getNodes.value) {
    currentPositions.set(n.id, { ...n.position });
  }

  // Update node data in-place, preserving positions
  nodes.value = nodes.value.map((node) => {
    const guid = node.id;
    const lineageData = nodeDataMap.value.get(guid);
    const nodeData: LineageNodeData = {
      guid,
      label: formatGuidLabel(guid),
      shortPath: formatGuidShort(guid),
      isRoot: guid === currentGuid.value,
      upstreamLoaded: lineageData?.upstreamLoaded ?? false,
      downstreamLoaded: lineageData?.downstreamLoaded ?? false,
      upstreamCount: lineageData?.upstream.length ?? 0,
      downstreamCount: lineageData?.downstream.length ?? 0,
      metaType: guidToMetaType(guid),
      columns: collectColumnsForGuid(guid),
      selectedColumn:
        selectedColumnGuid.value === guid ? selectedColumnName.value : null,
      highlightedColumns: highlightedColumnsMap.value.get(guid) ?? new Set(),
    };

    const position = currentPositions.get(guid) ?? node.position;
    return { ...node, position, data: nodeData };
  });
}

function clearColumnSelection() {
  selectedColumnGuid.value = null;
  selectedColumnName.value = null;
  highlightedColumnsMap.value.clear();
  updateGraphState();
}

function handleSelectNode(guid: string) {
  if (selectedNodeGuid.value === guid) {
    closeSelectedNode();
    return;
  }
  setSelectedNode(guid);
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
    params: { guid: toGuidPath(currentGuid.value) },
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
