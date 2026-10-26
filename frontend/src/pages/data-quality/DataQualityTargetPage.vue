<template>
  <div class="space-y-4">
    <PageHeader
      :title="displayName"
      :description="description"
    >
      <template #breadcrumb>
        <nav class="flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
          <RouterLink
            :to="{ name: 'DataQualityOverview' }"
            class="transition-colors hover:text-foreground"
          >
            {{ t("menu.dataQuality") }}
          </RouterLink>
          <ChevronRight class="h-3.5 w-3.5" />
          <RouterLink
            :to="{ name: 'DataQualityTargets' }"
            class="transition-colors hover:text-foreground"
          >
            {{ t("dataQuality.workspace.backToTargets") }}
          </RouterLink>
        </nav>
      </template>

      <template #title-extra>
        <Badge
          v-if="summary.orphaned"
          variant="danger"
          :title="t('dataQuality.workspace.orphanedHint')"
        >
          {{ t("dataQuality.orphaned") }}
        </Badge>
        <Badge
          v-if="summary.schemaChanged"
          variant="warning"
          :title="t('dataQuality.targetChanged')"
        >
          {{ t("dataQuality.targetChanged") }}
        </Badge>
      </template>

      <template #actions>
        <Button
          v-if="canRun"
          variant="outline"
          :disabled="runningAll || summary.ruleCount === 0"
          :title="t('dataQuality.workspace.runAllHint')"
          @click="runAll"
        >
          <Loader2
            v-if="runningAll"
            class="mr-2 h-4 w-4 animate-spin"
          />
          <Zap
            v-else
            class="mr-2 h-4 w-4"
          />
          {{ t("dataQuality.workspace.runAll") }}
        </Button>
        <Button
          v-if="canCreate"
          variant="outline"
          as-child
        >
          <RouterLink
            :to="newRuleTo"
            :title="t('dataQuality.workspace.addRuleHint')"
          >
            <Plus class="mr-2 h-4 w-4" />
            {{ t("dataQuality.workspace.addRule") }}
          </RouterLink>
        </Button>
        <Button
          v-if="canSuggest"
          @click="focusCopilot"
        >
          <Sparkles class="mr-2 h-4 w-4" />
          {{ t("dataQuality.copilot.title") }}
        </Button>
      </template>
    </PageHeader>

    <PageState
      :loading="loading"
      :error="error"
      :title="t('dataQuality.workspace.rulesLoadFailed')"
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

      <div class="space-y-4">
        <!-- The target's health as one compact line: the header answers "is this
             table okay?" before the reader scans a single rule. -->
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-sm">
          <span class="text-muted-foreground">
            {{ t("dataQuality.workspace.summaryRules", { count: summary.ruleCount }) }}
          </span>
          <Badge variant="success">
            {{ t("dataQuality.workspace.summaryPassing", { count: passingCount }) }}
          </Badge>
          <Badge :variant="failingCount > 0 ? 'destructive' : 'neutral'">
            {{ t("dataQuality.workspace.summaryFailing", { count: failingCount }) }}
          </Badge>
          <Badge :variant="summary.pendingDraftCount > 0 ? 'info' : 'neutral'">
            {{ t("dataQuality.workspace.summaryDrafts", { count: summary.pendingDraftCount }) }}
          </Badge>
          <Badge :variant="summary.openIssueCount > 0 ? 'danger' : 'neutral'">
            {{ t("dataQuality.workspace.summaryIssues", { count: summary.openIssueCount }) }}
          </Badge>
          <span class="text-xs text-muted-foreground">
            {{ lastRunText }}
          </span>
        </div>

        <p
          v-if="summary.orphaned"
          class="text-xs text-danger"
        >
          {{ t("dataQuality.workspace.targetMissingTitle") }} —
          {{ t("dataQuality.workspace.targetMissingHint") }}
        </p>

        <div class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_380px]">
          <div class="min-w-0 space-y-4">
            <div
              v-if="draftStripVisible"
              class="flex flex-wrap items-center justify-between gap-2 rounded-md border border-dashed px-3 py-2"
            >
              <div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
                <Sparkles class="h-3.5 w-3.5 shrink-0 text-primary" />
                <span class="font-medium">
                  {{ t("dataQuality.workspace.draftsTitle") }}
                </span>
                <span class="text-muted-foreground">
                  {{ t("dataQuality.workspace.draftsHint") }}
                </span>
              </div>
              <RouterLink
                :to="{ name: 'DataQualitySuggestions' }"
                class="text-xs text-primary hover:underline"
              >
                {{ t("dataQuality.workspace.draftsLink") }}
              </RouterLink>
            </div>
            <p
              v-if="draftsFailed"
              class="text-sm text-danger"
            >
              {{ t("dataQuality.workspace.draftsLoadFailed") }}
            </p>

            <EmptyState
              v-if="sections.length === 0"
              :icon="ShieldCheck"
              :title="t('dataQuality.workspace.noRulesTitle')"
              :description="t('dataQuality.workspace.noRulesHint')"
            >
              <Button
                v-if="canCreate"
                variant="outline"
                size="sm"
                as-child
              >
                <RouterLink :to="newRuleTo">
                  <Plus class="mr-2 h-4 w-4" />
                  {{ t("dataQuality.workspace.addRule") }}
                </RouterLink>
              </Button>
            </EmptyState>

            <Card
              v-for="section in sections"
              :key="section.key"
            >
              <CardHeader class="flex flex-row items-start justify-between gap-3 border-b">
                <div class="min-w-0 space-y-1">
                  <div class="flex flex-wrap items-center gap-2">
                    <CardTitle class="text-base">
                      {{ sectionTitle(section) }}
                    </CardTitle>
                    <Badge
                      variant="secondary"
                      class="tabular-nums"
                      :title="t('dataQuality.workspace.rulesTitle')"
                    >
                      {{ section.rules.length }}
                    </Badge>
                  </div>
                  <CardDescription>
                    {{ sectionHint(section) }}
                  </CardDescription>
                </div>
              </CardHeader>
              <CardContent class="p-0">
                <div
                  v-if="section.drafts.length > 0"
                  class="space-y-2 border-b p-4"
                >
                  <TargetDraftCard
                    v-for="draft in section.drafts"
                    :key="draft.name"
                    :suggestion="draft"
                    :can-create="canCreate"
                    :can-suggest="canSuggest"
                    :highlight="newNames.has(draft.name)"
                    @accepted="onDraftAccepted(draft, $event)"
                    @rejected="onDraftRejected"
                    @refresh="loadDrafts"
                  />
                </div>
                <ul
                  v-if="section.rules.length > 0"
                  class="divide-y"
                >
                  <TargetCheckRow
                    v-for="rule in section.rules"
                    :key="rule.name"
                    :rule="rule"
                    :issue-count="issuesByRule.get(rule.name)?.length ?? 0"
                    :can-run="canRun"
                    :can-update="canUpdate"
                    @run="runRule(rule)"
                  />
                </ul>
              </CardContent>
            </Card>
          </div>

          <div class="min-w-0 space-y-4 lg:sticky lg:top-0 lg:self-start">
            <TargetCopilotPanel
              v-if="canSuggest"
              ref="copilot"
              :target-guid="targetGuid"
              :meta-type="effectiveMetaType"
              :can-suggest="canSuggest"
              :disabled-reason="summary.orphaned ? t('dataQuality.copilot.unavailable') : undefined"
              @drafted="onDrafted"
            />

            <Card v-if="canListIssues">
              <CardHeader class="border-b">
                <CardTitle class="text-base">
                  {{ t("dataQuality.workspace.issuesTitle") }}
                </CardTitle>
                <CardDescription>
                  {{ t("dataQuality.workspace.issuesHint") }}
                </CardDescription>
              </CardHeader>
              <CardContent class="pt-4">
                <p
                  v-if="issuesFailed"
                  class="text-sm text-danger"
                >
                  {{ t("dataQuality.workspace.issuesLoadFailed") }}
                </p>
                <p
                  v-else-if="visibleIssues.length === 0"
                  class="text-sm text-muted-foreground"
                >
                  {{ t("dataQuality.workspace.issuesEmpty") }}
                </p>
                <ul
                  v-else
                  class="divide-y"
                >
                  <li
                    v-for="issue in visibleIssues"
                    :key="issue.name"
                    class="flex items-center justify-between gap-2 py-2"
                  >
                    <div class="min-w-0">
                      <RouterLink
                        v-if="canViewIssue"
                        :to="issueTo(issue)"
                        class="block truncate text-sm font-medium hover:underline"
                      >
                        {{ issueRuleTitle(issue) }}
                      </RouterLink>
                      <span
                        v-else
                        class="block truncate text-sm font-medium"
                      >
                        {{ issueRuleTitle(issue) }}
                      </span>
                      <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                        <Badge :variant="qualityIssueStatusVariant(issue.status)">
                          {{ t(qualityIssueStatusKey(issue.status)) }}
                        </Badge>
                        <span :title="formatDateTime(issue.lastSeenTime, locale)">
                          {{ formatRelativeTime(issue.lastSeenTime, locale) }}
                        </span>
                      </div>
                    </div>
                    <RouterLink
                      v-if="canViewIssue"
                      :to="issueTo(issue)"
                      class="shrink-0 text-xs text-primary hover:underline"
                    >
                      {{ t("dataQuality.viewIssue") }}
                    </RouterLink>
                  </li>
                </ul>
                <Button
                  variant="ghost"
                  size="sm"
                  class="mt-2 w-full"
                  as-child
                >
                  <RouterLink :to="{ name: 'DataQualityIssues' }">
                    {{ t("dataQuality.workspace.issuesViewAll") }}
                  </RouterLink>
                </Button>
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import {
  ChevronRight,
  Loader2,
  Plus,
  ShieldCheck,
  Sparkles,
  Zap,
} from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import {
  listQualityIssues,
  listQualityRules,
  listQualitySuggestions,
  runQualityRules,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TargetCheckRow from "@/components/data-quality/TargetCheckRow.vue";
import TargetCopilotPanel from "@/components/data-quality/TargetCopilotPanel.vue";
import TargetDraftCard from "@/components/data-quality/TargetDraftCard.vue";
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
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityIssue,
  QualityIssueStatus,
  type QualityRule,
  QualityStatus,
  type QualitySuggestion,
  type QualitySuggestionQueueSummary,
  QualitySuggestionStatus,
  QualityTargetPathSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  qualityIssueStatusKey,
  qualityIssueStatusVariant,
} from "@/utils/dataQuality";
import {
  isPendingDraft,
  suggestionTargetPath,
} from "@/utils/dataQualitySuggestion";
import {
  groupTargetChecks,
  type QualityTargetSummary,
  summarizeQualityTargets,
  type TargetCheckSection,
  targetDisplayName,
  unresolvedIssuesByRule,
} from "@/utils/dataQualityTarget";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { routeParamToGuid } from "@/utils/guid";
import { parseMetaType } from "@/utils/metaType";

/**
 * One target's quality workspace: every rule on a table or view, read in the
 * three sections a reader maintains it in (dataset-level, cross-column, one per
 * field), with the AI copilot pinned beside them. Drafts appear inline under the
 * section they would join, so accepting one creates the rule exactly there.
 *
 * The three reads are independent: a failed suggestions or issues call degrades
 * one region rather than the page, while a failed rules call is the page's own
 * error because it is what the workspace is.
 */

/** The page reads a target's whole rule set in one request. */
const RULE_PAGE_SIZE = 1000;

const { t, locale } = useI18n();
const route = useRoute();
const authStore = useAuthStore();
const { formatError } = useErrorMessage();

const targetGuid = computed(() => routeParamToGuid(route.params.targetGuid));

const canRun = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.run")
);
const canCreate = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.create")
);
const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.update")
);
const canSuggest = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.suggest")
);
const canListIssues = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.issues.list")
);
const canViewIssue = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.issues.get")
);

const rules = ref<QualityRule[]>([]);
const suggestions = ref<QualitySuggestion[]>([]);
const issues = ref<QualityIssue[]>([]);
const loading = ref(true);
const error = ref("");
const draftsFailed = ref(false);
const issuesFailed = ref(false);
const runningAll = ref(false);
const runningRule = ref("");
const newNames = ref<Set<string>>(new Set());
const copilot = ref<InstanceType<typeof TargetCopilotPanel>>();

/** The metadata type the target is read as: the query wins, then the rules. */
const effectiveMetaType = computed<MetaType>(() => {
  const fromQuery = parseMetaType(route.query.metaType);
  if (fromQuery !== null) {
    return fromQuery;
  }
  const fromRule = rules.value[0]?.target?.metaType;
  return fromRule !== undefined && fromRule !== MetaType.UNSPECIFIED
    ? fromRule
    : MetaType.TABLE;
});

/** The health of this one target, with a path-only fallback before rules load. */
const summary = computed<QualityTargetSummary>(() => {
  const found = summarizeQualityTargets(
    rules.value,
    issues.value,
    suggestions.value
  ).find((item) => item.guid === targetGuid.value);
  if (found) {
    return found;
  }
  const path = suggestionTargetPath(targetGuid.value);
  return {
    guid: targetGuid.value,
    metaType: effectiveMetaType.value,
    path: create(QualityTargetPathSchema, {
      instance: path.instance,
      database: path.database,
      schema: path.schema,
      object: path.object,
    }),
    rules: [],
    ruleCount: 0,
    enabledCount: 0,
    pausedCount: 0,
    columns: [],
    openIssueCount: 0,
    pendingDraftCount: 0,
    pendingLabelCount: 0,
    worstStatus: undefined,
    lastRunTime: undefined,
    orphaned: false,
    schemaChanged: false,
  };
});

const displayName = computed(() => targetDisplayName(summary.value));

const description = computed(() => {
  const path = summary.value.path;
  const parts = [path?.instance, path?.database, path?.schema].filter(Boolean);
  return [...parts, kindLabel.value].filter(Boolean).join(" · ");
});

/**
 * The target's object kind, as a singular label. `metaTypeSingularLabel` falls
 * back to the plural catalog label ("Tables") for relation types, which reads
 * wrong inside a one-line description.
 */
const kindLabel = computed(() => {
  switch (effectiveMetaType.value) {
    case MetaType.VIEW:
      return t("dataQuality.workspace.kindView");
    case MetaType.MATERIALIZED_VIEW:
      return t("dataQuality.workspace.kindMaterializedView");
    case MetaType.EXTERNAL_TABLE:
      return t("dataQuality.workspace.kindExternalTable");
    case MetaType.TABLE:
      return t("dataQuality.workspace.kindTable");
    default:
      return "";
  }
});

const sections = computed(() =>
  groupTargetChecks(rules.value, suggestions.value)
);

const pendingDrafts = computed(() => suggestions.value.filter(isPendingDraft));

const draftStripVisible = computed(
  () => canSuggest.value && pendingDrafts.value.length > 0
);

const issuesByRule = computed(() => unresolvedIssuesByRule(issues.value));

const openIssues = computed(() =>
  issues.value.filter((issue) => issue.status !== QualityIssueStatus.RESOLVED)
);

/** The card previews the newest problems; the rest are one click away. */
const ISSUE_PREVIEW_LIMIT = 5;
const visibleIssues = computed(() =>
  openIssues.value.slice(0, ISSUE_PREVIEW_LIMIT)
);

const passingCount = computed(
  () =>
    summary.value.rules.filter(
      (rule) => rule.lastResult?.status === QualityStatus.PASS
    ).length
);
const failingCount = computed(
  () =>
    summary.value.rules.filter((rule) => {
      const status = rule.lastResult?.status;
      return status === QualityStatus.FAIL || status === QualityStatus.ERROR;
    }).length
);

const lastRunText = computed(() =>
  summary.value.lastRunTime
    ? t("dataQuality.workspace.summaryLastRun", {
        time: formatRelativeTime(summary.value.lastRunTime, locale.value),
      })
    : t("dataQuality.workspace.summaryNeverRun")
);

const newRuleTo = computed(() => ({
  name: "DataQualityRuleNew",
  query: {
    table_guid: targetGuid.value,
    meta_type: String(effectiveMetaType.value),
  },
}));

function sectionTitle(section: TargetCheckSection): string {
  switch (section.kind) {
    case "dataset":
      return t("dataQuality.workspace.datasetRulesTitle");
    case "multi":
      return t("dataQuality.workspace.crossColumnRulesTitle");
    default:
      return section.column;
  }
}

function sectionHint(section: TargetCheckSection): string {
  switch (section.kind) {
    case "dataset":
      return t("dataQuality.workspace.datasetRulesHint");
    case "multi":
      return t("dataQuality.workspace.crossColumnRulesHint");
    default:
      return t("dataQuality.workspace.columnRulesHint");
  }
}

const ruleTitles = computed(() =>
  Object.fromEntries(rules.value.map((rule) => [rule.name, rule.title]))
);

function issueRuleTitle(issue: QualityIssue): string {
  return (
    ruleTitles.value[issue.rule] ||
    issue.rule.split("/").pop() ||
    issue.rule ||
    "-"
  );
}

function issueTo(issue: QualityIssue) {
  return {
    name: "DataQualityIssueDetail",
    params: { issueId: issue.name.split("/") },
  };
}

function focusCopilot() {
  copilot.value?.focus();
}

/** Guards against a slow read of the previous target landing on this one. */
let loadTicket = 0;

async function loadDrafts(ticket = loadTicket) {
  if (!canSuggest.value) {
    return;
  }
  try {
    const page = await listQualitySuggestions({
      targetGuid: targetGuid.value,
      status: QualitySuggestionStatus.PENDING,
      pageSize: RULE_PAGE_SIZE,
    });
    if (ticket !== loadTicket) {
      return;
    }
    suggestions.value = page.items;
    draftsFailed.value = false;
  } catch {
    if (ticket !== loadTicket) {
      return;
    }
    draftsFailed.value = true;
  }
}

async function loadIssues(ticket = loadTicket) {
  if (!canListIssues.value) {
    return;
  }
  try {
    const page = await listQualityIssues({
      targetGuid: targetGuid.value,
      pageSize: RULE_PAGE_SIZE,
    });
    if (ticket !== loadTicket) {
      return;
    }
    issues.value = page.items;
    issuesFailed.value = false;
  } catch {
    if (ticket !== loadTicket) {
      return;
    }
    issuesFailed.value = true;
  }
}

async function load() {
  const ticket = ++loadTicket;
  loading.value = true;
  error.value = "";
  rules.value = [];
  suggestions.value = [];
  issues.value = [];
  newNames.value = new Set();
  const guid = targetGuid.value;
  const rulesRead = listQualityRules({
    targetGuid: guid,
    pageSize: RULE_PAGE_SIZE,
  }).then(
    (page) => page.items,
    (cause: unknown) => {
      if (ticket === loadTicket) {
        error.value = formatError(
          cause,
          "dataQuality.workspace.rulesLoadFailed"
        );
      }
      return [];
    }
  );
  const [loadedRules] = await Promise.all([
    rulesRead,
    loadDrafts(ticket),
    loadIssues(ticket),
  ]);
  if (ticket !== loadTicket) {
    return;
  }
  rules.value = loadedRules;
  loading.value = false;
}

/** The copilot's drafts join the local queue without re-reading the full page. */
function onDrafted(
  drafted: QualitySuggestion[],
  _summary: QualitySuggestionQueueSummary | undefined,
  names: string[]
) {
  const known = new Set(suggestions.value.map((item) => item.name));
  const added = drafted.filter((item) => !known.has(item.name));
  suggestions.value = [...added, ...suggestions.value];
  // Refreshed proposals keep their identity and are not "new"; only the drafts
  // this pass actually added get the highlight.
  const addedNames = new Set(added.map((item) => item.name));
  newNames.value = new Set(names.filter((name) => addedNames.has(name)));
}

function onDraftAccepted(draft: QualitySuggestion, rule: QualityRule) {
  suggestions.value = suggestions.value.filter(
    (item) => item.name !== draft.name
  );
  newNames.value.delete(draft.name);
  rules.value = [...rules.value, rule];
}

function onDraftRejected(name: string) {
  suggestions.value = suggestions.value.filter((item) => item.name !== name);
  newNames.value.delete(name);
}

async function runAll() {
  if (runningAll.value || summary.value.ruleCount === 0) {
    return;
  }
  runningAll.value = true;
  try {
    const executions = await runQualityRules({ targetGuid: targetGuid.value });
    notify.success(
      t("dataQuality.workspace.runAllSubmitted", { count: executions.length })
    );
  } catch (cause: unknown) {
    notify.error(formatError(cause, "dataQuality.workspace.runAllFailed"));
  } finally {
    runningAll.value = false;
  }
}

async function runRule(rule: QualityRule) {
  if (runningRule.value !== "") {
    return;
  }
  runningRule.value = rule.name;
  try {
    await runQualityRules({ rules: [rule.name] });
    notify.success(t("dataQuality.executionStarted"));
  } catch (cause: unknown) {
    notify.error(formatError(cause, "dataQuality.workspace.runRuleFailed"));
  } finally {
    runningRule.value = "";
  }
}

onMounted(load);

// Navigating from one target's workspace to another reuses this component, so
// the whole workspace reloads with the GUID.
watch(targetGuid, () => {
  void load();
});
</script>
