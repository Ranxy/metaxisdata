<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.agent.title')"
      :description="t('dataQuality.agent.description')"
    >
      <template #actions>
        <Button
          v-if="canUpdate"
          @click="pickerOpen = true"
        >
          <Plus class="mr-2 h-4 w-4" />
          {{ t("dataQuality.agent.startFromTable") }}
        </Button>
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

    <!-- The runner's own liveness and budget spend. `runner_last_tick_time` is
         process-local, so a restart shows "not ticking" until the next tick. -->
    <Card>
      <CardContent class="flex flex-wrap items-center gap-x-5 gap-y-2 py-3 text-sm">
        <Badge
          v-if="runnerState === 'ticking'"
          variant="success"
        >
          {{ t("dataQuality.agent.runnerTicking") }}
        </Badge>
        <Badge
          v-else-if="runnerState === 'stopped'"
          variant="warning"
          :title="t('dataQuality.agent.runnerStoppedHint')"
        >
          {{ t("dataQuality.agent.runnerStopped") }}
        </Badge>
        <Badge
          v-else-if="runnerState === 'disabled'"
          variant="neutral"
        >
          {{ t("dataQuality.agent.runnerDisabled") }}
        </Badge>
        <Badge
          v-else
          variant="outline"
          :title="runnerError"
        >
          {{ t("dataQuality.agent.runnerUnknown") }}
        </Badge>

        <template v-if="runner">
          <Badge
            :variant="runner.autonomyEnabled ? 'secondary' : 'neutral'"
          >
            {{
              runner.autonomyEnabled
                ? t("dataQuality.agent.autonomyOn")
                : t("dataQuality.agent.autonomyOff")
            }}
          </Badge>
          <span class="text-xs text-muted-foreground">
            {{ t("dataQuality.agent.runningSessions", { count: runner.runningSessions }) }}
          </span>
          <span class="text-xs text-muted-foreground">
            {{ t("dataQuality.agent.idleSessions", { count: runner.idleSessions }) }}
          </span>
          <span class="text-xs text-muted-foreground">
            {{ t("dataQuality.agent.blockedSessions", { count: runner.blockedSessions }) }}
          </span>
          <span class="text-xs text-muted-foreground">
            {{ t("dataQuality.agent.tokensLastHour") }}:
            <span class="tabular-nums text-foreground">
              {{ formatTokens(runner.tokensLastHour, locale) }}
              / {{ formatTokens(runner.tokensPerHourLimit, locale) }}
            </span>
          </span>
          <span class="text-xs text-muted-foreground">
            {{ t("dataQuality.agent.wakesLastHour") }}:
            <span class="tabular-nums text-foreground">
              {{ runner.wakesLastHour }} / {{ runner.wakesPerHourLimit }}
            </span>
          </span>
          <span class="text-xs text-muted-foreground">
            {{ t("dataQuality.agent.lastActivity") }}:
            <span
              class="text-foreground"
              :title="formatDateTime(runner.lastActivityTime, locale)"
            >
              {{ runnerLastActivityLabel }}
            </span>
          </span>
        </template>
      </CardContent>
    </Card>

    <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <StatCard
        v-for="card in statCards"
        :key="card.state"
        :label="t(agentSessionStateKey(card.state))"
        :value="stateCount(card.state)"
        :icon="card.icon"
        :tone="card.tone"
      />
    </div>

    <Card>
      <div class="flex flex-wrap items-end gap-3 border-b p-4">
        <div class="flex min-w-48 flex-1 flex-col gap-1.5 sm:max-w-sm sm:flex-none">
          <Label
            for="quality-agent-search"
            class="sr-only"
          >
            {{ t("dataQuality.search") }}
          </Label>
          <Input
            id="quality-agent-search"
            v-model="search"
            class="w-full"
            :placeholder="t('dataQuality.agent.targetSearch')"
            @keyup.enter="applySearch"
          />
        </div>
        <div class="flex min-w-40 flex-col gap-1.5 [&>select]:hidden sm:ml-auto sm:w-44">
          <Label
            for="quality-agent-state"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.agent.filterState") }}
          </Label>
          <Select
            :model-value="stateFilter"
            @update:model-value="changeState"
          >
            <SelectTrigger
              id="quality-agent-state"
              class="w-full"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="option in stateOptions"
                :key="option.value"
                :value="option.value"
              >
                {{ option.label }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <Alert
        v-if="createError"
        variant="destructive"
        class="m-4"
      >
        <AlertTriangle class="h-4 w-4" />
        <AlertDescription>{{ createError }}</AlertDescription>
      </Alert>

      <PageState
        :loading="isLoading && sessions.length === 0"
        :error="loadError"
        :title="t('dataQuality.agent.loadFailed')"
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
          v-if="filtered.length === 0 && hasActiveFilters"
          :icon="SearchX"
          :title="t('dataQuality.agent.emptyFilteredTitle')"
          :description="t('dataQuality.agent.emptyFilteredDescription')"
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
          v-else-if="runnerState === 'disabled'"
          :icon="PowerOff"
          :title="t('dataQuality.agent.emptyDisabledTitle')"
          :description="t('dataQuality.agent.emptyDisabledDescription')"
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
          v-else-if="sessions.length === 0"
          :icon="Bot"
          :title="t('dataQuality.agent.emptyTitle')"
          :description="t('dataQuality.agent.emptyDescription')"
        >
          <Button
            v-if="canUpdate"
            @click="pickerOpen = true"
          >
            <Plus class="mr-2 h-4 w-4" />
            {{ t("dataQuality.agent.startFromTable") }}
          </Button>
          <Button
            variant="outline"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualityTargets' }">
              {{ t("dataQuality.agent.browseTargets") }}
            </RouterLink>
          </Button>
        </EmptyState>

        <Table
          v-else
          class="[&_th]:whitespace-nowrap"
        >
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("dataQuality.agent.columnTarget") }}</TableHead>
              <TableHead>{{ t("dataQuality.agent.columnState") }}</TableHead>
              <TableHead class="hidden text-right sm:table-cell">
                {{ t("dataQuality.agent.columnWakes") }}
              </TableHead>
              <TableHead class="hidden md:table-cell">
                {{ t("dataQuality.agent.columnLastActivity") }}
              </TableHead>
              <TableHead class="hidden text-right lg:table-cell">
                {{ t("dataQuality.agent.columnTokens") }}
              </TableHead>
              <TableHead class="hidden lg:table-cell">
                {{ t("dataQuality.agent.columnNextWake") }}
              </TableHead>
              <TableHead class="hidden xl:table-cell">
                {{ t("dataQuality.agent.columnBlocked") }}
              </TableHead>
              <TableHead class="w-12 text-right">
                <span class="sr-only">{{ t("common.actions") }}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="session in filtered"
              :key="session.name"
              class="cursor-pointer"
              @click="openSession(session)"
            >
              <TableCell>
                <div class="flex max-w-[14rem] flex-col gap-0.5 md:max-w-md">
                  <RouterLink
                    :to="targetRoute(session)"
                    class="truncate font-medium hover:underline"
                    :title="agentSessionTargetPath(session)"
                    @click.stop
                  >
                    {{ agentSessionTargetName(session) }}
                  </RouterLink>
                  <span class="truncate font-mono text-xs text-muted-foreground">
                    {{ agentSessionTargetPath(session) }}
                  </span>
                </div>
              </TableCell>
              <TableCell>
                <div class="flex flex-wrap items-center gap-1.5">
                  <Badge :variant="agentSessionStateVariant(session.state)">
                    {{ t(agentSessionStateKey(session.state)) }}
                  </Badge>
                  <span
                    v-if="heartbeatOf(session).running"
                    class="inline-flex items-center gap-1 text-xs text-info"
                  >
                    <span class="h-1.5 w-1.5 rounded-full bg-info" />
                    {{ t("dataQuality.agent.running") }}
                  </span>
                  <Badge
                    v-if="heartbeatOf(session).stale"
                    variant="warning"
                    :title="t('dataQuality.agent.possiblyStaleHint')"
                  >
                    {{ t("dataQuality.agent.possiblyStale") }}
                  </Badge>
                </div>
              </TableCell>
              <TableCell class="hidden text-right tabular-nums sm:table-cell">
                {{ session.wakeCount }}
              </TableCell>
              <TableCell class="hidden whitespace-nowrap md:table-cell">
                <span
                  v-if="session.lastActivityTime"
                  :title="formatDateTime(session.lastActivityTime, locale)"
                >
                  {{ formatRelativeTime(session.lastActivityTime, locale) }}
                </span>
                <span
                  v-else
                  class="text-muted-foreground"
                >
                  {{ t("dataQuality.agent.noActivity") }}
                </span>
              </TableCell>
              <TableCell class="hidden text-right tabular-nums lg:table-cell">
                {{ formatTokens(session.tokensUsed, locale) }}
              </TableCell>
              <TableCell class="hidden whitespace-nowrap lg:table-cell">
                <span
                  v-if="session.nextWakeTime"
                  :title="formatDateTime(session.nextWakeTime, locale)"
                >
                  {{ formatRelativeTime(session.nextWakeTime, locale) }}
                </span>
                <span
                  v-else
                  class="text-muted-foreground"
                >
                  {{ t("dataQuality.agent.noNextWake") }}
                </span>
              </TableCell>
              <TableCell class="hidden max-w-[16rem] xl:table-cell">
                <template v-if="session.blockedCode || session.blockedReason">
                  <Badge variant="warning">
                    {{ session.blockedCode || t("dataQuality.agent.state.blocked") }}
                  </Badge>
                  <p
                    v-if="session.blockedReason"
                    class="mt-1 line-clamp-2 text-xs text-muted-foreground"
                    :title="session.blockedReason"
                  >
                    {{ session.blockedReason }}
                  </p>
                </template>
                <span
                  v-else
                  class="text-muted-foreground"
                >-</span>
              </TableCell>
              <TableCell
                class="text-right"
                @click.stop
              >
                <div class="flex items-center justify-end gap-1">
                  <AgentSessionActions
                    :session="session"
                    :can-update="canUpdate"
                    @updated="onUpdated"
                    @archived="load()"
                    @reload="load()"
                  />
                  <Button
                    variant="ghost"
                    size="icon"
                    class="h-8 w-8"
                    as-child
                  >
                    <RouterLink
                      :to="sessionRoute(session)"
                      :title="t('dataQuality.agent.openConversation')"
                      :aria-label="t('dataQuality.agent.openConversation')"
                    >
                      <ChevronRight class="h-4 w-4" />
                    </RouterLink>
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </PageState>
      <p
        v-if="sessions.length > 0 && filtered.length > 0"
        class="border-t px-4 py-2 text-xs text-muted-foreground"
      >
        {{ t("dataQuality.agent.sessionCount", { count: filtered.length }) }}
      </p>
    </Card>

    <TargetPicker
      :open="pickerOpen"
      @update:open="pickerOpen = $event"
      @select="onTargetPicked"
    />
  </div>
</template>

<script setup lang="ts">
import {
  AlertTriangle,
  Archive,
  Bot,
  CheckCircle2,
  ChevronRight,
  OctagonAlert,
  PauseCircle,
  Plus,
  PowerOff,
  RefreshCcw,
  SearchX,
} from "lucide-vue-next";
import type { Component } from "vue";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { type LocationQuery, RouterLink, useRouter } from "vue-router";
import {
  createQualityAgentSession,
  getQualityAgentMetrics,
  getQualityAgentRunnerStatus,
  listQualityAgentSessions,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import StatCard, { type StatCardTone } from "@/components/common/StatCard.vue";
import AgentSessionActions from "@/components/data-quality/AgentSessionActions.vue";
import TargetPicker from "@/components/data-quality/TargetPicker.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
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
import { usePolling } from "@/composables/usePolling";
import { useUrlFilters } from "@/composables/useUrlFilters";
import { notify } from "@/lib/notify";
import {
  type QualityAgentMetrics,
  type QualityAgentRunnerStatus,
  type QualityAgentSession,
  QualityAgentSessionState,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  agentSessionHeartbeat,
  agentSessionMatchesSearch,
  agentSessionStateKey,
  agentSessionStateVariant,
  agentSessionTargetName,
  agentSessionTargetPath,
  formatTokens,
  runnerIsTicking,
} from "@/utils/agentSession";
import type { TargetSelection } from "@/utils/dataQualityForm";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { isAborted } from "@/utils/error";
import { guidToRouteParams } from "@/utils/guid";
import { queryString } from "@/utils/queryFilters";

/**
 * The workspace's quality agent sessions: one long-lived conversation per table
 * in the runner's scope.
 *
 * The list is the administrator's view — what is running, what is blocked, what
 * it spends and when it next wakes — while each row opens the transcript itself.
 * A live runner is polled every five seconds; an idle or disabled runner is not,
 * and a hidden tab pauses the poll through `usePolling`.
 */

type AgentStateFilter = "all" | "enabled" | "paused" | "blocked" | "archived";

interface SessionFilters {
  state: AgentStateFilter;
  search: string;
}

const FILTER_QUERY_KEYS = ["state", "q"] as const;
const POLL_INTERVAL_MS = 5000;

const { t, locale } = useI18n();
const router = useRouter();
const { formatError } = useErrorMessage();
const { canUpdate } = useDataQualityPermissions();

const sessions = ref<QualityAgentSession[]>([]);
const runner = ref<QualityAgentRunnerStatus>();
const metrics = ref<QualityAgentMetrics>();
const isLoading = ref(false);
const loadError = ref("");
const runnerError = ref("");
const createError = ref("");
const pickerOpen = ref(false);
const isCreating = ref(false);

let loadGeneration = 0;

// ---- filters ----

function parseFilters(query: LocationQuery): SessionFilters {
  const state = queryString(query.state);
  return {
    state:
      state === "enabled" ||
      state === "paused" ||
      state === "blocked" ||
      state === "archived"
        ? state
        : "all",
    search: queryString(query.q).trim(),
  };
}

function serializeFilters(filters: SessionFilters): Record<string, string> {
  const owned: Record<string, string> = {};
  if (filters.state !== "all") {
    owned.state = filters.state;
  }
  if (filters.search) {
    owned.q = filters.search;
  }
  return owned;
}

const urlFilters = useUrlFilters<SessionFilters>({
  keys: FILTER_QUERY_KEYS,
  parse: parseFilters,
  serialize: serializeFilters,
});

const initialFilters = urlFilters.read();
const stateFilter = ref<AgentStateFilter>(initialFilters.state);
const search = ref(initialFilters.search);
/** The term the rows are filtered by, which only changes on Enter. */
const appliedSearch = ref(initialFilters.search);

const selectedState = computed<QualityAgentSessionState>(() => {
  switch (stateFilter.value) {
    case "enabled":
      return QualityAgentSessionState.ENABLED;
    case "paused":
      return QualityAgentSessionState.PAUSED;
    case "blocked":
      return QualityAgentSessionState.BLOCKED;
    case "archived":
      return QualityAgentSessionState.ARCHIVED;
    default:
      return QualityAgentSessionState.UNSPECIFIED;
  }
});

const stateOptions = computed<
  Array<{ value: AgentStateFilter; label: string }>
>(() => [
  { value: "all", label: t("dataQuality.agent.allStates") },
  {
    value: "enabled",
    label: t(agentSessionStateKey(QualityAgentSessionState.ENABLED)),
  },
  {
    value: "paused",
    label: t(agentSessionStateKey(QualityAgentSessionState.PAUSED)),
  },
  {
    value: "blocked",
    label: t(agentSessionStateKey(QualityAgentSessionState.BLOCKED)),
  },
  {
    value: "archived",
    label: t(agentSessionStateKey(QualityAgentSessionState.ARCHIVED)),
  },
]);

const hasActiveFilters = computed(
  () => stateFilter.value !== "all" || appliedSearch.value !== ""
);

const filtered = computed(() =>
  sessions.value.filter((session) =>
    agentSessionMatchesSearch(session, appliedSearch.value)
  )
);

function currentFilters(): SessionFilters {
  return { state: stateFilter.value, search: appliedSearch.value };
}

function changeState(value: unknown) {
  const next = String(value);
  stateFilter.value =
    next === "enabled" ||
    next === "paused" ||
    next === "blocked" ||
    next === "archived"
      ? next
      : "all";
  urlFilters.write(currentFilters());
  void load();
}

function applySearch() {
  appliedSearch.value = search.value.trim();
  urlFilters.write(currentFilters());
}

function clearFilters() {
  stateFilter.value = "all";
  search.value = "";
  appliedSearch.value = "";
  urlFilters.write(currentFilters());
  void load();
}

urlFilters.watchExternal((filters) => {
  const stateChanged = filters.state !== stateFilter.value;
  stateFilter.value = filters.state;
  search.value = filters.search;
  appliedSearch.value = filters.search;
  if (stateChanged) {
    void load();
  }
});

// ---- runner status ----

const runnerState = computed<"ticking" | "stopped" | "disabled" | "unknown">(
  () => {
    if (!runner.value) {
      return "unknown";
    }
    if (!runner.value.enabled) {
      return "disabled";
    }
    return runnerIsTicking(runner.value.runnerLastTickTime)
      ? "ticking"
      : "stopped";
  }
);

const runnerLastActivityLabel = computed(() =>
  runner.value?.lastActivityTime
    ? formatRelativeTime(runner.value.lastActivityTime, locale.value)
    : t("dataQuality.agent.noActivity")
);

// ---- counts ----

/**
 * Per-state session counts, from the runner metrics only.
 *
 * The directory is filtered (state and target search), so counting its rows
 * would report the current filter's view as if it were the whole workspace.
 * Without metrics the cards show "—" rather than a wrong number.
 */
const stateCounts = computed(
  (): Map<QualityAgentSessionState, number> | undefined => {
    if (!metrics.value) {
      return undefined;
    }
    const counts = new Map<QualityAgentSessionState, number>();
    for (const count of metrics.value.sessions) {
      counts.set(count.state, count.count);
    }
    return counts;
  }
);

interface AgentStatCard {
  state: QualityAgentSessionState;
  icon: Component;
  tone: StatCardTone;
}

const statCards: AgentStatCard[] = [
  {
    state: QualityAgentSessionState.ENABLED,
    icon: CheckCircle2,
    tone: "success",
  },
  { state: QualityAgentSessionState.PAUSED, icon: PauseCircle, tone: "info" },
  {
    state: QualityAgentSessionState.BLOCKED,
    icon: OctagonAlert,
    tone: "warning",
  },
  { state: QualityAgentSessionState.ARCHIVED, icon: Archive, tone: "primary" },
];

function stateCount(state: QualityAgentSessionState): string | number {
  const counts = stateCounts.value;
  return counts ? (counts.get(state) ?? 0) : "—";
}

// ---- loading ----

const shouldPoll = computed(
  () =>
    runner.value?.enabled === true ||
    sessions.value.some(
      (session) =>
        session.running || session.state === QualityAgentSessionState.ENABLED
    )
);

const polling = usePolling((signal) => load(true, signal), {
  intervalMs: POLL_INTERVAL_MS,
  shouldContinue: () => shouldPoll.value,
  continueOnError: true,
});

async function loadRunner(signal?: AbortSignal) {
  try {
    runner.value = await getQualityAgentRunnerStatus(signal);
    runnerError.value = "";
  } catch (e: unknown) {
    if (isAborted(e)) {
      return;
    }
    runner.value = undefined;
    runnerError.value = formatError(e, "dataQuality.agent.runnerStatusFailed");
  }
}

async function loadMetrics(signal?: AbortSignal) {
  try {
    metrics.value = await getQualityAgentMetrics(signal);
  } catch {
    // The list still renders; the state cards show "—" instead of counts.
    metrics.value = undefined;
  }
}

async function load(silent = false, signal?: AbortSignal) {
  const mine = ++loadGeneration;
  if (!silent) {
    isLoading.value = true;
    loadError.value = "";
  }
  try {
    await Promise.all([loadRunner(signal), loadMetrics(signal)]);
    const next = await listQualityAgentSessions({
      state: selectedState.value,
      signal,
    });
    if (mine !== loadGeneration || signal?.aborted) {
      return;
    }
    sessions.value = next;
  } catch (e: unknown) {
    if (mine !== loadGeneration || isAborted(e)) {
      return;
    }
    if (!silent) {
      loadError.value = formatError(e, "dataQuality.agent.loadFailed");
    }
  } finally {
    if (mine === loadGeneration) {
      isLoading.value = false;
    }
  }
  if (shouldPoll.value) {
    polling.start();
  } else {
    polling.stop();
  }
}

// ---- row actions ----

function heartbeatOf(session: QualityAgentSession) {
  return agentSessionHeartbeat(session);
}

function sessionRoute(session: QualityAgentSession) {
  return {
    name: "DataQualityAgentSession",
    params: { sessionId: session.name.split("/") },
  };
}

function targetRoute(session: QualityAgentSession) {
  return {
    name: "DataQualityTarget",
    params: {
      targetGuid: guidToRouteParams(session.target?.metaGuid ?? ""),
    },
    query: {
      metaType: String(session.target?.metaType ?? MetaType.UNSPECIFIED),
    },
  };
}

function openSession(session: QualityAgentSession) {
  void router.push(sessionRoute(session));
}

function onUpdated(updated: QualityAgentSession) {
  const index = sessions.value.findIndex(
    (session) => session.name === updated.name
  );
  if (index >= 0) {
    sessions.value[index] = updated;
  }
}

async function onTargetPicked(selection: TargetSelection) {
  if (isCreating.value) {
    return;
  }
  isCreating.value = true;
  createError.value = "";
  try {
    const created = await createQualityAgentSession({
      targetGuid: selection.guid,
      metaType: selection.metaType,
    });
    notify.success(t("dataQuality.agent.started"));
    await router.push(sessionRoute(created));
  } catch (e: unknown) {
    createError.value = formatError(e, "dataQuality.agent.createFailed");
  } finally {
    isCreating.value = false;
  }
}

onMounted(() => {
  void load();
});
</script>
