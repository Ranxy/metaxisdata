<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.rulesTitle')"
      :description="t('dataQuality.rulesDescription')"
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
        <Button
          v-if="canCreate"
          @click="createRule"
        >
          <Plus class="mr-2 h-4 w-4" />
          {{ t("dataQuality.newRule") }}
        </Button>
      </template>
    </PageHeader>

    <Card>
      <!-- One filter row: a label over each control, all sharing a baseline, like
           the application-SQL list. Typing or picking does not re-query; Apply
           does. The target scope narrows by a metadata subtree (instance →
           database → schema → table/view) and is applied with the rest. -->
      <div class="flex flex-wrap items-end gap-3 border-b p-4">
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
             the members. Without users.list it cannot populate, so callers who
             lack the permission get no control rather than an empty one. -->
        <div
          v-if="canListUsers"
          class="flex min-w-48 flex-1 flex-col gap-1.5 sm:max-w-64 sm:flex-none"
        >
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
              : t('dataQuality.noRules')
          "
          :description="
            hasActiveFilters ? '' : t('dataQuality.noRulesHint')
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
            v-else-if="canCreate"
            @click="createRule"
          >
            <Plus class="mr-2 h-4 w-4" />
            {{ t("dataQuality.newRule") }}
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
                  {{ t("dataQuality.lastResult") }}
                </TableHead>
                <TableHead class="hidden whitespace-nowrap md:table-cell">
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
                    <!-- The title is the way into the rule's own page; editing
                         is the pencil action, so a reader-only caller still gets
                         the destination. -->
                    <RouterLink
                      :to="ruleRoute(rule)"
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
                    class="max-w-[14rem] truncate font-mono text-xs text-muted-foreground md:max-w-md"
                    :title="targetTitle(rule)"
                  >
                    {{ targetTitle(rule) }}
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
                    <span class="text-xs text-muted-foreground">
                      {{ formatRelativeTime(rule.updateTime, locale) }}
                    </span>
                  </div>
                </TableCell>
                <TableCell class="hidden lg:table-cell">
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
                <TableCell class="hidden md:table-cell">
                  <Badge
                    v-if="rule.lastResult"
                    :variant="qualityStatusVariant(rule.lastResult.status)"
                  >
                    {{ t(qualityStatusKey(rule.lastResult.status)) }}
                  </Badge>
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
                  class="hidden whitespace-nowrap text-muted-foreground md:table-cell"
                  :title="formatDateTime(rule.updateTime, locale)"
                >
                  {{ formatRelativeTime(rule.updateTime, locale) }}
                </TableCell>
                <TableCell class="text-right">
                  <div class="flex items-center justify-end gap-1">
                    <Button
                      v-if="canUpdate && rule.state !== QualityRuleState.ARCHIVED"
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :title="t('dataQuality.editRule')"
                      :aria-label="t('dataQuality.editRule')"
                      @click="editRule(rule)"
                    >
                      <Pencil class="h-4 w-4" />
                    </Button>
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
                      v-if="canUpdate && rule.state !== QualityRuleState.ARCHIVED"
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :title="
                        rule.state === QualityRuleState.ENABLED
                          ? t('dataQuality.pauseRule')
                          : t('dataQuality.resumeRule')
                      "
                      :aria-label="
                        rule.state === QualityRuleState.ENABLED
                          ? t('dataQuality.pauseRule')
                          : t('dataQuality.resumeRule')
                      "
                      @click="toggleRule(rule)"
                    >
                      <Pause
                        v-if="rule.state === QualityRuleState.ENABLED"
                        class="h-4 w-4"
                      />
                      <Play
                        v-else
                        class="h-4 w-4"
                      />
                    </Button>
                    <Button
                      v-if="canDelete && rule.state !== QualityRuleState.ARCHIVED"
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8 text-muted-foreground hover:text-destructive"
                      :title="t('common.delete')"
                      :aria-label="t('common.delete')"
                      @click="askDelete(rule)"
                    >
                      <Trash2 class="h-4 w-4" />
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

    <ConfirmDeleteDialog
      v-model="deleteOpen"
      :title="t('dataQuality.deleteRuleTitle')"
      :message="t('dataQuality.deleteRuleMessage')"
      :item-name="deleting?.title ?? ''"
      :loading="saving"
      @confirm="confirmDelete"
    />
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import {
  Pause,
  Pencil,
  Play,
  Plus,
  RefreshCcw,
  ShieldCheck,
  Trash2,
  Zap,
} from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRouter } from "vue-router";
import {
  deleteQualityRule,
  listQualityRules,
  qualityRuleID,
  runQualityRules,
  updateQualityRule,
} from "@/api/data-quality";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
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
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityDimension,
  type QualityRule,
  QualityRuleSchema,
  QualityRuleState,
  QualitySeverity,
} from "@/types/proto-es/v1/data_quality_service_pb";
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
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

const { t, locale } = useI18n();
const router = useRouter();
const { handleError } = useErrorHandler();
const authStore = useAuthStore();

const canCreate = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.create")
);
const canRun = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.run")
);
const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.update")
);
const canDelete = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.delete")
);
// The owner picker needs users.list to populate; the server also gates the
// owner filter's usefulness on being able to name a user.
const canListUsers = computed(() =>
  authStore.hasPermission("metaxisdata.users.list")
);

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
const severity = ref("-");
const state = ref("-");
const scope = ref<QualityTargetScope>();
const owner = ref("");
const appliedSearch = ref("");
const appliedDimension = ref("-");
const appliedSeverity = ref("-");
const appliedState = ref("-");
const appliedScope = ref<QualityTargetScope>();
const appliedOwner = ref("");

const pageSize = ref(50);
const error = ref("");
const saving = ref(false);
const deleteOpen = ref(false);
const deleting = ref<QualityRule>();

const hasActiveFilters = computed(
  () =>
    appliedSearch.value !== "" ||
    appliedDimension.value !== "-" ||
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
      dimension: enumFromFilter(appliedDimension.value, [
        QualityDimension.UNSPECIFIED,
        QualityDimension.COMPLETENESS,
        QualityDimension.VALIDITY,
        QualityDimension.UNIQUENESS,
        QualityDimension.CONSISTENCY,
        QualityDimension.TIMELINESS,
        QualityDimension.ACCURACY,
      ]),
      severity: enumFromFilter(appliedSeverity.value, [
        QualitySeverity.UNSPECIFIED,
        QualitySeverity.WARNING,
        QualitySeverity.ERROR,
        QualitySeverity.CRITICAL,
      ]),
      state: enumFromFilter(appliedState.value, [
        QualityRuleState.UNSPECIFIED,
        QualityRuleState.ENABLED,
        QualityRuleState.PAUSED,
        QualityRuleState.ARCHIVED,
      ]),
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

/**
 * The Select's value as an enum member. `-` and any value the enum no longer
 * has mean "no filter", so a stale URL or a hand-edited state cannot break the
 * request.
 */
function enumFromFilter<T extends number>(value: string, members: T[]): T {
  const parsed = Number(value);
  return members.includes(parsed as T) ? (parsed as T) : members[0];
}

function applyFilters() {
  appliedSearch.value = search.value.trim();
  appliedDimension.value = dimension.value;
  appliedSeverity.value = severity.value;
  appliedState.value = state.value;
  appliedScope.value = scope.value ? { ...scope.value } : undefined;
  appliedOwner.value = owner.value;
  void resetPage();
}

function clearFilters() {
  search.value = "";
  dimension.value = "-";
  severity.value = "-";
  state.value = "-";
  scope.value = undefined;
  owner.value = "";
  applyFilters();
}

function createRule() {
  void router.push({ name: "DataQualityRuleNew" });
}

function ruleRoute(rule: QualityRule) {
  return {
    name: "DataQualityRuleDetail",
    params: { ruleId: rule.name.split("/") },
  };
}

function editRule(rule: QualityRule) {
  void router.push({
    name: "DataQualityRuleEdit",
    params: { ruleId: rule.name.split("/") },
  });
}

function targetTitle(rule: QualityRule): string {
  const scope = targetScopeLabel(rule.target?.path);
  const path = formatTargetPath(rule.target?.path);
  return scope === "-" ? path : `${scope} · ${path}`;
}

async function runRule(rule: QualityRule) {
  try {
    const runs = await runQualityRules({ rules: [rule.name] });
    notify.success(t("dataQuality.runStarted"));
    const [run] = runs;
    if (run) {
      void router.push({
        name: "DataQualityRunDetail",
        params: { runId: run.name.split("/") },
      });
    }
  } catch (e: unknown) {
    handleError(e, "dataQuality.runError");
  }
}

async function toggleRule(rule: QualityRule) {
  const next =
    rule.state === QualityRuleState.ENABLED
      ? QualityRuleState.PAUSED
      : QualityRuleState.ENABLED;
  try {
    // Send the whole rule but write only the state: a full update would append a
    // revision for an edit nobody made.
    const updated = create(QualityRuleSchema, rule);
    updated.state = next;
    await updateQualityRule(updated, ["state"]);
    notify.success(
      t(
        next === QualityRuleState.ENABLED
          ? "dataQuality.ruleEnabled"
          : "dataQuality.rulePaused"
      )
    );
    await refreshPage();
  } catch (e: unknown) {
    handleError(e, "dataQuality.updateError");
  }
}

function askDelete(rule: QualityRule) {
  deleting.value = rule;
  deleteOpen.value = true;
}

async function confirmDelete() {
  if (!deleting.value) {
    return;
  }
  saving.value = true;
  try {
    await deleteQualityRule(deleting.value.name);
    notify.success(t("dataQuality.ruleDeleted"));
    deleteOpen.value = false;
    deleting.value = undefined;
    await refreshPage();
  } catch (e: unknown) {
    handleError(e, "dataQuality.deleteError");
  } finally {
    saving.value = false;
  }
}

onMounted(() => {
  void resetPage();
});
</script>
