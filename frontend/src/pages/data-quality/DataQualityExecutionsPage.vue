<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.executionsTitle')"
      :description="t('dataQuality.executionsDescription')"
    >
      <template #actions>
        <!-- The view switch lives in the header actions on purpose: a separate
             row above the card would push the table past this route's
             tableTopY <= 200 budget. -->
        <div
          class="flex items-center gap-0.5 rounded-lg bg-muted p-0.5"
          role="group"
          :aria-label="t('dataQuality.executionsTitle')"
        >
          <button
            v-for="option in viewOptions"
            :key="option.value"
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:outline-none"
            :class="
              activeView === option.value
                ? 'bg-card text-foreground shadow-e1'
                : 'text-muted-foreground hover:text-foreground'
            "
            :aria-pressed="activeView === option.value"
            @click="switchView(option.value)"
          >
            {{ option.label }}
          </button>
        </div>
        <ExportCsvButton
          :exporter="exportCsv"
          :file-name="exportFileName"
        />
        <Button
          variant="outline"
          :disabled="activeLoading"
          @click="refreshCurrent"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': activeLoading }"
          />
          {{ t("common.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <!-- BY TARGET: the directory of targets with their latest scan batch. This
         is the default view; ?view=executions keeps the flat list below. -->
    <Card v-if="!flatView">
      <div class="flex flex-wrap items-end gap-3 border-b p-4">
        <div class="flex min-w-48 flex-1 flex-col gap-1.5 sm:max-w-sm sm:flex-none">
          <Label
            for="data-quality-target-search"
            class="sr-only"
          >
            {{ t("dataQuality.search") }}
          </Label>
          <Input
            id="data-quality-target-search"
            v-model="targetSearch"
            class="w-full"
            :placeholder="t('dataQuality.executions.targetSearch')"
            @keyup.enter="applyTargetSearch"
          />
        </div>
        <div class="flex min-w-40 flex-col gap-1.5 [&>select]:hidden sm:ml-auto sm:w-44">
          <Label
            for="data-quality-target-sort"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.targets.sort") }}
          </Label>
          <Select v-model="targetSort">
            <SelectTrigger
              id="data-quality-target-sort"
              class="w-full"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="option in targetSortOptions"
                :key="option.value"
                :value="option.value"
              >
                {{ option.label }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <PageState
        :loading="targetsLoading && targets.length === 0"
        :error="targetsError"
        :title="t('dataQuality.targets.loadFailed')"
      >
        <template #error>
          <Button
            variant="outline"
            size="sm"
            @click="resetTargets"
          >
            {{ t("common.refresh") }}
          </Button>
        </template>

        <EmptyState
          v-if="targets.length === 0"
          :icon="Activity"
          :title="
            appliedTargetSearch
              ? t('dataQuality.noFilteredExecutions')
              : t('dataQuality.noExecutions')
          "
          :description="
            appliedTargetSearch ? '' : t('dataQuality.noExecutionsHint')
          "
        >
          <Button
            v-if="appliedTargetSearch"
            variant="outline"
            size="sm"
            @click="clearTargetSearch"
          >
            {{ t("common.filter.clear") }}
          </Button>
          <Button
            v-else
            variant="outline"
            size="sm"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualityTargets' }">
              {{ t("dataQuality.targets.title") }}
            </RouterLink>
          </Button>
        </EmptyState>

        <div v-else>
          <Table class="[&_th]:whitespace-nowrap">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("dataQuality.executions.targetColumn") }}</TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.executions.latestScan") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.executions.runResult") }}
                </TableHead>
                <TableHead class="hidden lg:table-cell">
                  {{ t("dataQuality.executions.runDuration") }}
                </TableHead>
                <TableHead class="hidden text-right md:table-cell">
                  {{ t("dataQuality.executions.failedChecks") }}
                </TableHead>
                <TableHead class="hidden text-right md:table-cell">
                  {{ t("dataQuality.executions.runCount") }}
                </TableHead>
                <TableHead class="w-12 text-right">
                  <span class="sr-only">{{ t("dataQuality.executions.historyTitle") }}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TargetRunRow
                v-for="summary in targets"
                :key="targetGuid(summary)"
                :summary="summary"
                :expanded="isExpanded(summary)"
                :rule-titles="ruleTitles"
                :expand-run="expandedRun"
                @toggle="toggleTarget(summary)"
                @update:expand-run="onRunToggle"
              />
            </TableBody>
          </Table>

          <div class="px-6 pb-6">
            <TablePager
              v-model:page-size="targetPageSize"
              :has-previous="targetsHasPrevious"
              :has-next="targetsHasNext"
              :disabled="targetsLoading"
              @previous="goPreviousTargets"
              @next="goNextTargets"
            />
          </div>
        </div>
      </PageState>
    </Card>

    <!-- BY EXECUTION: the flat list, unchanged apart from the run filter chip. -->
    <Card v-else>
      <!-- One filter row, applied together rather than per keystroke: a Select
           change does not re-query until Apply. A rule, target or run carried in
           the URL is shown as a removable chip instead, because it came from a
           cross-page link rather than from this row. -->
      <div class="flex flex-wrap items-end gap-3 border-b p-4">
        <div class="flex min-w-36 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-execution-state"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.filterState") }}
          </Label>
          <Select v-model="state">
            <SelectTrigger
              id="data-quality-execution-state"
              class="w-full sm:w-40"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.allExecutionStates") }}
              </SelectItem>
              <SelectItem
                v-for="option in stateOptions"
                :key="option.value"
                :value="String(option.value)"
              >
                {{ t(option.key) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex min-w-36 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-execution-outcome"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.outcome") }}
          </Label>
          <Select v-model="outcome">
            <SelectTrigger
              id="data-quality-execution-outcome"
              class="w-full sm:w-40"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.allOutcomes") }}
              </SelectItem>
              <SelectItem
                v-for="option in outcomeOptions"
                :key="option.value"
                :value="String(option.value)"
              >
                {{ t(option.key) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex min-w-36 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-execution-trigger"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.trigger") }}
          </Label>
          <Select v-model="trigger">
            <SelectTrigger
              id="data-quality-execution-trigger"
              class="w-full sm:w-40"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.allTriggers") }}
              </SelectItem>
              <SelectItem
                v-for="option in triggerOptions"
                :key="option.value"
                :value="String(option.value)"
              >
                {{ t(option.key) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <Button
          variant="outline"
          @click="applyFilters"
        >
          {{ t("common.apply") }}
        </Button>

        <div
          v-if="ruleFilter"
          class="flex min-w-0 items-center gap-1 pb-1.5"
        >
          <Badge
            variant="secondary"
            class="max-w-64 truncate"
            :title="ruleFilter"
          >
            {{ t("dataQuality.filteredByRule", { rule: ruleLabel(ruleFilter) }) }}
          </Badge>
          <Button
            variant="ghost"
            size="icon"
            class="h-7 w-7"
            :title="t('dataQuality.clearRuleFilter')"
            :aria-label="t('dataQuality.clearRuleFilter')"
            @click="clearRuleFilter"
          >
            <X class="h-3.5 w-3.5" />
          </Button>
        </div>
        <div
          v-if="targetFilter"
          class="flex min-w-0 items-center gap-1 pb-1.5"
        >
          <Badge
            variant="secondary"
            class="max-w-64 truncate"
            :title="targetFilter"
          >
            {{ t("dataQuality.filteredByTarget", { target: targetFilter }) }}
          </Badge>
          <Button
            variant="ghost"
            size="icon"
            class="h-7 w-7"
            :title="t('dataQuality.clearTargetFilter')"
            :aria-label="t('dataQuality.clearTargetFilter')"
            @click="clearTargetFilter"
          >
            <X class="h-3.5 w-3.5" />
          </Button>
        </div>
        <div
          v-if="runFilter"
          class="flex min-w-0 items-center gap-1 pb-1.5"
        >
          <Badge
            variant="secondary"
            class="max-w-64 truncate"
            :title="runFilter"
          >
            {{ t("dataQuality.filteredByRun", { run: runShortID }) }}
          </Badge>
          <Button
            variant="ghost"
            size="icon"
            class="h-7 w-7"
            :title="t('dataQuality.clearRunFilter')"
            :aria-label="t('dataQuality.clearRunFilter')"
            @click="clearRunFilter"
          >
            <X class="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      <PageState
        :loading="executionsLoading && executions.length === 0"
        :error="executionsError"
      >
        <EmptyState
          v-if="executions.length === 0"
          :icon="Activity"
          :title="
            hasActiveFilters
              ? t('dataQuality.noFilteredExecutions')
              : t('dataQuality.noExecutions')
          "
          :description="
            hasActiveFilters ? '' : t('dataQuality.noExecutionsHint')
          "
        >
          <Button
            v-if="hasActiveFilters"
            variant="outline"
            size="sm"
            @click="clearFilters"
          >
            {{ t("common.filter.clear") }}
          </Button>
          <Button
            v-else-if="canViewRule"
            variant="outline"
            size="sm"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualityChecks' }">
              {{ t("dataQuality.viewRules") }}
            </RouterLink>
          </Button>
        </EmptyState>

        <div v-else>
          <Table class="[&_th]:whitespace-nowrap">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("dataQuality.execution") }}</TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.rule") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.ruleTarget") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.observed") }}
                </TableHead>
                <TableHead class="hidden whitespace-nowrap md:table-cell">
                  {{ t("dataQuality.failedRows") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.trigger") }}
                </TableHead>
                <TableHead class="hidden whitespace-nowrap md:table-cell">
                  {{ t("dataQuality.startedAt") }}
                </TableHead>
                <TableHead class="w-12 text-right">
                  <span class="sr-only">{{ t("common.actions") }}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <ExecutionRunRow
                v-for="execution in executions"
                :key="execution.name"
                :execution="execution"
                :rule-titles="ruleTitles"
                :can-view-rule="canViewRule"
                @open="openExecution(execution)"
              />
            </TableBody>
          </Table>

          <!-- The pager footer keeps the card's content inset. -->
          <div class="px-6 pb-6">
            <TablePager
              v-model:page-size="executionsPageSize"
              :has-previous="executionsHasPrevious"
              :has-next="executionsHasNext"
              :disabled="executionsLoading"
              @previous="goPreviousExecutions"
              @next="goNextExecutions"
            />
          </div>
        </div>
      </PageState>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { Activity, RefreshCcw, X } from "lucide-vue-next";
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute, useRouter } from "vue-router";
import {
  exportQualityExecutions,
  exportQualityTargets,
  listAllQualityRules,
  listQualityExecutions,
  listQualityTargets,
  qualityRuleID,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import ExecutionRunRow from "@/components/data-quality/ExecutionRunRow.vue";
import ExportCsvButton from "@/components/data-quality/ExportCsvButton.vue";
import TargetRunRow from "@/components/data-quality/TargetRunRow.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { usePolling } from "@/composables/usePolling";
import { useDebounced } from "@/composables/useTimers";
import { useUrlFilters } from "@/composables/useUrlFilters";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityExecution,
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityRuleState,
  QualityStatus,
  QualityTargetOrder,
  type QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { isExecutionInProgress } from "@/utils/dataQuality";
import { targetGuid } from "@/utils/dataQualityExecutions";
import { enumFromFilter, queryString } from "@/utils/queryFilters";

/**
 * Quality runs, read two ways.
 *
 * By target (default): one row per target with the latest scan batch that
 * touched it — the batch's result, failed checks, duration and how many batches
 * the target has behind it. Expanding a row lazily loads the target's scan
 * history, and each run expands to its checks.
 *
 * By execution (`?view=executions`): the flat per-check list, kept for auditing
 * and for the deep links that name one rule, target or run.
 *
 * The URL carries which view is open and which target/run is expanded, so a
 * refresh, a back/forward step or a shared link lands on the same place.
 */

/** How often a page that still has pending/running executions is reloaded. */
const POLL_INTERVAL_MS = 5000;

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();
const authStore = useAuthStore();

const canViewRule = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.get")
);

// ---- view: by target (default) or by execution ----

type ExecutionsView = "targets" | "executions";

const viewOptions = computed<Array<{ value: ExecutionsView; label: string }>>(
  () => [
    { value: "targets", label: t("dataQuality.executions.viewTargets") },
    { value: "executions", label: t("dataQuality.executions.viewExecutions") },
  ]
);

/** `?view=executions` is the flat list; anything else is the target view. */
const viewParam = computed(() => queryString(route.query.view));
/**
 * A run on its own is an execution list; with a target it is one of that
 * target's scans, opened in place. An explicit `view` always wins.
 */
const flatView = computed(() => {
  if (viewParam.value) {
    return viewParam.value === "executions";
  }
  return (
    queryString(route.query.run) !== "" &&
    queryString(route.query.target) === ""
  );
});

const activeView = computed<ExecutionsView>(() =>
  flatView.value ? "executions" : "targets"
);

/** The four query parameters the view state travels under. */
const LINK_QUERY_KEYS = ["view", "rule", "target", "run"] as const;

interface LinkFilters {
  view: string;
  rule: string;
  target: string;
  run: string;
}

const linkFilters = useUrlFilters<LinkFilters>({
  keys: LINK_QUERY_KEYS,
  parse: (query) => ({
    view: queryString(query.view),
    rule: queryString(query.rule),
    target: queryString(query.target),
    run: queryString(query.run),
  }),
  serialize: (filters) => {
    const owned: Record<string, string> = {};
    if (filters.view) {
      owned.view = filters.view;
    }
    if (filters.rule) {
      owned.rule = filters.rule;
    }
    if (filters.target) {
      owned.target = filters.target;
    }
    if (filters.run) {
      owned.run = filters.run;
    }
    return owned;
  },
});

/** Change part of the view state, preserving the rest of it and foreign params. */
function setQuery(
  patch: Partial<Record<keyof LinkFilters, string | undefined>>
) {
  const current = linkFilters.read();
  linkFilters.write({
    view: "view" in patch ? (patch.view ?? "") : current.view,
    rule: "rule" in patch ? (patch.rule ?? "") : current.rule,
    target: "target" in patch ? (patch.target ?? "") : current.target,
    run: "run" in patch ? (patch.run ?? "") : current.run,
  });
}

function switchView(view: ExecutionsView) {
  if (view === "executions") {
    setQuery({ view: "executions" });
    return;
  }
  // The target view is the default, so it needs no `view`; the flat-only
  // filters stop applying, and `target` becomes the expanded row.
  setQuery({ view: undefined, rule: undefined, run: undefined });
}

// ---- shared: rule titles ----

const ruleTitles = ref<Record<string, string>>({});

/**
 * Titles for the rules the rows name. Best effort: a caller without rules.list
 * still sees the executions it is allowed to read, with the rule id as label.
 */
async function loadRuleTitles() {
  try {
    // Archived rules are not in the default list, but their historical
    // executions still need a title rather than a raw resource id.
    const [active, archived] = await Promise.all([
      listAllQualityRules(),
      listAllQualityRules({ state: QualityRuleState.ARCHIVED }),
    ]);
    ruleTitles.value = Object.fromEntries(
      [...active, ...archived].map((rule) => [rule.name, rule.title])
    );
  } catch {
    ruleTitles.value = {};
  }
}

// ---- by target ----

const targetsError = ref("");
const targetPageSize = ref(50);
const targetSearch = ref("");
const appliedTargetSearch = ref("");
const targetSort = ref<"lastRun" | "attention">("lastRun");

const targetSortOptions = computed<
  Array<{ value: "lastRun" | "attention"; label: string }>
>(() => [
  { value: "lastRun", label: t("dataQuality.targets.sortLastRun") },
  { value: "attention", label: t("dataQuality.targets.sortAttention") },
]);

const targetOrderBy = computed(() =>
  targetSort.value === "attention"
    ? QualityTargetOrder.ATTENTION
    : QualityTargetOrder.LAST_RUN
);

const {
  items: targets,
  isLoading: targetsLoading,
  hasNext: targetsHasNext,
  hasPrevious: targetsHasPrevious,
  reset: resetTargets,
  refresh: refreshTargets,
  goNext: goNextTargets,
  goPrevious: goPreviousTargets,
} = usePagedFetch<QualityTargetSummary>({
  fetchPage: async (pageToken, signal) => {
    targetsError.value = "";
    return await listQualityTargets({
      pageSize: targetPageSize.value,
      pageToken,
      search: appliedTargetSearch.value,
      orderBy: targetOrderBy.value,
      signal,
    });
  },
  onError: (e) => {
    targetsError.value = handleError(e, "dataQuality.targets.loadFailed");
  },
});

// The template reads the rows through plain helpers; the paged ref is the one
// source, so the expander and the scroll watcher see the same rows.

function applyTargetSearch() {
  appliedTargetSearch.value = targetSearch.value.trim();
  void resetTargets();
}

/** One request answers a pause in typing, not every keystroke. */
const debouncedTargetSearch = useDebounced(() => applyTargetSearch(), 300);

watch(targetSearch, () => void debouncedTargetSearch());

watch(targetSort, () => void resetTargets());
watch(targetPageSize, () => void resetTargets());

function clearTargetSearch() {
  targetSearch.value = "";
  applyTargetSearch();
}

// ---- target rows: expansion from the URL ----

const expandedTargetGuid = computed(() =>
  flatView.value ? "" : queryString(route.query.target)
);
const expandedRun = computed(() =>
  flatView.value ? "" : queryString(route.query.run)
);

function isExpanded(summary: QualityTargetSummary): boolean {
  return (
    expandedTargetGuid.value !== "" &&
    expandedTargetGuid.value === targetGuid(summary)
  );
}

function toggleTarget(summary: QualityTargetSummary) {
  const guid = targetGuid(summary);
  if (!guid) {
    return;
  }
  if (expandedTargetGuid.value === guid) {
    setQuery({ target: undefined, run: undefined });
    return;
  }
  setQuery({ target: guid, run: undefined });
}

function onRunToggle(name: string) {
  setQuery({ run: name || undefined });
}

/**
 * A deep link names the target to expand; once its row exists, bring it into
 * view. The scroll is best effort (the workspace shell owns the scroll
 * container, and a test environment has no scrolling).
 */
watch(
  [targets, expandedTargetGuid, targetsLoading],
  async () => {
    const guid = expandedTargetGuid.value;
    if (!guid || targetsLoading.value) {
      return;
    }
    if (!targets.value.some((summary) => targetGuid(summary) === guid)) {
      return;
    }
    await nextTick();
    document
      .querySelector<HTMLElement>(`[data-target-guid="${guid}"]`)
      ?.scrollIntoView?.({ block: "center" });
  },
  { flush: "post" }
);

// ---- target row helpers ----

// ---- by execution (flat) ----

const state = ref("-");
const outcome = ref("-");
const trigger = ref("-");
const appliedState = ref("-");
const appliedOutcome = ref("-");
const appliedTrigger = ref("-");

const executionsPageSize = ref(50);
const executionsError = ref("");

const stateOptions: Array<{ value: QualityExecutionState; key: string }> = [
  {
    value: QualityExecutionState.PENDING,
    key: "dataQuality.executionState.pending",
  },
  {
    value: QualityExecutionState.RUNNING,
    key: "dataQuality.executionState.running",
  },
  {
    value: QualityExecutionState.COMPLETED,
    key: "dataQuality.executionState.completed",
  },
  {
    value: QualityExecutionState.ABANDONED,
    key: "dataQuality.executionState.abandoned",
  },
];

const outcomeOptions: Array<{ value: QualityStatus; key: string }> = [
  { value: QualityStatus.PASS, key: "dataQuality.status.pass" },
  { value: QualityStatus.WARN, key: "dataQuality.status.warn" },
  { value: QualityStatus.FAIL, key: "dataQuality.status.fail" },
  { value: QualityStatus.ERROR, key: "dataQuality.status.error" },
  { value: QualityStatus.SKIPPED, key: "dataQuality.status.skipped" },
  {
    value: QualityStatus.NOT_EVALUATED,
    key: "dataQuality.status.notEvaluated",
  },
];

const triggerOptions: Array<{ value: QualityExecutionTrigger; key: string }> = [
  {
    value: QualityExecutionTrigger.MANUAL,
    key: "dataQuality.executionTrigger.manual",
  },
  {
    value: QualityExecutionTrigger.SCHEDULED,
    key: "dataQuality.executionTrigger.scheduled",
  },
];

/** The rule resource name carried by `?rule=`, if any. */
const ruleFilter = computed(() => queryString(route.query.rule));
/** The target GUID carried by `?target=`, if any (flat view: a filter). */
const targetFilter = computed(() => queryString(route.query.target));
/** The run resource name carried by `?run=`, if any (flat view: a filter). */
const runFilter = computed(() => queryString(route.query.run));
/** A run id reads better than the full resource name in a chip. */
const runShortID = computed(
  () => runFilter.value.split("/").pop() || runFilter.value
);

const hasActiveFilters = computed(
  () =>
    appliedState.value !== "-" ||
    appliedOutcome.value !== "-" ||
    appliedTrigger.value !== "-" ||
    ruleFilter.value !== "" ||
    targetFilter.value !== "" ||
    runFilter.value !== ""
);

const {
  items: executions,
  isLoading: executionsLoading,
  hasNext: executionsHasNext,
  hasPrevious: executionsHasPrevious,
  reset: resetExecutions,
  refresh: refreshExecutions,
  goNext: goNextExecutions,
  goPrevious: goPreviousExecutions,
} = usePagedFetch<QualityExecution>({
  fetchPage: async (pageToken, signal) => {
    executionsError.value = "";
    return await listQualityExecutions({
      pageSize: executionsPageSize.value,
      pageToken,
      state: enumFromFilter(appliedState.value, [
        QualityExecutionState.UNSPECIFIED,
        QualityExecutionState.PENDING,
        QualityExecutionState.RUNNING,
        QualityExecutionState.COMPLETED,
        QualityExecutionState.ABANDONED,
      ]),
      outcome: enumFromFilter(appliedOutcome.value, [
        QualityStatus.UNSPECIFIED,
        QualityStatus.PASS,
        QualityStatus.WARN,
        QualityStatus.FAIL,
        QualityStatus.ERROR,
        QualityStatus.SKIPPED,
        QualityStatus.NOT_EVALUATED,
      ]),
      trigger: enumFromFilter(appliedTrigger.value, [
        QualityExecutionTrigger.UNSPECIFIED,
        QualityExecutionTrigger.MANUAL,
        QualityExecutionTrigger.SCHEDULED,
      ]),
      rule: ruleFilter.value,
      targetGuid: targetFilter.value,
      run: runFilter.value,
      signal,
    });
  },
  onError: (e) => {
    executionsError.value = handleError(e, "dataQuality.fetchExecutionsError");
  },
});

// A cursor belongs to the query that produced it, so a new page size restarts
// the walk at the first page.
watch(executionsPageSize, () => void resetExecutions());

// The URL is the filter for the cross-page links: a rule, target or run page
// that points here must reload when the query changes, not only on mount.
watch(
  () => [ruleFilter.value, targetFilter.value, runFilter.value],
  () => {
    // The URL filters are already read by `fetchPage`; keep any half-edited
    // Select values un-applied and just restart the cursor walk.
    if (flatView.value) {
      void resetExecutions();
    }
  }
);

function applyFilters() {
  appliedState.value = state.value;
  appliedOutcome.value = outcome.value;
  appliedTrigger.value = trigger.value;
  void resetExecutions();
}

function clearFilters() {
  state.value = "-";
  outcome.value = "-";
  trigger.value = "-";
  // The applied values, not only the Select model: clearing while a URL filter
  // is present must not leave the previous request's filters in force.
  appliedState.value = "-";
  appliedOutcome.value = "-";
  appliedTrigger.value = "-";
  const next = { ...linkFilters.read(), rule: "", target: "", run: "" };
  if (linkFilters.isCurrent(next)) {
    void resetExecutions();
    return;
  }
  linkFilters.write(next);
}

function clearRuleFilter() {
  setQuery({ rule: "" });
}

function clearTargetFilter() {
  setQuery({ target: "" });
}

function clearRunFilter() {
  // A run-only deep link is the flat list by derivation; dropping the chip must
  // keep the reader in that list instead of bouncing to the target view.
  setQuery({ run: "", ...(viewParam.value ? {} : { view: "executions" }) });
}

function ruleLabel(ruleName: string): string {
  return ruleTitles.value[ruleName] || qualityRuleID(ruleName);
}

/** A click that ends a text selection is a copy, not a navigation. */
function openExecution(execution: QualityExecution) {
  if (window.getSelection()?.toString()) {
    return;
  }
  void router.push({
    name: "DataQualityExecutionDetail",
    params: { executionId: execution.name.split("/") },
  });
}

// ---- loading and polling ----

const activeLoading = computed(() =>
  flatView.value ? executionsLoading.value : targetsLoading.value
);

function refreshCurrent() {
  if (flatView.value) {
    void refreshExecutions();
  } else {
    void refreshTargets();
  }
}

function hasInProgress(): boolean {
  return executions.value.some((execution) =>
    isExecutionInProgress(execution.state)
  );
}

// The poll keeps the table honest while the runner works; a manual refresh is
// not needed to see PENDING become COMPLETED.
const executionsPolling = usePolling(
  async (signal) => {
    // A manual refresh may have cleared the in-progress rows since this round
    // was scheduled; do not spend a request on a settled page.
    if (!hasInProgress()) {
      return;
    }
    await refreshExecutions({ signal });
  },
  { intervalMs: POLL_INTERVAL_MS, shouldContinue: hasInProgress }
);

// A flat page that shows a pending/running execution reloads until none is
// left, so the state the user clicked through from is not frozen on screen.
watch(executions, () => {
  if (flatView.value && hasInProgress()) {
    executionsPolling.start();
  } else {
    executionsPolling.stop();
  }
});

// Switching views re-reads the view's first page rather than showing another
// view's rows under different columns.
watch(flatView, (flat) => {
  if (flat) {
    void resetExecutions();
  } else {
    executionsPolling.stop();
    void resetTargets();
  }
});

// ---- export ----

/** The CSV follows the open view, so the file matches the table on screen. */
const exportFileName = computed(() =>
  flatView.value ? "quality-executions" : "quality-targets"
);

/**
 * The export carries the filters the list requests carry, which is the whole
 * directory rather than the visible page. The target view's own filters (the
 * search and the order) are server filters too, so both views export correctly.
 */
function exportCsv(signal: AbortSignal) {
  if (flatView.value) {
    return exportQualityExecutions({
      state: enumFromFilter(appliedState.value, [
        QualityExecutionState.UNSPECIFIED,
        QualityExecutionState.PENDING,
        QualityExecutionState.RUNNING,
        QualityExecutionState.COMPLETED,
        QualityExecutionState.ABANDONED,
      ]),
      outcome: enumFromFilter(appliedOutcome.value, [
        QualityStatus.UNSPECIFIED,
        QualityStatus.PASS,
        QualityStatus.WARN,
        QualityStatus.FAIL,
        QualityStatus.ERROR,
        QualityStatus.SKIPPED,
        QualityStatus.NOT_EVALUATED,
      ]),
      trigger: enumFromFilter(appliedTrigger.value, [
        QualityExecutionTrigger.UNSPECIFIED,
        QualityExecutionTrigger.MANUAL,
        QualityExecutionTrigger.SCHEDULED,
      ]),
      rule: ruleFilter.value,
      targetGuid: targetFilter.value,
      run: runFilter.value,
      signal,
    });
  }
  return exportQualityTargets({
    search: appliedTargetSearch.value,
    orderBy: targetOrderBy.value,
    signal,
  });
}

onMounted(() => {
  void loadRuleTitles();
  if (flatView.value) {
    void resetExecutions();
  } else {
    void resetTargets();
  }
});
</script>
