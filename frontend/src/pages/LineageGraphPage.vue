<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ t("lineageGraph.title") }}
        </h1>
        <p class="text-muted-foreground">
          {{ rootLabel || t("lineageGraph.description") }}
        </p>
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

    <Card class="relative overflow-hidden" style="height: calc(100vh - 12rem)">
      <div v-if="initialLoading" class="absolute inset-0 flex items-center justify-center bg-background/80 z-10">
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
            @select-column="handleSelectColumn"
            @toggle-fields="handleToggleFields"
          />
        </template>
      </VueFlow>
    </Card>
  </div>
</template>

<script setup lang="ts">
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
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { LineageRelation } from "@/types/proto-es/v1/lineage_service_pb";
import { extractErrorMessage } from "@/utils/error";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const { fitView, getNodes } = useVueFlow();

const HORIZONTAL_GAP = 280;

const nodes = ref<Node[]>([]);
const edges = ref<Edge[]>([]);
const initialLoading = ref(true);

const expandedGuids = ref<Set<string>>(new Set());
const nodeDataMap = ref<
  Map<string, { upstream: LineageRelation[]; downstream: LineageRelation[] }>
>(new Map());

// Track actual MetaType per guid, derived from lineage relation sourceType/targetType
const guidMetaTypeMap = ref<Map<string, MetaType>>(new Map());

// Snapshot of initial state for reset
let initialExpandedGuids = new Set<string>();
let initialNodeDataMap = new Map<
  string,
  { upstream: LineageRelation[]; downstream: LineageRelation[] }
>();

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

const rootLabel = computed(() => formatGuidShort(currentGuid.value));

function formatGuidShort(guid: string): string {
  if (!guid) return "";
  const segments = guid.split(";").filter(Boolean);
  if (segments.length === 0) return guid;
  return segments.slice(-3).join(".");
}

function formatGuidLabel(guid: string): string {
  if (!guid) return "";
  const segments = guid.split(";").filter(Boolean);
  return segments[segments.length - 1] || guid;
}

function guidToMetaType(guid: string): string {
  const segments = guid.split(";").filter(Boolean);
  if (segments.length <= 1) return "instance";
  if (segments.length === 2) return "database";
  if (segments.length === 3) return "schema";
  return "table";
}

function toGuidPath(guid: string): string {
  return guid
    .split(";")
    .map((s) => (s === "" ? "~" : encodeURIComponent(s)))
    .join("/");
}

async function fetchLineageForGuid(
  guid: string
): Promise<{ upstream: LineageRelation[]; downstream: LineageRelation[] }> {
  if (nodeDataMap.value.has(guid)) {
    return nodeDataMap.value.get(guid)!;
  }

  const metaType = guidMetaTypeMap.value.get(guid) ?? currentMetaType.value;

  try {
    const response = await getLineage({
      guid,
      metaType,
    });
    const data = {
      upstream: response.relationsSource,
      downstream: response.relationsTarget,
    };
    nodeDataMap.value.set(guid, data);

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
    const empty = { upstream: [], downstream: [] };
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
      const isExpanded = expandedGuids.value.has(guid);

      const nodeData: LineageNodeData = {
        guid,
        label: formatGuidLabel(guid),
        shortPath: formatGuidShort(guid),
        isRoot: guid === currentGuid.value,
        expanded: isExpanded,
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
    const isExpanded = expandedGuids.value.has(guid);

    const nodeData: LineageNodeData = {
      guid,
      label: formatGuidLabel(guid),
      shortPath: formatGuidShort(guid),
      isRoot: guid === currentGuid.value,
      expanded: isExpanded,
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

function handleToggleFields(guid: string, visible: boolean) {
  if (visible) {
    fieldsVisibleGuids.value.add(guid);
  } else {
    fieldsVisibleGuids.value.delete(guid);
    clearColumnSelection();
  }
}

function handleSelectColumn(guid: string, column: string) {
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

async function handleExpandNode(guid: string) {
  if (expandedGuids.value.has(guid)) return;
  expandedGuids.value.add(guid);

  await fetchLineageForGuid(guid);
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
  setTimeout(() => fitView({ duration: 300 }), 50);
}

function handleFitView() {
  fitView({ duration: 300 });
}

function handleBackToMetadata() {
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
</script>
