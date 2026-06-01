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
          <div v-if="isEvidenceLoading" class="flex min-h-24 items-center justify-center">
            <AppLoading />
          </div>

          <div
            v-else-if="evidenceError"
            class="rounded-md border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
          >
            {{ evidenceError }}
          </div>

          <template v-else>
            <div class="grid gap-3">
              <div class="rounded-md border p-3">
                <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.relatedObjects") }}</div>
                <div class="mt-1 text-sm font-medium">{{ relatedObjectCount }}</div>
              </div>

              <div class="rounded-md border p-3">
                <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.upstreamRelations") }}</div>
                <div class="mt-1 text-sm font-medium">{{ upstreamRelationCount }}</div>
              </div>

              <div class="rounded-md border p-3">
                <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.downstreamRelations") }}</div>
                <div class="mt-1 text-sm font-medium">{{ downstreamRelationCount }}</div>
              </div>
            </div>

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

            <div v-if="selectedColumn" class="space-y-3">
              <div v-if="scopedUpstreamRelations.length > 0">
                <h3 class="text-sm font-semibold">{{ t("lineageGraph.upstreamRelations") }}</h3>
                <div class="mt-2 space-y-2">
                  <div
                    v-for="rel in scopedUpstreamRelations"
                    :key="`up-${rel.sourceGuid}-${rel.sourceColumn}`"
                    class="rounded-md border p-2 text-xs"
                  >
                    <div class="font-medium">{{ rel.sourceColumn || "-" }}</div>
                    <div class="mt-1 break-all text-muted-foreground">{{ formatGuidLabel(rel.sourceGuid) }}</div>
                    <div class="mt-1 flex flex-wrap gap-1">
                      <span v-if="rel.relationType" class="rounded bg-muted px-1.5 py-0.5 text-[10px]">
                        {{ formatRelationType(rel.relationType) }}
                      </span>
                      <span v-if="rel.transformation" class="rounded bg-muted px-1.5 py-0.5 text-[10px] font-mono">
                        {{ rel.transformation }}
                      </span>
                    </div>
                  </div>
                </div>
              </div>

              <div v-if="scopedDownstreamRelations.length > 0">
                <h3 class="text-sm font-semibold">{{ t("lineageGraph.downstreamRelations") }}</h3>
                <div class="mt-2 space-y-2">
                  <div
                    v-for="rel in scopedDownstreamRelations"
                    :key="`down-${rel.targetGuid}-${rel.targetColumn}`"
                    class="rounded-md border p-2 text-xs"
                  >
                    <div class="font-medium">{{ rel.targetColumn || "-" }}</div>
                    <div class="mt-1 break-all text-muted-foreground">{{ formatGuidLabel(rel.targetGuid) }}</div>
                    <div class="mt-1 flex flex-wrap gap-1">
                      <span v-if="rel.relationType" class="rounded bg-muted px-1.5 py-0.5 text-[10px]">
                        {{ formatRelationType(rel.relationType) }}
                      </span>
                      <span v-if="rel.transformation" class="rounded bg-muted px-1.5 py-0.5 text-[10px] font-mono">
                        {{ rel.transformation }}
                      </span>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <div class="space-y-3">
              <div>
                <h3 class="text-sm font-semibold">{{ t("lineageGraph.relatedRuns") }}</h3>
                <p class="text-sm text-muted-foreground">
                  {{
                    selectedColumn
                      ? t("lineageGraph.relatedRunsFiltered")
                      : t("lineageGraph.relatedRunsDescription")
                  }}
                </p>
              </div>

              <div
                v-if="relatedRuns.length === 0"
                class="rounded-md border border-dashed p-4 text-sm text-muted-foreground"
              >
                {{ t("lineageGraph.noRelatedRuns") }}
              </div>

              <div v-else class="space-y-2">
                <Button
                  v-for="run in relatedRuns"
                  :key="run.guid"
                  variant="outline"
                  class="h-auto w-full justify-start px-3 py-3 text-left"
                  @click="openRunDetail(run.guid)"
                >
                  <div class="space-y-1">
                    <div class="font-medium">{{ run.label }}</div>
                    <div class="text-xs text-muted-foreground">
                      {{ run.updatedAtLabel }}
                    </div>
                  </div>
                </Button>
              </div>
            </div>

            <Button variant="outline" class="w-full" @click="openTableLineage">
              {{ t("lineageGraph.viewGraph") }}
            </Button>
          </template>
        </CardContent>
      </Card>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { getLineage } from "@/api/lineage";
import AppLoading from "@/components/common/AppLoading.vue";
import TableLineageSection from "@/components/metadata/TableLineageSection.vue";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { LineageRelation } from "@/types/proto-es/v1/lineage_service_pb";
import { RelationType } from "@/types/proto-es/v1/lineage_service_pb";
import { extractErrorMessage } from "@/utils/error";

const OPENLINEAGE_META_TYPE = 100;

type RunSummary = {
  guid: string;
  label: string;
  updatedAt?: Timestamp;
  updatedAtLabel: string;
};

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const isEvidenceLoading = ref(false);
const evidenceError = ref("");
const upstreamRelations = ref<LineageRelation[]>([]);
const downstreamRelations = ref<LineageRelation[]>([]);

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

const scopedUpstreamRelations = computed(() => {
  if (!selectedColumn.value) {
    return upstreamRelations.value;
  }

  return upstreamRelations.value.filter(
    (relation) => relation.targetColumn === selectedColumn.value
  );
});

const scopedDownstreamRelations = computed(() => {
  if (!selectedColumn.value) {
    return downstreamRelations.value;
  }

  return downstreamRelations.value.filter(
    (relation) => relation.sourceColumn === selectedColumn.value
  );
});

const upstreamRelationCount = computed(
  () => scopedUpstreamRelations.value.length
);

const downstreamRelationCount = computed(
  () => scopedDownstreamRelations.value.length
);

const relatedObjectCount = computed(() => {
  return new Set(
    [
      ...scopedUpstreamRelations.value.map((relation) => relation.sourceGuid),
      ...scopedDownstreamRelations.value.map((relation) => relation.targetGuid),
    ].filter(Boolean)
  ).size;
});

const relatedRuns = computed<RunSummary[]>(() => {
  const runs = new Map<string, RunSummary>();

  const addRelation = (relation: LineageRelation) => {
    if (
      Number(relation.metaType) !== OPENLINEAGE_META_TYPE ||
      !relation.metaGuid
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

  for (const relation of scopedUpstreamRelations.value) {
    addRelation(relation);
  }
  for (const relation of scopedDownstreamRelations.value) {
    addRelation(relation);
  }

  return Array.from(runs.values()).sort((left, right) =>
    compareTimestamps(right.updatedAt, left.updatedAt)
  );
});

watch(
  () => ({
    guid: currentGuid.value,
    metaType: currentMetaType.value,
  }),
  async ({ guid, metaType }) => {
    if (!guid) {
      upstreamRelations.value = [];
      downstreamRelations.value = [];
      evidenceError.value = "";
      return;
    }

    isEvidenceLoading.value = true;
    evidenceError.value = "";
    try {
      const response = await getLineage({ guid, metaType });
      upstreamRelations.value = response.relationsSource;
      downstreamRelations.value = response.relationsTarget;
    } catch (error) {
      upstreamRelations.value = [];
      downstreamRelations.value = [];
      evidenceError.value =
        extractErrorMessage(error) || t("metadataBrowser.lineageFetchError");
    } finally {
      isEvidenceLoading.value = false;
    }
  },
  { immediate: true }
);

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

function openRunDetail(guid: string) {
  router.push({
    name: "OpenLineageRunDetail",
    params: { guid },
    query: { from: route.fullPath },
  });
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

function toGuidPath(guid: string): string {
  return guid
    .split(";")
    .map((segment) => (segment === "" ? "~" : encodeURIComponent(segment)))
    .join("/");
}

function formatGuidLabel(guid: string): string {
  const segments = guid.split(";").filter(Boolean);
  return segments[segments.length - 1] || guid;
}

function formatRelationType(relationType: number): string {
  switch (relationType) {
    case RelationType.DIRECT:
      return "DIRECT";
    case RelationType.INDIRECT:
      return "INDIRECT";
    default:
      return "";
  }
}
</script>
