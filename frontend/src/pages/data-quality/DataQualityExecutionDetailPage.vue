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
                    class="truncate font-mono text-sm"
                    :title="execution.initiator || undefined"
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
            <!-- One block per sampled row, not a table: the columns are
                 data-dependent, and a wide table is what the layout audit
                 rejects. A row is numbered because two failures on the same
                 column can otherwise look like the same row repeated, and a
                 NULL or an empty string is printed as such: a blank cell would
                 read as a rendering defect. -->
            <div class="space-y-2">
              <div
                v-for="(sample, index) in execution.samples"
                :key="index"
                class="rounded-md border p-3 text-xs"
              >
                <div class="mb-1 font-mono text-muted-foreground">
                  {{ t("dataQuality.sampleRow", { index: index + 1 }) }}
                </div>
                <div
                  v-for="cell in sample.columns"
                  :key="cell.column"
                  class="flex gap-2 py-0.5"
                >
                  <span
                    class="w-40 shrink-0 truncate font-mono text-muted-foreground"
                    :title="cell.column"
                  >
                    {{ cell.column }}
                  </span>
                  <span
                    v-if="cell.isNull"
                    class="min-w-0 font-mono text-muted-foreground italic"
                  >
                    {{ t("dataQuality.sampleNull") }}
                  </span>
                  <span
                    v-else-if="cell.value === ''"
                    class="min-w-0 font-mono text-muted-foreground italic"
                  >
                    {{ t("dataQuality.sampleEmpty") }}
                  </span>
                  <span
                    v-else
                    class="min-w-0 break-all"
                  >{{ cell.value }}</span>
                </div>
              </div>
            </div>
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

        <Card v-if="execution.executedSql">
          <CardHeader class="flex flex-row items-center justify-between gap-3 border-b">
            <CardTitle class="text-base">
              {{ t("dataQuality.executedSql") }}
            </CardTitle>
            <Button
              variant="outline"
              size="sm"
              @click="copySql(execution.executedSql)"
            >
              <Copy class="mr-2 h-3.5 w-3.5" />
              {{ t("dataQuality.copySql") }}
            </Button>
          </CardHeader>
          <CardContent class="pt-4">
            <pre class="rounded-md border bg-muted p-3 text-xs whitespace-pre-wrap wrap-anywhere">{{ execution.executedSql }}</pre>
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
  XCircle,
} from "lucide-vue-next";
import type { Component } from "vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import {
  getQualityExecution,
  getQualityRule,
  qualityRuleID,
} from "@/api/data-quality";
import { getWorkspaceProfileSetting } from "@/api/setting";
import PageState from "@/components/common/PageState.vue";
import QualityExplainDrawer from "@/components/data-quality/QualityExplainDrawer.vue";
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
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityExecution,
  QualityExecutionState,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
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
const authStore = useAuthStore();

const execution = ref<QualityExecution>();
const ruleTitle = ref("");
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

let pollTimer: number | undefined;
let loading = false;

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

const rowsLabel = computed(() =>
  execution.value && hasVerdict.value
    ? `${formatRowCount(execution.value.failedRows, locale.value)} / ${formatRowCount(execution.value.totalRows, locale.value)}`
    : "-"
);

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
    ? (execution.value.initiator.split("/").pop() ?? "-")
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
async function load(notifyErrors: boolean) {
  if (loading) {
    return;
  }
  loading = true;
  try {
    const loaded = await getQualityExecution(executionName.value);
    execution.value = loaded;
    error.value = "";
    if (loaded.rule) {
      void loadRuleTitle(loaded.rule);
    }
  } catch (e: unknown) {
    if (notifyErrors) {
      error.value = formatError(e, "dataQuality.fetchExecutionError");
    }
  } finally {
    loading = false;
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
async function poll() {
  if (isPolling.value || loading) {
    return;
  }
  isPolling.value = true;
  try {
    await load(false);
  } finally {
    isPolling.value = false;
  }
  if (!inProgress.value) {
    stopPolling();
  }
}

function startPolling() {
  if (pollTimer !== undefined) {
    return;
  }
  pollTimer = window.setInterval(() => {
    void poll();
  }, POLL_INTERVAL_MS);
}

function stopPolling() {
  if (pollTimer !== undefined) {
    clearInterval(pollTimer);
    pollTimer = undefined;
  }
}

/** Best effort: a caller without rules.get still sees the execution by rule id. */
async function loadRuleTitle(name: string) {
  try {
    const rule = await getQualityRule(name);
    ruleTitle.value = rule.title || qualityRuleID(name);
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

function ruleRoute(name: string) {
  return {
    name: "DataQualityRuleDetail",
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

onBeforeUnmount(stopPolling);

// A direct navigation from one execution to another reuses this component
// instance, so the timers and the data must be reset with the route.
watch(
  () => route.params.executionId,
  () => {
    void initialLoad();
  }
);
</script>
