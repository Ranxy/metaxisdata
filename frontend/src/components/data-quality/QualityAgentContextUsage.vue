<template>
  <div class="space-y-1.5 border-t pt-3">
    <template v-if="usage && hasWindow">
      <div class="flex items-baseline justify-between gap-2 text-xs">
        <span class="text-muted-foreground">{{ t("qualityAgent.contextTitle") }}</span>
        <span class="tabular-nums text-muted-foreground">
          {{ t("qualityAgent.contextPercent", { percent }) }}
        </span>
      </div>
      <div
        class="relative h-1.5 w-full overflow-hidden rounded-full bg-muted"
        role="progressbar"
        :aria-valuenow="ariaValue"
        :aria-valuetext="ariaValueText"
        aria-valuemin="0"
        :aria-valuemax="usage.contextWindow"
        :aria-label="t('qualityAgent.contextTitle')"
      >
        <div
          class="h-full rounded-full transition-[width]"
          :class="fillClass"
          :style="{ width: barWidth }"
        />
        <span
          v-if="markerLeft"
          aria-hidden="true"
          class="absolute inset-y-0 w-px bg-foreground/40"
          :style="{ left: markerLeft }"
        />
      </div>
      <p
        class="text-xs tabular-nums"
        :title="t('qualityAgent.contextWindowHint')"
      >
        {{
          t("qualityAgent.contextUsage", {
            used: formatTokens(usage.usedTokens),
            window: formatTokens(usage.contextWindow),
          })
        }}
        <span
          v-if="usage.compactionTriggerTokens > 0"
          class="text-muted-foreground"
        >
          ·
          {{
            t("qualityAgent.contextCompactsNear", {
              tokens: formatTokens(usage.compactionTriggerTokens),
            })
          }}
        </span>
        <span v-else class="text-muted-foreground">
          · {{ t("qualityAgent.contextCannotCompact") }}
        </span>
      </p>
      <p class="break-words text-xs text-muted-foreground">{{ sourceLabel }}</p>
      <p v-if="modelChanged" class="break-words text-xs text-warning">
        {{ t("qualityAgent.contextModelChanged", { model: agentModel }) }}
      </p>
      <p v-else-if="modelRemoved" class="break-words text-xs text-warning">
        {{ t("qualityAgent.contextModelRemoved") }}
      </p>
    </template>
    <p v-else class="break-words text-xs text-muted-foreground">
      {{ emptyLabel }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  type QualityAgentSessionContextUsage,
  QualityAgentSessionMode,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatRelativeTime } from "@/utils/datetime";
import {
  agentSessionContextLevel,
  agentSessionContextRatio,
} from "@/utils/qualityAgentSession";

/**
 * The session's last model-input sample: used tokens against the pinned model's
 * window, with the engine's compaction point marked. The sample is an engine
 * observation, not the session's spend; the transcript rows own that number.
 *
 * The page renders the run state twice (wide rail and below-xl disclosure), so this
 * block lives with the rest of the run-state content instead of being duplicated.
 */
const props = defineProps<{
  usage?: QualityAgentSessionContextUsage;
  /** The model the agent is configured for now; a mismatch is shown as stale. */
  agentModel?: string;
  /**
   * False while the agent read is pending or has failed. An empty agentModel with
   * this true means the agent really has no model, not that the page does not know.
   */
  agentModelKnown?: boolean;
  /** A triage session parks without a model; a monitor runs the deterministic survey. */
  sessionMode?: QualityAgentSessionMode;
}>();

const { t, locale } = useI18n();

const hasWindow = computed(() => (props.usage?.contextWindow ?? 0) > 0);

const ratio = computed(() => agentSessionContextRatio(props.usage));
// Floor keeps 3,999/4,000 from reading as a completed bar while the level stays normal.
const percent = computed(() => Math.floor(ratio.value * 100));
const barWidth = computed(() => `${ratio.value * 100}%`);

const ariaValue = computed(() =>
  Math.min(props.usage?.usedTokens ?? 0, props.usage?.contextWindow ?? 0)
);

const ariaValueText = computed(() =>
  t("qualityAgent.contextValueText", {
    used: formatTokens(props.usage?.usedTokens ?? 0),
    window: formatTokens(props.usage?.contextWindow ?? 0),
    percent: percent.value,
  })
);

const markerLeft = computed(() => {
  const usage = props.usage;
  if (
    !usage ||
    usage.contextWindow <= 0 ||
    usage.compactionTriggerTokens <= 0
  ) {
    return "";
  }
  const share = Math.min(
    100,
    (usage.compactionTriggerTokens / usage.contextWindow) * 100
  );
  return `${share}%`;
});

const fillClass = computed(() => {
  switch (agentSessionContextLevel(props.usage)) {
    case "critical":
      return "bg-destructive";
    case "warning":
      return "bg-warning";
    default:
      return "bg-info";
  }
});

/** Numbers follow the application language, not the browser's default locale. */
function formatTokens(value: number): string {
  return new Intl.NumberFormat(locale.value).format(value);
}

const sourceLabel = computed(() => {
  const usage = props.usage;
  if (!usage) {
    return "";
  }
  const source = usage.estimated
    ? t("qualityAgent.contextEstimated")
    : t("qualityAgent.contextProviderReported");
  const measured = usage.turn
    ? t("qualityAgent.contextMeasuredAtTurn", {
        wake: usage.wake,
        turn: usage.turn,
        time: formatRelativeTime(usage.measuredAt, locale.value),
      })
    : t("qualityAgent.contextMeasuredAt", {
        wake: usage.wake,
        time: formatRelativeTime(usage.measuredAt, locale.value),
      });
  return [usage.model, source, measured].filter(Boolean).join(" · ");
});

/** A model change leaves the last sample from the previous wake; say so. */
const modelChanged = computed(
  () =>
    Boolean(props.usage?.model) &&
    props.agentModelKnown === true &&
    Boolean(props.agentModel) &&
    props.usage?.model !== props.agentModel
);

/** The model was removed, so the last sample is historical rather than current. */
const modelRemoved = computed(
  () =>
    Boolean(props.usage?.model) &&
    props.agentModelKnown === true &&
    props.agentModel === ""
);

const emptyLabel = computed(() => {
  if (props.agentModelKnown !== true) {
    return t("qualityAgent.contextModelUnknown");
  }
  if (!props.agentModel) {
    return props.sessionMode === QualityAgentSessionMode.TRIAGE
      ? t("qualityAgent.contextNoModelTriage")
      : t("qualityAgent.contextNoModel");
  }
  return t("qualityAgent.contextNotMeasured");
});
</script>
