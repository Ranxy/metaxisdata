<template>
  <div class="space-y-4">
    <PageHeader
      :title="pageTitle"
      :description="session ? agentSessionTargetPath(session) : ''"
    >
      <template #breadcrumb>
        <QualityAgentBreadcrumb
          :agent-name="agentName"
          :agent-label="agentLabel"
          variant="session"
          :session-label="session ? agentSessionTargetName(session) : ''"
        />
      </template>
      <template #title-extra>
        <template v-if="session">
          <Badge :variant="agentSessionStateVariant(session.state)">
            {{ t(agentSessionStateKey(session.state)) }}
          </Badge>
          <Badge :variant="agentSessionModeVariant(session.mode)">
            {{ t(agentSessionModeKey(session.mode)) }}
          </Badge>
          <Badge
            v-if="heartbeat.running"
            variant="info"
          >
            {{ t("qualityAgent.sessionRunning") }}
          </Badge>
          <Badge
            v-if="heartbeat.stale"
            variant="warning"
            :title="t('qualityAgent.sessionStaleHint')"
          >
            {{ t("qualityAgent.sessionStale") }}
          </Badge>
        </template>
      </template>
      <template #actions>
        <Button
          v-if="targetRoute"
          variant="outline"
          size="sm"
          as-child
        >
          <RouterLink :to="targetRoute">
            <Target class="mr-2 h-4 w-4" />
            {{ t("qualityAgent.openTarget") }}
          </RouterLink>
        </Button>
        <Button
          variant="outline"
          size="sm"
          :disabled="isLoading"
          @click="reload"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': isLoading }"
          />
          {{ t("common.refresh") }}
        </Button>
        <Button
          v-if="session && canManage && session.state === QualityAgentSessionState.BLOCKED"
          size="sm"
          :disabled="resuming"
          @click="resume"
        >
          <Loader2
            v-if="resuming"
            class="mr-2 h-4 w-4 animate-spin"
          />
          {{ t("qualityAgent.resumeSession") }}
        </Button>
      </template>
    </PageHeader>

    <QualityAgentTabs
      :agent-name="agentName"
      active="sessions"
    />

    <Alert
      v-if="error"
      variant="destructive"
    >
      <AlertTriangle class="h-4 w-4" />
      <AlertDescription>{{ error }}</AlertDescription>
    </Alert>

    <PageState
      :loading="isLoading && !session"
      :error="loadError"
      :title="t('qualityAgent.sessionLoadFailed')"
    >
      <template #error>
        <Button
          variant="outline"
          size="sm"
          @click="reload"
        >
          {{ t("common.refresh") }}
        </Button>
      </template>

      <div
        v-if="session"
        class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem] lg:items-start"
      >
        <Card class="min-w-0">
          <CardHeader class="border-b">
            <CardTitle class="text-base">{{ t("qualityAgent.transcriptTitle") }}</CardTitle>
            <CardDescription>{{ t("qualityAgent.transcriptHint") }}</CardDescription>
          </CardHeader>
          <CardContent class="space-y-4 pt-4">
            <PageState
              :loading="messagesLoading && messages.length === 0"
              :error="messagesError"
              :title="t('qualityAgent.messagesLoadFailed')"
            >
              <template #error>
                <Button
                  variant="outline"
                  size="sm"
                  @click="loadMessages"
                >
                  {{ t("common.refresh") }}
                </Button>
              </template>

              <div
                v-if="hasEarlier"
                class="flex flex-col items-start gap-2"
              >
                <Button
                  variant="outline"
                  size="sm"
                  :disabled="loadingEarlier"
                  @click="loadEarlier"
                >
                  <Loader2
                    v-if="loadingEarlier"
                    class="mr-2 h-4 w-4 animate-spin"
                  />
                  {{
                    loadingEarlier
                      ? t("qualityAgent.loadingEarlier")
                      : t("qualityAgent.loadEarlier")
                  }}
                </Button>
                <p
                  v-if="loadEarlierError"
                  class="text-xs text-destructive"
                >
                  {{ loadEarlierError }}
                </p>
              </div>

              <p
                v-if="messages.length === 0"
                class="text-sm text-muted-foreground"
              >
                {{ t("qualityAgent.transcriptEmpty") }}
              </p>
              <QualityAgentTranscript
                v-else
                :groups="groups"
                :session-name="session.name"
              />
            </PageState>
          </CardContent>
        </Card>

        <Card>
          <CardHeader class="border-b">
            <CardTitle class="text-base">{{ t("qualityAgent.runPanelTitle") }}</CardTitle>
          </CardHeader>
          <CardContent class="space-y-3 pt-4">
            <div class="flex items-center gap-2 text-sm">
              <span
                class="h-2 w-2 shrink-0 rounded-full"
                :class="heartbeat.running ? 'bg-info' : 'bg-muted-foreground/40'"
              />
              <span>{{ statusLabel }}</span>
              <span
                v-if="currentTurn !== undefined"
                class="ml-auto text-xs tabular-nums text-muted-foreground"
              >
                {{ t("qualityAgent.turn", { turn: currentTurn }) }}
              </span>
            </div>

            <dl class="grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
              <dt class="text-muted-foreground">{{ t("qualityAgent.sessionWakes") }}</dt>
              <dd class="text-right tabular-nums">{{ session.wakeCount }}</dd>
              <dt class="text-muted-foreground">{{ t("qualityAgent.sessionNextWake") }}</dt>
              <dd
                class="text-right tabular-nums"
                :title="formatDateTime(session.nextWakeTime, locale)"
              >
                {{ nextWakeLabel }}
              </dd>
              <dt class="text-muted-foreground">{{ t("qualityAgent.sessionLastActivity") }}</dt>
              <dd
                class="text-right tabular-nums"
                :title="formatDateTime(session.lastActivityTime, locale)"
              >
                {{ lastActivityLabel }}
              </dd>
              <dt class="text-muted-foreground">{{ t("qualityAgent.sessionRestarts") }}</dt>
              <dd class="text-right tabular-nums">{{ session.restartCount }}</dd>
            </dl>

            <p
              v-if="pollError"
              class="text-xs text-destructive"
            >
              {{ pollError }}
            </p>

            <div
              v-if="heartbeat.stale"
              class="rounded-md border border-warning/40 bg-warning-subtle/30 px-2.5 py-1.5 text-xs text-warning"
            >
              {{ t("qualityAgent.sessionStaleHint") }}
            </div>

            <div
              v-if="session.blockedCode || session.blockedReason"
              class="space-y-1 rounded-md border border-warning/40 bg-warning-subtle/30 px-2.5 py-1.5 text-xs"
            >
              <p class="font-medium text-warning">
                {{ t("qualityAgent.sessionBlockedCode") }}:
                <span class="font-mono">{{ session.blockedCode || "-" }}</span>
              </p>
              <p
                v-if="session.blockedReason"
                class="text-warning"
              >
                {{ session.blockedReason }}
              </p>
            </div>
          </CardContent>
        </Card>
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import { AlertTriangle, Loader2, RefreshCcw, Target } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import {
  getQualityAgent,
  getQualityAgentSession,
  listQualityAgentSessionMessages,
  resumeQualityAgentSession,
} from "@/api/data-quality";
import PageState from "@/components/common/PageState.vue";
import QualityAgentBreadcrumb from "@/components/data-quality/QualityAgentBreadcrumb.vue";
import QualityAgentTabs from "@/components/data-quality/QualityAgentTabs.vue";
import QualityAgentTranscript from "@/components/data-quality/QualityAgentTranscript.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
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
import { usePolling } from "@/composables/usePolling";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityAgent,
  QualityAgentMessageRole,
  type QualityAgentSession,
  type QualityAgentSessionMessage,
  QualityAgentSessionState,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";
import {
  qualityAgentName,
  qualityAgentSessionName,
} from "@/utils/qualityAgent";
import {
  agentSessionHeartbeat,
  agentSessionModeKey,
  agentSessionModeVariant,
  agentSessionStateKey,
  agentSessionStateVariant,
  agentSessionTargetName,
  agentSessionTargetPath,
  groupMessagesByWake,
  lastTranscriptSeq,
  mergeTranscriptMessages,
} from "@/utils/qualityAgentSession";

const RUNNING_POLL_INTERVAL_MS = 2000;
const IDLE_POLL_INTERVAL_MS = 10_000;
const MESSAGE_PAGE_SIZE = 50;
const POLL_PAGE_SIZE = 200;

const { t, locale } = useI18n();
const route = useRoute();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const session = ref<QualityAgentSession>();
const agent = ref<QualityAgent>();
const messages = ref<QualityAgentSessionMessage[]>([]);
const hasEarlier = ref(false);
const isLoading = ref(false);
const pollError = ref("");
const messagesLoading = ref(false);
const loadError = ref("");
const messagesError = ref("");
const loadingEarlier = ref(false);
const loadEarlierError = ref("");
const error = ref("");
const resuming = ref(false);

const workspace = computed(() => String(route.params.workspace));
const agentId = computed(() => String(route.params.agentId));
const sessionId = computed(() => String(route.params.sessionId));
const agentName = computed(() =>
  qualityAgentName(workspace.value, agentId.value)
);
const sessionName = computed(() =>
  qualityAgentSessionName(workspace.value, sessionId.value)
);

const groups = computed(() => groupMessagesByWake(messages.value));
const lastSeq = computed(() => lastTranscriptSeq(messages.value));
const heartbeat = computed(() =>
  session.value
    ? agentSessionHeartbeat(session.value)
    : { running: false, stale: false }
);

const agentLabel = computed(() => agent.value?.displayName || agentId.value);

const canManage = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.agents.manage")
);

const pageTitle = computed(() =>
  session.value
    ? agentSessionTargetName(session.value)
    : t("qualityAgent.sessionDetailTitle")
);

const statusLabel = computed(() => {
  if (!session.value) {
    return "";
  }
  if (session.value.state === QualityAgentSessionState.ARCHIVED) {
    return t("qualityAgent.statusArchived");
  }
  if (session.value.state === QualityAgentSessionState.BLOCKED) {
    return t("qualityAgent.statusBlocked");
  }
  if (heartbeat.value.running) {
    return t("qualityAgent.statusRunning");
  }
  return t("qualityAgent.statusIdle");
});

const nextWakeLabel = computed(() =>
  session.value?.nextWakeTime
    ? formatRelativeTime(session.value.nextWakeTime, locale.value)
    : "-"
);

const lastActivityLabel = computed(() =>
  session.value?.lastActivityTime
    ? formatRelativeTime(session.value.lastActivityTime, locale.value)
    : "-"
);

const targetRoute = computed(() => {
  const target = session.value?.target;
  if (!target || !target.metaGuid) {
    return undefined;
  }
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.metaGuid) },
    query: { metaType: String(target.metaType ?? MetaType.UNSPECIFIED) },
  };
});

/** The newest "model turn N started" note, while a wake is in flight. */
const currentTurn = computed((): number | undefined => {
  if (!heartbeat.value.running) {
    return undefined;
  }
  for (let index = messages.value.length - 1; index >= 0; index -= 1) {
    const message = messages.value[index];
    if (message.role !== QualityAgentMessageRole.ENGINE) {
      continue;
    }
    const match = /^model turn (\d+) started/.exec(message.content.trim());
    if (match) {
      return Number(match[1]);
    }
  }
  return undefined;
});

async function loadAgent() {
  try {
    agent.value = await getQualityAgent(agentName.value);
  } catch {
    // The breadcrumb falls back to the agent id; a failed metadata read must not
    // block the session itself.
    agent.value = undefined;
  }
}

async function loadSession(signal?: AbortSignal) {
  const next = await getQualityAgentSession(sessionName.value, signal);
  if (!signal?.aborted) {
    session.value = next;
  }
}

async function loadMessages(signal?: AbortSignal) {
  messagesLoading.value = true;
  messagesError.value = "";
  try {
    const page = await listQualityAgentSessionMessages(sessionName.value, {
      pageSize: MESSAGE_PAGE_SIZE,
      signal,
    });
    if (signal?.aborted) {
      return;
    }
    messages.value = page.messages;
    hasEarlier.value = page.nextPageToken !== "";
  } catch (err) {
    if (!signal?.aborted) {
      messagesError.value = formatError(err);
    }
  } finally {
    messagesLoading.value = false;
  }
}

async function load() {
  if (disposed) {
    return;
  }
  isLoading.value = true;
  loadError.value = "";
  error.value = "";
  try {
    await Promise.all([loadAgent(), loadSession(), loadMessages()]);
  } catch (err) {
    loadError.value = formatError(err);
  } finally {
    isLoading.value = false;
  }
}

async function reload() {
  await load();
  syncPolling();
}

async function loadEarlier() {
  if (!hasEarlier.value || loadingEarlier.value) {
    return;
  }
  const oldest = messages.value[0]?.seq ?? 0n;
  if (oldest === 0n) {
    hasEarlier.value = false;
    return;
  }
  loadingEarlier.value = true;
  loadEarlierError.value = "";
  try {
    const older = await listQualityAgentSessionMessages(sessionName.value, {
      pageSize: MESSAGE_PAGE_SIZE,
      beforeSeq: oldest,
    });
    if (older.messages.length === 0) {
      hasEarlier.value = false;
      return;
    }
    messages.value = mergeTranscriptMessages(older.messages, messages.value);
    hasEarlier.value = older.messages.length >= MESSAGE_PAGE_SIZE;
  } catch (err) {
    loadEarlierError.value = formatError(err);
  } finally {
    loadingEarlier.value = false;
  }
}

/** Append everything newer than the watermark, including a burst larger than one page. */
async function poll(signal: AbortSignal) {
  pollError.value = "";
  await loadSession(signal);
  let after = lastSeq.value;
  for (;;) {
    const page = await listQualityAgentSessionMessages(sessionName.value, {
      afterSeq: after,
      pageSize: POLL_PAGE_SIZE,
      signal,
    });
    if (signal.aborted || page.messages.length === 0) {
      return;
    }
    messages.value = mergeTranscriptMessages(messages.value, page.messages);
    after = lastTranscriptSeq(page.messages);
    if (page.messages.length < POLL_PAGE_SIZE) {
      return;
    }
  }
}

const fastPolling = usePolling(poll, {
  intervalMs: RUNNING_POLL_INTERVAL_MS,
  shouldContinue: () => heartbeat.value.running,
  continueOnError: true,
  onError: (err) => {
    pollError.value = formatError(err);
  },
});

const idlePolling = usePolling(poll, {
  intervalMs: IDLE_POLL_INTERVAL_MS,
  shouldContinue: () =>
    session.value !== undefined &&
    session.value.state !== QualityAgentSessionState.ARCHIVED,
  continueOnError: true,
  onError: (err) => {
    pollError.value = formatError(err);
  },
});

let disposed = false;

function syncPolling() {
  if (disposed || !session.value) {
    return;
  }
  if (heartbeat.value.running) {
    idlePolling.stop();
    fastPolling.start();
    return;
  }
  fastPolling.stop();
  if (session.value.state === QualityAgentSessionState.ARCHIVED) {
    idlePolling.stop();
    return;
  }
  idlePolling.start();
}

watch(
  () => session.value?.running,
  () => syncPolling()
);

async function resume() {
  if (!session.value || resuming.value) {
    return;
  }
  resuming.value = true;
  error.value = "";
  try {
    session.value = await resumeQualityAgentSession(
      session.value.name,
      session.value.revision
    );
    notify.success(t("qualityAgent.resumed"));
    syncPolling();
  } catch (err) {
    error.value = formatError(err);
  } finally {
    resuming.value = false;
  }
}

watch(sessionName, async () => {
  fastPolling.stop();
  idlePolling.stop();
  session.value = undefined;
  messages.value = [];
  hasEarlier.value = false;
  pollError.value = "";
  loadError.value = "";
  messagesError.value = "";
  await load();
  syncPolling();
});

onMounted(async () => {
  await load();
  syncPolling();
});

onBeforeUnmount(() => {
  disposed = true;
  fastPolling.stop();
  idlePolling.stop();
});
</script>
