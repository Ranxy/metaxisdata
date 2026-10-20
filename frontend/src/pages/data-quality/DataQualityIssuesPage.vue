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
import { useRouter } from "vue-router";
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
      status: enumFromFilter(appliedStatus.value, [
        QualityIssueStatus.UNSPECIFIED,
        QualityIssueStatus.OPEN,
        QualityIssueStatus.ACKNOWLEDGED,
        QualityIssueStatus.RESOLVED,
      ]),
      severity: enumFromFilter(appliedSeverity.value, [
        Severity.UNSPECIFIED,
        Severity.WARNING,
        Severity.ERROR,
        Severity.CRITICAL,
      ]),
      signal,
    });
  },
  onError: (e) => {
    error.value = handleError(e, "dataQuality.fetchIssuesError");
  },
});

watch(pageSize, () => void resetPage());

function enumFromFilter<T extends number>(value: string, members: T[]): T {
  const parsed = Number(value);
  return members.includes(parsed as T) ? (parsed as T) : members[0];
}

function applyFilters() {
  appliedStatus.value = status.value;
  appliedSeverity.value = severity.value;
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
