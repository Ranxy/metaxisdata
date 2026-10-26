<template>
  <Card>
    <CardHeader class="border-b">
      <CardTitle class="text-base">
        {{ t("dataQuality.coverage.title") }}
      </CardTitle>
      <CardDescription>
        {{ t("dataQuality.coverage.description") }}
      </CardDescription>
    </CardHeader>
    <CardContent class="space-y-3 pt-4">
      <!-- A failed column read degrades only this card: the rules around it stay
           usable and the retry re-reads just the column list. -->
      <div
        v-if="loadFailed"
        class="flex flex-wrap items-center gap-2 text-sm"
      >
        <span class="text-destructive">
          {{ t("dataQuality.coverage.loadFailed") }}
        </span>
        <Button
          variant="ghost"
          size="sm"
          class="h-7 px-2"
          @click="loadColumns"
        >
          {{ t("common.retry") }}
        </Button>
      </div>

      <p
        v-else-if="loading"
        class="text-sm text-muted-foreground"
      >
        {{ t("common.loading") }}
      </p>

      <!-- No column metadata to compare against: the description already says
           what the card is for, so there is no second empty message. -->
      <template v-else-if="columnList.length > 0">
        <div class="space-y-1.5">
          <div class="flex flex-wrap items-baseline justify-between gap-2">
            <span class="text-sm">{{ coverageSummary }}</span>
            <span class="text-xs tabular-nums text-muted-foreground">
              {{ coveredColumns.length }}/{{ columnList.length }}
            </span>
          </div>
          <div
            class="h-1.5 overflow-hidden rounded-full bg-muted"
            role="progressbar"
            aria-valuemin="0"
            :aria-valuemax="columnList.length"
            :aria-valuenow="coveredColumns.length"
            :aria-label="coverageSummary"
          >
            <div
              class="h-full rounded-full"
              :style="{
                width: `${coveragePercent}%`,
                backgroundColor: 'hsl(var(--success))',
              }"
            />
          </div>
        </div>

        <div
          v-if="coveredColumns.length > 0"
          class="space-y-1.5"
        >
          <h3 class="text-xs font-medium">
            {{ t("dataQuality.coverage.covered") }}
          </h3>
          <ul class="flex flex-wrap gap-1.5">
            <li
              v-for="column in coveredColumns"
              :key="column.name"
              class="inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs"
            >
              <span class="font-mono">{{ column.name }}</span>
              <Badge
                variant="secondary"
                class="px-1.5 tabular-nums"
              >
                {{ column.count }}
              </Badge>
            </li>
          </ul>
        </div>

        <div class="space-y-1.5">
          <h3 class="text-xs font-medium">
            {{ t("dataQuality.coverage.uncoveredTitle") }}
          </h3>
          <ul
            v-if="uncoveredColumns.length > 0"
            class="flex flex-wrap gap-1.5"
          >
            <li
              v-for="name in visibleUncoveredColumns"
              :key="name"
            >
              <Badge
                variant="outline"
                :title="t('dataQuality.coverage.uncovered')"
              >
                {{ name }}
              </Badge>
            </li>
            <li
              v-if="hiddenUncoveredCount > 0"
              class="inline-flex items-center text-xs text-muted-foreground"
            >
              +{{ hiddenUncoveredCount }}
            </li>
          </ul>
          <p
            v-else
            class="text-xs text-muted-foreground"
          >
            {{ t("dataQuality.coverage.uncoveredEmpty") }}
          </p>
        </div>
      </template>

      <Button
        v-if="canCreate"
        variant="outline"
        size="sm"
        @click="emit('create')"
      >
        {{ t("dataQuality.coverage.addRule") }}
      </Button>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listAllMetadata } from "@/api/database";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import type { QualityRule } from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { TargetColumnDetail } from "@/utils/dataQualityForm";
import { ruleColumn } from "@/utils/dataQualityTarget";

/**
 * Which of the target's columns a rule watches, as a progress line plus the two
 * compact lists. The rules arrive from the page, and the columns now usually do
 * too: the target workspace reads them from the quality API, which also covers
 * views and external tables. The registry fetch stays as the fallback for a
 * caller that has no column list of its own.
 */

const props = defineProps<{
  /** The metadata GUID of the target whose columns the card covers. */
  targetGuid: string;
  /** The target's meta type; the directory is always read as COLUMN children. */
  metaType: MetaType;
  /** The target's rules, as the page has already loaded them. */
  rules: QualityRule[];
  /**
   * The target's columns, when the page already has them. Passing this (even an
   * empty list) takes precedence over the registry read; leaving it undefined
   * keeps the card self-sufficient.
   */
  columns?: TargetColumnDetail[];
  /** Whether the caller may create a rule; only then is the entry point shown. */
  canCreate: boolean;
}>();

const emit = defineEmits<{
  /** The page opens its rule composer for this target. */
  create: [];
}>();

const { t } = useI18n();

/** How many uncovered columns are named before the rest are just counted. */
const UNCOVERED_PREVIEW = 12;

/** The columns the registry read returned; used only when the page passes none. */
const fetchedColumns = ref<TargetColumnDetail[]>([]);
const loading = ref(false);
const loadFailed = ref(false);

/** The columns the card renders: the page's list wins, else the fetched one. */
const columnList = computed(() => props.columns ?? fetchedColumns.value);

// A page that switches target must not paint the previous target's columns while
// the new list loads; the ticket drops whatever the old request returns.
let loadTicket = 0;

async function loadColumns() {
  const ticket = ++loadTicket;
  loading.value = true;
  loadFailed.value = false;
  try {
    const items = await listAllMetadata({
      parentGuid: props.targetGuid,
      metaType: MetaType.COLUMN,
    });
    if (ticket !== loadTicket) {
      return;
    }
    fetchedColumns.value = items.map((item) => {
      const value = item.type.value as { name?: string } | undefined;
      return { name: value?.name ?? item.guid, type: "", nullable: true };
    });
  } catch {
    if (ticket !== loadTicket) {
      return;
    }
    fetchedColumns.value = [];
    loadFailed.value = true;
  } finally {
    if (ticket === loadTicket) {
      loading.value = false;
    }
  }
}

watch(
  () => [props.targetGuid, props.columns] as const,
  () => {
    // The page's list wins: cancel any in-flight read and never request one.
    loadTicket++;
    fetchedColumns.value = [];
    loading.value = false;
    loadFailed.value = false;
    if (props.columns === undefined) {
      void loadColumns();
    }
  },
  { immediate: true }
);

/** How many rules each column carries; dataset-level rules name no column. */
const ruleCountsByColumn = computed(() => {
  const counts = new Map<string, number>();
  for (const rule of props.rules) {
    const column = ruleColumn(rule);
    if (!column) {
      continue;
    }
    counts.set(column, (counts.get(column) ?? 0) + 1);
  }
  return counts;
});

const coveredColumns = computed(() =>
  columnList.value
    .filter((column) => ruleCountsByColumn.value.has(column.name))
    .map((column) => ({
      name: column.name,
      count: ruleCountsByColumn.value.get(column.name) ?? 0,
    }))
);

const uncoveredColumns = computed(() =>
  columnList.value
    .filter((column) => !ruleCountsByColumn.value.has(column.name))
    .map((column) => column.name)
);

const visibleUncoveredColumns = computed(() =>
  uncoveredColumns.value.slice(0, UNCOVERED_PREVIEW)
);

const hiddenUncoveredCount = computed(
  () => uncoveredColumns.value.length - visibleUncoveredColumns.value.length
);

const coverageSummary = computed(() =>
  t("dataQuality.coverage.coveredSummary", {
    covered: coveredColumns.value.length,
    total: columnList.value.length,
  })
);

const coveragePercent = computed(() => {
  if (columnList.value.length === 0) {
    return 0;
  }
  return Math.round(
    (coveredColumns.value.length / columnList.value.length) * 100
  );
});
</script>
