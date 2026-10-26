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
          v-if="orphaned"
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
          :title="t('dataQuality.workspace.addRuleHint')"
          @click="openComposer()"
        >
          <Plus class="mr-2 h-4 w-4" />
          {{ t("dataQuality.workspace.addRule") }}
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

        <!-- The contract sits under the health line: the rules' state first, then
             the promise they add up to. Its read degrades on its own. -->
        <TargetContractCard
          :target-guid="targetGuid"
          :meta-type="effectiveMetaType"
          :contract="contract"
          :loading="contractLoading"
          :error="contractError"
          :can-publish="canPublish"
          :can-update="canUpdate"
          @published="onContractChanged"
          @updated="onContractChanged"
          @retry="loadContract()"
        />

        <p
          v-if="orphaned"
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

            <TargetCoverageCard
              :target-guid="targetGuid"
              :meta-type="effectiveMetaType"
              :rules="rules"
              :can-create="canCreate"
              @create="openComposer"
            />

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
                @click="openComposer()"
              >
                <Plus class="mr-2 h-4 w-4" />
                {{ t("dataQuality.workspace.addRule") }}
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
                <Button
                  v-if="canCreate && section.kind === 'dataset'"
                  variant="ghost"
                  size="sm"
                  class="shrink-0"
                  @click="openAddCheck({ scope: 'dataset' })"
                >
                  <Plus class="mr-1 h-4 w-4" />
                  {{ t("dataQuality.workspace.addDatasetCheck") }}
                </Button>
                <Button
                  v-if="canCreate && section.kind === 'column'"
                  variant="ghost"
                  size="sm"
                  class="shrink-0"
                  @click="openAddCheck({ scope: 'column', column: section.column })"
                >
                  <Plus class="mr-1 h-4 w-4" />
                  {{ t("dataQuality.workspace.addColumnCheck") }}
                </Button>
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
                    @edit="openComposer(draft)"
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
                    :history="ruleHistory.get(rule.name) ?? []"
                    :highlight="highlightedRule === rule.name"
                    @run="runRule(rule)"
                    @edit="openEditor(rule)"
                    @open="openCheck(rule)"
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
              :disabled-reason="orphaned ? t('dataQuality.copilot.unavailable') : undefined"
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
                  v-if="visibleIssues.length === 0"
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

    <!-- Rule authoring stays on the target: the dialog fixes the target, and the
         saved rule joins (or replaces itself in) its section without a page read. -->
    <TargetRuleComposer
      v-if="canCreate || canUpdate"
      v-model:open="composerOpen"
      :rule="editingRule"
      :suggestion="composerSuggestion"
      :scope="composerScope"
      :initial-column="composerInitialColumn"
      :target-guid="targetGuid"
      :meta-type="effectiveMetaType"
      :target-label="displayName"
      :default-schedule-seconds="contractDefaultScheduleSeconds"
      :default-severity="contractDefaultSeverity"
      @saved="onRuleSaved"
    />

    <!-- One check's own side sheet. It replaces the old rule page: opening a row
         keeps the target workspace behind it, so its siblings stay in view. -->
    <QualityCheckDrawer
      :open="selectedCheck !== ''"
      :rule-name="selectedCheck"
      :target-guid="targetGuid"
      :meta-type="effectiveMetaType"
      @update:open="onCheckOpenChange"
      @edit="onCheckEdit"
      @changed="onCheckChanged"
      @deleted="onCheckDeleted"
    />
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
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute, useRouter } from "vue-router";
import {
  getQualityContract,
  getQualityTarget,
  listQualitySuggestions,
  runQualityRules,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import QualityCheckDrawer from "@/components/data-quality/QualityCheckDrawer.vue";
import TargetCheckRow from "@/components/data-quality/TargetCheckRow.vue";
import TargetContractCard from "@/components/data-quality/TargetContractCard.vue";
import TargetCopilotPanel from "@/components/data-quality/TargetCopilotPanel.vue";
import TargetCoverageCard from "@/components/data-quality/TargetCoverageCard.vue";
import TargetDraftCard from "@/components/data-quality/TargetDraftCard.vue";
import TargetRuleComposer from "@/components/data-quality/TargetRuleComposer.vue";
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
  type QualityContract,
  type QualityExecutionSummary,
  type QualityIssue,
  type QualityRule,
  QualityRuleState,
  QualitySeverity,
  QualityStatus,
  type QualitySuggestion,
  type QualitySuggestionQueueSummary,
  QualitySuggestionStatus,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  qualityIssueStatusKey,
  qualityIssueStatusVariant,
} from "@/utils/dataQuality";
import { isPendingDraft } from "@/utils/dataQualitySuggestion";
import {
  groupTargetChecks,
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
 * One aggregate read supplies the health summary, the rules and the open issues;
 * its failure is the page's own error because it is what the workspace is. AI
 * drafts need the suggest permission the aggregate read does not require, so a
 * suggest-capable reader loads that queue separately and a failed draft read
 * degrades one region. Local decisions then adjust the summary counts in place
 * rather than re-reading everything.
 */

/** The page re-reads the target's draft queue in one request. */
const RULE_PAGE_SIZE = 1000;

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
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
const canPublish = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.contracts.publish")
);

const rules = ref<QualityRule[]>([]);
/** Per-rule recent completed executions, newest first (server order). */
const ruleHistory = ref<Map<string, QualityExecutionSummary[]>>(new Map());
const suggestions = ref<QualitySuggestion[]>([]);
const issues = ref<QualityIssue[]>([]);
/** The server's health summary; adjusted in place by local draft decisions. */
const summary = ref<QualityTargetSummary>(
  create(QualityTargetSummarySchema, {})
);
const orphaned = ref(false);
/** The target's contract: defaults, published version and drift. */
const contract = ref<QualityContract>();
const contractLoading = ref(true);
const contractError = ref("");
const loading = ref(true);
const error = ref("");
const draftsFailed = ref(false);
const runningAll = ref(false);
const runningRule = ref("");
const newNames = ref<Set<string>>(new Set());
/** The inline composer, open on the target this page names. */
const composerOpen = ref(false);
/** The rule the composer edits; undefined makes it create instead. */
const editingRule = ref<QualityRule>();
/** The section a new check was opened from, which seeds its type. */
const composerScope = ref<"dataset" | "column">();
/** The column a column section named, written into the new check's config. */
const composerInitialColumn = ref("");
/** The AI draft the composer was opened from; undefined means a blank create. */
const composerSuggestion = ref<QualitySuggestion>();
/** The check whose side sheet is open, by full resource name; empty closes it. */
const selectedCheck = ref("");
const copilot = ref<InstanceType<typeof TargetCopilotPanel>>();

/**
 * The metadata type the target is read as: the query wins, then the summary the
 * server resolved, then the rules it returned, then the table default.
 */
const effectiveMetaType = computed<MetaType>(() => {
  const fromQuery = parseMetaType(route.query.metaType);
  if (fromQuery !== null) {
    return fromQuery;
  }
  const fromSummary = summary.value.target?.metaType;
  if (fromSummary !== undefined && fromSummary !== MetaType.UNSPECIFIED) {
    return fromSummary;
  }
  const fromRule = rules.value[0]?.target?.metaType;
  return fromRule !== undefined && fromRule !== MetaType.UNSPECIFIED
    ? fromRule
    : MetaType.TABLE;
});

const displayName = computed(() => targetDisplayName(summary.value));

const description = computed(() => {
  const path = summary.value.target?.path;
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

/**
 * The rule a back-link from another page named with `?highlight=` (a full rule
 * resource name). It survives the local edits: it only names the row to mark.
 */
const highlightedRule = computed(() =>
  typeof route.query.highlight === "string" ? route.query.highlight : ""
);

/**
 * Bring the named row into view once the sections have rendered. A highlight for
 * a rule that this target does not have (deleted, or another target's) simply
 * does not resolve; the workspace renders normally.
 */
async function scrollToHighlightedRule() {
  const name = highlightedRule.value;
  if (name === "") {
    return;
  }
  await nextTick();
  const element = document.getElementById(
    `dq-rule-${name.split("/").pop() ?? ""}`
  );
  element?.scrollIntoView?.({ block: "center" });
}

const pendingDrafts = computed(() => suggestions.value.filter(isPendingDraft));

const draftStripVisible = computed(
  () => canSuggest.value && pendingDrafts.value.length > 0
);

const issuesByRule = computed(() => unresolvedIssuesByRule(issues.value));

/** The card previews the newest problems; the rest are one click away. */
const ISSUE_PREVIEW_LIMIT = 5;
const visibleIssues = computed(() =>
  issues.value.slice(0, ISSUE_PREVIEW_LIMIT)
);

const passingCount = computed(
  () =>
    rules.value.filter((rule) => rule.lastResult?.status === QualityStatus.PASS)
      .length
);
const failingCount = computed(
  () =>
    rules.value.filter((rule) => {
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

/** The contract's seed for a new rule; absent settings keep the form's default. */
const contractDefaultScheduleSeconds = computed(() =>
  Number(contract.value?.settings?.defaultScheduleIntervalSeconds ?? 0n)
);
const contractDefaultSeverity = computed(
  () => contract.value?.settings?.defaultSeverity ?? QualitySeverity.UNSPECIFIED
);

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

/**
 * Read the contract beside the workspace. A failure degrades only the contract
 * strip: the rules, drafts and issues are still the page, and the strip offers a
 * retry of its own.
 */
async function loadContract(ticket = loadTicket) {
  contractLoading.value = true;
  contractError.value = "";
  const guid = targetGuid.value;
  const metaType = parseMetaType(route.query.metaType) ?? MetaType.UNSPECIFIED;
  try {
    const loaded = await getQualityContract({
      targetGuid: guid,
      metaType,
    });
    if (ticket !== loadTicket) {
      return;
    }
    contract.value = loaded;
  } catch (cause: unknown) {
    if (ticket !== loadTicket) {
      return;
    }
    contract.value = undefined;
    contractError.value = formatError(cause, "dataQuality.contract.loadFailed");
  } finally {
    if (ticket === loadTicket) {
      contractLoading.value = false;
    }
  }
}

async function load() {
  const ticket = ++loadTicket;
  loading.value = true;
  error.value = "";
  rules.value = [];
  suggestions.value = [];
  issues.value = [];
  summary.value = create(QualityTargetSummarySchema, {});
  orphaned.value = false;
  draftsFailed.value = false;
  contract.value = undefined;
  contractError.value = "";
  contractLoading.value = true;
  newNames.value = new Set();
  selectedCheck.value = "";
  composerSuggestion.value = undefined;
  const guid = targetGuid.value;
  const metaType = parseMetaType(route.query.metaType) ?? MetaType.UNSPECIFIED;
  // Drafts need the suggest permission the aggregate read does not, and the
  // contract is a projection read of its own; both start alongside the workspace
  // and degrade one region each instead of failing the page.
  const draftsRead = loadDrafts(ticket);
  const contractRead = loadContract(ticket);
  try {
    const response = await getQualityTarget({ targetGuid: guid, metaType });
    if (ticket !== loadTicket) {
      return;
    }
    summary.value = response.summary ?? create(QualityTargetSummarySchema, {});
    rules.value = response.qualityRules;
    ruleHistory.value = new Map(
      response.ruleHistory.map((history) => [history.rule, history.executions])
    );
    issues.value = response.openIssues;
    orphaned.value = response.orphanedReason !== "";
  } catch (cause: unknown) {
    if (ticket !== loadTicket) {
      return;
    }
    error.value = formatError(cause, "dataQuality.workspace.rulesLoadFailed");
  } finally {
    await draftsRead;
    await contractRead;
    if (ticket === loadTicket) {
      loading.value = false;
      void scrollToHighlightedRule();
    }
  }
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
  summary.value.pendingDraftCount += added.length;
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
  summary.value.ruleCount += 1;
  if (rule.state === QualityRuleState.ENABLED) {
    summary.value.enabledRuleCount += 1;
  }
  summary.value.pendingDraftCount = Math.max(
    0,
    summary.value.pendingDraftCount - 1
  );
}

/**
 * A rule created in the composer joins the local list where the section grouping
 * files it; only the counts it changed are adjusted, nothing is re-read.
 */
function onRuleCreated(rule: QualityRule) {
  rules.value = [...rules.value, rule];
  summary.value.ruleCount += 1;
  if (rule.state === QualityRuleState.ENABLED) {
    summary.value.enabledRuleCount += 1;
  }
}

/** A saved rule replaces itself in the list; an edit changes no count. */
function onRuleSaved(rule: QualityRule) {
  const suggestion = composerSuggestion.value;
  composerSuggestion.value = undefined;
  if (editingRule.value) {
    editingRule.value = undefined;
    replaceRule(rule);
    return;
  }
  if (suggestion) {
    // Creating from a draft accepts it server-side in the same request, so the
    // local queue drops it and the header's draft count follows.
    suggestions.value = suggestions.value.filter(
      (item) => item.name !== suggestion.name
    );
    newNames.value.delete(suggestion.name);
    summary.value.pendingDraftCount = Math.max(
      0,
      summary.value.pendingDraftCount - 1
    );
  }
  onRuleCreated(rule);
}

/**
 * Opens the composer for a new check. A section can narrow it: the dataset header
 * starts on a table-level check, a column header on a check for that column.
 */
function openAddCheck(
  options: { scope?: "dataset" | "column"; column?: string } = {}
) {
  editingRule.value = undefined;
  composerSuggestion.value = undefined;
  composerScope.value = options.scope;
  composerInitialColumn.value = options.column ?? "";
  composerOpen.value = true;
}

/** The header's New rule starts a blank create; a row can pass a draft instead. */
function openComposer(suggestion?: QualitySuggestion) {
  if (suggestion) {
    openComposerFromDraft(suggestion);
    return;
  }
  openAddCheck();
}

/**
 * A draft's "adjust" opens the same composer prefilled from it, so its typed
 * config is reviewed beside every other check rather than in a separate editor.
 */
function openComposerFromDraft(suggestion: QualitySuggestion) {
  editingRule.value = undefined;
  composerSuggestion.value = suggestion;
  composerScope.value = undefined;
  composerInitialColumn.value = "";
  composerOpen.value = true;
}

/** The row's edit opens the same composer, fixed to that rule. */
function openEditor(rule: QualityRule) {
  editingRule.value = rule;
  composerSuggestion.value = undefined;
  composerScope.value = undefined;
  composerInitialColumn.value = "";
  composerOpen.value = true;
}

/** A row click opens the check's side sheet without leaving the workspace. */
function openCheck(rule: QualityRule) {
  selectedCheck.value = rule.name;
}

/** Removes `check` from the URL while preserving the other query keys. */
function removeCheckQuery() {
  if (route.query.check === undefined) {
    return;
  }
  const query = { ...route.query };
  delete query.check;
  void router.replace({ query });
}

function closeCheck() {
  selectedCheck.value = "";
  removeCheckQuery();
}

function onCheckOpenChange(open: boolean) {
  if (!open) {
    closeCheck();
  }
}

/**
 * A check named in the URL opens once the rules have loaded; a name this target
 * does not have (a stale link) is dropped silently rather than shown as an error.
 */
function syncCheckFromQuery() {
  const name = typeof route.query.check === "string" ? route.query.check : "";
  if (name === "" || loading.value) {
    return;
  }
  if (rules.value.some((rule) => rule.name === name)) {
    selectedCheck.value = name;
    return;
  }
  selectedCheck.value = "";
  removeCheckQuery();
}

/** Replaces a rule in place, or appends it when it is new to this page. */
function replaceRule(rule: QualityRule) {
  const index = rules.value.findIndex((item) => item.name === rule.name);
  rules.value =
    index === -1
      ? [...rules.value, rule]
      : rules.value.map((item, position) => (position === index ? rule : item));
}

/** The drawer's edit hands the rule to the page's composer, then closes. */
function onCheckEdit(rule: QualityRule) {
  openEditor(rule);
  closeCheck();
}

/** A pause/resume writes only the state; the counts move with it. */
function onCheckChanged(rule: QualityRule) {
  const previous = rules.value.find((item) => item.name === rule.name);
  replaceRule(rule);
  if (!previous) {
    return;
  }
  const wasEnabled = previous.state === QualityRuleState.ENABLED;
  const isEnabled = rule.state === QualityRuleState.ENABLED;
  if (wasEnabled !== isEnabled) {
    summary.value.enabledRuleCount += isEnabled ? 1 : -1;
  }
  const wasPaused = previous.state === QualityRuleState.PAUSED;
  const isPaused = rule.state === QualityRuleState.PAUSED;
  if (wasPaused !== isPaused) {
    summary.value.pausedRuleCount += isPaused ? 1 : -1;
  }
}

/** Archiving drops the row and its count, and closes the sheet. */
function onCheckDeleted(name: string) {
  const removed = rules.value.find((item) => item.name === name);
  rules.value = rules.value.filter((item) => item.name !== name);
  const history = new Map(ruleHistory.value);
  history.delete(name);
  ruleHistory.value = history;
  if (removed) {
    summary.value.ruleCount = Math.max(0, summary.value.ruleCount - 1);
    if (removed.state === QualityRuleState.ENABLED) {
      summary.value.enabledRuleCount = Math.max(
        0,
        summary.value.enabledRuleCount - 1
      );
    }
    if (removed.state === QualityRuleState.PAUSED) {
      summary.value.pausedRuleCount = Math.max(
        0,
        summary.value.pausedRuleCount - 1
      );
    }
  }
  closeCheck();
}

/**
 * A publish or a settings save returns the contract it produced; the strip shows
 * that response rather than re-reading, so the version and drift counts are the
 * server's own.
 */
function onContractChanged(next: QualityContract) {
  contract.value = next;
  contractError.value = "";
}

function onDraftRejected(name: string) {
  suggestions.value = suggestions.value.filter((item) => item.name !== name);
  newNames.value.delete(name);
  summary.value.pendingDraftCount = Math.max(
    0,
    summary.value.pendingDraftCount - 1
  );
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

// A query-opened check waits for the rules it names; watching the loading flag
// re-runs the resolution when the workspace arrives.
watch([() => route.query.check, rules, loading], syncCheckFromQuery, {
  immediate: true,
});

/**
 * The DQ directory's start-monitoring picker lands here with `add=1`: open the
 * check dialog as soon as the (possibly empty) workspace is ready, then consume
 * the flag so a refresh does not reopen it.
 */
function syncAddFromQuery() {
  if (loading.value || route.query.add !== "1") {
    return;
  }
  if (route.query.check !== undefined) {
    return;
  }
  const query = { ...route.query };
  delete query.add;
  void router.replace({ query });
  openComposer();
}

watch([() => route.query.add, loading], syncAddFromQuery, { immediate: true });
</script>
