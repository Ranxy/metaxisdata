<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-3">
      <div class="text-sm font-medium">{{ title }}</div>
      <div class="flex items-center gap-2">
        <RouterLink
          v-if="guid"
          :to="lineageGraphRoute"
          class="inline-flex items-center gap-1 text-sm text-primary hover:underline"
        >
          <Share2 class="size-4" />
          {{ t("lineageGraph.viewGraph") }}
        </RouterLink>
        <Input
          v-model="search"
          class="h-9 w-80 max-w-full"
          :placeholder="t('metadataBrowser.searchLineagePlaceholder')"
        />
        <Badge variant="outline">
          {{ filteredRelations.length }} / {{ scopedRelations.length }}
          {{ t("metadataBrowser.lineageRelationsCount") }}
        </Badge>
      </div>
    </div>

    <div
      v-if="isLoading"
      class="py-8 flex justify-center"
    >
      <AppLoading />
    </div>

    <div
      v-else-if="error"
      class="text-sm text-destructive"
    >
      {{ error }}
    </div>

    <template v-else-if="displayRelations.length > 0">
      <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.totalRelations") }}</div>
          <div class="text-sm font-medium">{{ displayRelations.length }}</div>
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
            <TableCell
              class="max-w-md text-muted-foreground truncate"
              :title="relation.relatedGuid"
            >
              <RouterLink
                v-if="relation.relatedRoute"
                :to="relation.relatedRoute"
                class="text-primary hover:underline"
              >
                {{ relation.relatedObject }}
              </RouterLink>
              <span v-else>{{ relation.relatedObject }}</span>
            </TableCell>
            <TableCell class="text-muted-foreground">{{ relation.relatedColumn }}</TableCell>
            <TableCell>
              <Badge :variant="relation.relationTypeVariant">
                {{ relation.relationTypeLabel }}
              </Badge>
            </TableCell>
            <TableCell class="max-w-xl text-muted-foreground">
              <LineageTransformationCell
                :text="relation.transformation"
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
      class="text-sm text-muted-foreground"
    >
      {{ t("metadataBrowser.noLineageRelations") }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { Share2 } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  type LocationQueryRaw,
  type RouteLocationRaw,
  RouterLink,
} from "vue-router";
import { getLineage } from "@/api/lineage";
import AppLoading from "@/components/common/AppLoading.vue";
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
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  type ExternalDatasetInfo,
  type LineageRelation,
  RelationType,
} from "@/types/proto-es/v1/lineage_service_pb";
import { extractErrorMessage } from "@/utils/error";

type DisplayRelation = {
  currentColumn: string;
  directionLabel: string;
  directionVariant: "outline" | "warning";
  key: string;
  relatedColumn: string;
  relatedGuid: string;
  relatedRoute: RouteLocationRaw | null;
  relatedObject: string;
  relationTypeLabel: string;
  relationTypeVariant: "secondary" | "success";
  searchText: string;
  transformation: string;
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

const lineageGraphRoute = computed(() => {
  const query: LocationQueryRaw = {
    ...(props.graphQuery ?? {}),
    metaType: String(props.metaType),
  };

  const guidPath = props.guid
    .split(";")
    .map((s) => (s === "" ? "~" : encodeURIComponent(s)))
    .join("/");
  return {
    name: "LineageGraph",
    params: { guid: guidPath },
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
      const message = extractErrorMessage(e);
      error.value = message || t("metadataBrowser.lineageFetchError");
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
  const relationTypeLabel =
    options.relation.relationType === RelationType.DIRECT
      ? t("metadataBrowser.relationDirect")
      : options.relation.relationType === RelationType.INDIRECT
        ? t("metadataBrowser.relationIndirect")
        : String(options.relation.relationType);

  const externalDataset = externalDatasetMap.value.get(options.relatedGuid);
  const relatedObject = formatGuidForDisplay(
    options.relatedGuid,
    externalDataset
  );

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
    relatedObject,
    relationTypeLabel,
    relationTypeVariant:
      options.relation.relationType === RelationType.DIRECT
        ? "success"
        : "secondary",
    searchText: [
      options.currentColumn,
      options.relatedColumn,
      options.relatedGuid,
      relatedObject,
      options.relation.transformation,
      options.directionLabel,
    ]
      .join(" ")
      .toLowerCase(),
    transformation: options.relation.transformation,
  };
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
    relation.transformation,
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
    params: { guid: toGuidPath(guid) },
    query,
  };
}

function toGuidPath(guid: string): string {
  return guid
    .split(";")
    .map((segment) => (segment === "" ? "~" : encodeURIComponent(segment)))
    .join("/");
}

function formatGuidForDisplay(
  guid: string,
  externalDataset?: ExternalDatasetInfo
): string {
  if (!guid) return "-";

  if (externalDataset) {
    if (externalDataset.namespace && externalDataset.name) {
      return `${externalDataset.namespace} / ${externalDataset.name}`;
    }
    return externalDataset.name || externalDataset.namespace || guid;
  }

  const segments = guid.split(";").filter(Boolean);
  if (segments.length === 0) return guid;
  return segments.slice(-3).join(".");
}
</script>