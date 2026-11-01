<template>
  <div
    v-if="visible"
    class="min-w-0"
    :title="tooltipText"
  >
    <div class="flex items-center gap-1.5">
      <QualitySignalDot :tone="dotTone" />
      <span
        :class="[
          'truncate text-sm',
          signal ? 'font-medium' : 'text-muted-foreground',
        ]"
      >
        {{ status }}
      </span>
    </div>
    <div
      v-if="detail"
      class="mt-0.5 truncate text-xs text-muted-foreground"
    >
      {{ detail }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import type { QualitySignalState } from "@/composables/useQualitySignals";
import { useQualitySignalText } from "@/composables/useQualitySignalText";
import type { QualityTargetSummary } from "@/types/proto-es/v1/data_quality_service_pb";
import QualitySignalDot from "./QualitySignalDot.vue";

/**
 * The metadata lists' one quality column: line one is the status point and
 * word, line two the muted coverage and last run.
 *
 * It carries no link of its own. The whole row opens the object, and a link
 * nested in a row that is itself clickable swallows the row's click.
 */
const props = withDefaults(
  defineProps<{
    signal?: QualityTargetSummary;
    /** Whether the read that would answer this row has finished. */
    state?: QualitySignalState;
  }>(),
  { signal: undefined, state: "idle" }
);

const { statusText, detailText, tooltip, tone } = useQualitySignalText();

// A known answer renders even while a later batch loads (a "load more" must not
// blank the rows already on screen). An unknown one waits for the read: an
// unfinished or failed load is not the same fact as "not monitored".
const visible = computed(
  () => props.signal !== undefined || props.state === "ready"
);
const status = computed(() => statusText(props.signal));
const detail = computed(() => detailText(props.signal));
const tooltipText = computed(() => tooltip(props.signal));
const dotTone = computed(() => tone(props.signal));
</script>
