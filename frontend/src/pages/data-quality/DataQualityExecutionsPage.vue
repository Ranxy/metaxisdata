<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.executionsTitle')"
      :description="t('dataQuality.executionsDescription')"
    >
      <template #actions>
        <Button
          variant="outline"
          :disabled="isLoading"
          @click="refreshPage"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': isLoading }"
          />
          {{ t("common.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <Card>
      <!-- One filter row, applied together rather than per keystroke: a Select
           change does not re-query until Apply. A rule or target carried in the
           URL is shown as a removable chip instead, because it came from a
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
      </div>

      <PageState
        :loading="isLoading && executions.length === 0"
        :error="error"
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
              <TableRow
                v-for="execution in executions"
                :key="execution.name"
                class="cursor-pointer"
                @click="openExecution(execution)"
              >
                <TableCell>
                  <div class="flex flex-wrap items-center gap-2">
                    <Badge
                      :variant="qualityExecutionStateVariant(execution.state)"
                    >
                      {{ t(qualityExecutionStateKey(execution.state)) }}
                    </Badge>
                    <Badge
                      v-if="execution.outcome !== QualityStatus.UNSPECIFIED"
                      :variant="qualityStatusVariant(execution.outcome)"
                      :title="t('dataQuality.outcome')"
                    >
                      {{ t(qualityStatusKey(execution.outcome)) }}
                    </Badge>
                  </div>
                  <div class="mt-1 font-mono text-xs text-muted-foreground">
                    {{ qualityExecutionID(execution.name) }}
                  </div>
                  <!-- Below md the rule, target, observed, rows, trigger,
                       duration and started columns collapse in here. -->
                  <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                    <RouterLink
                      v-if="canViewRule"
                      :to="ruleTargetRoute(execution)"
                      class="min-w-0 truncate hover:underline"
                      @click.stop
                    >
                      {{ ruleLabel(execution.rule) }}
                    </RouterLink>
                    <span
                      v-else
                      class="min-w-0 truncate"
                    >
                      {{ ruleLabel(execution.rule) }}
                    </span>
                    <span aria-hidden="true">·</span>
                    <RouterLink
                      v-if="hasTarget(execution)"
                      :to="executionTargetRoute(execution)"
                      class="min-w-0 truncate hover:underline"
                      :title="targetTitle(execution)"
                      @click.stop
                    >
                      {{ targetPath(execution) }}
                    </RouterLink>
                    <span
                      v-else
                      class="min-w-0 truncate"
                      :title="targetTitle(execution)"
                    >
                      {{ targetPath(execution) }}
                    </span>
                    <span aria-hidden="true">·</span>
                    <span class="tabular-nums">{{ observedLabel(execution) }}</span>
                    <span aria-hidden="true">·</span>
                    <span class="tabular-nums">{{ rowsLabel(execution) }}</span>
                    <span aria-hidden="true">·</span>
                    <span>{{ t(qualityExecutionTriggerKey(execution.trigger)) }}</span>
                    <span aria-hidden="true">·</span>
                    <span>{{ durationLabel(execution) }}</span>
                    <span aria-hidden="true">·</span>
                    <span :title="formatDateTime(execution.startTime, locale)">
                      {{ formatRelativeTime(execution.startTime, locale) }}
                    </span>
                  </div>
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  <RouterLink
                    v-if="canViewRule"
                    :to="ruleTargetRoute(execution)"
                    class="block max-w-[11rem] truncate font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none lg:max-w-[14rem]"
                    :title="ruleLabel(execution.rule)"
                    @click.stop
                  >
                    {{ ruleLabel(execution.rule) }}
                  </RouterLink>
                  <div
                    v-else
                    class="max-w-[11rem] truncate font-medium lg:max-w-[14rem]"
                    :title="ruleLabel(execution.rule)"
                  >
                    {{ ruleLabel(execution.rule) }}
                  </div>
                </TableCell>
                <TableCell class="hidden max-w-[14rem] md:table-cell">
                  <RouterLink
                    v-if="hasTarget(execution)"
                    :to="executionTargetRoute(execution)"
                    class="block truncate text-xs hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    :title="targetTitle(execution)"
                    @click.stop
                  >
                    {{ targetPath(execution) }}
                  </RouterLink>
                  <div
                    v-else
                    class="truncate text-xs"
                    :title="targetTitle(execution)"
                  >
                    {{ targetPath(execution) }}
                  </div>
                </TableCell>
                <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                  {{ observedLabel(execution) }}
                </TableCell>
                <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                  {{ rowsLabel(execution) }}
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  {{ t(qualityExecutionTriggerKey(execution.trigger)) }}
                </TableCell>
                <TableCell
                  class="hidden whitespace-nowrap text-muted-foreground md:table-cell"
                  :title="formatDateTime(execution.startTime, locale)"
                >
                  {{ formatRelativeTime(execution.startTime, locale) }}
                  <div class="mt-1 text-xs tabular-nums">
                    {{ t("dataQuality.duration") }} {{ durationLabel(execution) }}
                  </div>
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

          <!-- The pager footer keeps the card's content inset. -->
          <div class="px-6 pb-6">
            <TablePager
              v-model:page-size="pageSize"
              :has-previous="hasPrevious"
              :has-next="hasNext"
              :disabled="isLoading"
              @previous="goPreviousPage"
              @next="goNextPage"
            />
          </div>
        </div>
      </PageState>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { Activity, Eye, RefreshCcw, X } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  type RouteLocationRaw,
  RouterLink,
  useRoute,
  useRouter,
} from "vue-router";
import {
  listAllQualityRules,
  listQualityExecutions,
  qualityExecutionID,
  qualityRuleID,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityExecution,
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityRuleState,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  executionDurationSeconds,
  formatObservedValue,
  formatRowCount,
  formatSeconds,
  formatTargetPath,
  isExecutionInProgress,
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
 * Every rule check the workspace has run, newest first. One row is one
 * execution — one rule checked once — so the list reports the lifecycle state
 * and, once a verdict exists, the outcome on the same row. A rule or a target
 * can narrow the list through the URL, which is how the rule page links to this
 * rule's history.
 */

/** How often a page that still has pending/running executions is reloaded. */
const POLL_INTERVAL_MS = 5000;

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();
const authStore = useAuthStore();

const canViewRule = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.get")
);

// The filter controls hold strings for the Select; the enums are built at the
// request boundary, where they belong. `-` is the "any" item.
const state = ref("-");
const outcome = ref("-");
const trigger = ref("-");
const appliedState = ref("-");
const appliedOutcome = ref("-");
const appliedTrigger = ref("-");

const pageSize = ref(50);
const error = ref("");
const ruleTitles = ref<Record<string, string>>({});

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
/** The target GUID carried by `?target=`, if any. */
const targetFilter = computed(() => queryString(route.query.target));

const hasActiveFilters = computed(
  () =>
    appliedState.value !== "-" ||
    appliedOutcome.value !== "-" ||
    appliedTrigger.value !== "-" ||
    ruleFilter.value !== "" ||
    targetFilter.value !== ""
);

const {
  items: executions,
  isLoading,
  hasNext,
  hasPrevious,
  reset: resetPage,
  refresh: refreshPage,
  goNext: goNextPage,
  goPrevious: goPreviousPage,
} = usePagedFetch<QualityExecution>({
  fetchPage: async (pageToken, signal) => {
    error.value = "";
    return await listQualityExecutions({
      pageSize: pageSize.value,
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
      signal,
    });
  },
  onError: (e) => {
    error.value = handleError(e, "dataQuality.fetchExecutionsError");
  },
});

// A cursor belongs to the query that produced it, so a new page size restarts
// the walk at the first page.
watch(pageSize, () => void resetPage());

// The URL is the filter for the cross-page links: a rule page that points here
// with `?rule=` must reload when the query changes, not only when the component
// mounts.
watch(
  () => [ruleFilter.value, targetFilter.value],
  () => {
    // The URL filters are already read by `fetchPage`; keep any half-edited
    // Select values un-applied and just restart the cursor walk.
    void resetPage();
  }
);

/**
 * The Select's value as an enum member. `-` and any value the enum no longer
 * has mean "no filter", so a stale URL or a hand-edited state cannot break the
 * request.
 */
function enumFromFilter<T extends number>(value: string, members: T[]): T {
  const parsed = Number(value);
  return members.includes(parsed as T) ? (parsed as T) : members[0];
}

function queryString(value: unknown): string {
  if (Array.isArray(value)) {
    return String(value[0] ?? "");
  }
  return value === undefined || value === null ? "" : String(value);
}

function applyFilters() {
  appliedState.value = state.value;
  appliedOutcome.value = outcome.value;
  appliedTrigger.value = trigger.value;
  void resetPage();
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
  const query = { ...route.query };
  delete query.rule;
  delete query.target;
  if (Object.keys(query).length !== Object.keys(route.query).length) {
    void router.replace({ query });
    return;
  }
  void resetPage();
}

function clearRuleFilter() {
  const query = { ...route.query };
  delete query.rule;
  void router.replace({ query });
}

function clearTargetFilter() {
  const query = { ...route.query };
  delete query.target;
  void router.replace({ query });
}

function ruleLabel(ruleName: string): string {
  return ruleTitles.value[ruleName] || qualityRuleID(ruleName);
}

function ruleRoute(ruleName: string) {
  return {
    name: "DataQualityRuleRedirect",
    params: { ruleId: ruleName.split("/") },
  };
}

/** Whether the execution carries the target its rule watched. */
function hasTarget(execution: QualityExecution): boolean {
  return Boolean(execution.target?.metaGuid);
}

/**
 * The target workspace for one execution, opening the check drawer when a check
 * name is given. Undefined when the execution carries no target: historical
 * rows can predate the target snapshot.
 */
function targetWorkspaceRoute(
  execution: QualityExecution,
  check = ""
): RouteLocationRaw | undefined {
  const target = execution.target;
  if (!target?.metaGuid) {
    return undefined;
  }
  // A snapshot without a type is not an error: the target page derives it from
  // the summary or the rules, so the query carries only what it knows.
  const query: Record<string, string> = {};
  if (target.metaType !== MetaType.UNSPECIFIED) {
    query.metaType = String(target.metaType);
  }
  if (check) {
    query.check = check;
  }
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.metaGuid) },
    query,
  };
}

/**
 * A rule name links into its target's workspace with the check selected. A row
 * without a target snapshot falls back to the retired per-rule URL, whose
 * redirect resolves the target itself.
 */
function ruleTargetRoute(execution: QualityExecution): RouteLocationRaw {
  return (
    targetWorkspaceRoute(execution, execution.rule) ?? ruleRoute(execution.rule)
  );
}

/** The target itself, without selecting a check. */
function executionTargetRoute(execution: QualityExecution): RouteLocationRaw {
  return targetWorkspaceRoute(execution) ?? ruleRoute(execution.rule);
}

function targetPath(execution: QualityExecution): string {
  const path = formatTargetPath(execution.target?.path);
  return path === "-" ? execution.target?.metaGuid || "-" : path;
}

/** Every target's instance and path, for the cell's tooltip. */
function targetTitle(execution: QualityExecution): string {
  const path = targetPath(execution);
  const scope = targetScopeLabel(execution.target?.path);
  return scope === "-" ? path : `${scope} · ${path}`;
}

/** The verdict's measured value, or "-" while there is no verdict yet. */
function observedLabel(execution: QualityExecution): string {
  if (execution.outcome === QualityStatus.UNSPECIFIED) {
    return "-";
  }
  return formatObservedValue(
    execution.observedValue,
    execution.observedKind,
    locale.value
  );
}

function rowsLabel(execution: QualityExecution): string {
  if (execution.outcome === QualityStatus.UNSPECIFIED) {
    return "-";
  }
  return `${formatRowCount(execution.failedRows, locale.value)} / ${formatRowCount(execution.totalRows, locale.value)}`;
}

function durationLabel(execution: QualityExecution): string {
  const seconds = executionDurationSeconds(execution);
  return seconds === undefined ? "-" : formatSeconds(seconds);
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

let pollTimer: number | undefined;

function hasInProgress(): boolean {
  return executions.value.some((execution) =>
    isExecutionInProgress(execution.state)
  );
}

function startPolling() {
  if (pollTimer !== undefined) {
    return;
  }
  pollTimer = window.setInterval(() => {
    // The poll keeps the table honest while the runner works; a manual refresh
    // is not needed to see PENDING become COMPLETED.
    if (hasInProgress()) {
      void refreshPage();
    } else {
      stopPolling();
    }
  }, POLL_INTERVAL_MS);
}

function stopPolling() {
  if (pollTimer !== undefined) {
    clearInterval(pollTimer);
    pollTimer = undefined;
  }
}

// A page that shows a pending/running execution reloads until none is left, so
// the state the user clicked through from is not frozen on screen.
watch(executions, () => {
  if (hasInProgress()) {
    startPolling();
  } else {
    stopPolling();
  }
});

onMounted(() => {
  void loadRuleTitles();
  void resetPage();
});

onBeforeUnmount(stopPolling);
</script>
