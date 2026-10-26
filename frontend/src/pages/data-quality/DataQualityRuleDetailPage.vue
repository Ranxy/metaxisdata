<template>
  <div class="space-y-4">
    <PageHeader
      :title="pageTitle"
      :description="rule?.description"
    >
      <template #breadcrumb>
        <RouterLink
          :to="{ name: 'DataQualityRules' }"
          class="inline-flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft class="h-3.5 w-3.5" />
          {{ t("dataQuality.rulesTitle") }}
        </RouterLink>
      </template>
      <template #title-extra>
        <template v-if="rule">
          <Badge :variant="qualityRuleStateVariant(rule.state)">
            {{ t(qualityRuleStateKey(rule.state)) }}
          </Badge>
          <Badge :variant="qualitySeverityVariant(rule.severity)">
            {{ t(qualitySeverityKey(rule.severity)) }}
          </Badge>
          <Badge
            v-if="rule.targetSchemaChanged"
            variant="warning"
          >
            {{ t("dataQuality.targetChanged") }}
          </Badge>
          <Badge
            v-if="rule.orphanedReason"
            variant="danger"
          >
            {{ t("dataQuality.orphaned") }}
          </Badge>
        </template>
      </template>
      <template #actions>
        <Button
          v-if="targetWorkspaceTo"
          variant="outline"
          as-child
        >
          <RouterLink :to="targetWorkspaceTo">
            <Target class="mr-2 h-4 w-4" />
            {{ t("dataQuality.asset.openWorkspace") }}
          </RouterLink>
        </Button>
        <Button
          variant="outline"
          :disabled="isLoading"
          @click="load"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': isLoading }"
          />
          {{ t("common.refresh") }}
        </Button>
        <Button
          v-if="canRun"
          :disabled="running || rule?.state === QualityRuleState.ARCHIVED"
          @click="runNow"
        >
          <Zap class="mr-2 h-4 w-4" />
          {{ t("dataQuality.runNow") }}
        </Button>
        <Button
          v-if="canUpdate && rule?.state !== QualityRuleState.ARCHIVED"
          variant="outline"
          @click="editRule"
        >
          <Pencil class="mr-2 h-4 w-4" />
          {{ t("dataQuality.editRule") }}
        </Button>
      </template>
    </PageHeader>

    <PageState
      :loading="isLoading"
      :error="error"
    >
      <template #error>
        <Button
          variant="outline"
          size="sm"
          @click="load"
        >
          {{ t("common.refresh") }}
        </Button>
      </template>

      <div
        v-if="rule"
        class="space-y-4"
      >
        <Alert
          v-if="rule.orphanedReason"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>{{ t("dataQuality.orphaned") }}</AlertTitle>
          <AlertDescription class="break-words">
            {{ rule.orphanedReason }}
          </AlertDescription>
        </Alert>
        <Alert v-else-if="rule.targetSchemaChanged">
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>{{ t("dataQuality.targetChanged") }}</AlertTitle>
          <AlertDescription>
            {{ t("dataQuality.targetChangedHint") }}
          </AlertDescription>
        </Alert>
        <Alert v-if="mutedUntil">
          <BellOff class="h-4 w-4" />
          <AlertTitle>{{ t("dataQuality.issueMuted") }}</AlertTitle>
          <AlertDescription>
            {{ t("dataQuality.issueMutedHint", { time: formatDateTime(rule.muteUntil, locale) }) }}
          </AlertDescription>
        </Alert>

        <!-- The answer to "is this rule healthy?" before anything else: one big
             status, then the recent history as a strip a reader scans in one
             glance, then the counts that explain it. -->
        <Card>
          <CardContent class="space-y-5 pt-6">
            <div class="flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
              <div class="flex min-w-0 items-start gap-4">
                <span
                  class="grid h-14 w-14 shrink-0 place-items-center rounded-full"
                  :class="latestTone.circle"
                >
                  <component
                    :is="latestTone.icon"
                    class="h-7 w-7"
                  />
                </span>
                <div class="min-w-0 space-y-1">
                  <div class="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                    {{ t("dataQuality.lastResult") }}
                  </div>
                  <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                    <span
                      class="text-2xl font-semibold"
                      :class="latestTone.text"
                    >
                      {{ latestSummary.status ? t(qualityStatusKey(latestSummary.status)) : t("dataQuality.noResultYet") }}
                    </span>
                    <span
                      v-if="latestSummary.status"
                      class="text-sm text-muted-foreground"
                      :title="formatDateTime(latestSummary.finishTime, locale)"
                    >
                      {{ formatRelativeTime(latestSummary.finishTime, locale) }}
                    </span>
                  </div>
                  <p
                    v-if="latestSummary.status"
                    class="text-sm text-muted-foreground"
                  >
                    {{ t("dataQuality.observed") }}
                    <span class="font-medium text-foreground tabular-nums">
                      {{ latestSummary.observed }}
                    </span>
                    <span aria-hidden="true"> · </span>
                    {{ t("dataQuality.threshold") }}
                    <span class="tabular-nums">{{ latestSummary.threshold }}</span>
                    <span aria-hidden="true"> · </span>
                    {{ t("dataQuality.failedRows") }}
                    <span class="tabular-nums">{{ latestSummary.rows }}</span>
                  </p>
                  <p
                    v-else
                    class="text-sm text-muted-foreground"
                  >
                    {{ t("dataQuality.noResults") }}
                  </p>
                  <RouterLink
                    v-if="canViewExecution && latestSummary.execution"
                    :to="executionRoute(latestSummary.execution)"
                    class="inline-flex text-xs text-primary hover:underline"
                  >
                    {{ t("dataQuality.viewExecution") }}
                  </RouterLink>
                </div>
              </div>

              <div class="flex shrink-0 flex-col gap-3 lg:items-end">
                <div
                  v-if="passRate !== undefined"
                  class="text-right"
                >
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.passRate") }}
                  </div>
                  <div class="text-2xl font-semibold tabular-nums">
                    {{ t("dataQuality.passRateValue", { value: passRate }) }}
                  </div>
                </div>
                <div
                  v-if="statusCounts.length > 0"
                  class="flex flex-wrap gap-1 lg:justify-end"
                >
                  <Badge
                    v-for="count in statusCounts"
                    :key="count.status"
                    :variant="qualityStatusVariant(count.status)"
                    class="tabular-nums"
                  >
                    {{ t(qualityStatusKey(count.status)) }} · {{ count.value }}
                  </Badge>
                </div>
              </div>
            </div>

            <div
              v-if="executions.length > 0"
              class="space-y-2 border-t pt-4"
            >
              <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
                <span>{{ t("dataQuality.recentResultWindow", { count: executions.length }) }}</span>
                <span>{{ t("dataQuality.resultHistoryLabel") }}</span>
              </div>
              <QualityResultStrip
                :executions="executions"
                :limit="RECENT_RESULT_LIMIT"
                :label="t('dataQuality.resultHistoryLabel')"
                @select="openExecution"
              />
            </div>
          </CardContent>
        </Card>

        <div class="grid gap-4 lg:grid-cols-3">
          <Card class="lg:col-span-2">
            <CardHeader class="border-b">
              <CardTitle class="text-base">{{ t("dataQuality.recentExecutions") }}</CardTitle>
              <CardDescription>{{ t("dataQuality.recentExecutionsHint") }}</CardDescription>
            </CardHeader>
            <CardContent class="pt-4">
              <PageState
                :loading="executionsLoading"
                :error="executionsError"
              >
                <template #error>
                  <Button
                    variant="outline"
                    size="sm"
                    @click="loadExecutions"
                  >
                    {{ t("common.refresh") }}
                  </Button>
                </template>
                <EmptyState
                  v-if="executions.length === 0"
                  :icon="Activity"
                  :title="t('dataQuality.noExecutions')"
                >
                  <Button
                    v-if="canRun && rule.state !== QualityRuleState.ARCHIVED"
                    variant="outline"
                    size="sm"
                    @click="runNow"
                  >
                    <Zap class="mr-2 h-4 w-4" />
                    {{ t("dataQuality.runNow") }}
                  </Button>
                </EmptyState>
                <div v-else>
                  <Table class="[&_th]:whitespace-nowrap">
                    <TableHeader>
                      <TableRow>
                        <TableHead>{{ t("dataQuality.startedAt") }}</TableHead>
                        <TableHead>{{ t("dataQuality.resultStatus") }}</TableHead>
                        <TableHead class="hidden md:table-cell">
                          {{ t("dataQuality.observed") }}
                        </TableHead>
                        <TableHead class="hidden md:table-cell">
                          {{ t("dataQuality.threshold") }}
                        </TableHead>
                        <TableHead class="hidden whitespace-nowrap md:table-cell">
                          {{ t("dataQuality.failedRows") }}
                        </TableHead>
                        <TableHead class="hidden whitespace-nowrap md:table-cell">
                          {{ t("dataQuality.duration") }}
                        </TableHead>
                        <TableHead class="w-12 text-right">
                          <span class="sr-only">{{ t("dataQuality.viewExecution") }}</span>
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      <TableRow
                        v-for="execution in executions"
                        :key="execution.name"
                        class="cursor-pointer"
                        @click="openExecution(execution)"
                      >
                        <TableCell
                          class="whitespace-nowrap text-muted-foreground"
                          :title="formatDateTime(execution.startTime, locale)"
                        >
                          {{ formatRelativeTime(execution.startTime, locale) }}
                        </TableCell>
                        <TableCell>
                          <div class="flex flex-wrap items-center gap-1">
                            <Badge
                              :variant="qualityExecutionStateVariant(execution.state)"
                            >
                              {{ t(qualityExecutionStateKey(execution.state)) }}
                            </Badge>
                            <Badge
                              v-if="execution.outcome !== QualityStatus.UNSPECIFIED"
                              :variant="qualityStatusVariant(execution.outcome)"
                            >
                              {{ t(qualityStatusKey(execution.outcome)) }}
                            </Badge>
                            <Badge
                              v-if="execution.errorCode"
                              variant="danger"
                              :title="execution.errorMessage"
                            >
                              {{ errorLabel(execution) }}
                            </Badge>
                          </div>
                          <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                            <span class="tabular-nums">{{ observedLabel(execution) }}</span>
                            <span aria-hidden="true">·</span>
                            <span class="tabular-nums">{{ thresholdLabel(execution) }}</span>
                            <span aria-hidden="true">·</span>
                            <span class="tabular-nums">{{ rowsLabel(execution) }}</span>
                            <span aria-hidden="true">·</span>
                            <span>{{ durationLabel(execution) }}</span>
                          </div>
                        </TableCell>
                        <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                          {{ observedLabel(execution) }}
                        </TableCell>
                        <TableCell
                          class="hidden whitespace-nowrap tabular-nums md:table-cell"
                          :title="warnTitle(execution)"
                        >
                          {{ thresholdLabel(execution) }}
                        </TableCell>
                        <TableCell class="hidden tabular-nums md:table-cell">
                          {{ rowsLabel(execution) }}
                        </TableCell>
                        <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                          {{ durationLabel(execution) }}
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
                            @click="openExecution(execution)"
                          >
                            <Eye class="h-4 w-4 text-muted-foreground" />
                          </Button>
                        </TableCell>
                      </TableRow>
                    </TableBody>
                  </Table>
                </div>
              </PageState>
            </CardContent>
          </Card>

          <Card>
            <CardHeader class="border-b">
              <CardTitle class="text-base">{{ t("dataQuality.ruleDefinition") }}</CardTitle>
            </CardHeader>
            <CardContent class="pt-4">
              <dl class="grid gap-x-4 gap-y-3 text-sm sm:grid-cols-2 lg:grid-cols-1">
                <div>
                  <dt class="text-xs text-muted-foreground">
                    {{ t("dataQuality.ruleTarget") }}
                  </dt>
                  <dd class="mt-0.5 font-mono text-xs break-all">
                    <RouterLink
                      v-if="targetRoute"
                      :to="targetRoute"
                      :title="t('dataQuality.viewMetadata')"
                      class="inline-flex items-center gap-1 transition-colors hover:text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    >
                      {{ targetTitle }}
                      <ExternalLink class="h-3 w-3 shrink-0" />
                    </RouterLink>
                    <span v-else>{{ targetTitle }}</span>
                  </dd>
                </div>
                <div v-if="ruleColumns.length > 0">
                  <dt class="text-xs text-muted-foreground">
                    {{ t("dataQuality.columns") }}
                  </dt>
                  <dd class="mt-1 flex flex-wrap gap-1">
                    <Badge
                      v-for="column in ruleColumns"
                      :key="column"
                      variant="outline"
                      class="font-mono"
                    >
                      {{ column }}
                    </Badge>
                  </dd>
                </div>
                <div>
                  <dt class="text-xs text-muted-foreground">
                    {{ t("dataQuality.type") }}
                  </dt>
                  <dd class="mt-0.5">
                    {{ t(qualityRuleTypeKey(rule.ruleType)) }}
                  </dd>
                </div>
                <div>
                  <dt class="text-xs text-muted-foreground">
                    {{ t("dataQuality.filterDimension") }}
                  </dt>
                  <dd class="mt-0.5">
                    {{ t(qualityDimensionKey(rule.dimension)) }}
                  </dd>
                </div>
                <div>
                  <dt class="text-xs text-muted-foreground">
                    {{ t("dataQuality.filterSeverity") }}
                  </dt>
                  <dd class="mt-0.5">
                    <Badge :variant="qualitySeverityVariant(rule.severity)">
                      {{ t(qualitySeverityKey(rule.severity)) }}
                    </Badge>
                  </dd>
                </div>
                <div>
                  <dt class="text-xs text-muted-foreground">
                    {{ t("dataQuality.filterOwner") }}
                  </dt>
                  <dd
                    class="mt-0.5"
                    :class="{ 'text-muted-foreground': !rule.owner }"
                    :title="rule.owner || undefined"
                  >
                    {{ ownerName }}
                  </dd>
                </div>
                <div>
                  <dt class="text-xs text-muted-foreground">
                    {{ t("dataQuality.schedule") }}
                  </dt>
                  <dd class="mt-0.5 tabular-nums">
                    {{ formatScheduleInterval(rule.scheduleInterval) }}
                  </dd>
                </div>
                <div>
                  <dt class="text-xs text-muted-foreground">
                    {{ t("dataQuality.revision") }}
                  </dt>
                  <dd class="mt-0.5 tabular-nums">
                    {{ rule.revision }}
                  </dd>
                </div>
                <div>
                  <dt class="text-xs text-muted-foreground">
                    {{ t("dataQuality.updatedAt") }}
                  </dt>
                  <dd
                    class="mt-0.5"
                    :title="formatDateTime(rule.updateTime, locale)"
                  >
                    {{ formatRelativeTime(rule.updateTime, locale) }}
                  </dd>
                </div>
                <div>
                  <dt class="text-xs text-muted-foreground">
                    {{ t("dataQuality.createdAt") }}
                  </dt>
                  <dd
                    class="mt-0.5"
                    :title="formatDateTime(rule.createTime, locale)"
                  >
                    {{ formatRelativeTime(rule.createTime, locale) }}
                  </dd>
                </div>
                <div v-if="rule.tags.length > 0">
                  <dt class="text-xs text-muted-foreground">
                    {{ t("dataQuality.tags") }}
                  </dt>
                  <dd class="mt-1 flex flex-wrap gap-1">
                    <Badge
                      v-for="tag in rule.tags"
                      :key="tag"
                      variant="secondary"
                    >
                      {{ tag }}
                    </Badge>
                  </dd>
                </div>
              </dl>
              <Button
                v-if="canUpdate && rule.state !== QualityRuleState.ARCHIVED"
                variant="outline"
                size="sm"
                class="mt-4 w-full"
                @click="editRule"
              >
                <Pencil class="mr-2 h-4 w-4" />
                {{ t("dataQuality.editRule") }}
              </Button>
            </CardContent>
          </Card>
        </div>
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  BellOff,
  CheckCircle2,
  CircleAlert,
  ExternalLink,
  Eye,
  HelpCircle,
  MinusCircle,
  Pencil,
  RefreshCcw,
  Target,
  XCircle,
  Zap,
} from "lucide-vue-next";
import type { Component } from "vue";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute, useRouter } from "vue-router";
import {
  getQualityRule,
  listQualityExecutions,
  qualityRuleID,
  runQualityRules,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import QualityResultStrip from "@/components/data-quality/QualityResultStrip.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { useUserDirectory } from "@/composables/useUserDirectory";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityExecution,
  type QualityRule,
  QualityRuleState,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  describeThreshold,
  executionDurationSeconds,
  formatObservedValue,
  formatRowCount,
  formatScheduleInterval,
  formatSeconds,
  formatTargetPath,
  formatWarnThreshold,
  qualityDimensionKey,
  qualityErrorCodeKey,
  qualityExecutionStateKey,
  qualityExecutionStateVariant,
  qualityRuleStateKey,
  qualityRuleStateVariant,
  qualityRuleTypeKey,
  qualitySeverityKey,
  qualitySeverityVariant,
  qualityStatusKey,
  qualityStatusVariant,
  qualityTargetMetadataRoute,
  targetScopeLabel,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";

/**
 * One rule's own page: what it most recently found, how it has behaved over its
 * last executions, and the definition behind both. The page is readable by any
 * member (rules.get is in the member baseline); running and editing are gated.
 */

/** How many recent executions a detail read keeps: enough to see a trend. */
const RECENT_RESULT_LIMIT = 20;

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { formatError, handleError } = useErrorHandler();
const authStore = useAuthStore();

const canRun = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.run")
);
const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.update")
);
const canViewExecution = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.executions.get")
);

// Owner names come from the workspace directory; the definition falls back to
// the raw resource name when it is unavailable.
const { emailOf, load: loadUserDirectory } = useUserDirectory();

const rule = ref<QualityRule>();
// The workspace reads all of this rule's target's rules; it lives beside the
// rule so a reader can jump from one check to the picture it belongs to.
const targetWorkspaceTo = computed(() => {
  const target = rule.value?.target;
  if (!target?.metaGuid) {
    return undefined;
  }
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.metaGuid) },
    query: { metaType: String(target.metaType) },
  };
});
const executions = ref<QualityExecution[]>([]);
const isLoading = ref(true);
const executionsLoading = ref(true);
const error = ref("");
const executionsError = ref("");
const running = ref(false);

// One path segment per resource-name segment; joining restores the name a Get
// needs (Get cannot resolve the list RPC's `workspaces/-` shorthand).
const ruleName = computed(() => {
  const param = route.params.ruleId;
  return Array.isArray(param) ? param.join("/") : String(param ?? "");
});

const pageTitle = computed(() =>
  rule.value?.title
    ? rule.value.title
    : ruleName.value
      ? qualityRuleID(ruleName.value)
      : t("dataQuality.ruleDetailTitle")
);

/** The newest terminal execution, the one whose verdict the hero reports. */
const latest = computed(() =>
  executions.value.find(
    (execution) => execution.outcome !== QualityStatus.UNSPECIFIED
  )
);

/** The hero's facts, with the rule's stored last result as the fallback. */
const latestSummary = computed(() => {
  const execution = latest.value;
  if (execution) {
    const threshold = describeThreshold(
      execution.thresholds,
      execution.observedKind
    );
    return {
      status: execution.outcome,
      finishTime: execution.finishTime,
      observed: formatObservedValue(
        execution.observedValue,
        execution.observedKind,
        locale.value
      ),
      threshold: threshold ? t(threshold.key, threshold.params) : "-",
      rows: `${formatRowCount(execution.failedRows, locale.value)} / ${formatRowCount(execution.totalRows, locale.value)}`,
      execution: execution.name,
    };
  }
  const summary = rule.value?.lastResult;
  return {
    status: summary?.status,
    finishTime: summary?.finishTime,
    observed: summary
      ? formatObservedValue(
          summary.observedValue,
          summary.observedKind,
          locale.value
        )
      : "-",
    threshold: "-",
    rows: summary
      ? `${formatRowCount(summary.failedRows, locale.value)} / ${formatRowCount(summary.totalRows, locale.value)}`
      : "-",
    execution: summary?.execution,
  };
});

const targetTitle = computed(() => {
  const target = rule.value?.target;
  if (!target) {
    return "-";
  }
  const path = formatTargetPath(target.path);
  if (path === "-") {
    return target.metaGuid || "-";
  }
  const scope = targetScopeLabel(target.path);
  return scope === "-" ? path : `${scope} · ${path}`;
});

/** The target's metadata page; absent when the snapshot carries no GUID. */
const targetRoute = computed(() =>
  qualityTargetMetadataRoute(rule.value?.target)
);

const ruleColumns = computed(() => rule.value?.target?.columns ?? []);

/** The owner's email from the directory, the raw name if unknown, or unassigned. */
const ownerName = computed(() =>
  rule.value?.owner
    ? emailOf(rule.value.owner)
    : t("dataQuality.unassignedOwner")
);

const mutedUntil = computed(() => {
  const seconds = rule.value?.muteUntil?.seconds;
  return seconds !== undefined && Number(seconds) * 1000 > Date.now();
});

const STATUS_ORDER = [
  QualityStatus.PASS,
  QualityStatus.WARN,
  QualityStatus.FAIL,
  QualityStatus.ERROR,
  QualityStatus.SKIPPED,
  QualityStatus.NOT_EVALUATED,
];

/** The verdict tallies of the recent window, only the ones that occurred. */
const statusCounts = computed(() =>
  STATUS_ORDER.map((status) => ({
    status,
    value: terminalExecutions.value.filter(
      (execution) => execution.outcome === status
    ).length,
  })).filter((count) => count.value > 0)
);

/** Share of the recent window that passed; empty when there is no verdict. */
const passRate = computed(() => {
  const total = terminalExecutions.value.length;
  if (total === 0) {
    return undefined;
  }
  const passed = terminalExecutions.value.filter(
    (execution) => execution.outcome === QualityStatus.PASS
  ).length;
  return Math.round((passed / total) * 100);
});

interface StatusTone {
  icon: Component;
  /** The tinted circle behind the icon. */
  circle: string;
  /** The status word itself. */
  text: string;
}

function statusTone(status: QualityStatus | undefined): StatusTone {
  switch (status) {
    case QualityStatus.PASS:
      return {
        icon: CheckCircle2,
        circle: "bg-success-subtle text-success",
        text: "text-success",
      };
    case QualityStatus.WARN:
      return {
        icon: AlertTriangle,
        circle: "bg-warning-subtle text-warning",
        text: "text-warning",
      };
    case QualityStatus.FAIL:
      return {
        icon: XCircle,
        circle: "bg-destructive/10 text-destructive",
        text: "text-destructive",
      };
    case QualityStatus.ERROR:
      return {
        icon: CircleAlert,
        circle: "bg-danger-subtle text-danger",
        text: "text-danger",
      };
    case QualityStatus.SKIPPED:
      return {
        icon: MinusCircle,
        circle: "bg-muted text-muted-foreground",
        text: "text-muted-foreground",
      };
    case QualityStatus.NOT_EVALUATED:
      return {
        icon: HelpCircle,
        circle: "bg-muted text-muted-foreground",
        text: "text-muted-foreground",
      };
    default:
      return {
        icon: HelpCircle,
        circle: "bg-muted text-muted-foreground",
        text: "text-foreground",
      };
  }
}

const latestTone = computed(() => statusTone(latestSummary.value.status));

/** The executions that reached a terminal verdict, newest first. */
const terminalExecutions = computed(() =>
  executions.value.filter(
    (execution) => execution.outcome !== QualityStatus.UNSPECIFIED
  )
);

function hasVerdict(execution: QualityExecution): boolean {
  return execution.outcome !== QualityStatus.UNSPECIFIED;
}

function observedLabel(execution: QualityExecution): string {
  return hasVerdict(execution)
    ? formatObservedValue(
        execution.observedValue,
        execution.observedKind,
        locale.value
      )
    : "-";
}

function thresholdLabel(execution: QualityExecution): string {
  if (!hasVerdict(execution)) {
    return "-";
  }
  const threshold = describeThreshold(
    execution.thresholds,
    execution.observedKind
  );
  return threshold ? t(threshold.key, threshold.params) : "-";
}

function warnTitle(execution: QualityExecution): string | undefined {
  const warn = formatWarnThreshold(
    execution.thresholds,
    execution.observedKind
  );
  return warn ? t("dataQuality.warnAt", { value: warn }) : undefined;
}

function rowsLabel(execution: QualityExecution): string {
  return hasVerdict(execution)
    ? `${formatRowCount(execution.failedRows, locale.value)} / ${formatRowCount(execution.totalRows, locale.value)}`
    : "-";
}

function durationLabel(execution: QualityExecution): string {
  const seconds = executionDurationSeconds(execution);
  return seconds === undefined ? "-" : formatSeconds(seconds);
}

function errorLabel(execution: QualityExecution): string {
  const key = qualityErrorCodeKey(execution.errorCode);
  return key ? t(key) : execution.errorCode;
}

function executionRoute(name: string) {
  return {
    name: "DataQualityExecutionDetail",
    params: { executionId: name.split("/") },
  };
}

/** A click that ends a text selection is a copy, not a navigation. */
function openExecution(execution: QualityExecution) {
  if (window.getSelection()?.toString()) {
    return;
  }
  void router.push(executionRoute(execution.name));
}

function editRule() {
  void router.push({
    name: "DataQualityRuleEdit",
    params: { ruleId: ruleName.value.split("/") },
  });
}

async function loadExecutions() {
  if (!rule.value) {
    executions.value = [];
    executionsLoading.value = false;
    return;
  }
  executionsLoading.value = true;
  try {
    const page = await listQualityExecutions({
      rule: rule.value.name,
      pageSize: RECENT_RESULT_LIMIT,
    });
    executions.value = page.items;
    executionsError.value = "";
  } catch (e: unknown) {
    executions.value = [];
    executionsError.value = formatError(e, "dataQuality.fetchExecutionsError");
  } finally {
    executionsLoading.value = false;
  }
}

async function load() {
  isLoading.value = true;
  error.value = "";
  executionsError.value = "";
  try {
    rule.value = await getQualityRule(ruleName.value);
  } catch (e: unknown) {
    rule.value = undefined;
    error.value = formatError(e, "dataQuality.fetchRuleError");
    isLoading.value = false;
    executionsLoading.value = false;
    return;
  }
  isLoading.value = false;
  // A rule without an owner needs no directory; the lookup loads at most once.
  if (rule.value?.owner) {
    void loadUserDirectory();
  }
  await loadExecutions();
}

async function runNow() {
  if (!rule.value || running.value) {
    return;
  }
  running.value = true;
  try {
    const executions = await runQualityRules({ rules: [rule.value.name] });
    notify.success(t("dataQuality.executionStarted"));
    // One rule yields one execution; the page goes straight to it so the
    // PENDING → RUNNING → COMPLETED poll is visible. A wider selection (the
    // service can plan several) lands on the filtered list instead.
    if (executions.length === 1) {
      const [created] = executions;
      if (created) {
        void router.push(executionRoute(created.name));
        return;
      }
    }
    if (executions.length > 1) {
      void router.push({
        name: "DataQualityExecutions",
        query: { rule: rule.value.name },
      });
      return;
    }
    await loadExecutions();
  } catch (e: unknown) {
    handleError(e, "dataQuality.executionError");
  } finally {
    running.value = false;
  }
}

onMounted(() => {
  void load();
});

// Navigating from one rule to another reuses this component instance, so the
// data has to be refetched with the route.
watch(
  () => route.params.ruleId,
  () => {
    void load();
  }
);
</script>
