<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.suggestions.title')"
      :description="t('dataQuality.suggestions.description')"
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
          :disabled="!llmAvailability.available"
          :title="
            llmAvailability.available
              ? ''
              : t(llmAvailability.reasonKey ?? 'dataQuality.llm.disabledHint')
          "
          @click="openWorkbench"
        >
          <Sparkles class="mr-2 h-4 w-4" />
          {{ t("dataQuality.llm.draft.openButton") }}
        </Button>
      </template>
    </PageHeader>

    <SuggestionJobsPanel
      :refresh-key="jobsRefreshKey"
      @settled="onJobsSettled"
    />

    <Card>
      <div class="flex flex-wrap items-end gap-3 border-b p-4">
        <div class="flex min-w-32 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="dq-suggestion-status"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.suggestions.filterStatus") }}
          </Label>
          <Select v-model="status">
            <SelectTrigger
              id="dq-suggestion-status"
              class="w-full sm:w-36"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.suggestions.allStatuses") }}
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
            for="dq-suggestion-kind"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.suggestions.filterKind") }}
          </Label>
          <Select v-model="kind">
            <SelectTrigger
              id="dq-suggestion-kind"
              class="w-full sm:w-36"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.suggestions.allKinds") }}
              </SelectItem>
              <SelectItem
                v-for="option in kindOptions"
                :key="option.value"
                :value="String(option.value)"
              >
                {{ t(option.key) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex min-w-36 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="dq-suggestion-source"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.suggestions.filterSource") }}
          </Label>
          <Select v-model="source">
            <SelectTrigger
              id="dq-suggestion-source"
              class="w-full sm:w-44"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.suggestions.allSources") }}
              </SelectItem>
              <SelectItem
                v-for="option in sourceOptions"
                :key="option.value"
                :value="String(option.value)"
              >
                {{ t(option.key) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex min-w-44 flex-1 flex-col gap-1.5 sm:max-w-64 sm:flex-none">
          <Label
            for="dq-suggestion-target-scope"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.targetScope.label") }}
          </Label>
          <TargetScopeFilter
            id="dq-suggestion-target-scope"
            v-model="scope"
          />
        </div>

        <Button
          variant="outline"
          @click="applyFilters"
        >
          {{ t("common.apply") }}
        </Button>
      </div>

      <!-- The applied filters, removable one at a time: the same path the Apply
           button takes, without reopening every control. -->
      <div
        v-if="activeFilterChips.length > 0"
        class="flex flex-wrap items-center gap-2 border-b px-4 py-2"
      >
        <span class="text-xs text-muted-foreground">
          {{ t("dataQuality.suggestions.activeFilters") }}
        </span>
        <button
          v-for="chip in activeFilterChips"
          :key="chip.key"
          type="button"
          class="inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-xs transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          :title="t('common.removeFilter')"
          @click="removeFilter(chip.key)"
        >
          {{ chip.label }}
          <X class="h-3 w-3" />
        </button>
      </div>

      <PageState
        :loading="isLoading"
        :error="error"
      >
        <template #error>
          <Button
            variant="outline"
            size="sm"
            @click="resetPage"
          >
            {{ t("common.refresh") }}
          </Button>
        </template>

        <EmptyState
          v-if="suggestions.length === 0"
          :icon="Lightbulb"
          :title="emptyTitle"
          :description="emptyDescription"
        >
          <template v-if="hasActiveFilters">
            <Button
              variant="outline"
              size="sm"
              @click="clearFilters"
            >
              {{ t("common.filter.clear") }}
            </Button>
          </template>
          <template v-else>
            <Button
              :disabled="!llmAvailability.available"
              @click="openWorkbench"
            >
              <Sparkles class="mr-2 h-4 w-4" />
              {{ t("dataQuality.llm.draft.openButton") }}
            </Button>
            <Button
              v-if="appliedStatus === String(QualitySuggestionStatus.PENDING)"
              variant="outline"
              @click="showAllStatuses"
            >
              {{ t("dataQuality.suggestions.viewAllStatuses") }}
            </Button>
          </template>
        </EmptyState>

        <div v-else>
          <div
            v-if="selectedSuggestions.length > 0"
            class="sticky top-0 z-10 flex flex-wrap items-center gap-2 border-b bg-card px-4 py-2"
          >
            <span class="text-sm font-medium">
              {{ t("dataQuality.suggestions.selectedCount", { count: selectionCounts.total }) }}
            </span>
            <template v-if="selectionCounts.drafts > 0">
              <Button
                size="sm"
                :disabled="!canCreateRule"
                @click="openBatchCreate(selectedDrafts)"
              >
                {{ t("dataQuality.suggestions.createRules", { count: selectionCounts.drafts }) }}
              </Button>
              <Button
                variant="outline"
                size="sm"
                :disabled="!canDecide"
                @click="openDecision(QualitySuggestionStatus.REJECTED, selectedDrafts)"
              >
                {{ t("dataQuality.suggestions.ignoreDrafts", { count: selectionCounts.drafts }) }}
              </Button>
            </template>
            <template v-if="selectionCounts.labels > 0">
              <Button
                size="sm"
                :disabled="!canDecide"
                @click="openDecision(QualitySuggestionStatus.ACCEPTED, selectedLabels)"
              >
                {{ t("dataQuality.suggestions.acceptLabels", { count: selectionCounts.labels }) }}
              </Button>
              <Button
                variant="outline"
                size="sm"
                :disabled="!canDecide"
                @click="openDecision(QualitySuggestionStatus.REJECTED, selectedLabels)"
              >
                {{ t("dataQuality.suggestions.rejectLabels", { count: selectionCounts.labels }) }}
              </Button>
            </template>
          </div>

          <Table class="[&_th]:whitespace-nowrap">
            <TableHeader>
              <TableRow>
                <TableHead class="w-10">
                  <span class="sr-only">{{ t("common.selectAll") }}</span>
                </TableHead>
                <TableHead>{{ t("dataQuality.suggestions.suggestion") }}</TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.type") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.suggestions.confidence") }}
                </TableHead>
                <TableHead>{{ t("dataQuality.suggestions.status") }}</TableHead>
                <TableHead class="hidden whitespace-nowrap xl:table-cell">
                  {{ t("dataQuality.suggestions.createdAt") }}
                </TableHead>
                <TableHead class="w-12 text-right">
                  <span class="sr-only">{{ t("common.actions") }}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <template
                v-for="group in groupedSuggestions"
                :key="group.targetGuid || 'ungrouped'"
              >
                <TableRow class="bg-muted/40 hover:bg-muted/40">
                  <TableCell class="align-middle">
                    <Checkbox
                      :checked="groupSelectionState(group)"
                      :disabled="selectableIn(group).length === 0"
                      :aria-label="t('dataQuality.suggestions.selectGroup')"
                      @update:checked="(checked: boolean | 'indeterminate') => toggleGroup(group, checked === true)"
                    />
                  </TableCell>
                  <TableCell :colspan="6">
                    <div class="flex flex-wrap items-center gap-2">
                      <button
                        type="button"
                        class="rounded-sm p-0.5 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        :aria-label="
                          collapsedGroups.includes(group.targetGuid)
                            ? t('dataQuality.suggestions.expandGroup')
                            : t('dataQuality.suggestions.collapseGroup')
                        "
                        :aria-expanded="
                          !collapsedGroups.includes(group.targetGuid)
                        "
                        @click="toggleGroupCollapsed(group.targetGuid)"
                      >
                        <ChevronRight
                          class="h-4 w-4 transition-transform"
                          :class="{
                            'rotate-90': !collapsedGroups.includes(group.targetGuid),
                          }"
                        />
                      </button>
                      <Badge variant="outline">
                        {{ metaTypeSingularLabel(groupMetaType(group), t) }}
                      </Badge>
                      <span class="min-w-0 truncate font-mono text-xs font-medium">
                        {{ group.targetLabel }}
                      </span>
                      <span class="text-xs text-muted-foreground">
                        {{ t("dataQuality.suggestions.groupCount", { count: group.suggestions.length }) }}
                      </span>
                    </div>
                  </TableCell>
                </TableRow>
                <template v-if="!collapsedGroups.includes(group.targetGuid)">
                  <TableRow
                    v-for="suggestion in group.suggestions"
                    :key="suggestion.name"
                  >
                    <TableCell class="align-top">
                      <Checkbox
                        v-if="isPendingSuggestion(suggestion)"
                        :checked="selectedNames.includes(suggestion.name)"
                        class="mt-0.5"
                        :aria-label="t('dataQuality.suggestions.selectSuggestion')"
                        @update:checked="(checked: boolean | 'indeterminate') => toggleSuggestion(suggestion.name, checked === true)"
                      />
                    </TableCell>
                    <TableCell class="min-w-56">
                      <!-- Both kinds read the same way: the kind, the subject
                           the model proposed (a rule title, or the column a
                           label belongs to, set in mono), then its reason. -->
                      <div class="flex items-start gap-2">
                        <Badge
                          variant="outline"
                          class="mt-0.5 shrink-0"
                        >
                          {{ t(suggestionKindKey(suggestion.kind)) }}
                        </Badge>
                        <div class="min-w-0 space-y-1">
                          <button
                            type="button"
                            class="line-clamp-1 text-left text-sm font-medium wrap-anywhere hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                            :class="{ 'font-mono': isLabelSuggestion(suggestion) }"
                            :title="suggestionText(suggestion)"
                            @click="inspect(suggestion)"
                          >
                            {{ suggestionText(suggestion) }}
                          </button>
                          <p
                            v-if="suggestionRationale(suggestion)"
                            class="line-clamp-1 text-xs wrap-anywhere text-muted-foreground md:max-xl:hidden"
                            :title="suggestionRationale(suggestion)"
                          >
                            {{ suggestionRationale(suggestion) }}
                          </p>
                        </div>
                      </div>
                      <!-- Below xl the source and time collapse in here. -->
                      <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground xl:hidden">
                        <span>{{ t(suggestionSourceKey(suggestion.source)) }}</span>
                        <span aria-hidden="true">·</span>
                        <span :title="formatDateTime(suggestion.createTime, locale)">
                          {{ formatRelativeTime(suggestion.createTime, locale) }}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell class="hidden align-top md:table-cell">
                      <!-- The subtype both kinds carry, then the severity only
                           a draft has; keeping it out of the confidence cell is
                           what makes the two kinds' rows line up. Below xl the
                           columns are cramped enough that the two may wrap. -->
                      <div class="flex items-center gap-1 max-xl:flex-wrap">
                        <Badge
                          v-if="ruleDraftLabel(suggestion)"
                          variant="secondary"
                        >
                          {{ t(ruleDraftLabel(suggestion) ?? "") }}
                        </Badge>
                        <Badge
                          v-else-if="labelTypeKey(suggestion)"
                          variant="secondary"
                        >
                          {{ t(labelTypeKey(suggestion) ?? "") }}
                        </Badge>
                        <span
                          v-else
                          class="text-muted-foreground"
                        >-</span>
                        <Badge
                          v-if="draftSeverity(suggestion)"
                          :variant="qualitySeverityVariant(draftSeverity(suggestion) ?? QualitySeverity.UNSPECIFIED)"
                        >
                          {{ t(qualitySeverityKey(draftSeverity(suggestion) ?? QualitySeverity.UNSPECIFIED)) }}
                        </Badge>
                      </div>
                    </TableCell>
                    <TableCell class="hidden align-top md:table-cell">
                      <div class="flex items-center gap-2 max-xl:flex-wrap">
                        <div
                          class="h-1.5 w-16 overflow-hidden rounded-full bg-muted"
                          aria-hidden="true"
                        >
                          <div
                            class="h-full rounded-full bg-primary/70"
                            :style="{ width: `${confidencePercent(suggestion)}%` }"
                          />
                        </div>
                        <span class="w-12 text-right text-xs tabular-nums text-muted-foreground">
                          {{ formatConfidence(confidenceOf(suggestion)) }}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell class="align-top">
                      <div class="space-y-1">
                        <Badge
                          :variant="suggestionStatusVariant(suggestion.status)"
                          :title="suggestion.decisionNote"
                        >
                          {{ t(suggestionStatusKey(suggestion.status)) }}
                        </Badge>
                        <!-- A pending proposal re-proposed by a later pass keeps its
                             row and is refreshed in place; the extra line says so
                             without adding a column the other rows would leave empty. -->
                        <div
                          v-if="suggestion.status === QualitySuggestionStatus.PENDING && suggestion.refreshTime"
                          class="text-xs whitespace-nowrap text-muted-foreground"
                          :title="formatDateTime(suggestion.refreshTime, locale)"
                        >
                          {{
                            t("dataQuality.suggestions.refreshedAt", {
                              time: formatRelativeTime(suggestion.refreshTime, locale),
                            })
                          }}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell class="hidden whitespace-nowrap align-top text-muted-foreground xl:table-cell">
                      <span :title="formatDateTime(suggestion.createTime, locale)">
                        {{ formatRelativeTime(suggestion.createTime, locale) }}
                      </span>
                    </TableCell>
                    <TableCell
                      class="text-right align-top"
                      @click.stop
                    >
                      <!-- The draft shortcut sits before the primary action
                           and review closes the cell: with the primary button
                           in the same slot on every row, "Create rule" and
                           "Accept" share one right edge. A draft is adjusted
                           where it will live — inside its target's contract
                           workspace, which lists the target's pending proposals
                           inline — so this opens the target, not an editor. -->
                      <div class="flex items-center justify-end gap-1">
                        <Button
                          v-if="canEditDraft(suggestion)"
                          variant="ghost"
                          size="icon"
                          class="h-8 w-8"
                          :title="t('dataQuality.targets.openWorkspace')"
                          :aria-label="t('dataQuality.targets.openWorkspace')"
                          @click="openTarget(suggestion)"
                        >
                          <ExternalLink class="h-4 w-4 text-muted-foreground" />
                        </Button>
                        <Button
                          v-if="canEditDraft(suggestion)"
                          variant="outline"
                          size="sm"
                          class="min-w-20"
                          @click="openBatchCreate([suggestion])"
                        >
                          {{ t("dataQuality.suggestions.createRule") }}
                        </Button>
                        <Button
                          v-if="isPendingLabel(suggestion) && canDecide"
                          variant="outline"
                          size="sm"
                          class="min-w-20"
                          @click="acceptLabel(suggestion)"
                        >
                          {{ t("dataQuality.suggestions.accept") }}
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          class="h-8 w-8"
                          :title="t('dataQuality.suggestions.review')"
                          :aria-label="t('dataQuality.suggestions.review')"
                          @click="inspect(suggestion)"
                        >
                          <Eye class="h-4 w-4 text-muted-foreground" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                </template>
              </template>
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

    <SuggestionWorkbench
      v-model:open="workbenchOpen"
      :llm-enabled="llmEnabled"
      @submitted="onJobSubmitted"
    />

    <SuggestionDetailDrawer
      v-model:open="drawerOpen"
      :suggestion="selected"
      :target-label="selected ? suggestionTargetLabel(selected.target?.metaGuid ?? '', selectedInstanceTitle(selected)) : ''"
      :can-decide="canDecide"
      :can-create-rule="canCreateRule"
      :deciding="deciding"
      :error="decideError"
      @accept="accept"
      @reject="reject"
      @open-target="selected && openTarget(selected)"
      @create-rule="selected && createRuleFromDrawer(selected)"
    />

    <BatchCreateRulesDialog
      v-model:open="batchCreateOpen"
      :suggestions="batchCreateSuggestions"
      @done="onBatchDone"
    />

    <BatchDecideSuggestionsDialog
      v-model:open="decisionOpen"
      :suggestions="decisionSuggestions"
      :decision="decision"
      @done="onBatchDone"
    />
  </div>
</template>

<script setup lang="ts">
import {
  ChevronRight,
  ExternalLink,
  Eye,
  Lightbulb,
  RefreshCcw,
  Sparkles,
  X,
} from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  type LocationQuery,
  type LocationQueryRaw,
  useRoute,
  useRouter,
} from "vue-router";
import {
  listQualitySuggestions,
  updateQualitySuggestion,
} from "@/api/data-quality";
import { getWorkspaceProfileSetting } from "@/api/setting";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import BatchCreateRulesDialog from "@/components/data-quality/BatchCreateRulesDialog.vue";
import BatchDecideSuggestionsDialog from "@/components/data-quality/BatchDecideSuggestionsDialog.vue";
import SuggestionDetailDrawer from "@/components/data-quality/SuggestionDetailDrawer.vue";
import SuggestionJobsPanel from "@/components/data-quality/SuggestionJobsPanel.vue";
import SuggestionWorkbench from "@/components/data-quality/SuggestionWorkbench.vue";
import TargetScopeFilter from "@/components/data-quality/TargetScopeFilter.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import { useInstanceStore } from "@/store/modules/instance";
import {
  QualitySemanticType,
  QualitySeverity,
  type QualitySuggestion,
  QualitySuggestionKind,
  QualitySuggestionSource,
  QualitySuggestionStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  type QualityTargetScope,
  qualityRuleTypeKey,
  qualitySeverityKey,
  qualitySeverityVariant,
} from "@/utils/dataQuality";
import { llmFeatureAvailability } from "@/utils/dataQualityLlm";
import {
  formatConfidence,
  groupSuggestionsByTarget,
  isLabelSuggestion,
  isPendingDraft,
  isPendingLabel,
  isPendingSuggestion,
  labelSuggestionOf,
  MAX_SUGGESTION_BATCH,
  ruleDraftOf,
  type SuggestionTargetGroup,
  selectionCountsByKind,
  semanticTypeKey,
  suggestionKindKey,
  suggestionRationale,
  suggestionSourceKey,
  suggestionStatusKey,
  suggestionStatusVariant,
  suggestionTargetLabel,
  suggestionToEditorPrefill,
} from "@/utils/dataQualitySuggestion";
import { scopeFromQuery, writeScopeToQuery } from "@/utils/dataQualityTarget";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";
import { metaTypeSingularLabel } from "@/utils/metaType";
import {
  enumFilterValue,
  enumFromFilter,
  type QueryValue,
  queryString,
  readFlag,
} from "@/utils/queryFilters";

/**
 * The suggestion center: model rule drafts and semantic labels waiting for a
 * person.
 *
 * The queue is grouped by target so one run's output can be reviewed as a block.
 * Review is batch-first: a selection carries the common decisions, while a row
 * keeps its own inspect/edit path. AI drafting is a first-class action here; the
 * rule editor keeps it as a contextual shortcut.
 */

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();
const authStore = useAuthStore();
const instanceStore = useInstanceStore();

const canDecide = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.suggest")
);
const canCreateRule = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.create")
);

const llmEnabled = ref(false);
const canSuggest = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.suggest")
);
const llmAvailability = computed(() =>
  llmFeatureAvailability(llmEnabled.value, canSuggest.value)
);

const PENDING = String(QualitySuggestionStatus.PENDING);

const statusOptions = [
  {
    value: QualitySuggestionStatus.PENDING,
    key: "dataQuality.suggestionStatus.pending",
  },
  {
    value: QualitySuggestionStatus.ACCEPTED,
    key: "dataQuality.suggestionStatus.accepted",
  },
  {
    value: QualitySuggestionStatus.REJECTED,
    key: "dataQuality.suggestionStatus.rejected",
  },
  {
    value: QualitySuggestionStatus.SUPERSEDED,
    key: "dataQuality.suggestionStatus.superseded",
  },
];
const kindOptions = [
  {
    value: QualitySuggestionKind.RULE_DRAFT,
    key: "dataQuality.suggestionKind.ruleDraft",
  },
  {
    value: QualitySuggestionKind.LABEL,
    key: "dataQuality.suggestionKind.label",
  },
];
const sourceOptions = [
  {
    value: QualitySuggestionSource.NATURAL_LANGUAGE,
    key: "dataQuality.suggestionSource.naturalLanguage",
  },
  {
    value: QualitySuggestionSource.PROFILE,
    key: "dataQuality.suggestionSource.profile",
  },
];

/** Every member each filter accepts, UNSPECIFIED included as "any". */
const STATUS_FILTER_VALUES: QualitySuggestionStatus[] = [
  QualitySuggestionStatus.UNSPECIFIED,
  ...statusOptions.map((option) => option.value),
];
const KIND_FILTER_VALUES: QualitySuggestionKind[] = [
  QualitySuggestionKind.UNSPECIFIED,
  ...kindOptions.map((option) => option.value),
];
const SOURCE_FILTER_VALUES: QualitySuggestionSource[] = [
  QualitySuggestionSource.UNSPECIFIED,
  ...sourceOptions.map((option) => option.value),
];

/** The query parameter each filter travels under. */
const FILTER_QUERY_KEYS = [
  "status",
  "kind",
  "source",
  "instance",
  "database",
  "schema",
  "table",
] as const;

/** The filters as one value, so a URL and the applied set share a shape. */
interface SuggestionFilters {
  status: string;
  kind: string;
  source: string;
  scope: QualityTargetScope | undefined;
}

/**
 * The status filter travels differently from the others: absent means the
 * page's default pending queue, and `0` (the API's UNSPECIFIED) means every
 * status, which the controls show as "-" and which must be written because it is
 * not the default.
 */
function statusFromQuery(value: QueryValue | QueryValue[]): string {
  const raw = queryString(value).trim();
  if (!raw) {
    return PENDING;
  }
  const parsed = Number(raw);
  if (parsed === QualitySuggestionStatus.UNSPECIFIED) {
    return "-";
  }
  return STATUS_FILTER_VALUES.includes(parsed) ? String(parsed) : PENDING;
}

/** The filters a URL carries: reload, shared link and back/forward. */
function parseFilters(query: LocationQuery): SuggestionFilters {
  return {
    status: statusFromQuery(query.status),
    kind: enumFilterValue(query.kind, KIND_FILTER_VALUES),
    source: enumFilterValue(query.source, SOURCE_FILTER_VALUES),
    scope: scopeFromQuery(query),
  };
}

/** Only non-default values are written, so the URL stays readable. */
function serializeFilters(filters: SuggestionFilters): Record<string, string> {
  const owned: Record<string, string> = {};
  if (filters.status !== PENDING) {
    owned.status = filters.status === "-" ? "0" : filters.status;
  }
  if (filters.kind !== "-") {
    owned.kind = filters.kind;
  }
  if (filters.source !== "-") {
    owned.source = filters.source;
  }
  writeScopeToQuery(owned, filters.scope);
  return owned;
}

/** The filters the request currently applies. */
function appliedFilters(): SuggestionFilters {
  return {
    status: appliedStatus.value,
    kind: appliedKind.value,
    source: appliedSource.value,
    scope: appliedScope.value,
  };
}

const urlFilters = useUrlFilters<SuggestionFilters>({
  keys: FILTER_QUERY_KEYS,
  parse: parseFilters,
  serialize: serializeFilters,
});

// Read the URL before the first request: a reloaded or shared link queries what
// it carries instead of flashing the default pending queue first. `?draft=1` is
// not a filter — it stays in the query untouched.
const initialFilters = urlFilters.read();
const status = ref(initialFilters.status);
const kind = ref(initialFilters.kind);
const source = ref(initialFilters.source);
const scope = ref<QualityTargetScope | undefined>(initialFilters.scope);
const appliedStatus = ref(initialFilters.status);
const appliedKind = ref(initialFilters.kind);
const appliedSource = ref(initialFilters.source);
const appliedScope = ref<QualityTargetScope | undefined>(
  initialFilters.scope ? { ...initialFilters.scope } : undefined
);
const pageSize = ref(50);
const error = ref("");
const drawerOpen = ref(false);
const selected = ref<QualitySuggestion>();
const deciding = ref(false);
const decideError = ref("");
const workbenchOpen = ref(false);
const jobsRefreshKey = ref(0);
const batchCreateOpen = ref(false);
const batchCreateSuggestions = ref<QualitySuggestion[]>([]);
const decisionOpen = ref(false);
const decisionSuggestions = ref<QualitySuggestion[]>([]);
const decision = ref(QualitySuggestionStatus.REJECTED);
const selectedNames = ref<string[]>([]);
const collapsedGroups = ref<string[]>([]);

/** The instance display titles the group labels prefer over raw ids. */
const instanceTitles = computed<Record<string, string>>(() => {
  const titles: Record<string, string> = {};
  for (const instance of instanceStore.instances) {
    const id = instance.name.split("/").pop() ?? "";
    if (id) {
      titles[id] = instance.title || id;
    }
  }
  return titles;
});

const {
  items: suggestions,
  isLoading,
  hasNext,
  hasPrevious,
  reset: resetPage,
  refresh: refreshPage,
  goNext: goNextPage,
  goPrevious: goPreviousPage,
} = usePagedFetch<QualitySuggestion>({
  fetchPage: async (pageToken, signal) => {
    error.value = "";
    return await listQualitySuggestions({
      pageSize: pageSize.value,
      pageToken,
      status: enumFromFilter(appliedStatus.value, STATUS_FILTER_VALUES),
      kind: enumFromFilter(appliedKind.value, KIND_FILTER_VALUES),
      source: enumFromFilter(appliedSource.value, SOURCE_FILTER_VALUES),
      ...scopeFilters(appliedScope.value),
      signal,
    });
  },
  onError: (e) => {
    error.value = handleError(e, "dataQuality.suggestions.fetchError");
  },
});

const groupedSuggestions = computed(() =>
  groupSuggestionsByTarget(suggestions.value, instanceTitles.value)
);

const suggestionsByName = computed(
  () =>
    new Map(
      suggestions.value.map((suggestion) => [suggestion.name, suggestion])
    )
);

const selectedSuggestions = computed(() =>
  selectedNames.value
    .map((name) => suggestionsByName.value.get(name))
    .filter(
      (suggestion): suggestion is QualitySuggestion => suggestion !== undefined
    )
);

const selectionCounts = computed(() =>
  selectionCountsByKind(selectedSuggestions.value)
);

const selectedDrafts = computed(() =>
  selectedSuggestions.value.filter(isPendingDraft)
);
const selectedLabels = computed(() =>
  selectedSuggestions.value.filter(isPendingLabel)
);

/** Filters the reader changed away from the default PENDING queue. */
const hasActiveFilters = computed(
  () =>
    appliedStatus.value !== PENDING ||
    appliedKind.value !== "-" ||
    appliedSource.value !== "-" ||
    appliedScope.value !== undefined
);

interface FilterChip {
  key: string;
  label: string;
}

const activeFilterChips = computed<FilterChip[]>(() => {
  const chips: FilterChip[] = [];
  if (appliedStatus.value !== PENDING) {
    chips.push({
      key: "status",
      label:
        appliedStatus.value === "-"
          ? t("dataQuality.suggestions.allStatuses")
          : t(suggestionStatusKey(Number(appliedStatus.value))),
    });
  }
  if (appliedKind.value !== "-") {
    chips.push({
      key: "kind",
      label: t(suggestionKindKey(Number(appliedKind.value))),
    });
  }
  if (appliedSource.value !== "-") {
    chips.push({
      key: "source",
      label: t(suggestionSourceKey(Number(appliedSource.value))),
    });
  }
  if (appliedScope.value) {
    chips.push({
      key: "scope",
      label: appliedScope.value.label || appliedScope.value.instance,
    });
  }
  return chips;
});

const emptyTitle = computed(() => {
  if (hasActiveFilters.value) {
    return t("dataQuality.suggestions.noFiltered");
  }
  if (appliedStatus.value === PENDING) {
    return t("dataQuality.suggestions.pendingEmptyTitle");
  }
  return t("dataQuality.suggestions.emptyTitle");
});

const emptyDescription = computed(() => {
  if (hasActiveFilters.value) {
    return "";
  }
  if (appliedStatus.value === PENDING) {
    return t("dataQuality.suggestions.pendingEmptyDescription");
  }
  return t("dataQuality.suggestions.emptyDescription");
});

watch(pageSize, () => void resetPage());

// A reload, a shared link or back/forward changes the query from outside; the
// page adopts the filters it names. The replace this composable performs matches
// the applied set, so it is not adopted and cannot loop. `draft` is not a
// filter and never reaches here.
urlFilters.watchExternal((next) => {
  if (urlFilters.isCurrent(appliedFilters())) {
    return;
  }
  status.value = next.status;
  kind.value = next.kind;
  source.value = next.source;
  scope.value = next.scope ? { ...next.scope } : undefined;
  appliedStatus.value = next.status;
  appliedKind.value = next.kind;
  appliedSource.value = next.source;
  appliedScope.value = next.scope ? { ...next.scope } : undefined;
  selectedNames.value = [];
  void resetPage();
});

// Closing the workbench is the one action that retires its deep link: the query
// keeps every filter, and only `draft` goes.
watch(workbenchOpen, (open) => {
  if (open || !readFlag(route.query.draft)) {
    return;
  }
  const next: LocationQueryRaw = { ...route.query };
  delete next.draft;
  void router.replace({ query: next });
});

// A re-fetch can drop the rows a stale selection points at.
watch(suggestions, () => {
  const pending = new Set(
    suggestions.value
      .filter(isPendingSuggestion)
      .map((suggestion) => suggestion.name)
  );
  selectedNames.value = selectedNames.value.filter((name) => pending.has(name));
});

function scopeFilters(selection: QualityTargetScope | undefined) {
  if (!selection) {
    return {};
  }
  return {
    instance: selection.instance,
    database: selection.database,
    schema: selection.schema,
    table: selection.table,
  };
}

function applyFilters() {
  appliedStatus.value = status.value;
  appliedKind.value = kind.value;
  appliedSource.value = source.value;
  appliedScope.value = scope.value;
  selectedNames.value = [];
  // The URL is written first: the watcher then sees the applied set already
  // agrees with it, so adopting the route costs no second request.
  urlFilters.write(appliedFilters());
  void resetPage();
}

/** Back to the page's default view (the pending queue), not to every status. */
function clearFilters() {
  status.value = PENDING;
  kind.value = "-";
  source.value = "-";
  scope.value = undefined;
  applyFilters();
}

function removeFilter(key: string) {
  switch (key) {
    case "status":
      status.value = PENDING;
      appliedStatus.value = PENDING;
      break;
    case "kind":
      kind.value = "-";
      appliedKind.value = "-";
      break;
    case "source":
      source.value = "-";
      appliedSource.value = "-";
      break;
    case "scope":
      scope.value = undefined;
      appliedScope.value = undefined;
      break;
    default:
      return;
  }
  selectedNames.value = [];
  urlFilters.write(appliedFilters());
  void resetPage();
}

function showAllStatuses() {
  status.value = "-";
  appliedStatus.value = "-";
  selectedNames.value = [];
  urlFilters.write(appliedFilters());
  void resetPage();
}

function suggestionText(suggestion: QualitySuggestion): string {
  const draft = ruleDraftOf(suggestion);
  if (draft?.title) {
    return draft.title;
  }
  const label = labelSuggestionOf(suggestion);
  if (label) {
    // The kind and the semantic type already have their own badges; the row's
    // own text is the column the label belongs to.
    return (
      label.column || (suggestion.name.split("/").pop() ?? suggestion.name)
    );
  }
  return suggestion.name.split("/").pop() ?? suggestion.name;
}

function labelTypeKey(suggestion: QualitySuggestion): string | undefined {
  const label = labelSuggestionOf(suggestion);
  return label && label.label !== QualitySemanticType.UNSPECIFIED
    ? semanticTypeKey(label.label)
    : undefined;
}

/** The rule-type label of a draft this build can prefill, if it is one. */
function ruleDraftLabel(suggestion: QualitySuggestion): string | undefined {
  const prefill = suggestionToEditorPrefill(suggestion);
  return prefill ? qualityRuleTypeKey(prefill.ruleType) : undefined;
}

function draftSeverity(
  suggestion: QualitySuggestion
): QualitySeverity | undefined {
  const draft = ruleDraftOf(suggestion);
  return draft?.severity ? draft.severity : undefined;
}

/** Both kinds carry their own confidence, 0-1; 0 reads as unset. */
function confidenceOf(suggestion: QualitySuggestion): number {
  const draft = ruleDraftOf(suggestion);
  if (draft) {
    return draft.confidence;
  }
  return labelSuggestionOf(suggestion)?.confidence ?? 0;
}

/** The same confidence as a 0-100 meter width. */
function confidencePercent(suggestion: QualitySuggestion): number {
  return Math.round(Math.min(1, Math.max(0, confidenceOf(suggestion))) * 100);
}

function groupMetaType(group: SuggestionTargetGroup): MetaType {
  return group.suggestions[0]?.target?.metaType ?? MetaType.TABLE;
}

function selectedInstanceTitle(suggestion: QualitySuggestion): string {
  const guid = suggestion.target?.metaGuid ?? "";
  const instance = guid.split(";")[0] ?? "";
  return instance ? instanceStore.titleOf(`instances/${instance}`) : "";
}

function canEditDraft(suggestion: QualitySuggestion): boolean {
  return isPendingDraft(suggestion) && canCreateRule.value;
}

function selectableIn(group: SuggestionTargetGroup): QualitySuggestion[] {
  return group.suggestions.filter(isPendingSuggestion);
}

function groupSelectionState(
  group: SuggestionTargetGroup
): boolean | "indeterminate" {
  const selectable = selectableIn(group);
  if (selectable.length === 0) {
    return false;
  }
  const selected = selectable.filter((suggestion) =>
    selectedNames.value.includes(suggestion.name)
  ).length;
  if (selected === 0) {
    return false;
  }
  return selected === selectable.length ? true : "indeterminate";
}

function toggleGroup(group: SuggestionTargetGroup, checked: boolean) {
  const names = selectableIn(group).map((suggestion) => suggestion.name);
  if (checked) {
    selectedNames.value = [...new Set([...selectedNames.value, ...names])];
  } else {
    selectedNames.value = selectedNames.value.filter(
      (name) => !names.includes(name)
    );
  }
}

function toggleSuggestion(name: string, checked: boolean) {
  if (checked && !selectedNames.value.includes(name)) {
    selectedNames.value = [...selectedNames.value, name];
  } else if (!checked) {
    selectedNames.value = selectedNames.value.filter((item) => item !== name);
  }
}

function toggleGroupCollapsed(targetGuid: string) {
  collapsedGroups.value = collapsedGroups.value.includes(targetGuid)
    ? collapsedGroups.value.filter((guid) => guid !== targetGuid)
    : [...collapsedGroups.value, targetGuid];
}

function removeFromSelection(names: string[]) {
  const removed = new Set(names);
  selectedNames.value = selectedNames.value.filter(
    (name) => !removed.has(name)
  );
}

function inspect(suggestion: QualitySuggestion) {
  selected.value = suggestion;
  decideError.value = "";
  drawerOpen.value = true;
}

/**
 * Where a suggestion is adjusted: its target's contract workspace. The draft
 * itself needs no query parameter — the workspace reads the target's pending
 * proposals and renders them inline.
 */
function targetWorkspaceRoute(suggestion: QualitySuggestion) {
  const target = suggestion.target;
  if (!target?.metaGuid) {
    return undefined;
  }
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.metaGuid) },
    query: { metaType: String(target.metaType) },
  };
}

function openTarget(suggestion: QualitySuggestion) {
  const route = targetWorkspaceRoute(suggestion);
  if (!route) {
    return;
  }
  drawerOpen.value = false;
  void router.push(route);
}

function openWorkbench() {
  if (!llmAvailability.value.available) {
    return;
  }
  workbenchOpen.value = true;
}

function openBatchCreate(items: QualitySuggestion[]) {
  if (!canCreateRule.value || items.length === 0) {
    return;
  }
  batchCreateSuggestions.value = items.slice(0, MAX_SUGGESTION_BATCH);
  batchCreateOpen.value = true;
}

function createRuleFromDrawer(suggestion: QualitySuggestion) {
  drawerOpen.value = false;
  openBatchCreate([suggestion]);
}

function openDecision(
  next: QualitySuggestionStatus,
  items: QualitySuggestion[]
) {
  if (!canDecide.value || items.length === 0) {
    return;
  }
  decision.value = next;
  decisionSuggestions.value = items.slice(0, MAX_SUGGESTION_BATCH);
  decisionOpen.value = true;
}

/** A row-level accept writes one label; the bulk path opens the dialog. */
async function acceptLabel(suggestion: QualitySuggestion) {
  deciding.value = true;
  decideError.value = "";
  try {
    await updateQualitySuggestion({
      name: suggestion.name,
      decision: QualitySuggestionStatus.ACCEPTED,
    });
    notify.success(t("dataQuality.suggestions.accepted"));
    await refreshPage();
  } catch (e: unknown) {
    decideError.value = handleError(e, "dataQuality.suggestions.decideError");
  } finally {
    deciding.value = false;
  }
}

function accept(note: string) {
  void decideSelectedWithNote(QualitySuggestionStatus.ACCEPTED, note);
}

function reject(note: string) {
  void decideSelectedWithNote(QualitySuggestionStatus.REJECTED, note);
}

async function decideSelectedWithNote(
  decisionValue: QualitySuggestionStatus,
  note: string
) {
  const suggestion = selected.value;
  if (!suggestion) {
    return;
  }
  deciding.value = true;
  decideError.value = "";
  try {
    await updateQualitySuggestion({
      name: suggestion.name,
      decision: decisionValue,
      note,
    });
    drawerOpen.value = false;
    notify.success(
      t(
        decisionValue === QualitySuggestionStatus.ACCEPTED
          ? "dataQuality.suggestions.accepted"
          : "dataQuality.suggestions.rejected"
      )
    );
    await refreshPage();
  } catch (e: unknown) {
    decideError.value = handleError(e, "dataQuality.suggestions.decideError");
  } finally {
    deciding.value = false;
  }
}

/** One batch path for both dialogs: clear the succeeded rows and re-read. */
function onBatchDone(succeededNames: string[]) {
  removeFromSelection(succeededNames);
  void refreshPage();
}

/** A new job exists: let the panel reload and start polling immediately. */
function onJobSubmitted() {
  jobsRefreshKey.value += 1;
}

/**
 * A drafting job settled. Its suggestions are the newest rows, so the queue
 * moves to page one and selects the ones visible there under the current
 * filters; the rest stay reviewable in the queue.
 */
async function onJobsSettled(suggestionNames: string[]) {
  if (suggestionNames.length === 0) {
    await refreshPage();
    return;
  }
  await resetPage();
  const names = new Set(suggestionNames);
  selectedNames.value = suggestions.value
    .filter(
      (suggestion) =>
        names.has(suggestion.name) && isPendingSuggestion(suggestion)
    )
    .map((suggestion) => suggestion.name);
  if (selectedNames.value.length < suggestionNames.length) {
    notify.info(
      t("dataQuality.suggestions.queuedPartial", {
        selected: selectedNames.value.length,
        total: suggestionNames.length,
      })
    );
  }
  notify.success(
    t("dataQuality.suggestions.jobs.settledToast", {
      count: suggestionNames.length,
    })
  );
}

/** Only the workspace switch is read here; the settings page changes it. */
async function loadLlmSetting() {
  try {
    const profile = await getWorkspaceProfileSetting();
    llmEnabled.value = profile.dataQuality?.enableLlmAnalysis ?? false;
  } catch {
    llmEnabled.value = false;
  }
}

onMounted(async () => {
  void instanceStore.ensureLoaded().catch(() => {});
  void resetPage();
  await loadLlmSetting();
  // `?draft=1` is the rules page's discovery link. Open it even when the switch
  // is off or the caller may not draft: the workbench explains why with a link
  // to the settings, so the link is never a dead end.
  if (route.query.draft === "1") {
    workbenchOpen.value = true;
  }
});
</script>
