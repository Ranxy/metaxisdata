<template>
  <Dialog
    :open="open"
    @update:open="(value: boolean) => emit('update:open', value)"
  >
    <DialogContent
      class="left-auto right-0 top-0 h-dvh max-h-dvh w-full max-w-3xl translate-x-0 translate-y-0 gap-0 overflow-hidden rounded-none border-l p-0 sm:max-w-3xl sm:rounded-none"
    >
      <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>
            {{ t("dataQuality.suggestions.jobs.historyTitle") }}
          </DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.suggestions.jobs.historyDescription") }}
          </DialogDescription>
          <div class="flex flex-wrap items-center gap-2 pt-1">
            <Select
              :model-value="statusFilter"
              @update:model-value="onStatusChange"
            >
              <SelectTrigger
                class="h-8 w-40"
                :aria-label="t('dataQuality.suggestions.filterStatus')"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="-">
                  {{ t("dataQuality.suggestions.allStatuses") }}
                </SelectItem>
                <SelectItem
                  v-for="status in STATUS_OPTIONS"
                  :key="status"
                  :value="String(status)"
                >
                  {{ t(suggestionJobStatusKey(status)) }}
                </SelectItem>
              </SelectContent>
            </Select>
            <Button
              variant="outline"
              size="sm"
              :disabled="isLoading"
              @click="refreshPage"
            >
              <RefreshCcw
                class="mr-1.5 h-4 w-4"
                :class="{ 'animate-spin': isLoading }"
              />
              {{ t("common.refresh") }}
            </Button>
          </div>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-3 overflow-auto px-6 py-5">
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
            v-if="isLoading && jobs.length === 0"
            class="flex justify-center p-10"
          >
            <Loader2 class="h-5 w-5 animate-spin text-muted-foreground" />
          </div>

          <EmptyState
            v-else-if="jobs.length === 0 && !error"
            :icon="Inbox"
            :title="
              statusFilter === '-'
                ? t('dataQuality.suggestions.jobs.emptyTitle')
                : t('dataQuality.suggestions.jobs.emptyFiltered')
            "
            :description="
              statusFilter === '-'
                ? t('dataQuality.suggestions.jobs.emptyDescription')
                : ''
            "
          >
            <Button
              v-if="statusFilter !== '-'"
              variant="outline"
              size="sm"
              @click="onStatusChange('-')"
            >
              {{ t("dataQuality.suggestions.jobs.clearFilter") }}
            </Button>
          </EmptyState>

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
                    {{ t("dataQuality.suggestions.jobs.progress", progressParams(job)) }}
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
                  :aria-expanded="expandedName === job.name"
                  :aria-label="
                    expandedName === job.name
                      ? t('dataQuality.suggestions.jobs.collapse')
                      : t('dataQuality.suggestions.jobs.expand')
                  "
                  :title="
                    expandedName === job.name
                      ? t('dataQuality.suggestions.jobs.collapse')
                      : t('dataQuality.suggestions.jobs.expand')
                  "
                  @click="toggleExpanded(job.name)"
                >
                  <ChevronDown
                    class="h-4 w-4 transition-transform"
                    :class="{ 'rotate-180': expandedName === job.name }"
                  />
                </Button>
              </div>
            </div>

            <div
              v-if="expandedName === job.name"
              class="space-y-3 border-t p-3"
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
        </div>

        <TablePager
          v-if="jobs.length > 0"
          class="shrink-0 px-6 pb-2"
          :page-size="pageSize"
          :has-previous="hasPrevious"
          :has-next="hasNext"
          :disabled="isLoading"
          @update:page-size="pageSize = $event"
          @previous="goPreviousPage"
          @next="goNextPage"
        />
      </div>
    </DialogContent>
  </Dialog>

  <SuggestionJobTargetDebugDialog
    v-model:open="debugOpen"
    :job-name="debugSelection?.jobName ?? ''"
    :target-index="debugSelection?.targetIndex ?? 0"
    :correlation-id="debugSelection?.correlationId ?? ''"
  />
</template>

<script setup lang="ts">
import {
  AlertTriangle,
  ChevronDown,
  Copy,
  Inbox,
  Loader2,
  RefreshCcw,
} from "lucide-vue-next";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  cancelQualitySuggestionJob,
  listQualitySuggestionJobs,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import TablePager from "@/components/common/TablePager.vue";
import SuggestionJobTargetDebugDialog from "@/components/data-quality/SuggestionJobTargetDebugDialog.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { usePolling } from "@/composables/usePolling";
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
  jobTargetLabel,
  suggestionJobProgress,
  suggestionJobStatusKey,
  suggestionJobStatusVariant,
  suggestionJobTargetStateKey,
  suggestionJobTargetStateVariant,
} from "@/utils/dataQualitySuggestion";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * Every drafting job, one page at a time, with the per-target result of one job
 * expanded in place.
 *
 * The suggestion page keeps only a one-line status strip for the live job; the
 * full history lives here so a growing job list can never push the review queue
 * below the fold. The drawer scrolls internally, which is why the detail (targets,
 * failure diagnostics, raw payload) can stay in the same surface.
 */

const PAGE_SIZE = 20;
const POLL_INTERVAL_MS = 2500;
/** The filter offers exactly the lifecycle statuses; "-" means all of them. */
const STATUS_OPTIONS = [
  QualitySuggestionJobStatus.PENDING,
  QualitySuggestionJobStatus.RUNNING,
  QualitySuggestionJobStatus.COMPLETED,
  QualitySuggestionJobStatus.FAILED,
  QualitySuggestionJobStatus.CANCELED,
];

const props = defineProps<{
  open: boolean;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  /** A job was canceled here, so the strip's counts may be stale. */
  changed: [];
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const { summaryText } = useSuggestionSummary();
const instanceStore = useInstanceStore();

const statusFilter = ref("-");
const pageSize = ref(PAGE_SIZE);
const error = ref("");
const expandedName = ref("");
const cancelTarget = ref("");
const canceling = ref("");

interface DebugSelection {
  jobName: string;
  targetIndex: number;
  correlationId: string;
}

const debugOpen = ref(false);
const debugSelection = ref<DebugSelection>();

const {
  items: jobs,
  isLoading,
  hasNext,
  hasPrevious,
  reset: resetPage,
  refresh: refreshPage,
  goNext: goNextPage,
  goPrevious: goPreviousPage,
} = usePagedFetch<QualitySuggestionJob>({
  fetchPage: async (pageToken, signal) => {
    error.value = "";
    return await listQualitySuggestionJobs({
      pageSize: pageSize.value,
      pageToken,
      status:
        statusFilter.value === "-"
          ? undefined
          : (Number(statusFilter.value) as QualitySuggestionJobStatus),
      signal,
    });
  },
  onError: (e: unknown) => {
    error.value = formatError(e, "dataQuality.suggestions.jobs.fetchFailed");
  },
});

/**
 * Live progress while the visible page still holds a job that can progress.
 * The drawer is a snapshot otherwise; a manual refresh is always one click away.
 */
const polling = usePolling(() => refreshPage(), {
  intervalMs: POLL_INTERVAL_MS,
  shouldContinue: () => props.open && jobs.value.some(isActiveSuggestionJob),
});

function stopPoll() {
  polling.stop();
}

function schedulePoll() {
  polling.stop();
  if (props.open && jobs.value.some(isActiveSuggestionJob)) {
    polling.start();
  }
}

watch(
  () => props.open,
  (open) => {
    if (open) {
      expandedName.value = "";
      cancelTarget.value = "";
      debugOpen.value = false;
      // Reopening starts from the newest page of all statuses: a filter left
      // behind by the previous visit would hide the job the strip advertises.
      if (statusFilter.value !== "-") {
        statusFilter.value = "-";
      } else {
        void resetPage();
      }
    } else {
      stopPoll();
      debugOpen.value = false;
    }
  },
  { immediate: true }
);

// A cursor belongs to the query that produced it; a new filter restarts the walk.
watch(statusFilter, () => {
  if (props.open) {
    void resetPage();
  }
});
watch(pageSize, () => {
  if (props.open) {
    void resetPage();
  }
});
watch(jobs, schedulePoll);

function onStatusChange(value: unknown) {
  statusFilter.value = String(value);
}

function progressParams(job: QualitySuggestionJob) {
  const progress = suggestionJobProgress(job);
  return {
    completed: progress.completed,
    total: progress.total,
    suggestions: progress.suggestions,
    // Drives the pipe plural in English: 1 suggestion, 2 suggestions.
    count: progress.suggestions,
  };
}

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

/** One row open at a time: expansion is a read, not a page state to accumulate. */
function toggleExpanded(name: string) {
  expandedName.value = expandedName.value === name ? "" : name;
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
    await refreshPage();
    emit("changed");
  } catch (e: unknown) {
    notify.error(formatError(e, "dataQuality.suggestions.jobs.cancelFailed"));
  } finally {
    canceling.value = "";
  }
}
</script>
