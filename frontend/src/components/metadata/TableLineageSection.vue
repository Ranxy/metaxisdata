<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-3">
      <div class="text-sm font-medium">{{ title }}</div>
      <div class="flex items-center gap-2">
        <Input
          v-model="search"
          class="h-9 w-80 max-w-full"
          :placeholder="t('metadataBrowser.searchLineagePlaceholder')"
        />
        <Badge variant="outline">
          {{ filteredRelations.length }} / {{ displayRelations.length }}
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
              <Badge :variant="relation.directionVariant">
                {{ relation.directionLabel }}
              </Badge>
            </TableCell>
            <TableCell class="font-medium">{{ relation.currentColumn }}</TableCell>
            <TableCell
              class="max-w-md text-muted-foreground truncate"
              :title="relation.relatedGuid"
            >
              {{ relation.relatedObject }}
            </TableCell>
            <TableCell class="text-muted-foreground">{{ relation.relatedColumn }}</TableCell>
            <TableCell>
              <Badge :variant="relation.relationTypeVariant">
                {{ relation.relationTypeLabel }}
              </Badge>
            </TableCell>
            <TableCell class="max-w-xl text-muted-foreground">
              <ExpandableText
                :text="relation.transformation"
                :item-name="relation.currentColumn"
                :dialog-title="t('metadataBrowser.expression')"
                text-class="block max-w-xl truncate"
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
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { getLineage } from "@/api/lineage";
import AppLoading from "@/components/common/AppLoading.vue";
import ExpandableText from "@/components/metadata/ExpandableText.vue";
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
    title?: string;
  }>(),
  {
    title: "",
  }
);

const { t } = useI18n();

const isLoading = ref(false);
const error = ref<string | null>(null);
const upstreamRelations = ref<LineageRelation[]>([]);
const downstreamRelations = ref<LineageRelation[]>([]);
const search = ref("");

const displayRelations = computed<DisplayRelation[]>(() => {
  const sourceRows = upstreamRelations.value.map((relation) =>
    buildDisplayRelation({
      relation,
      directionLabel: t("metadataBrowser.upstream"),
      directionVariant: "warning",
      currentColumn: relation.targetColumn,
      relatedGuid: relation.sourceGuid,
      relatedColumn: relation.sourceColumn,
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

const filteredRelations = computed(() => {
  const query = search.value.trim().toLowerCase();
  if (!query) return displayRelations.value;
  return displayRelations.value.filter((relation) =>
    relation.searchText.includes(query)
  );
});

const upstreamRelationCount = computed(() => upstreamRelations.value.length);

const downstreamRelationCount = computed(
  () => downstreamRelations.value.length
);

const relatedObjectCount = computed(() => {
  return new Set(displayRelations.value.map((relation) => relation.relatedGuid))
    .size;
});

watch(
  () => props.guid,
  async (guid) => {
    if (!guid) {
      upstreamRelations.value = [];
      downstreamRelations.value = [];
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
    } catch (e) {
      upstreamRelations.value = [];
      downstreamRelations.value = [];
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
}): DisplayRelation {
  const relationTypeLabel =
    options.relation.relationType === RelationType.DIRECT
      ? t("metadataBrowser.relationDirect")
      : options.relation.relationType === RelationType.INDIRECT
        ? t("metadataBrowser.relationIndirect")
        : String(options.relation.relationType);

  const relatedObject = formatGuidForDisplay(options.relatedGuid);

  return {
    currentColumn: options.currentColumn || "-",
    directionLabel: options.directionLabel,
    directionVariant: options.directionVariant,
    key: options.relation.id
      ? options.relation.id.toString()
      : buildRelationKey(options.relation, options.directionLabel),
    relatedColumn: options.relatedColumn || "-",
    relatedGuid: options.relatedGuid,
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

function formatGuidForDisplay(guid: string): string {
  if (!guid) return "-";

  const segments = guid.split(";").filter(Boolean);
  if (segments.length === 0) return guid;
  return segments.slice(-3).join(".");
}
</script>