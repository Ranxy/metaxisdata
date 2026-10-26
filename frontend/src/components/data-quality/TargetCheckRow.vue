<template>
  <li
    :id="rowId"
    class="flex flex-wrap items-start justify-between gap-3 px-4 py-3"
    :class="{
      'animate-in bg-primary/5 ring-2 ring-primary/50 fade-in': highlight,
    }"
  >
    <div class="flex min-w-0 flex-1 items-start gap-3">
      <Badge
        :variant="statusVariant"
        class="mt-0.5 shrink-0 tabular-nums"
      >
        {{ statusLabel }}
      </Badge>
      <div class="min-w-0 space-y-1">
        <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
          <RouterLink
            :to="detailTo"
            class="min-w-0 truncate text-sm font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            {{ ruleTitle }}
          </RouterLink>
          <Badge variant="secondary">
            {{ t(qualityRuleTypeKey(rule.ruleType)) }}
          </Badge>
          <Badge :variant="qualitySeverityVariant(rule.severity)">
            {{ t(qualitySeverityKey(rule.severity)) }}
          </Badge>
          <Badge
            v-if="rule.orphanedReason"
            variant="danger"
            :title="rule.orphanedReason"
          >
            {{ t("dataQuality.orphaned") }}
          </Badge>
          <Badge
            v-if="rule.targetSchemaChanged"
            variant="warning"
          >
            {{ t("dataQuality.targetChanged") }}
          </Badge>
          <Badge
            v-if="issueCount > 0"
            variant="danger"
            :title="t('dataQuality.workspace.issuesTitle')"
          >
            {{ t("dataQuality.issue") }} · {{ issueCount }}
          </Badge>
        </div>
        <div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <span
            class="font-mono break-all"
            :title="targetPath"
          >
            {{ targetPath }}
          </span>
          <span aria-hidden="true">·</span>
          <span class="tabular-nums">{{ t("dataQuality.schedule") }} {{ schedule }}</span>
        </div>
        <TargetRuleHistoryStrip
          v-if="history.length > 0"
          :executions="history"
          :label="t('dataQuality.recentExecutions')"
        />
      </div>
    </div>

    <div class="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1">
      <div
        v-if="lastResult"
        class="flex items-center gap-1.5 text-xs text-muted-foreground"
      >
        <span class="font-medium text-foreground tabular-nums">
          {{ observed }}
        </span>
        <span :title="formatDateTime(lastResult.finishTime, locale)">
          {{ formatRelativeTime(lastResult.finishTime, locale) }}
        </span>
      </div>
      <div class="flex items-center gap-0.5">
        <Button
          v-if="canRun && rule.state !== QualityRuleState.ARCHIVED"
          variant="ghost"
          size="icon"
          class="h-8 w-8"
          :title="t('dataQuality.workspace.runRule')"
          :aria-label="t('dataQuality.workspace.runRule')"
          @click="emit('run')"
        >
          <Zap class="h-4 w-4 text-muted-foreground" />
        </Button>
        <Button
          v-if="canUpdate && rule.state !== QualityRuleState.ARCHIVED"
          variant="ghost"
          size="icon"
          class="h-8 w-8"
          :title="t('dataQuality.editRule')"
          :aria-label="t('dataQuality.editRule')"
          @click="emit('edit')"
        >
          <Pencil class="h-4 w-4 text-muted-foreground" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          class="h-8 w-8"
          as-child
        >
          <RouterLink
            :to="detailTo"
            :title="t('dataQuality.workspace.ruleDetails')"
            :aria-label="t('dataQuality.workspace.ruleDetails')"
          >
            <Eye class="h-4 w-4 text-muted-foreground" />
          </RouterLink>
        </Button>
      </div>
    </div>
  </li>
</template>

<script setup lang="ts">
import { Eye, Pencil, Zap } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import TargetRuleHistoryStrip from "@/components/data-quality/TargetRuleHistoryStrip.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  type QualityExecutionSummary,
  type QualityRule,
  QualityRuleState,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  formatObservedValue,
  formatScheduleInterval,
  formatTargetPath,
  qualityRuleTypeKey,
  qualitySeverityKey,
  qualitySeverityVariant,
  qualityStatusKey,
  qualityStatusVariant,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * One rule as a row of its target workspace. The row is a flex list item, not a
 * table row: the title is the only navigation affordance, and the run/edit
 * controls stay reachable per row without a fake `<tr>`-sized click target.
 *
 * Edit opens the workspace's inline composer rather than the full editor; the
 * link to the rule's own page stays the place to see results and history.
 */

const props = withDefaults(
  defineProps<{
    rule: QualityRule;
    /** Unresolved issues the rule's latest failures opened. */
    issueCount: number;
    canRun: boolean;
    canUpdate: boolean;
    /** Recent completed executions, newest first, for the row's history strip. */
    history?: QualityExecutionSummary[];
    /** True for the row a back-link from elsewhere pointed at. */
    highlight?: boolean;
  }>(),
  { history: () => [], highlight: false }
);

const emit = defineEmits<{
  run: [];
  /** Start editing this rule in place. */
  edit: [];
}>();

const { t, locale } = useI18n();

const lastResult = computed(() => props.rule.lastResult);
const ruleTitle = computed(
  () => props.rule.title || props.rule.name.split("/").pop() || props.rule.name
);
const targetPath = computed(() => {
  const path = formatTargetPath(props.rule.target?.path);
  return path === "-" ? (props.rule.target?.metaGuid ?? "-") : path;
});
const schedule = computed(() =>
  formatScheduleInterval(props.rule.scheduleInterval)
);
const observed = computed(() =>
  lastResult.value
    ? formatObservedValue(
        lastResult.value.observedValue,
        lastResult.value.observedKind,
        locale.value
      )
    : ""
);
const statusLabel = computed(() =>
  lastResult.value
    ? t(qualityStatusKey(lastResult.value.status))
    : t("dataQuality.noResultYet")
);
const statusVariant = computed(() =>
  lastResult.value
    ? qualityStatusVariant(lastResult.value.status)
    : ("outline" as const)
);

/**
 * The row's DOM id, addressable as `dq-rule-{id}`. The rule's resource name has
 * slashes in it, so only its last segment is used; an id with slashes would be an
 * invalid CSS selector for the page that scrolls to it.
 */
const resourceId = computed(() => props.rule.name.split("/").pop() ?? "");
const rowId = computed(() => `dq-rule-${resourceId.value}`);

const detailTo = computed(() => ({
  name: "DataQualityRuleDetail",
  params: { ruleId: props.rule.name.split("/") },
}));
</script>
