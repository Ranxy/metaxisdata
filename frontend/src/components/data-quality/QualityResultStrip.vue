<template>
  <div
    v-if="squares.length > 0"
    role="list"
    :aria-label="label"
    class="flex flex-wrap gap-1"
  >
    <button
      v-for="(execution, index) in squares"
      :key="execution.name || index"
      type="button"
      role="listitem"
      class="h-9 w-5 shrink-0 rounded-sm transition hover:opacity-80 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:outline-none"
      :class="ringClass(execution, index)"
      :style="{ backgroundColor: colorFor(execution) }"
      :title="squareLabel(execution)"
      :aria-label="squareLabel(execution)"
      @click="emit('select', execution)"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  type QualityExecution,
  QualityExecutionState,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  qualityExecutionStateKey,
  qualityStatusKey,
} from "@/utils/dataQuality";
import { formatDateTime } from "@/utils/datetime";

/**
 * One rule's recent execution history as a strip of small squares, oldest on
 * the left and the newest on the right, so the eye reads pass/fail at a glance
 * instead of scanning a table.
 *
 * `executions` arrives newest-first, exactly as the API returns it; the strip
 * reverses the visible window for display. A pending or running execution is
 * drawn in the info colour with its lifecycle state as the label, so the strip
 * does not pretend an unfinished check already has a verdict. The squares are
 * buttons so an execution is reachable and clickable by keyboard, and each
 * carries its state and absolute time as its accessible name.
 */

const props = withDefaults(
  defineProps<{
    /** The rule's executions, newest first, as the API returns them. */
    executions: QualityExecution[];
    /** How many of the newest executions to show. */
    limit?: number;
    /** The strip's localized accessible name; supplied by the page. */
    label: string;
    /** The name of an execution to mark as selected, if any. */
    selectedName?: string;
  }>(),
  { limit: 20 }
);

const emit = defineEmits<{
  select: [execution: QualityExecution];
}>();

const { t, locale } = useI18n();

/** The displayed squares, oldest first. */
const squares = computed(() =>
  props.executions.slice(0, props.limit).reverse()
);

function inProgress(execution: QualityExecution): boolean {
  return (
    execution.state === QualityExecutionState.PENDING ||
    execution.state === QualityExecutionState.RUNNING
  );
}

// The theme tokens are HSL triplets, so they are used through `hsl(...)`.
function colorFor(execution: QualityExecution): string {
  if (inProgress(execution)) {
    return "hsl(var(--info))";
  }
  switch (execution.outcome) {
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

/** State or verdict and absolute time, for the tooltip and the accessible name. */
function squareLabel(execution: QualityExecution): string {
  const status = inProgress(execution)
    ? t(qualityExecutionStateKey(execution.state))
    : t(qualityStatusKey(execution.outcome));
  const time = execution.finishTime ?? execution.startTime;
  return `${status} · ${formatDateTime(time, locale.value)}`;
}

/** The selected square wins over the newest-execution emphasis. */
function ringClass(execution: QualityExecution, index: number): string {
  if (execution.name === props.selectedName) {
    return "ring-2 ring-primary ring-offset-1";
  }
  return index === squares.value.length - 1
    ? "ring-2 ring-foreground/20 ring-offset-1"
    : "";
}
</script>
