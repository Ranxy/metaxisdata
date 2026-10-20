<template>
  <div
    v-if="squares.length > 0"
    role="list"
    :aria-label="label"
    class="flex flex-wrap gap-1"
  >
    <button
      v-for="(result, index) in squares"
      :key="result.name || index"
      type="button"
      role="listitem"
      class="h-9 w-5 shrink-0 rounded-sm transition hover:opacity-80 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:outline-none"
      :class="ringClass(result, index)"
      :style="{ backgroundColor: colorFor(result.status) }"
      :title="squareLabel(result)"
      :aria-label="squareLabel(result)"
      @click="emit('select', result)"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  type QualityRuleResult,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityStatusKey } from "@/utils/dataQuality";
import { formatDateTime } from "@/utils/datetime";

/**
 * One rule's recent execution history as a strip of small squares, oldest on
 * the left and the newest on the right, so the eye reads pass/fail at a glance
 * instead of scanning a table.
 *
 * `results` arrives newest-first, exactly as the API returns it; the strip
 * reverses the visible window for display. The squares are buttons so a result
 * is reachable and clickable by keyboard, and each carries its status and
 * absolute finish time as its accessible name.
 */

const props = withDefaults(
  defineProps<{
    /** The rule's results, newest first, as the API returns them. */
    results: QualityRuleResult[];
    /** How many of the newest results to show. */
    limit?: number;
    /** The strip's localized accessible name; supplied by the page. */
    label: string;
    /** The name of a result to mark as selected, if any. */
    selectedName?: string;
  }>(),
  { limit: 20 }
);

const emit = defineEmits<{
  select: [result: QualityRuleResult];
}>();

const { t, locale } = useI18n();

/** The displayed squares, oldest first. */
const squares = computed(() => props.results.slice(0, props.limit).reverse());

// The theme tokens are HSL triplets, so they are used through `hsl(...)`.
function colorFor(status: QualityStatus): string {
  switch (status) {
    case QualityStatus.PASS:
      return "hsl(var(--success))";
    case QualityStatus.WARN:
      return "hsl(var(--warning))";
    case QualityStatus.FAIL:
      return "hsl(var(--destructive))";
    case QualityStatus.ERROR:
      return "hsl(var(--danger))";
    case QualityStatus.SKIPPED:
      return "hsl(var(--muted-foreground) / 0.35)";
    case QualityStatus.NOT_EVALUATED:
      return "hsl(var(--border))";
    default:
      return "hsl(var(--muted))";
  }
}

/** Status and absolute finish time, for the tooltip and the accessible name. */
function squareLabel(result: QualityRuleResult): string {
  return `${t(qualityStatusKey(result.status))} · ${formatDateTime(result.finishTime, locale.value)}`;
}

/** The selected square wins over the newest-result emphasis. */
function ringClass(result: QualityRuleResult, index: number): string {
  if (result.name === props.selectedName) {
    return "ring-2 ring-primary ring-offset-1";
  }
  return index === squares.value.length - 1
    ? "ring-2 ring-foreground/20 ring-offset-1"
    : "";
}
</script>
