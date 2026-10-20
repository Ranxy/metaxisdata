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
      </template>
    </PageHeader>

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
          v-if="suggestions.length === 0"
          :icon="Lightbulb"
          :title="
            hasActiveFilters
              ? t('dataQuality.suggestions.noFiltered')
              : t('dataQuality.suggestions.emptyTitle')
          "
          :description="
            hasActiveFilters ? '' : t('dataQuality.suggestions.emptyDescription')
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
        </EmptyState>

        <div v-else>
          <Table class="[&_th]:whitespace-nowrap">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("dataQuality.suggestions.suggestion") }}</TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.suggestions.source") }}
                </TableHead>
                <TableHead class="hidden lg:table-cell">
                  {{ t("dataQuality.suggestions.provider") }}
                </TableHead>
                <TableHead>{{ t("dataQuality.suggestions.status") }}</TableHead>
                <TableHead class="hidden whitespace-nowrap md:table-cell">
                  {{ t("dataQuality.suggestions.createdAt") }}
                </TableHead>
                <TableHead class="w-12 text-right">
                  <span class="sr-only">{{ t("common.actions") }}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="suggestion in suggestions"
                :key="suggestion.name"
                class="cursor-pointer"
                @click="review(suggestion)"
              >
                <TableCell>
                  <div class="flex flex-wrap items-center gap-2">
                    <Badge variant="outline">
                      {{ t(suggestionKindKey(suggestion.kind)) }}
                    </Badge>
                    <span class="truncate text-sm font-medium">
                      {{ suggestionTitle(suggestion) }}
                    </span>
                  </div>
                  <div class="mt-1 truncate font-mono text-xs text-muted-foreground">
                    {{ suggestion.target?.metaGuid || "-" }}
                  </div>
                  <!-- Below md the source and created columns collapse in here. -->
                  <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                    <span>{{ t(suggestionSourceKey(suggestion.source)) }}</span>
                    <span aria-hidden="true">·</span>
                    <span :title="formatDateTime(suggestion.createTime, locale)">
                      {{ formatRelativeTime(suggestion.createTime, locale) }}
                    </span>
                  </div>
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  {{ t(suggestionSourceKey(suggestion.source)) }}
                </TableCell>
                <TableCell class="hidden max-w-48 truncate lg:table-cell">
                  {{ [suggestion.provider, suggestion.model].filter(Boolean).join(" · ") || "-" }}
                </TableCell>
                <TableCell>
                  <Badge :variant="suggestionStatusVariant(suggestion.status)">
                    {{ t(suggestionStatusKey(suggestion.status)) }}
                  </Badge>
                </TableCell>
                <TableCell
                  class="hidden whitespace-nowrap text-muted-foreground md:table-cell"
                  :title="formatDateTime(suggestion.createTime, locale)"
                >
                  {{ formatRelativeTime(suggestion.createTime, locale) }}
                </TableCell>
                <TableCell
                  class="text-right"
                  @click.stop
                >
                  <Button
                    v-if="canOpenInEditor(suggestion)"
                    variant="ghost"
                    size="icon"
                    class="h-8 w-8"
                    :title="t('dataQuality.suggestions.openInEditor')"
                    :aria-label="t('dataQuality.suggestions.openInEditor')"
                    @click="openEditor(suggestion)"
                  >
                    <ExternalLink class="h-4 w-4 text-muted-foreground" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    class="h-8 w-8"
                    :title="t('dataQuality.suggestions.review')"
                    :aria-label="t('dataQuality.suggestions.review')"
                    @click="review(suggestion)"
                  >
                    <Eye class="h-4 w-4 text-muted-foreground" />
                  </Button>
                </TableCell>
              </TableRow>
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

    <SuggestionDetailDrawer
      v-model:open="drawerOpen"
      :suggestion="selected"
      :can-decide="canDecide"
      :can-create-rule="canCreateRule"
      :deciding="deciding"
      :error="decideError"
      @accept="accept"
      @reject="reject"
      @open-editor="selected && openEditor(selected)"
    />
  </div>
</template>

<script setup lang="ts">
import { ExternalLink, Eye, Lightbulb, RefreshCcw } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import {
  listQualitySuggestions,
  updateQualitySuggestion,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import SuggestionDetailDrawer from "@/components/data-quality/SuggestionDetailDrawer.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
  type QualitySuggestion,
  QualitySuggestionKind,
  QualitySuggestionSource,
  QualitySuggestionStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  labelSuggestionOf,
  ruleDraftOf,
  suggestionKindKey,
  suggestionSourceKey,
  suggestionStatusKey,
  suggestionStatusVariant,
} from "@/utils/dataQualitySuggestion";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * The suggestion queue: model drafts and column labels waiting for a person to
 * accept or reject them. Accepting a rule draft opens the editor prefilled with
 * it, so the person reviews and saves a real rule; accepting a label writes the
 * label server-side.
 */

const { t, locale } = useI18n();
const router = useRouter();
const { handleError } = useErrorHandler();
const authStore = useAuthStore();

const canDecide = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.suggest")
);
const canCreateRule = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.create")
);

const status = ref("-");
const kind = ref("-");
const source = ref("-");
const appliedStatus = ref("-");
const appliedKind = ref("-");
const appliedSource = ref("-");
const pageSize = ref(50);
const error = ref("");
const drawerOpen = ref(false);
const selected = ref<QualitySuggestion>();
const deciding = ref(false);
const decideError = ref("");

const hasActiveFilters = computed(
  () =>
    appliedStatus.value !== "-" ||
    appliedKind.value !== "-" ||
    appliedSource.value !== "-"
);

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
      status: enumFromFilter(appliedStatus.value, [
        QualitySuggestionStatus.UNSPECIFIED,
        QualitySuggestionStatus.PENDING,
        QualitySuggestionStatus.ACCEPTED,
        QualitySuggestionStatus.REJECTED,
      ]),
      kind: enumFromFilter(appliedKind.value, [
        QualitySuggestionKind.UNSPECIFIED,
        QualitySuggestionKind.RULE_DRAFT,
        QualitySuggestionKind.LABEL,
      ]),
      source: enumFromFilter(appliedSource.value, [
        QualitySuggestionSource.UNSPECIFIED,
        QualitySuggestionSource.NATURAL_LANGUAGE,
        QualitySuggestionSource.PROFILE,
      ]),
      signal,
    });
  },
  onError: (e) => {
    error.value = handleError(e, "dataQuality.suggestions.fetchError");
  },
});

watch(pageSize, () => void resetPage());

function enumFromFilter<T extends number>(value: string, members: T[]): T {
  const parsed = Number(value);
  return members.includes(parsed as T) ? (parsed as T) : members[0];
}

function applyFilters() {
  appliedStatus.value = status.value;
  appliedKind.value = kind.value;
  appliedSource.value = source.value;
  void resetPage();
}

function clearFilters() {
  status.value = "-";
  kind.value = "-";
  source.value = "-";
  applyFilters();
}

function suggestionTitle(suggestion: QualitySuggestion): string {
  const draft = ruleDraftOf(suggestion);
  if (draft?.title) {
    return draft.title;
  }
  const label = labelSuggestionOf(suggestion);
  if (label) {
    return `${label.column} · ${t(suggestionKindKey(suggestion.kind))}`;
  }
  return suggestion.name.split("/").pop() ?? suggestion.name;
}

function review(suggestion: QualitySuggestion) {
  selected.value = suggestion;
  decideError.value = "";
  drawerOpen.value = true;
}

/** Accept writes the decision for a LABEL suggestion; rules are accepted by
 * the editor after the rule is actually created. */
async function accept(note: string) {
  const suggestion = selected.value;
  if (!suggestion) {
    return;
  }
  deciding.value = true;
  decideError.value = "";
  try {
    await updateQualitySuggestion({
      name: suggestion.name,
      decision: QualitySuggestionStatus.ACCEPTED,
      note,
    });
    drawerOpen.value = false;
    notify.success(t("dataQuality.suggestions.accepted"));
    await refreshPage();
  } catch (e: unknown) {
    decideError.value = handleError(e, "dataQuality.suggestions.decideError");
  } finally {
    deciding.value = false;
  }
}

/** A rule draft stays PENDING while it is reviewed: no decision is written until
 * the editor saves a rule from it. */
function canOpenInEditor(suggestion: QualitySuggestion): boolean {
  return (
    suggestion.status === QualitySuggestionStatus.PENDING &&
    suggestion.kind === QualitySuggestionKind.RULE_DRAFT &&
    canCreateRule.value
  );
}

function openEditor(suggestion: QualitySuggestion) {
  drawerOpen.value = false;
  void router.push({
    name: "DataQualityRuleNew",
    query: { suggestion: suggestion.name },
  });
}

async function reject(note: string) {
  const suggestion = selected.value;
  if (!suggestion) {
    return;
  }
  deciding.value = true;
  decideError.value = "";
  try {
    await updateQualitySuggestion({
      name: suggestion.name,
      decision: QualitySuggestionStatus.REJECTED,
      note,
    });
    drawerOpen.value = false;
    notify.success(t("dataQuality.suggestions.rejected"));
    await refreshPage();
  } catch (e: unknown) {
    decideError.value = handleError(e, "dataQuality.suggestions.decideError");
  } finally {
    deciding.value = false;
  }
}

onMounted(() => {
  void resetPage();
});
</script>
