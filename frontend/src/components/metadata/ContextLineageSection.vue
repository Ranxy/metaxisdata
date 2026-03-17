<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-3">
      <div class="text-sm font-medium">{{ t("metadataBrowser.lineageAnalysis") }}</div>
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
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.directRelations") }}</div>
          <div class="text-sm font-medium">{{ directRelationCount }}</div>
        </div>
        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.indirectRelations") }}</div>
          <div class="text-sm font-medium">{{ indirectRelationCount }}</div>
        </div>
        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.upstreamObjects") }}</div>
          <div class="text-sm font-medium">{{ upstreamObjectCount }}</div>
        </div>
      </div>

      <Table v-if="filteredRelations.length > 0">
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("metadataBrowser.targetColumn") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.sourceObject") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.sourceColumn") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.relationType") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.expression") }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="relation in filteredRelations"
            :key="relation.key"
          >
            <TableCell class="font-medium">{{ relation.targetColumn }}</TableCell>
            <TableCell
              class="max-w-md text-muted-foreground truncate"
              :title="relation.sourceGuid"
            >
              {{ relation.sourceObject }}
            </TableCell>
            <TableCell class="text-muted-foreground">{{ relation.sourceColumn }}</TableCell>
            <TableCell>
              <Badge :variant="relation.relationTypeVariant">
                {{ relation.relationTypeLabel }}
              </Badge>
            </TableCell>
            <TableCell class="max-w-xl text-muted-foreground">
              <ExpandableText
                :text="relation.transformation"
                :item-name="relation.targetColumn"
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
import { getLineageForContext } from "@/api/lineage";
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
  key: string;
  relationTypeLabel: string;
  relationTypeVariant: "secondary" | "success";
  searchText: string;
  sourceColumn: string;
  sourceGuid: string;
  sourceObject: string;
  targetColumn: string;
  transformation: string;
};

const props = defineProps<{
  guid: string;
  metaType: MetaType;
}>();

const { t } = useI18n();

const isLoading = ref(false);
const error = ref<string | null>(null);
const relations = ref<LineageRelation[]>([]);
const search = ref("");

const displayRelations = computed<DisplayRelation[]>(() => {
  return [...relations.value]
    .sort((left, right) => {
      const targetCompare = left.targetColumn.localeCompare(right.targetColumn);
      if (targetCompare !== 0) return targetCompare;

      const sourceGuidCompare = left.sourceGuid.localeCompare(right.sourceGuid);
      if (sourceGuidCompare !== 0) return sourceGuidCompare;

      return left.sourceColumn.localeCompare(right.sourceColumn);
    })
    .map((relation) => {
      const relationTypeLabel =
        relation.relationType === RelationType.DIRECT
          ? t("metadataBrowser.relationDirect")
          : relation.relationType === RelationType.INDIRECT
            ? t("metadataBrowser.relationIndirect")
            : String(relation.relationType);

      return {
        key: relation.id ? relation.id.toString() : buildRelationKey(relation),
        relationTypeLabel,
        relationTypeVariant:
          relation.relationType === RelationType.DIRECT
            ? "success"
            : "secondary",
        searchText: [
          relation.targetColumn,
          relation.sourceColumn,
          relation.sourceGuid,
          relation.transformation,
          formatGuidForDisplay(relation.sourceGuid),
        ]
          .join(" ")
          .toLowerCase(),
        sourceColumn: relation.sourceColumn || "-",
        sourceGuid: relation.sourceGuid,
        sourceObject: formatGuidForDisplay(relation.sourceGuid),
        targetColumn: relation.targetColumn || "-",
        transformation: relation.transformation,
      };
    });
});

const filteredRelations = computed(() => {
  const query = search.value.trim().toLowerCase();
  if (!query) return displayRelations.value;
  return displayRelations.value.filter((relation) =>
    relation.searchText.includes(query)
  );
});

const directRelationCount = computed(() => {
  return relations.value.filter(
    (relation) => relation.relationType === RelationType.DIRECT
  ).length;
});

const indirectRelationCount = computed(() => {
  return relations.value.filter(
    (relation) => relation.relationType === RelationType.INDIRECT
  ).length;
});

const upstreamObjectCount = computed(() => {
  return new Set(
    relations.value.map((relation) => relation.sourceGuid).filter(Boolean)
  ).size;
});

watch(
  [() => props.guid, () => props.metaType],
  async ([guid, metaType]) => {
    if (!guid) {
      relations.value = [];
      error.value = null;
      return;
    }

    isLoading.value = true;
    error.value = null;

    try {
      const response = await getLineageForContext({ guid, metaType });
      relations.value = response.relations;
    } catch (e) {
      relations.value = [];
      const message = extractErrorMessage(e);
      error.value = message || t("metadataBrowser.lineageFetchError");
    } finally {
      isLoading.value = false;
    }
  },
  { immediate: true }
);

function buildRelationKey(relation: LineageRelation): string {
  return [
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