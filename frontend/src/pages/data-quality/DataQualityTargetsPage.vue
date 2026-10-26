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
          <RouterLink :to="{ name: 'DataQualityRules' }">
            <ListChecks class="mr-2 h-4 w-4" />
            {{ t("dataQuality.targets.allRules") }}
          </RouterLink>
        </Button>
        <Button
          v-if="canCreate"
          as-child
        >
          <RouterLink :to="{ name: 'DataQualityRuleNew' }">
            <Plus class="mr-2 h-4 w-4" />
            {{ t("dataQuality.targets.newRule") }}
          </RouterLink>
        </Button>
      </template>
    </PageHeader>

    <Card>
      <!-- One toolbar: the search and the three attention filters narrow the
           already-loaded rows on the client, so nothing here re-queries. The
           target rows are cheap to filter, and a search that waits for Apply
           would make "which target is failing?" feel slower than it is. -->
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
          <Select v-model="order">
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

        <!-- Nothing to group: no rule exists yet, so there is no target. The
             next step is creating the first rule, or reading the flat list when
             the caller may not create one. -->
        <EmptyState
          v-if="summaries.length === 0"
          :icon="ShieldCheck"
          :title="t('dataQuality.targets.emptyTitle')"
          :description="t('dataQuality.targets.emptyDescription')"
        >
          <Button
            v-if="canCreate"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualityRuleNew' }">
              <Plus class="mr-2 h-4 w-4" />
              {{ t("dataQuality.targets.emptyAction") }}
            </RouterLink>
          </Button>
          <Button
            v-else
            variant="outline"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualityRules' }">
              {{ t("dataQuality.targets.allRules") }}
            </RouterLink>
          </Button>
        </EmptyState>

        <!-- Targets exist; the search and filters hide all of them. Clearing is
             the only action that can lead anywhere. -->
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
              :key="summary.guid"
            >
              <TableCell>
                <div class="flex max-w-[14rem] min-w-0 items-center gap-2 md:max-w-md">
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
                    {{ metaTypeSingularLabel(summary.metaType, t) }}
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
                    v-if="summary.worstStatus !== undefined"
                    :variant="qualityStatusVariant(summary.worstStatus)"
                  >
                    {{ t(qualityStatusKey(summary.worstStatus)) }}
                  </Badge>
                  <span
                    v-if="summary.lastRunTime"
                    :title="formatDateTime(summary.lastRunTime, locale)"
                  >
                    {{ t("dataQuality.targets.columnLastRun") }}
                    {{ formatRelativeTime(summary.lastRunTime, locale) }}
                  </span>
                  <span v-else>{{ t("dataQuality.targets.neverRun") }}</span>
                  <span v-if="summary.columns.length > 0">
                    {{ t("dataQuality.targets.columnColumns") }}
                    {{ summary.columns.length }}
                  </span>
                  <span v-if="summary.openIssueCount > 0">
                    {{ t("dataQuality.targets.columnIssues") }}
                    {{ summary.openIssueCount }}
                  </span>
                  <span v-if="summary.pendingDraftCount > 0">
                    {{ t("dataQuality.targets.pendingDraftsCount", { count: summary.pendingDraftCount }) }}
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
                  v-if="summary.pausedCount > 0"
                  class="text-xs tabular-nums text-muted-foreground"
                  :title="t('dataQuality.ruleState.enabled')"
                >
                  {{ summary.enabledCount }} / {{ summary.ruleCount }}
                </div>
              </TableCell>
              <TableCell
                class="hidden tabular-nums lg:table-cell"
                :title="summary.columns.length > 0 ? summary.columns.join(', ') : undefined"
              >
                {{ summary.columns.length > 0 ? summary.columns.length : "-" }}
              </TableCell>
              <TableCell class="hidden md:table-cell">
                <template v-if="summary.lastRunTime">
                  <Badge
                    v-if="summary.worstStatus !== undefined"
                    :variant="qualityStatusVariant(summary.worstStatus)"
                  >
                    {{ t(qualityStatusKey(summary.worstStatus)) }}
                  </Badge>
                  <div
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
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import {
  listAllQualityRules,
  listQualityIssues,
  listQualitySuggestions,
} from "@/api/data-quality";
import { listAll } from "@/api/list";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
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
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityIssueStatus,
  QualityStatus,
  type QualitySuggestion,
  QualitySuggestionStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityStatusKey, qualityStatusVariant } from "@/utils/dataQuality";
import {
  type QualityTargetOrder,
  type QualityTargetSummary,
  sortQualityTargets,
  summarizeQualityTargets,
  targetDisplayName,
  targetFullPath,
  targetMatchesSearch,
  targetScope,
} from "@/utils/dataQualityTarget";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";
import { metaTypeSingularLabel } from "@/utils/metaType";

/**
 * The quality directory: every target with rules, one row per table or view.
 *
 * The rule list is the source of truth for which targets exist — a target with
 * no rules is not a quality target — so the page reads every rule once and lets
 * the pure summarizer group them. Issues and pending drafts only decorate rows
 * that already exist; they never add one.
 */

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const canCreate = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.create")
);

/** The three "needs attention" filters, all off by default. */
type TargetToggle = "failing" | "issues" | "drafts";

const summaries = ref<QualityTargetSummary[]>([]);
const isLoading = ref(true);
const error = ref("");
const search = ref("");
const toggles = reactive<Record<TargetToggle, boolean>>({
  failing: false,
  issues: false,
  drafts: false,
});
const order = ref("attention");

/** The orders the summary sort knows; anything else falls back to attention. */
const ORDER_VALUES: readonly QualityTargetOrder[] = [
  "attention",
  "name",
  "rules",
  "lastRun",
];

const sortOptions = computed<
  Array<{ value: QualityTargetOrder; label: string }>
>(() => [
  {
    value: "attention",
    label: t("dataQuality.targets.sortAttention"),
  },
  { value: "name", label: t("dataQuality.targets.sortName") },
  { value: "rules", label: t("dataQuality.targets.sortRules") },
  { value: "lastRun", label: t("dataQuality.targets.sortLastRun") },
]);

const toggleOptions = computed<Array<{ key: TargetToggle; label: string }>>(
  () => [
    { key: "failing", label: t("dataQuality.targets.onlyFailing") },
    { key: "issues", label: t("dataQuality.targets.onlyIssues") },
    { key: "drafts", label: t("dataQuality.targets.onlyDrafts") },
  ]
);

const selectedOrder = computed<QualityTargetOrder>(() =>
  ORDER_VALUES.includes(order.value as QualityTargetOrder)
    ? (order.value as QualityTargetOrder)
    : "attention"
);

const visible = computed(() =>
  sortQualityTargets(
    summaries.value.filter(
      (summary) =>
        targetMatchesSearch(summary, search.value) && matchesToggles(summary)
    ),
    selectedOrder.value
  )
);

/** Whether a target passes the three attention filters; all three must hold. */
function matchesToggles(summary: QualityTargetSummary): boolean {
  if (
    toggles.failing &&
    summary.worstStatus !== QualityStatus.FAIL &&
    summary.worstStatus !== QualityStatus.ERROR
  ) {
    return false;
  }
  if (toggles.issues && summary.openIssueCount === 0) {
    return false;
  }
  if (toggles.drafts && summary.pendingDraftCount === 0) {
    return false;
  }
  return true;
}

function toggle(key: TargetToggle) {
  toggles[key] = !toggles[key];
}

function clearFilters() {
  search.value = "";
  toggles.failing = false;
  toggles.issues = false;
  toggles.drafts = false;
}

/** A target's workspace: the GUID as repeated path segments plus its meta type. */
function workspaceRoute(summary: QualityTargetSummary) {
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(summary.guid) },
    query: { metaType: String(summary.metaType) },
  };
}

async function load() {
  isLoading.value = true;
  error.value = "";
  try {
    // The three reads start together. Rules and issues decide the page: a
    // failure in either is a failure of the directory, because an empty page
    // would claim there are no targets. Pending drafts only annotate rows, so
    // that block degrades to zero counts instead of failing the page.
    const pendingDrafts = listAll((pageToken) =>
      listQualitySuggestions({
        status: QualitySuggestionStatus.PENDING,
        pageSize: 1000,
        pageToken,
      })
    ).catch((): QualitySuggestion[] => []);
    // Archived rules are excluded by the server unless explicitly requested.
    // Only open and acknowledged issues count. Reading them by status (two
    // pages) avoids walking the resolved rows that the 90-day ledger keeps.
    const openIssues = Promise.all([
      listAll((pageToken) =>
        listQualityIssues({
          status: QualityIssueStatus.OPEN,
          pageSize: 1000,
          pageToken,
        })
      ),
      listAll((pageToken) =>
        listQualityIssues({
          status: QualityIssueStatus.ACKNOWLEDGED,
          pageSize: 1000,
          pageToken,
        })
      ),
    ]).then(([open, acknowledged]) => [...open, ...acknowledged]);
    const [rules, issues, suggestions] = await Promise.all([
      listAllQualityRules({ pageSize: 1000 }),
      openIssues,
      pendingDrafts,
    ]);
    summaries.value = summarizeQualityTargets(rules, issues, suggestions);
  } catch (e: unknown) {
    error.value = formatError(e, "dataQuality.targets.loadFailed");
    summaries.value = [];
  } finally {
    isLoading.value = false;
  }
}

onMounted(load);
</script>
