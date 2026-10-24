<template>
  <Card v-if="jobs.length > 0 || error">
    <CardHeader class="flex flex-row items-center justify-between gap-3 pb-2">
      <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
        <CardTitle class="text-base">
          {{ t("dataQuality.suggestions.jobs.title") }}
        </CardTitle>
        <span
          v-if="panelSummary"
          class="text-xs text-muted-foreground"
        >
          {{ panelSummary }}
        </span>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <p class="hidden text-xs text-muted-foreground sm:block">
          {{ t("dataQuality.suggestions.jobs.hint") }}
        </p>
        <Button
          variant="ghost"
          size="sm"
          :aria-expanded="panelExpanded"
          @click="panelExpanded = !panelExpanded"
        >
          {{ panelExpanded ? t("dataQuality.suggestions.jobs.hide") : t("dataQuality.suggestions.jobs.show") }}
          <ChevronDown
            class="ml-1 h-4 w-4 transition-transform"
            :class="{ 'rotate-180': panelExpanded }"
          />
        </Button>
      </div>
    </CardHeader>
    <CardContent
      v-if="panelExpanded"
      class="space-y-3"
    >
      <Alert
        v-if="error"
        variant="destructive"
      >
        <AlertTriangle class="h-4 w-4" />
        <AlertTitle>{{ t("dataQuality.suggestions.jobs.fetchFailed") }}</AlertTitle>
        <AlertDescription class="break-words">
          {{ error }}
        </AlertDescription>
      </Alert>

      <div
        v-for="job in jobs"
        :key="job.name"
        class="rounded-md border"
      >
        <div class="flex flex-wrap items-start justify-between gap-2 p-3">
          <div class="min-w-0 space-y-1">
            <div class="flex flex-wrap items-center gap-2">
              <Badge :variant="suggestionJobStatusVariant(job.status)">
                {{ t(suggestionJobStatusKey(job.status)) }}
              </Badge>
              <span
                class="min-w-0 truncate text-sm font-medium"
                :title="job.intent"
              >
                {{ jobIntentSnippet(job) || "-" }}
              </span>
            </div>
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
              <span>
                {{ t("dataQuality.suggestions.jobs.progress", {
                  completed: suggestionJobProgress(job).completed,
                  total: suggestionJobProgress(job).total,
                  suggestions: suggestionJobProgress(job).suggestions,
                }) }}
              </span>
              <span :title="formatDateTime(job.createTime, locale)">
                {{ formatRelativeTime(job.createTime, locale) }}
              </span>
              <span
                v-if="job.error"
                class="break-words text-destructive"
              >
                {{ job.error }}
              </span>
            </div>
          </div>

          <div class="flex shrink-0 items-center gap-1">
            <template v-if="isActiveSuggestionJob(job)">
              <template v-if="cancelTarget === job.name">
                <span class="text-xs text-muted-foreground">
                  {{ t("dataQuality.suggestions.jobs.cancelConfirm") }}
                </span>
                <Button
                  variant="destructive"
                  size="sm"
                  :disabled="canceling === job.name"
                  @click="confirmCancel(job)"
                >
                  {{ t("dataQuality.suggestions.jobs.cancelConfirmAction") }}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  :disabled="canceling === job.name"
                  @click="cancelTarget = ''"
                >
                  {{ t("dataQuality.suggestions.jobs.cancelKeep") }}
                </Button>
              </template>
              <Button
                v-else
                variant="outline"
                size="sm"
                :disabled="canceling === job.name"
                @click="cancelTarget = job.name"
              >
                {{ t("dataQuality.suggestions.jobs.cancel") }}
              </Button>
            </template>
            <Button
              variant="ghost"
              size="icon"
              class="h-8 w-8"
              :aria-expanded="expanded.includes(job.name)"
              :aria-label="
                expanded.includes(job.name)
                  ? t('dataQuality.suggestions.jobs.collapse')
                  : t('dataQuality.suggestions.jobs.expand')
              "
              :title="
                expanded.includes(job.name)
                  ? t('dataQuality.suggestions.jobs.collapse')
                  : t('dataQuality.suggestions.jobs.expand')
              "
              @click="toggleExpanded(job.name)"
            >
              <ChevronDown
                class="h-4 w-4 transition-transform"
                :class="{ 'rotate-180': expanded.includes(job.name) }"
              />
            </Button>
          </div>
        </div>

        <div
          v-if="expanded.includes(job.name)"
          class="space-y-2 border-t p-3"
        >
          <div
            v-if="job.targets.length === 0"
            class="text-sm text-muted-foreground"
          >
            {{ t("dataQuality.suggestions.jobs.noTargets") }}
          </div>
          <div
            v-for="(target, index) in job.targets"
            :key="`${job.name}-${index}`"
            class="flex flex-wrap items-start justify-between gap-2 text-sm"
          >
            <div class="min-w-0 space-y-2">
              <div class="truncate">
                {{ jobTargetLabel(target, targetInstanceTitle(target.target?.path?.instance ?? '')) }}
              </div>
              <!-- What this target's pass did to the queue: created, refreshed,
                   skipped, or why it skipped everything. -->
              <p
                v-if="targetSummaryText(target)"
                class="text-xs text-muted-foreground"
              >
                {{ targetSummaryText(target) }}
              </p>
              <template v-if="target.diagnostic">
                <div class="flex flex-wrap items-center gap-2">
                  <Badge variant="warning">
                    {{ t(diagnosticCodeKey(target.diagnostic.errorCode)) }}
                  </Badge>
                  <p class="text-sm">
                    {{ t(diagnosticHintKey(target.diagnostic.errorCode)) }}
                  </p>
                </div>
                <dl class="grid gap-x-4 gap-y-1 text-xs sm:grid-cols-2">
                  <template
                    v-for="fact in diagnosticFacts(target.diagnostic)"
                    :key="fact.labelKey"
                  >
                    <dt class="text-muted-foreground">
                      {{ t(fact.labelKey) }}
                    </dt>
                    <dd class="break-words">
                      {{ fact.value }}
                    </dd>
                  </template>
                </dl>
                <div class="flex flex-wrap items-center gap-2">
                  <span class="font-mono text-xs">
                    {{ t("dataQuality.suggestions.diagnostic.facts.correlationId") }}:
                    {{ target.diagnostic.correlationId || "-" }}
                  </span>
                  <Button
                    v-if="target.diagnostic.correlationId"
                    variant="ghost"
                    size="icon"
                    class="h-7 w-7"
                    :title="t('dataQuality.suggestions.diagnostic.copyCorrelation')"
                    :aria-label="t('dataQuality.suggestions.diagnostic.copyCorrelation')"
                    @click="copyText(target.diagnostic.correlationId)"
                  >
                    <Copy class="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    v-if="target.diagnostic.debugPayloadAvailable"
                    variant="outline"
                    size="sm"
                    @click="openDebug(job, index)"
                  >
                    {{ t("dataQuality.suggestions.diagnostic.viewPayload") }}
                  </Button>
                </div>
              </template>
              <p
                v-else-if="target.error"
                class="text-xs break-words text-destructive"
              >
                {{ target.error }}
              </p>
            </div>
            <div class="flex shrink-0 items-center gap-2">
              <Badge :variant="suggestionJobTargetStateVariant(target.state)">
                {{ t(suggestionJobTargetStateKey(target.state)) }}
              </Badge>
              <span class="text-xs text-muted-foreground">
                {{ t("dataQuality.suggestions.groupCount", { count: target.suggestions.length }) }}
              </span>
            </div>
          </div>
        </div>
      </div>
    </CardContent>
  </Card>

  <SuggestionJobTargetDebugDialog
    v-model:open="debugOpen"
    :job-name="debugSelection?.jobName ?? ''"
    :target-index="debugSelection?.targetIndex ?? 0"
    :correlation-id="debugSelection?.correlationId ?? ''"
  />
</template>

<script setup lang="ts">
import { AlertTriangle, ChevronDown, Copy } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  cancelQualitySuggestionJob,
  getQualitySuggestionJob,
  listQualitySuggestionJobs,
} from "@/api/data-quality";
import SuggestionJobTargetDebugDialog from "@/components/data-quality/SuggestionJobTargetDebugDialog.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useSuggestionSummary } from "@/composables/useSuggestionSummary";
import { notify } from "@/lib/notify";
import { useInstanceStore } from "@/store/modules/instance";
import {
  type QualityLLMDiagnostic,
  type QualitySuggestionJob,
  QualitySuggestionJobStatus,
  type QualitySuggestionJobTarget,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  diagnosticCodeKey,
  diagnosticHintKey,
  diagnosticSummary,
  isActiveSuggestionJob,
  jobIntentSnippet,
  jobSuggestionNames,
  jobTargetLabel,
  suggestionJobProgress,
  suggestionJobStatusKey,
  suggestionJobStatusVariant,
  suggestionJobTargetStateKey,
  suggestionJobTargetStateVariant,
} from "@/utils/dataQualitySuggestion";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * The recent AI drafting jobs, polled while one is still active.
 *
 * The workbench only submits a job; this panel is how the reader follows it
 * without keeping the drafting dialog open. A job that finishes while the page
 * is mounted settles once: the page refreshes the queue, selects the new pending
 * suggestions when they are visible, and the reader hears about it once.
 */

const JOBS_PAGE_SIZE = 10;
/** The runner claims the oldest PENDING job first, so an active job may be older
 * than the recent page; this list finds it independently of the history. */
const ACTIVE_JOBS_PAGE_SIZE = 20;
const POLL_INTERVAL_MS = 2500;

const props = withDefaults(
  defineProps<{
    /** Bumped by the page after a submission to reload immediately. */
    refreshKey?: number;
  }>(),
  { refreshKey: 0 }
);

const emit = defineEmits<{
  /** A job left PENDING/RUNNING; `names` are the suggestions it queued. */
  settled: [suggestionNames: string[]];
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const { summaryText } = useSuggestionSummary();
const instanceStore = useInstanceStore();

const jobs = ref<QualitySuggestionJob[]>([]);
const error = ref("");
const expanded = ref<string[]>([]);
const cancelTarget = ref("");
const canceling = ref("");
/**
 * The list is collapsed by default: the panel sits above the suggestion table,
 * and a long history must not push the queue below the fold. A submission
 * expands it; the compact header always carries the live counts.
 */
const panelExpanded = ref(false);

/** Job names that were active in the last snapshot; transitions come from here. */
let activeNames = new Set<string>();
/** Terminal jobs already announced, so a later poll cannot settle them twice. */
const handled = new Set<string>();
let pollTimer: ReturnType<typeof setTimeout> | undefined;
let inFlight = false;
let disposed = false;
/** A reload asked for while a request was in flight must not be dropped. */
let reloadRequested = false;
/** Previously-active names we failed to re-read; poll again rather than stall. */
let retryNames: string[] = [];

/** Something may still progress: a visible active job or an unread transition. */
function hasActiveWork(): boolean {
  return jobs.value.some(isActiveSuggestionJob) || retryNames.length > 0;
}
const panelSummary = computed(() => {
  if (jobs.value.length === 0) {
    return "";
  }
  const active = jobs.value.filter(isActiveSuggestionJob).length;
  return t("dataQuality.suggestions.jobs.summary", {
    active,
    finished: jobs.value.length - active,
  });
});

interface DebugSelection {
  jobName: string;
  targetIndex: number;
  correlationId: string;
}

const debugOpen = ref(false);
const debugSelection = ref<DebugSelection>();

function targetInstanceTitle(instanceId: string): string {
  return instanceId ? instanceStore.titleOf(`instances/${instanceId}`) : "";
}

/** What one target's generation pass did; the job persists it per target. */
function targetSummaryText(target: QualitySuggestionJobTarget): string {
  return target.summary
    ? summaryText(target.summary, "rule", target.suggestions.length)
    : "";
}

/**
 * The compact facts of a failed model call, in display order. Reported-only
 * values are shown; "-" is skipped and the correlation id is rendered beside
 * its own copy action.
 */
function diagnosticFacts(
  diagnostic: QualityLLMDiagnostic
): Array<{ labelKey: string; value: string }> {
  const summary = diagnosticSummary(diagnostic);
  const facts = [
    {
      labelKey: "dataQuality.suggestions.diagnostic.facts.provider",
      value: summary.providerModel,
    },
    {
      labelKey: "dataQuality.suggestions.diagnostic.facts.promptVersion",
      value: summary.promptVersion,
    },
    {
      labelKey: "dataQuality.suggestions.diagnostic.facts.finishReason",
      value: summary.finishReason,
    },
    {
      labelKey: "dataQuality.suggestions.diagnostic.facts.maxOutputTokens",
      value: summary.maxOutputTokens,
    },
    {
      labelKey: "dataQuality.suggestions.diagnostic.facts.promptTokens",
      value: summary.promptTokens,
    },
    {
      labelKey: "dataQuality.suggestions.diagnostic.facts.completionTokens",
      value: summary.completionTokens,
    },
    {
      labelKey: "dataQuality.suggestions.diagnostic.facts.totalTokens",
      value: summary.totalTokens,
    },
    {
      labelKey: "dataQuality.suggestions.diagnostic.facts.promptChars",
      value: summary.promptChars,
    },
    {
      labelKey: "dataQuality.suggestions.diagnostic.facts.providerStatus",
      value: summary.providerStatus,
    },
  ];
  return facts.filter((fact) => fact.value !== "-");
}

function openDebug(job: QualitySuggestionJob, index: number) {
  debugSelection.value = {
    jobName: job.name,
    targetIndex: index,
    correlationId: job.targets[index]?.diagnostic?.correlationId ?? "",
  };
  debugOpen.value = true;
}

/** A correlation id is meant to be pasted into server logs, so copying it is one click. */
async function copyText(text: string) {
  if (!text || !navigator.clipboard) {
    return;
  }
  await navigator.clipboard.writeText(text);
  notify.success(t("common.copied"));
}

function toggleExpanded(name: string) {
  expanded.value = expanded.value.includes(name)
    ? expanded.value.filter((item) => item !== name)
    : [...expanded.value, name];
}

/**
 * Fold one page of jobs into the panel. A job that was active in the previous
 * snapshot and is terminal now settles exactly once; the first snapshot never
 * settles anything, so old jobs on page load do not replay toasts.
 */
function applyJobs(next: QualitySuggestionJob[]) {
  if (disposed) {
    return;
  }
  const previousActive = activeNames;
  const nextActive = new Set(
    next.filter(isActiveSuggestionJob).map((job) => job.name)
  );
  const transitioned = next.filter(
    (job) =>
      previousActive.has(job.name) &&
      !isActiveSuggestionJob(job) &&
      !handled.has(job.name)
  );
  for (const job of transitioned) {
    handled.add(job.name);
  }
  jobs.value = next;
  activeNames = nextActive;
  if (transitioned.length > 0) {
    // Only a completed job's suggestions are new review work; a canceled or
    // failed job still settles the panel so the page can refresh its queue.
    emit(
      "settled",
      transitioned
        .filter((job) => job.status === QualitySuggestionJobStatus.COMPLETED)
        .flatMap((job) => jobSuggestionNames(job))
    );
  }
}

function scheduleNextPoll() {
  if (disposed) {
    return;
  }
  if (pollTimer !== undefined) {
    clearTimeout(pollTimer);
    pollTimer = undefined;
  }
  if (!hasActiveWork()) {
    return;
  }
  pollTimer = setTimeout(() => {
    pollTimer = undefined;
    void poll();
  }, POLL_INTERVAL_MS);
}

/**
 * The recent page for history plus every RUNNING job, merged by name with the
 * page's order first. The recent page alone cannot prove nothing is active:
 * active jobs are the oldest PENDING ones and may sit below the page.
 */
async function fetchSnapshot(): Promise<QualitySuggestionJob[]> {
  const [recent, running] = await Promise.all([
    listQualitySuggestionJobs({ pageSize: JOBS_PAGE_SIZE }),
    listQualitySuggestionJobs({
      status: QualitySuggestionJobStatus.RUNNING,
      pageSize: ACTIVE_JOBS_PAGE_SIZE,
    }),
  ]);
  const merged = [...recent.items];
  const seen = new Set(merged.map((job) => job.name));
  for (const job of running.items) {
    if (!seen.has(job.name)) {
      seen.add(job.name);
      merged.push(job);
    }
  }
  return merged;
}

/**
 * A job that was active last time but is absent now may have just finished and
 * fallen off both lists; re-read it so its transition still settles. Bounded by
 * the previously-active set (normally zero or one) and skipped once handled.
 */
async function rereadMissingActiveJobs(
  next: QualitySuggestionJob[]
): Promise<string[]> {
  const present = new Set(next.map((job) => job.name));
  // Include last round's failed re-reads: applyJobs replaced activeNames with the
  // merged snapshot, so a failed name would otherwise never be retried.
  const candidates = new Set([...activeNames, ...retryNames]);
  const missing = [...candidates].filter(
    (name) => !present.has(name) && !handled.has(name)
  );
  const retry: string[] = [];
  for (const name of missing) {
    try {
      const job = await getQualitySuggestionJob(name);
      if (!present.has(job.name)) {
        present.add(job.name);
        next.push(job);
      }
    } catch {
      // Polling must not stall on a read that failed; try again next tick.
      retry.push(name);
    }
  }
  return retry;
}

async function loadJobs() {
  if (inFlight) {
    // The caller wants fresh data (e.g. a submission): rerun instead of dropping.
    reloadRequested = true;
    return;
  }
  inFlight = true;
  try {
    const next = await fetchSnapshot();
    retryNames = await rereadMissingActiveJobs(next);
    error.value = "";
    applyJobs(next);
    // A failed re-read has to stay "previously active" so the next successful
    // read still counts as an active→terminal transition.
    for (const name of retryNames) {
      activeNames.add(name);
    }
  } catch (e: unknown) {
    error.value = formatError(e, "dataQuality.suggestions.jobs.fetchFailed");
  } finally {
    inFlight = false;
    if (reloadRequested) {
      reloadRequested = false;
      if (pollTimer !== undefined) {
        clearTimeout(pollTimer);
        pollTimer = undefined;
      }
      // Run now: the deferred reload must not wait for the poll interval, and
      // its own finally re-arms polling from the fresh snapshot.
      void loadJobs();
    } else {
      // Re-arm even after a failed poll: an active job is worth retrying, and a
      // finished one clears the timer here.
      scheduleNextPoll();
    }
  }
}

/** Poll only while a job can still progress; never overlap a request. */
async function poll() {
  if (inFlight) {
    scheduleNextPoll();
    return;
  }
  await loadJobs();
}

async function confirmCancel(job: QualitySuggestionJob) {
  canceling.value = job.name;
  try {
    const canceled = await cancelQualitySuggestionJob(job.name);
    notify.success(
      t(
        canceled.status === QualitySuggestionJobStatus.CANCELED
          ? "dataQuality.suggestions.jobs.canceledToast"
          : "dataQuality.suggestions.jobs.cancelRequested"
      )
    );
    cancelTarget.value = "";
    await loadJobs();
  } catch (e: unknown) {
    notify.error(formatError(e, "dataQuality.suggestions.jobs.cancelFailed"));
  } finally {
    canceling.value = "";
  }
}

watch(
  () => props.refreshKey,
  () => {
    // A fresh submission is worth showing immediately; a page load stays
    // collapsed so the queue keeps its place above the fold.
    panelExpanded.value = true;
    void loadJobs();
  }
);

watch(error, (message) => {
  if (message) {
    panelExpanded.value = true;
  }
});

onMounted(() => {
  void loadJobs();
});

onBeforeUnmount(() => {
  disposed = true;
  if (pollTimer !== undefined) {
    clearTimeout(pollTimer);
    pollTimer = undefined;
  }
});
</script>
