<template>
  <Card v-if="jobs.length > 0 || error">
    <CardHeader class="pt-4 pb-2">
      <div class="flex flex-wrap items-center justify-between gap-3">
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
          <Badge
            v-if="failedCount > 0"
            variant="danger"
          >
            {{ t("dataQuality.suggestions.jobs.failedCount", { count: failedCount }) }}
          </Badge>
        </div>
        <div class="flex shrink-0 items-center gap-2">
          <p class="hidden text-xs text-muted-foreground sm:block">
            {{ t("dataQuality.suggestions.jobs.hint") }}
          </p>
          <Button
            variant="outline"
            size="sm"
            aria-haspopup="dialog"
            @click="historyOpen = true"
          >
            <History class="mr-1.5 h-4 w-4" />
            {{ t("dataQuality.suggestions.jobs.history") }}
          </Button>
        </div>
      </div>

      <!-- The one job worth watching: the live pass, else the newest outcome.
           A single bounded line keeps the review queue above the fold no matter
           how many jobs the workspace has accumulated. -->
      <div
        v-if="spotlightJob"
        class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground"
      >
        <Badge :variant="suggestionJobStatusVariant(spotlightJob.status)">
          {{ t(suggestionJobStatusKey(spotlightJob.status)) }}
        </Badge>
        <span
          class="min-w-0 max-w-full flex-1 truncate text-sm text-foreground"
          :title="spotlightJob.intent"
        >
          {{ jobIntentSnippet(spotlightJob) || "-" }}
        </span>
        <!-- Decorative meter: the counts are stated in the text beside it. -->
        <div
          v-if="spotlightActive"
          class="h-1.5 w-24 overflow-hidden rounded-full bg-muted"
          aria-hidden="true"
        >
          <div
            class="h-full rounded-full bg-primary transition-all"
            :style="{ width: `${spotlightProgress.percent}%` }"
          />
        </div>
        <span class="whitespace-nowrap">
          {{ t("dataQuality.suggestions.jobs.progress", spotlightProgressParams) }}
        </span>
        <span
          v-if="!spotlightActive"
          :title="formatDateTime(spotlightJob.createTime, locale)"
          class="whitespace-nowrap"
        >
          {{ formatRelativeTime(spotlightJob.createTime, locale) }}
        </span>
        <span
          v-if="spotlightJob.error"
          class="min-w-0 max-w-full truncate text-destructive"
          :title="spotlightJob.error"
        >
          {{ spotlightJob.error }}
        </span>
        <!-- The live job is the one worth cancelling, and it may be older than
             the drawer's first page: keep the two-step cancel on the strip. -->
        <template v-if="spotlightActive">
          <template v-if="cancelTarget === spotlightJob.name">
            <span class="text-xs text-muted-foreground">
              {{ t("dataQuality.suggestions.jobs.cancelConfirm") }}
            </span>
            <Button
              variant="destructive"
              size="sm"
              :disabled="canceling === spotlightJob.name"
              @click="confirmCancel(spotlightJob)"
            >
              {{ t("dataQuality.suggestions.jobs.cancelConfirmAction") }}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              :disabled="canceling === spotlightJob.name"
              @click="cancelTarget = ''"
            >
              {{ t("dataQuality.suggestions.jobs.cancelKeep") }}
            </Button>
          </template>
          <Button
            v-else
            variant="ghost"
            size="sm"
            :disabled="canceling === spotlightJob.name"
            @click="cancelTarget = spotlightJob.name"
          >
            {{ t("dataQuality.suggestions.jobs.cancel") }}
          </Button>
        </template>
      </div>
    </CardHeader>

    <CardContent v-if="error">
      <Alert variant="destructive">
        <AlertTriangle class="h-4 w-4" />
        <AlertTitle>{{ t("dataQuality.suggestions.jobs.fetchFailed") }}</AlertTitle>
        <AlertDescription class="space-y-2">
          <p class="break-words">{{ error }}</p>
          <Button
            variant="outline"
            size="sm"
            :disabled="loading"
            @click="loadJobs"
          >
            {{ t("common.retry") }}
          </Button>
        </AlertDescription>
      </Alert>
    </CardContent>
  </Card>

  <SuggestionJobHistoryDrawer
    v-model:open="historyOpen"
    @changed="loadJobs"
  />
</template>

<script setup lang="ts">
import { AlertTriangle, History } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  cancelQualitySuggestionJob,
  getQualitySuggestionJob,
  listQualitySuggestionJobs,
} from "@/api/data-quality";
import SuggestionJobHistoryDrawer from "@/components/data-quality/SuggestionJobHistoryDrawer.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { usePolling } from "@/composables/usePolling";
import { notify } from "@/lib/notify";
import {
  type QualitySuggestionJob,
  QualitySuggestionJobStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  isActiveSuggestionJob,
  jobIntentSnippet,
  jobSuggestionNames,
  type SuggestionJobProgress,
  suggestionJobProgress,
  suggestionJobStatusKey,
  suggestionJobStatusVariant,
} from "@/utils/dataQualitySuggestion";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * The recent AI drafting jobs, polled while one is still active.
 *
 * The workbench only submits a job; this strip is how the reader follows it
 * without keeping the drafting dialog open. It is deliberately a fixed height:
 * one summary line plus the single most relevant job. The full history (and the
 * per-target results of any job) lives in the drawer, so a workspace with many
 * jobs cannot push the suggestion queue below the fold.
 *
 * A job that finishes while the page is mounted settles once: the page refreshes
 * the queue, selects the new pending suggestions when they are visible, and the
 * reader hears about it once.
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

const jobs = ref<QualitySuggestionJob[]>([]);
const error = ref("");
const loading = ref(false);
const historyOpen = ref(false);
const cancelTarget = ref("");
const canceling = ref("");

/** Job names that were active in the last snapshot; transitions come from here. */
let activeNames = new Set<string>();
/** Terminal jobs already announced, so a later poll cannot settle them twice. */
const handled = new Set<string>();
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

/** The poll waits for a round to settle before scheduling the next one. */
const polling = usePolling(() => loadJobs(true), {
  intervalMs: POLL_INTERVAL_MS,
  shouldContinue: hasActiveWork,
});
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
/** Failures in the recent window; the one-job strip would otherwise hide them. */
const failedCount = computed(
  () =>
    jobs.value.filter((job) => job.status === QualitySuggestionJobStatus.FAILED)
      .length
);

/**
 * What the strip reports: a running job first (there is at most one), then a
 * queued one, then the newest finished job. Its progress and outcome are the
 * only per-job detail shown inline; everything else is behind the history button.
 */
const spotlightJob = computed<QualitySuggestionJob | undefined>(() => {
  return (
    jobs.value.find(
      (job) => job.status === QualitySuggestionJobStatus.RUNNING
    ) ??
    jobs.value.find(isActiveSuggestionJob) ??
    jobs.value[0]
  );
});
const spotlightActive = computed(() =>
  spotlightJob.value ? isActiveSuggestionJob(spotlightJob.value) : false
);
const spotlightProgress = computed<SuggestionJobProgress>(() =>
  spotlightJob.value
    ? suggestionJobProgress(spotlightJob.value)
    : { total: 0, completed: 0, percent: 0, suggestions: 0 }
);
const spotlightProgressParams = computed(() => ({
  completed: spotlightProgress.value.completed,
  total: spotlightProgress.value.total,
  suggestions: spotlightProgress.value.suggestions,
  count: spotlightProgress.value.suggestions,
}));

// A cancel confirmation belongs to the job that was live when it was opened;
// when the spotlight moves on (that job finished), do not leave it dangling.
watch(
  () => spotlightJob.value?.name,
  () => {
    cancelTarget.value = "";
  }
);

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
  if (hasActiveWork()) {
    polling.start();
  } else {
    polling.stop();
  }
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

async function loadJobs(fromPoll = false) {
  if (inFlight) {
    // A user reload (e.g. after a submission) must rerun instead of dropping;
    // a poll that merely collided with it waits for the next round.
    if (!fromPoll) {
      reloadRequested = true;
    }
    return;
  }
  inFlight = true;
  loading.value = true;
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
    loading.value = false;
    if (reloadRequested) {
      reloadRequested = false;
      polling.stop();
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

/**
 * Cancel the job the strip is reporting. The reload turns the terminal answer
 * into the usual active→terminal transition, so the page still refreshes its
 * queue and the reader hears about it once.
 */
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
    void loadJobs();
  }
);

onMounted(() => {
  void loadJobs();
});

onBeforeUnmount(() => {
  disposed = true;
});
</script>
