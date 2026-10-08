<template>
  <div class="flex min-h-0 flex-col gap-4 xl:h-full">
    <PageHeader :title="t('lineageGraph.title')">
      <template #title-extra>
        <Badge
          variant="outline"
          class="max-w-64 truncate"
          :title="focusAssetLabel"
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

    <!-- One row that fills the remaining viewport height at `xl` and up: the
         graph and the detail column both scroll internally, so the page itself
         never scrolls and the canvas keeps its full height whatever the header
         or the selected node renders. Below `xl` the two cards stack and the
         shell's scroll container does its usual job. -->
    <div class="grid gap-4 xl:min-h-0 xl:flex-1 xl:grid-cols-[minmax(0,1fr)_24rem] xl:grid-rows-[minmax(0,1fr)]">
      <Card class="relative min-h-0 h-[70vh] overflow-hidden xl:h-full">
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
          <MiniMap
            :node-color="miniMapNodeColor"
            :node-stroke-color="miniMapNodeColor"
            :node-border-radius="3"
          />

          <template #node-lineage="nodeProps">
            <LineageNode
              :data="nodeProps.data"
              @expand="handleExpandNode"
              @select-node="handleSelectNode"
              @select-column="handleSelectColumn"
              @toggle-fields="handleToggleFields"
              @view-schema="openSchemaFor"
            />
          </template>
        </VueFlow>

        <!-- The legend is also the origin filter: an edge's colour and stroke
             say which writer stored the relation, and a row strips that writer
             off the canvas. -->
        <div class="pointer-events-none absolute left-3 top-3 z-10">
          <LineageLegend
            v-model:selected="originFilter"
            :scopes="legendScopes"
            :origins="legendOrigins"
          />
        </div>
      </Card>

      <!-- Always rendered: reserving a 24rem column only while a node happened
           to be selected left a blank strip beside the graph on arrival and
           shifted the canvas the moment anything was clicked. -->
      <Card class="overflow-hidden xl:h-full xl:min-h-0">
        <CardContent v-if="selectedNodeSummary" class="flex h-full flex-col p-0">
          <div class="flex items-start justify-between gap-3 border-b px-5 py-4">
            <div class="min-w-0 space-y-1">
              <div class="text-xs uppercase tracking-wide text-muted-foreground">
                {{ t("common.details") }}
              </div>
              <h2 class="flex items-center gap-2 text-lg font-semibold leading-tight">
                <span
                  class="size-2.5 shrink-0 rounded-full"
                  :style="{ backgroundColor: selectedNodeSummary.scopeColor }"
                />
                <span class="truncate">{{ selectedNodeSummary.label }}</span>
              </h2>
              <p class="truncate text-xs text-muted-foreground" :title="selectedNodeSummary.guid">
                {{ selectedNodeSummary.fullLabel }}
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
              </div>

              <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("lineageGraph.scope") }}</div>
                  <div class="mt-1 flex items-center gap-2 text-sm font-medium">
                    <span
                      class="size-2.5 shrink-0 rounded-full"
                      :style="{ backgroundColor: selectedNodeSummary.scopeColor }"
                    />
                    <span class="truncate">{{ selectedNodeSummary.scopeLabel || "-" }}</span>
                  </div>
                </div>
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("lineageGraph.qualifiedName") }}</div>
                  <div class="mt-1 break-all text-sm font-medium">{{ selectedNodeSummary.fullLabel }}</div>
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

              <!-- How far the field runs across the canvas. The trail is walked
                   over the drawn graph, so these counts are about what is here,
                   not about the whole stored graph. -->
              <div v-if="fieldTrailSummary" class="space-y-2">
                <div class="text-xs uppercase tracking-wide text-muted-foreground">
                  {{ t("lineageGraph.fieldTrail") }}
                </div>
                <p class="text-xs text-muted-foreground">
                  {{ t("lineageGraph.fieldTrailHint") }}
                </p>
                <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
                  <div
                    v-for="side in [
                      { key: 'up', label: t('metadataBrowser.upstream'), data: fieldTrailSummary.upstream },
                      { key: 'down', label: t('metadataBrowser.downstream'), data: fieldTrailSummary.downstream },
                    ]"
                    :key="side.key"
                    class="rounded-md border p-3"
                  >
                    <div class="text-xs text-muted-foreground">{{ side.label }}</div>
                    <div class="mt-1 text-sm font-medium">
                      {{ t("lineageGraph.legendObjectCount", { count: side.data.nodeIds.size }) }}
                      ·
                      {{
                        t("lineageGraph.fieldTrailRelationCount", {
                          count: side.data.relationCount,
                        })
                      }}
                    </div>
                  </div>
                </div>
                <p
                  v-if="fieldTrailSummary.truncated"
                  class="text-xs text-muted-foreground"
                >
                  {{ t("lineageGraph.fieldTrailTruncated") }}
                </p>
                <Button
                  variant="outline"
                  size="sm"
                  :disabled="!dimsOffTrail"
                  @click="handleFitTrail"
                >
                  {{ t("lineageGraph.fieldTrailFit") }}
                </Button>
              </div>

              <!-- Which writers reached this object, before the graph's own
                   origin filter: the number is about the stored graph, not
                   about what happens to be drawn. -->
              <div v-if="selectedNodeOrigins.length > 0" class="space-y-2">
                <div class="text-xs uppercase tracking-wide text-muted-foreground">
                  {{ t("lineageGraph.originLabel") }}
                </div>
                <div class="flex flex-wrap gap-2">
                  <span
                    v-for="item in selectedNodeOrigins"
                    :key="item.origin"
                    class="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs"
                  >
                    <OriginSwatch :origin="item.origin" :width="18" />
                    {{ t(originLabelKey(item.origin)) }}
                    <span class="tabular-nums text-muted-foreground">
                      {{ item.count }}
                      {{ t("metadataBrowser.lineageRelationsCount") }}
                    </span>
                  </span>
                </div>
              </div>

              <div class="flex flex-wrap gap-2">
                <!-- The same overlay the metadata pages show, so a reader never has
                     to leave the graph to read an object's definition. A dataset
                     outside every instance has no definition to read. -->
                <Button
                  variant="outline"
                  size="sm"
                  :disabled="selectedNodeSummary.isExternal"
                  @click="openSchemaFor(selectedNodeSummary.guid)"
                >
                  <Code class="mr-1 size-3.5" />
                  {{ t("metadataBrowser.viewSchema") }}
                </Button>
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
        <CardContent v-else class="flex h-full items-center justify-center">
          <EmptyState
            :icon="MousePointerClick"
            :title="t('lineageGraph.selectNodeHint')"
          />
        </CardContent>
      </Card>
    </div>

    <SchemaDefinitionDialog
      v-if="schemaTarget"
      v-model:open="schemaOpen"
      :guid="schemaTarget.guid"
      :meta-type="schemaTarget.metaType"
      :object-name="schemaTarget.objectName"
    />
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Background } from "@vue-flow/background";
import { Controls } from "@vue-flow/controls";
import {
  type Edge,
  type GraphNode,
  type Node,
  useVueFlow,
  VueFlow,
} from "@vue-flow/core";
import { MiniMap } from "@vue-flow/minimap";
import {
  computed,
  defineAsyncComponent,
  nextTick,
  onMounted,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import "@vue-flow/core/dist/style.css";
import "@vue-flow/core/dist/theme-default.css";
import "@vue-flow/controls/dist/style.css";
import "@vue-flow/minimap/dist/style.css";
import {
  ArrowLeft,
  Code,
  Maximize2,
  MousePointerClick,
  RotateCcw,
} from "lucide-vue-next";
import { getLineage, getLineageCounts, type LineageCount } from "@/api/lineage";
import AppLoading from "@/components/common/AppLoading.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import LineageLegend, {
  type LegendOrigin,
  type LegendScope,
} from "@/components/lineage/LineageLegend.vue";
import type {
  LineageNodeData,
  LineageNodeKind,
} from "@/components/lineage/LineageNode.vue";
import LineageNode from "@/components/lineage/LineageNode.vue";
import OriginSwatch from "@/components/lineage/OriginSwatch.vue";
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
  distinctRelationCounts,
  layoutNodes,
  nodeHeight,
} from "@/lib/lineageGraph";
import {
  countRelationsByOrigin,
  LINEAGE_ORIGINS,
  type LineageOrigin,
  originLabelKey,
  type RelationOrigin,
} from "@/lib/lineageOrigin";
import { collectFieldTrail, type FieldTrail } from "@/lib/lineageTrail";

// Loaded on demand: the dialog renders its definition in Monaco, and the lineage
// canvas has no use for a 3MB editor until a reader asks for a schema.
const SchemaDefinitionDialog = defineAsyncComponent(
  () => import("@/components/metadata/SchemaDefinitionDialog.vue")
);

import { openlineageRunLabel } from "@/lib/openlineageRun";
import { useInstanceStore } from "@/store/modules/instance";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type {
  ExternalDatasetInfo,
  LineageRelation,
} from "@/types/proto-es/v1/lineage_service_pb";
import { LineageType } from "@/types/proto-es/v1/lineage_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { extractErrorMessage } from "@/utils/error";
import { guidToRouteParams, routeParamToGuid } from "@/utils/guid";
import {
  buildLineageScopeColors,
  isExternalGuid,
  type LineageAssetView,
  lineageAssetView,
  scopeColor,
} from "@/utils/lineageAsset";
import { metaTypeLabel, parseMetaType } from "@/utils/metaType";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { fitView, getNodes } = useVueFlow();
const instanceStore = useInstanceStore();

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

// Which (node, direction) pairs the user has drawn into the graph. This is
// deliberately separate from `NodeLineageData`'s loaded flags, which only mean
// the data was fetched: selecting a node fetches both directions for the detail
// panel without expanding anything, so a loaded node is not an expanded one.
const expandedDirections = ref<Set<string>>(new Set());
const nodeDataMap = ref<Map<string, NodeLineageData>>(new Map());
// Every visible node's degree, from the batched counts RPC: a node cannot be
// labelled from its neighbours' relations, and fetching each node's relations
// just to print two numbers downloads the whole graph. Kept apart from
// `nodeDataMap`, which only holds the relations of expanded objects.
const lineageCounts = ref<Map<string, LineageCount>>(new Map());
const expandDepth = ref("1");

const focusAssetLabel = computed(() => {
  return assetViewFor(currentGuid.value).fullLabel;
});

// Track actual MetaType per guid, derived from lineage relation sourceType/targetType
const guidMetaTypeMap = ref<Map<string, MetaType>>(new Map());

// External dataset info cache: guid -> ExternalDatasetInfo
const externalDatasetMap = ref<Map<string, ExternalDatasetInfo>>(new Map());

/**
 * The instance titles the graph labels its nodes with. An instance is what tells
 * two objects with the same database, schema and table name apart, so the label
 * and the accent both come from here rather than from the GUID's trailing
 * segments.
 */
const scopeTitles = computed<Map<string, string>>(() => {
  const titles = new Map<string, string>();
  for (const instance of instanceStore.instances) {
    const id = instance.name.split("/").pop() ?? instance.name;
    titles.set(id, instance.title || id);
  }
  return titles;
});

/**
 * One accent per scope. Instances come first in sorted order and external
 * namespaces after them, so expanding the graph to a dataset outside every
 * instance never recolours the instances already on screen.
 */
const scopeColorMap = computed(() =>
  buildLineageScopeColors({
    instanceIds: scopeTitles.value.keys(),
    externalDatasetGuids: externalDatasetMap.value.keys(),
    externalOf: (guid) => externalDatasetMap.value.get(guid),
  })
);

const originFilter = ref<RelationOrigin[]>([...LINEAGE_ORIGINS]);

/**
 * The object whose schema is on screen, and whether that overlay is open. It is
 * held apart from the selection because both the details panel and a node's
 * context menu open it, and the context menu does not have to select anything.
 */
const schemaTarget = ref<{
  guid: string;
  metaType: MetaType;
  objectName: string;
} | null>(null);
const schemaOpen = ref(false);

// Snapshot of initial state for reset
let initialExpandedDirections = new Set<string>();
let initialNodeDataMap = new Map<string, NodeLineageData>();

// Field-level column selection state
const selectedColumnGuid = ref<string | null>(null);
const selectedColumnName = ref<string | null>(null);
/**
 * The trail the selected column runs through the drawn graph, refreshed whenever
 * the graph is. Null when no column is selected.
 */
const fieldTrail = ref<FieldTrail | null>(null);

// Track which nodes have their fields panel visible (for layout height calculation)
const fieldsVisibleGuids = ref<Set<string>>(new Set());

const hasExpandedBeyondRoot = computed(() => {
  return expandedDirections.value.size > initialExpandedDirections.size;
});

const selectedNodeGuid = computed(() => {
  const node = route.query.node;
  return typeof node === "string" && node.length > 0 ? node : null;
});

const currentGuid = computed(() => routeParamToGuid(route.params.guid));

const currentMetaType = computed(
  () => parseMetaType(route.query.metaType) ?? MetaType.TABLE
);

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

  const counts = lineageCountFor(selectedNodeGuid.value);
  const view = assetViewFor(selectedNodeGuid.value);

  return {
    guid: selectedNodeGuid.value,
    label: view.name,
    fullLabel: view.fullLabel,
    scopeLabel: view.scopeLabel,
    scopeColor: scopeColor(scopeColorMap.value, view.scopeKey),
    isRoot: selectedNodeGuid.value === currentGuid.value,
    isExternal: view.isExternal,
    metaTypeValue,
    metaTypeLabel: metaTypeLabel(metaTypeValue, t),
    upstreamCount: counts.upstream,
    downstreamCount: counts.downstream,
    columns: collectColumnsForGuid(selectedNodeGuid.value),
    externalNamespace: externalInfo?.namespace ?? "",
    externalDatasetType: externalInfo?.datasetType ?? "",
  };
});

/**
 * Which writers reached the selected object, from its loaded relations. This is a
 * property of the stored graph, so it is reported before the canvas' origin filter
 * — the graph can hide an origin, but the detail panel still says it is there.
 */
const selectedNodeOrigins = computed<LegendOrigin[]>(() => {
  if (!selectedNodeGuid.value) {
    return [];
  }

  const data = getNodeLineageData(selectedNodeGuid.value);
  const counts = countRelationsByOrigin([...data.upstream, ...data.downstream]);
  return LINEAGE_ORIGINS.filter((origin) => counts[origin] > 0).map(
    (origin) => ({
      origin,
      count: counts[origin],
    })
  );
});

/**
 * The scopes the drawn nodes belong to, with how many nodes each contributes, so
 * a reader can see at a glance that e.g. two of the six nodes are in another
 * instance.
 */
const legendScopes = computed<LegendScope[]>(() => {
  const byScope = new Map<string, LegendScope>();
  for (const node of nodes.value) {
    const view = assetViewFor(node.id);
    const entry = byScope.get(view.scopeKey);
    if (entry) {
      entry.count += 1;
      continue;
    }
    byScope.set(view.scopeKey, {
      key: view.scopeKey,
      label: view.scopeLabel || t("lineageGraph.unknownScope"),
      color: scopeColor(scopeColorMap.value, view.scopeKey),
      count: 1,
    });
  }
  return [...byScope.values()].sort((left, right) =>
    left.label.localeCompare(right.label)
  );
});

/**
 * The origin rows of the legend: every origin a filter can name, plus `mixed`
 * once the canvas actually bundles two writers into one edge. Counts are of drawn
 * edges, so a filtered-out origin reads as zero.
 */
const legendOrigins = computed<LegendOrigin[]>(() => {
  const drawn = new Map<LineageOrigin, number>();
  for (const edge of edges.value) {
    const origin = (edge.data as { origin?: LineageOrigin } | undefined)
      ?.origin;
    if (origin) {
      drawn.set(origin, (drawn.get(origin) ?? 0) + 1);
    }
  }

  const rows: LegendOrigin[] = LINEAGE_ORIGINS.map((origin) => ({
    origin,
    count: drawn.get(origin) ?? 0,
  }));
  if (drawn.has("mixed")) {
    rows.push({ origin: "mixed", count: drawn.get("mixed") ?? 0 });
  }
  return rows;
});

/**
 * The selected field's trail, summarised for the detail panel: how far it reaches
 * on each side of the pivot. An empty side is not an error — it says this field is
 * a source or a sink in what is drawn.
 */
const fieldTrailSummary = computed(() => {
  const trail = fieldTrail.value;
  if (!trail) {
    return null;
  }
  return {
    upstream: trail.upstream,
    downstream: trail.downstream,
    truncated: trail.truncated,
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
        label: openlineageRunLabel(relation.metaGuid),
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

/** The scope, name and path of one object, from the instance titles and the
 * external dataset metadata the graph has gathered so far. */
function assetViewFor(guid: string): LineageAssetView {
  if (!guid) {
    return {
      scopeKey: "",
      scopeLabel: "",
      name: "",
      qualifier: "",
      fullLabel: "",
      isExternal: false,
    };
  }
  return lineageAssetView(
    guid,
    scopeTitles.value,
    externalDatasetMap.value.get(guid)
  );
}

/**
 * The metadata type a node's icon and type badge use. A GUID names its own type by
 * how many segments it has — and a MySQL schema is an empty segment, not a missing
 * one — so the count is positional. The type the relations report wins whenever
 * they report one; this is what a node nothing has described yet falls back to.
 */
function fallbackMetaType(guid: string): MetaType {
  if (isExternalGuid(guid)) return MetaType.EXTERNAL_DATASET;
  const segments = guid.split(";");
  while (segments.length > 0 && segments[segments.length - 1] === "") {
    segments.pop();
  }
  if (segments.length >= 4) return MetaType.TABLE;
  if (segments.length === 3) return MetaType.SCHEMA;
  if (segments.length === 2) return MetaType.DATABASE;
  return MetaType.INSTANCE;
}

/** The minimap keeps the canvas' scope colours, so the overview reads like the
 * graph it summarises rather than as anonymous grey blocks. */
function miniMapNodeColor(node: GraphNode): string {
  const data = node.data as LineageNodeData | undefined;
  return data?.scopeColor ?? "hsl(var(--muted-foreground))";
}

function nodeKind(metaType: MetaType, external: boolean): LineageNodeKind {
  if (
    external ||
    metaType === MetaType.EXTERNAL_DATASET ||
    metaType === MetaType.EXTERNAL_TABLE
  ) {
    return "external";
  }
  if (metaType === MetaType.VIEW || metaType === MetaType.MATERIALIZED_VIEW) {
    return "view";
  }
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

/** Opens the same schema overlay the metadata pages show, for one node. */
function openSchemaFor(guid: string) {
  if (!guid || isExternalGuid(guid)) {
    return;
  }
  const view = assetViewFor(guid);
  schemaTarget.value = {
    guid,
    metaType: guidMetaTypeMap.value.get(guid) ?? fallbackMetaType(guid),
    objectName: view.name,
  };
  schemaOpen.value = true;
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

/** Whether the requested directions are already in `nodeDataMap`. */
function isDirectionFetched(
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

/** Whether the user has already drawn this direction's neighbours into the graph. */
function isDirectionExpanded(
  guid: string,
  direction: LineageDirection
): boolean {
  return expandedDirections.value.has(expansionKey(guid, direction));
}

/** A GUID may contain `:`, so the direction is the prefix, not part of the key. */
function expansionKey(guid: string, direction: LineageDirection): string {
  return `${direction}:${guid}`;
}

function directionToLineageType(direction: LineageDirection): LineageType {
  return direction === "upstream" ? LineageType.SOURCE : LineageType.TARGET;
}

async function fetchLineageForGuid(
  guid: string,
  lineageType: LineageType = LineageType.LINEAGE_TYPE_UNSPECIFIED
): Promise<NodeLineageData> {
  const existingData = getNodeLineageData(guid);
  if (isDirectionFetched(existingData, lineageType)) {
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

/**
 * A node's degree: the batched count when the server has answered for it, and
 * otherwise what its own relations say. The two agree, because both count the
 * distinct objects at the far end of the node's column-level relations.
 */
function lineageCountFor(guid: string): {
  upstream: number;
  downstream: number;
} {
  const counted = lineageCounts.value.get(guid);
  if (counted) {
    return { upstream: counted.upstream, downstream: counted.downstream };
  }
  return distinctRelationCounts(getNodeLineageData(guid));
}

function nodeDataFor(guid: string): LineageNodeData {
  const counts = lineageCountFor(guid);
  const view = assetViewFor(guid);
  // The type is resolved from the lineage relations (or the route) rather than
  // guessed from the GUID, which cannot name a MySQL object's level.
  const metaTypeValue =
    guidMetaTypeMap.value.get(guid) ?? fallbackMetaType(guid);

  return {
    guid,
    label: view.name,
    scopeLabel: view.scopeLabel,
    scopeColor: scopeColor(scopeColorMap.value, view.scopeKey),
    qualifier: view.qualifier,
    fullLabel: view.fullLabel,
    typeLabel: metaTypeLabel(metaTypeValue, t),
    kind: nodeKind(metaTypeValue, view.isExternal),
    isRoot: guid === currentGuid.value,
    upstreamExpanded: isDirectionExpanded(guid, "upstream"),
    downstreamExpanded: isDirectionExpanded(guid, "downstream"),
    upstreamCount: counts.upstream,
    downstreamCount: counts.downstream,
    columns: collectColumnsForGuid(guid),
    selectedColumn:
      selectedColumnGuid.value === guid ? selectedColumnName.value : null,
    highlightedColumns: fieldTrail.value?.columns.get(guid) ?? new Set(),
    onTrail: fieldTrail.value?.nodeIds.has(guid) ?? false,
    dimmed:
      dimsOffTrail.value && !(fieldTrail.value?.nodeIds.has(guid) ?? false),
  };
}

/**
 * Every object the drawn graph will hold: the view's own keys plus both ends of
 * every relation in it, which is what `assignLayers` lays out.
 */
function allGuidsIn(view: Map<string, NodeLineageData>): Set<string> {
  const guids = new Set<string>(view.keys());
  for (const data of view.values()) {
    for (const relation of [...data.upstream, ...data.downstream]) {
      guids.add(relation.sourceGuid);
      guids.add(relation.targetGuid);
    }
  }
  return guids;
}

/**
 * The relations the graph may draw: only directions the user has expanded.
 * `nodeDataMap` also holds what selecting a node pre-fetched for the detail
 * panel, and drawing that would add neighbours — and drop the expand
 * affordance — for a node nobody asked to expand.
 */
function graphView(): Map<string, NodeLineageData> {
  const view = new Map<string, NodeLineageData>();
  for (const [guid, data] of nodeDataMap.value) {
    view.set(guid, {
      upstream: isDirectionExpanded(guid, "upstream") ? data.upstream : [],
      downstream: isDirectionExpanded(guid, "downstream")
        ? data.downstream
        : [],
      upstreamLoaded: data.upstreamLoaded,
      downstreamLoaded: data.downstreamLoaded,
    });
  }
  return view;
}

/**
 * Whether the canvas should step back around the trail. A selected column whose
 * relations are simply not drawn has an empty trail, and fading the whole graph
 * for it would read as "this field has no lineage" rather than "nothing here
 * carries it yet".
 */
const dimsOffTrail = computed(() => (fieldTrail.value?.edgeIds.size ?? 0) > 0);

/** Recomputes the selected column's trail over the graph about to be drawn. */
function refreshFieldTrail(
  view: Map<string, NodeLineageData>,
  validNodeIds: ReadonlySet<string>
) {
  const pivot = columnFilter.value;
  fieldTrail.value = pivot
    ? collectFieldTrail(view, { pivot, validNodeIds })
    : null;
}

/** The canvas' edges: the loaded relations, the selected column's trail, and the
 * origins the legend's filter currently keeps. */
function buildEdges(
  view: Map<string, NodeLineageData>,
  validNodeIds: ReadonlySet<string>
) {
  const trail = fieldTrail.value;
  return buildLineageEdges(view, {
    validNodeIds,
    highlightedEdgeIds: trail && trail.edgeIds.size > 0 ? trail.edgeIds : null,
    originFilter: new Set(originFilter.value),
  });
}

/**
 * The height each node actually rendered at. `nodeHeight` can only guess: how many
 * lines a node's action row wraps to depends on the locale and on which actions
 * the node offers, so a node with all three of them came out 15px taller than its
 * slot and the node below crowded its footer. The browser is the only thing that
 * knows, so its answer is kept and used for the next stack.
 */
let renderedHeights = new Map<string, number>();

/** The height to place a node at: what it rendered at last, or a first guess. */
function heightFor(guid: string): number {
  return (
    renderedHeights.get(guid) ??
    nodeHeight(
      collectColumnsForGuid(guid).length,
      fieldsVisibleGuids.value.has(guid)
    )
  );
}

/** Stacks every node in its layer, and rebuilds the edges between them. */
function stack(view: Map<string, NodeLineageData>) {
  const layers = assignLayers(currentGuid.value, view);
  const positions = layoutNodes(layers, heightFor);

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
  edges.value = buildEdges(view, new Set(nodeMap.keys()));
}

/**
 * Re-stacks the layers with the heights the browser gave the cards, once the
 * renderer has measured them. Only ever one pass: positions do not feed back into
 * heights, so the second measurement agrees with the first.
 */
async function settleNodeHeights() {
  // Positions do not feed back into heights, so this converges in one or two
  // passes; the loop is here because the first measurement can be taken before the
  // browser has finished laying the cards out.
  for (let pass = 0; pass < 4; pass++) {
    const heights = await measureDrawnNodes();
    if (!heights) {
      return;
    }

    let changed = false;
    for (const [guid, height] of heights) {
      changed ||= renderedHeights.get(guid) !== height;
    }
    if (!changed) {
      return;
    }

    renderedHeights = heights;
    stack(graphView());
  }
}

/**
 * The height the browser gave each node, read off the rendered card. Vue Flow also
 * measures its nodes, but that number does not follow a card that grows in place —
 * opening a field list left it at the closed height — and a stack that is wrong
 * about a height overlaps the node below it. `offsetHeight` is a layout measurement,
 * so the canvas' zoom cannot distort it.
 */
async function measureDrawnNodes(): Promise<Map<string, number> | null> {
  for (let attempt = 0; attempt < 10; attempt++) {
    await nextTick();
    const heights = new Map<string, number>();
    for (const element of document.querySelectorAll<HTMLElement>(
      ".vue-flow__node[data-id]"
    )) {
      if (element.dataset.id) {
        heights.set(element.dataset.id, element.offsetHeight);
      }
    }
    if (heights.size > 0 && heights.size === nodes.value.length) {
      return heights;
    }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  return null;
}

/**
 * Rebuilds every node from scratch, which also re-runs the layer layout. Awaiting
 * it means awaiting the stack the browser actually rendered, so a caller that fits
 * the view afterwards frames the graph it will really see.
 */
async function rebuildGraph() {
  const view = graphView();
  // The trail is computed over the graph about to be drawn, so it can only ever
  // highlight a path the canvas shows.
  refreshFieldTrail(view, allGuidsIn(view));
  stack(view);

  // The canvas changed, so the newly drawn nodes have no degree yet. Expanding
  // the graph is the only way nodes appear, so this is where they are asked for.
  ensureLineageCounts();

  await settleNodeHeights();
}

/**
 * Labels every node on the canvas that has no degree yet. One click can reveal
 * a whole layer, and the request is batched, so twenty new nodes still cost one
 * call — the server answers from two aggregates rather than one GetLineage per
 * node. A failure leaves those nodes unlabelled (their own relations, once
 * expanded, still supply the number) rather than failing the redraw.
 */
async function ensureLineageCounts() {
  const missing = nodes.value
    .map((node) => node.id)
    .filter((guid) => !lineageCounts.value.has(guid));
  if (missing.length === 0) {
    return;
  }

  let fetched: Map<string, LineageCount>;
  try {
    fetched = await getLineageCounts(missing);
  } catch (e) {
    console.error(
      `Failed to load lineage counts for ${missing.length} nodes:`,
      extractErrorMessage(e)
    );
    return;
  }

  for (const [guid, count] of fetched) {
    lineageCounts.value.set(guid, count);
  }
  updateGraphState();
}

/** Refreshes node data and edges, keeping positions the user may have dragged. */
function updateGraphState() {
  const currentPositions = new Map<string, { x: number; y: number }>();
  for (const node of getNodes.value) {
    currentPositions.set(node.id, { ...node.position });
  }

  const view = graphView();
  refreshFieldTrail(view, new Set(nodes.value.map((node) => node.id)));

  nodes.value = nodes.value.map((node) => ({
    ...node,
    position: currentPositions.get(node.id) ?? node.position,
    data: nodeDataFor(node.id),
  }));
  edges.value = buildEdges(view, new Set(nodes.value.map((node) => node.id)));

  // A node's box can change without its position being recomputed — the trail
  // names a column on a node whose path line was empty until now. Restacking is a
  // no-op unless a box really did change.
  void settleNodeHeights();
}

function clearColumnSelection() {
  selectedColumnGuid.value = null;
  selectedColumnName.value = null;
  updateGraphState();
}

async function handleSelectNode(guid: string) {
  if (selectedNodeGuid.value === guid) {
    closeSelectedNode();
    return;
  }
  setSelectedNode(guid);

  // Pre-fetch lineage data for non-root nodes so the drawer shows accurate
  // upstream/downstream counts and related runs immediately, without expanding
  // the graph (the user can do that via "Expand"). Every non-root node is asked,
  // not just the ones the map has never seen: a node the user expanded in one
  // direction only holds an empty list for the other, and the detail panel would
  // otherwise report that empty direction as "no lineage". `fetchLineageForGuid`
  // is the cache — it returns at once when both directions are already loaded.
  if (guid !== currentGuid.value) {
    await fetchLineageForGuid(guid);
    // The fetch marks both directions as loaded but deliberately leaves the
    // graph alone, so the node still renders the pre-fetch state. Refresh the
    // node data (counts, and the expand affordances, which key off the
    // expansion set rather than the fetch flags) without adding neighbours:
    // `updateGraphState` only redraws edges between nodes that are on screen.
    updateGraphState();
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
  // The field list changes the node's height, so the column has to be stacked
  // again: without this the grown node overlaps the one below it. The height this
  // node last rendered at no longer describes it, so it goes back to the guess.
  renderedHeights.delete(guid);
  void rebuildGraph();
}

async function handleSelectColumn(guid: string, column: string) {
  // Toggle off if same column clicked again
  if (
    selectedColumnGuid.value === guid &&
    selectedColumnName.value === column
  ) {
    clearColumnSelection();
    return;
  }

  const selectingNode = selectedNodeGuid.value !== guid;
  selectedColumnGuid.value = guid;
  selectedColumnName.value = column;

  // A field click has to do what a node click does — select the node and make sure
  // its relations are loaded. Otherwise the detail panel stays empty (and with it
  // the field's trail summary) for every node whose relations were never fetched,
  // which is every node the user has not expanded or clicked before.
  if (selectingNode) {
    setSelectedNode(guid);
    if (guid !== currentGuid.value) {
      await fetchLineageForGuid(guid);
    }
  }

  // The trail is walked by `updateGraphState`, over the same drawn graph the edges
  // come from — one implementation for both, so a column can never be highlighted
  // without the edge that carries it.
  updateGraphState();
}

/** The depth control's value, clamped to the one-to-three levels it offers. */
function parseExpandDepth(value: string): number {
  const depth = Number.parseInt(value, 10);
  return Number.isFinite(depth) && depth > 0 ? depth : 1;
}

/**
 * Walks `depth` levels away from the clicked node in one direction, fetching
 * each level before the next so the metadata type of every neighbour is known
 * by the time it is requested. The depth control is what bounds the walk; the
 * expansion is deduplicated across the whole walk, so a diamond in the graph
 * is fetched once. A node the detail panel already pre-fetched is a cache hit
 * here, so the click only costs the redraw.
 */
async function handleExpandNode(guid: string, direction: LineageDirection) {
  if (isDirectionExpanded(guid, direction)) return;

  const lineageType = directionToLineageType(direction);
  const depth = parseExpandDepth(expandDepth.value);
  const revealed = new Set<string>();
  let frontier = [guid];

  for (let level = 0; level < depth && frontier.length > 0; level++) {
    const pending = frontier.filter(
      (candidate) =>
        !revealed.has(candidate) && !isDirectionExpanded(candidate, direction)
    );
    for (const candidate of pending) {
      revealed.add(candidate);
      expandedDirections.value.add(expansionKey(candidate, direction));
    }
    if (pending.length === 0) {
      break;
    }

    const loaded = await Promise.all(
      pending.map((candidate) => fetchLineageForGuid(candidate, lineageType))
    );

    const next = new Set<string>();
    for (const data of loaded) {
      const relations =
        direction === "upstream" ? data.upstream : data.downstream;
      for (const rel of relations) {
        const neighbour =
          direction === "upstream" ? rel.sourceGuid : rel.targetGuid;
        if (neighbour) {
          next.add(neighbour);
        }
      }
    }
    frontier = Array.from(next);
  }

  await rebuildGraph();

  if (revealed.size > 0) {
    fitViewWhenMeasured();
  }
}

/**
 * Fits the graph once Vue Flow holds the expanded node set with every node
 * measured. Both steps are asynchronous — the `nodes` prop is applied on a
 * later tick, and the sizes come from a resize observer — so fitting any
 * earlier frames the previous graph and leaves the new nodes off-screen.
 * The `maxZoom` cap stops a click on a two-node graph from zooming past 1:1.
 */
async function fitViewWhenMeasured() {
  for (let attempt = 0; attempt < 20; attempt++) {
    await nextTick();
    const settled =
      getNodes.value.length === nodes.value.length &&
      getNodes.value.every(
        (node) => node.dimensions.width > 0 && node.dimensions.height > 0
      );
    if (settled) {
      break;
    }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  fitView({ duration: 300, maxZoom: 1 });
}

function handleReset() {
  // Restore the initial snapshot, the origin filter included: it is part of the
  // view the user is resetting away from.
  expandedDirections.value = new Set(initialExpandedDirections);
  nodeDataMap.value = new Map(
    Array.from(initialNodeDataMap.entries()).map(([k, v]) => [k, { ...v }])
  );
  selectedColumnGuid.value = null;
  selectedColumnName.value = null;
  fieldsVisibleGuids.value.clear();
  originFilter.value = [...LINEAGE_ORIGINS];
  renderedHeights = new Map();
  void rebuildGraph();
  if (selectedNodeGuid.value && selectedNodeGuid.value !== currentGuid.value) {
    closeSelectedNode();
  }
  setTimeout(() => fitView({ duration: 300 }), 50);
}

function handleFitView() {
  fitView({ duration: 300 });
}

/** Frames the canvas on the selected field's trail, so a long flow can be read
 * without hunting for it. Falls back to the whole graph when there is no trail. */
function handleFitTrail() {
  const trail = fieldTrail.value;
  if (!trail || trail.edgeIds.size === 0) {
    handleFitView();
    return;
  }
  fitView({ nodes: [...trail.nodeIds], duration: 300, maxZoom: 1 });
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
  initialExpandedDirections = new Set(expandedDirections.value);
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
  expandedDirections.value.clear();
  nodeDataMap.value.clear();
  lineageCounts.value.clear();
  guidMetaTypeMap.value.clear();
  selectedColumnGuid.value = null;
  selectedColumnName.value = null;
  // A new object starts from the whole picture. A filter left over from the
  // previous one would draw an empty canvas — and the legend's zeroes would read
  // as "this object has no lineage" rather than "you hid its only source".
  originFilter.value = [...LINEAGE_ORIGINS];

  // The initial fetch draws the root's immediate neighbours, so both of its
  // directions count as expanded — that is what hides its expand buttons and
  // keeps "Reset" hidden until the user expands something else. The instance
  // titles are fetched alongside: they are what names and colours every node, and
  // they are one cached list shared with the rest of the app.
  expandedDirections.value.add(expansionKey(currentGuid.value, "upstream"));
  expandedDirections.value.add(expansionKey(currentGuid.value, "downstream"));
  guidMetaTypeMap.value.set(currentGuid.value, currentMetaType.value);
  // A failed instance list leaves the nodes labelled by instance id; it must not
  // cost the whole graph.
  await Promise.all([
    fetchLineageForGuid(currentGuid.value),
    instanceStore.ensureLoaded().catch(() => undefined),
  ]);
  await rebuildGraph();
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

// Instance titles arrive after the first paint on a cold cache, and the layout
// they colour is already drawn: relabel it in place rather than waiting.
watch(scopeTitles, () => {
  if (nodes.value.length > 0) {
    updateGraphState();
  }
});

// Hiding a lineage source redraws the edges only: every node keeps its place, its
// degree and its selection.
watch(originFilter, () => {
  if (nodes.value.length > 0) {
    updateGraphState();
  }
});
</script>
