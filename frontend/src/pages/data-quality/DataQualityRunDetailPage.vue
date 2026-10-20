<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.runDetailTitle')"
    >
      <template #breadcrumb>
        <RouterLink
          :to="{ name: 'DataQualityRuns' }"
          class="inline-flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft class="h-3.5 w-3.5" />
          {{ t("dataQuality.runsTitle") }}
        </RouterLink>
      </template>
      <template #title-extra>
        <Badge
          v-if="run"
          :variant="qualityRunStatusVariant(run.status)"
        >
          {{ t(qualityRunStatusKey(run.status)) }}
        </Badge>
      </template>
      <template #actions>
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

    <Card v-if="run">
      <CardContent class="space-y-4 pt-6">
        <div class="flex flex-wrap items-center gap-2">
          <Badge
            v-if="run.outcome !== QualityStatus.UNSPECIFIED"
            :variant="qualityStatusVariant(run.outcome)"
          >
            {{ t(qualityStatusKey(run.outcome)) }}
          </Badge>
          <Badge variant="outline">
            {{ t(qualityRunTriggerKey(run.trigger)) }}
          </Badge>
          <span class="font-mono text-xs text-muted-foreground">
            {{ instanceLabel }}
          </span>
        </div>

        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <div class="text-xs text-muted-foreground">
              {{ t("dataQuality.startedAt") }}
            </div>
            <div
              class="text-sm"
              :title="formatDateTime(run.startTime, locale)"
            >
              {{ formatRelativeTime(run.startTime, locale) }}
            </div>
          </div>
          <div>
            <div class="text-xs text-muted-foreground">
              {{ t("dataQuality.finishedAt") }}
            </div>
            <div
              class="text-sm"
              :title="formatDateTime(run.finishTime, locale)"
            >
              {{ formatRelativeTime(run.finishTime, locale) }}
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
              {{ t("dataQuality.initiator") }}
            </div>
            <div class="truncate font-mono text-sm">{{ initiatorLabel }}</div>
          </div>
        </div>

        <div class="flex flex-wrap items-center gap-2 border-t pt-4">
          <span class="text-sm text-muted-foreground">
            {{ t("dataQuality.results") }}
          </span>
          <Badge
            v-for="count in allCounts"
            :key="count.status"
            :variant="
              count.value > 0 ? qualityStatusVariant(count.status) : 'neutral'
            "
            :title="t(qualityStatusKey(count.status))"
            class="tabular-nums"
          >
            {{ count.value }}
          </Badge>
        </div>

        <!-- While the run can still change the page polls; saying so is what
             makes a status that does not move for a few seconds read as work in
             progress rather than a stuck page. -->
        <p
          v-if="inProgress"
          class="flex items-center gap-2 text-xs text-muted-foreground"
        >
          <Loader2 class="h-3 w-3 animate-spin" />
          {{ t("dataQuality.polling") }}
        </p>
      </CardContent>
    </Card>

    <Card>
      <CardHeader class="border-b">
        <CardTitle class="text-base">{{ t("dataQuality.resultsTitle") }}</CardTitle>
        <CardDescription>{{ t("dataQuality.resultsDescription") }}</CardDescription>
      </CardHeader>
      <CardContent class="pt-6">
        <PageState
          v-if="error"
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
        </PageState>
        <PageState
          v-else
          :loading="resultsLoading"
          :error="resultsError"
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
          <EmptyState
            v-if="results.length === 0"
            :icon="ListChecks"
            :title="t('dataQuality.noResults')"
            :description="inProgress ? t('dataQuality.polling') : ''"
          />
          <Table
            v-else
            class="[&_th]:whitespace-nowrap"
          >
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("dataQuality.rule") }}</TableHead>
                <TableHead class="hidden sm:table-cell">
                  {{ t("dataQuality.resultStatus") }}
                </TableHead>
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
                <TableHead class="hidden lg:table-cell">
                  {{ t("dataQuality.error") }}
                </TableHead>
                <TableHead class="w-12 text-right">
                  <span class="sr-only">{{ t("common.details") }}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <template
                v-for="result in results"
                :key="result.name"
              >
                <TableRow>
                  <TableCell>
                    <div
                      class="max-w-[11rem] truncate font-medium sm:max-w-[14rem] md:max-w-sm"
                      :title="ruleLabel(result)"
                    >
                      {{ ruleLabel(result) }}
                    </div>
                    <div class="mt-1 flex flex-wrap items-center gap-1">
                      <Badge
                        v-if="result.sampled"
                        variant="outline"
                        :title="t('dataQuality.sampled')"
                      >
                        {{ t("dataQuality.sampled") }}
                      </Badge>
                      <Badge
                        v-if="result.truncated"
                        variant="warning"
                        :title="t('dataQuality.truncated')"
                      >
                        {{ t("dataQuality.truncated") }}
                      </Badge>
                    </div>
                    <!-- Below md the observed, threshold, failed-rows and
                         duration columns collapse in here: the reader keeps the
                         facts without the table growing a horizontal
                         scrollbar. -->
                    <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                      <Badge :variant="qualityStatusVariant(result.status)">
                        {{ t(qualityStatusKey(result.status)) }}
                      </Badge>
                      <span class="tabular-nums">
                        {{
                          formatObservedValue(
                            result.observedValue,
                            result.observedKind,
                            locale
                          )
                        }}
                      </span>
                      <span aria-hidden="true">·</span>
                      <span class="tabular-nums">{{ thresholdLabel(result) }}</span>
                      <span aria-hidden="true">·</span>
                      <span>{{ formatRowCount(result.failedRows, locale) }} / {{ formatRowCount(result.totalRows, locale) }}</span>
                      <span aria-hidden="true">·</span>
                      <span>{{ formatDuration(result.duration) }}</span>
                    </div>
                  </TableCell>
                  <TableCell class="hidden sm:table-cell">
                    <Badge :variant="qualityStatusVariant(result.status)">
                      {{ t(qualityStatusKey(result.status)) }}
                    </Badge>
                  </TableCell>
                  <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                    {{
                      formatObservedValue(
                        result.observedValue,
                        result.observedKind,
                        locale
                      )
                    }}
                  </TableCell>
                  <TableCell
                    class="hidden whitespace-nowrap tabular-nums md:table-cell"
                    :title="warnTitle(result)"
                  >
                    {{ thresholdLabel(result) }}
                  </TableCell>
                  <TableCell class="hidden tabular-nums md:table-cell">
                    {{ formatRowCount(result.failedRows, locale) }}
                    <span class="text-muted-foreground">
                      / {{ formatRowCount(result.totalRows, locale) }}
                    </span>
                  </TableCell>
                  <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                    {{ formatDuration(result.duration) }}
                  </TableCell>
                  <TableCell class="hidden max-w-48 lg:table-cell">
                    <span
                      v-if="result.errorMessage"
                      class="block truncate text-xs text-destructive"
                      :title="result.errorMessage"
                    >
                      {{ errorLabel(result) }}
                    </span>
                    <span
                      v-else
                      class="text-sm text-muted-foreground"
                    >
                      -
                    </span>
                  </TableCell>
                  <TableCell class="text-right">
                    <Button
                      v-if="showExplain(result)"
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :title="t('dataQuality.llm.explain.action')"
                      :aria-label="t('dataQuality.llm.explain.action')"
                      @click="openExplain(result)"
                    >
                      <Sparkles class="h-4 w-4 text-primary" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :title="
                        isExpanded(result.name)
                          ? t('dataQuality.hideDetails')
                          : t('dataQuality.showDetails')
                      "
                      :aria-label="
                        isExpanded(result.name)
                          ? t('dataQuality.hideDetails')
                          : t('dataQuality.showDetails')
                      "
                      @click="toggleExpanded(result.name)"
                    >
                      <ChevronDown
                        :class="[
                          'h-4 w-4 transition-transform',
                          isExpanded(result.name) && 'rotate-180',
                        ]"
                      />
                    </Button>
                  </TableCell>
                </TableRow>
                <TableRow v-if="isExpanded(result.name)">
                  <TableCell :colspan="8">
                    <div class="space-y-4">
                      <div v-if="result.errorCode || result.errorMessage">
                        <div class="text-xs font-medium text-muted-foreground">
                          {{ t("dataQuality.error") }}
                        </div>
                        <p class="mt-1 text-sm">
                          <Badge
                            v-if="result.errorCode"
                            variant="danger"
                            class="mr-2"
                          >
                            {{ errorLabel(result) }}
                          </Badge>
                          <span class="break-words">{{ result.errorMessage }}</span>
                        </p>
                      </div>

                      <div v-if="result.samples.length > 0">
                        <div class="text-xs font-medium text-muted-foreground">
                          {{ t("dataQuality.failureSamples") }}
                        </div>
                        <!-- One block per sampled row, not a table: the columns
                             are data-dependent, and a wide table is what the
                             layout audit rejects. -->
                        <div class="mt-1 space-y-2">
                          <div
                            v-for="(sample, index) in result.samples"
                            :key="index"
                            class="rounded-md border p-3 text-xs"
                          >
                            <div
                              v-for="cell in sample.columns"
                              :key="cell.column"
                              class="flex gap-2 py-0.5"
                            >
                              <span class="w-40 shrink-0 truncate font-mono text-muted-foreground">
                                {{ cell.column }}
                              </span>
                              <span class="min-w-0 break-all">{{ cell.value }}</span>
                            </div>
                          </div>
                        </div>
                      </div>
                      <p
                        v-else
                        class="text-xs text-muted-foreground"
                      >
                        {{ t("dataQuality.noSamples") }}
                      </p>

                      <div v-if="result.executedSql">
                        <div class="flex flex-wrap items-center justify-between gap-2">
                          <div class="text-xs font-medium text-muted-foreground">
                            {{ t("dataQuality.executedSql") }}
                          </div>
                          <Button
                            variant="outline"
                            size="sm"
                            @click="copySql(result.executedSql)"
                          >
                            <Copy class="mr-2 h-3.5 w-3.5" />
                            {{ t("dataQuality.copySql") }}
                          </Button>
                        </div>
                        <pre class="mt-1 rounded-md border bg-muted p-3 text-xs whitespace-pre-wrap wrap-anywhere">{{ result.executedSql }}</pre>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              </template>
            </TableBody>
          </Table>
        </PageState>
      </CardContent>
    </Card>

    <QualityExplainDrawer
      v-model:open="explainOpen"
      :result="explainResult"
      :result-title="explainResult ? ruleLabel(explainResult) : ''"
    />
  </div>
</template>

<script setup lang="ts">
import {
  ArrowLeft,
  ChevronDown,
  Copy,
  ListChecks,
  Loader2,
  RefreshCcw,
  Sparkles,
} from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import {
  getQualityRun,
  listAllQualityRuleResults,
  listAllQualityRules,
  qualityRuleID,
} from "@/api/data-quality";
import { getWorkspaceProfileSetting } from "@/api/setting";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import QualityExplainDrawer from "@/components/data-quality/QualityExplainDrawer.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
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
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityRuleResult,
  type QualityRun,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  describeThreshold,
  formatDuration,
  formatObservedValue,
  formatRowCount,
  formatSeconds,
  formatWarnThreshold,
  isRunInProgress,
  qualityErrorCodeKey,
  qualityRunStatusKey,
  qualityRunStatusVariant,
  qualityRunTriggerKey,
  qualityStatusKey,
  qualityStatusVariant,
  runDurationSeconds,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/** How often a run that can still change is reloaded. */
const POLL_INTERVAL_MS = 3000;

const { t, locale } = useI18n();
const route = useRoute();
const { handleError, formatError } = useErrorHandler();
const authStore = useAuthStore();

const run = ref<QualityRun>();
const results = ref<QualityRuleResult[]>([]);
const ruleTitles = ref<Record<string, string>>({});
const isLoading = ref(true);
const resultsLoading = ref(true);
const isPolling = ref(false);
const error = ref("");
const resultsError = ref("");
const expanded = ref<string[]>([]);
const llmEnabled = ref(false);
const explainOpen = ref(false);
const explainResult = ref<QualityRuleResult>();
const canExplain = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.explain")
);

let pollTimer: number | undefined;
let loading = false;

// One path segment per resource-name segment; joining restores the name a Get
// needs (Get cannot resolve the list RPC's `workspaces/-` shorthand).
const runName = computed(() => {
  const param = route.params.runId;
  return Array.isArray(param) ? param.join("/") : String(param ?? "");
});

const inProgress = computed(
  () => run.value !== undefined && isRunInProgress(run.value.status)
);

const durationLabel = computed(() => {
  const seconds = run.value ? runDurationSeconds(run.value) : undefined;
  return seconds === undefined ? "-" : formatSeconds(seconds);
});

const instanceLabel = computed(() =>
  run.value?.instance ? (run.value.instance.split("/").pop() ?? "-") : "-"
);

const initiatorLabel = computed(() =>
  run.value?.initiator ? (run.value.initiator.split("/").pop() ?? "-") : "-"
);

/** The six-state tallies in a fixed order, zeroes included in the summary. */
const allCounts = computed(() => {
  const counts = run.value?.counts;
  if (!counts) {
    return [];
  }
  return [
    { status: QualityStatus.PASS, value: counts.pass },
    { status: QualityStatus.WARN, value: counts.warn },
    { status: QualityStatus.FAIL, value: counts.fail },
    { status: QualityStatus.ERROR, value: counts.error },
    { status: QualityStatus.SKIPPED, value: counts.skipped },
    { status: QualityStatus.NOT_EVALUATED, value: counts.notEvaluated },
  ];
});

/**
 * Loads the run and its results. `notifyErrors` is false for a poll: a page
 * that already shows an error must not raise a toast every three seconds.
 */
async function load(notifyErrors: boolean) {
  if (loading) {
    return;
  }
  loading = true;
  const fetched = await loadRun(notifyErrors);
  if (fetched) {
    // A poll refreshes in place; only the first paint and the manual refresh
    // show the results placeholder, so a three-second poll does not blink.
    await loadResults(notifyErrors, pollTimer === undefined);
  }
  loading = false;
  if (!fetched) {
    stopPolling();
  } else if (inProgress.value) {
    startPolling();
  } else {
    stopPolling();
  }
}

async function loadRun(notifyErrors: boolean): Promise<boolean> {
  try {
    run.value = await getQualityRun(runName.value);
    error.value = "";
    return true;
  } catch (e: unknown) {
    if (notifyErrors) {
      error.value = handleError(e, "dataQuality.fetchRunError");
    }
    return false;
  }
}

async function loadResults(notifyErrors: boolean, showLoading: boolean) {
  if (!run.value) {
    return;
  }
  if (showLoading) {
    resultsLoading.value = true;
  }
  try {
    results.value = await listAllQualityRuleResults(run.value.name);
    resultsError.value = "";
  } catch (e: unknown) {
    if (notifyErrors) {
      resultsError.value = formatError(e, "dataQuality.fetchResultsError");
    }
  } finally {
    if (showLoading) {
      resultsLoading.value = false;
    }
  }
}

/** The first paint: both halves, with the loading placeholders on screen. */
async function initialLoad() {
  isLoading.value = true;
  resultsLoading.value = true;
  run.value = undefined;
  results.value = [];
  expanded.value = [];
  error.value = "";
  resultsError.value = "";
  stopPolling();
  await Promise.all([load(true), loadRuleTitles()]);
  isLoading.value = false;
}

/** The manual refresh button: reload both halves and keep the toast on failure. */
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

/**
 * Titles for the rules a run names. Best effort: a result whose rule is not in
 * the map falls back to the rule id, so a caller without rules.list still sees
 * the evidence it is allowed to read.
 */
async function loadRuleTitles() {
  try {
    const rules = await listAllQualityRules();
    ruleTitles.value = Object.fromEntries(
      rules.map((rule) => [rule.name, rule.title])
    );
  } catch {
    ruleTitles.value = {};
  }
}

function ruleLabel(result: QualityRuleResult): string {
  return ruleTitles.value[result.rule] || qualityRuleID(result.rule);
}

function errorLabel(result: QualityRuleResult): string {
  const key = qualityErrorCodeKey(result.errorCode);
  return key ? t(key) : result.errorCode;
}

function thresholdLabel(result: QualityRuleResult): string {
  const threshold = describeThreshold(result.thresholds, result.observedKind);
  return threshold ? t(threshold.key, threshold.params) : "-";
}

function warnTitle(result: QualityRuleResult): string | undefined {
  const warn = formatWarnThreshold(result.thresholds, result.observedKind);
  return warn ? t("dataQuality.warnAt", { value: warn }) : undefined;
}

function isExpanded(name: string): boolean {
  return expanded.value.includes(name);
}

function toggleExpanded(name: string) {
  expanded.value = isExpanded(name)
    ? expanded.value.filter((item) => item !== name)
    : [...expanded.value, name];
}

async function copySql(sql: string) {
  if (!navigator.clipboard) {
    return;
  }
  await navigator.clipboard.writeText(sql);
  notify.success(t("common.copied"));
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

/** The model explains a verdict, not a pass: only failed/errored rows offer it. */
function showExplain(result: QualityRuleResult): boolean {
  return (
    canExplain.value &&
    llmEnabled.value &&
    (result.status === QualityStatus.FAIL ||
      result.status === QualityStatus.ERROR)
  );
}

function openExplain(result: QualityRuleResult) {
  explainResult.value = result;
  explainOpen.value = true;
}

onMounted(() => {
  void loadLlmSetting();
  void initialLoad();
});

onBeforeUnmount(() => {
  stopPolling();
});

// A direct navigation from one run to another reuses this component instance, so
// the timers and the expanded rows must be reset with the data.
watch(
  () => route.params.runId,
  () => {
    void initialLoad();
  }
);
</script>
