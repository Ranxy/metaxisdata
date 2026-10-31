<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.overview.title')"
      :description="t('dataQuality.overview.description')"
    >
      <template #actions>
        <Button
          as-child
        >
          <RouterLink :to="{ name: 'DataQualityTargets', query: { new: '1' } }">
            <Plus class="mr-2 h-4 w-4" />
            {{ t("dataQuality.targets.startMonitoring") }}
          </RouterLink>
        </Button>
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
        <Button as-child>
          <RouterLink :to="{ name: 'DataQualityTargets', query: { new: '1' } }">
            <Plus class="mr-2 h-4 w-4" />
            {{ t("dataQuality.targets.startMonitoring") }}
          </RouterLink>
        </Button>
      </EmptyState>

      <div
        v-else
        class="space-y-4"
      >
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
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
            :to="{ name: 'DataQualityChecks' }"
          />
          <StatCard
            :label="t('dataQuality.overview.statCoverage')"
            :value="coverageValue"
            :hint="coverageHint"
            :icon="Table2"
            :to="{ name: 'DataQualityTargets' }"
          />
          <StatCard
            :label="t('dataQuality.overview.statQualityScore')"
            :value="qualityScoreValue"
            :hint="qualityScoreHint"
            :icon="Gauge"
            :to="{ name: 'DataQualityChecks' }"
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

        <!-- Coverage and score cut four ways: the workspace totals above say how
             much is watched and how it is doing, this table says where. All four
             lenses arrive in one overview response, so switching is local. -->
        <Card>
          <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-3 border-b">
            <div class="min-w-0 space-y-1">
              <CardTitle class="text-base">
                {{ t("dataQuality.overview.breakdownTitle") }}
              </CardTitle>
              <CardDescription>
                {{ t("dataQuality.overview.breakdownDescription") }}
              </CardDescription>
            </div>
            <div
              class="flex shrink-0 items-center gap-0.5 rounded-lg bg-muted p-0.5"
              role="group"
              :aria-label="t('dataQuality.overview.breakdownTitle')"
            >
              <button
                v-for="option in BREAKDOWN_MODES"
                :key="option"
                type="button"
                class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:outline-none"
                :class="
                  breakdownMode === option
                    ? 'bg-card text-foreground shadow-e1'
                    : 'text-muted-foreground hover:text-foreground'
                "
                :aria-pressed="breakdownMode === option"
                @click="breakdownMode = option"
              >
                {{ breakdownModeLabels[option] }}
              </button>
            </div>
          </CardHeader>
          <CardContent class="pt-4">
            <EmptyState
              v-if="breakdownEmpty"
              compact
              :icon="ChartNoAxesColumn"
              :title="t('dataQuality.overview.breakdownEmptyTitle')"
              :description="t('dataQuality.overview.breakdownEmptyDescription')"
            />
            <Table
              v-else-if="isCoverageMode"
              class="[&_th]:whitespace-nowrap"
            >
              <TableHeader>
                <TableRow>
                  <TableHead>{{ breakdownLensLabel }}</TableHead>
                  <TableHead class="text-right">
                    {{ t("dataQuality.overview.breakdownTables") }}
                  </TableHead>
                  <TableHead class="hidden text-right sm:table-cell">
                    {{ t("dataQuality.overview.breakdownColumns") }}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow
                  v-for="row in coverageRows"
                  :key="row.key || 'unassigned'"
                >
                  <TableCell class="font-medium">
                    {{ coverageLabel(row) }}
                  </TableCell>
                  <TableCell class="text-right tabular-nums">
                    <div>{{ formatPercentValue(row.tableCoveragePercent) }}</div>
                    <div class="text-xs text-muted-foreground">
                      {{ row.monitoredTableCount }} / {{ row.totalTableCount }}
                    </div>
                  </TableCell>
                  <TableCell class="hidden text-right tabular-nums sm:table-cell">
                    <div>{{ formatPercentValue(row.columnCoveragePercent) }}</div>
                    <div class="text-xs text-muted-foreground">
                      {{ row.coveredColumnCount }} / {{ row.totalColumnCount }}
                    </div>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
            <Table
              v-else
              class="[&_th]:whitespace-nowrap"
            >
              <TableHeader>
                <TableRow>
                  <TableHead>{{ breakdownLensLabel }}</TableHead>
                  <TableHead class="text-right">
                    {{ t("dataQuality.overview.breakdownScore") }}
                  </TableHead>
                  <TableHead class="hidden text-right sm:table-cell">
                    {{ t("dataQuality.overview.breakdownChecks") }}
                  </TableHead>
                  <TableHead class="hidden text-right sm:table-cell">
                    {{ t("dataQuality.overview.breakdownJudged") }}
                  </TableHead>
                  <TableHead class="hidden text-right md:table-cell">
                    {{ t("dataQuality.overview.breakdownFailing") }}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow
                  v-for="row in scoreRows"
                  :key="row.key || 'unassigned'"
                >
                  <TableCell class="font-medium">
                    {{ scoreLabel(row) }}
                  </TableCell>
                  <TableCell class="text-right tabular-nums">
                    <template v-if="row.qualityScore !== undefined">
                      {{ formatQualityScore(row.qualityScore) }}
                    </template>
                    <template v-else>
                      <div class="text-muted-foreground">-</div>
                      <div class="text-xs text-muted-foreground">
                        {{ t("dataQuality.overview.breakdownUnscored") }}
                      </div>
                    </template>
                  </TableCell>
                  <TableCell class="hidden text-right tabular-nums sm:table-cell">
                    {{ row.ruleCount }}
                  </TableCell>
                  <TableCell class="hidden text-right tabular-nums sm:table-cell">
                    {{ row.scoredRuleCount }}
                  </TableCell>
                  <TableCell class="hidden text-right tabular-nums md:table-cell">
                    {{ row.failingRuleCount > 0 ? row.failingRuleCount : "-" }}
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </CardContent>
        </Card>

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
            <CardHeader class="border-b">
              <div class="min-w-0 space-y-1">
                <CardTitle class="text-base">
                  {{ t("dataQuality.overview.attentionTitle") }}
                </CardTitle>
                <CardDescription>
                  {{ t("dataQuality.overview.attentionDescription") }}
                </CardDescription>
              </div>
            </CardHeader>
            <CardContent class="pt-4">
              <EmptyState
                v-if="attentionTargets.length === 0"
                compact
                :icon="CircleCheck"
                :title="t('dataQuality.overview.attentionEmpty')"
              />
              <ul
                v-else
                class="divide-y"
              >
                <li
                  v-for="target in attentionTargets"
                  :key="target.target?.metaGuid"
                  class="py-2.5"
                >
                  <RouterLink
                    :to="targetRoute(target)"
                    class="flex items-start justify-between gap-3 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                  >
                    <div class="min-w-0">
                      <span class="block truncate text-sm font-medium hover:underline">
                        {{ targetDisplayName(target) }}
                      </span>
                      <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                        <span class="truncate font-mono">
                          {{ targetFullPath(target) }}
                        </span>
                        <span aria-hidden="true">·</span>
                        <span class="tabular-nums">
                          {{
                            t("dataQuality.targets.rulesCount", {
                              count: target.ruleCount,
                            })
                          }}
                        </span>
                      </div>
                    </div>
                    <div class="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
                      <Badge
                        v-if="target.worstStatus !== QualityStatus.UNSPECIFIED"
                        :variant="qualityStatusVariant(target.worstStatus)"
                      >
                        {{ t(qualityStatusKey(target.worstStatus)) }}
                      </Badge>
                      <Badge
                        v-if="target.openIssueCount > 0"
                        variant="danger"
                      >
                        {{
                          t("dataQuality.workspace.summaryIssues", {
                            count: target.openIssueCount,
                          })
                        }}
                      </Badge>
                    </div>
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
                      :to="issueWorkspaceRoute(issue)"
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
import {
  Activity,
  ChartNoAxesColumn,
  CircleAlert,
  CircleCheck,
  Gauge,
  ListChecks,
  Plus,
  RefreshCcw,
  ShieldCheck,
  Table2,
  TriangleAlert,
} from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import {
  getQualityOverview,
  listAllQualityRules,
  listQualityIssues,
  listQualityTargets,
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useUserDirectory } from "@/composables/useUserDirectory";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityIssue,
  QualityIssueStatus,
  type QualityOverview,
  QualityOverviewSchema,
  QualitySeverity,
  QualityStatus,
  QualityTargetOrder,
  type QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  qualityDimensionKey,
  qualityIssueStatusKey,
  qualityIssueStatusVariant,
  qualitySeverityKey,
  qualitySeverityVariant,
  qualityStatusKey,
  qualityStatusVariant,
} from "@/utils/dataQuality";
import {
  type CoverageBreakdownRow,
  coveragePercent,
  failurePercent,
  formatPercentValue,
  formatQualityScore,
  hasTrendData,
  issueSeverityParts,
  normalizeCoverageBreakdown,
  normalizeDimensionScores,
  normalizeOwnerScores,
  normalizeTrend,
  openIssueCount,
  type ScoreBreakdownRow,
  TREND_WINDOWS,
  type TrendWindow,
  trendFailurePercent,
} from "@/utils/dataQualityOverview";
import { targetDisplayName, targetFullPath } from "@/utils/dataQualityTarget";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";

/**
 * The quality workspace at a glance: the counts a steward acts on, a stacked
 * bar per day of the last week or month, the targets whose latest checks failed
 * or that still have open issues, and the unresolved issues themselves.
 */

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const { emailOf, load: loadUserDirectory } = useUserDirectory();
const authStore = useAuthStore();

const canViewRule = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.get")
);

const EMPTY_OVERVIEW: QualityOverview = create(QualityOverviewSchema, {});

const overview = ref<QualityOverview>(EMPTY_OVERVIEW);
const windowDays = ref<TrendWindow>(7);
const attentionTargets = ref<QualityTargetSummary[]>([]);
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

const coverageValue = computed(() =>
  formatPercentValue(
    coveragePercent(
      overview.value.monitoredTableCount,
      overview.value.totalTableCount
    )
  )
);

const coverageHint = computed(() =>
  t("dataQuality.overview.statCoverageHint", {
    monitoredTables: overview.value.monitoredTableCount,
    totalTables: overview.value.totalTableCount,
    tablePercent: coverageValue.value,
    coveredColumns: overview.value.coveredColumnCount,
    totalColumns: overview.value.totalColumnCount,
    columnPercent: formatPercentValue(
      coveragePercent(
        overview.value.coveredColumnCount,
        overview.value.totalColumnCount
      )
    ),
  })
);

const qualityScoreValue = computed(() =>
  formatQualityScore(overview.value.qualityScore)
);

// "N judged / M not judged" is the score's evidence line: a score with no
// judged checks is "-", not 100.
const qualityScoreHint = computed(() =>
  t("dataQuality.overview.statQualityScoreHint", {
    judged: overview.value.scoredRuleCount,
    notJudged: overview.value.unscoredRuleCount,
  })
);

// ---- the four-way breakdown ----

const BREAKDOWN_MODES = [
  "instance",
  "environment",
  "dimension",
  "owner",
] as const;
type BreakdownMode = (typeof BREAKDOWN_MODES)[number];

const breakdownMode = ref<BreakdownMode>("instance");

// Only the owner lens needs the member directory, so it stays off the page's
// critical path: the request starts when the reader first opens that lens, and
// the labels switch from resource names to emails when it lands.
watch(breakdownMode, (mode) => {
  if (mode === "owner") {
    void loadUserDirectory();
  }
});

const breakdownModeLabels = computed<Record<BreakdownMode, string>>(() => ({
  instance: t("dataQuality.overview.breakdownInstance"),
  environment: t("dataQuality.overview.breakdownEnvironment"),
  dimension: t("dataQuality.overview.breakdownDimension"),
  owner: t("dataQuality.overview.breakdownOwner"),
}));

const breakdownLensLabel = computed(() => {
  switch (breakdownMode.value) {
    case "instance":
      return t("dataQuality.overview.breakdownInstance");
    case "environment":
      return t("dataQuality.overview.breakdownEnvironment");
    case "dimension":
      return t("dataQuality.overview.breakdownDimension");
    default:
      return t("dataQuality.overview.breakdownOwner");
  }
});

const isCoverageMode = computed(
  () =>
    breakdownMode.value === "instance" || breakdownMode.value === "environment"
);

const coverageRows = computed<CoverageBreakdownRow[]>(() => {
  switch (breakdownMode.value) {
    case "instance":
      return normalizeCoverageBreakdown(overview.value.coverageByInstance);
    case "environment":
      return normalizeCoverageBreakdown(overview.value.coverageByEnvironment);
    default:
      return [];
  }
});

const scoreRows = computed<ScoreBreakdownRow[]>(() => {
  switch (breakdownMode.value) {
    case "dimension":
      return normalizeDimensionScores(overview.value.scoreByDimension);
    case "owner":
      return normalizeOwnerScores(overview.value.scoreByOwner);
    default:
      return [];
  }
});

const breakdownEmpty = computed(() =>
  isCoverageMode.value
    ? coverageRows.value.length === 0
    : scoreRows.value.length === 0
);

/** An instance/environment row: its title, the key, or the unassigned bucket. */
function coverageLabel(row: CoverageBreakdownRow): string {
  return row.title || row.key || t("dataQuality.overview.breakdownUnassigned");
}

/** A dimension row is named by the enum catalog; an owner by the directory. */
function scoreLabel(row: ScoreBreakdownRow): string {
  if (row.dimension !== undefined) {
    return t(qualityDimensionKey(row.dimension));
  }
  return row.key
    ? emailOf(row.key)
    : t("dataQuality.overview.breakdownUnassigned");
}

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

/**
 * The retired per-rule URL still resolves through its redirect, which resolves
 * the check's target. It is the fallback for an issue that carries no target.
 */
function ruleRedirectRoute(ruleName: string) {
  return {
    name: "DataQualityRuleRedirect",
    params: { ruleId: ruleName.split("/") },
  };
}

/** An issue's check, opened inside its target's workspace. */
function issueWorkspaceRoute(issue: QualityIssue) {
  const target = issue.target;
  if (!target?.metaGuid) {
    return ruleRedirectRoute(issue.rule);
  }
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.metaGuid) },
    query: {
      ...(target.metaType !== MetaType.UNSPECIFIED
        ? { metaType: String(target.metaType) }
        : {}),
      check: issue.rule,
    },
  };
}

/** A target's workspace, the destination every attention row opens. */
function targetRoute(target: QualityTargetSummary) {
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.target?.metaGuid ?? "") },
    query: {
      metaType: String(target.target?.metaType ?? MetaType.UNSPECIFIED),
    },
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
      loadAttentionTargets(),
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

/**
 * The targets whose latest results failed or that still have open issues. The
 * server orders by attention, so the first page already holds the worst rows;
 * the client filter keeps only the targets that actually need a look, and a
 * failed read degrades the card rather than the page.
 */
async function loadAttentionTargets() {
  try {
    const page = await listQualityTargets({
      pageSize: 5,
      orderBy: QualityTargetOrder.ATTENTION,
    });
    attentionTargets.value = page.items.filter(
      (target) =>
        target.worstStatus === QualityStatus.FAIL ||
        target.worstStatus === QualityStatus.ERROR ||
        target.openIssueCount > 0
    );
  } catch {
    attentionTargets.value = [];
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
