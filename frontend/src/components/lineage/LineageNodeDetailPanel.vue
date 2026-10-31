<template>
  <CardContent
    v-if="summary"
    class="flex h-full flex-col p-0"
  >
    <div class="flex items-start justify-between gap-3 border-b px-5 py-4">
      <div class="min-w-0 space-y-1">
        <div class="text-xs uppercase tracking-wide text-muted-foreground">
          {{ t("common.details") }}
        </div>
        <h2 class="flex items-center gap-2 text-lg font-semibold leading-tight">
          <span
            class="size-2.5 shrink-0 rounded-full"
            :style="{ backgroundColor: summary.scopeColor }"
          />
          <span class="truncate">{{ summary.label }}</span>
        </h2>
        <p
          class="truncate text-xs text-muted-foreground"
          :title="summary.guid"
        >
          {{ summary.fullLabel }}
        </p>
      </div>
      <Button
        variant="ghost"
        size="sm"
        @click="emit('close')"
      >
        {{ t("lineageGraph.closeDetail") }}
      </Button>
    </div>

    <div class="flex-1 space-y-6 overflow-y-auto px-5 py-4">
      <section class="space-y-3">
        <div class="flex flex-wrap gap-2">
          <Badge :variant="summary.isExternal ? 'outline' : 'default'">
            {{
              summary.isExternal
                ? t("openlineage.external")
                : t("openlineage.internal")
            }}
          </Badge>
          <Badge variant="outline">
            {{ summary.metaTypeLabel }}
          </Badge>
        </div>

        <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
          <div class="rounded-md border p-3">
            <div class="text-xs text-muted-foreground">
              {{ t("lineageGraph.scope") }}
            </div>
            <div class="mt-1 flex items-center gap-2 text-sm font-medium">
              <span
                class="size-2.5 shrink-0 rounded-full"
                :style="{ backgroundColor: summary.scopeColor }"
              />
              <span class="truncate">{{ summary.scopeLabel || "-" }}</span>
            </div>
          </div>
          <div class="rounded-md border p-3">
            <div class="text-xs text-muted-foreground">
              {{ t("lineageGraph.qualifiedName") }}
            </div>
            <div class="mt-1 break-all text-sm font-medium">
              {{ summary.fullLabel }}
            </div>
          </div>
          <div class="rounded-md border p-3">
            <div class="text-xs text-muted-foreground">
              {{ t("lineageGraph.fieldCount") }}
            </div>
            <div class="mt-1 text-sm font-medium">
              {{ summary.columns.length }}
            </div>
          </div>
          <div class="rounded-md border p-3">
            <div class="text-xs text-muted-foreground">
              {{ t("lineageGraph.upstreamObjects") }}
            </div>
            <div class="mt-1 text-sm font-medium">
              {{ summary.upstreamCount }}
            </div>
          </div>
          <div class="rounded-md border p-3">
            <div class="text-xs text-muted-foreground">
              {{ t("lineageGraph.downstreamObjects") }}
            </div>
            <div class="mt-1 text-sm font-medium">
              {{ summary.downstreamCount }}
            </div>
          </div>
          <div
            v-if="summary.isExternal"
            class="rounded-md border p-3 sm:col-span-2 xl:col-span-1"
          >
            <div class="text-xs text-muted-foreground">
              {{ t("openlineageSettings.namespace") }}
            </div>
            <div class="mt-1 break-all text-sm font-medium">
              {{ summary.externalNamespace || "-" }}
            </div>
          </div>
          <div
            v-if="summary.isExternal"
            class="rounded-md border p-3 sm:col-span-2 xl:col-span-1"
          >
            <div class="text-xs text-muted-foreground">
              {{ t("openlineage.datasetType") }}
            </div>
            <div class="mt-1 text-sm font-medium">
              {{ summary.externalDatasetType || "-" }}
            </div>
          </div>
          <div
            v-if="columnContext"
            class="rounded-md border p-3 sm:col-span-2 xl:col-span-1"
          >
            <div class="text-xs text-muted-foreground">
              {{ t("lineageGraph.selectedField") }}
            </div>
            <div class="mt-1 text-sm font-medium">{{ columnContext }}</div>
          </div>
        </div>

        <!-- How far the field runs across the canvas. The trail is walked over
             the drawn graph, so these counts are about what is here, not about
             the whole stored graph. -->
        <div
          v-if="fieldTrail"
          class="space-y-2"
        >
          <div class="text-xs uppercase tracking-wide text-muted-foreground">
            {{ t("lineageGraph.fieldTrail") }}
          </div>
          <p class="text-xs text-muted-foreground">
            {{ t("lineageGraph.fieldTrailHint") }}
          </p>
          <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
            <div
              v-for="side in trailSides"
              :key="side.key"
              class="rounded-md border p-3"
            >
              <div class="text-xs text-muted-foreground">{{ side.label }}</div>
              <div class="mt-1 text-sm font-medium">
                {{
                  t("lineageGraph.legendObjectCount", {
                    count: side.data.nodeIds.size,
                  })
                }}
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
            v-if="fieldTrail.truncated"
            class="text-xs text-muted-foreground"
          >
            {{ t("lineageGraph.fieldTrailTruncated") }}
          </p>
          <Button
            variant="outline"
            size="sm"
            :disabled="!dimsOffTrail"
            @click="emit('fit-trail')"
          >
            {{ t("lineageGraph.fieldTrailFit") }}
          </Button>
        </div>

        <!-- Which writers reached this object, before the graph's own origin
             filter: the number is about the stored graph, not about what happens
             to be drawn. -->
        <div
          v-if="origins.length > 0"
          class="space-y-2"
        >
          <div class="text-xs uppercase tracking-wide text-muted-foreground">
            {{ t("lineageGraph.originLabel") }}
          </div>
          <div class="flex flex-wrap gap-2">
            <span
              v-for="item in origins"
              :key="item.origin"
              class="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs"
            >
              <OriginSwatch
                :origin="item.origin"
                :width="18"
              />
              {{ t(originLabelKey(item.origin)) }}
              <span class="tabular-nums text-muted-foreground">
                {{ item.count }}
                {{ t("metadataBrowser.lineageRelationsCount") }}
              </span>
            </span>
          </div>
        </div>

        <div class="flex flex-wrap gap-2">
          <!-- The same overlay the metadata pages show, so a reader never has to
               leave the graph to read an object's definition. A dataset outside
               every instance has no definition to read. -->
          <Button
            variant="outline"
            size="sm"
            :disabled="summary.isExternal"
            @click="emit('open-schema', summary.guid)"
          >
            <Code class="mr-1 size-3.5" />
            {{ t("metadataBrowser.viewSchema") }}
          </Button>
          <Button
            variant="outline"
            size="sm"
            @click="emit('refocus')"
          >
            {{ t("lineageGraph.refocusGraph") }}
          </Button>
          <Button
            variant="outline"
            size="sm"
            :disabled="!canOpenColumnLineage"
            @click="emit('open-column-lineage')"
          >
            {{ t("lineageGraph.openColumnLineage") }}
          </Button>
          <Button
            variant="outline"
            size="sm"
            :disabled="summary.isExternal"
            @click="emit('open-metadata')"
          >
            {{ t("openlineage.openMetadata") }}
          </Button>
        </div>
      </section>

      <section class="space-y-3">
        <div>
          <h3 class="text-sm font-semibold">
            {{ t("lineageGraph.relatedRuns") }}
          </h3>
          <p class="text-sm text-muted-foreground">
            {{
              columnContext
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

        <div
          v-else
          class="space-y-2"
        >
          <Button
            v-for="run in relatedRuns"
            :key="run.guid"
            variant="outline"
            class="h-auto w-full justify-start px-3 py-3 text-left"
            @click="emit('open-run', run.guid)"
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
  <CardContent
    v-else
    class="flex h-full items-center justify-center"
  >
    <EmptyState
      :icon="MousePointerClick"
      :title="t('lineageGraph.selectNodeHint')"
    />
  </CardContent>
</template>

<script setup lang="ts">
import { Code, MousePointerClick } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import EmptyState from "@/components/common/EmptyState.vue";
import OriginSwatch from "@/components/lineage/OriginSwatch.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { CardContent } from "@/components/ui/card";
import type { LineageOrigin } from "@/lib/lineageOrigin";
import { originLabelKey } from "@/lib/lineageOrigin";

/** One object on the canvas, as the detail column prints it. */
export interface NodeDetailSummary {
  guid: string;
  label: string;
  fullLabel: string;
  scopeLabel: string;
  scopeColor: string;
  isExternal: boolean;
  metaTypeLabel: string;
  upstreamCount: number;
  downstreamCount: number;
  columns: string[];
  externalNamespace: string;
  externalDatasetType: string;
}

/** Which writers reached the object, and how many relations each contributed. */
export interface NodeDetailOrigin {
  origin: LineageOrigin;
  count: number;
}

export interface FieldTrailSide {
  nodeIds: Set<string>;
  relationCount: number;
}

/** The selected field's trail across the drawn graph. */
export interface FieldTrailSummary {
  upstream: FieldTrailSide;
  downstream: FieldTrailSide;
  truncated: boolean;
}

/** One ingestion run that touched the object, for the related-runs list. */
export interface RelatedRunSummary {
  guid: string;
  label: string;
  updatedAtLabel: string;
}

const props = withDefaults(
  defineProps<{
    summary?: NodeDetailSummary | null;
    origins?: NodeDetailOrigin[];
    fieldTrail?: FieldTrailSummary | null;
    columnContext?: string | null;
    relatedRuns?: RelatedRunSummary[];
    dimsOffTrail?: boolean;
    canOpenColumnLineage?: boolean;
  }>(),
  {
    summary: null,
    origins: () => [],
    fieldTrail: null,
    columnContext: null,
    relatedRuns: () => [],
    dimsOffTrail: false,
    canOpenColumnLineage: false,
  }
);

const emit = defineEmits<{
  close: [];
  refocus: [];
  "fit-trail": [];
  "open-schema": [guid: string];
  "open-column-lineage": [];
  "open-metadata": [];
  "open-run": [guid: string];
}>();

const { t } = useI18n();

const trailSides = computed(() => {
  const trail = props.fieldTrail;
  if (!trail) {
    return [];
  }
  return [
    { key: "up", label: t("metadataBrowser.upstream"), data: trail.upstream },
    {
      key: "down",
      label: t("metadataBrowser.downstream"),
      data: trail.downstream,
    },
  ];
});
</script>
