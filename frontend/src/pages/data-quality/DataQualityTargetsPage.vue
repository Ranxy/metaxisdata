<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.targets.title')"
      :description="t('dataQuality.targets.description')"
    >
      <template #meta>
        <p class="text-xs text-muted-foreground">
          {{ t("dataQuality.targets.workspaceHint") }}
        </p>
      </template>
      <template #actions>
        <Button
          v-if="canCreate"
          @click="startMonitoring"
        >
          <Plus class="mr-2 h-4 w-4" />
          {{ t("dataQuality.targets.startMonitoring") }}
        </Button>
        <ExportCsvButton
          :exporter="exportCsv"
          file-name="quality-targets"
        />
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
          variant="outline"
          as-child
        >
          <RouterLink :to="{ name: 'DataQualityChecks' }">
            <ListChecks class="mr-2 h-4 w-4" />
            {{ t("dataQuality.targets.allRules") }}
          </RouterLink>
        </Button>
      </template>
    </PageHeader>

    <Card>
      <!-- One toolbar. The server owns the search, the issue/draft filters and
           the order, so changing them re-reads the directory; only "failing"
           stays local, because the API filters one status at a time and a
           target is failing on FAIL *or* ERROR. Every state is in the URL, so a
           reload or a shared link lands on the same view. -->
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
            v-model="search"
            class="w-full"
            :placeholder="t('dataQuality.targets.searchPlaceholder')"
          />
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <Button
            v-for="option in toggleOptions"
            :key="option.key"
            :variant="toggles[option.key] ? 'secondary' : 'outline'"
            size="sm"
            :aria-pressed="toggles[option.key]"
            @click="toggle(option.key)"
          >
            <Check
              v-if="toggles[option.key]"
              class="mr-1.5 h-3.5 w-3.5"
            />
            {{ option.label }}
          </Button>
        </div>

        <div class="flex min-w-40 flex-col gap-1.5 [&>select]:hidden sm:ml-auto sm:w-44">
          <Label
            for="data-quality-target-sort"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.targets.sort") }}
          </Label>
          <Select
            :model-value="order"
            @update:model-value="changeOrder"
          >
            <SelectTrigger
              id="data-quality-target-sort"
              class="w-full"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="option in sortOptions"
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
        :loading="isLoading"
        :error="error"
        :title="t('dataQuality.targets.loadFailed')"
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

        <!-- Nothing monitored yet. Creating a check is the only way a target
             appears here, so that is the next step for a caller who may do it;
             everyone else gets the metadata browser, where a table or view can
             at least be inspected. -->
        <EmptyState
          v-if="summaries.length === 0 && !hasActiveFilters"
          :icon="ShieldCheck"
          :title="t('dataQuality.targets.emptyTitle')"
          :description="t('dataQuality.targets.emptyDescription')"
        >
          <Button
            v-if="canCreate"
            @click="startMonitoring"
          >
            <Plus class="mr-2 h-4 w-4" />
            {{ t("dataQuality.targets.startMonitoring") }}
          </Button>
          <Button
            v-else
            variant="outline"
            as-child
          >
            <RouterLink :to="{ name: 'MetadataBrowser' }">
              {{ t("dataQuality.targets.browseMetadata") }}
            </RouterLink>
          </Button>
        </EmptyState>

        <!-- The filters matched nothing; they are in the URL, so clearing them
             is the only action that can lead anywhere. -->
        <EmptyState
          v-else-if="visible.length === 0"
          :icon="SearchX"
          :title="t('dataQuality.targets.emptyFilteredTitle')"
          :description="t('dataQuality.targets.emptyFilteredDescription')"
        >
          <Button
            variant="outline"
            size="sm"
            @click="clearFilters"
          >
            {{ t("dataQuality.targets.clearFilters") }}
          </Button>
        </EmptyState>

        <Table
          v-else
          class="[&_th]:whitespace-nowrap"
        >
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("dataQuality.targets.columnTarget") }}</TableHead>
              <TableHead class="hidden md:table-cell">
                {{ t("dataQuality.targets.columnRules") }}
              </TableHead>
              <TableHead class="hidden lg:table-cell">
                {{ t("dataQuality.targets.columnColumns") }}
              </TableHead>
              <TableHead class="hidden md:table-cell">
                {{ t("dataQuality.targets.columnResult") }}
              </TableHead>
              <TableHead class="hidden text-right md:table-cell">
                {{ t("dataQuality.targets.columnIssues") }}
              </TableHead>
              <TableHead class="hidden text-right md:table-cell">
                {{ t("dataQuality.targets.columnDrafts") }}
              </TableHead>
              <TableHead class="w-12 text-right">
                <span class="sr-only">{{ t("common.actions") }}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="summary in visible"
              :key="summary.target?.metaGuid"
            >
              <TableCell>
                <div class="flex max-w-[14rem] min-w-0 flex-wrap items-center gap-2 md:max-w-md">
                  <!-- The target's name is the way into its workspace; the
                       actions column repeats the same destination as an icon so
                       keyboard readers reach it after the row's counts. -->
                  <RouterLink
                    :to="workspaceRoute(summary)"
                    class="truncate text-left font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    :title="targetFullPath(summary)"
                  >
                    {{ targetDisplayName(summary) }}
                  </RouterLink>
                  <Badge variant="outline">
                    {{ metaTypeSingularLabel(summary.target?.metaType ?? MetaType.UNSPECIFIED, t) }}
                  </Badge>
                  <Badge
                    v-if="summary.schemaChanged"
                    variant="warning"
                  >
                    {{ t("dataQuality.targetChanged") }}
                  </Badge>
                  <Badge
                    v-if="summary.orphaned"
                    variant="danger"
                    :title="t('dataQuality.workspace.orphanedHint')"
                  >
                    {{ t("dataQuality.orphaned") }}
                  </Badge>
                </div>
                <div
                  class="max-w-[14rem] truncate font-mono text-xs text-muted-foreground md:max-w-md"
                  :title="targetFullPath(summary)"
                >
                  {{ targetScope(summary) }}
                </div>
                <!-- Below md the secondary columns collapse in here, so the
                     table never grows a horizontal scrollbar on a phone. -->
                <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                  <span>
                    {{ t("dataQuality.targets.rulesCount", { count: summary.ruleCount }) }}
                  </span>
                  <Badge
                    v-if="hasResultBadge(summary)"
                    :variant="resultBadgeVariant(summary)"
                  >
                    {{ resultBadgeText(summary) }}
                  </Badge>
                  <span
                    v-if="summary.lastRunTime"
                    :title="formatDateTime(summary.lastRunTime, locale)"
                  >
                    {{ t("dataQuality.targets.columnLastRun") }}
                    {{ formatRelativeTime(summary.lastRunTime, locale) }}
                  </span>
                  <span v-else>{{ t("dataQuality.targets.neverRun") }}</span>
                  <span
                    v-if="qualitySignalCoverage(summary)"
                    :title="columnCoverageTitle(summary)"
                  >
                    {{ t("dataQuality.targets.columnColumns") }}
                    {{ columnCoverage(summary) }}
                  </span>
                  <span v-if="summary.openIssueCount > 0">
                    {{ t("dataQuality.targets.columnIssues") }}
                    {{ summary.openIssueCount }}
                  </span>
                  <span v-if="summary.pendingDraftCount > 0">
                    {{ t("dataQuality.targets.pendingDraftsCount", { count: summary.pendingDraftCount }) }}
                  </span>
                  <span
                    v-if="!qualitySignalAllPaused(summary)"
                    class="tabular-nums"
                  >
                    {{ scoreLine(summary) }}
                  </span>
                </div>
              </TableCell>
              <TableCell class="hidden md:table-cell">
                <div class="tabular-nums">
                  {{ t("dataQuality.targets.rulesCount", { count: summary.ruleCount }) }}
                </div>
                <!-- The enabled/total hint only matters while some rule is
                     paused, which explains why the target reports less than it
                     has. -->
                <div
                  v-if="summary.pausedRuleCount > 0"
                  class="text-xs tabular-nums text-muted-foreground"
                  :title="t('dataQuality.ruleState.enabled')"
                >
                  {{ summary.enabledRuleCount }} / {{ summary.ruleCount }}
                </div>
              </TableCell>
              <TableCell
                class="hidden tabular-nums lg:table-cell"
                :title="columnCoverageTitle(summary)"
              >
                {{ columnCoverage(summary) }}
              </TableCell>
              <TableCell class="hidden md:table-cell">
                <template v-if="hasResultBadge(summary) || summary.lastRunTime">
                  <Badge
                    v-if="hasResultBadge(summary)"
                    :variant="resultBadgeVariant(summary)"
                  >
                    {{ resultBadgeText(summary) }}
                  </Badge>
                  <div
                    v-if="summary.lastRunTime && !qualitySignalAllPaused(summary)"
                    class="mt-1 whitespace-nowrap text-xs text-muted-foreground"
                    :title="formatDateTime(summary.lastRunTime, locale)"
                  >
                    {{ formatRelativeTime(summary.lastRunTime, locale) }}
                  </div>
                </template>
                <span
                  v-else
                  class="text-sm text-muted-foreground"
                >
                  {{ t("dataQuality.targets.neverRun") }}
                </span>
                <!-- The score and pass rate are target aggregates like the
                     latest result above them, so they ride in that cell instead
                     of widening the table. A paused target has no current
                     verdict, so it shows its state and no aggregate. -->
                <div
                  v-if="!qualitySignalAllPaused(summary)"
                  class="mt-1 whitespace-nowrap text-xs tabular-nums text-muted-foreground"
                  :title="
                    summary.recentExecutionCount > 0
                      ? t('dataQuality.targets.scoreLineHint', {
                          window: RECENT_PASS_RATE_WINDOW,
                          judged: summary.recentExecutionCount,
                        })
                      : undefined
                  "
                >
                  {{ scoreLine(summary) }}
                </div>
              </TableCell>
              <TableCell class="hidden text-right tabular-nums md:table-cell">
                {{ summary.openIssueCount > 0 ? summary.openIssueCount : "-" }}
              </TableCell>
              <TableCell class="hidden text-right tabular-nums md:table-cell">
                {{ summary.pendingDraftCount > 0 ? summary.pendingDraftCount : "-" }}
              </TableCell>
              <TableCell class="text-right">
                <Button
                  variant="ghost"
                  size="icon"
                  class="h-8 w-8"
                  as-child
                >
                  <RouterLink
                    :to="workspaceRoute(summary)"
                    :title="t('dataQuality.targets.openWorkspace')"
                    :aria-label="t('dataQuality.targets.openWorkspace')"
                  >
                    <ChevronRight class="h-4 w-4" />
                  </RouterLink>
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </PageState>
    </Card>

    <!-- The DQ section's own entry to the product: pick a table or view and go
         straight to its contract workspace with the check dialog open. -->
    <TargetPicker
      :open="pickerOpen"
      @update:open="onPickerOpen"
      @select="onTargetPicked"
    />
  </div>
</template>

<script setup lang="ts">
import {
  Check,
  ChevronRight,
  ListChecks,
  Plus,
  RefreshCcw,
  SearchX,
  ShieldCheck,
} from "lucide-vue-next";
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  type LocationQuery,
  RouterLink,
  useRoute,
  useRouter,
} from "vue-router";
import { exportQualityTargets, listQualityTargets } from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import ExportCsvButton from "@/components/data-quality/ExportCsvButton.vue";
import TargetPicker from "@/components/data-quality/TargetPicker.vue";
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
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useAsync } from "@/composables/useAsync";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useDebounced } from "@/composables/useTimers";
import { useUrlFilters } from "@/composables/useUrlFilters";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityStatus,
  QualityTargetOrder,
  type QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  type QualityBadgeVariant,
  qualityStatusKey,
  qualityStatusVariant,
} from "@/utils/dataQuality";
import type { TargetSelection } from "@/utils/dataQualityForm";
import {
  formatPassRate,
  formatQualityScore,
} from "@/utils/dataQualityOverview";
import {
  qualitySignalAllPaused,
  qualitySignalCoverage,
} from "@/utils/dataQualitySignal";
import {
  targetDisplayName,
  targetFullPath,
  targetScope,
} from "@/utils/dataQualityTarget";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";
import { metaTypeSingularLabel } from "@/utils/metaType";
import { queryString, readFlag } from "@/utils/queryFilters";

/**
 * The quality directory: every target with rules, one row per table or view.
 *
 * Filtering and ordering are server concerns: every row is an aggregate the
 * server computes (counts, worst result, issue and draft totals), so the page
 * passes the search, the issue/draft filters and the order straight to
 * `ListQualityTargets` and renders what comes back. Only "failing" stays local,
 * because a target is failing on FAIL *or* ERROR while the API filters one
 * status at a time.
 *
 * The filter state lives in the URL: reload, back/forward and a shared link all
 * land on the same view, and the first request already carries it.
 */

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const canCreate = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.create")
);

/** The three "needs attention" filters, all off by default. */
type TargetToggle = "failing" | "issues" | "drafts";

/** How the sort select names an order; the API enum is derived from it. */
type TargetOrder = "attention" | "name" | "rules" | "lastRun";

/** The directory filters as the URL and the server both understand them. */
interface TargetFilters {
  search: string;
  failing: boolean;
  issues: boolean;
  drafts: boolean;
  order: TargetOrder;
}

/** The query parameter each filter travels under. */
const FILTER_QUERY_KEYS = ["q", "issues", "drafts", "failing", "sort"] as const;

const pickerOpen = ref(false);
const error = ref("");
const search = ref("");
/** The search term the last request and the export used, not the live input. */
const appliedSearch = ref("");
const toggles = reactive<Record<TargetToggle, boolean>>({
  failing: false,
  issues: false,
  drafts: false,
});
const order = ref<TargetOrder>("attention");

const sortOptions = computed<Array<{ value: TargetOrder; label: string }>>(
  () => [
    {
      value: "attention",
      label: t("dataQuality.targets.sortAttention"),
    },
    { value: "name", label: t("dataQuality.targets.sortName") },
    { value: "rules", label: t("dataQuality.targets.sortRules") },
    { value: "lastRun", label: t("dataQuality.targets.sortLastRun") },
  ]
);

const selectedOrder = computed<QualityTargetOrder>(() => {
  switch (order.value) {
    case "name":
      return QualityTargetOrder.NAME;
    case "rules":
      return QualityTargetOrder.RULE_COUNT;
    case "lastRun":
      return QualityTargetOrder.LAST_RUN;
    default:
      return QualityTargetOrder.ATTENTION;
  }
});

const toggleOptions = computed<Array<{ key: TargetToggle; label: string }>>(
  () => [
    { key: "failing", label: t("dataQuality.targets.onlyFailing") },
    { key: "issues", label: t("dataQuality.targets.onlyIssues") },
    { key: "drafts", label: t("dataQuality.targets.onlyDrafts") },
  ]
);

/** The filters a URL carries: reload, shared link and back/forward. */
function parseFilters(query: LocationQuery): TargetFilters {
  const sort = queryString(query.sort);
  return {
    search: queryString(query.q).trim(),
    failing: readFlag(query.failing),
    issues: readFlag(query.issues),
    drafts: readFlag(query.drafts),
    order:
      sort === "name" || sort === "rules" || sort === "lastRun"
        ? sort
        : "attention",
  };
}

/** Only non-default values are written, so the URL stays readable. */
function serializeFilters(filters: TargetFilters): Record<string, string> {
  const owned: Record<string, string> = {};
  if (filters.search) {
    owned.q = filters.search;
  }
  if (filters.failing) {
    owned.failing = "1";
  }
  if (filters.issues) {
    owned.issues = "1";
  }
  if (filters.drafts) {
    owned.drafts = "1";
  }
  if (filters.order !== "attention") {
    owned.sort = filters.order;
  }
  return owned;
}

/** The filters the controls currently express. */
function currentFilters(): TargetFilters {
  return {
    search: search.value.trim(),
    failing: toggles.failing,
    issues: toggles.issues,
    drafts: toggles.drafts,
    order: order.value,
  };
}

const urlFilters = useUrlFilters<TargetFilters>({
  keys: FILTER_QUERY_KEYS,
  parse: parseFilters,
  serialize: serializeFilters,
});

// Read the URL before the first request: the first read already carries the
// filters, so a reloaded or shared link never flashes the unfiltered directory.
const initialFilters = urlFilters.read();
search.value = initialFilters.search;
appliedSearch.value = initialFilters.search;
toggles.failing = initialFilters.failing;
toggles.issues = initialFilters.issues;
toggles.drafts = initialFilters.drafts;
order.value = initialFilters.order;

/**
 * The directory's one aggregate read. `useAsync` owns the loading, error and
 * supersede behaviour; the page only renders the rows.
 */
const {
  data: summaryData,
  loading: isLoading,
  refresh: refreshTargets,
} = useAsync(
  async () => {
    const page = await listQualityTargets({
      pageSize: 1000,
      search: appliedSearch.value,
      hasOpenIssue: toggles.issues,
      hasPendingDraft: toggles.drafts,
      orderBy: selectedOrder.value,
    });
    return page.items;
  },
  {
    initialData: [] as QualityTargetSummary[],
    onError: (e: unknown) => {
      error.value = formatError(e, "dataQuality.targets.loadFailed");
    },
  }
);
const summaries = computed(() => summaryData.value ?? []);

/** Only the client-side "failing" filter narrows the rows afterwards. */
const visible = computed(() =>
  toggles.failing
    ? summaries.value.filter(
        (summary) =>
          summary.worstStatus === QualityStatus.FAIL ||
          summary.worstStatus === QualityStatus.ERROR
      )
    : summaries.value
);

const hasActiveFilters = computed(
  () =>
    search.value.trim() !== "" ||
    toggles.failing ||
    toggles.issues ||
    toggles.drafts
);

/**
 * How many of a target's most recent completed executions the server reads for
 * its pass rate. Mirrors `qualityTargetRecentExecutionLimit` in the backend;
 * `recentExecutionCount` is only the judged subset of this window, so the two
 * must not be confused in the tooltip.
 */
const RECENT_PASS_RATE_WINDOW = 20;

/**
 * The row's column coverage as `covered/total`: how many of the target's current
 * columns an enabled check reads. "-" means there is no denominator — the target
 * no longer resolves — which is not the same as "0 of its columns are watched".
 */
function columnCoverage(summary: QualityTargetSummary): string {
  const coverage = qualitySignalCoverage(summary);
  return coverage === null ? "-" : `${coverage.covered}/${coverage.total}`;
}

/** The coverage cell's tooltip: the share, or why it has no denominator. */
function columnCoverageTitle(summary: QualityTargetSummary): string {
  const coverage = qualitySignalCoverage(summary);
  if (coverage !== null) {
    return t("dataQuality.coverage.coveredSummary", {
      covered: coverage.covered,
      total: coverage.total,
    });
  }
  // 0/0 has two causes, and only one of them is an orphan: a resolvable object
  // whose registry record carries no columns is not gone, it just has nothing to
  // cover.
  return summary.orphaned
    ? t("dataQuality.targets.coverageUnresolved")
    : t("dataQuality.targets.coverageNoColumns");
}

/**
 * Whether the result cell has a badge to draw: a verdict, or the neutral state
 * of a target whose checks are all paused. Paused is not a verdict.
 */
function hasResultBadge(summary: QualityTargetSummary): boolean {
  return (
    qualitySignalAllPaused(summary) ||
    summary.worstStatus !== QualityStatus.UNSPECIFIED
  );
}

/** The result badge's word. */
function resultBadgeText(summary: QualityTargetSummary): string {
  return qualitySignalAllPaused(summary)
    ? t("dataQuality.signal.allPaused")
    : t(qualityStatusKey(summary.worstStatus));
}

/** The result badge's tone; a paused target is never a health colour. */
function resultBadgeVariant(
  summary: QualityTargetSummary
): QualityBadgeVariant {
  return qualitySignalAllPaused(summary)
    ? "neutral"
    : qualityStatusVariant(summary.worstStatus);
}

/**
 * The row's aggregate health under the latest result: the quality score over
 * the target's judged checks and the pass share of its recent executions. Both
 * are "-" until a check has been judged; that is not a zero.
 */
function scoreLine(summary: QualityTargetSummary): string {
  return t("dataQuality.targets.scoreLine", {
    score: formatQualityScore(summary.qualityScore),
    passRate: formatPassRate(summary.recentPassRate),
  });
}

/** Push the current filters to the URL and let the server answer them. */
function applyFilters() {
  appliedSearch.value = search.value.trim();
  urlFilters.write(currentFilters());
  void load();
}

function toggle(key: TargetToggle) {
  toggles[key] = !toggles[key];
  if (key === "failing") {
    // Client-side only: the API filters one status at a time, so a target that
    // failed vs. one that errored would otherwise need two requests.
    urlFilters.write(currentFilters());
    return;
  }
  applyFilters();
}

function changeOrder(value: unknown) {
  order.value =
    value === "name" || value === "rules" || value === "lastRun"
      ? value
      : "attention";
  applyFilters();
}

function clearFilters() {
  search.value = "";
  toggles.failing = false;
  toggles.issues = false;
  toggles.drafts = false;
  applyFilters();
}

/**
 * The export carries the directory's server filters, so the file holds every
 * matching target rather than the visible page. "Failing" travels as its own
 * flag because the API's status filter names one state while the directory's
 * switch means FAIL or ERROR.
 */
function exportCsv(signal: AbortSignal) {
  return exportQualityTargets({
    search: appliedSearch.value,
    failingOnly: toggles.failing,
    hasOpenIssue: toggles.issues,
    hasPendingDraft: toggles.drafts,
    orderBy: selectedOrder.value,
    signal,
  });
}

/** One request when the reader pauses, not one per keystroke. */
const debouncedSearch = useDebounced(() => {
  // An external navigation already adopted this query; do not answer it with
  // a second request.
  if (urlFilters.isCurrent(currentFilters())) {
    return;
  }
  applyFilters();
}, 300);

watch(search, () => void debouncedSearch());

// Back/forward or a pasted URL changes the query from outside; adopt it and let
// the server answer. The replace this composable writes matches the controls,
// so it is ignored and cannot loop.
urlFilters.watchExternal((next) => {
  if (urlFilters.isCurrent(currentFilters())) {
    return;
  }
  search.value = next.search;
  appliedSearch.value = next.search;
  toggles.failing = next.failing;
  toggles.issues = next.issues;
  toggles.drafts = next.drafts;
  order.value = next.order;
  void load();
});

/** A target's workspace: the GUID as repeated path segments plus its meta type. */
function workspaceRoute(summary: QualityTargetSummary) {
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(summary.target?.metaGuid ?? "") },
    query: {
      metaType: String(summary.target?.metaType ?? MetaType.UNSPECIFIED),
    },
  };
}

async function load() {
  error.value = "";
  await refreshTargets();
}

onMounted(async () => {
  if (readFlag(route.query.new)) {
    // A link from the overview asks the directory to open its picker. Only a
    // caller allowed to create a check gets that; either way the flag is
    // consumed, so a refresh does not repeat the visit.
    if (canCreate.value) {
      pickerOpen.value = true;
    }
    const query = { ...route.query };
    delete query.new;
    await router.replace({ query });
  }
});

function startMonitoring() {
  pickerOpen.value = true;
}

function onPickerOpen(open: boolean) {
  pickerOpen.value = open;
}

function onTargetPicked(selection: TargetSelection) {
  void router.push({
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(selection.guid) },
    query: { metaType: String(selection.metaType), add: "1" },
  });
}
</script>
