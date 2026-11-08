<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.agent.enrollment.title')"
      :description="t('dataQuality.agent.enrollment.description')"
    >
      <template #actions>
        <AgentPageTabs />
        <Button
          variant="outline"
          :disabled="isLoading"
          @click="load()"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': isLoading }"
          />
          {{ t("common.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <!-- The queue at a glance: one card per state, in the queue's workflow order.
         A card links to the queue filtered by that state, so the summary is also
         the page's filter. -->
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
      <StatCard
        v-for="card in summaryCards"
        :key="card.state"
        :label="t(enrollmentStateKey(card.state))"
        :value="stateCount(card.state)"
        :hint="stateHint(card.state)"
        :icon="card.icon"
        :tone="card.tone"
        :to="stateCardRoute(card.state)"
      />
    </div>

    <Card>
      <!-- Discovery's own state and the effective admission limits. The scan time
           is process-local, so a restarted server shows "not scanned yet" until
           the next tick. -->
      <div class="flex flex-wrap items-center gap-x-5 gap-y-2 border-b p-4 text-sm">
        <span class="text-xs text-muted-foreground">
          {{ t("dataQuality.agent.enrollment.lastScan") }}:
          <span
            class="text-foreground"
            :title="
              summary?.lastScanTime
                ? formatDateTime(summary.lastScanTime, locale)
                : ''
            "
          >
            {{ lastScanLabel }}
          </span>
        </span>
        <template v-if="runnerStatus">
          <span
            v-if="!runnerStatus.autoEnrollEnabled"
            class="text-xs text-muted-foreground"
          >
            {{ t("dataQuality.agent.enrollment.admissionAutoEnrollOff") }}
          </span>
          <template v-else>
            <span class="text-xs text-muted-foreground">
              {{
                t("dataQuality.agent.enrollment.admissionNewSessions", {
                  count: runnerStatus.maxNewSessionsPerHour,
                })
              }}
            </span>
            <span class="text-xs text-muted-foreground">
              {{
                t("dataQuality.agent.enrollment.admissionActiveSessions", {
                  count: runnerStatus.maxActiveSessions,
                })
              }}
            </span>
          </template>
        </template>
      </div>

      <div
        v-if="summaryDatabases.length > 0"
        class="divide-y"
      >
        <button
          v-for="database in summaryDatabases"
          :key="database.database"
          type="button"
          class="flex w-full flex-wrap items-center gap-x-3 gap-y-1.5 px-4 py-2.5 text-left transition-colors hover:bg-accent/40"
          :title="database.database"
          :aria-label="databaseRefLabel(database.database)"
          @click="filterByDatabase(database.database)"
        >
          <span class="min-w-40 flex-1 truncate text-sm font-medium">
            {{ databaseRefLabel(database.database) }}
          </span>
          <span class="flex flex-wrap items-center gap-1.5">
            <template
              v-for="entry in databaseStateCounts(database)"
              :key="entry.state"
            >
              <Badge
                v-if="entry.count > 0"
                :variant="enrollmentStateVariant(entry.state)"
                class="font-normal"
              >
                {{ t(databaseCountKey(entry.state), { count: entry.count }) }}
              </Badge>
            </template>
          </span>
        </button>
      </div>
      <p
        v-else
        class="p-4 text-sm text-muted-foreground"
      >
        {{ t("dataQuality.agent.enrollment.byDatabaseEmpty") }}
      </p>
    </Card>

    <Card>
      <div class="flex flex-wrap items-end gap-3 border-b p-4">
        <div class="flex min-w-48 flex-1 flex-col gap-1.5 sm:max-w-sm sm:flex-none">
          <Label
            for="quality-enrollment-search"
            class="sr-only"
          >
            {{ t("dataQuality.search") }}
          </Label>
          <Input
            id="quality-enrollment-search"
            v-model="search"
            class="w-full"
            :placeholder="t('dataQuality.agent.targetSearch')"
            @keyup.enter="applySearch"
          />
        </div>
        <div class="flex min-w-40 flex-col gap-1.5 [&>select]:hidden sm:ml-auto sm:w-48">
          <Label
            for="quality-enrollment-database"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.agent.filterDatabase") }}
          </Label>
          <Select
            :model-value="databaseFilter || '-'"
            @update:model-value="changeDatabase"
          >
            <SelectTrigger
              id="quality-enrollment-database"
              class="w-full"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.agent.allDatabases") }}
              </SelectItem>
              <SelectItem
                v-for="option in databaseOptions"
                :key="option.value"
                :value="option.value"
              >
                {{ option.label }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div class="flex min-w-36 flex-col gap-1.5 [&>select]:hidden sm:w-40">
          <Label
            for="quality-enrollment-state"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.agent.filterState") }}
          </Label>
          <Select
            :model-value="stateFilter"
            @update:model-value="changeState"
          >
            <SelectTrigger
              id="quality-enrollment-state"
              class="w-full"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.agent.allStates") }}
              </SelectItem>
              <SelectItem
                v-for="state in ENROLLMENT_STATE_ORDER"
                :key="state"
                :value="String(state)"
              >
                {{ t(enrollmentStateKey(state)) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div class="flex min-w-32 flex-col gap-1.5 [&>select]:hidden sm:w-36">
          <Label
            for="quality-enrollment-mode"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.agent.filterMode") }}
          </Label>
          <Select
            :model-value="modeFilter"
            @update:model-value="changeMode"
          >
            <SelectTrigger
              id="quality-enrollment-mode"
              class="w-full"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.agent.allModes") }}
              </SelectItem>
              <SelectItem
                v-for="mode in MODES"
                :key="mode"
                :value="String(mode)"
              >
                {{ t(agentSessionModeKey(mode)) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <PageState
        :loading="isLoading && enrollments.length === 0"
        :error="loadError"
        :title="t('dataQuality.agent.enrollment.loadFailed')"
      >
        <template #error>
          <Button
            variant="outline"
            size="sm"
            @click="load()"
          >
            {{ t("common.refresh") }}
          </Button>
        </template>

        <EmptyState
          v-if="enrollments.length === 0 && hasActiveFilters"
          :icon="SearchX"
          :title="t('dataQuality.agent.enrollment.emptyFilteredTitle')"
          :description="t('dataQuality.agent.enrollment.emptyFilteredDescription')"
        >
          <Button
            variant="outline"
            size="sm"
            @click="clearFilters"
          >
            {{ t("dataQuality.agent.clearFilters") }}
          </Button>
        </EmptyState>

        <EmptyState
          v-else-if="runnerDisabled"
          :icon="PowerOff"
          :title="t('dataQuality.agent.enrollment.runnerOffTitle')"
          :description="t('dataQuality.agent.enrollment.runnerOffDescription')"
        >
          <Button
            variant="outline"
            size="sm"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualitySettings' }">
              {{ t("dataQuality.agent.openSettings") }}
            </RouterLink>
          </Button>
        </EmptyState>

        <EmptyState
          v-else-if="autoEnrollOff"
          :icon="Settings"
          :title="t('dataQuality.agent.enrollment.autoEnrollOffTitle')"
          :description="t('dataQuality.agent.enrollment.autoEnrollOffDescription')"
        >
          <Button
            variant="outline"
            size="sm"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualitySettings' }">
              {{ t("dataQuality.agent.openSettings") }}
            </RouterLink>
          </Button>
        </EmptyState>

        <EmptyState
          v-else-if="!summary?.lastScanTime"
          :icon="Clock"
          :title="t('dataQuality.agent.enrollment.waitingTitle')"
          :description="t('dataQuality.agent.enrollment.waitingDescription')"
        />

        <EmptyState
          v-else
          :icon="Bot"
          :title="t('dataQuality.agent.enrollment.emptyTitle')"
          :description="t('dataQuality.agent.enrollment.emptyDescription')"
        />

        <div v-if="enrollments.length > 0">
          <Table class="[&_th]:whitespace-nowrap">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("dataQuality.agent.columnTarget") }}</TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.agent.columnDatabase") }}
                </TableHead>
                <TableHead>{{ t("dataQuality.agent.columnState") }}</TableHead>
                <TableHead class="hidden lg:table-cell">
                  {{ t("dataQuality.agent.columnMode") }}
                </TableHead>
                <TableHead class="hidden xl:table-cell">
                  {{ t("dataQuality.agent.enrollment.columnPriorityReason") }}
                </TableHead>
                <TableHead class="hidden xl:table-cell">
                  {{ t("dataQuality.agent.enrollment.columnBlockedReason") }}
                </TableHead>
                <TableHead class="hidden whitespace-nowrap lg:table-cell">
                  {{ t("dataQuality.agent.enrollment.columnLastTriaged") }}
                </TableHead>
                <TableHead class="hidden text-right md:table-cell">
                  {{ t("dataQuality.agent.enrollment.columnSession") }}
                </TableHead>
                <TableHead class="w-12 text-right">
                  <span class="sr-only">{{ t("common.actions") }}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="enrollment in enrollments"
                :key="enrollment.name"
              >
                <TableCell>
                  <div class="flex max-w-[14rem] flex-col gap-0.5 md:max-w-xs">
                    <RouterLink
                      :to="targetRoute(enrollment)"
                      class="truncate font-medium hover:underline"
                      :title="enrollmentTargetPath(enrollment)"
                    >
                      {{ enrollmentTargetName(enrollment) }}
                    </RouterLink>
                    <span class="truncate font-mono text-xs text-muted-foreground">
                      {{ enrollmentTargetPath(enrollment) }}
                    </span>
                  </div>
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  <span
                    class="whitespace-nowrap text-xs"
                    :title="enrollment.database"
                  >
                    {{ databaseRefLabel(enrollment.database) }}
                  </span>
                </TableCell>
                <TableCell>
                  <Badge :variant="enrollmentStateVariant(enrollment.state)">
                    {{ t(enrollmentStateKey(enrollment.state)) }}
                  </Badge>
                </TableCell>
                <TableCell class="hidden lg:table-cell">
                  <Badge :variant="agentSessionModeVariant(enrollment.mode)">
                    {{ t(agentSessionModeKey(enrollment.mode)) }}
                  </Badge>
                </TableCell>
                <TableCell class="hidden max-w-[16rem] xl:table-cell">
                  <div
                    v-if="enrollment.priorityReason"
                    class="flex flex-wrap items-center gap-1"
                    :title="priorityReasonText(enrollment)"
                  >
                    <Badge
                      v-for="(label, index) in enrollmentPriorityReasonLabels(
                        enrollment.priorityReason
                      )"
                      :key="`${enrollment.name}-${index}`"
                      variant="secondary"
                      class="font-normal"
                    >
                      {{ reasonText(label) }}
                    </Badge>
                  </div>
                  <span
                    v-else
                    class="text-muted-foreground"
                  >-</span>
                </TableCell>
                <TableCell class="hidden max-w-[14rem] xl:table-cell">
                  <span
                    v-if="blockedReasonOf(enrollment)"
                    class="line-clamp-2 text-xs text-warning"
                    :title="reasonText(blockedReasonOf(enrollment))"
                  >
                    {{ reasonText(blockedReasonOf(enrollment)) }}
                  </span>
                  <span
                    v-else
                    class="text-muted-foreground"
                  >-</span>
                </TableCell>
                <TableCell class="hidden whitespace-nowrap lg:table-cell">
                  <span
                    v-if="enrollment.lastTriagedTime"
                    :title="formatDateTime(enrollment.lastTriagedTime, locale)"
                  >
                    {{ formatRelativeTime(enrollment.lastTriagedTime, locale) }}
                  </span>
                  <span
                    v-else
                    class="text-muted-foreground"
                  >
                    {{ t("dataQuality.agent.enrollment.neverTriaged") }}
                  </span>
                </TableCell>
                <TableCell class="hidden text-right md:table-cell">
                  <Button
                    v-if="enrollment.session"
                    variant="ghost"
                    size="icon"
                    class="h-8 w-8"
                    as-child
                  >
                    <RouterLink
                      :to="sessionRoute(enrollment)"
                      :title="t('dataQuality.agent.enrollment.openSession')"
                      :aria-label="t('dataQuality.agent.enrollment.openSession')"
                    >
                      <ChevronRight class="h-4 w-4" />
                    </RouterLink>
                  </Button>
                  <span
                    v-else
                    class="text-xs text-muted-foreground"
                  >-</span>
                </TableCell>
                <TableCell class="text-right">
                  <div
                    v-if="canUpdate"
                    class="flex items-center justify-end gap-1"
                  >
                    <Button
                      v-if="canRetry(enrollment)"
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :disabled="busy"
                      :title="t('dataQuality.agent.enrollment.retry')"
                      :aria-label="t('dataQuality.agent.enrollment.retry')"
                      @click="retry(enrollment)"
                    >
                      <RotateCcw class="h-4 w-4" />
                    </Button>
                    <Button
                      v-if="enrollment.state === QualityAgentEnrollmentState.EXCLUDED"
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :disabled="busy"
                      :title="t('dataQuality.agent.enrollment.include')"
                      :aria-label="t('dataQuality.agent.enrollment.include')"
                      @click="include(enrollment)"
                    >
                      <Undo2 class="h-4 w-4" />
                    </Button>
                    <Button
                      v-else-if="canExclude(enrollment)"
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8 text-destructive hover:text-destructive"
                      :disabled="busy"
                      :title="t('dataQuality.agent.enrollment.exclude')"
                      :aria-label="t('dataQuality.agent.enrollment.exclude')"
                      @click="askExclude(enrollment)"
                    >
                      <Ban class="h-4 w-4" />
                    </Button>
                  </div>
                  <span
                    v-else
                    class="text-muted-foreground"
                  >-</span>
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
              @previous="goPrevious"
              @next="goNext"
            />
          </div>
        </div>
      </PageState>
    </Card>

    <ConfirmDeleteDialog
      v-model="excludeOpen"
      :title="t('dataQuality.agent.enrollment.excludeTitle')"
      :message="t('dataQuality.agent.enrollment.excludeDescription')"
      :item-name="excludeTarget ? enrollmentTargetName(excludeTarget) : ''"
      :confirm-label="t('dataQuality.agent.enrollment.exclude')"
      :loading="excludeBusy"
      @confirm="confirmExclude"
    />
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import {
  Ban,
  Bot,
  ChevronRight,
  Clock,
  Hourglass,
  Moon,
  PlayCircle,
  PowerOff,
  RefreshCcw,
  RotateCcw,
  SearchX,
  Settings,
  Trash2,
  Undo2,
  Unplug,
} from "lucide-vue-next";
import type { Component } from "vue";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  type LocationQuery,
  type RouteLocationRaw,
  RouterLink,
  useRoute,
} from "vue-router";
import {
  getQualityAgentEnrollmentSummary,
  getQualityAgentRunnerStatus,
  listQualityAgentEnrollments,
  retryQualityAgentEnrollment,
  updateQualityAgentEnrollment,
} from "@/api/data-quality";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import StatCard, { type StatCardTone } from "@/components/common/StatCard.vue";
import TablePager from "@/components/common/TablePager.vue";
import AgentPageTabs from "@/components/data-quality/AgentPageTabs.vue";
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
import { useDataQualityPermissions } from "@/composables/useDataQualityPermissions";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { usePolling } from "@/composables/usePolling";
import { useUrlFilters } from "@/composables/useUrlFilters";
import { notify } from "@/lib/notify";
import {
  type QualityAgentEnrollment,
  QualityAgentEnrollmentSchema,
  QualityAgentEnrollmentState,
  type QualityAgentEnrollmentSummary,
  type QualityAgentRunnerStatus,
  QualityAgentSessionMode,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  databaseRefLabel,
  databaseStateCounts,
  ENROLLMENT_STATE_ORDER,
  type EnrollmentReasonLabel,
  enrollmentBlockedReasonLabel,
  enrollmentPercent,
  enrollmentPriorityReasonLabels,
  enrollmentStateCount,
  enrollmentStateCounts,
  enrollmentStateKey,
  enrollmentStateVariant,
  enrollmentTargetName,
  enrollmentTargetPath,
} from "@/utils/agentEnrollment";
import {
  agentSessionModeKey,
  agentSessionModeVariant,
} from "@/utils/agentSession";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { isAborted } from "@/utils/error";
import { guidToRouteParams } from "@/utils/guid";
import {
  enumFilterValue,
  enumFromFilter,
  queryString,
} from "@/utils/queryFilters";

/**
 * The runner's enrollment queue: every table in the database scope, its state,
 * why it is where it is, and the one action that moves it.
 *
 * The summary is the filter as well: a state card or a database row narrows the
 * table. The table itself is server-paged and server-filtered, and the page polls
 * only while the queue still has pending or active rows, so a disabled runner
 * costs nothing.
 */

const MODES: readonly QualityAgentSessionMode[] = [
  QualityAgentSessionMode.MONITOR,
  QualityAgentSessionMode.TRIAGE,
];

const ALL_ENROLLMENT_STATES: readonly QualityAgentEnrollmentState[] = [
  QualityAgentEnrollmentState.UNSPECIFIED,
  ...ENROLLMENT_STATE_ORDER,
];

const FILTER_QUERY_KEYS = ["database", "state", "mode", "q"] as const;
const POLL_INTERVAL_MS = 10_000;

const { t, locale } = useI18n();
const route = useRoute();
const { formatError } = useErrorMessage();
const { canUpdate } = useDataQualityPermissions();

interface EnrollmentFilters {
  database: string;
  state: string;
  mode: string;
  search: string;
}

const summary = ref<QualityAgentEnrollmentSummary>();
const runnerStatus = ref<QualityAgentRunnerStatus>();
const loadError = ref("");
const pageSize = ref(50);
const busy = ref(false);
const excludeOpen = ref(false);
const excludeTarget = ref<QualityAgentEnrollment>();
const excludeBusy = ref(false);

// ---- filters and the URL ----

function parseFilters(query: LocationQuery): EnrollmentFilters {
  return {
    database: queryString(query.database),
    state: enumFilterValue(query.state, ALL_ENROLLMENT_STATES),
    mode: enumFilterValue(query.mode, MODES),
    search: queryString(query.q).trim(),
  };
}

function serializeFilters(filters: EnrollmentFilters): Record<string, string> {
  const owned: Record<string, string> = {};
  if (filters.database) {
    owned.database = filters.database;
  }
  if (filters.state !== "-") {
    owned.state = filters.state;
  }
  if (filters.mode !== "-") {
    owned.mode = filters.mode;
  }
  if (filters.search) {
    owned.q = filters.search;
  }
  return owned;
}

const urlFilters = useUrlFilters<EnrollmentFilters>({
  keys: FILTER_QUERY_KEYS,
  parse: parseFilters,
  serialize: serializeFilters,
});

const initialFilters = urlFilters.read();
const databaseFilter = ref(initialFilters.database);
const stateFilter = ref(initialFilters.state);
const modeFilter = ref(initialFilters.mode);
const search = ref(initialFilters.search);
/** The term the table is filtered by; the server search only runs on Enter. */
const appliedSearch = ref(initialFilters.search);

const hasActiveFilters = computed(
  () =>
    databaseFilter.value !== "" ||
    stateFilter.value !== "-" ||
    modeFilter.value !== "-" ||
    appliedSearch.value !== ""
);

function currentFilters(): EnrollmentFilters {
  return {
    database: databaseFilter.value,
    state: stateFilter.value,
    mode: modeFilter.value,
    search: appliedSearch.value,
  };
}

/** The query one filter set travels under, preserving foreign parameters. */
function filtersQuery(filters: EnrollmentFilters) {
  const query = { ...route.query };
  for (const key of FILTER_QUERY_KEYS) {
    delete query[key];
  }
  return { ...query, ...serializeFilters(filters) };
}

function changeState(value: unknown) {
  stateFilter.value = enumFilterValue(String(value), ALL_ENROLLMENT_STATES);
  urlFilters.write(currentFilters());
  void reset();
}

function changeMode(value: unknown) {
  modeFilter.value = enumFilterValue(String(value), MODES);
  urlFilters.write(currentFilters());
  void reset();
}

function changeDatabase(value: unknown) {
  const next = String(value);
  databaseFilter.value = next === "-" ? "" : next;
  urlFilters.write(currentFilters());
  void reset();
}

function applySearch() {
  appliedSearch.value = search.value.trim();
  urlFilters.write(currentFilters());
  void reset();
}

function clearFilters() {
  databaseFilter.value = "";
  stateFilter.value = "-";
  modeFilter.value = "-";
  search.value = "";
  appliedSearch.value = "";
  urlFilters.write(currentFilters());
  void reset();
}

function filterByDatabase(database: string) {
  databaseFilter.value = databaseFilter.value === database ? "" : database;
  urlFilters.write(currentFilters());
  void reset();
}

urlFilters.watchExternal((filters) => {
  databaseFilter.value = filters.database;
  stateFilter.value = filters.state;
  modeFilter.value = filters.mode;
  search.value = filters.search;
  appliedSearch.value = filters.search;
  void reset();
});

// ---- summary ----

const summaryCards: Array<{
  state: QualityAgentEnrollmentState;
  icon: Component;
  tone: StatCardTone;
}> = [
  {
    state: QualityAgentEnrollmentState.PENDING,
    icon: Hourglass,
    tone: "info",
  },
  {
    state: QualityAgentEnrollmentState.ACTIVE,
    icon: PlayCircle,
    tone: "success",
  },
  { state: QualityAgentEnrollmentState.DORMANT, icon: Moon, tone: "primary" },
  {
    state: QualityAgentEnrollmentState.EXCLUDED,
    icon: Ban,
    tone: "warning",
  },
  {
    state: QualityAgentEnrollmentState.OUT_OF_SCOPE,
    icon: Unplug,
    tone: "info",
  },
  { state: QualityAgentEnrollmentState.GONE, icon: Trash2, tone: "danger" },
];

const summaryDatabases = computed(() => summary.value?.databases ?? []);

const databaseOptions = computed(() => {
  const names = summaryDatabases.value.map((database) => database.database);
  // A stale link may name a database whose rows are gone from the summary; keep
  // it selectable so the filter state is visible instead of silently dropped.
  if (databaseFilter.value && !names.includes(databaseFilter.value)) {
    names.unshift(databaseFilter.value);
  }
  return names.map((name) => ({
    value: name,
    label: databaseRefLabel(name),
  }));
});

const databaseCountKeys: Record<number, string> = {
  [QualityAgentEnrollmentState.PENDING]:
    "dataQuality.agent.enrollment.databasePending",
  [QualityAgentEnrollmentState.ACTIVE]:
    "dataQuality.agent.enrollment.databaseActive",
  [QualityAgentEnrollmentState.DORMANT]:
    "dataQuality.agent.enrollment.databaseDormant",
  [QualityAgentEnrollmentState.EXCLUDED]:
    "dataQuality.agent.enrollment.databaseExcluded",
  [QualityAgentEnrollmentState.OUT_OF_SCOPE]:
    "dataQuality.agent.enrollment.databaseOutOfScope",
  [QualityAgentEnrollmentState.GONE]:
    "dataQuality.agent.enrollment.databaseGone",
};

function databaseCountKey(state: QualityAgentEnrollmentState): string {
  return databaseCountKeys[state] ?? "";
}

function stateCount(state: QualityAgentEnrollmentState): string | number {
  if (!summary.value) {
    return "—";
  }
  return enrollmentStateCount(enrollmentStateCounts(summary.value), state);
}

function stateHint(state: QualityAgentEnrollmentState): string {
  if (!summary.value) {
    return "";
  }
  const counts = enrollmentStateCounts(summary.value);
  const total = ENROLLMENT_STATE_ORDER.reduce(
    (sum, member) => sum + enrollmentStateCount(counts, member),
    0
  );
  if (total === 0) {
    return "";
  }
  const part = enrollmentStateCount(counts, state);
  return t("dataQuality.agent.enrollment.stateShare", {
    percent: enrollmentPercent(part, total),
  });
}

/** The same queue filtered to one state, for a summary card's link. */
function stateCardRoute(state: QualityAgentEnrollmentState): RouteLocationRaw {
  return {
    name: "DataQualityAgentEnrollment",
    query: filtersQuery({ ...currentFilters(), state: String(state) }),
  };
}

const lastScanLabel = computed(() =>
  summary.value?.lastScanTime
    ? formatRelativeTime(summary.value.lastScanTime, locale.value)
    : t("dataQuality.agent.enrollment.neverScanned")
);

const runnerDisabled = computed(() => runnerStatus.value?.enabled === false);
const autoEnrollOff = computed(
  () =>
    runnerStatus.value?.enabled === true &&
    !runnerStatus.value.autoEnrollEnabled
);

async function loadSummary(signal?: AbortSignal) {
  try {
    summary.value = await getQualityAgentEnrollmentSummary(signal);
  } catch (e: unknown) {
    if (isAborted(e)) {
      return;
    }
    // The table still renders; the summary cards show "—" and the empty states
    // fall back to the neutral one.
    summary.value = undefined;
  }
}

async function loadRunnerStatus(signal?: AbortSignal) {
  try {
    runnerStatus.value = await getQualityAgentRunnerStatus(signal);
  } catch (e: unknown) {
    if (isAborted(e)) {
      return;
    }
    runnerStatus.value = undefined;
  }
}

// ---- paged table ----

const {
  items: enrollments,
  isLoading,
  hasNext,
  hasPrevious,
  reset,
  refresh,
  goNext,
  goPrevious,
} = usePagedFetch<QualityAgentEnrollment>({
  fetchPage: async (pageToken, signal) => {
    loadError.value = "";
    return await listQualityAgentEnrollments({
      pageSize: pageSize.value,
      pageToken,
      database: databaseFilter.value,
      state: enumFromFilter(stateFilter.value, ALL_ENROLLMENT_STATES),
      mode: enumFromFilter(modeFilter.value, [
        QualityAgentSessionMode.UNSPECIFIED,
        ...MODES,
      ]),
      search: appliedSearch.value,
      signal,
    });
  },
  onError: (e) => {
    loadError.value = formatError(e, "dataQuality.agent.enrollment.loadFailed");
  },
});

watch(pageSize, () => void reset());

/** Whether the queue still has work the runner will visibly move. */
const hasActiveWork = computed(() => {
  const counts = enrollmentStateCounts(summary.value);
  return (
    enrollmentStateCount(counts, QualityAgentEnrollmentState.PENDING) +
      enrollmentStateCount(counts, QualityAgentEnrollmentState.ACTIVE) >
    0
  );
});

const polling = usePolling(
  async (signal) => {
    if (!hasActiveWork.value) {
      return;
    }
    await Promise.all([loadSummary(signal), loadRunnerStatus(signal)]);
    await refresh({ signal });
  },
  {
    intervalMs: POLL_INTERVAL_MS,
    shouldContinue: () => hasActiveWork.value,
    continueOnError: true,
  }
);

watch(hasActiveWork, (active) => {
  if (active) {
    polling.start();
  } else {
    polling.stop();
  }
});

async function load() {
  loadError.value = "";
  await Promise.all([loadSummary(), loadRunnerStatus()]);
  await reset();
  if (hasActiveWork.value) {
    polling.start();
  }
}

// ---- row actions ----

function sessionRoute(enrollment: QualityAgentEnrollment) {
  return {
    name: "DataQualityAgentSession",
    params: { sessionId: enrollment.session.split("/") },
  };
}

function targetRoute(enrollment: QualityAgentEnrollment) {
  return {
    name: "DataQualityTarget",
    params: {
      targetGuid: guidToRouteParams(enrollment.target?.metaGuid ?? ""),
    },
    query: {
      metaType: String(enrollment.target?.metaType ?? MetaType.UNSPECIFIED),
    },
  };
}

function blockedReasonOf(enrollment: QualityAgentEnrollment) {
  return enrollmentBlockedReasonLabel(enrollment.blockedReason);
}

/** A reason label as text: a translated key when there is one, else raw text. */
function reasonText(label: EnrollmentReasonLabel | undefined): string {
  if (!label) {
    return "";
  }
  return label.key ? t(label.key, label.params ?? {}) : (label.text ?? "");
}

function priorityReasonText(enrollment: QualityAgentEnrollment): string {
  return enrollmentPriorityReasonLabels(enrollment.priorityReason)
    .map(reasonText)
    .join(", ");
}

function canRetry(enrollment: QualityAgentEnrollment): boolean {
  return (
    enrollment.state !== QualityAgentEnrollmentState.EXCLUDED &&
    enrollment.state !== QualityAgentEnrollmentState.GONE
  );
}

function canExclude(enrollment: QualityAgentEnrollment): boolean {
  return (
    enrollment.state !== QualityAgentEnrollmentState.EXCLUDED &&
    enrollment.state !== QualityAgentEnrollmentState.GONE
  );
}

/** Put a changed row back into the page it came from without re-reading it. */
function replaceRow(updated: QualityAgentEnrollment) {
  const index = enrollments.value.findIndex(
    (enrollment) => enrollment.name === updated.name
  );
  if (index >= 0) {
    enrollments.value[index] = updated;
  }
}

/** Apply one state change with the revision the caller saw. */
async function setEnrollmentState(
  enrollment: QualityAgentEnrollment,
  state: QualityAgentEnrollmentState
): Promise<QualityAgentEnrollment> {
  const next = create(QualityAgentEnrollmentSchema, enrollment);
  next.state = state;
  return await updateQualityAgentEnrollment({
    enrollment: next,
    updateMask: ["state"],
    expectedRevision: enrollment.revision,
  });
}

async function retry(enrollment: QualityAgentEnrollment) {
  if (busy.value) {
    return;
  }
  busy.value = true;
  try {
    const updated = await retryQualityAgentEnrollment(
      enrollment.name,
      enrollment.revision
    );
    replaceRow(updated);
    notify.success(t("dataQuality.agent.enrollment.retriedToast"));
    await loadSummary();
  } catch (e: unknown) {
    notify.error(formatError(e, "dataQuality.agent.enrollment.actionFailed"));
    // A stale revision means the runner moved the row first; re-read it rather
    // than retrying with the revision already lost.
    await refresh();
    await loadSummary();
  } finally {
    busy.value = false;
  }
}

async function include(enrollment: QualityAgentEnrollment) {
  if (busy.value) {
    return;
  }
  busy.value = true;
  try {
    const updated = await setEnrollmentState(
      enrollment,
      QualityAgentEnrollmentState.PENDING
    );
    replaceRow(updated);
    notify.success(t("dataQuality.agent.enrollment.includedToast"));
    await loadSummary();
  } catch (e: unknown) {
    notify.error(formatError(e, "dataQuality.agent.enrollment.actionFailed"));
    await refresh();
    await loadSummary();
  } finally {
    busy.value = false;
  }
}

function askExclude(enrollment: QualityAgentEnrollment) {
  excludeTarget.value = enrollment;
  excludeOpen.value = true;
}

async function confirmExclude() {
  const enrollment = excludeTarget.value;
  if (!enrollment || excludeBusy.value) {
    return;
  }
  excludeBusy.value = true;
  try {
    const updated = await setEnrollmentState(
      enrollment,
      QualityAgentEnrollmentState.EXCLUDED
    );
    replaceRow(updated);
    notify.success(t("dataQuality.agent.enrollment.excludedToast"));
    excludeOpen.value = false;
    excludeTarget.value = undefined;
    await loadSummary();
  } catch (e: unknown) {
    excludeOpen.value = false;
    excludeTarget.value = undefined;
    notify.error(formatError(e, "dataQuality.agent.enrollment.actionFailed"));
    await refresh();
    await loadSummary();
  } finally {
    excludeBusy.value = false;
  }
}

onMounted(() => {
  void load();
});
</script>
