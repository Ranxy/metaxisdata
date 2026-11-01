<template>
  <RouterLink
    v-if="visible && to"
    :to="to"
    class="inline-flex rounded-full focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-1 focus-visible:outline-none"
    :title="tooltipText"
    :aria-label="tooltipText"
  >
    <Badge :variant="badgeVariant">
      <QualitySignalDot :tone="dotTone" />
      {{ status }}
    </Badge>
  </RouterLink>
  <Badge
    v-else-if="visible"
    :variant="badgeVariant"
    :title="tooltipText"
  >
    <QualitySignalDot :tone="dotTone" />
    {{ status }}
  </Badge>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { RouterLink } from "vue-router";
import { Badge } from "@/components/ui/badge";
import type { QualitySignalState } from "@/composables/useQualitySignals";
import { useQualitySignalText } from "@/composables/useQualitySignalText";
import {
  QualityStatus,
  type QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  qualityStatusVariant,
  qualityTargetWorkspaceRoute,
} from "@/utils/dataQuality";
import {
  type QualitySignalTone,
  qualitySignalAllPaused,
} from "@/utils/dataQualitySignal";
import QualitySignalDot from "./QualitySignalDot.vue";

/**
 * The compact trust signal an asset page shows beside the object's own type
 * badge, visible without opening the data-quality tab.
 *
 * It is the way into the target workspace: a monitored object opens its
 * contract, and an unmonitored one opens the same workspace with the check
 * composer already open (`add=1`), which is the only next step.
 */
const props = withDefaults(
  defineProps<{
    signal?: QualityTargetSummary;
    state?: QualitySignalState;
    guid?: string;
    metaType?: MetaType;
  }>(),
  { signal: undefined, state: "idle", guid: "", metaType: MetaType.UNSPECIFIED }
);

const { statusText, tooltip, tone } = useQualitySignalText();

const visible = computed(
  () => props.signal !== undefined || props.state === "ready"
);
const status = computed(() => statusText(props.signal));
const tooltipText = computed(() => tooltip(props.signal));
const dotTone = computed<QualitySignalTone>(() => tone(props.signal));
const to = computed(() =>
  qualityTargetWorkspaceRoute(props.guid, props.metaType, {
    add: props.signal === undefined,
  })
);
const badgeVariant = computed(() => {
  if (!props.signal || qualitySignalAllPaused(props.signal)) {
    return "neutral" as const;
  }
  return props.signal.worstStatus === QualityStatus.UNSPECIFIED
    ? ("neutral" as const)
    : qualityStatusVariant(props.signal.worstStatus);
});
</script>
