<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.issuesTitle')"
      :description="t('dataQuality.issuesDescription')"
    >
      <template #actions>
        <ExportCsvButton
          :exporter="exportCsv"
          file-name="quality-issues"
        />
        <Button
          variant="outline"
          :disabled="isLoading"
          @click="() => refreshPage()"
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
      <div class="flex flex-wrap items-end gap-3 border-b p-4">
        <div class="flex min-w-36 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-issue-status"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.filterIssueStatus") }}
          </Label>
          <Select v-model="status">
            <SelectTrigger
              id="data-quality-issue-status"
              class="w-full sm:w-40"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.allIssueStatuses") }}
              </SelectItem>
              <SelectItem
                v-for="option in statusOptions"
                :key="option.value"
                :value="String(option.value)"
              >
                {{ t(option.key) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex min-w-32 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-issue-severity"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.filterSeverity") }}
          </Label>
          <Select v-model="severity">
            <SelectTrigger
              id="data-quality-issue-severity"
              class="w-full sm:w-36"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.allSeverities") }}
              </SelectItem>
              <SelectItem
                v-for="option in severityOptions"
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
      </div>

      <PageState
        :loading="isLoading"
        :error="error"
      >
        <EmptyState
          v-if="issues.length === 0"
          :icon="CircleAlert"
          :title="
            hasActiveFilters
              ? t('dataQuality.noFilteredIssues')
              : t('dataQuality.noIssues')
          "
          :description="hasActiveFilters ? '' : t('dataQuality.noIssuesHint')"
        >
          <Button
            v-if="hasActiveFilters"
            variant="outline"
            size="sm"
            @click="clearFilters"
          >
            {{ t("common.filter.clear") }}
          </Button>
        </EmptyState>

        <div v-else>
          <Table class="[&_th]:whitespace-nowrap">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("dataQuality.issue") }}</TableHead>
                <TableHead>{{ t("dataQuality.resultStatus") }}</TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.filterSeverity") }}
                </TableHead>
                <TableHead class="hidden whitespace-nowrap md:table-cell">
                  {{ t("dataQuality.firstSeen") }}
                </TableHead>
                <TableHead class="hidden whitespace-nowrap md:table-cell">
                  {{ t("dataQuality.lastSeen") }}
                </TableHead>
                <TableHead class="hidden text-right sm:table-cell">
                  {{ t("dataQuality.occurrences") }}
                </TableHead>
                <TableHead class="w-12 text-right">
                  <span class="sr-only">{{ t("common.actions") }}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="issue in issues"
                :key="issue.name"
                class="cursor-pointer"
                @click="openIssue(issue)"
              >
                <TableCell>
                  <!-- An issue names a check that failed on a target; both lines
                       lead into that target's workspace, the check selected on
                       the first one. -->
                  <RouterLink
                    v-if="hasTarget(issue)"
                    :to="issueRuleRoute(issue)"
                    class="block max-w-[14rem] truncate text-left font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 md:max-w-md"
                    :title="ruleTitle(issue)"
                    @click.stop
                  >
                    {{ ruleTitle(issue) }}
                  </RouterLink>
                  <button
                    v-else
                    type="button"
                    class="block max-w-[14rem] truncate text-left font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 md:max-w-md"
                    :title="ruleTitle(issue)"
                    @click.stop="openIssue(issue)"
                  >
                    {{ ruleTitle(issue) }}
                  </button>
                  <RouterLink
                    v-if="hasTarget(issue)"
                    :to="issueTargetRoute(issue)"
                    class="block max-w-[14rem] truncate font-mono text-xs text-muted-foreground hover:underline md:max-w-md"
                    :title="targetLabel(issue)"
                    @click.stop
                  >
                    {{ targetLabel(issue) }}
                  </RouterLink>
                  <div
                    v-else
                    class="max-w-[14rem] truncate font-mono text-xs text-muted-foreground md:max-w-md"
                    :title="targetLabel(issue)"
                  >
                    {{ targetLabel(issue) }}
                  </div>
                  <!-- Below md the severity, times and occurrences collapse in here. -->
                  <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                    <Badge :variant="qualitySeverityVariant(issue.severity)">
                      {{ t(qualitySeverityKey(issue.severity)) }}
                    </Badge>
                    <span :title="formatDateTime(issue.lastSeenTime, locale)">
                      {{ formatRelativeTime(issue.lastSeenTime, locale) }}
                    </span>
                    <span>{{ t("dataQuality.occurrenceCount", { count: issue.occurrenceCount }) }}</span>
                  </div>
                </TableCell>
                <TableCell>
                  <Badge :variant="qualityIssueStatusVariant(issue.status)">
                    {{ t(qualityIssueStatusKey(issue.status)) }}
                  </Badge>
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  <Badge :variant="qualitySeverityVariant(issue.severity)">
                    {{ t(qualitySeverityKey(issue.severity)) }}
                  </Badge>
                </TableCell>
                <TableCell
                  class="hidden whitespace-nowrap text-muted-foreground md:table-cell"
                  :title="formatDateTime(issue.firstSeenTime, locale)"
                >
                  {{ formatRelativeTime(issue.firstSeenTime, locale) }}
                </TableCell>
                <TableCell
                  class="hidden whitespace-nowrap text-muted-foreground md:table-cell"
                  :title="formatDateTime(issue.lastSeenTime, locale)"
                >
                  {{ formatRelativeTime(issue.lastSeenTime, locale) }}
                </TableCell>
                <TableCell class="hidden text-right tabular-nums sm:table-cell">
                  {{ issue.occurrenceCount }}
                </TableCell>
                <TableCell
                  class="text-right"
                  @click.stop
                >
                  <Button
                    variant="ghost"
                    size="icon"
                    class="h-8 w-8"
                    :title="t('dataQuality.viewIssue')"
                    :aria-label="t('dataQuality.viewIssue')"
                    @click="openIssue(issue)"
                  >
                    <Eye class="h-4 w-4 text-muted-foreground" />
                  </Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>

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
import { CircleAlert, Eye, RefreshCcw } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  type LocationQuery,
  type RouteLocationRaw,
  RouterLink,
  useRouter,
} from "vue-router";
import {
  exportQualityIssues,
  listAllQualityRules,
  listQualityIssues,
  qualityRuleID,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import ExportCsvButton from "@/components/data-quality/ExportCsvButton.vue";
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
import { useUrlFilters } from "@/composables/useUrlFilters";
import {
  type QualityIssue,
  QualityIssueStatus,
  type QualitySeverity,
  QualitySeverity as Severity,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  formatTargetPath,
  qualityIssueStatusKey,
  qualityIssueStatusVariant,
  qualitySeverityKey,
  qualitySeverityVariant,
  targetScopeLabel,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";
import { enumFilterValue, enumFromFilter } from "@/utils/queryFilters";

/**
 * The issue directory. An issue is the persistent problem behind a rule's
 * failures; it is opened when a rule starts failing and resolved when it passes
 * again or a person closes it. Rows open the detail page, where the lifecycle
 * actions live.
 */

const { t, locale } = useI18n();
const router = useRouter();
const { handleError } = useErrorHandler();

const status = ref("-");
const severity = ref("-");
const appliedStatus = ref("-");
const appliedSeverity = ref("-");
const pageSize = ref(50);
const error = ref("");
const ruleTitles = ref<Record<string, string>>({});

const hasActiveFilters = computed(
  () => appliedStatus.value !== "-" || appliedSeverity.value !== "-"
);

const statusOptions: Array<{ value: QualityIssueStatus; key: string }> = [
  { value: QualityIssueStatus.OPEN, key: "dataQuality.issueStatus.open" },
  {
    value: QualityIssueStatus.ACKNOWLEDGED,
    key: "dataQuality.issueStatus.acknowledged",
  },
  {
    value: QualityIssueStatus.RESOLVED,
    key: "dataQuality.issueStatus.resolved",
  },
];

const severityOptions: Array<{ value: QualitySeverity; key: string }> = [
  { value: Severity.WARNING, key: "dataQuality.severity.warning" },
  { value: Severity.ERROR, key: "dataQuality.severity.error" },
  { value: Severity.CRITICAL, key: "dataQuality.severity.critical" },
];

/** Every member each filter accepts, UNSPECIFIED included as "any". */
const STATUS_FILTER_VALUES: QualityIssueStatus[] = [
  QualityIssueStatus.UNSPECIFIED,
  ...statusOptions.map((option) => option.value),
];
const SEVERITY_FILTER_VALUES: QualitySeverity[] = [
  Severity.UNSPECIFIED,
  ...severityOptions.map((option) => option.value),
];

/** The query parameter each filter travels under. */
const FILTER_QUERY_KEYS = ["status", "severity"] as const;

/** The filters as one value, so a URL and the applied set share a shape. */
interface IssueFilters {
  status: string;
  severity: string;
}

/** The filters a URL carries: reload, shared link and back/forward. */
function parseFilters(query: LocationQuery): IssueFilters {
  return {
    status: enumFilterValue(query.status, STATUS_FILTER_VALUES),
    severity: enumFilterValue(query.severity, SEVERITY_FILTER_VALUES),
  };
}

/** Only non-default values are written, so the URL stays readable. */
function serializeFilters(filters: IssueFilters): Record<string, string> {
  const owned: Record<string, string> = {};
  if (filters.status !== "-") {
    owned.status = filters.status;
  }
  if (filters.severity !== "-") {
    owned.severity = filters.severity;
  }
  return owned;
}

/** The filters the request currently applies. */
function appliedFilters(): IssueFilters {
  return {
    status: appliedStatus.value,
    severity: appliedSeverity.value,
  };
}

const urlFilters = useUrlFilters<IssueFilters>({
  keys: FILTER_QUERY_KEYS,
  parse: parseFilters,
  serialize: serializeFilters,
});

// Read the URL before the first request: a reloaded or shared link queries what
// it carries instead of flashing the unfiltered directory first.
const initialFilters = urlFilters.read();
status.value = initialFilters.status;
severity.value = initialFilters.severity;
appliedStatus.value = initialFilters.status;
appliedSeverity.value = initialFilters.severity;

const {
  items: issues,
  isLoading,
  hasNext,
  hasPrevious,
  reset: resetPage,
  refresh: refreshPage,
  goNext: goNextPage,
  goPrevious: goPreviousPage,
} = usePagedFetch<QualityIssue>({
  fetchPage: async (pageToken, signal) => {
    error.value = "";
    return await listQualityIssues({
      pageSize: pageSize.value,
      pageToken,
      status: enumFromFilter(appliedStatus.value, STATUS_FILTER_VALUES),
      severity: enumFromFilter(appliedSeverity.value, SEVERITY_FILTER_VALUES),
      signal,
    });
  },
  onError: (e) => {
    error.value = handleError(e, "dataQuality.fetchIssuesError");
  },
});

watch(pageSize, () => void resetPage());

// A reload, a shared link or back/forward changes the query from outside; the
// page adopts those filters. The replace this composable performs matches the
// applied set, so it is not adopted and cannot cause a second request.
urlFilters.watchExternal((next) => {
  if (urlFilters.isCurrent(appliedFilters())) {
    return;
  }
  status.value = next.status;
  severity.value = next.severity;
  appliedStatus.value = next.status;
  appliedSeverity.value = next.severity;
  void resetPage();
});

function applyFilters() {
  appliedStatus.value = status.value;
  appliedSeverity.value = severity.value;
  // The URL is written first: the watcher then sees the applied set already
  // agrees with it, so adopting the route costs no second request.
  urlFilters.write(appliedFilters());
  void resetPage();
}

function clearFilters() {
  status.value = "-";
  severity.value = "-";
  applyFilters();
}

/**
 * The export carries the applied filters, so the file holds every matching
 * issue rather than the visible page.
 */
function exportCsv(signal: AbortSignal) {
  return exportQualityIssues({
    status: enumFromFilter(appliedStatus.value, STATUS_FILTER_VALUES),
    severity: enumFromFilter(appliedSeverity.value, SEVERITY_FILTER_VALUES),
    signal,
  });
}

function ruleTitle(issue: QualityIssue): string {
  return ruleTitles.value[issue.rule] || qualityRuleID(issue.rule);
}

function targetLabel(issue: QualityIssue): string {
  const path = issue.target?.path;
  const scope = targetScopeLabel(path);
  const name = formatTargetPath(path);
  if (name === "-") {
    return issue.target?.metaGuid || "-";
  }
  return scope === "-" ? name : `${scope} · ${name}`;
}

/** The issue's rule titles, best effort: ids still identify the rule. */
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

/** Whether the issue carries the target its rule watched. */
function hasTarget(issue: QualityIssue): boolean {
  return Boolean(issue.target?.metaGuid);
}

/**
 * The target workspace for one issue, opening the check drawer when a check
 * name is given. Undefined for the historical rows that carry no snapshot.
 */
function targetWorkspaceRoute(
  issue: QualityIssue,
  check = ""
): RouteLocationRaw | undefined {
  const target = issue.target;
  if (!target?.metaGuid) {
    return undefined;
  }
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

/** The failing check's destination: its target's workspace, check selected. */
function issueRuleRoute(issue: QualityIssue): RouteLocationRaw {
  return targetWorkspaceRoute(issue, issue.rule) ?? issueDetailRoute(issue);
}

/** The target itself, without selecting a check. */
function issueTargetRoute(issue: QualityIssue): RouteLocationRaw {
  return targetWorkspaceRoute(issue) ?? issueDetailRoute(issue);
}

function issueDetailRoute(issue: QualityIssue) {
  return {
    name: "DataQualityIssueDetail",
    params: { issueId: issue.name.split("/") },
  };
}

function openIssue(issue: QualityIssue) {
  if (window.getSelection()?.toString()) {
    return;
  }
  void router.push(issueDetailRoute(issue));
}

onMounted(() => {
  void loadRuleTitles();
  void resetPage();
});
</script>
