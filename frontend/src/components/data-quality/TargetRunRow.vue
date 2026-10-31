<template>
            <TableRow :data-target-guid="targetGuid(summary)">
              <TableCell>
                <div class="flex max-w-[14rem] min-w-0 flex-wrap items-center gap-2 md:max-w-md">
                  <RouterLink
                    :to="targetRoute(summary)"
                    class="truncate text-left font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
                    :title="targetPathLine(summary)"
                  >
                    {{ targetName(summary) }}
                  </RouterLink>
                  <Badge variant="outline">
                    {{ metaTypeSingularLabel(summary.target?.metaType ?? MetaType.UNSPECIFIED, t) }}
                  </Badge>
                  <Badge
                    v-if="summary.orphaned"
                    variant="danger"
                    :title="t('dataQuality.workspace.orphanedHint')"
                  >
                    {{ t("dataQuality.orphaned") }}
                  </Badge>
                </div>
                <div
                  class="max-w-[14rem] truncate font-mono text-xs text-muted-foreground md:max-w-md"
                  :title="targetPathLine(summary)"
                >
                  {{ targetPathLine(summary) }}
                </div>
                <!-- Below md the scan columns collapse in here. -->
                <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                  <template v-if="latestRun(summary)">
                    <span :title="scanTimeTitle(summary)">
                      {{ scanTimeLabel(summary) }}
                    </span>
                    <span aria-hidden="true">·</span>
                    <span>
                      {{ t(qualityExecutionTriggerKey(latestRun(summary)?.trigger ?? QualityExecutionTrigger.UNSPECIFIED)) }}
                    </span>
                  </template>
                  <span v-else>-</span>
                  <Badge
                    v-if="runOutcome(summary) !== QualityStatus.UNSPECIFIED"
                    :variant="qualityStatusVariant(runOutcome(summary))"
                  >
                    {{ t(qualityStatusKey(runOutcome(summary))) }}
                  </Badge>
                  <span
                    v-if="runStateHint(summary)"
                    class="min-w-0"
                  >
                    {{ runStateHint(summary) }}
                  </span>
                  <span class="tabular-nums">
                    {{ t("dataQuality.executions.runCountValue", { count: summary.runCount }) }}
                  </span>
                </div>
              </TableCell>
              <TableCell
                class="hidden md:table-cell"
                :title="scanTimeTitle(summary)"
              >
                <template v-if="latestRun(summary)">
                  <div class="whitespace-nowrap">
                    {{ scanTimeLabel(summary) }}
                  </div>
                  <div class="mt-1 text-xs text-muted-foreground">
                    {{ t(qualityExecutionTriggerKey(latestRun(summary)?.trigger ?? QualityExecutionTrigger.UNSPECIFIED)) }}
                  </div>
                </template>
                <span
                  v-else
                  class="text-sm text-muted-foreground"
                >
                  -
                </span>
              </TableCell>
              <TableCell
                class="hidden md:table-cell"
                :title="runStateHint(summary)"
              >
                <template v-if="latestRun(summary)">
                  <Badge
                    v-if="runOutcome(summary) !== QualityStatus.UNSPECIFIED"
                    :variant="qualityStatusVariant(runOutcome(summary))"
                  >
                    {{ t(qualityStatusKey(runOutcome(summary))) }}
                  </Badge>
                  <Badge
                    v-else-if="runState(summary) !== QualityExecutionState.COMPLETED"
                    :variant="qualityExecutionStateVariant(runState(summary))"
                  >
                    {{ t(qualityExecutionStateKey(runState(summary))) }}
                  </Badge>
                  <div
                    v-if="countsSummary(summary)"
                    class="mt-1 text-xs text-muted-foreground tabular-nums"
                  >
                    {{ countsSummary(summary) }}
                  </div>
                  <div
                    v-if="partialChecks(summary) > 0"
                    class="mt-1 text-xs text-muted-foreground tabular-nums"
                  >
                    {{ t("dataQuality.executions.runPartial", { done: runCounts(summary)?.total ?? 0, all: partialChecks(summary) }) }}
                  </div>
                  <div
                    v-if="unfinishedChecks(summary) > 0"
                    class="mt-1 text-xs text-warning tabular-nums"
                  >
                    {{ t("dataQuality.executions.runIncomplete", { count: unfinishedChecks(summary) }) }}
                  </div>
                </template>
                <span
                  v-else
                  class="text-sm text-muted-foreground"
                >
                  -
                </span>
              </TableCell>
              <TableCell class="hidden whitespace-nowrap tabular-nums lg:table-cell">
                {{ runDurationLabel(summary) }}
              </TableCell>
              <TableCell class="hidden text-right tabular-nums md:table-cell">
                <span :class="{ 'text-destructive': failedChecks(summary) > 0 }">
                  {{ failedChecks(summary) > 0 ? failedChecks(summary) : "-" }}
                </span>
              </TableCell>
              <TableCell class="hidden text-right tabular-nums md:table-cell">
                {{ t("dataQuality.executions.runCountValue", { count: summary.runCount }) }}
              </TableCell>
              <TableCell class="text-right">
                <Button
                  variant="ghost"
                  size="icon"
                  class="h-8 w-8"
                  :aria-expanded="expanded"
                  :aria-label="t('dataQuality.executions.historyTitle')"
                  :title="t('dataQuality.executions.historyTitle')"
                  @click="emit('toggle')"
                >
                  <ChevronDown
                    v-if="expanded"
                    class="h-4 w-4"
                  />
                  <ChevronRight
                    v-else
                    class="h-4 w-4"
                  />
                </Button>
              </TableCell>
            </TableRow>
            <TableRow v-if="expanded">
              <TableCell
                :colspan="7"
                class="bg-muted/20 p-0"
              >
                <TargetScanHistory
                  :target-guid="targetGuid(summary)"
                  :meta-type="summary.target?.metaType ?? MetaType.UNSPECIFIED"
                  :rule-titles="ruleTitles"
                  :target-rule-count="summary.enabledRuleCount"
                  :expand-run="expandRun"
                  @update:expand-run="(name) => emit('update:expand-run', name)"
                />
              </TableCell>
            </TableRow>
</template>

<script setup lang="ts">
import { ChevronDown, ChevronRight } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import { partialQualityRunChecks } from "@/lib/qualityRun";
import {
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityStatus,
  type QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  qualityExecutionStateKey,
  qualityExecutionStateVariant,
  qualityExecutionTriggerKey,
  qualityStatusKey,
  qualityStatusVariant,
} from "@/utils/dataQuality";
import {
  countsSummary as countsSummaryText,
  failedChecks,
  latestRun,
  runCounts,
  runDurationLabel,
  runOutcome,
  runState,
  runStateHint as runStateHintText,
  scanTimeLabel as scanTimeLabelText,
  scanTimeTitle as scanTimeTitleText,
  targetGuid,
  targetName,
  targetPathLine,
  unfinishedChecks,
} from "@/utils/dataQualityExecutions";
import { guidToRouteParams } from "@/utils/guid";
import { metaTypeSingularLabel } from "@/utils/metaType";
import TargetScanHistory from "./TargetScanHistory.vue";

/** One target's row on the executions directory, plus its expanded history. */

defineProps<{
  summary: QualityTargetSummary;
  expanded: boolean;
  ruleTitles: Record<string, string>;
  expandRun: string;
}>();

const emit = defineEmits<{
  toggle: [];
  "update:expand-run": [name: string];
}>();

const { t, locale } = useI18n();

function countsSummary(summary: QualityTargetSummary): string {
  return countsSummaryText(summary, t);
}

function scanTimeLabel(summary: QualityTargetSummary): string {
  return scanTimeLabelText(summary, locale.value);
}

function scanTimeTitle(summary: QualityTargetSummary): string {
  return scanTimeTitleText(summary, locale.value);
}

function runStateHint(summary: QualityTargetSummary): string {
  return runStateHintText(summary, t);
}

/**
 * The target's enabled rule count when a scheduled batch covered only a slice of
 * an oversize target (batches are target-complete, but a legacy oversize target
 * may be split). Only a batch at the composition cap is disclosed, so a retry
 * batch or a check added since the run never reads as partial.
 */
function partialChecks(summary: QualityTargetSummary): number {
  return partialQualityRunChecks(summary.latestRun, summary.enabledRuleCount);
}

function targetRoute(summary: QualityTargetSummary) {
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(targetGuid(summary)) },
    query: {
      metaType: String(summary.target?.metaType ?? MetaType.UNSPECIFIED),
    },
  };
}
</script>
