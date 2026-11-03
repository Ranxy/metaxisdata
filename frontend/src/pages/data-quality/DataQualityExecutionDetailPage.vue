<template>
  <div class="space-y-4">
    <PageHeader :title="pageTitle">
      <template #breadcrumb>
        <RouterLink
          :to="{ name: 'DataQualityExecutions' }"
          class="inline-flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft class="h-3.5 w-3.5" />
          {{ t("dataQuality.executionsTitle") }}
        </RouterLink>
      </template>
      <template #title-extra>
        <template v-if="execution">
          <Badge :variant="qualityExecutionStateVariant(execution.state)">
            {{ t(qualityExecutionStateKey(execution.state)) }}
          </Badge>
          <Badge
            v-if="execution.outcome !== QualityStatus.UNSPECIFIED"
            :variant="qualityStatusVariant(execution.outcome)"
          >
            {{ t(qualityStatusKey(execution.outcome)) }}
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
          v-if="showExplain"
          :disabled="isLoading"
          @click="openExplain"
        >
          <Sparkles class="mr-2 h-4 w-4" />
          {{ t("dataQuality.llm.explain.action") }}
        </Button>
        <Button
          variant="outline"
          :disabled="isLoading"
          @click="reload"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': isLoading || isPolling }"
          />
          {{ t("common.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <PageState
      :loading="isLoading && !execution"
      :error="error"
    >
      <template #error>
        <Button
          variant="outline"
          size="sm"
          @click="reload"
        >
          {{ t("common.refresh") }}
        </Button>
      </template>

      <div
        v-if="execution"
        class="space-y-4"
      >
        <Card>
          <CardContent class="space-y-5 pt-6">
            <div class="flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
              <div class="flex min-w-0 items-start gap-4">
                <span
                  class="grid h-14 w-14 shrink-0 place-items-center rounded-full"
                  :class="tone.circle"
                >
                  <component
                    :is="tone.icon"
                    class="h-7 w-7"
                    :class="{ 'animate-spin': running }"
                  />
                </span>
                <div class="min-w-0 space-y-1">
                  <div class="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                    {{ t("dataQuality.outcome") }}
                  </div>
                  <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                    <span
                      class="text-2xl font-semibold"
                      :class="tone.text"
                    >
                      {{ verdictLabel }}
                    </span>
                    <span
                      class="text-sm text-muted-foreground"
                      :title="formatDateTime(execution.finishTime ?? execution.startTime, locale)"
                    >
                      {{
                        formatRelativeTime(
                          execution.finishTime ?? execution.startTime,
                          locale
                        )
                      }}
                    </span>
                  </div>
                  <p
                    v-if="hasVerdict"
                    class="text-sm text-muted-foreground"
                  >
                    {{ t("dataQuality.observed") }}
                    <span class="font-medium text-foreground tabular-nums">
                      {{ observedLabel }}
                    </span>
                    <span aria-hidden="true"> · </span>
                    {{ t("dataQuality.threshold") }}
                    <span
                      class="tabular-nums"
                      :title="warnTitle"
                    >{{ thresholdLabel }}</span>
                    <span aria-hidden="true"> · </span>
                    {{ t("dataQuality.failedRows") }}
                    <span class="tabular-nums">{{ rowsLabel }}</span>
                  </p>
                  <p
                    v-else-if="abandoned"
                    class="text-sm text-muted-foreground"
                  >
                    {{ t("dataQuality.executionAbandonedHint") }}
                  </p>
                  <p
                    v-else
                    class="text-sm text-muted-foreground"
                  >
                    {{ t("dataQuality.executionInProgressHint") }}
                  </p>
                  <div class="flex flex-wrap items-center gap-1">
                    <Badge
                      v-if="execution.sampled"
                      variant="outline"
                      :title="t('dataQuality.sampled')"
                    >
                      {{ t("dataQuality.sampled") }}
                    </Badge>
                    <Badge
                      v-if="execution.truncated"
                      variant="warning"
                      :title="t('dataQuality.truncated')"
                    >
                      {{ t("dataQuality.truncated") }}
                    </Badge>
                  </div>
                </div>
              </div>

              <div class="grid w-full shrink-0 gap-3 sm:grid-cols-2 lg:w-auto lg:grid-cols-2">
                <div class="min-w-0">
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.rule") }}
                  </div>
                  <RouterLink
                    v-if="canViewRule"
                    :to="ruleRoute(execution.rule)"
                    class="block truncate text-sm font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
                    :title="ruleLabel"
                  >
                    {{ ruleLabel }}
                  </RouterLink>
                  <div
                    v-else
                    class="truncate text-sm font-medium"
                    :title="ruleLabel"
                  >
                    {{ ruleLabel }}
                  </div>
                </div>
                <div class="min-w-0">
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.ruleTarget") }}
                  </div>
                  <div
                    class="truncate font-mono text-xs"
                    :title="targetTitle"
                  >
                    {{ targetTitle }}
                  </div>
                </div>
                <div>
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.trigger") }}
                  </div>
                  <div class="text-sm">
                    {{ t(qualityExecutionTriggerKey(execution.trigger)) }}
                  </div>
                </div>
                <div>
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.instance") }}
                  </div>
                  <div
                    class="truncate font-mono text-sm"
                    :title="execution.instance || undefined"
                  >
                    {{ instanceLabel }}
                  </div>
                </div>
                <div>
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.initiator") }}
                  </div>
                  <div
                    class="truncate text-sm"
                    :title="initiatorLabel"
                  >
                    {{ initiatorLabel }}
                  </div>
                </div>
                <div>
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.duration") }}
                  </div>
                  <div class="text-sm tabular-nums">{{ durationLabel }}</div>
                </div>
                <div>
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.revision") }}
                  </div>
                  <div class="text-sm tabular-nums">
                    {{ execution.ruleRevision }}
                  </div>
                </div>
                <div v-if="isRowCountChange">
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.baseline.label") }}
                  </div>
                  <div class="text-sm tabular-nums">
                    {{ baselineLabel }}
                  </div>
                </div>
                <div v-if="execution.attempt > 1">
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.attempt") }}
                  </div>
                  <div class="text-sm tabular-nums">
                    {{ t("dataQuality.executions.attempt", { count: execution.attempt }) }}
                  </div>
                </div>
                <div>
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.engineVersion") }}
                  </div>
                  <div
                    class="truncate font-mono text-xs"
                    :title="execution.ruleEngineVersion || undefined"
                  >
                    {{ execution.ruleEngineVersion || "-" }}
                  </div>
                </div>
                <div>
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.startedAt") }}
                  </div>
                  <div
                    class="text-sm"
                    :title="formatDateTime(execution.startTime, locale)"
                  >
                    {{ formatRelativeTime(execution.startTime, locale) }}
                  </div>
                </div>
                <div>
                  <div class="text-xs text-muted-foreground">
                    {{ t("dataQuality.finishedAt") }}
                  </div>
                  <div
                    class="text-sm"
                    :title="formatDateTime(execution.finishTime, locale)"
                  >
                    {{ formatRelativeTime(execution.finishTime, locale) }}
                  </div>
                </div>
              </div>
            </div>

            <!-- While the execution can still change the page polls; saying so
                 is what makes a status that does not move for a few seconds
                 read as work in progress rather than a stuck page. -->
            <p
              v-if="inProgress"
              class="flex items-center gap-2 text-xs text-muted-foreground"
            >
              <Loader2 class="h-3 w-3 animate-spin" />
              {{ t("dataQuality.polling") }}
            </p>
          </CardContent>
        </Card>

        <Alert
          v-if="execution.errorCode || execution.errorMessage"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle class="flex flex-wrap items-center gap-2">
            <Badge
              v-if="execution.errorCode"
              variant="danger"
            >
              {{ errorLabel }}
            </Badge>
            <span>{{ t("dataQuality.error") }}</span>
          </AlertTitle>
          <AlertDescription class="break-words">
            {{ execution.errorMessage }}
          </AlertDescription>
        </Alert>

        <!-- Only a failed or warned verdict has an outcome worth explaining;
             the root-cause window then ends at the run's start time, so a
             candidate is evidence from before this run, not after it. -->
        <QualityImpactCard
          v-if="impactTarget"
          :target-guid="impactTarget.targetGuid"
          :meta-type="impactTarget.metaType"
          :as-of="execution.startTime"
        />

        <!-- A segment check's evidence is the segment card below: it keeps no
             failed-row samples, so it gets no empty failed-rows card. -->
        <template v-if="showsFailedSamples">
          <Card v-if="execution.samples.length > 0">
            <CardHeader class="border-b">
              <CardTitle class="text-base">
                {{ t("dataQuality.failureSamples") }}
              </CardTitle>
              <CardDescription>
                {{ t("dataQuality.failureSamplesHint") }}
              </CardDescription>
            </CardHeader>
            <CardContent class="pt-4">
              <ExecutionSampleRows :samples="execution.samples" />
            </CardContent>
          </Card>
          <Card v-else>
            <CardHeader class="border-b">
              <CardTitle class="text-base">
                {{ t("dataQuality.failureSamples") }}
              </CardTitle>
            </CardHeader>
            <CardContent class="pt-4">
              <p class="text-sm text-muted-foreground">
                {{ t("dataQuality.noSamples") }}
              </p>
            </CardContent>
          </Card>
        </template>

        <!-- The rows that passed the same check, kept beside the failed ones so
             a reviewer can compare what the rule lets through with what it
             rejects. Only some row-level checks and only a WARN/FAIL verdict
             carry them, so an empty list renders no card at all. -->
        <Card v-if="passingSamples.length > 0">
          <CardHeader class="border-b">
            <CardTitle class="text-base">
              {{ t("dataQuality.passingSamples") }}
            </CardTitle>
            <CardDescription>
              {{ t("dataQuality.passingSamplesHint") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="pt-4">
            <ExecutionSampleRows :samples="passingSamples" />
          </CardContent>
        </Card>

        <!-- A segment check judges the segment set, so its evidence is the set
             it observed compared with the baseline: what disappeared (the
             failures), what is new, and how large the comparison was. -->
        <Card v-if="segmentEvidence">
          <CardHeader class="border-b">
            <CardTitle class="text-base">
              {{ t("dataQuality.segmentEvidence.title") }}
            </CardTitle>
            <CardDescription>
              {{ t("dataQuality.segmentEvidence.hint") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-5 pt-4">
            <section class="space-y-2">
              <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <h3 class="text-sm font-medium">
                  {{ t("dataQuality.segmentEvidence.disappeared") }}
                </h3>
                <span class="text-xs text-muted-foreground tabular-nums">
                  {{
                    t("dataQuality.segmentEvidence.segmentsCount", {
                      count: disappearedSegments.length,
                    })
                  }}
                </span>
              </div>
              <p class="text-xs text-muted-foreground">
                {{ t("dataQuality.segmentEvidence.disappearedHint") }}
              </p>
              <p
                v-if="disappearedSegments.length === 0"
                class="text-sm text-muted-foreground"
              >
                {{ t("dataQuality.segmentEvidence.noDisappeared") }}
              </p>
              <div
                v-else
                class="space-y-2"
              >
                <div
                  v-for="(record, index) in disappearedSegments"
                  :key="index"
                  class="flex flex-wrap items-start justify-between gap-2 rounded-md border p-2.5 text-xs"
                >
                  <span
                    class="min-w-0 break-all font-mono"
                    :title="segmentText(record)"
                  >
                    {{ segmentText(record) }}
                  </span>
                  <span class="flex shrink-0 items-center gap-3 text-muted-foreground tabular-nums">
                    <span>
                      {{
                        t("dataQuality.segmentEvidence.rowCount", {
                          count: formatRowCount(record.rowCount, locale),
                        })
                      }}
                    </span>
                    <span
                      v-if="record.lastSeen"
                      :title="formatDateTime(record.lastSeen, locale)"
                    >
                      {{
                        t("dataQuality.segmentEvidence.lastSeen", {
                          time: formatRelativeTime(record.lastSeen, locale),
                        })
                      }}
                    </span>
                  </span>
                </div>
              </div>
            </section>

            <section class="space-y-2">
              <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <h3 class="text-sm font-medium">
                  {{ t("dataQuality.segmentEvidence.newSegments") }}
                </h3>
                <span class="text-xs text-muted-foreground tabular-nums">
                  {{
                    t("dataQuality.segmentEvidence.segmentsCount", {
                      count: newSegments.length,
                    })
                  }}
                </span>
              </div>
              <p class="text-xs text-muted-foreground">
                {{ t("dataQuality.segmentEvidence.newSegmentsHint") }}
              </p>
              <p
                v-if="newSegments.length === 0"
                class="text-sm text-muted-foreground"
              >
                {{ t("dataQuality.segmentEvidence.noNewSegments") }}
              </p>
              <div
                v-else
                class="space-y-2"
              >
                <div
                  v-for="(record, index) in newSegments"
                  :key="index"
                  class="flex flex-wrap items-start justify-between gap-2 rounded-md border p-2.5 text-xs"
                >
                  <span
                    class="min-w-0 break-all font-mono"
                    :title="segmentText(record)"
                  >
                    {{ segmentText(record) }}
                  </span>
                  <span class="flex shrink-0 items-center text-muted-foreground tabular-nums">
                    {{
                      t("dataQuality.segmentEvidence.rowCount", {
                        count: formatRowCount(record.rowCount, locale),
                      })
                    }}
                  </span>
                </div>
              </div>
            </section>

            <p
              v-if="segmentBaselineLabel"
              class="border-t pt-3 text-xs text-muted-foreground"
            >
              {{ segmentBaselineLabel }}
            </p>
            <p
              v-if="segmentEvidence.truncated"
              class="text-xs text-warning"
            >
              {{ t("dataQuality.segmentEvidence.truncated") }}
            </p>
          </CardContent>
        </Card>

        <!-- A reconciliation over a separate reference connection runs two
             statements; the second is the reference side's, kept in the same
             card as the statement it belongs with. -->
        <Card v-if="execution.executedSql || execution.referenceExecutedSql">
          <CardHeader class="flex flex-row items-center justify-between gap-3 border-b">
            <CardTitle class="text-base">
              {{ t("dataQuality.executedSql") }}
            </CardTitle>
            <Button
              v-if="execution.executedSql"
              variant="outline"
              size="sm"
              @click="copySql(execution.executedSql)"
            >
              <Copy class="mr-2 h-3.5 w-3.5" />
              {{ t("dataQuality.copySql") }}
            </Button>
          </CardHeader>
          <CardContent class="space-y-3 pt-4">
            <pre v-if="execution.executedSql" class="rounded-md border bg-muted p-3 text-xs whitespace-pre-wrap wrap-anywhere">{{ execution.executedSql }}</pre>
            <div
              v-if="execution.referenceExecutedSql"
              class="space-y-2"
            >
              <div class="flex items-center justify-between gap-2">
                <span class="text-sm font-medium">
                  {{ t("dataQuality.referenceExecutedSql") }}
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  @click="copySql(execution.referenceExecutedSql)"
                >
                  <Copy class="mr-2 h-3.5 w-3.5" />
                  {{ t("dataQuality.copySql") }}
                </Button>
              </div>
              <pre class="rounded-md border bg-muted p-3 text-xs whitespace-pre-wrap wrap-anywhere">{{ execution.referenceExecutedSql }}</pre>
            </div>
          </CardContent>
        </Card>
      </div>
    </PageState>

    <QualityExplainDrawer
      v-model:open="explainOpen"
      :execution="execution"
      :execution-title="pageTitle"
    />
  </div>
</template>

<script setup lang="ts">
import {
  AlertTriangle,
  ArrowLeft,
  CheckCircle2,
  CircleAlert,
  Clock,
  Copy,
  HelpCircle,
  Loader2,
  RefreshCcw,
  Sparkles,
  Target,
  XCircle,
} from "lucide-vue-next";
import type { Component } from "vue";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import {
  getQualityExecution,
  getQualityRule,
  qualityRuleID,
} from "@/api/data-quality";
import { getWorkspaceProfileSetting } from "@/api/setting";
import PageState from "@/components/common/PageState.vue";
import ExecutionSampleRows from "@/components/data-quality/ExecutionSampleRows.vue";
import QualityExplainDrawer from "@/components/data-quality/QualityExplainDrawer.vue";
import QualityImpactCard from "@/components/data-quality/QualityImpactCard.vue";
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
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePolling } from "@/composables/usePolling";
import { useUserDirectory } from "@/composables/useUserDirectory";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityExecution,
  QualityExecutionState,
  QualityRuleType,
  QualityStatus,
  type SegmentRecord,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  describeThreshold,
  executionDurationSeconds,
  formatObservedValue,
  formatRowCount,
  formatSeconds,
  formatTargetPath,
  formatWarnThreshold,
  isExecutionInProgress,
  qualityErrorCodeKey,
  qualityExecutionStateKey,
  qualityExecutionStateVariant,
  qualityExecutionTriggerKey,
  qualityStatusKey,
  qualityStatusVariant,
  targetScopeLabel,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";

/**
 * One execution's full record: the verdict, the rule and target it judged, the
 * observed value against the snapshotted thresholds, the rows behind it, the
 * failure samples, the SQL, and — when the check could not run at all — the
 * error. The page polls until the execution is final, so a row opened straight
 * from "Run now" fills in rather than staying PENDING.
 */

/** How often an execution that can still change is reloaded. */
const POLL_INTERVAL_MS = 3000;

const { t, locale } = useI18n();
const route = useRoute();
const { formatError } = useErrorHandler();
const userDirectory = useUserDirectory();
const authStore = useAuthStore();

const execution = ref<QualityExecution>();
// The workspace groups every rule on the execution's target; it is the natural
// next step from one piece of evidence.
const targetWorkspaceTo = computed(() => {
  const target = execution.value?.target;
  if (!target?.metaGuid) {
    return undefined;
  }
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.metaGuid) },
    query: {
      metaType: String(target.metaType),
      highlight: execution.value?.rule ?? "",
    },
  };
});
/**
 * The target the impact card reads, when the verdict makes its lineage worth
 * explaining (FAIL or WARN) and the target still carries an address. A passing
 * run needs no downstream/cause story and an errored check never produced a
 * data verdict, so neither gets the card.
 */
const impactTarget = computed(() => {
  const current = execution.value;
  if (
    !current ||
    isExecutionInProgress(current.state) ||
    (current.outcome !== QualityStatus.FAIL &&
      current.outcome !== QualityStatus.WARN)
  ) {
    return undefined;
  }
  const target = current.target;
  if (!target?.metaGuid || target.metaType === MetaType.UNSPECIFIED) {
    return undefined;
  }
  return { targetGuid: target.metaGuid, metaType: target.metaType };
});
const ruleTitle = ref("");
/** The check's type; only the execution's own record cannot tell it. */
const ruleType = ref<QualityRuleType>();
const isLoading = ref(true);
const isPolling = ref(false);
const error = ref("");
const explainOpen = ref(false);
const llmEnabled = ref(false);

const canExplain = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.explain")
);
const canViewRule = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.get")
);

/** Identifies the newest page read; an older answer must never write state. */
let loadTicket = 0;

// One path segment per resource-name segment; joining restores the name a Get
// needs (Get cannot resolve the list RPC's `workspaces/-` shorthand).
const executionName = computed(() => {
  const param = route.params.executionId;
  return Array.isArray(param) ? param.join("/") : String(param ?? "");
});

const pageTitle = computed(() =>
  ruleTitle.value
    ? ruleTitle.value
    : execution.value?.rule
      ? qualityRuleID(execution.value.rule)
      : t("dataQuality.executionDetailTitle")
);

const ruleLabel = computed(() =>
  ruleTitle.value
    ? ruleTitle.value
    : execution.value?.rule
      ? qualityRuleID(execution.value.rule)
      : "-"
);

const inProgress = computed(
  () =>
    execution.value !== undefined &&
    isExecutionInProgress(execution.value.state)
);

const running = computed(
  () => execution.value?.state === QualityExecutionState.RUNNING
);

const abandoned = computed(
  () => execution.value?.state === QualityExecutionState.ABANDONED
);

const hasVerdict = computed(
  () =>
    execution.value !== undefined &&
    execution.value.outcome !== QualityStatus.UNSPECIFIED
);

const verdictLabel = computed(() => {
  if (!execution.value) {
    return "";
  }
  return hasVerdict.value
    ? t(qualityStatusKey(execution.value.outcome))
    : t(qualityExecutionStateKey(execution.value.state));
});

const observedLabel = computed(() =>
  execution.value && hasVerdict.value
    ? formatObservedValue(
        execution.value.observedValue,
        execution.value.observedKind,
        locale.value
      )
    : "-"
);

/** A count-change execution compares against a baseline; the page names it. */
const isRowCountChange = computed(
  () => ruleType.value === QualityRuleType.ROW_COUNT_CHANGE
);

const baselineLabel = computed(() => {
  const current = execution.value;
  if (!current || current.baselineValue === undefined) {
    return t("dataQuality.baseline.missing");
  }
  return t("dataQuality.baseline.value", {
    value: formatRowCount(current.baselineValue, locale.value),
    time: formatDateTime(current.baselineTime, locale.value),
  });
});

const rowsLabel = computed(() =>
  execution.value && hasVerdict.value
    ? `${formatRowCount(execution.value.failedRows, locale.value)} / ${formatRowCount(execution.value.totalRows, locale.value)}`
    : "-"
);

/** The rows that passed the same check; empty for every check type that has none. */
const passingSamples = computed(() => execution.value?.passingSamples ?? []);

/** A segment check's evidence: the observed set compared with its baseline. */
const segmentEvidence = computed(() => execution.value?.segmentEvidence);

/**
 * A segment check's verdict is the disappeared-segment count, and its evidence
 * is the segment card; it keeps no failed-row samples, so the failed-rows card
 * is hidden. The type comes from the rule read, but the evidence alone proves
 * the type when the caller may not read the rule.
 */
const isSegmentCheck = computed(
  () =>
    ruleType.value === QualityRuleType.SEGMENT_CHECK ||
    segmentEvidence.value !== undefined
);
const showsFailedSamples = computed(() => !isSegmentCheck.value);

const disappearedSegments = computed(
  () => segmentEvidence.value?.disappeared ?? []
);
const newSegments = computed(() => segmentEvidence.value?.newSegments ?? []);

/** How the baseline was sized, or why the comparison was skipped. */
const segmentBaselineLabel = computed(() => {
  const evidence = segmentEvidence.value;
  if (!evidence) {
    return "";
  }
  if (evidence.baselineRuns === 0) {
    // A truncated observation records the set but never judges it, so it says
    // that instead of pretending there was no baseline at all.
    return evidence.truncated
      ? ""
      : t("dataQuality.segmentEvidence.noBaseline");
  }
  return t("dataQuality.segmentEvidence.baseline", {
    runs: formatRowCount(evidence.baselineRuns, locale.value),
    expected: formatRowCount(evidence.expectedCount, locale.value),
  });
});

/**
 * One segment as display text: the grouping values in rule order, with the same
 * NULL / empty / masked wording the failure samples use.
 */
function segmentText(record: SegmentRecord): string {
  return record.values
    .map((value, index) => {
      if (record.isNull[index] === true) {
        return t("dataQuality.sampleNull");
      }
      if (value === "") {
        return t("dataQuality.sampleEmpty");
      }
      return record.masked[index] === true
        ? `${value} (${t("dataQuality.sampleMasked")})`
        : value;
    })
    .join(" · ");
}

const thresholdLabel = computed(() => {
  if (!execution.value) {
    return "-";
  }
  const threshold = describeThreshold(
    execution.value.thresholds,
    execution.value.observedKind
  );
  return threshold ? t(threshold.key, threshold.params) : "-";
});

const warnTitle = computed(() => {
  if (!execution.value) {
    return undefined;
  }
  const warn = formatWarnThreshold(
    execution.value.thresholds,
    execution.value.observedKind
  );
  return warn ? t("dataQuality.warnAt", { value: warn }) : undefined;
});

const durationLabel = computed(() => {
  if (!execution.value) {
    return "-";
  }
  const seconds = executionDurationSeconds(execution.value);
  return seconds === undefined ? "-" : formatSeconds(seconds);
});

const instanceLabel = computed(() =>
  execution.value?.instance
    ? (execution.value.instance.split("/").pop() ?? "-")
    : "-"
);

const initiatorLabel = computed(() =>
  execution.value?.initiator
    ? userDirectory.emailOf(execution.value.initiator)
    : "-"
);

const targetTitle = computed(() => {
  const target = execution.value?.target;
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

const errorLabel = computed(() => {
  const code = execution.value?.errorCode ?? "";
  const key = qualityErrorCodeKey(code);
  return key ? t(key) : code;
});

interface StatusTone {
  icon: Component;
  /** The tinted circle behind the icon. */
  circle: string;
  /** The status word itself. */
  text: string;
}

const PENDING_TONE: StatusTone = {
  icon: Clock,
  circle: "bg-info-subtle text-info",
  text: "text-info",
};

const NEUTRAL_TONE: StatusTone = {
  icon: HelpCircle,
  circle: "bg-muted text-muted-foreground",
  text: "text-muted-foreground",
};

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
    case QualityStatus.NOT_EVALUATED:
      return NEUTRAL_TONE;
    default:
      return NEUTRAL_TONE;
  }
}

const tone = computed<StatusTone>(() => {
  if (inProgress.value) {
    return PENDING_TONE;
  }
  if (hasVerdict.value) {
    return statusTone(execution.value?.outcome);
  }
  return NEUTRAL_TONE;
});

/** Only a failed or errored verdict is worth explaining. */
const showExplain = computed(
  () =>
    canExplain.value &&
    llmEnabled.value &&
    hasVerdict.value &&
    (execution.value?.outcome === QualityStatus.FAIL ||
      execution.value?.outcome === QualityStatus.ERROR)
);

/**
 * Loads the execution. `notifyErrors` is false for a poll: a page that already
 * shows an error must not raise a toast every three seconds.
 */
async function load(notifyErrors: boolean, signal?: AbortSignal) {
  const ticket = ++loadTicket;
  try {
    const loaded = await getQualityExecution(executionName.value, { signal });
    if (ticket !== loadTicket) {
      return;
    }
    execution.value = loaded;
    error.value = "";
    if (loaded.initiator) {
      void userDirectory.load();
    }
    if (loaded.rule) {
      void loadRuleTitle(loaded.rule);
    }
  } catch (e: unknown) {
    if (ticket !== loadTicket) {
      return;
    }
    if (notifyErrors) {
      error.value = formatError(e, "dataQuality.fetchExecutionError");
    }
  }
  if (inProgress.value) {
    startPolling();
  } else {
    stopPolling();
  }
}

/** The first paint and a route change: clear first, then load with the spinner. */
async function initialLoad() {
  isLoading.value = true;
  execution.value = undefined;
  ruleTitle.value = "";
  ruleType.value = undefined;
  error.value = "";
  stopPolling();
  await Promise.all([load(true), loadLlmSetting()]);
  isLoading.value = false;
}

/** The manual refresh button: reload and keep the toast on failure. */
async function reload() {
  isLoading.value = true;
  await load(true);
  isLoading.value = false;
}

/**
 * The poll is silent. A transient failure stops it rather than raising a toast
 * per attempt; the next manual refresh or route visit starts it again.
 */
const polling = usePolling(
  async (signal) => {
    isPolling.value = true;
    try {
      await load(false, signal);
    } finally {
      isPolling.value = false;
    }
  },
  {
    intervalMs: POLL_INTERVAL_MS,
    shouldContinue: () => inProgress.value,
  }
);

function startPolling() {
  polling.start();
}

function stopPolling() {
  polling.stop();
}

/** Best effort: a caller without rules.get still sees the execution by rule id. */
async function loadRuleTitle(name: string) {
  try {
    const rule = await getQualityRule(name);
    ruleTitle.value = rule.title || qualityRuleID(name);
    ruleType.value = rule.ruleType;
  } catch {
    ruleTitle.value = qualityRuleID(name);
  }
}

/** Only read the workspace switch; the settings page owns changing it. */
async function loadLlmSetting() {
  try {
    const profile = await getWorkspaceProfileSetting();
    llmEnabled.value = profile.dataQuality?.enableLlmAnalysis ?? false;
  } catch {
    llmEnabled.value = false;
  }
}

/** A check is opened inside its target's contract workspace (the old per-rule
 * page is gone); a stray link without a target falls back to the redirect. */
function ruleRoute(name: string) {
  const target = execution.value?.target;
  if (target?.metaGuid) {
    return {
      name: "DataQualityTarget",
      params: { targetGuid: guidToRouteParams(target.metaGuid) },
      query: { metaType: String(target.metaType), check: name },
    };
  }
  return {
    name: "DataQualityRuleRedirect",
    params: { ruleId: name.split("/") },
  };
}

function openExplain() {
  explainOpen.value = true;
}

async function copySql(sql: string) {
  if (!navigator.clipboard) {
    return;
  }
  await navigator.clipboard.writeText(sql);
  notify.success(t("common.copied"));
}

onMounted(initialLoad);

// A direct navigation from one execution to another reuses this component
// instance, so the timers and the data must be reset with the route.
watch(
  () => route.params.executionId,
  () => {
    void initialLoad();
  }
);
</script>
