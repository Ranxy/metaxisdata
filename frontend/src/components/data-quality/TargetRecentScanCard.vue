<template>
  <Card>
    <CardHeader class="border-b">
      <CardTitle class="text-base">
        {{ t("dataQuality.workspace.recentScanTitle") }}
      </CardTitle>
      <CardDescription v-if="latestRun">
        <span :title="formatDateTime(scanTime, locale)">
          {{ scanTimeLabel }}
        </span>
      </CardDescription>
      <CardDescription v-else>
        {{ t("dataQuality.workspace.recentScanEmpty") }}
      </CardDescription>
    </CardHeader>
    <CardContent class="space-y-3 pt-4">
      <template v-if="latestRun">
        <div class="flex flex-wrap items-center gap-2">
          <Badge
            v-if="latestRun.outcome !== QualityStatus.UNSPECIFIED"
            :variant="qualityStatusVariant(latestRun.outcome)"
          >
            {{ t(qualityStatusKey(latestRun.outcome)) }}
          </Badge>
          <Badge variant="outline">
            {{ t(qualityExecutionTriggerKey(latestRun.trigger)) }}
          </Badge>
          <p
            v-if="stateHintKey"
            class="text-xs text-muted-foreground"
          >
            {{ t(stateHintKey) }}
          </p>
        </div>

        <div class="flex flex-wrap gap-1">
          <Badge variant="secondary" class="tabular-nums">
            {{ t("dataQuality.workspace.recentScanChecks", { count: totalChecks }) }}
          </Badge>
          <Badge
            v-if="partialChecks > 0"
            variant="outline"
            class="tabular-nums"
          >
            {{ t("dataQuality.executions.runPartial", { done: totalChecks, all: partialChecks }) }}
          </Badge>
          <Badge
            v-for="count in verdictCounts"
            :key="count.status"
            :variant="qualityStatusVariant(count.status)"
            class="tabular-nums"
          >
            {{ t(qualityStatusKey(count.status)) }} · {{ count.value }}
          </Badge>
          <Badge
            v-if="failedChecks > 0"
            variant="destructive"
            class="tabular-nums"
          >
            {{ t("dataQuality.workspace.recentScanFailed", { count: failedChecks }) }}
          </Badge>
          <Badge
            v-if="unfinishedChecks > 0"
            variant="warning"
            class="tabular-nums"
          >
            {{ t("dataQuality.workspace.recentScanUnfinished", { count: unfinishedChecks }) }}
          </Badge>
        </div>

        <div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <span class="tabular-nums">
            {{ t("dataQuality.workspace.recentScanDuration", { duration: durationLabel }) }}
          </span>
          <template v-if="revisionLabel">
            <span aria-hidden="true">·</span>
            <span class="tabular-nums">{{ revisionLabel }}</span>
          </template>
          <span aria-hidden="true">·</span>
          <span class="tabular-nums">
            {{ t("dataQuality.executions.runCountValue", { count: summary.runCount }) }}
          </span>
        </div>
      </template>

      <p
        v-else
        class="text-sm text-muted-foreground"
      >
        {{ t("dataQuality.workspace.recentScanEmptyHint") }}
      </p>

      <Button
        variant="ghost"
        size="sm"
        class="w-full"
        as-child
      >
        <RouterLink :to="executionsTo">
          {{ t("dataQuality.workspace.recentScanViewAll") }}
        </RouterLink>
      </Button>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  QualityExecutionState,
  QualityExecutionTrigger,
  type QualityRun,
  QualityStatus,
  type QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  executionDurationSeconds,
  formatSeconds,
  qualityExecutionTriggerKey,
  qualityStatusKey,
  qualityStatusVariant,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * The most recent scan batch for this target, as the workspace's right sidebar
 * shows it: what the batch found, how long it took and which rule revisions it
 * judged. A batch belongs to exactly one target, so `summary.latestRun` is this
 * target's own run and its counts are this target's counts.
 *
 * Every number here is a verdict count of this target's planned checks. A manual
 * or scheduled run starts PENDING, so a batch may still be running or have been
 * abandoned; the card says so instead of painting an empty result.
 */

const props = defineProps<{
  summary: QualityTargetSummary;
  targetGuid: string;
}>();

const { t, locale } = useI18n();

const latestRun = computed<QualityRun | undefined>(
  () => props.summary.latestRun
);
const counts = computed(() => latestRun.value?.counts);

const totalChecks = computed(() => counts.value?.total ?? 0);
/**
 * The target's live rule count when a scheduled batch covered only a slice of an
 * oversize target. A manual run planned an explicit selection, so it is not a
 * partial view of the target.
 */
const partialChecks = computed(() => {
  const run = latestRun.value;
  if (
    run?.trigger !== QualityExecutionTrigger.SCHEDULED ||
    props.summary.ruleCount <= totalChecks.value
  ) {
    return 0;
  }
  return props.summary.ruleCount;
});

/** The verdicts that occurred, in status order, so the badge row is stable. */
const VERDICT_ORDER: QualityStatus[] = [
  QualityStatus.PASS,
  QualityStatus.WARN,
  QualityStatus.FAIL,
  QualityStatus.ERROR,
  QualityStatus.SKIPPED,
  QualityStatus.NOT_EVALUATED,
];

function verdictCount(status: QualityStatus): number {
  const current = counts.value;
  if (!current) {
    return 0;
  }
  switch (status) {
    case QualityStatus.PASS:
      return current.pass;
    case QualityStatus.WARN:
      return current.warn;
    case QualityStatus.FAIL:
      return current.fail;
    case QualityStatus.ERROR:
      return current.error;
    case QualityStatus.SKIPPED:
      return current.skipped;
    case QualityStatus.NOT_EVALUATED:
      return current.notEvaluated;
    default:
      return 0;
  }
}

const verdictCounts = computed(() =>
  VERDICT_ORDER.map((status) => ({
    status,
    value: verdictCount(status),
  })).filter((count) => count.value > 0)
);

/** Checks that failed or errored, the two the reader must act on. */
const failedChecks = computed(
  () => verdictCount(QualityStatus.FAIL) + verdictCount(QualityStatus.ERROR)
);

/**
 * Planned checks that never reached a verdict. A batch that is still running (or
 * one abandoned part way) shows this rather than looking like an empty result.
 */
const unfinishedChecks = computed(() => {
  const finished = VERDICT_ORDER.reduce(
    (sum, status) => sum + verdictCount(status),
    0
  );
  return Math.max(0, totalChecks.value - finished);
});

const durationLabel = computed(() => {
  const run = latestRun.value;
  if (!run) {
    return "-";
  }
  const seconds = executionDurationSeconds({
    startTime: run.startTime,
    finishTime: run.finishTime,
  });
  return seconds === undefined ? "-" : formatSeconds(seconds);
});

/** The rule revisions the batch judged; it is only comparable at one range. */
const revisionLabel = computed(() => {
  const run = latestRun.value;
  if (!run || run.ruleRevisionMax === 0) {
    return "";
  }
  if (run.ruleRevisionMin === run.ruleRevisionMax) {
    return t("dataQuality.executions.revisionSingle", {
      revision: run.ruleRevisionMax,
    });
  }
  return t("dataQuality.executions.revisionRange", {
    min: run.ruleRevisionMin,
    max: run.ruleRevisionMax,
  });
});

const scanTime = computed(
  () => latestRun.value?.finishTime ?? latestRun.value?.startTime
);
const scanTimeLabel = computed(() =>
  formatRelativeTime(scanTime.value, locale.value)
);

/** A batch can be queued, running or abandoned; only the last two need words. */
const stateHintKey = computed(() => {
  switch (latestRun.value?.state) {
    case QualityExecutionState.PENDING:
    case QualityExecutionState.RUNNING:
      return "dataQuality.executions.runRunningHint";
    case QualityExecutionState.ABANDONED:
      return "dataQuality.executions.runAbandonedHint";
    default:
      return "";
  }
});

const executionsTo = computed(() => ({
  name: "DataQualityExecutions",
  query: { view: "targets", target: props.targetGuid },
}));
</script>
