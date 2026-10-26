<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.issuesTitle')"
      :description="t('dataQuality.issuesDescription')"
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
                  <button
                    type="button"
                    class="block max-w-[14rem] truncate text-left font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 md:max-w-md"
                    :title="ruleTitle(issue)"
                    @click.stop="openIssue(issue)"
                  >
                    {{ ruleTitle(issue) }}
                  </button>
                  <div
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
  type LocationQueryRaw,
  useRoute,
  useRouter,
} from "vue-router";
import {
  listAllQualityRules,
  listQualityIssues,
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
import {
  type QualityIssue,
  QualityIssueStatus,
  type QualitySeverity,
  QualitySeverity as Severity,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  formatTargetPath,
  qualityIssueStatusKey,
  qualityIssueStatusVariant,
  qualitySeverityKey,
  qualitySeverityVariant,
  targetScopeLabel,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * The issue directory. An issue is the persistent problem behind a rule's
 * failures; it is opened when a rule starts failing and resolved when it passes
 * again or a person closes it. Rows open the detail page, where the lifecycle
 * actions live.
 */

const { t, locale } = useI18n();
const route = useRoute();
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

function queryString(value: unknown): string {
  if (Array.isArray(value)) {
    return String(value[0] ?? "");
  }
  return value === undefined || value === null ? "" : String(value);
}

/** A numeric enum filter from the URL; anything the enum lacks reads as "any". */
function enumFilter(value: unknown, members: readonly number[]): string {
  const raw = queryString(value).trim();
  if (!raw || raw === "-") {
    return "-";
  }
  const parsed = Number(raw);
  return Number.isInteger(parsed) && parsed > 0 && members.includes(parsed)
    ? String(parsed)
    : "-";
}

/** The filters a URL carries: reload, shared link and back/forward. */
function filtersFromQuery(query: LocationQuery): IssueFilters {
  return {
    status: enumFilter(query.status, STATUS_FILTER_VALUES),
    severity: enumFilter(query.severity, SEVERITY_FILTER_VALUES),
  };
}

/** Only non-default values are written, so the URL stays readable. */
function queryFromFilters(filters: IssueFilters): Record<string, string> {
  const query: Record<string, string> = {};
  if (filters.status !== "-") {
    query.status = filters.status;
  }
  if (filters.severity !== "-") {
    query.severity = filters.severity;
  }
  return query;
}

function sameFilters(a: IssueFilters, b: IssueFilters): boolean {
  return a.status === b.status && a.severity === b.severity;
}

/** The filters the request currently applies. */
function appliedFilters(): IssueFilters {
  return {
    status: appliedStatus.value,
    severity: appliedSeverity.value,
  };
}

// Read the URL before the first request: a reloaded or shared link queries what
// it carries instead of flashing the unfiltered directory first.
const initialFilters = filtersFromQuery(route.query);
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

/**
 * Write exactly the applied filters into the URL, preserving any parameter this
 * page does not own. `replace` keeps history clean: applying a filter is not a
 * navigation the back button should walk.
 */
function writeFiltersToUrl(filters: IssueFilters) {
  const next: LocationQueryRaw = { ...route.query };
  for (const key of FILTER_QUERY_KEYS) {
    delete next[key];
  }
  Object.assign(next, queryFromFilters(filters));
  if (sameQuery(next, route.query)) {
    return;
  }
  void router.replace({ query: next });
}

/** Whether a query about to be written already reads like the current one. */
function sameQuery(next: LocationQueryRaw, current: LocationQuery): boolean {
  const keys = Object.keys(next);
  if (keys.length !== Object.keys(current).length) {
    return false;
  }
  return keys.every(
    (key) => queryString(next[key]) === queryString(current[key])
  );
}

// A reload, a shared link or back/forward changes the query from outside; the
// page adopts those filters. The replace this page itself performs writes
// exactly the applied filters, so the watcher recognizes it and stops — no loop,
// no second request.
watch(
  () => route.query,
  (query) => {
    const next = filtersFromQuery(query);
    if (sameFilters(next, appliedFilters())) {
      return;
    }
    status.value = next.status;
    severity.value = next.severity;
    appliedStatus.value = next.status;
    appliedSeverity.value = next.severity;
    void resetPage();
  }
);

function enumFromFilter<T extends number>(value: string, members: T[]): T {
  const parsed = Number(value);
  return members.includes(parsed as T) ? (parsed as T) : members[0];
}

function applyFilters() {
  appliedStatus.value = status.value;
  appliedSeverity.value = severity.value;
  // The URL is written first: the watcher then sees the applied set already
  // agrees with it, so adopting the route costs no second request.
  writeFiltersToUrl(appliedFilters());
  void resetPage();
}

function clearFilters() {
  status.value = "-";
  severity.value = "-";
  applyFilters();
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

function openIssue(issue: QualityIssue) {
  if (window.getSelection()?.toString()) {
    return;
  }
  void router.push({
    name: "DataQualityIssueDetail",
    params: { issueId: issue.name.split("/") },
  });
}

onMounted(() => {
  void loadRuleTitles();
  void resetPage();
});
</script>
