<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('qualityAgent.teamTitle')"
      :description="t('qualityAgent.teamPageHint')"
    >
      <template #breadcrumb>
        <QualityAgentBreadcrumb
          :agent-name="agentName"
          :agent-label="agent?.displayName || agentId"
          variant="sessions"
        />
      </template>
      <template #actions>
        <Button
          variant="outline"
          size="sm"
          :disabled="isLoading"
          @click="refresh"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': isLoading }"
          />
          {{ t("common.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <QualityAgentTabs
      :agent-name="agentName"
      active="sessions"
    />

    <Alert
      v-if="agentError"
      variant="destructive"
    >
      <AlertTriangle class="h-4 w-4" />
      <AlertDescription>{{ agentError }}</AlertDescription>
    </Alert>

    <Card>
      <div class="flex flex-wrap items-end gap-3 border-b p-4">
        <div class="flex min-w-40 flex-col gap-1.5 sm:w-44">
          <Label
            for="quality-agent-session-state"
            class="text-sm font-medium leading-none"
          >
            {{ t("qualityAgent.filterState") }}
          </Label>
          <Select v-model="stateFilter">
            <SelectTrigger
              id="quality-agent-session-state"
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
        <div class="flex min-w-36 flex-col gap-1.5 sm:w-40">
          <Label
            for="quality-agent-session-mode"
            class="text-sm font-medium leading-none"
          >
            {{ t("qualityAgent.filterMode") }}
          </Label>
          <Select v-model="modeFilter">
            <SelectTrigger
              id="quality-agent-session-mode"
              class="w-full"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="option in modeOptions"
                :key="option.value"
                :value="option.value"
              >
                {{ option.label }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div class="flex min-w-44 flex-col gap-1.5 sm:w-56">
          <Label
            for="quality-agent-session-database"
            class="text-sm font-medium leading-none"
          >
            {{ t("qualityAgent.filterDatabase") }}
          </Label>
          <Select v-model="databaseFilter">
            <SelectTrigger
              id="quality-agent-session-database"
              class="w-full"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("qualityAgent.allDatabases") }}
              </SelectItem>
              <SelectItem
                v-for="database in databaseOptions"
                :key="database.value"
                :value="database.value"
              >
                {{ database.label }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <PageState
        :loading="!initialized || (isLoading && sessions.length === 0)"
        :error="loadError"
        :title="t('qualityAgent.sessionsLoadFailed')"
      >
        <template #error>
          <Button
            variant="outline"
            size="sm"
            @click="refresh"
          >
            {{ t("common.refresh") }}
          </Button>
        </template>

        <div
          v-if="sessions.length === 0"
          class="p-8 text-center"
        >
          <p class="font-medium">{{ t("qualityAgent.sessionsEmptyTitle") }}</p>
          <p class="mt-1 text-sm text-muted-foreground">
            {{ t("qualityAgent.sessionsEmptyHint") }}
          </p>
        </div>
        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("qualityAgent.subagentName") }}</TableHead>
              <TableHead class="hidden md:table-cell">{{ t("qualityAgent.sessionMode") }}</TableHead>
              <TableHead>{{ t("qualityAgent.sessionState") }}</TableHead>
              <TableHead class="hidden lg:table-cell">{{ t("qualityAgent.sessionLastActivity") }}</TableHead>
              <TableHead class="hidden lg:table-cell">{{ t("qualityAgent.sessionNextWake") }}</TableHead>
              <TableHead class="hidden xl:table-cell">{{ t("qualityAgent.sessionWakes") }}</TableHead>
              <TableHead class="text-right">{{ t("common.actions") }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="session in sessions"
              :key="session.name"
            >
              <TableCell class="max-w-[24rem]">
                <RouterLink
                  :to="sessionRoute(session)"
                  class="font-medium hover:underline"
                >
                  {{ agentSessionDisplayName(session, t("qualityAgent.roleLead")) }}
                </RouterLink>
                <p class="truncate text-xs text-muted-foreground">
                  {{ agentSessionTargetName(session) }} · {{ databaseLabel(session) }} ·
                  {{ agentSessionTargetPath(session) }}
                </p>
              </TableCell>
              <TableCell class="hidden md:table-cell">
                <Badge :variant="agentSessionModeVariant(session.mode)">
                  {{ t(agentSessionModeKey(session.mode)) }}
                </Badge>
              </TableCell>
              <TableCell>
                <div class="flex flex-wrap items-center gap-1">
                  <Badge :variant="agentSessionStateVariant(session.state)">
                    {{ t(agentSessionStateKey(session.state)) }}
                  </Badge>
                  <Badge
                    v-if="heartbeat(session).running"
                    variant="info"
                  >
                    {{ t("qualityAgent.sessionRunning") }}
                  </Badge>
                  <Badge
                    v-if="heartbeat(session).stale"
                    variant="warning"
                    :title="t('qualityAgent.sessionStaleHint')"
                  >
                    {{ t("qualityAgent.sessionStale") }}
                  </Badge>
                </div>
              </TableCell>
              <TableCell class="hidden text-xs text-muted-foreground lg:table-cell">
                <span
                  v-if="session.lastActivityTime"
                  :title="formatDateTime(session.lastActivityTime, locale)"
                >
                  {{ formatRelativeTime(session.lastActivityTime, locale) }}
                </span>
                <span v-else>-</span>
              </TableCell>
              <TableCell class="hidden text-xs text-muted-foreground lg:table-cell">
                <span
                  v-if="session.nextWakeTime"
                  :title="formatDateTime(session.nextWakeTime, locale)"
                >
                  {{ formatRelativeTime(session.nextWakeTime, locale) }}
                </span>
                <span v-else>-</span>
              </TableCell>
              <TableCell class="hidden text-xs tabular-nums text-muted-foreground xl:table-cell">
                {{ session.wakeCount }}
              </TableCell>
              <TableCell class="text-right">
                <Button
                  variant="ghost"
                  size="sm"
                  as-child
                >
                  <RouterLink :to="sessionRoute(session)">
                    {{ t("qualityAgent.openSession") }}
                  </RouterLink>
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>

        <div
          v-if="sessions.length > 0"
          class="flex justify-end gap-2 px-6 pb-6"
        >
          <Button
            variant="outline"
            size="sm"
            :disabled="!hasPrevious || isLoading"
            @click="goPrevious"
          >
            {{ t("common.previous") }}
          </Button>
          <Button
            variant="outline"
            size="sm"
            :disabled="!hasNext || isLoading"
            @click="goNext"
          >
            {{ t("common.next") }}
          </Button>
        </div>
      </PageState>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { AlertTriangle, RefreshCcw } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import {
  getQualityAgent,
  listQualityAgentSessionsPage,
} from "@/api/data-quality";
import PageState from "@/components/common/PageState.vue";
import QualityAgentBreadcrumb from "@/components/data-quality/QualityAgentBreadcrumb.vue";
import QualityAgentTabs from "@/components/data-quality/QualityAgentTabs.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
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
import { useErrorMessage } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import {
  type QualityAgent,
  type QualityAgentSession,
  QualityAgentSessionMode,
  QualityAgentSessionRole,
  QualityAgentSessionState,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import {
  qualityAgentName,
  qualityAgentSessionRoute,
} from "@/utils/qualityAgent";
import {
  agentSessionDisplayName,
  agentSessionHeartbeat,
  agentSessionModeKey,
  agentSessionModeVariant,
  agentSessionStateKey,
  agentSessionStateVariant,
  agentSessionTargetName,
  agentSessionTargetPath,
} from "@/utils/qualityAgentSession";

const { t, locale } = useI18n();
const route = useRoute();
const { formatError } = useErrorMessage();

const workspace = computed(() => String(route.params.workspace));
const agentId = computed(() => String(route.params.agentId));
const agentName = computed(() =>
  qualityAgentName(workspace.value, agentId.value)
);

const agent = ref<QualityAgent>();
const error = ref("");
const agentError = ref("");
const initialized = ref(false);
const stateFilter = ref(String(QualityAgentSessionState.UNSPECIFIED));
const modeFilter = ref(String(QualityAgentSessionMode.UNSPECIFIED));
const databaseFilter = ref("-");

const databaseOptions = computed(() => {
  const seen = new Set<string>();
  const options: Array<{ value: string; label: string }> = [];
  for (const database of agent.value?.databases ?? []) {
    if (seen.has(database)) {
      continue;
    }
    seen.add(database);
    const parts = database.split("/");
    options.push({
      value: database,
      label: parts[parts.length - 1] || database,
    });
  }
  return options.sort((left, right) => left.label.localeCompare(right.label));
});

const stateOptions = computed(() => [
  {
    value: String(QualityAgentSessionState.UNSPECIFIED),
    label: t("qualityAgent.stateAll"),
  },
  {
    value: String(QualityAgentSessionState.ENABLED),
    label: t("qualityAgent.stateEnabled"),
  },
  {
    value: String(QualityAgentSessionState.BLOCKED),
    label: t("qualityAgent.stateBlocked"),
  },
  {
    value: String(QualityAgentSessionState.ARCHIVED),
    label: t("qualityAgent.stateArchived"),
  },
]);

const modeOptions = computed(() => [
  {
    value: String(QualityAgentSessionMode.UNSPECIFIED),
    label: t("qualityAgent.modeAll"),
  },
  {
    value: String(QualityAgentSessionMode.MONITOR),
    label: t("qualityAgent.modeMonitor"),
  },
  {
    value: String(QualityAgentSessionMode.TRIAGE),
    label: t("qualityAgent.modeTriage"),
  },
]);

const {
  items: sessions,
  isLoading,
  hasNext,
  hasPrevious,
  reset,
  refresh: refreshPage,
  goNext,
  goPrevious,
} = usePagedFetch<QualityAgentSession>({
  fetchPage: async (pageToken, signal) => {
    const page = await listQualityAgentSessionsPage({
      agent: agentName.value,
      role: QualityAgentSessionRole.SUBAGENT,
      state: Number(stateFilter.value) as QualityAgentSessionState,
      mode: Number(modeFilter.value) as QualityAgentSessionMode,
      database: databaseFilter.value === "-" ? "" : databaseFilter.value,
      pageSize: 50,
      pageToken,
      signal,
    });
    return { items: page.sessions, nextPageToken: page.nextPageToken };
  },
  onError: (err) => {
    error.value = formatError(err);
  },
});

const loadError = computed(() => error.value);

function heartbeat(session: QualityAgentSession) {
  return agentSessionHeartbeat(session);
}

function sessionRoute(session: QualityAgentSession) {
  return qualityAgentSessionRoute(agentName.value, session.name);
}

/** The database segment a session is scoped to, for the target subtitle. */
function databaseLabel(session: QualityAgentSession): string {
  const parts = session.database.split("/");
  return parts[parts.length - 1] || session.database || "-";
}

async function loadAgent() {
  agentError.value = "";
  try {
    agent.value = await getQualityAgent(agentName.value);
  } catch (err) {
    agentError.value = formatError(err);
  }
}

async function refresh() {
  error.value = "";
  agentError.value = "";
  await Promise.all([loadAgent(), refreshPage()]);
  initialized.value = true;
}

watch([stateFilter, modeFilter, databaseFilter], () => {
  error.value = "";
  void reset();
});

watch(agentName, async () => {
  agent.value = undefined;
  error.value = "";
  agentError.value = "";
  initialized.value = false;
  await Promise.all([loadAgent(), reset()]);
  initialized.value = true;
});

onMounted(async () => {
  await Promise.all([loadAgent(), reset()]);
  initialized.value = true;
});
</script>
