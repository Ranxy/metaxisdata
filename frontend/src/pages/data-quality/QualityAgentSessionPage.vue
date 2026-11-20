<template>
  <div class="flex h-full min-h-0 flex-col gap-3">
    <PageHeader
      :title="pageTitle"
      :description="session ? agentSessionTargetPath(session) : ''"
      class="shrink-0"
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
      class="shrink-0 self-start"
    />

    <Alert
      v-if="error"
      variant="destructive"
      class="shrink-0"
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
        class="flex min-h-0 flex-1 flex-col gap-4 xl:flex-row"
      >
        <!-- Below xl the run state is a disclosure above the transcript: the
             chat pane keeps the rest of the height instead of losing 300+px to
             a sidebar. -->
        <Collapsible class="shrink-0 rounded-lg border border-border/70 bg-card shadow-e1 xl:hidden">
          <CollapsibleTrigger
            class="flex w-full items-center gap-2 px-4 py-2.5 text-left text-sm hover:bg-accent/40"
          >
            <span
              class="h-2 w-2 shrink-0 rounded-full"
              :class="heartbeat.running ? 'bg-info' : 'bg-muted-foreground/40'"
            />
            <span class="truncate font-medium">{{ statusLabel }}</span>
            <span class="ml-auto flex shrink-0 items-center gap-3 text-xs tabular-nums text-muted-foreground">
              <span>{{ t("qualityAgent.sessionWakes") }} {{ session.wakeCount }}</span>
              <span class="hidden sm:inline">{{ lastActivityLabel }}</span>
              <ChevronDown class="h-4 w-4" />
            </span>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div class="border-t px-4 py-3">
              <QualityAgentRunState
                :session="session"
                :heartbeat="heartbeat"
                :status-label="statusLabel"
                :next-wake-label="nextWakeLabel"
                :last-activity-label="lastActivityLabel"
                :current-turn="currentTurn"
                :poll-error="pollError"
              />
            </div>
          </CollapsibleContent>
        </Collapsible>

        <Card class="flex min-h-[24rem] min-w-0 flex-1 flex-col overflow-hidden">
          <CardHeader class="shrink-0 border-b">
            <CardTitle class="text-base">{{ t("qualityAgent.transcriptTitle") }}</CardTitle>
            <CardDescription>{{ t("qualityAgent.transcriptHint") }}</CardDescription>
          </CardHeader>
          <CardContent class="flex min-h-0 flex-1 flex-col p-0">
            <div
              :class="
                transcriptLoading
                  ? 'grid min-h-0 flex-1 place-items-center'
                  : 'flex min-h-0 flex-1 flex-col'
              "
            >
              <PageState
                :loading="messagesLoading && messages.length === 0"
                :error="messagesError"
                :title="t('qualityAgent.messagesLoadFailed')"
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

                <div class="relative flex min-h-0 flex-1 flex-col">
                  <!-- The transcript pane: the only scroll container besides the
                       shell. It opens on the newest message and follows new ones
                       until the reader scrolls back. -->
                  <div
                    ref="scrollerRef"
                    class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 pt-4 pb-6 [scrollbar-gutter:stable]"
                    tabindex="0"
                    role="region"
                    :aria-label="t('qualityAgent.transcriptTitle')"
                    @scroll="onScroll"
                  >
                    <div class="mx-auto w-full max-w-[56rem]">
                      <div
                        ref="topSentinelRef"
                        aria-hidden="true"
                        class="h-px"
                      />
                      <!-- A fixed-height marker row: "load earlier" and the
                           start-of-history note swap without moving the content
                           below them. -->
                      <div class="flex h-9 items-center justify-center">
                        <Button
                          v-if="hasEarlier"
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
                          v-else-if="messages.length > 0"
                          class="text-xs text-muted-foreground"
                        >
                          {{ t("qualityAgent.historyStart") }}
                        </p>
                      </div>
                      <p
                        v-if="loadEarlierError"
                        class="pb-2 text-center text-xs text-destructive"
                      >
                        {{ loadEarlierError }}
                      </p>
                      <div
                        v-if="messages.length === 0"
                        class="grid min-h-[16rem] place-items-center text-sm text-muted-foreground"
                      >
                        {{ t("qualityAgent.transcriptEmpty") }}
                      </div>
                      <QualityAgentTranscript
                        v-else
                        :groups="groups"
                        :session-name="session.name"
                      />
                    </div>
                  </div>
                  <div
                    class="pointer-events-none absolute inset-x-0 top-0 h-6 bg-gradient-to-b from-card to-transparent"
                    aria-hidden="true"
                  />
                  <Button
                    v-if="!pinned"
                    variant="secondary"
                    size="sm"
                    class="absolute bottom-4 left-1/2 z-10 -translate-x-1/2 rounded-full shadow-e2"
                    @click="jumpToLatest"
                  >
                    <ArrowDown class="mr-1 h-3.5 w-3.5" />
                    {{
                      unreadCount > 0
                        ? t("qualityAgent.newMessages", { count: unreadCount })
                        : t("qualityAgent.jumpToLatest")
                    }}
                  </Button>
                </div>
              </PageState>
            </div>
          </CardContent>
        </Card>

        <aside class="hidden w-80 shrink-0 self-start xl:block xl:max-h-full xl:overflow-y-auto">
          <Card>
            <CardHeader class="border-b">
              <CardTitle class="text-base">{{ t("qualityAgent.runPanelTitle") }}</CardTitle>
            </CardHeader>
            <CardContent class="space-y-3 pt-4">
              <QualityAgentRunState
                :session="session"
                :heartbeat="heartbeat"
                :status-label="statusLabel"
                :next-wake-label="nextWakeLabel"
                :last-activity-label="lastActivityLabel"
                :current-turn="currentTurn"
                :poll-error="pollError"
              />
            </CardContent>
          </Card>
        </aside>
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import {
  AlertTriangle,
  ArrowDown,
  ChevronDown,
  Loader2,
  RefreshCcw,
  Target,
} from "lucide-vue-next";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import {
  getQualityAgent,
  getQualityAgentSession,
  listQualityAgentSessionMessages,
  resumeQualityAgentSession,
} from "@/api/data-quality";
import { preloadMarkdown } from "@/components/common/AsyncMarkdown";
import PageState from "@/components/common/PageState.vue";
import QualityAgentBreadcrumb from "@/components/data-quality/QualityAgentBreadcrumb.vue";
import QualityAgentRunState from "@/components/data-quality/QualityAgentRunState.vue";
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
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { useChatScroll } from "@/composables/useChatScroll";
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
import { formatRelativeTime } from "@/utils/datetime";
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
  countMessagesAfterSeq,
  groupMessagesByWake,
  lastTranscriptSeq,
  mergeTranscriptMessages,
} from "@/utils/qualityAgentSession";

const RUNNING_POLL_INTERVAL_MS = 2000;
const IDLE_POLL_INTERVAL_MS = 10_000;
const MESSAGE_PAGE_SIZE = 50;
const POLL_PAGE_SIZE = 200;
/** How close to the top of the pane the older-history request fires. */
const EARLIER_ROOT_MARGIN_PX = 200;

const { t, locale } = useI18n();
const route = useRoute();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();
const {
  scrollerRef,
  pinned,
  onScroll,
  scrollToLatest,
  settleAtLatest,
  preserveAnchor,
} = useChatScroll();

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
const topSentinelRef = ref<HTMLElement | null>(null);
/** The newest transcript seq the reader has seen while following the bottom. */
const seenSeq = ref(0n);

let disposed = false;
let earlierObserver: IntersectionObserver | undefined;

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

/** The first-page spinner fills the pane instead of sitting at its top. */
const transcriptLoading = computed(
  () => messagesLoading.value && messages.value.length === 0
);

/** Messages that arrived after the reader scrolled away from the newest one. */
const unreadCount = computed(() =>
  pinned.value ? 0 : countMessagesAfterSeq(messages.value, seenSeq.value)
);

// Following the newest message means the reader has seen everything loaded.
watch(
  [pinned, lastSeq],
  ([following, newest]) => {
    if (following) {
      seenSeq.value = newest;
    }
  },
  { immediate: true }
);

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

/**
 * Put the newest message in view and keep it there while lazily rendered content
 * (the Markdown chunk) settles. Runs after every first-page load and session
 * switch; a reader who scrolls away ends it early.
 */
async function landAtLatest() {
  await nextTick();
  if (disposed) {
    return;
  }
  seenSeq.value = lastSeq.value;
  await settleAtLatest();
}

async function load() {
  if (disposed) {
    return;
  }
  isLoading.value = true;
  loadError.value = "";
  error.value = "";
  try {
    // Warm the Markdown chunk before the first page of messages is applied, so
    // the transcript paints once at its final height. The renderer is still a
    // lazy chunk and the async component resolves in a microtask after this, but
    // no network wait is left for the first paint; the landing window absorbs
    // whatever small growth remains. Only the first visit downloads it.
    await Promise.all([loadAgent(), loadSession(), preloadMarkdown()]);
    await loadMessages();
  } catch (err) {
    loadError.value = formatError(err);
  } finally {
    isLoading.value = false;
  }
  await landAtLatest();
}

/** Retry the transcript page, then land on the newest message it renders. */
async function reloadMessages() {
  await loadMessages();
  await landAtLatest();
}

/**
 * A full resync replaces the loaded page with the newest one, so it explicitly
 * returns to the live end. Reading history through Refresh is not a supported
 * gesture; scrolling back is (the poll keeps history in place).
 */
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
    hasEarlier.value = older.messages.length >= MESSAGE_PAGE_SIZE;
    // Inserting above the viewport must not move what the reader is reading.
    await preserveAnchor(() => {
      messages.value = mergeTranscriptMessages(older.messages, messages.value);
    });
  } catch (err) {
    loadEarlierError.value = formatError(err);
  } finally {
    loadingEarlier.value = false;
  }
}

/**
 * Ask for older history when the reader reaches the top of the pane. The
 * explicit button above the transcript stays as the keyboard and fallback path.
 */
function setupEarlierObserver() {
  earlierObserver?.disconnect();
  earlierObserver = undefined;
  const root = scrollerRef.value;
  const sentinel = topSentinelRef.value;
  if (
    !root ||
    !sentinel ||
    messages.value.length === 0 ||
    typeof IntersectionObserver === "undefined"
  ) {
    return;
  }
  earlierObserver = new IntersectionObserver(
    (entries) => {
      // A reader still following the newest message is not asking for history:
      // this also keeps the first landing from reading as a scroll to the top.
      if (pinned.value) {
        return;
      }
      if (entries.some((entry) => entry.isIntersecting)) {
        void loadEarlier();
      }
    },
    { root, rootMargin: `${EARLIER_ROOT_MARGIN_PX}px 0px 0px 0px` }
  );
  earlierObserver.observe(sentinel);
}

watch([scrollerRef, topSentinelRef], setupEarlierObserver, { flush: "post" });

/** Append everything newer than the watermark, including a burst larger than one page. */
async function poll(signal: AbortSignal) {
  pollError.value = "";
  await loadSession(signal);
  let after = lastSeq.value;
  let appended = false;
  for (;;) {
    const page = await listQualityAgentSessionMessages(sessionName.value, {
      afterSeq: after,
      pageSize: POLL_PAGE_SIZE,
      signal,
    });
    if (signal.aborted || page.messages.length === 0) {
      break;
    }
    messages.value = mergeTranscriptMessages(messages.value, page.messages);
    after = lastTranscriptSeq(page.messages);
    appended = true;
    if (page.messages.length < POLL_PAGE_SIZE) {
      break;
    }
  }
  if (appended && pinned.value) {
    await nextTick();
    // A session opened before its first answer mounts the lazy renderer on this
    // very append; a single jump can land short, so use the landing window.
    await settleAtLatest();
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

async function jumpToLatest() {
  seenSeq.value = lastSeq.value;
  await scrollToLatest({ smooth: true });
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
  loadEarlierError.value = "";
  seenSeq.value = 0n;
  pinned.value = true;
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
  earlierObserver?.disconnect();
  earlierObserver = undefined;
});
</script>
