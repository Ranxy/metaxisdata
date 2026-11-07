<template>
  <div class="space-y-4">
    <PageHeader :title="pageTitle">
      <template #breadcrumb>
        <RouterLink
          :to="{ name: 'DataQualityAgent' }"
          class="inline-flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft class="h-3.5 w-3.5" />
          {{ t("dataQuality.agent.title") }}
        </RouterLink>
      </template>
      <template #title-extra>
        <template v-if="session">
          <Badge :variant="agentSessionStateVariant(session.state)">
            {{ t(agentSessionStateKey(session.state)) }}
          </Badge>
          <Badge
            v-if="heartbeat.running"
            variant="info"
          >
            {{ t("dataQuality.agent.running") }}
          </Badge>
          <Badge
            v-if="heartbeat.stale"
            variant="warning"
            :title="t('dataQuality.agent.possiblyStaleHint')"
          >
            {{ t("dataQuality.agent.possiblyStale") }}
          </Badge>
        </template>
      </template>
      <template #description>
        <span
          v-if="session"
          class="font-mono text-xs"
        >{{ agentSessionTargetPath(session) }}</span>
      </template>
      <template #actions>
        <Button
          v-if="targetTo"
          variant="outline"
          as-child
        >
          <RouterLink :to="targetTo">
            <Target class="mr-2 h-4 w-4" />
            {{ t("dataQuality.agent.openTarget") }}
          </RouterLink>
        </Button>
        <Button
          variant="outline"
          :disabled="isLoading"
          @click="reload"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': isLoading || isPolling }"
          />
          {{ t("common.refresh") }}
        </Button>
        <AgentSessionActions
          v-if="session"
          :session="session"
          :can-update="canUpdate"
          @updated="onUpdated"
          @archived="onArchived"
          @reload="reload"
        />
      </template>
    </PageHeader>

    <PageState
      :loading="isLoading && !session"
      :error="loadError"
      :title="t('dataQuality.session.loadFailed')"
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
        <div class="min-w-0 space-y-4">
          <Card>
            <CardHeader class="border-b">
              <CardTitle class="text-base">
                {{ t("dataQuality.session.conversationTitle") }}
              </CardTitle>
              <CardDescription v-if="session.objective">
                {{ session.objective }}
              </CardDescription>
            </CardHeader>
            <CardContent class="space-y-4 pt-4">
              <PageState
                :loading="messagesLoading && messages.length === 0"
                :error="messagesError"
                :title="t('dataQuality.session.messagesLoadFailed')"
              >
                <template #error>
                  <Button
                    variant="outline"
                    size="sm"
                    @click="reloadMessages"
                  >
                    {{ t("common.refresh") }}
                  </Button>
                </template>

                <p
                  v-if="messages.length === 0"
                  class="text-sm text-muted-foreground"
                >
                  {{ t("dataQuality.session.noTranscript") }}
                </p>

                <div
                  v-else
                  class="space-y-4"
                >
                  <div
                    v-if="nextPageToken"
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
                          ? t("dataQuality.session.loadingEarlier")
                          : t("dataQuality.session.loadEarlier")
                      }}
                    </Button>
                    <p
                      v-if="loadEarlierError"
                      class="text-xs text-destructive"
                    >
                      {{ loadEarlierError }}
                    </p>
                  </div>
                  <AgentConversation :groups="groups" />
                </div>
              </PageState>
            </CardContent>
          </Card>

          <Card v-if="canUpdate">
            <CardHeader class="border-b">
              <CardTitle class="text-base">
                {{ t("dataQuality.session.askTitle") }}
              </CardTitle>
              <CardDescription>{{ t("dataQuality.session.askHint") }}</CardDescription>
            </CardHeader>
            <CardContent class="space-y-3 pt-4">
              <div class="space-y-1.5">
                <div class="flex items-center justify-between gap-2">
                  <Label
                    for="quality-agent-ask"
                    class="sr-only"
                  >
                    {{ t("dataQuality.session.askTitle") }}
                  </Label>
                  <span class="ml-auto text-xs text-muted-foreground tabular-nums">
                    {{ askContent.length }} / {{ ASK_MAX_LENGTH }}
                  </span>
                </div>
                <textarea
                  id="quality-agent-ask"
                  v-model="askContent"
                  rows="3"
                  :maxlength="ASK_MAX_LENGTH"
                  class="min-h-20 w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/25 focus-visible:outline-none"
                  :placeholder="t('dataQuality.session.askPlaceholder')"
                  :disabled="asking"
                />
              </div>
              <p
                v-if="askError"
                class="text-sm text-destructive"
              >
                {{ askError }}
              </p>
              <div class="flex justify-end">
                <Button
                  :disabled="asking || !canAsk"
                  @click="ask"
                >
                  <component
                    :is="asking ? Loader2 : Send"
                    class="mr-2 h-4 w-4"
                    :class="{ 'animate-spin': asking }"
                  />
                  {{
                    asking
                      ? t("dataQuality.session.askSending")
                      : t("dataQuality.session.askSend")
                  }}
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>

        <div class="min-w-0 space-y-4">
          <Card>
            <CardHeader class="border-b">
              <CardTitle class="text-base">
                {{ t("dataQuality.session.runPanelTitle") }}
              </CardTitle>
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
                  class="ml-auto text-xs text-muted-foreground tabular-nums"
                >
                  {{ t("dataQuality.session.turn", { turn: currentTurn }) }}
                </span>
              </div>

              <p class="text-xs text-muted-foreground">
                {{ currentActivity }}
              </p>

              <dl class="grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
                <dt class="text-muted-foreground">
                  {{ t("dataQuality.session.contextTokens") }}
                </dt>
                <dd class="text-right tabular-nums">
                  {{ formatTokens(session.contextTokens, locale) }}
                </dd>
                <dt class="text-muted-foreground">
                  {{ t("dataQuality.session.tokensUsed") }}
                </dt>
                <dd class="text-right tabular-nums">
                  {{ formatTokens(session.tokensUsed, locale) }}
                </dd>
                <dt class="text-muted-foreground">
                  {{ t("dataQuality.agent.columnWakes") }}
                </dt>
                <dd class="text-right tabular-nums">
                  {{ session.wakeCount }}
                </dd>
                <dt class="text-muted-foreground">
                  {{ t("dataQuality.session.restartCount") }}
                </dt>
                <dd class="text-right tabular-nums">
                  {{ session.restartCount }}
                </dd>
                <dt class="text-muted-foreground">
                  {{ t("dataQuality.session.nextWake") }}
                </dt>
                <dd
                  class="text-right tabular-nums"
                  :title="formatDateTime(session.nextWakeTime, locale)"
                >
                  {{ nextWakeLabel }}
                </dd>
                <dt class="text-muted-foreground">
                  {{ t("dataQuality.agent.lastActivity") }}
                </dt>
                <dd
                  class="text-right tabular-nums"
                  :title="formatDateTime(session.lastActivityTime, locale)"
                >
                  {{ lastActivityLabel }}
                </dd>
              </dl>

              <div
                v-if="heartbeat.stale"
                class="rounded-md border border-warning/40 bg-warning-subtle/30 px-2.5 py-1.5 text-xs text-warning"
              >
                {{ t("dataQuality.agent.possiblyStaleHint") }}
              </div>

              <div
                v-if="session.blockedCode || session.blockedReason"
                class="space-y-1 rounded-md border border-warning/40 bg-warning-subtle/30 px-2.5 py-1.5 text-xs"
              >
                <p class="font-medium text-warning">
                  {{ t("dataQuality.agent.blockedCode") }}:
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
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import { ArrowLeft, Loader2, RefreshCcw, Send, Target } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute, useRouter } from "vue-router";
import {
  getQualityAgentSession,
  listQualityAgentSessionMessages,
  wakeQualityAgentSession,
} from "@/api/data-quality";
import PageState from "@/components/common/PageState.vue";
import AgentConversation from "@/components/data-quality/AgentConversation.vue";
import AgentSessionActions from "@/components/data-quality/AgentSessionActions.vue";
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
import { Label } from "@/components/ui/label";
import { useDataQualityPermissions } from "@/composables/useDataQualityPermissions";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { usePolling } from "@/composables/usePolling";
import { notify } from "@/lib/notify";
import {
  QualityAgentMessageRole,
  type QualityAgentSession,
  type QualityAgentSessionMessage,
  QualityAgentSessionState,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  agentMessageRoleKey,
  agentSessionHeartbeat,
  agentSessionStateKey,
  agentSessionStateVariant,
  agentSessionTargetName,
  agentSessionTargetPath,
  formatTokens,
  groupMessagesByWake,
  lastTranscriptSeq,
  mergeTranscriptMessages,
} from "@/utils/agentSession";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { isAborted } from "@/utils/error";
import { guidToRouteParams } from "@/utils/guid";

/**
 * One target's long-lived agent conversation: the run state beside the full
 * transcript, with a box that appends a person's question and wakes the agent.
 *
 * The transcript is paged newest-first (the initial read returns the newest page
 * in chronological order and "Load earlier" walks the token backwards) and
 * polled incrementally with `after_seq` while the session is running, so a live
 * wake appends messages without re-reading the page. A page the reader left
 * stops polling through `usePolling`; a hidden tab pauses it.
 */

const RUNNING_POLL_INTERVAL_MS = 2000;
const IDLE_POLL_INTERVAL_MS = 10_000;
const MESSAGE_PAGE_SIZE = 50;
const POLL_PAGE_SIZE = 200;
/** Matches the server's bound on one wake message. */
const ASK_MAX_LENGTH = 4000;

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { formatError } = useErrorMessage();
const { canUpdate } = useDataQualityPermissions();

const session = ref<QualityAgentSession>();
const messages = ref<QualityAgentSessionMessage[]>([]);
const nextPageToken = ref("");
const isLoading = ref(false);
const isPolling = ref(false);
const messagesLoading = ref(false);
const loadError = ref("");
const messagesError = ref("");
const loadingEarlier = ref(false);
const loadEarlierError = ref("");
const askContent = ref("");
const asking = ref(false);
const askError = ref("");

let loadGeneration = 0;

// One path segment per resource-name segment; joining restores the name a Get
// needs (Get cannot resolve the list RPC's `workspaces/-` shorthand).
const sessionName = computed(() => {
  const param = route.params.sessionId;
  return Array.isArray(param) ? param.join("/") : String(param ?? "");
});

const pageTitle = computed(() =>
  session.value
    ? agentSessionTargetName(session.value)
    : t("dataQuality.session.title")
);

const heartbeat = computed(() =>
  session.value
    ? agentSessionHeartbeat(session.value)
    : { running: false, stale: false }
);

const groups = computed(() => groupMessagesByWake(messages.value));

const lastSeq = computed(() => lastTranscriptSeq(messages.value));

const latestMessage = computed(() => messages.value[messages.value.length - 1]);

/** The newest "model turn N started" note, while a wake is in flight. */
const currentTurn = computed((): number | undefined => {
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

const statusLabel = computed(() => {
  if (!session.value) {
    return "";
  }
  if (session.value.state === QualityAgentSessionState.ARCHIVED) {
    return t("dataQuality.session.statusArchived");
  }
  if (heartbeat.value.running) {
    return t("dataQuality.session.statusRunning");
  }
  return t("dataQuality.session.statusIdle");
});

const currentActivity = computed(() => {
  const latest = latestMessage.value;
  if (!latest) {
    return t("dataQuality.agent.noActivity");
  }
  const parts = [
    t(agentMessageRoleKey(latest.role)),
    t("dataQuality.session.wake", { wake: latest.wake }),
  ];
  if (latest.turn > 0) {
    parts.push(t("dataQuality.session.turn", { turn: latest.turn }));
  }
  if (latest.createTime) {
    parts.push(formatRelativeTime(latest.createTime, locale.value));
  }
  return parts.join(" · ");
});

const lastActivityLabel = computed(() =>
  session.value?.lastActivityTime
    ? formatRelativeTime(session.value.lastActivityTime, locale.value)
    : t("dataQuality.agent.noActivity")
);

const targetTo = computed(() => {
  const target = session.value?.target;
  if (!target || !target.metaGuid) {
    return undefined;
  }
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.metaGuid) },
    query: {
      metaType: String(target.metaType ?? MetaType.UNSPECIFIED),
    },
  };
});

/** The wake the run panel shows: relative, with the exact time in the title. */
const nextWakeLabel = computed(() =>
  session.value?.nextWakeTime
    ? formatRelativeTime(session.value.nextWakeTime, locale.value)
    : t("dataQuality.agent.noNextWake")
);

const canAsk = computed(() => askContent.value.trim() !== "");

// ---- loading ----

async function load(signal?: AbortSignal) {
  const mine = ++loadGeneration;
  isLoading.value = true;
  loadError.value = "";
  try {
    const nextSession = await getQualityAgentSession(sessionName.value, signal);
    if (mine !== loadGeneration || signal?.aborted) {
      return;
    }
    session.value = nextSession;
  } catch (e: unknown) {
    if (mine !== loadGeneration || isAborted(e)) {
      return;
    }
    loadError.value = formatError(e, "dataQuality.session.loadFailed");
  } finally {
    if (mine === loadGeneration) {
      isLoading.value = false;
    }
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
    nextPageToken.value = page.nextPageToken;
  } catch (e: unknown) {
    if (isAborted(e)) {
      return;
    }
    messagesError.value = formatError(
      e,
      "dataQuality.session.messagesLoadFailed"
    );
  } finally {
    messagesLoading.value = false;
  }
}

async function reload() {
  await Promise.all([load(), loadMessages()]);
}

async function reloadMessages() {
  await loadMessages();
}

/** Append everything newer than the watermark; no-op when nothing is new. */
async function pollMessages(signal: AbortSignal) {
  isPolling.value = true;
  try {
    const [nextSession, page] = await Promise.all([
      getQualityAgentSession(sessionName.value, signal),
      listQualityAgentSessionMessages(sessionName.value, {
        afterSeq: lastSeq.value,
        pageSize: POLL_PAGE_SIZE,
        signal,
      }),
    ]);
    if (signal.aborted) {
      return;
    }
    session.value = nextSession;
    if (page.messages.length > 0) {
      messages.value = mergeTranscriptMessages(messages.value, page.messages);
    }
  } finally {
    isPolling.value = false;
  }
}

const fastPolling = usePolling(pollMessages, {
  intervalMs: RUNNING_POLL_INTERVAL_MS,
  shouldContinue: () => heartbeat.value.running,
  continueOnError: true,
});

const idlePolling = usePolling(pollMessages, {
  intervalMs: IDLE_POLL_INTERVAL_MS,
  shouldContinue: () =>
    session.value !== undefined &&
    session.value.state !== QualityAgentSessionState.ARCHIVED,
  continueOnError: true,
});

function syncPolling() {
  if (!session.value) {
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

async function loadEarlier() {
  if (!nextPageToken.value || loadingEarlier.value) {
    return;
  }
  loadingEarlier.value = true;
  loadEarlierError.value = "";
  try {
    // The token is an offset from the newest message. Messages are only ever
    // appended, so a live wake shifts the offset toward newer rows and an older
    // page can overlap what is already shown; the merge deduplicates by seq and
    // never skips a row.
    const older = await listQualityAgentSessionMessages(sessionName.value, {
      pageSize: MESSAGE_PAGE_SIZE,
      pageToken: nextPageToken.value,
    });
    messages.value = mergeTranscriptMessages(older.messages, messages.value);
    nextPageToken.value = older.nextPageToken;
  } catch (e: unknown) {
    loadEarlierError.value = formatError(
      e,
      "dataQuality.session.loadEarlierFailed"
    );
  } finally {
    loadingEarlier.value = false;
  }
}

// ---- actions ----

function onUpdated(updated: QualityAgentSession) {
  session.value = updated;
}

async function onArchived() {
  await router.push({ name: "DataQualityAgent" });
}

async function ask() {
  if (asking.value || !canAsk.value) {
    if (!asking.value && askContent.value.trim() === "") {
      askError.value = t("dataQuality.session.askEmpty");
    }
    return;
  }
  asking.value = true;
  askError.value = "";
  try {
    const updated = await wakeQualityAgentSession(
      sessionName.value,
      askContent.value.trim()
    );
    session.value = updated;
    askContent.value = "";
    notify.success(t("dataQuality.session.asked"));
    // The new question is a message; read it back immediately instead of
    // waiting for the next poll tick.
    await pollMessages(new AbortController().signal);
  } catch (e: unknown) {
    askError.value = formatError(e, "dataQuality.session.askFailed");
  } finally {
    asking.value = false;
  }
}

watch(sessionName, async () => {
  messages.value = [];
  nextPageToken.value = "";
  session.value = undefined;
  await Promise.all([load(), loadMessages()]);
  syncPolling();
});

onMounted(async () => {
  await Promise.all([load(), loadMessages()]);
  syncPolling();
});
</script>
