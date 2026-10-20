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
        <Button
          v-if="canSuggest"
          variant="outline"
          size="sm"
          :disabled="suggesting"
          @click="suggestLabels"
        >
          <Sparkles class="mr-1 h-4 w-4" />
          {{ suggesting ? t("dataQuality.asset.suggesting") : t("dataQuality.asset.suggestLabels") }}
        </Button>
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
              {{ t("dataQuality.asset.resultsTitle") }}
            </CardTitle>
            <CardDescription>
              {{ t("dataQuality.asset.resultsDescription") }}
            </CardDescription>
          </div>
          <Button
            variant="ghost"
            size="sm"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualityRuns' }">
              {{ t("dataQuality.asset.viewRuns") }}
            </RouterLink>
          </Button>
        </CardHeader>
        <CardContent class="pt-4">
          <EmptyState
            v-if="results.length === 0"
            compact
            :icon="ListChecks"
            :title="t('dataQuality.asset.noResults')"
          />
          <ul
            v-else
            class="divide-y"
          >
            <li
              v-for="result in results.slice(0, 5)"
              :key="result.name"
              class="flex items-center justify-between gap-3 py-2"
            >
              <div class="min-w-0">
                <div class="truncate text-sm font-medium">
                  {{ ruleTitle(result.rule) }}
                </div>
                <div class="mt-1 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                  <Badge :variant="qualityStatusVariant(result.status)">
                    {{ t(qualityStatusKey(result.status)) }}
                  </Badge>
                  <span class="tabular-nums">
                    {{ t("dataQuality.failedRows") }}
                    {{ formatRowCount(result.failedRows, locale) }}
                  </span>
                  <span :title="formatDateTime(result.finishTime, locale)">
                    {{ formatRelativeTime(result.finishTime, locale) }}
                  </span>
                </div>
              </div>
              <RouterLink
                v-if="result.run"
                :to="runRoute(result.run)"
                class="shrink-0 text-xs text-primary hover:underline"
              >
                {{ t("dataQuality.viewRun") }}
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
  CircleCheck,
  ListChecks,
  Plus,
  ShieldCheck,
  Sparkles,
  X,
} from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import {
  listColumnLabels,
  listColumnProfiles,
  listQualityIssues,
  listQualityRuleResults,
  listQualityRules,
  profileColumns,
  suggestColumnLabels,
  updateColumnLabel,
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  ColumnLabelAction,
  type ColumnProfile,
  type ColumnSemanticLabel,
  type QualityIssue,
  QualityIssueStatus,
  type QualityRule,
  type QualityRuleResult,
} from "@/types/proto-es/v1/data_quality_service_pb";
import type { ColumnMetadata } from "@/types/proto-es/v1/database_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  formatRowCount,
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
  labelStatusKey,
  semanticTypeKey,
} from "@/utils/dataQualitySuggestion";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * The table detail's Data Quality tab: this target's rules, latest results and
 * open issues, plus the deterministic column profiles and accepted semantic
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

const rules = ref<QualityRule[]>([]);
const results = ref<QualityRuleResult[]>([]);
const openIssues = ref<QualityIssue[]>([]);
const labels = ref<ColumnSemanticLabel[]>([]);
const profiles = ref<ColumnProfile[]>([]);
const profiling = ref(false);
const suggesting = ref(false);
const actionError = ref("");

const createRuleTo = computed(() => ({
  name: "DataQualityRuleNew",
  query: {
    table_guid: props.tableGuid,
    meta_type: String(props.metaType),
  },
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

function ruleRoute(name: string) {
  return {
    name: "DataQualityRuleDetail",
    params: { ruleId: name.split("/") },
  };
}

function runRoute(name: string) {
  return { name: "DataQualityRunDetail", params: { runId: name.split("/") } };
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

async function loadResults() {
  try {
    const page = await listQualityRuleResults({
      targetGuid: props.tableGuid,
      pageSize: 20,
    });
    results.value = page.items;
  } catch {
    results.value = [];
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

async function loadAll() {
  if (!canView.value) {
    return;
  }
  await Promise.all([
    loadRules(),
    loadResults(),
    loadIssues(),
    loadLabels(),
    loadProfiles(),
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

async function suggestLabels() {
  if (suggesting.value) {
    return;
  }
  suggesting.value = true;
  actionError.value = "";
  try {
    const suggestions = await suggestColumnLabels({
      tableGuid: props.tableGuid,
      metaType: props.metaType,
      columns: sampleColumns(),
      language: locale.value,
    });
    notify.success(
      t("dataQuality.asset.suggestionsQueued", { count: suggestions.length })
    );
  } catch (e: unknown) {
    actionError.value = formatError(e, "dataQuality.asset.suggestFailed");
  } finally {
    suggesting.value = false;
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
  () => void loadAll()
);
</script>
