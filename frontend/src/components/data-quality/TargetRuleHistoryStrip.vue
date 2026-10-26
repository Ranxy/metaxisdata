<template>
  <div
    v-if="squares.length > 0"
    role="list"
    :aria-label="label"
    class="flex flex-wrap gap-1"
  >
    <RouterLink
      v-for="(execution, index) in squares"
      :key="execution.execution || index"
      :to="executionTo(execution)"
      role="listitem"
      class="h-9 w-5 shrink-0 rounded-sm transition hover:opacity-80 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:outline-none"
      :class="
        index === squares.length - 1
          ? 'ring-2 ring-foreground/20 ring-offset-1'
          : ''
      "
      :style="{ backgroundColor: colorFor(execution) }"
      :title="squareLabel(execution)"
      :aria-label="squareLabel(execution)"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import {
  type QualityExecutionSummary,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityStatusKey } from "@/utils/dataQuality";
import { formatDateTime } from "@/utils/datetime";

/**
 * A target's recent executions as one strip of small squares, oldest on the left
 * and the newest on the right, so a workspace reads the target's health at a
 * glance without opening a table. Each square links to the execution it draws.
 *
 * `executions` arrives newest-first, exactly as the API returns it; the strip
 * reverses the visible window for display. The squares are links, so an
 * execution is reachable by keyboard, and each carries its verdict and absolute
 * finish time as its accessible name.
 */

const props = withDefaults(
  defineProps<{
    /** The target's executions, newest first, as the API returns them. */
    executions: QualityExecutionSummary[];
    /** How many of the newest executions to show. */
    limit?: number;
    /** The strip's localized accessible name; supplied by the page. */
    label: string;
  }>(),
  { limit: 10 }
);

const { t, locale } = useI18n();

/** The displayed squares, oldest first. */
const squares = computed(() =>
  props.executions.slice(0, props.limit).reverse()
);

// The theme tokens are HSL triplets, so they are used through `hsl(...)`.
// FAIL and ERROR stay distinct: one is a failed check, the other a check that
// could not run.
function colorFor(execution: QualityExecutionSummary): string {
  switch (execution.status) {
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

/** Verdict and absolute finish time, for the tooltip and the accessible name. */
function squareLabel(execution: QualityExecutionSummary): string {
  const status = t(qualityStatusKey(execution.status));
  return `${status} · ${formatDateTime(execution.finishTime, locale.value)}`;
}

/** The execution's resource name is carried whole, one segment per path part. */
function executionTo(execution: QualityExecutionSummary) {
  return {
    name: "DataQualityExecutionDetail",
    params: { executionId: execution.execution.split("/") },
  };
}
</script>
