<template>
  <div class="space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h2 class="text-sm font-medium">{{ title }}</h2>
      <div class="flex flex-wrap items-center gap-2">
        <!-- Search and counting only make sense once relations exist; an empty
             section used to reserve a heading, a search box, a badge and a
             separate empty-state paragraph for nothing. -->
        <template v-if="scopedRelations.length > 0">
          <Input
            v-model="search"
            class="h-8 w-full sm:w-72"
            :placeholder="t('metadataBrowser.searchLineagePlaceholder')"
          />
          <Badge variant="outline">
            {{ relationsCountLabel }}
            {{ t("metadataBrowser.lineageRelationsCount") }}
          </Badge>
          <RouterLink
            v-if="guid"
            :to="lineageGraphRoute"
            class="inline-flex items-center gap-1 text-sm text-primary hover:underline"
          >
            <Share2 class="size-4" />
            {{ t("lineageGraph.viewGraph") }}
          </RouterLink>
        </template>
      </div>
    </div>

    <PageState
      :loading="isLoading"
      :error="error"
    >
      <template v-if="displayRelations.length > 0">
        <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
          <!-- Every figure describes the same relation set the table lists: when
               the section is scoped to one field, the unscoped total would
               contradict the two direction counts beside it. -->
          <div class="rounded-md border px-3 py-2">
            <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.totalRelations") }}</div>
            <div class="text-sm font-medium">{{ scopedRelations.length }}</div>
          </div>
          <div class="rounded-md border px-3 py-2">
            <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.upstreamRelations") }}</div>
            <div class="text-sm font-medium">{{ upstreamRelationCount }}</div>
          </div>
          <div class="rounded-md border px-3 py-2">
            <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.downstreamRelations") }}</div>
            <div class="text-sm font-medium">{{ downstreamRelationCount }}</div>
          </div>
          <div class="rounded-md border px-3 py-2">
            <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.relatedObjects") }}</div>
            <div class="text-sm font-medium">{{ relatedObjectCount }}</div>
          </div>
        </div>

        <Table v-if="filteredRelations.length > 0">
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("metadataBrowser.direction") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.currentColumn") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.relatedObject") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.relatedColumn") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.relationType") }}</TableHead>
            <TableHead>{{ t("lineageGraph.originLabel") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.expression") }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="relation in filteredRelations"
            :key="relation.key"
          >
            <TableCell>
              <Badge
                :variant="relation.directionVariant"
                class="whitespace-nowrap"
              >
                {{ relation.directionLabel }}
              </Badge>
            </TableCell>
            <TableCell class="font-medium">{{ relation.currentColumn }}</TableCell>
            <TableCell class="max-w-md text-muted-foreground">
              <RouterLink
                v-if="relation.relatedRoute"
                :to="relation.relatedRoute"
                class="text-primary hover:underline"
                :title="relation.relatedGuid"
              >
                <span class="block truncate">{{ relation.relatedObject }}</span>
              </RouterLink>
              <span v-else class="block truncate" :title="relation.relatedGuid">
                {{ relation.relatedObject }}
              </span>
              <!-- The scope and path, spelled out: two objects in different
                   instances can share a database, schema and table name, and the
                   name alone would render them identically. -->
              <span
                v-if="relation.relatedPath"
                class="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground"
                :title="relation.relatedPath"
              >
                <span
                  class="size-2 shrink-0 rounded-full"
                  :style="{ backgroundColor: relation.relatedScopeColor }"
                />
                <span class="truncate">{{ relation.relatedPath }}</span>
              </span>
            </TableCell>
            <TableCell class="text-muted-foreground">{{ relation.relatedColumn }}</TableCell>
            <TableCell>
              <Badge :variant="relation.relationTypeVariant">
                {{ relation.relationTypeLabel }}
              </Badge>
            </TableCell>
            <TableCell>
              <Badge
                variant="outline"
                class="whitespace-nowrap font-normal"
                :style="{ borderColor: relation.originColor, color: relation.originColor }"
                :title="t(relation.originHintKey)"
              >
                {{ t(relation.originLabelKey) }}
              </Badge>
              <div
                v-if="relation.originSource"
                class="mt-0.5 max-w-64 truncate text-xs text-muted-foreground"
                :title="relation.originDetail"
              >
                {{ relation.originSource }}
              </div>
            </TableCell>
            <TableCell class="max-w-xl text-muted-foreground">
              <LineageTransformationCell
                :transformations="relation.transformations"
                :item-name="relation.currentColumn"
                :dialog-title="t('metadataBrowser.transformationDetails')"
              />
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>

        <div
          v-else
          class="text-sm text-muted-foreground"
        >
          {{ t("metadataBrowser.noLineageRelations") }}
        </div>
      </template>

      <div
        v-else
        class="flex flex-wrap items-center gap-2 text-sm text-muted-foreground"
      >
        <span>{{ t("metadataBrowser.noLineageRelations") }}</span>
        <RouterLink
          v-if="guid"
          :to="lineageGraphRoute"
          class="inline-flex items-center gap-1 text-primary hover:underline"
        >
          {{ t("lineageGraph.viewGraph") }}
          <ArrowRight class="size-3.5" />
        </RouterLink>
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import { ArrowRight, Share2 } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { LocationQueryRaw, RouteLocationRaw } from "vue-router";
import { RouterLink } from "vue-router";
import { getLineage } from "@/api/lineage";
import PageState from "@/components/common/PageState.vue";
import LineageTransformationCell from "@/components/metadata/LineageTransformationCell.vue";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorMessage } from "@/composables/useErrorHandler";
import {
  originColor,
  originLabelKey,
  type RelationOrigin,
  relationOrigin,
} from "@/lib/lineageOrigin";
import { openlineageJobName, openlineageRunLabel } from "@/lib/openlineageRun";
import { relationTypeKey } from "@/lib/relationType";
import { useInstanceStore } from "@/store/modules/instance";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  type ExternalDatasetInfo,
  type LineageRelation,
  RelationType,
  type Transformation,
} from "@/types/proto-es/v1/lineage_service_pb";
import { guidToRouteParams } from "@/utils/guid";
import {
  buildLineageScopeColors,
  lineageAssetView,
  scopeColor,
} from "@/utils/lineageAsset";

type DisplayRelation = {
  currentColumn: string;
  directionLabel: string;
  directionVariant: "outline" | "warning";
  key: string;
  relatedColumn: string;
  relatedGuid: string;
  relatedRoute: RouteLocationRaw | null;
  relatedObject: string;
  /** `scope · qualifier`, the second line that places the object; may be empty. */
  relatedPath: string;
  relatedScopeColor: string;
  relationTypeLabel: string;
  relationTypeVariant: "secondary" | "success";
  /** Which writer stored the relation: the SQL analyzer, or OpenLineage. */
  originLabelKey: string;
  originHintKey: string;
  originColor: string;
  /** The object whose SQL definition was analyzed, or the job that reported it. */
  originSource: string;
  /** The same writer named in full, for the tooltip: the analyzed object's path,
   * or the job with its run id. */
  originDetail: string;
  searchText: string;
  transformations: Transformation[];
};

const props = withDefaults(
  defineProps<{
    guid: string;
    metaType: MetaType;
    focusColumn?: string;
    graphQuery?: LocationQueryRaw;
    title?: string;
  }>(),
  {
    focusColumn: "",
    graphQuery: undefined,
    title: "",
  }
);

const { t } = useI18n();
const { formatError } = useErrorMessage();
const instanceStore = useInstanceStore();

/** The workspace instances, keyed by the resource id a GUID starts with. */
const scopeTitles = computed<Map<string, string>>(() => {
  const titles = new Map<string, string>();
  for (const instance of instanceStore.instances) {
    const id = instance.name.split("/").pop() ?? instance.name;
    titles.set(id, instance.title || id);
  }
  return titles;
});

/** The accent of every scope this table can name, shared with the graph. */
const scopeColorMap = computed(() =>
  buildLineageScopeColors({
    instanceIds: scopeTitles.value.keys(),
    externalDatasetGuids: externalDatasetMap.value.keys(),
    externalOf: (guid) => externalDatasetMap.value.get(guid),
  })
);

const lineageGraphRoute = computed(() => {
  const query: LocationQueryRaw = {
    ...(props.graphQuery ?? {}),
    metaType: String(props.metaType),
  };

  return {
    name: "LineageGraph",
    params: { guid: guidToRouteParams(props.guid) },
    query,
  };
});

const isLoading = ref(false);
const error = ref<string | null>(null);
const upstreamRelations = ref<LineageRelation[]>([]);
const downstreamRelations = ref<LineageRelation[]>([]);
const externalDatasets = ref<ExternalDatasetInfo[]>([]);
const search = ref("");

const externalDatasetMap = computed(() => {
  return new Map(
    externalDatasets.value
      .filter((dataset) => dataset.guid)
      .map((dataset) => [dataset.guid, dataset])
  );
});

const displayRelations = computed<DisplayRelation[]>(() => {
  const sourceRows = upstreamRelations.value.map((relation) =>
    buildDisplayRelation({
      relation,
      directionLabel: t("metadataBrowser.upstream"),
      directionVariant: "warning",
      currentColumn: relation.targetColumn,
      relatedGuid: relation.sourceGuid,
      relatedColumn: relation.sourceColumn,
      relatedMetaType: relation.sourceType,
    })
  );
  const targetRows = downstreamRelations.value.map((relation) =>
    buildDisplayRelation({
      relation,
      directionLabel: t("metadataBrowser.downstream"),
      directionVariant: "outline",
      currentColumn: relation.sourceColumn,
      relatedGuid: relation.targetGuid,
      relatedColumn: relation.targetColumn,
      relatedMetaType: relation.targetType,
    })
  );

  return [...sourceRows, ...targetRows].sort((left, right) => {
    const directionCompare = left.directionLabel.localeCompare(
      right.directionLabel
    );
    if (directionCompare !== 0) return directionCompare;

    const columnCompare = left.currentColumn.localeCompare(right.currentColumn);
    if (columnCompare !== 0) return columnCompare;

    const objectCompare = left.relatedGuid.localeCompare(right.relatedGuid);
    if (objectCompare !== 0) return objectCompare;

    return left.relatedColumn.localeCompare(right.relatedColumn);
  });
});

const scopedRelations = computed(() => {
  const focusColumn = props.focusColumn.trim();
  if (!focusColumn) {
    return displayRelations.value;
  }

  return displayRelations.value.filter(
    (relation) => relation.currentColumn === focusColumn
  );
});

const filteredRelations = computed(() => {
  const query = search.value.trim().toLowerCase();
  if (!query) return scopedRelations.value;
  return scopedRelations.value.filter((relation) =>
    relation.searchText.includes(query)
  );
});

/**
 * How many relations the table lists. The `shown / total` form only appears while
 * a search hides some of them: with nothing filtered out it read as a fraction of
 * itself, which said nothing.
 */
const relationsCountLabel = computed(() => {
  if (filteredRelations.value.length === scopedRelations.value.length) {
    return String(scopedRelations.value.length);
  }
  return `${filteredRelations.value.length} / ${scopedRelations.value.length}`;
});

const upstreamRelationCount = computed(() => {
  const focusColumn = props.focusColumn.trim();
  if (!focusColumn) {
    return upstreamRelations.value.length;
  }

  return upstreamRelations.value.filter(
    (relation) => relation.targetColumn === focusColumn
  ).length;
});

const downstreamRelationCount = computed(() => {
  const focusColumn = props.focusColumn.trim();
  if (!focusColumn) {
    return downstreamRelations.value.length;
  }

  return downstreamRelations.value.filter(
    (relation) => relation.sourceColumn === focusColumn
  ).length;
});

const relatedObjectCount = computed(() => {
  return new Set(scopedRelations.value.map((relation) => relation.relatedGuid))
    .size;
});

watch(
  () => props.guid,
  async (guid) => {
    if (!guid) {
      upstreamRelations.value = [];
      downstreamRelations.value = [];
      externalDatasets.value = [];
      error.value = null;
      return;
    }

    isLoading.value = true;
    error.value = null;

    try {
      const response = await getLineage({
        guid,
        metaType: props.metaType,
      });
      upstreamRelations.value = response.relationsSource;
      downstreamRelations.value = response.relationsTarget;
      externalDatasets.value = response.externalDatasets;
    } catch (e) {
      upstreamRelations.value = [];
      downstreamRelations.value = [];
      externalDatasets.value = [];
      error.value = formatError(e, "metadataBrowser.lineageFetchError");
    } finally {
      isLoading.value = false;
    }
  },
  { immediate: true }
);

function buildDisplayRelation(options: {
  relation: LineageRelation;
  directionLabel: string;
  directionVariant: "outline" | "warning";
  currentColumn: string;
  relatedGuid: string;
  relatedColumn: string;
  relatedMetaType: MetaType;
}): DisplayRelation {
  const relationTypeLabelKey = relationTypeKey(options.relation.relationType);
  const relationTypeLabel = relationTypeLabelKey
    ? t(relationTypeLabelKey)
    : String(options.relation.relationType);

  const externalDataset = externalDatasetMap.value.get(options.relatedGuid);
  const relatedView = assetViewFor(options.relatedGuid, externalDataset);
  const origin: RelationOrigin = relationOrigin(options.relation);

  return {
    currentColumn: options.currentColumn || "-",
    directionLabel: options.directionLabel,
    directionVariant: options.directionVariant,
    key: options.relation.id
      ? options.relation.id.toString()
      : buildRelationKey(options.relation, options.directionLabel),
    relatedColumn: options.relatedColumn || "-",
    relatedGuid: options.relatedGuid,
    relatedRoute: buildMetadataRoute(
      options.relatedGuid,
      options.relatedMetaType,
      externalDataset
    ),
    relatedObject: relatedView.name,
    relatedPath: [relatedView.scopeLabel, relatedView.qualifier]
      .filter(Boolean)
      .join(" · "),
    relatedScopeColor: scopeColor(scopeColorMap.value, relatedView.scopeKey),
    relationTypeLabel,
    relationTypeVariant:
      options.relation.relationType === RelationType.DIRECT
        ? "success"
        : "secondary",
    originLabelKey: originLabelKey(origin),
    originHintKey: originHintKey(origin),
    originColor: originColor(origin),
    originSource: relationOriginSource(options.relation, origin),
    originDetail: relationOriginDetail(options.relation, origin),
    searchText: [
      options.currentColumn,
      options.relatedColumn,
      options.relatedGuid,
      relatedView.fullLabel,
      relationOriginSource(options.relation, origin),
      // The full run label, so a search for a run id still finds its row.
      relationOriginDetail(options.relation, origin),
      transformationText(options.relation.transformations),
      options.directionLabel,
    ]
      .join(" ")
      .toLowerCase(),
    transformations: options.relation.transformations,
  };
}

/**
 * Which writer produced a relation: the object whose SQL definition the analyzer
 * read, or the OpenLineage run that reported it. The relation's own meta GUID is
 * that writer in both cases.
 *
 * An OpenLineage row names the job only, because a cell cannot hold both the job
 * and the run id; the run is the row's tooltip.
 */
function relationOriginSource(
  relation: LineageRelation,
  origin: RelationOrigin
): string {
  if (!relation.metaGuid) {
    return "";
  }
  if (origin === "openlineage") {
    return openlineageJobName(relation.metaGuid);
  }
  return assetViewFor(relation.metaGuid).name;
}

/** The writer in full: the job with its run id, or the analyzed object's path. */
function relationOriginDetail(
  relation: LineageRelation,
  origin: RelationOrigin
): string {
  if (!relation.metaGuid) {
    return "";
  }
  if (origin === "openlineage") {
    return openlineageRunLabel(relation.metaGuid);
  }
  return assetViewFor(relation.metaGuid).fullLabel;
}

function originHintKey(origin: RelationOrigin): string {
  return origin === "openlineage"
    ? "lineageGraph.originOpenlineageHint"
    : "lineageGraph.originSqlHint";
}

function assetViewFor(guid: string, external?: ExternalDatasetInfo) {
  return lineageAssetView(guid, scopeTitles.value, external);
}

// transformationText flattens the structured transformation steps into one
// string for the search index and the relation key.
function transformationText(
  transformations: Transformation[] | undefined
): string {
  return (transformations ?? [])
    .map((item) =>
      [
        item.operation,
        item.functionName,
        item.opType,
        item.expression,
        item.condition,
      ]
        .filter(Boolean)
        .join(" ")
    )
    .join(" ");
}

function buildRelationKey(
  relation: LineageRelation,
  directionLabel: string
): string {
  return [
    directionLabel,
    relation.targetGuid,
    relation.targetColumn,
    relation.sourceGuid,
    relation.sourceColumn,
    relation.relationType,
    transformationText(relation.transformations),
  ].join(":");
}

function buildMetadataRoute(
  guid: string,
  metaType: MetaType,
  externalDataset?: ExternalDatasetInfo
): RouteLocationRaw | null {
  if (!guid) return null;

  const query: Record<string, string> = {};
  if (externalDataset) {
    if (externalDataset.namespace) {
      query.externalNamespace = externalDataset.namespace;
    }
    if (externalDataset.name) {
      query.externalName = externalDataset.name;
    }
    if (externalDataset.datasetType) {
      query.externalDatasetType = externalDataset.datasetType;
    }
  } else if (metaType) {
    query.metaType = String(metaType);
  }

  return {
    name: "MetadataDetail",
    params: { guid: guidToRouteParams(guid) },
    query,
  };
}

// The scope labels and accents come from the shared instance list. A cold cache
// still renders the rows; it relabels them once the titles arrive.
onMounted(() => {
  // A failed list leaves the labels as instance ids rather than breaking the page.
  instanceStore.ensureLoaded().catch(() => undefined);
});
</script>