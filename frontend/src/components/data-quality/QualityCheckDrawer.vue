<template>
  <Dialog
    :open="open"
    @update:open="(value: boolean) => emit('update:open', value)"
  >
    <DialogContent
      class="left-auto right-0 top-0 h-dvh max-h-dvh w-full max-w-2xl translate-x-0 translate-y-0 gap-0 overflow-hidden rounded-none border-l p-0 sm:max-w-2xl sm:rounded-none"
    >
      <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
        <DialogHeader class="shrink-0 space-y-2 border-b px-6 py-5">
          <DialogTitle class="truncate">
            {{ ruleTitle }}
          </DialogTitle>
          <DialogDescription class="flex flex-wrap items-center gap-x-2 gap-y-1">
            <template v-if="rule">
              <Badge variant="secondary">
                {{ t(qualityRuleTypeKey(rule.ruleType)) }}
              </Badge>
              <Badge :variant="qualitySeverityVariant(rule.severity)">
                {{ t(qualitySeverityKey(rule.severity)) }}
              </Badge>
              <Badge :variant="qualityRuleStateVariant(rule.state)">
                {{ t(qualityRuleStateKey(rule.state)) }}
              </Badge>
              <RouterLink
                v-if="targetRoute"
                :to="targetRoute"
                :title="t('dataQuality.viewMetadata')"
                class="inline-flex items-center gap-1 font-mono text-xs break-all transition-colors hover:text-primary hover:underline"
              >
                {{ targetTitle }}
                <ExternalLink class="h-3 w-3 shrink-0" />
              </RouterLink>
              <span v-else class="font-mono text-xs break-all">{{ targetTitle }}</span>
              <span class="text-xs text-muted-foreground">
                {{ t("dataQuality.revision") }} {{ rule.revision }}
              </span>
            </template>
            <template v-else>
              {{ t("dataQuality.ruleDetailTitle") }}
            </template>
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-4 overflow-auto px-6 py-5">
          <EmptyState
            v-if="!canView"
            :icon="Lock"
            :title="t('error.permissionDenied')"
          />
          <PageState
            v-else
            :loading="loading"
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
              <Alert
                v-else-if="rule.targetSchemaChanged"
                variant="warning"
              >
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

              <!-- The verdict first: one big status, then the recent history as
                   a strip a reader scans in one glance, then the counts. -->
              <Card>
                <CardContent class="space-y-4 pt-6">
                  <div class="flex flex-col gap-5 sm:flex-row sm:items-start sm:justify-between">
                    <div class="flex min-w-0 items-start gap-4">
                      <span
                        class="grid h-12 w-12 shrink-0 place-items-center rounded-full"
                        :class="latestTone.circle"
                      >
                        <component
                          :is="latestTone.icon"
                          class="h-6 w-6"
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

                    <div class="flex shrink-0 flex-col gap-2 sm:max-w-[45%] sm:items-end">
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
                        class="flex flex-wrap gap-1 sm:justify-end"
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
                    v-if="executions.length > 1"
                    class="space-y-2 border-t pt-4"
                  >
                    <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
                      <span>{{ t("dataQuality.recentResultWindow", { count: executions.length }) }}</span>
                      <span>{{ t("dataQuality.resultHistoryLabel") }}</span>
                    </div>
                    <QualityResultStrip
                      :executions="executions"
                      :limit="RECENT_EXECUTION_LIMIT"
                      :label="t('dataQuality.resultHistoryLabel')"
                      @select="openExecution"
                    />
                  </div>
                </CardContent>
              </Card>

              <Card v-if="canListExecutions">
                <CardHeader class="border-b">
                  <CardTitle class="text-base">
                    {{ t("dataQuality.recentExecutions") }}
                  </CardTitle>
                </CardHeader>
                <CardContent class="pt-4">
                  <p
                    v-if="executionsError"
                    class="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm break-words text-destructive"
                  >
                    {{ executionsError }}
                  </p>
                  <EmptyState
                    v-else-if="executions.length === 0"
                    :icon="Activity"
                    :title="t('dataQuality.noExecutions')"
                  />
                  <Table v-else>
                    <TableHeader>
                      <TableRow>
                        <TableHead>
                          {{ t("dataQuality.startedAt") }}
                        </TableHead>
                        <TableHead>
                          {{ t("dataQuality.resultStatus") }}
                        </TableHead>
                        <TableHead class="hidden sm:table-cell">
                          {{ t("dataQuality.observed") }}
                        </TableHead>
                        <TableHead class="hidden sm:table-cell">
                          {{ t("dataQuality.failedRows") }}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      <TableRow
                        v-for="execution in executions"
                        :key="execution.name"
                      >
                        <TableCell class="whitespace-nowrap">
                          <RouterLink
                            v-if="canViewExecution"
                            :to="executionRoute(execution.name)"
                            :title="formatDateTime(execution.startTime, locale)"
                            class="text-primary hover:underline"
                          >
                            {{ formatRelativeTime(execution.startTime, locale) }}
                          </RouterLink>
                          <span
                            v-else
                            :title="formatDateTime(execution.startTime, locale)"
                          >
                            {{ formatRelativeTime(execution.startTime, locale) }}
                          </span>
                          <div class="mt-0.5 text-xs text-muted-foreground">
                            {{ executionMeta(execution) }}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div class="flex flex-wrap items-center gap-1">
                            <Badge :variant="executionBadge(execution).variant">
                              {{ t(executionBadge(execution).key) }}
                            </Badge>
                            <Badge
                              v-if="execution.errorCode"
                              variant="danger"
                              :title="execution.errorMessage"
                            >
                              {{ errorLabel(execution) }}
                            </Badge>
                          </div>
                        </TableCell>
                        <TableCell class="hidden whitespace-nowrap tabular-nums sm:table-cell">
                          {{ observedLabel(execution) }}
                        </TableCell>
                        <TableCell class="hidden tabular-nums sm:table-cell">
                          {{ rowsLabel(execution) }}
                        </TableCell>
                      </TableRow>
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>

              <Card>
                <CardHeader class="border-b">
                  <CardTitle class="text-base">
                    {{ t("dataQuality.ruleDefinition") }}
                  </CardTitle>
                </CardHeader>
                <CardContent class="pt-4">
                  <dl class="grid gap-x-4 gap-y-3 text-sm sm:grid-cols-2">
                    <div>
                      <dt class="text-xs text-muted-foreground">
                        {{ t("dataQuality.ruleTarget") }}
                      </dt>
                      <dd class="mt-0.5 font-mono text-xs break-all">
                        {{ targetTitle }}
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
                        {{ t("dataQuality.editor.filter") }}
                      </dt>
                      <dd class="mt-0.5 break-words">
                        {{ filterText }}
                      </dd>
                    </div>
                    <div v-if="rule && isOnEmptyRelevant(rule.ruleType)">
                      <dt class="text-xs text-muted-foreground">
                        {{ t("dataQuality.editor.onEmpty") }}
                      </dt>
                      <dd class="mt-0.5">
                        {{ t(qualityEmptyBehaviorKey(rule.config?.onEmpty ?? 0)) }}
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
                    <div
                      v-if="rule.tags.length > 0"
                      class="sm:col-span-2"
                    >
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
                </CardContent>
              </Card>
            </div>
          </PageState>
        </div>

        <DialogFooter class="shrink-0 flex-row flex-wrap items-center justify-end gap-2 border-t px-6 py-4">
          <Button
            v-if="canRun && !archived"
            :disabled="running"
            @click="runNow"
          >
            <Loader2
              v-if="running"
              class="mr-2 h-4 w-4 animate-spin"
            />
            <Zap
              v-else
              class="mr-2 h-4 w-4"
            />
            {{ t("dataQuality.runNow") }}
          </Button>
          <Button
            v-if="canExplain && latestFailedExecution"
            variant="outline"
            @click="explainOpen = true"
          >
            <Sparkles class="mr-2 h-4 w-4" />
            {{ t("dataQuality.llm.explain.action") }}
          </Button>
          <Button
            v-if="canUpdate && !archived"
            variant="outline"
            :disabled="changingState"
            @click="togglePause"
          >
            <component
              :is="paused ? Play : Pause"
              class="mr-2 h-4 w-4"
            />
            {{ paused ? t("dataQuality.resumeRule") : t("dataQuality.pauseRule") }}
          </Button>
          <Button
            v-if="canUpdate && !archived"
            variant="outline"
            @click="edit"
          >
            <Pencil class="mr-2 h-4 w-4" />
            {{ t("dataQuality.editRule") }}
          </Button>
          <Button
            v-if="canDelete && !archived"
            variant="outline"
            class="text-destructive"
            @click="confirmDelete = true"
          >
            <Trash2 class="mr-2 h-4 w-4" />
            {{ t("common.delete") }}
          </Button>
          <Button
            variant="ghost"
            @click="close"
          >
            {{ t("common.close") }}
          </Button>
        </DialogFooter>
      </div>

      <QualityExplainDrawer
        v-model:open="explainOpen"
        :execution="latestFailedExecution"
        :execution-title="ruleTitle"
      />

      <ConfirmDeleteDialog
        v-model="confirmDelete"
        :title="t('dataQuality.deleteRuleTitle')"
        :message="t('dataQuality.deleteRuleMessage')"
        :item-name="ruleTitle"
        :loading="deleting"
        @confirm="remove"
      />
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import {
  Activity,
  AlertTriangle,
  BellOff,
  CheckCircle2,
  CircleAlert,
  ExternalLink,
  HelpCircle,
  Loader2,
  Lock,
  MinusCircle,
  Pause,
  Pencil,
  Play,
  Sparkles,
  Trash2,
  XCircle,
  Zap,
} from "lucide-vue-next";
import type { Component } from "vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRouter } from "vue-router";
import {
  deleteQualityRule,
  getQualityExecution,
  getQualityRule,
  listQualityExecutions,
  runQualityRules,
  updateQualityRule,
} from "@/api/data-quality";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import QualityExplainDrawer from "@/components/data-quality/QualityExplainDrawer.vue";
import QualityResultStrip from "@/components/data-quality/QualityResultStrip.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useDataQualityPermissions } from "@/composables/useDataQualityPermissions";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { usePolling } from "@/composables/usePolling";
import { useUserDirectory } from "@/composables/useUserDirectory";
import { notify } from "@/lib/notify";
import {
  type QualityExecution,
  QualityExecutionState,
  type QualityRule,
  QualityRuleSchema,
  QualityRuleState,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  describeThreshold,
  executionDurationSeconds,
  formatObservedValue,
  formatRowCount,
  formatSeconds,
  formatTargetPath,
  qualityDimensionKey,
  qualityEmptyBehaviorKey,
  qualityErrorCodeKey,
  qualityExecutionStateKey,
  qualityExecutionStateVariant,
  qualityExecutionTriggerKey,
  qualityFilterOperatorKey,
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
import { isOnEmptyRelevant } from "@/utils/dataQualityForm";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * One check's own side sheet: what it last found, how it has behaved over its
 * latest executions, and the definition behind both.
 *
 * This is the old rule page rebuilt for the target workspace: opening a row no
 * longer navigates away from its siblings, so a reader can compare one check
 * against the target it belongs to and close the sheet to keep their place.
 * Writes return the server's rule and emit `changed`, so the workspace adjusts
 * its row and counts without re-reading it.
 */

/** How many recent executions the sheet reads and shows. */
const RECENT_EXECUTION_LIMIT = 20;

/** How often a manual run's planned executions are polled, and for how long. */
const RUN_POLL_INTERVAL_MS = 2000;
const RUN_POLL_TIMEOUT_MS = 60_000;

const props = defineProps<{
  open: boolean;
  /** The check's full resource name. */
  ruleName: string;
  /** The workspace target the check belongs to. */
  targetGuid: string;
  metaType: MetaType;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  /** Start editing the check in the workspace composer. */
  edit: [rule: QualityRule];
  /** A write returned the check's new state. */
  changed: [rule: QualityRule];
  /** The check was archived; the workspace drops its row. */
  deleted: [name: string];
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const router = useRouter();
const { emailOf, load: loadUserDirectory } = useUserDirectory();

const {
  canViewRule: canView,
  canRun,
  canUpdate,
  canDelete,
  canViewExecution,
  canListExecutions,
  canExplain,
} = useDataQualityPermissions();

const rule = ref<QualityRule>();
const executions = ref<QualityExecution[]>([]);
const loading = ref(false);
const error = ref("");
const executionsError = ref("");
const running = ref(false);
const changingState = ref(false);
const deleting = ref(false);
const confirmDelete = ref(false);
const explainOpen = ref(false);

/** Guards a slow read of the previous check landing on the new one. */
let loadTicket = 0;

/** The executions the wait in flight planned; refilled per manual run. */
let pendingRunNames: string[] = [];
let runSettled = false;
/** True once the sheet is gone; a finishing run must then not paint or emit. */
let unmounted = false;

const runPolling = usePolling(
  async (signal) => {
    const settled = await Promise.all(
      pendingRunNames.map((name) => getQualityExecution(name, { signal }))
    );
    runSettled = settled.every(isExecutionSettled);
  },
  {
    intervalMs: RUN_POLL_INTERVAL_MS,
    shouldContinue: () => !runSettled,
    timeoutMs: RUN_POLL_TIMEOUT_MS,
    // A failed poll is not a failed run: keep trying until the deadline, and
    // still let the caller refresh after a timeout.
    continueOnError: true,
  }
);

/** Closing, switching or unmounting cancels the wait; `until()` then resolves. */
function stopPolling() {
  runPolling.stop();
}

/** A planned run is settled once the runner has completed or abandoned it. */
function isExecutionSettled(execution: QualityExecution): boolean {
  return (
    execution.state === QualityExecutionState.COMPLETED ||
    execution.state === QualityExecutionState.ABANDONED
  );
}

/**
 * Wait until every execution a manual run planned has a verdict.
 *
 * `runQualityRules` only plans executions: the response names them PENDING (or
 * RUNNING) and carries no verdict. Polling until they settle is what makes the
 * re-read that follows show the result this click produced rather than the
 * previous one. A failed poll is not a failed run: the wait keeps trying until
 * the deadline, and a timeout still lets the caller refresh.
 */
function waitForRun(names: string[]): Promise<void> {
  pendingRunNames = names;
  runSettled = false;
  runPolling.start();
  return runPolling.until();
}

const ruleTitle = computed(
  () =>
    rule.value?.title ||
    props.ruleName.split("/").pop() ||
    t("dataQuality.ruleDetailTitle")
);

const archived = computed(
  () => rule.value?.state === QualityRuleState.ARCHIVED
);
const paused = computed(() => rule.value?.state === QualityRuleState.PAUSED);
const ruleColumns = computed(() => rule.value?.target?.columns ?? []);

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

const targetRoute = computed(() =>
  qualityTargetMetadataRoute(rule.value?.target)
);

const ownerName = computed(() =>
  rule.value?.owner
    ? emailOf(rule.value.owner)
    : t("dataQuality.unassignedOwner")
);

const mutedUntil = computed(() => {
  const seconds = rule.value?.muteUntil?.seconds;
  return seconds !== undefined && Number(seconds) * 1000 > Date.now();
});

/** The newest terminal execution, the one whose verdict the sheet reports. */
const latest = computed(() =>
  executions.value.find(
    (execution) => execution.outcome !== QualityStatus.UNSPECIFIED
  )
);

/** The newest execution that failed, for the AI explanation. */
const latestFailedExecution = computed(() =>
  executions.value.find(
    (execution) =>
      execution.outcome === QualityStatus.FAIL ||
      execution.outcome === QualityStatus.ERROR
  )
);

/** The hero's facts, with the check's stored last result as the fallback. */
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

const STATUS_ORDER = [
  QualityStatus.PASS,
  QualityStatus.WARN,
  QualityStatus.FAIL,
  QualityStatus.ERROR,
  QualityStatus.SKIPPED,
  QualityStatus.NOT_EVALUATED,
];

/** The executions that reached a terminal verdict, newest first. */
const terminalExecutions = computed(() =>
  executions.value.filter(
    (execution) => execution.outcome !== QualityStatus.UNSPECIFIED
  )
);

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

/** The rule's pre-filter as one readable line, or the "no filter" wording. */
const filterText = computed(() => {
  const clauses = rule.value?.config?.filter?.clauses ?? [];
  if (clauses.length === 0) {
    return t("dataQuality.editor.noFilters");
  }
  return clauses
    .map((clause) => {
      const values =
        clause.values.length > 0 ? ` ${clause.values.join(", ")}` : "";
      return `${clause.column} ${t(qualityFilterOperatorKey(clause.op))}${values}`;
    })
    .join(" · ");
});

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

function rowsLabel(execution: QualityExecution): string {
  return hasVerdict(execution)
    ? `${formatRowCount(execution.failedRows, locale.value)} / ${formatRowCount(execution.totalRows, locale.value)}`
    : "-";
}

/** How the execution was triggered, plus how long it took once it finished. */
function executionMeta(execution: QualityExecution): string {
  const trigger = t(qualityExecutionTriggerKey(execution.trigger));
  const seconds = executionDurationSeconds(execution);
  return seconds === undefined
    ? trigger
    : `${trigger} · ${formatSeconds(seconds)}`;
}

/**
 * One badge per execution: the verdict once it has run, the lifecycle state
 * before that. Printing "Completed" next to "Fail" would say the same thing
 * twice and, in the sheet's narrow table, wrap the two onto separate lines.
 */
function executionBadge(execution: QualityExecution) {
  return hasVerdict(execution)
    ? {
        key: qualityStatusKey(execution.outcome),
        variant: qualityStatusVariant(execution.outcome),
      }
    : {
        key: qualityExecutionStateKey(execution.state),
        variant: qualityExecutionStateVariant(execution.state),
      };
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

/** A strip click is navigation, so it needs the execution's own permission. */
function openExecution(execution: QualityExecution) {
  if (!canViewExecution.value) {
    return;
  }
  if (window.getSelection()?.toString()) {
    return;
  }
  void router.push(executionRoute(execution.name));
}

async function loadExecutions(name: string, ticket: number) {
  if (!canListExecutions.value) {
    return;
  }
  try {
    const page = await listQualityExecutions({
      rule: name,
      pageSize: RECENT_EXECUTION_LIMIT,
    });
    if (ticket !== loadTicket) {
      return;
    }
    executions.value = page.items;
    executionsError.value = "";
  } catch (cause: unknown) {
    if (ticket !== loadTicket) {
      return;
    }
    executions.value = [];
    executionsError.value = formatError(
      cause,
      "dataQuality.fetchExecutionsError"
    );
  }
}

async function load() {
  if (!props.open || !canView.value) {
    return;
  }
  const name = props.ruleName;
  if (!name) {
    return;
  }
  const ticket = ++loadTicket;
  loading.value = true;
  error.value = "";
  executionsError.value = "";
  rule.value = undefined;
  executions.value = [];
  const executionsRead = loadExecutions(name, ticket);
  try {
    const loaded = await getQualityRule(name);
    if (ticket !== loadTicket) {
      return;
    }
    rule.value = loaded;
    // Owner names come from the workspace directory; a check without an owner
    // needs no lookup.
    if (loaded.owner) {
      void loadUserDirectory();
    }
  } catch (cause: unknown) {
    if (ticket !== loadTicket) {
      return;
    }
    rule.value = undefined;
    error.value = formatError(cause, "dataQuality.fetchRuleError");
  } finally {
    if (ticket === loadTicket) {
      loading.value = false;
    }
  }
  await executionsRead;
}

watch(
  () => [props.open, props.ruleName] as const,
  ([isOpen]) => {
    stopPolling();
    if (isOpen) {
      void load();
    }
  },
  { immediate: true }
);

onBeforeUnmount(() => {
  unmounted = true;
  stopPolling();
});

function close() {
  emit("update:open", false);
}

async function runNow() {
  if (!rule.value || running.value) {
    return;
  }
  const name = rule.value.name;
  running.value = true;
  try {
    const planned = await runQualityRules({ rules: [name] });
    notify.success(t("dataQuality.executionStarted"));
    // The call only plans the run. Wait for the runner to settle the plan before
    // re-reading, otherwise the refreshed last result is still the old verdict.
    const unsettled = planned
      .filter((execution) => !isExecutionSettled(execution))
      .map((execution) => execution.name)
      .filter(Boolean);
    if (unsettled.length > 0) {
      await waitForRun(unsettled);
    }
    // Closing, switching or unmounting the sheet cancels the wait; do not paint
    // a workspace that is no longer showing this check.
    if (unmounted || !props.open || props.ruleName !== name) {
      return;
    }
    await load();
    if (rule.value) {
      emit("changed", rule.value);
    }
  } catch (cause: unknown) {
    notify.error(formatError(cause, "dataQuality.executionError"));
  } finally {
    if (!unmounted) {
      running.value = false;
    }
  }
}

/** Pause or resume by writing just the state, then hand the rule back. */
async function togglePause() {
  if (!rule.value || changingState.value) {
    return;
  }
  const resume = paused.value;
  const next = create(QualityRuleSchema, rule.value);
  next.state = resume ? QualityRuleState.ENABLED : QualityRuleState.PAUSED;
  changingState.value = true;
  try {
    const updated = await updateQualityRule(next, ["state"]);
    rule.value = updated;
    notify.success(
      t(resume ? "dataQuality.ruleEnabled" : "dataQuality.rulePaused")
    );
    emit("changed", updated);
  } catch (cause: unknown) {
    notify.error(formatError(cause, "dataQuality.editor.saveFailed"));
  } finally {
    changingState.value = false;
  }
}

function edit() {
  if (!rule.value) {
    return;
  }
  emit("edit", rule.value);
  close();
}

async function remove() {
  if (!rule.value || deleting.value) {
    return;
  }
  deleting.value = true;
  try {
    await deleteQualityRule(rule.value.name);
    notify.success(t("dataQuality.ruleDeleted"));
    confirmDelete.value = false;
    emit("deleted", rule.value.name);
    close();
  } catch (cause: unknown) {
    notify.error(formatError(cause, "dataQuality.deleteError"));
  } finally {
    deleting.value = false;
  }
}
</script>
