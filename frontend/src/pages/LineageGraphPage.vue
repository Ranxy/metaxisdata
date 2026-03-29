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
          <LineageNode :data="nodeProps.data" @expand="handleExpandNode" />
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
import { ArrowLeft, Maximize2 } from "lucide-vue-next";
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
const { fitView } = useVueFlow();

const HORIZONTAL_GAP = 280;
const VERTICAL_GAP = 140;

const nodes = ref<Node[]>([]);
const edges = ref<Edge[]>([]);
const initialLoading = ref(true);

const expandedGuids = ref<Set<string>>(new Set());
const nodeDataMap = ref<
  Map<string, { upstream: LineageRelation[]; downstream: LineageRelation[] }>
>(new Map());

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

  try {
    const response = await getLineage({
      guid,
      metaType: currentMetaType.value,
    });
    const data = {
      upstream: response.relationsSource,
      downstream: response.relationsTarget,
    };
    nodeDataMap.value.set(guid, data);
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
    guids.forEach((guid, index) => {
      const y = index * VERTICAL_GAP;
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
      };

      nodeMap.set(guid, {
        id: guid,
        type: "lineage",
        position: { x, y },
        data: nodeData,
      });
    });
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
        edgeList.push({
          id: edgeId,
          source: rel.sourceGuid,
          target: guid,
          animated: true,
          style: { stroke: "hsl(var(--primary))", strokeWidth: 2 },
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
        edgeList.push({
          id: edgeId,
          source: guid,
          target: rel.targetGuid,
          animated: true,
          style: { stroke: "hsl(var(--primary))", strokeWidth: 2 },
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

async function handleExpandNode(guid: string) {
  if (expandedGuids.value.has(guid)) return;
  expandedGuids.value.add(guid);

  await fetchLineageForGuid(guid);
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

async function initializeGraph() {
  if (!currentGuid.value) return;

  initialLoading.value = true;
  expandedGuids.value.clear();
  nodeDataMap.value.clear();

  expandedGuids.value.add(currentGuid.value);
  await fetchLineageForGuid(currentGuid.value);
  rebuildGraph();

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
