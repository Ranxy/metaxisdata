<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.overview.title')"
      :description="t('dataQuality.overview.description')"
    >
      <template #actions>
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
          v-if="canCreate"
          as-child
        >
          <RouterLink :to="{ name: 'DataQualityRuleNew' }">
            <Plus class="mr-2 h-4 w-4" />
            {{ t("dataQuality.newRule") }}
          </RouterLink>
        </Button>
      </template>
    </PageHeader>

    <PageState
      :loading="isLoading"
      :error="error"
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

      <EmptyState
        v-if="isEmptyWorkspace"
        :icon="ShieldCheck"
        :title="t('dataQuality.overview.emptyTitle')"
        :description="t('dataQuality.overview.emptyDescription')"
      >
        <Button
          v-if="canCreate"
          as-child
        >
          <RouterLink :to="{ name: 'DataQualityRuleNew' }">
            <Plus class="mr-2 h-4 w-4" />
            {{ t("dataQuality.newRule") }}
          </RouterLink>
        </Button>
      </EmptyState>

      <div
        v-else
        class="space-y-4"
      >
        <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
          <StatCard
            :label="t('dataQuality.overview.statEnabledRules')"
            :value="overview.enabledRuleCount"
            :hint="
              t('dataQuality.overview.statEnabledRulesHint', {
                total: overview.ruleCount,
                paused: overview.pausedRuleCount,
              })
            "
            :icon="ListChecks"
            :to="{ name: 'DataQualityRules' }"
          />
          <StatCard
            :label="t('dataQuality.overview.statMonitoredTables')"
            :value="overview.monitoredTableCount"
            :hint="t('dataQuality.overview.statMonitoredTablesHint')"
            :icon="Table2"
            :to="{ name: 'DataQualityTargets' }"
          />
          <StatCard
            :label="t('dataQuality.overview.statOpenIssues')"
            :value="issueCount"
            :hint="issueHint"
            :icon="CircleAlert"
            :tone="issueTone"
            :to="{ name: 'DataQualityIssues' }"
          />
          <StatCard
            :label="t('dataQuality.overview.statFailingRules')"
            :value="overview.failingRuleCount"
            :hint="failingHint"
            :icon="TriangleAlert"
            :tone="failingTone"
            :to="{ name: 'DataQualityExecutions' }"
          />
          <StatCard
            :label="t('dataQuality.overview.statExecutions')"
            :value="overview.executionCount"
            :hint="executionsHint"
            :icon="Activity"
            :to="{ name: 'DataQualityExecutions' }"
          />
        </div>

        <Card>
          <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-3 border-b">
            <div class="min-w-0 space-y-1">
              <CardTitle class="text-base">
                {{ t("dataQuality.overview.trendTitle") }}
              </CardTitle>
              <CardDescription>
                {{ t("dataQuality.overview.trendDescription") }}
              </CardDescription>
            </div>
            <div
              class="flex shrink-0 items-center gap-0.5 rounded-lg bg-muted p-0.5"
              role="group"
              :aria-label="t('dataQuality.overview.trendTitle')"
            >
              <button
                v-for="option in TREND_WINDOWS"
                :key="option"
                type="button"
                class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:outline-none"
                :class="
                  windowDays === option
                    ? 'bg-card text-foreground shadow-e1'
                    : 'text-muted-foreground hover:text-foreground'
                "
                :aria-pressed="windowDays === option"
                @click="selectWindow(option)"
              >
                {{ t("dataQuality.overview.windowDays", { days: option }) }}
              </button>
            </div>
          </CardHeader>
          <CardContent class="pt-4">
            <div v-if="trendLoading">
              <div class="h-[264px] animate-pulse rounded-md bg-muted/40" />
            </div>
            <EmptyState
              v-else-if="!hasTrendData(trend)"
              compact
              :icon="ChartNoAxesColumn"
              :title="t('dataQuality.overview.trendEmptyTitle')"
              :description="t('dataQuality.overview.trendEmptyDescription')"
            />
            <template v-else>
              <QualityTrendChart :days="trend" />
              <div
                class="mt-4 flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t pt-3 text-xs text-muted-foreground"
              >
                <span>
                  {{ t("dataQuality.overview.failureRate", { percent: trendFailurePercent(trend) }) }}
                </span>
                <span class="tabular-nums">
                  {{ t("dataQuality.overview.trendWindowTotal", { count: windowTotal }) }}
                </span>
              </div>
            </template>
          </CardContent>
        </Card>

        <div class="grid gap-4 xl:grid-cols-2">
          <Card>
            <CardHeader class="flex flex-row items-center justify-between gap-3 border-b">
              <div class="min-w-0 space-y-1">
                <CardTitle class="text-base">
                  {{ t("dataQuality.overview.recentFailuresTitle") }}
                </CardTitle>
                <CardDescription>
                  {{ t("dataQuality.overview.recentFailuresDescription") }}
                </CardDescription>
              </div>
              <Button
                v-if="canListExecutions"
                variant="ghost"
                size="sm"
                as-child
              >
                <RouterLink :to="{ name: 'DataQualityExecutions' }">
                  {{ t("dataQuality.viewAllExecutions") }}
                </RouterLink>
              </Button>
            </CardHeader>
            <CardContent class="pt-4">
              <EmptyState
                v-if="recentFailures.length === 0"
                compact
                :icon="CircleCheck"
                :title="t('dataQuality.overview.noRecentFailures')"
              />
              <ul
                v-else
                class="divide-y"
              >
                <li
                  v-for="failure in recentFailures"
                  :key="failure.name"
                  class="flex items-center justify-between gap-3 py-2.5"
                >
                  <div class="min-w-0">
                    <RouterLink
                      v-if="canViewRule"
                      :to="ruleRoute(failure.rule)"
                      class="block truncate text-sm font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    >
                      {{ ruleTitle(failure.rule) }}
                    </RouterLink>
                    <div
                      v-else
                      class="truncate text-sm font-medium"
                    >
                      {{ ruleTitle(failure.rule) }}
                    </div>
                    <div class="flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                      <Badge
                        :variant="qualityStatusVariant(failure.outcome)"
                        class="mr-1"
                      >
                        {{ t(qualityStatusKey(failure.outcome)) }}
                      </Badge>
                      <span class="tabular-nums">
                        {{ t("dataQuality.failedRows") }}
                        {{ formatRowCount(failure.failedRows, locale) }}
                      </span>
                      <span aria-hidden="true">·</span>
                      <span :title="formatDateTime(failure.finishTime ?? failure.startTime, locale)">
                        {{ formatRelativeTime(failure.finishTime ?? failure.startTime, locale) }}
                      </span>
                    </div>
                  </div>
                  <RouterLink
                    v-if="canViewExecution"
                    :to="executionRoute(failure.name)"
                    class="shrink-0 text-xs text-primary hover:underline"
                  >
                    {{ t("dataQuality.viewExecution") }}
                  </RouterLink>
                </li>
              </ul>
            </CardContent>
          </Card>

          <Card>
            <CardHeader class="flex flex-row items-center justify-between gap-3 border-b">
              <div class="min-w-0 space-y-1">
                <CardTitle class="text-base">
                  {{ t("dataQuality.overview.openIssuesTitle") }}
                </CardTitle>
                <CardDescription>
                  {{ t("dataQuality.overview.openIssuesDescription") }}
                </CardDescription>
              </div>
              <Button
                variant="ghost"
                size="sm"
                as-child
              >
                <RouterLink :to="{ name: 'DataQualityIssues' }">
                  {{ t("dataQuality.viewIssue") }}
                </RouterLink>
              </Button>
            </CardHeader>
            <CardContent class="pt-4">
              <EmptyState
                v-if="openIssues.length === 0"
                compact
                :icon="CircleCheck"
                :title="t('dataQuality.overview.noOpenIssues')"
              />
              <ul
                v-else
                class="divide-y"
              >
                <li
                  v-for="issue in openIssues"
                  :key="issue.name"
                  class="flex items-center justify-between gap-3 py-2.5"
                >
                  <div class="min-w-0">
                    <RouterLink
                      v-if="canViewRule"
                      :to="ruleRoute(issue.rule)"
                      class="block truncate text-sm font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    >
                      {{ ruleTitle(issue.rule) }}
                    </RouterLink>
                    <div
                      v-else
                      class="truncate text-sm font-medium"
                    >
                      {{ ruleTitle(issue.rule) }}
                    </div>
                    <div class="flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                      <Badge
                        :variant="qualityIssueStatusVariant(issue.status)"
                        class="mr-1"
                      >
                        {{ t(qualityIssueStatusKey(issue.status)) }}
                      </Badge>
                      <Badge :variant="qualitySeverityVariant(issue.severity)">
                        {{ t(qualitySeverityKey(issue.severity)) }}
                      </Badge>
                      <span :title="formatDateTime(issue.lastSeenTime, locale)">
                        {{ formatRelativeTime(issue.lastSeenTime, locale) }}
                      </span>
                    </div>
                  </div>
                  <RouterLink
                    :to="issueRoute(issue.name)"
                    class="shrink-0 text-xs text-primary hover:underline"
                  >
                    {{ t("dataQuality.viewIssue") }}
                  </RouterLink>
                </li>
              </ul>
            </CardContent>
          </Card>
        </div>
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  Activity,
  ChartNoAxesColumn,
  CircleAlert,
  CircleCheck,
  ListChecks,
  Plus,
  RefreshCcw,
  ShieldCheck,
  Table2,
  TriangleAlert,
} from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import {
  getQualityOverview,
  listAllQualityRules,
  listQualityExecutions,
  listQualityIssues,
  qualityRuleID,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import StatCard, { type StatCardTone } from "@/components/common/StatCard.vue";
import QualityTrendChart from "@/components/data-quality/QualityTrendChart.vue";
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
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityExecution,
  type QualityIssue,
  QualityIssueStatus,
  type QualityOverview,
  QualityOverviewSchema,
  QualitySeverity,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  formatRowCount,
  qualityIssueStatusKey,
  qualityIssueStatusVariant,
  qualitySeverityKey,
  qualitySeverityVariant,
  qualityStatusKey,
  qualityStatusVariant,
} from "@/utils/dataQuality";
import {
  failurePercent,
  hasTrendData,
  issueSeverityParts,
  normalizeTrend,
  openIssueCount,
  TREND_WINDOWS,
  type TrendWindow,
  trendFailurePercent,
} from "@/utils/dataQualityOverview";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * The quality workspace at a glance: the counts a steward acts on, a stacked
 * bar per day of the last week or month, and the two lists that need attention
 * (recent failures and unresolved issues).
 */

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const canCreate = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.create")
);
const canViewRule = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.get")
);
const canViewExecution = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.executions.get")
);
const canListExecutions = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.executions.list")
);

const EMPTY_OVERVIEW: QualityOverview = create(QualityOverviewSchema, {});

const overview = ref<QualityOverview>(EMPTY_OVERVIEW);
const windowDays = ref<TrendWindow>(7);
const recentFailures = ref<QualityExecution[]>([]);
const openIssues = ref<QualityIssue[]>([]);
const ruleTitles = ref<Record<string, string>>({});
const isLoading = ref(true);
const trendLoading = ref(false);
const error = ref("");

const isEmptyWorkspace = computed(
  () =>
    overview.value.ruleCount === 0 && overview.value.monitoredTableCount === 0
);

const trend = computed(() =>
  normalizeTrend(overview.value.trend, windowDays.value)
);

const windowTotal = computed(() =>
  trend.value.reduce((sum, day) => sum + day.total, 0)
);

const issueCount = computed(() => openIssueCount(overview.value));

// The tile turns semantic only while it has something to say: a red icon next
// to a zero reads as an alarm about nothing.
const issueTone = computed<StatCardTone>(() =>
  issueCount.value > 0 ? "warning" : "primary"
);

const failingTone = computed<StatCardTone>(() =>
  overview.value.failingRuleCount > 0 ? "danger" : "primary"
);

const issueHint = computed(() => {
  const parts = issueSeverityParts(overview.value);
  return parts.length === 0
    ? t("dataQuality.overview.noOpenIssues")
    : parts
        .map(
          (part) =>
            `${part.count} ${t(qualitySeverityKey(severityOf(part.severity)))}`
        )
        .join(" · ");
});

const failingHint = computed(() =>
  t("dataQuality.overview.failedResultsShare", {
    percent: failurePercent(overview.value.lastDayCounts),
  })
);

const executionsHint = computed(() => {
  const counts = overview.value.lastDayCounts;
  return t("dataQuality.overview.executionsHint", {
    pass: counts?.pass ?? 0,
    warn: counts?.warn ?? 0,
    fail: counts?.fail ?? 0,
  });
});

function severityOf(
  severity: "warning" | "error" | "critical"
): QualitySeverity {
  switch (severity) {
    case "warning":
      return QualitySeverity.WARNING;
    case "error":
      return QualitySeverity.ERROR;
    default:
      return QualitySeverity.CRITICAL;
  }
}

function ruleTitle(ruleName: string): string {
  return ruleTitles.value[ruleName] || qualityRuleID(ruleName);
}

function ruleRoute(ruleName: string) {
  return {
    name: "DataQualityRuleDetail",
    params: { ruleId: ruleName.split("/") },
  };
}

function executionRoute(name: string) {
  return {
    name: "DataQualityExecutionDetail",
    params: { executionId: name.split("/") },
  };
}

function issueRoute(name: string) {
  return {
    name: "DataQualityIssueDetail",
    params: { issueId: name.split("/") },
  };
}

async function load() {
  isLoading.value = true;
  error.value = "";
  try {
    overview.value = await getQualityOverview({
      trendDays: windowDays.value,
    });
    await Promise.all([
      loadRuleTitles(),
      loadRecentFailures(),
      loadOpenIssues(),
    ]);
  } catch (e: unknown) {
    error.value = formatError(e, "dataQuality.overview.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function selectWindow(days: TrendWindow) {
  if (windowDays.value === days || trendLoading.value) {
    return;
  }
  windowDays.value = days;
  trendLoading.value = true;
  try {
    overview.value = await getQualityOverview({ trendDays: days });
  } catch (e: unknown) {
    error.value = formatError(e, "dataQuality.overview.fetchError");
  } finally {
    trendLoading.value = false;
  }
}

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

/** The last day's failures, one row per rule (the newest execution wins). */
async function loadRecentFailures() {
  try {
    const page = await listQualityExecutions({
      outcome: QualityStatus.FAIL,
      startTime: timestampFromDate(new Date(Date.now() - 86_400_000)),
      pageSize: 50,
    });
    const seen = new Set<string>();
    recentFailures.value = page.items
      .filter((execution) => {
        if (seen.has(execution.rule)) {
          return false;
        }
        seen.add(execution.rule);
        return true;
      })
      .slice(0, 5);
  } catch {
    recentFailures.value = [];
  }
}

/** Unresolved issues, newest first; the page shows the first five. */
async function loadOpenIssues() {
  try {
    const page = await listQualityIssues({ pageSize: 50 });
    openIssues.value = page.items
      .filter((issue) => issue.status !== QualityIssueStatus.RESOLVED)
      .slice(0, 5);
  } catch {
    openIssues.value = [];
  }
}

onMounted(load);
</script>
