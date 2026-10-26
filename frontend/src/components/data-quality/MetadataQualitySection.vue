<template>
  <div
    v-if="canView"
    class="space-y-4"
  >
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="space-y-1">
        <h2 class="text-sm font-medium">{{ t("dataQuality.asset.title") }}</h2>
        <p class="text-sm text-muted-foreground">
          {{ t("dataQuality.asset.description") }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <Button
          variant="outline"
          size="sm"
          as-child
        >
          <RouterLink :to="targetWorkspaceTo">
            <Target class="mr-1 h-4 w-4" />
            {{ t("dataQuality.asset.openWorkspace") }}
          </RouterLink>
        </Button>
        <Button
          v-if="canCreateRule"
          variant="outline"
          size="sm"
          as-child
        >
          <RouterLink :to="createRuleTo">
            <Plus class="mr-1 h-4 w-4" />
            {{ t("dataQuality.asset.createRule") }}
          </RouterLink>
        </Button>
        <div
          v-if="canSuggest"
          class="flex"
        >
          <Button
            variant="outline"
            size="sm"
            class="rounded-r-none"
            :disabled="suggesting"
            :title="t('dataQuality.asset.suggestLabelsHint')"
            @click="suggestLabels(false)"
          >
            <Sparkles class="mr-1 h-4 w-4" />
            {{ suggesting ? t("dataQuality.asset.suggesting") : t("dataQuality.asset.suggestLabels") }}
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger as-child>
              <Button
                variant="outline"
                size="sm"
                class="rounded-l-none border-l-0 px-2"
                :disabled="suggesting"
                :title="t('dataQuality.asset.suggestRegenerateHint')"
                :aria-label="t('dataQuality.asset.suggestRegenerate')"
              >
                <ChevronDown class="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              align="end"
              class="max-w-72"
            >
              <DropdownMenuItem
                :disabled="suggesting"
                @select="suggestLabels(true)"
              >
                <div class="space-y-1">
                  <div class="flex items-center gap-2">
                    <RefreshCw class="h-4 w-4" />
                    {{ t("dataQuality.asset.suggestRegenerate") }}
                  </div>
                  <p class="text-xs whitespace-normal text-muted-foreground">
                    {{ t("dataQuality.asset.suggestRegenerateHint") }}
                  </p>
                </div>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <Button
          v-if="canProfile"
          variant="outline"
          size="sm"
          :disabled="profiling"
          @click="analyzeColumns"
        >
          <Activity class="mr-1 h-4 w-4" />
          {{ profiling ? t("dataQuality.asset.profiling") : t("dataQuality.asset.analyzeColumns") }}
        </Button>
      </div>
    </div>

    <p
      v-if="actionError"
      class="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm break-words text-destructive"
    >
      {{ actionError }}
    </p>

    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <div class="rounded-md border px-4 py-3">
        <div class="text-xs text-muted-foreground">
          {{ t("dataQuality.asset.ruleCount") }}
        </div>
        <div class="mt-1 text-xl font-semibold tabular-nums">
          {{ rules.length }}
        </div>
      </div>
      <div class="rounded-md border px-4 py-3">
        <div class="text-xs text-muted-foreground">
          {{ t("dataQuality.asset.openIssueCount") }}
        </div>
        <div class="mt-1 text-xl font-semibold tabular-nums">
          {{ openIssues.length }}
        </div>
      </div>
      <div class="rounded-md border px-4 py-3">
        <div class="text-xs text-muted-foreground">
          {{ t("dataQuality.asset.labeledCount") }}
        </div>
        <div class="mt-1 text-xl font-semibold tabular-nums">
          {{ labels.length }}
        </div>
      </div>
      <div class="rounded-md border px-4 py-3">
        <div class="text-xs text-muted-foreground">
          {{ t("dataQuality.asset.profiledCount") }}
        </div>
        <div class="mt-1 text-xl font-semibold tabular-nums">
          {{ profiles.length }}
        </div>
      </div>
    </div>

    <!-- The queue read is the one load worth reporting: a pass just queued
         proposals, and an empty card would otherwise read as "nothing happened". -->
    <p
      v-if="suggestionsLoadFailed"
      class="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm break-words text-destructive"
    >
      {{ t("dataQuality.asset.pendingSuggestionsLoadFailed") }}
    </p>

    <!-- The table's review inbox. Both kinds a generation pass queues for this
         target appear here — label proposals and rule drafts — so the output of
         "Suggest labels" is reviewed in place; the card stays absent until
         something is pending. The list is bounded like the other cards; the rest
         is one click away in the suggestion center. -->
    <Card v-if="pendingSuggestions.length > 0">
      <CardHeader class="flex flex-row items-center justify-between gap-3 border-b">
        <div class="min-w-0 space-y-1">
          <div class="flex flex-wrap items-center gap-2">
            <CardTitle class="text-base">
              {{ t("dataQuality.asset.pendingSuggestionsTitle") }}
            </CardTitle>
            <Badge variant="secondary">
              {{ pendingSuggestions.length }}{{ pendingSuggestionsTruncated ? "+" : "" }}
            </Badge>
          </div>
          <CardDescription>
            {{ t("dataQuality.asset.pendingSuggestionsDescription") }}
          </CardDescription>
        </div>
        <Button
          variant="ghost"
          size="sm"
          as-child
        >
          <RouterLink :to="{ name: 'DataQualitySuggestions' }">
            {{ t("dataQuality.suggestions.openCenter") }}
          </RouterLink>
        </Button>
      </CardHeader>
      <CardContent class="pt-4">
        <!-- A failed decision is explained next to the row, not at the top of
             the tab where the reader would not connect the two. -->
        <p
          v-if="decideError"
          class="mb-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm break-words text-destructive"
        >
          {{ decideError }}
        </p>
        <ul class="divide-y">
          <li
            v-for="suggestion in visibleSuggestions"
            :key="suggestion.name"
            class="flex flex-wrap items-start justify-between gap-3 py-2"
          >
            <div class="min-w-0 space-y-1">
              <div class="flex flex-wrap items-center gap-2">
                <Badge variant="outline">
                  {{ t(suggestionKindKey(suggestion.kind)) }}
                </Badge>
                <!-- A label proposal names a column; a rule draft names the rule. -->
                <span
                  v-if="isLabelSuggestion(suggestion)"
                  class="font-mono text-sm font-medium"
                >
                  {{ labelSuggestionOf(suggestion)?.column || suggestion.name.split("/").pop() }}
                </span>
                <span
                  v-else
                  class="text-sm font-medium"
                >
                  {{ draftTitle(suggestion) }}
                </span>
                <Badge
                  v-if="isLabelSuggestion(suggestion)"
                  variant="secondary"
                >
                  {{ t(semanticTypeKey(labelSuggestionOf(suggestion)?.label ?? 0)) }}
                </Badge>
                <template v-else>
                  <Badge
                    v-if="draftRuleTypeKey(suggestion)"
                    variant="secondary"
                  >
                    {{ t(draftRuleTypeKey(suggestion) ?? "") }}
                  </Badge>
                  <Badge
                    v-if="draftSeverity(suggestion)"
                    :variant="qualitySeverityVariant(draftSeverity(suggestion) ?? QualitySeverity.UNSPECIFIED)"
                  >
                    {{ t(qualitySeverityKey(draftSeverity(suggestion) ?? QualitySeverity.UNSPECIFIED)) }}
                  </Badge>
                </template>
                <span
                  class="text-xs text-muted-foreground"
                  :title="t('dataQuality.suggestions.confidence')"
                >
                  {{ formatConfidence(confidenceOf(suggestion)) }}
                </span>
                <span
                  class="text-xs text-muted-foreground"
                  :title="formatDateTime(suggestion.refreshTime ?? suggestion.createTime, locale)"
                >
                  {{ formatRelativeTime(suggestion.refreshTime ?? suggestion.createTime, locale) }}
                </span>
              </div>
              <p
                v-if="suggestionRationale(suggestion)"
                class="line-clamp-2 max-w-3xl text-xs text-muted-foreground"
              >
                {{ suggestionRationale(suggestion) }}
              </p>
            </div>
            <div class="flex shrink-0 items-center gap-2">
              <Button
                v-if="isLabelSuggestion(suggestion)"
                variant="outline"
                size="sm"
                :disabled="decidingSuggestion !== ''"
                @click="decideSuggestion(suggestion, QualitySuggestionStatus.ACCEPTED)"
              >
                {{ t("dataQuality.suggestions.accept") }}
              </Button>
              <Button
                v-else-if="canOpenDraft(suggestion)"
                variant="outline"
                size="sm"
                as-child
              >
                <RouterLink :to="draftEditorTo(suggestion)">
                  {{ t("dataQuality.suggestions.openInEditor") }}
                </RouterLink>
              </Button>
              <Button
                variant="ghost"
                size="sm"
                :disabled="decidingSuggestion !== ''"
                @click="decideSuggestion(suggestion, QualitySuggestionStatus.REJECTED)"
              >
                {{ t("dataQuality.suggestions.reject") }}
              </Button>
            </div>
          </li>
        </ul>
        <p
          v-if="hiddenSuggestionCount"
          class="pt-2 text-xs text-muted-foreground"
        >
          {{ t("dataQuality.asset.pendingSuggestionsMore", { count: hiddenSuggestionCount }) }}
        </p>
      </CardContent>
    </Card>

    <div class="grid gap-4 xl:grid-cols-2">
      <Card>
        <CardHeader class="flex flex-row items-center justify-between gap-3 border-b">
          <div class="min-w-0 space-y-1">
            <CardTitle class="text-base">
              {{ t("dataQuality.asset.rulesTitle") }}
            </CardTitle>
            <CardDescription>
              {{ t("dataQuality.asset.rulesDescription") }}
            </CardDescription>
          </div>
          <Button
            variant="ghost"
            size="sm"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualityRules' }">
              {{ t("dataQuality.asset.viewAll") }}
            </RouterLink>
          </Button>
        </CardHeader>
        <CardContent class="pt-4">
          <EmptyState
            v-if="rules.length === 0"
            compact
            :icon="ShieldCheck"
            :title="t('dataQuality.asset.noRules')"
          />
          <ul
            v-else
            class="divide-y"
          >
            <li
              v-for="rule in rules.slice(0, 5)"
              :key="rule.name"
              class="flex items-center justify-between gap-3 py-2"
            >
              <div class="min-w-0">
                <RouterLink
                  :to="ruleRoute(rule.name)"
                  class="block truncate text-sm font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                >
                  {{ rule.title || rule.name.split("/").pop() }}
                </RouterLink>
                <div class="mt-1 flex flex-wrap items-center gap-1">
                  <Badge variant="secondary">
                    {{ t(qualityRuleTypeKey(rule.ruleType)) }}
                  </Badge>
                  <Badge :variant="qualitySeverityVariant(rule.severity)">
                    {{ t(qualitySeverityKey(rule.severity)) }}
                  </Badge>
                  <Badge :variant="qualityRuleStateVariant(rule.state)">
                    {{ t(qualityRuleStateKey(rule.state)) }}
                  </Badge>
                </div>
              </div>
              <Badge
                v-if="rule.lastResult"
                :variant="qualityStatusVariant(rule.lastResult.status)"
              >
                {{ t(qualityStatusKey(rule.lastResult.status)) }}
              </Badge>
            </li>
          </ul>
        </CardContent>
      </Card>

      <Card>
        <CardHeader class="flex flex-row items-center justify-between gap-3 border-b">
          <div class="min-w-0 space-y-1">
            <CardTitle class="text-base">
              {{ t("dataQuality.asset.executionsTitle") }}
            </CardTitle>
            <CardDescription>
              {{ t("dataQuality.asset.executionsDescription") }}
            </CardDescription>
          </div>
          <Button
            v-if="canListExecutions"
            variant="ghost"
            size="sm"
            as-child
          >
            <RouterLink :to="executionsTo">
              {{ t("dataQuality.asset.viewExecutions") }}
            </RouterLink>
          </Button>
        </CardHeader>
        <CardContent class="pt-4">
          <EmptyState
            v-if="executions.length === 0"
            compact
            :icon="ListChecks"
            :title="t('dataQuality.asset.noExecutions')"
          />
          <ul
            v-else
            class="divide-y"
          >
            <li
              v-for="execution in executions.slice(0, 5)"
              :key="execution.name"
              class="flex items-center justify-between gap-3 py-2"
            >
              <div class="min-w-0">
                <div class="truncate text-sm font-medium">
                  {{ ruleTitle(execution.rule) }}
                </div>
                <div class="mt-1 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                  <Badge :variant="qualityExecutionStateVariant(execution.state)">
                    {{ t(qualityExecutionStateKey(execution.state)) }}
                  </Badge>
                  <Badge
                    v-if="execution.outcome !== QualityStatus.UNSPECIFIED"
                    :variant="qualityStatusVariant(execution.outcome)"
                  >
                    {{ t(qualityStatusKey(execution.outcome)) }}
                  </Badge>
                  <span class="tabular-nums">
                    {{ t("dataQuality.failedRows") }}
                    {{ formatRowCount(execution.failedRows, locale) }}
                  </span>
                  <span :title="formatDateTime(execution.finishTime ?? execution.startTime, locale)">
                    {{ formatRelativeTime(execution.finishTime ?? execution.startTime, locale) }}
                  </span>
                </div>
              </div>
              <RouterLink
                v-if="canViewExecution && execution.name"
                :to="executionRoute(execution.name)"
                class="shrink-0 text-xs text-primary hover:underline"
              >
                {{ t("dataQuality.viewExecution") }}
              </RouterLink>
            </li>
          </ul>
        </CardContent>
      </Card>
    </div>

    <Card>
      <CardHeader class="flex flex-row items-center justify-between gap-3 border-b">
        <div class="min-w-0 space-y-1">
          <CardTitle class="text-base">
            {{ t("dataQuality.asset.issuesTitle") }}
          </CardTitle>
          <CardDescription>
            {{ t("dataQuality.asset.issuesDescription") }}
          </CardDescription>
        </div>
        <Button
          variant="ghost"
          size="sm"
          as-child
        >
          <RouterLink :to="{ name: 'DataQualityIssues' }">
            {{ t("dataQuality.asset.viewAll") }}
          </RouterLink>
        </Button>
      </CardHeader>
      <CardContent class="pt-4">
        <EmptyState
          v-if="openIssues.length === 0"
          compact
          :icon="CircleCheck"
          :title="t('dataQuality.asset.noIssues')"
        />
        <ul
          v-else
          class="divide-y"
        >
          <li
            v-for="issue in openIssues.slice(0, 5)"
            :key="issue.name"
            class="flex items-center justify-between gap-3 py-2"
          >
            <div class="min-w-0">
              <div class="truncate text-sm font-medium">
                {{ ruleTitle(issue.rule) }}
              </div>
              <div class="mt-1 flex flex-wrap items-center gap-1">
                <Badge :variant="qualityIssueStatusVariant(issue.status)">
                  {{ t(qualityIssueStatusKey(issue.status)) }}
                </Badge>
                <Badge :variant="qualitySeverityVariant(issue.severity)">
                  {{ t(qualitySeverityKey(issue.severity)) }}
                </Badge>
                <span
                  class="text-xs text-muted-foreground"
                  :title="formatDateTime(issue.lastSeenTime, locale)"
                >
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

    <Card>
      <CardHeader class="border-b">
        <CardTitle class="text-base">
          {{ t("dataQuality.asset.columnsTitle") }}
        </CardTitle>
        <CardDescription>
          {{ t("dataQuality.asset.columnsDescription") }}
        </CardDescription>
      </CardHeader>
      <CardContent class="pt-4">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("metadataBrowser.columnName") }}</TableHead>
              <TableHead>{{ t("dataQuality.asset.semanticLabel") }}</TableHead>
              <TableHead>{{ t("dataQuality.asset.profileSummary") }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="column in columns"
              :key="column.name"
            >
              <TableCell class="font-medium">
                {{ column.name }}
              </TableCell>
              <TableCell>
                <div
                  v-if="labelOf(column.name)"
                  class="flex flex-wrap items-center gap-1"
                >
                  <Badge variant="secondary">
                    {{ t(semanticTypeKey(labelOf(column.name)?.label ?? 0)) }}
                  </Badge>
                  <span class="text-xs text-muted-foreground">
                    {{ t(labelStatusKey(labelOf(column.name)?.status ?? 0)) }}
                  </span>
                  <span
                    v-if="(labelOf(column.name)?.confidence ?? 0) > 0"
                    class="text-xs text-muted-foreground"
                  >
                    {{ formatConfidence(labelOf(column.name)?.confidence ?? 0) }}
                  </span>
                  <Button
                    v-if="canUpdateLabels"
                    variant="ghost"
                    size="icon"
                    class="h-6 w-6 text-muted-foreground hover:text-destructive"
                    :title="t('dataQuality.asset.clearLabel')"
                    :aria-label="t('dataQuality.asset.clearLabel')"
                    @click="clearLabel(column.name)"
                  >
                    <X class="h-3.5 w-3.5" />
                  </Button>
                </div>
                <span
                  v-else
                  class="text-xs text-muted-foreground"
                >
                  {{ t("dataQuality.asset.noLabel") }}
                </span>
              </TableCell>
              <TableCell>
                <div
                  v-if="profileOf(column.name)"
                  class="flex flex-wrap gap-x-2 gap-y-1 text-xs text-muted-foreground"
                >
                  <span
                    v-for="highlight in columnProfileHighlights(profileOf(column.name))"
                    :key="highlight.key + JSON.stringify(highlight.params)"
                  >
                    {{ t(highlight.key, highlight.params) }}
                  </span>
                </div>
                <span
                  v-else
                  class="text-xs text-muted-foreground"
                >
                  {{ t("dataQuality.asset.noProfile") }}
                </span>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import {
  Activity,
  ChevronDown,
  CircleCheck,
  ListChecks,
  Plus,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  Target,
  X,
} from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import {
  listColumnLabels,
  listColumnProfiles,
  listQualityExecutions,
  listQualityIssues,
  listQualityRules,
  listQualitySuggestions,
  profileColumns,
  suggestColumnLabels,
  updateColumnLabel,
  updateQualitySuggestion,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useSuggestionSummary } from "@/composables/useSuggestionSummary";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  ColumnLabelAction,
  type ColumnProfile,
  type ColumnSemanticLabel,
  type QualityExecution,
  type QualityIssue,
  QualityIssueStatus,
  type QualityRule,
  QualitySeverity,
  QualityStatus,
  type QualitySuggestion,
  QualitySuggestionStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import type { ColumnMetadata } from "@/types/proto-es/v1/database_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  formatRowCount,
  qualityExecutionStateKey,
  qualityExecutionStateVariant,
  qualityIssueStatusKey,
  qualityIssueStatusVariant,
  qualityRuleStateKey,
  qualityRuleStateVariant,
  qualityRuleTypeKey,
  qualitySeverityKey,
  qualitySeverityVariant,
  qualityStatusKey,
  qualityStatusVariant,
} from "@/utils/dataQuality";
import {
  columnProfileHighlights,
  formatConfidence,
  isLabelSuggestion,
  labelStatusKey,
  labelSuggestionOf,
  ruleDraftOf,
  semanticTypeKey,
  suggestionKindKey,
  suggestionRationale,
  suggestionToEditorPrefill,
} from "@/utils/dataQualitySuggestion";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";

/**
 * The table detail's Data Quality tab: this target's rules, latest executions
 * and open issues, plus the deterministic column profiles and accepted semantic
 * labels. Every load is independent — a metadata page must survive a quality
 * fetch failure — and the actions are permission-gated.
 */

const props = defineProps<{
  tableGuid: string;
  metaType: MetaType;
  columns: ColumnMetadata[];
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const { notifySummary } = useSuggestionSummary();
const authStore = useAuthStore();

const canView = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.list")
);
const canCreateRule = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.create")
);
const canProfile = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.profile.run")
);
const canSuggest = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.suggest")
);
const canUpdateLabels = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.labels.update")
);
const canViewExecution = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.executions.get")
);
const canListExecutions = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.executions.list")
);

const rules = ref<QualityRule[]>([]);
const executions = ref<QualityExecution[]>([]);
const openIssues = ref<QualityIssue[]>([]);
const labels = ref<ColumnSemanticLabel[]>([]);
const profiles = ref<ColumnProfile[]>([]);
const pendingSuggestions = ref<QualitySuggestion[]>([]);
const pendingSuggestionsTruncated = ref(false);
const suggestionsLoadFailed = ref(false);
const profiling = ref(false);
const suggesting = ref(false);
const decidingSuggestion = ref("");
const decideError = ref("");
const actionError = ref("");

const createRuleTo = computed(() => ({
  name: "DataQualityRuleNew",
  query: {
    table_guid: props.tableGuid,
    meta_type: String(props.metaType),
  },
}));

// The target workspace is the same table read as one list of rules; it is the
// page that can add another rule without walking the target picker again.
const targetWorkspaceTo = computed(() => ({
  name: "DataQualityTarget",
  params: { targetGuid: guidToRouteParams(props.tableGuid) },
  query: { metaType: String(props.metaType) },
}));

const executionsTo = computed(() => ({
  name: "DataQualityExecutions",
  query: { target: props.tableGuid },
}));

const ruleTitles = computed(() =>
  Object.fromEntries(rules.value.map((rule) => [rule.name, rule.title]))
);

function ruleTitle(ruleName: string): string {
  return ruleTitles.value[ruleName] || ruleName.split("/").pop() || ruleName;
}

function labelOf(column: string): ColumnSemanticLabel | undefined {
  return labels.value.find((label) => label.column === column);
}

function profileOf(column: string): ColumnProfile | undefined {
  return profiles.value.find((profile) => profile.column === column);
}

/** How many pending proposals the tab previews before deferring to the center. */
const ASSET_SUGGESTION_PREVIEW = 5;

/** How many pending proposals one queue read loads; more shows the "N+" count. */
const SUGGESTION_PAGE_SIZE = 100;

/** The preview: label proposals first, then rule drafts, newest first within a kind. */
const visibleSuggestions = computed(() => {
  const labelProposals = pendingSuggestions.value.filter(isLabelSuggestion);
  const drafts = pendingSuggestions.value.filter(
    (suggestion) => !isLabelSuggestion(suggestion)
  );
  return [...labelProposals, ...drafts].slice(0, ASSET_SUGGESTION_PREVIEW);
});

/**
 * The proposals the card leaves to the center, as a lower bound: a full page
 * means there are more than that. Undefined leaves the footer out entirely.
 */
const hiddenSuggestionCount = computed<string | undefined>(() => {
  const hidden =
    pendingSuggestions.value.length - visibleSuggestions.value.length;
  if (hidden <= 0 && !pendingSuggestionsTruncated.value) {
    return undefined;
  }
  return pendingSuggestionsTruncated.value
    ? `${Math.max(hidden, 0)}+`
    : String(hidden);
});

function draftTitle(suggestion: QualitySuggestion): string {
  return (
    ruleDraftOf(suggestion)?.title ||
    suggestion.name.split("/").pop() ||
    suggestion.name
  );
}

function draftRuleTypeKey(suggestion: QualitySuggestion): string | undefined {
  const prefill = suggestionToEditorPrefill(suggestion);
  return prefill ? qualityRuleTypeKey(prefill.ruleType) : undefined;
}

function draftSeverity(
  suggestion: QualitySuggestion
): QualitySeverity | undefined {
  return ruleDraftOf(suggestion)?.severity;
}

/** The confidence of either kind; 0 renders as "-" rather than a fake 0%. */
function confidenceOf(suggestion: QualitySuggestion): number {
  return (
    ruleDraftOf(suggestion)?.confidence ??
    labelSuggestionOf(suggestion)?.confidence ??
    0
  );
}

/** Only a build that can prefill the editor offers a draft's creation path. */
function canOpenDraft(suggestion: QualitySuggestion): boolean {
  return (
    canCreateRule.value && suggestionToEditorPrefill(suggestion) !== undefined
  );
}

function draftEditorTo(suggestion: QualitySuggestion) {
  return { name: "DataQualityRuleNew", query: { suggestion: suggestion.name } };
}

function ruleRoute(name: string) {
  return {
    name: "DataQualityRuleDetail",
    params: { ruleId: name.split("/") },
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

async function loadRules() {
  try {
    const page = await listQualityRules({
      targetGuid: props.tableGuid,
      pageSize: 100,
    });
    rules.value = page.items;
  } catch {
    rules.value = [];
  }
}

async function loadExecutions() {
  try {
    const page = await listQualityExecutions({
      targetGuid: props.tableGuid,
      pageSize: 20,
    });
    executions.value = page.items;
  } catch {
    executions.value = [];
  }
}

async function loadIssues() {
  try {
    const page = await listQualityIssues({
      targetGuid: props.tableGuid,
      pageSize: 50,
    });
    openIssues.value = page.items.filter(
      (issue) => issue.status !== QualityIssueStatus.RESOLVED
    );
  } catch {
    openIssues.value = [];
  }
}

async function loadLabels() {
  try {
    labels.value = await listColumnLabels({
      tableGuid: props.tableGuid,
      metaType: props.metaType,
    });
  } catch {
    labels.value = [];
  }
}

async function loadProfiles() {
  try {
    profiles.value = await listColumnProfiles({
      tableGuid: props.tableGuid,
      metaType: props.metaType,
    });
  } catch {
    profiles.value = [];
  }
}

/**
 * The proposals waiting for review on this table, both kinds: a label pass and
 * the AI workbench queue rule drafts here too. The queue is behind the suggest
 * permission — the same one the review actions need — so a read-only caller
 * never requests it. A failed read keeps the last list and flags the failure
 * rather than silently pretending the queue is empty.
 */
async function loadSuggestions() {
  if (!canSuggest.value) {
    return;
  }
  try {
    const page = await listQualitySuggestions({
      targetGuid: props.tableGuid,
      status: QualitySuggestionStatus.PENDING,
      pageSize: SUGGESTION_PAGE_SIZE,
    });
    pendingSuggestions.value = page.items;
    pendingSuggestionsTruncated.value = page.nextPageToken !== "";
    suggestionsLoadFailed.value = false;
  } catch {
    suggestionsLoadFailed.value = true;
  }
}

async function loadAll() {
  if (!canView.value) {
    return;
  }
  await Promise.all([
    loadRules(),
    loadExecutions(),
    loadIssues(),
    loadLabels(),
    loadProfiles(),
    loadSuggestions(),
  ]);
}

/** The first columns profiled/suggested in one action, keeping the request small. */
function sampleColumns(): string[] {
  return props.columns.slice(0, 50).map((column) => column.name);
}

async function analyzeColumns() {
  if (profiling.value) {
    return;
  }
  profiling.value = true;
  actionError.value = "";
  try {
    await profileColumns({
      tableGuid: props.tableGuid,
      metaType: props.metaType,
      columns: sampleColumns(),
    });
    await loadProfiles();
    notify.success(
      t("dataQuality.asset.profileDone", { count: sampleColumns().length })
    );
  } catch (e: unknown) {
    actionError.value = formatError(e, "dataQuality.asset.profileFailed");
  } finally {
    profiling.value = false;
  }
}

/**
 * Queue label proposals for the sampled columns. `forceRegenerate` re-analyzes
 * columns that are already labeled or already have a proposal; either way the
 * server reconciles the queue, so the toast reports what the pass actually did.
 */
async function suggestLabels(forceRegenerate: boolean) {
  if (suggesting.value) {
    return;
  }
  suggesting.value = true;
  actionError.value = "";
  try {
    const response = await suggestColumnLabels({
      tableGuid: props.tableGuid,
      metaType: props.metaType,
      columns: sampleColumns(),
      language: locale.value,
      forceRegenerate,
    });
    notifySummary(
      response.summary,
      "label",
      response.qualitySuggestions.length
    );
  } catch (e: unknown) {
    actionError.value = formatError(e, "dataQuality.asset.suggestFailed");
  } finally {
    suggesting.value = false;
    // A pass that fails part way keeps the proposals earlier batches queued, so
    // the card re-reads the queue on failure too instead of trusting the response.
    await loadSuggestions();
  }
}

/**
 * Decide one proposal from the inbox. Accepting a label writes the column's
 * label server-side, so the labels list reloads together with the queue. A
 * failed decision re-reads the queue too: the usual failure is that another
 * reviewer already decided the row, and the stale row must not survive.
 */
async function decideSuggestion(
  suggestion: QualitySuggestion,
  decision: QualitySuggestionStatus
) {
  if (decidingSuggestion.value) {
    return;
  }
  decidingSuggestion.value = suggestion.name;
  decideError.value = "";
  actionError.value = "";
  try {
    await updateQualitySuggestion({ name: suggestion.name, decision });
    notify.success(
      t(
        decision === QualitySuggestionStatus.ACCEPTED
          ? "dataQuality.suggestions.accepted"
          : "dataQuality.suggestions.rejected"
      )
    );
    await Promise.all([
      loadSuggestions(),
      decision === QualitySuggestionStatus.ACCEPTED
        ? loadLabels()
        : Promise.resolve(),
    ]);
  } catch (e: unknown) {
    decideError.value = formatError(e, "dataQuality.suggestions.decideError");
    await loadSuggestions();
  } finally {
    decidingSuggestion.value = "";
  }
}

async function clearLabel(column: string) {
  actionError.value = "";
  try {
    await updateColumnLabel({
      tableGuid: props.tableGuid,
      metaType: props.metaType,
      column,
      action: ColumnLabelAction.CLEAR,
    });
    await loadLabels();
    notify.success(t("dataQuality.asset.labelCleared"));
  } catch (e: unknown) {
    actionError.value = formatError(e, "dataQuality.asset.labelUpdateFailed");
  }
}

onMounted(loadAll);
watch(
  () => props.tableGuid,
  () => {
    // The previous target's queue must not leak into the new one while it loads.
    pendingSuggestions.value = [];
    pendingSuggestionsTruncated.value = false;
    suggestionsLoadFailed.value = false;
    decideError.value = "";
    void loadAll();
  }
);
</script>
