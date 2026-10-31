<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.checks.title')"
      :description="t('dataQuality.checks.description')"
    >
      <template #actions>
        <Button as-child>
          <RouterLink :to="{ name: 'DataQualityTargets', query: { new: '1' } }">
            <Plus class="mr-2 h-4 w-4" />
            {{ t("dataQuality.targets.startMonitoring") }}
          </RouterLink>
        </Button>
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
        <Button
          v-if="canSuggest"
          variant="outline"
          @click="draftWithAi"
        >
          <Sparkles class="mr-2 h-4 w-4" />
          {{ t("dataQuality.llm.draft.openButton") }}
        </Button>
      </template>
    </PageHeader>

    <Card>
      <!-- One filter row: a label over each control, all sharing a baseline, like
           the application-SQL list. Typing or picking does not re-query; Apply
           does. The target scope narrows by a metadata subtree (instance →
           database → schema → table/view) and is applied with the rest. -->
      <div class="flex flex-wrap items-end gap-3 border-b px-4 py-3">
        <div class="flex min-w-48 flex-1 flex-col gap-1.5 sm:max-w-sm sm:flex-none">
          <Label
            for="data-quality-search"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.search") }}
          </Label>
          <Input
            id="data-quality-search"
            v-model="search"
            class="w-full"
            :placeholder="t('dataQuality.searchPlaceholder')"
            @keyup.enter="applyFilters"
          />
        </div>

        <div class="flex min-w-44 flex-1 flex-col gap-1.5 sm:max-w-64 sm:flex-none">
          <Label
            for="data-quality-target-scope"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.targetScope.label") }}
          </Label>
          <TargetScopeFilter
            id="data-quality-target-scope"
            v-model="scope"
          />
        </div>

        <!-- Owner is optional, so the picker carries an "unassigned" row besides
             the members. The directory is loaded once by the page and passed in,
             so the filter costs no request of its own. -->
        <div class="flex min-w-48 flex-1 flex-col gap-1.5 sm:max-w-64 sm:flex-none">
          <Label
            for="data-quality-owner"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.filterOwner") }}
          </Label>
          <UserSelect
            id="data-quality-owner"
            v-model="owner"
            :placeholder="t('dataQuality.allOwners')"
            :any-label="t('dataQuality.allOwners')"
            :no-owner-value="UNASSIGNED_OWNER"
            :no-owner-label="t('dataQuality.unassignedOwner')"
            :clear-label="t('dataQuality.clearOwnerFilter')"
            :preloaded-users="userDirectory"
          />
        </div>

        <div class="flex min-w-36 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-dimension"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.filterDimension") }}
          </Label>
          <Select v-model="dimension">
            <SelectTrigger
              id="data-quality-dimension"
              class="w-full sm:w-40"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.allDimensions") }}
              </SelectItem>
              <SelectItem
                v-for="option in dimensionOptions"
                :key="option.value"
                :value="String(option.value)"
              >
                {{ t(option.key) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex min-w-40 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-rule-type"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.filterRuleType") }}
          </Label>
          <Select v-model="ruleType">
            <SelectTrigger
              id="data-quality-rule-type"
              class="w-full sm:w-48"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.allRuleTypes") }}
              </SelectItem>
              <SelectItem
                v-for="option in EDITABLE_RULE_TYPES"
                :key="option"
                :value="String(option)"
              >
                {{ t(qualityRuleTypeKey(option)) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex min-w-32 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-severity"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.filterSeverity") }}
          </Label>
          <Select v-model="severity">
            <SelectTrigger
              id="data-quality-severity"
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

        <div class="flex min-w-32 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-state"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.filterState") }}
          </Label>
          <Select v-model="state">
            <SelectTrigger
              id="data-quality-state"
              class="w-full sm:w-36"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.allStates") }}
              </SelectItem>
              <SelectItem
                v-for="option in stateOptions"
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
          v-if="rules.length === 0"
          :icon="ShieldCheck"
          :title="
            hasActiveFilters
              ? t('dataQuality.noFilteredRules')
              : t('dataQuality.checks.emptyTitle')
          "
          :description="
            hasActiveFilters ? '' : t('dataQuality.checks.emptyDescription')
          "
        >
          <Button
            v-if="hasActiveFilters"
            variant="outline"
            size="sm"
            @click="clearFilters"
          >
            {{ t("common.filter.clear") }}
          </Button>
          <Button
            v-else
            variant="outline"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualityTargets' }">
              {{ t("dataQuality.targets.title") }}
            </RouterLink>
          </Button>
          <Button
            v-if="!hasActiveFilters && canSuggest"
            variant="outline"
            @click="draftWithAi"
          >
            <Sparkles class="mr-2 h-4 w-4" />
            {{ t("dataQuality.llm.draft.openButton") }}
          </Button>
        </EmptyState>

        <div v-else>
          <Table class="[&_th]:whitespace-nowrap">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("dataQuality.rule") }}</TableHead>
                <TableHead class="hidden lg:table-cell">
                  {{ t("dataQuality.type") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.filterSeverity") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.filterState") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.filterOwner") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.lastResult") }}
                </TableHead>
                <TableHead class="hidden whitespace-nowrap 2xl:table-cell">
                  {{ t("dataQuality.updatedAt") }}
                </TableHead>
                <TableHead class="w-12 text-right md:w-32">
                  <span class="sr-only md:not-sr-only">
                    {{ t("common.actions") }}
                  </span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="rule in rules"
                :key="rule.name"
              >
                <TableCell>
                  <div class="flex max-w-[14rem] min-w-0 items-center gap-2 md:max-w-md">
                    <!-- The title is the way into the check's target workspace,
                         with this check selected there. -->
                    <RouterLink
                      :to="ruleTargetRoute(rule)"
                      class="truncate text-left font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                      :title="rule.title"
                    >
                      {{ rule.title || qualityRuleID(rule.name) }}
                    </RouterLink>
                    <Badge
                      v-if="rule.orphanedReason"
                      variant="danger"
                      :title="rule.orphanedReason"
                    >
                      {{ t("dataQuality.orphaned") }}
                    </Badge>
                    <Badge
                      v-else-if="rule.targetSchemaChanged"
                      variant="warning"
                    >
                      {{ t("dataQuality.targetChanged") }}
                    </Badge>
                  </div>
                  <div
                    class="max-w-[14rem] truncate font-mono text-xs text-muted-foreground"
                    :title="targetTitle(rule)"
                  >
                    <span class="sr-only">{{ t("dataQuality.ruleTarget") }}: </span>
                    <RouterLink
                      :to="targetRoute(rule)"
                      class="hover:underline"
                    >
                      {{ targetTitle(rule) }}
                    </RouterLink>
                  </div>
                  <!-- Below md the type, severity, state, last result and
                       updated-at columns collapse in here, so the table never
                       grows a horizontal scrollbar on a phone. -->
                  <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 md:hidden">
                    <Badge variant="secondary">
                      {{ t(qualityRuleTypeKey(rule.ruleType)) }}
                    </Badge>
                    <Badge :variant="qualitySeverityVariant(rule.severity)">
                      {{ t(qualitySeverityKey(rule.severity)) }}
                    </Badge>
                    <Badge :variant="qualityRuleStateVariant(rule.state)">
                      {{ t(qualityRuleStateKey(rule.state)) }}
                    </Badge>
                    <Badge
                      v-if="rule.lastResult"
                      :variant="qualityStatusVariant(rule.lastResult.status)"
                    >
                      {{ t(qualityStatusKey(rule.lastResult.status)) }}
                    </Badge>
                    <span
                      v-else
                      class="text-xs text-muted-foreground"
                    >
                      {{ t("dataQuality.noResultYet") }}
                    </span>
                    <span
                      class="max-w-[12rem] truncate text-xs text-muted-foreground"
                      :title="rule.owner || undefined"
                    >
                      {{ ownerName(rule) }}
                    </span>
                    <span class="text-xs text-muted-foreground">
                      {{ formatRelativeTime(rule.updateTime, locale) }}
                    </span>
                  </div>
                </TableCell>
                <TableCell class="hidden whitespace-nowrap lg:table-cell">
                  {{ t(qualityRuleTypeKey(rule.ruleType)) }}
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  <Badge :variant="qualitySeverityVariant(rule.severity)">
                    {{ t(qualitySeverityKey(rule.severity)) }}
                  </Badge>
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  <Badge :variant="qualityRuleStateVariant(rule.state)">
                    {{ t(qualityRuleStateKey(rule.state)) }}
                  </Badge>
                </TableCell>
                <TableCell
                  class="hidden md:table-cell"
                  :title="rule.owner || undefined"
                >
                  <span
                    class="whitespace-nowrap"
                    :class="{ 'text-muted-foreground': !rule.owner }"
                  >
                    {{ ownerName(rule) }}
                  </span>
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  <template v-if="rule.lastResult">
                    <RouterLink
                      v-if="canViewExecution && rule.lastResult.execution"
                      :to="executionRoute(rule.lastResult.execution)"
                      :title="t('dataQuality.viewExecution')"
                      class="inline-block hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
                    >
                      <Badge :variant="qualityStatusVariant(rule.lastResult.status)">
                        {{ t(qualityStatusKey(rule.lastResult.status)) }}
                      </Badge>
                    </RouterLink>
                    <Badge
                      v-else
                      :variant="qualityStatusVariant(rule.lastResult.status)"
                    >
                      {{ t(qualityStatusKey(rule.lastResult.status)) }}
                    </Badge>
                  </template>
                  <span
                    v-else
                    class="text-sm text-muted-foreground"
                  >
                    {{ t("dataQuality.noResultYet") }}
                  </span>
                  <div
                    v-if="rule.lastResult"
                    class="mt-1 whitespace-nowrap text-xs text-muted-foreground"
                    :title="formatDateTime(rule.lastResult.finishTime, locale)"
                  >
                    {{ formatRelativeTime(rule.lastResult.finishTime, locale) }}
                  </div>
                </TableCell>
                <TableCell
                  class="hidden whitespace-nowrap text-muted-foreground 2xl:table-cell"
                  :title="formatDateTime(rule.updateTime, locale)"
                >
                  {{ formatRelativeTime(rule.updateTime, locale) }}
                </TableCell>
                <TableCell class="text-right">
                  <div class="flex items-center justify-end gap-1">
                    <Button
                      v-if="canRun"
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :disabled="rule.state === QualityRuleState.ARCHIVED"
                      :title="t('dataQuality.runNow')"
                      :aria-label="t('dataQuality.runNow')"
                      @click="runRule(rule)"
                    >
                      <Zap class="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      as-child
                    >
                      <RouterLink
                        :to="targetRoute(rule)"
                        :title="t('dataQuality.checks.openTarget')"
                        :aria-label="t('dataQuality.checks.openTarget')"
                      >
                        <ExternalLink class="h-4 w-4" />
                      </RouterLink>
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>

          <!-- The pager footer keeps the card's content inset. -->
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
import {
  ExternalLink,
  Plus,
  RefreshCcw,
  ShieldCheck,
  Sparkles,
  Zap,
} from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { type LocationQuery, RouterLink, useRouter } from "vue-router";
import {
  listQualityRules,
  qualityRuleID,
  runQualityRules,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import UserSelect from "@/components/common/UserSelect.vue";
import TargetScopeFilter from "@/components/data-quality/TargetScopeFilter.vue";
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
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { useUrlFilters } from "@/composables/useUrlFilters";
import { useUserDirectory } from "@/composables/useUserDirectory";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityDimension,
  type QualityRule,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  formatTargetPath,
  type QualityTargetScope,
  qualityRuleStateKey,
  qualityRuleStateVariant,
  qualityRuleTypeKey,
  qualitySeverityKey,
  qualitySeverityVariant,
  qualityStatusKey,
  qualityStatusVariant,
  targetScopeLabel,
} from "@/utils/dataQuality";
import { EDITABLE_RULE_TYPES } from "@/utils/dataQualityForm";
import { scopeFromQuery, writeScopeToQuery } from "@/utils/dataQualityTarget";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";
import {
  enumFilterValue,
  enumFromFilter,
  queryString,
  readFlag,
} from "@/utils/queryFilters";

/**
 * The cross-target check directory: every check the workspace holds, for audit
 * and search. It is read-only — a check is authored inside its target's
 * contract workspace — so a row names its target and links there; the only
 * action besides "run now" is opening that workspace.
 */

const { t, locale } = useI18n();
const router = useRouter();
const { handleError } = useErrorHandler();
const authStore = useAuthStore();

const canSuggest = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.suggest")
);
const canRun = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.run")
);
const canViewExecution = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.executions.get")
);
// The directory feeds both the owner column and the filter picker, so the page
// loads it once; the picker receives it and issues no request of its own.
const {
  users: userDirectory,
  emailOf,
  load: loadUserDirectory,
} = useUserDirectory();

/**
 * The owner picker's "unassigned" choice. It is a page-local value, never sent:
 * the request boundary turns it into `ownerUnassigned: true` with an empty
 * owner.
 */
const UNASSIGNED_OWNER = "__unassigned__";

// The filter controls hold strings for the Select; the enums are built at the
// request boundary, where they belong. `-` is the "any" item.
const search = ref("");
const dimension = ref("-");
const ruleType = ref("-");
const severity = ref("-");
const state = ref("-");
const scope = ref<QualityTargetScope>();
const owner = ref("");
const appliedSearch = ref("");
const appliedDimension = ref("-");
const appliedRuleType = ref("-");
const appliedSeverity = ref("-");
const appliedState = ref("-");
const appliedScope = ref<QualityTargetScope>();
const appliedOwner = ref("");

const pageSize = ref(50);
const error = ref("");

const hasActiveFilters = computed(
  () =>
    appliedSearch.value !== "" ||
    appliedDimension.value !== "-" ||
    appliedRuleType.value !== "-" ||
    appliedSeverity.value !== "-" ||
    appliedState.value !== "-" ||
    appliedScope.value !== undefined ||
    appliedOwner.value !== ""
);

const dimensionOptions: Array<{
  value: QualityDimension;
  key: string;
}> = [
  {
    value: QualityDimension.COMPLETENESS,
    key: "dataQuality.dimension.completeness",
  },
  { value: QualityDimension.VALIDITY, key: "dataQuality.dimension.validity" },
  {
    value: QualityDimension.UNIQUENESS,
    key: "dataQuality.dimension.uniqueness",
  },
  {
    value: QualityDimension.CONSISTENCY,
    key: "dataQuality.dimension.consistency",
  },
  {
    value: QualityDimension.TIMELINESS,
    key: "dataQuality.dimension.timeliness",
  },
  { value: QualityDimension.ACCURACY, key: "dataQuality.dimension.accuracy" },
];

const severityOptions: Array<{
  value: QualitySeverity;
  key: string;
}> = [
  { value: QualitySeverity.WARNING, key: "dataQuality.severity.warning" },
  { value: QualitySeverity.ERROR, key: "dataQuality.severity.error" },
  { value: QualitySeverity.CRITICAL, key: "dataQuality.severity.critical" },
];

const stateOptions: Array<{
  value: QualityRuleState;
  key: string;
}> = [
  { value: QualityRuleState.ENABLED, key: "dataQuality.ruleState.enabled" },
  { value: QualityRuleState.PAUSED, key: "dataQuality.ruleState.paused" },
  { value: QualityRuleState.ARCHIVED, key: "dataQuality.ruleState.archived" },
];

/** The rule types the editor can create; UNSPECIFIED is the "any" member. */
const RULE_TYPE_FILTER_VALUES: QualityRuleType[] = [
  QualityRuleType.UNSPECIFIED,
  ...EDITABLE_RULE_TYPES,
];

/** Every member each enum filter accepts, UNSPECIFIED included as "any". */
const DIMENSION_FILTER_VALUES: QualityDimension[] = [
  QualityDimension.UNSPECIFIED,
  ...dimensionOptions.map((option) => option.value),
];
const SEVERITY_FILTER_VALUES: QualitySeverity[] = [
  QualitySeverity.UNSPECIFIED,
  ...severityOptions.map((option) => option.value),
];
const STATE_FILTER_VALUES: QualityRuleState[] = [
  QualityRuleState.UNSPECIFIED,
  ...stateOptions.map((option) => option.value),
];

/** The query parameter each filter travels under. */
const FILTER_QUERY_KEYS = [
  "q",
  "instance",
  "database",
  "schema",
  "table",
  "owner",
  "ownerUnassigned",
  "dimension",
  "ruleType",
  "severity",
  "state",
] as const;

/** The filters as one value, so a URL and the applied set share a shape. */
interface RuleFilters {
  search: string;
  scope: QualityTargetScope | undefined;
  owner: string;
  dimension: string;
  ruleType: string;
  severity: string;
  state: string;
}

/** The filters a URL carries: reload, shared link and back/forward. */
function parseFilters(query: LocationQuery): RuleFilters {
  const owner = queryString(query.owner).trim();
  return {
    search: queryString(query.q).trim(),
    scope: scopeFromQuery(query),
    // A named member wins: the server rejects it combined with the flag.
    owner: owner || (readFlag(query.ownerUnassigned) ? UNASSIGNED_OWNER : ""),
    dimension: enumFilterValue(query.dimension, DIMENSION_FILTER_VALUES),
    ruleType: enumFilterValue(query.ruleType, RULE_TYPE_FILTER_VALUES),
    severity: enumFilterValue(query.severity, SEVERITY_FILTER_VALUES),
    state: enumFilterValue(query.state, STATE_FILTER_VALUES),
  };
}

/** Only non-default values are written, so the URL stays readable. */
function serializeFilters(filters: RuleFilters): Record<string, string> {
  const owned: Record<string, string> = {};
  if (filters.search) {
    owned.q = filters.search;
  }
  writeScopeToQuery(owned, filters.scope);
  if (filters.owner === UNASSIGNED_OWNER) {
    owned.ownerUnassigned = "1";
  } else if (filters.owner) {
    owned.owner = filters.owner;
  }
  if (filters.dimension !== "-") {
    owned.dimension = filters.dimension;
  }
  if (filters.ruleType !== "-") {
    owned.ruleType = filters.ruleType;
  }
  if (filters.severity !== "-") {
    owned.severity = filters.severity;
  }
  if (filters.state !== "-") {
    owned.state = filters.state;
  }
  return owned;
}

/** The filters the request currently applies. */
function appliedFilters(): RuleFilters {
  return {
    search: appliedSearch.value,
    scope: appliedScope.value,
    owner: appliedOwner.value,
    dimension: appliedDimension.value,
    ruleType: appliedRuleType.value,
    severity: appliedSeverity.value,
    state: appliedState.value,
  };
}

const urlFilters = useUrlFilters<RuleFilters>({
  keys: FILTER_QUERY_KEYS,
  parse: parseFilters,
  serialize: serializeFilters,
});

// Read the URL before the first request: a reloaded or shared link queries what
// it carries instead of flashing the unfiltered directory first. Both the
// controls and the applied set start there, so Apply has nothing pending.
const initialFilters = urlFilters.read();
search.value = initialFilters.search;
dimension.value = initialFilters.dimension;
ruleType.value = initialFilters.ruleType;
severity.value = initialFilters.severity;
state.value = initialFilters.state;
scope.value = initialFilters.scope;
owner.value = initialFilters.owner;
appliedSearch.value = initialFilters.search;
appliedDimension.value = initialFilters.dimension;
appliedRuleType.value = initialFilters.ruleType;
appliedSeverity.value = initialFilters.severity;
appliedState.value = initialFilters.state;
appliedScope.value = initialFilters.scope
  ? { ...initialFilters.scope }
  : undefined;
appliedOwner.value = initialFilters.owner;

const {
  items: rules,
  isLoading,
  hasNext,
  hasPrevious,
  reset: resetPage,
  refresh: refreshPage,
  goNext: goNextPage,
  goPrevious: goPreviousPage,
} = usePagedFetch<QualityRule>({
  fetchPage: async (pageToken, signal) => {
    error.value = "";
    return await listQualityRules({
      pageSize: pageSize.value,
      pageToken,
      search: appliedSearch.value,
      instance: appliedScope.value?.instance,
      database: appliedScope.value?.database,
      schema: appliedScope.value?.schema,
      table: appliedScope.value?.table,
      dimension: enumFromFilter(
        appliedDimension.value,
        DIMENSION_FILTER_VALUES
      ),
      ruleType: enumFromFilter(appliedRuleType.value, RULE_TYPE_FILTER_VALUES),
      severity: enumFromFilter(appliedSeverity.value, SEVERITY_FILTER_VALUES),
      state: enumFromFilter(appliedState.value, STATE_FILTER_VALUES),
      // The sentinel cannot be sent: an empty owner plus `owner_unassigned` is
      // how the server reads "only rules nobody owns"; the two fields never both
      // apply.
      owner: appliedOwner.value === UNASSIGNED_OWNER ? "" : appliedOwner.value,
      ownerUnassigned: appliedOwner.value === UNASSIGNED_OWNER,
      signal,
    });
  },
  onError: (e) => {
    error.value = handleError(e, "dataQuality.fetchRulesError");
  },
});

// A cursor belongs to the query that produced it, so a new page size restarts
// the walk at the first page.
watch(pageSize, () => void resetPage());

// A reload, a shared link or back/forward changes the query from outside; the
// page adopts those filters. The replace this composable performs matches the
// applied set, so it is not adopted and cannot cause a second request.
urlFilters.watchExternal((next) => {
  if (urlFilters.isCurrent(appliedFilters())) {
    return;
  }
  search.value = next.search;
  dimension.value = next.dimension;
  ruleType.value = next.ruleType;
  severity.value = next.severity;
  state.value = next.state;
  scope.value = next.scope ? { ...next.scope } : undefined;
  owner.value = next.owner;
  appliedSearch.value = next.search;
  appliedDimension.value = next.dimension;
  appliedRuleType.value = next.ruleType;
  appliedSeverity.value = next.severity;
  appliedState.value = next.state;
  appliedScope.value = next.scope ? { ...next.scope } : undefined;
  appliedOwner.value = next.owner;
  void resetPage();
});

function applyFilters() {
  appliedSearch.value = search.value.trim();
  appliedDimension.value = dimension.value;
  appliedRuleType.value = ruleType.value;
  appliedSeverity.value = severity.value;
  appliedState.value = state.value;
  appliedScope.value = scope.value ? { ...scope.value } : undefined;
  appliedOwner.value = owner.value;
  // The URL is written first: the watcher then sees the applied set already
  // agrees with it, so adopting the route costs no second request.
  urlFilters.write(appliedFilters());
  void resetPage();
}

function clearFilters() {
  search.value = "";
  dimension.value = "-";
  ruleType.value = "-";
  severity.value = "-";
  state.value = "-";
  scope.value = undefined;
  owner.value = "";
  applyFilters();
}

/**
 * AI drafting lives in the suggestion center; this is a discovery path, not a
 * second implementation. `?draft=1` opens the workbench on arrival.
 */
function draftWithAi() {
  void router.push({
    name: "DataQualitySuggestions",
    query: { draft: "1" },
  });
}

/**
 * The target workspace route: the check's own target, opening the drawer on the
 * check when one is named. A check lives inside its target, so the audit
 * directory links there rather than to a standalone rule page.
 */
function targetWorkspaceRoute(target: QualityRule["target"], check = "") {
  const guid = target?.metaGuid ?? "";
  const metaType = String(target?.metaType ?? MetaType.UNSPECIFIED);
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(guid) },
    query: check ? { metaType, check } : { metaType },
  };
}

/** The check's own destination: its target's workspace with the check open. */
function ruleTargetRoute(rule: QualityRule) {
  return targetWorkspaceRoute(rule.target, rule.name);
}

/** The target's destination, without selecting a check. */
function targetRoute(rule: QualityRule) {
  return targetWorkspaceRoute(rule.target);
}

function executionRoute(name: string) {
  return {
    name: "DataQualityExecutionDetail",
    params: { executionId: name.split("/") },
  };
}

// The table's row subline is width-capped, so the object goes first: truncating
// the instance/database is recoverable from the tooltip, truncating the table name
// is not.
function targetTitle(rule: QualityRule): string {
  const scope = targetScopeLabel(rule.target?.path);
  const path = formatTargetPath(rule.target?.path);
  return path === "-" ? scope : `${path} · ${scope}`;
}

/** The owner's email from the directory (raw name if unknown), or "unassigned". */
function ownerName(rule: QualityRule): string {
  return rule.owner ? emailOf(rule.owner) : t("dataQuality.unassignedOwner");
}

async function runRule(rule: QualityRule) {
  try {
    const executions = await runQualityRules({ rules: [rule.name] });
    notify.success(t("dataQuality.executionStarted"));
    // One rule yields one execution, which the detail page polls; a service
    // that planned several lands on the executions list filtered to the rule.
    if (executions.length === 1) {
      const [created] = executions;
      if (created) {
        void router.push(executionRoute(created.name));
        return;
      }
    }
    if (executions.length > 1) {
      void router.push({
        name: "DataQualityExecutions",
        query: { rule: rule.name },
      });
    }
  } catch (e: unknown) {
    handleError(e, "dataQuality.executionError");
  }
}

onMounted(() => {
  void resetPage();
  void loadUserDirectory();
});
</script>
