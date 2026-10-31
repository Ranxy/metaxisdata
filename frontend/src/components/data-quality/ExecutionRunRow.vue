<template>
  <TableRow
    class="cursor-pointer"
    @click="emit('open')"
  >
              <TableCell>
                <div class="flex flex-wrap items-center gap-2">
                  <Badge
                    :variant="qualityExecutionStateVariant(execution.state)"
                  >
                    {{ t(qualityExecutionStateKey(execution.state)) }}
                  </Badge>
                  <Badge
                    v-if="execution.outcome !== QualityStatus.UNSPECIFIED"
                    :variant="qualityStatusVariant(execution.outcome)"
                    :title="t('dataQuality.outcome')"
                  >
                    {{ t(qualityStatusKey(execution.outcome)) }}
                  </Badge>
                </div>
                <div class="mt-1 font-mono text-xs text-muted-foreground">
                  {{ qualityExecutionID(execution.name) }}
                </div>
                <!-- Below md the rule, target, observed, rows, trigger,
                     duration and started columns collapse in here. -->
                <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                  <RouterLink
                    v-if="canViewRule"
                    :to="ruleTargetRoute(execution)"
                    class="min-w-0 truncate hover:underline"
                    @click.stop
                  >
                    {{ ruleLabel(execution.rule) }}
                  </RouterLink>
                  <span
                    v-else
                    class="min-w-0 truncate"
                  >
                    {{ ruleLabel(execution.rule) }}
                  </span>
                  <span aria-hidden="true">·</span>
                  <RouterLink
                    v-if="hasTarget(execution)"
                    :to="executionTargetRoute(execution)"
                    class="min-w-0 truncate hover:underline"
                    :title="targetTitle(execution)"
                    @click.stop
                  >
                    {{ targetPath(execution) }}
                  </RouterLink>
                  <span
                    v-else
                    class="min-w-0 truncate"
                    :title="targetTitle(execution)"
                  >
                    {{ targetPath(execution) }}
                  </span>
                  <span aria-hidden="true">·</span>
                  <span class="tabular-nums">{{ observedLabel(execution) }}</span>
                  <span aria-hidden="true">·</span>
                  <span class="tabular-nums">{{ rowsLabel(execution) }}</span>
                  <span aria-hidden="true">·</span>
                  <span>{{ t(qualityExecutionTriggerKey(execution.trigger)) }}</span>
                  <span aria-hidden="true">·</span>
                  <span>{{ durationLabel(execution) }}</span>
                  <span aria-hidden="true">·</span>
                  <span :title="formatDateTime(execution.startTime, locale)">
                    {{ formatRelativeTime(execution.startTime, locale) }}
                  </span>
                </div>
              </TableCell>
              <TableCell class="hidden md:table-cell">
                <RouterLink
                  v-if="canViewRule"
                  :to="ruleTargetRoute(execution)"
                  class="block max-w-[11rem] truncate font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none lg:max-w-[14rem]"
                  :title="ruleLabel(execution.rule)"
                  @click.stop
                >
                  {{ ruleLabel(execution.rule) }}
                </RouterLink>
                <div
                  v-else
                  class="max-w-[11rem] truncate font-medium lg:max-w-[14rem]"
                  :title="ruleLabel(execution.rule)"
                >
                  {{ ruleLabel(execution.rule) }}
                </div>
              </TableCell>
              <TableCell class="hidden max-w-[14rem] md:table-cell">
                <RouterLink
                  v-if="hasTarget(execution)"
                  :to="executionTargetRoute(execution)"
                  class="block truncate text-xs hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
                  :title="targetTitle(execution)"
                  @click.stop
                >
                  {{ targetPath(execution) }}
                </RouterLink>
                <div
                  v-else
                  class="truncate text-xs"
                  :title="targetTitle(execution)"
                >
                  {{ targetPath(execution) }}
                </div>
              </TableCell>
              <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                {{ observedLabel(execution) }}
              </TableCell>
              <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                {{ rowsLabel(execution) }}
              </TableCell>
              <TableCell class="hidden md:table-cell">
                {{ t(qualityExecutionTriggerKey(execution.trigger)) }}
              </TableCell>
              <TableCell
                class="hidden whitespace-nowrap text-muted-foreground md:table-cell"
                :title="formatDateTime(execution.startTime, locale)"
              >
                {{ formatRelativeTime(execution.startTime, locale) }}
                <div class="mt-1 text-xs tabular-nums">
                  {{ t("dataQuality.duration") }} {{ durationLabel(execution) }}
                </div>
              </TableCell>
              <TableCell
                class="text-right"
                @click.stop
              >
                <Button
                  variant="ghost"
                  size="icon"
                  class="h-8 w-8"
                  :title="t('dataQuality.viewExecution')"
                  :aria-label="t('dataQuality.viewExecution')"
                  @click="emit('open')"
                >
                  <Eye class="h-4 w-4 text-muted-foreground" />
                </Button>
              </TableCell>
  </TableRow>
</template>

<script setup lang="ts">
import { Eye } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import { type RouteLocationRaw, RouterLink } from "vue-router";
import { qualityExecutionID, qualityRuleID } from "@/api/data-quality";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import {
  type QualityExecution,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  executionDurationSeconds,
  formatObservedValue,
  formatRowCount,
  formatSeconds,
  formatTargetPath,
  qualityExecutionStateKey,
  qualityExecutionStateVariant,
  qualityExecutionTriggerKey,
  qualityStatusKey,
  qualityStatusVariant,
  targetScopeLabel,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";

/** One check's row on the flat executions list. */

const props = defineProps<{
  execution: QualityExecution;
  ruleTitles: Record<string, string>;
  canViewRule: boolean;
}>();

const emit = defineEmits<{ open: [] }>();

const { t, locale } = useI18n();

function ruleLabel(ruleName: string): string {
  return props.ruleTitles[ruleName] || qualityRuleID(ruleName);
}

function ruleRoute(ruleName: string) {
  return {
    name: "DataQualityRuleRedirect",
    params: { ruleId: ruleName.split("/") },
  };
}

/** Whether the execution carries the target its rule watched. */
function hasTarget(execution: QualityExecution): boolean {
  return Boolean(execution.target?.metaGuid);
}

/**
 * The target workspace for one execution, opening the check drawer when a check
 * name is given. Undefined when the execution carries no target: historical
 * rows can predate the target snapshot.
 */
function targetWorkspaceRoute(
  execution: QualityExecution,
  check = ""
): RouteLocationRaw | undefined {
  const target = execution.target;
  if (!target?.metaGuid) {
    return undefined;
  }
  // A snapshot without a type is not an error: the target page derives it from
  // the summary or the rules, so the query carries only what it knows.
  const query: Record<string, string> = {};
  if (target.metaType !== MetaType.UNSPECIFIED) {
    query.metaType = String(target.metaType);
  }
  if (check) {
    query.check = check;
  }
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.metaGuid) },
    query,
  };
}

/**
 * A rule name links into its target's workspace with the check selected. A row
 * without a target snapshot falls back to the retired per-rule URL, whose
 * redirect resolves the target itself.
 */
function ruleTargetRoute(execution: QualityExecution): RouteLocationRaw {
  return (
    targetWorkspaceRoute(execution, execution.rule) ?? ruleRoute(execution.rule)
  );
}

/** The target itself, without selecting a check. */
function executionTargetRoute(execution: QualityExecution): RouteLocationRaw {
  return targetWorkspaceRoute(execution) ?? ruleRoute(execution.rule);
}

function targetPath(execution: QualityExecution): string {
  const path = formatTargetPath(execution.target?.path);
  return path === "-" ? execution.target?.metaGuid || "-" : path;
}

/** Every target's instance and path, for the cell's tooltip. */
function targetTitle(execution: QualityExecution): string {
  const path = targetPath(execution);
  const scope = targetScopeLabel(execution.target?.path);
  return scope === "-" ? path : `${scope} · ${path}`;
}

/** The verdict's measured value, or "-" while there is no verdict yet. */
function observedLabel(execution: QualityExecution): string {
  if (execution.outcome === QualityStatus.UNSPECIFIED) {
    return "-";
  }
  return formatObservedValue(
    execution.observedValue,
    execution.observedKind,
    locale.value
  );
}

function rowsLabel(execution: QualityExecution): string {
  if (execution.outcome === QualityStatus.UNSPECIFIED) {
    return "-";
  }
  return `${formatRowCount(execution.failedRows, locale.value)} / ${formatRowCount(execution.totalRows, locale.value)}`;
}

function durationLabel(execution: QualityExecution): string {
  const seconds = executionDurationSeconds(execution);
  return seconds === undefined ? "-" : formatSeconds(seconds);
}
</script>
