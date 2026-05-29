<template>
  <div class="space-y-6">
    <OpenLineageSectionHeader
      :title="pageTitle"
      :description="pageDescription"
    >
      <template #actions>
        <Button variant="outline" @click="goBack">
          {{ t("lineageGraph.backToMetadata") }}
        </Button>
      </template>
    </OpenLineageSectionHeader>

    <p class="-mt-2 break-all font-mono text-sm text-muted-foreground">
      {{ currentGuid }}
    </p>

    <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,18rem)]">
      <Card>
        <CardContent class="pt-6">
          <TableLineageSection
            :guid="currentGuid"
            :meta-type="currentMetaType"
            :focus-column="selectedColumn"
            :graph-query="graphQuery"
            :title="sectionTitle"
          />
        </CardContent>
      </Card>

      <Card>
        <CardContent class="space-y-4 pt-6">
          <div class="rounded-md border p-3">
            <div class="text-xs text-muted-foreground">{{ t("openlineage.currentAsset") }}</div>
            <div class="mt-1 break-all text-sm font-medium">{{ currentGuid }}</div>
          </div>

          <div class="rounded-md border p-3">
            <div class="text-xs text-muted-foreground">{{ t("openlineage.currentField") }}</div>
            <div class="mt-1 text-sm font-medium">
              {{ selectedColumn || t("openlineage.allFields") }}
            </div>
          </div>

          <Button variant="outline" class="w-full" @click="openTableLineage">
            {{ t("lineageGraph.viewGraph") }}
          </Button>
        </CardContent>
      </Card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import TableLineageSection from "@/components/metadata/TableLineageSection.vue";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const currentGuid = computed(() => {
  const guidParam = route.params.guid;
  if (!guidParam) return "";
  const guidStr = Array.isArray(guidParam) ? guidParam.join("/") : guidParam;
  return guidStr
    .split("/")
    .map((segment) => decodeURIComponent(segment))
    .map((segment) => (segment === "~" ? "" : segment))
    .join(";");
});

const currentMetaType = computed(() => {
  const raw = Array.isArray(route.query.metaType)
    ? route.query.metaType[0]
    : route.query.metaType;
  const value = Number(raw);
  return Number.isFinite(value) ? (value as MetaType) : MetaType.TABLE;
});

const selectedColumn = computed(() => {
  const raw = Array.isArray(route.query.column)
    ? route.query.column[0]
    : route.query.column;
  return typeof raw === "string" ? raw : "";
});

const graphQuery = computed(() => ({
  metaType: String(currentMetaType.value),
  from: route.fullPath,
}));

const pageTitle = computed(() => {
  if (selectedColumn.value) {
    return t("openlineage.columnLineageForField", {
      column: selectedColumn.value,
    });
  }

  return t("openlineage.columnLineage");
});

const pageDescription = computed(() => {
  return selectedColumn.value
    ? t("openlineage.columnLineageDescriptionFocused")
    : t("openlineage.columnLineageDescription");
});

const sectionTitle = computed(() => {
  return selectedColumn.value
    ? t("openlineage.columnLineageFilteredRelations")
    : t("openlineage.columnLineageAllRelations");
});

function openTableLineage() {
  router.push({
    name: "LineageGraph",
    params: { guid: toGuidPath(currentGuid.value) },
    query: {
      metaType: String(currentMetaType.value),
      from: route.fullPath,
    },
  });
}

function goBack() {
  const from = route.query.from;
  if (typeof from === "string" && from.length > 0) {
    router.push(from);
    return;
  }

  openTableLineage();
}

function toGuidPath(guid: string): string {
  return guid
    .split(";")
    .map((segment) => (segment === "" ? "~" : encodeURIComponent(segment)))
    .join("/");
}
</script>
