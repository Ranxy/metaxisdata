<template>
  <div class="space-y-4">
    <PageHeader
      :title="agent?.displayName || t('qualityAgents.title')"
      :description="agent?.description || ''"
    >
      <template #breadcrumb>
        <QualityAgentBreadcrumb
          :agent-name="agentName"
          :agent-label="agent?.displayName || agentId"
          variant="home"
        />
      </template>
      <template #actions>
        <Button
          v-if="(canManage || canChat) && agent && agent.state === QualityAgentState.ACTIVE"
          size="sm"
          :disabled="busy"
          @click="pause"
        >
          {{ t("qualityAgents.pause") }}
        </Button>
        <Button
          v-if="(canManage || canChat) && agent && (agent.state === QualityAgentState.PAUSED || agent.state === QualityAgentState.DRAFT)"
          size="sm"
          :disabled="busy"
          @click="start"
        >
          {{ t("qualityAgents.start") }}
        </Button>
      </template>
    </PageHeader>

    <QualityAgentTabs
      :agent-name="agentName"
      active="conversation"
    />

    <Alert
      v-if="error"
      variant="destructive"
    >
      <AlertTriangle class="h-4 w-4" />
      <AlertDescription>{{ error }}</AlertDescription>
    </Alert>

    <PageState :loading="loading">
      <div
        v-if="!agent"
        class="rounded-lg border border-dashed p-8 text-center"
      >
        <p class="font-medium">{{ t("qualityAgent.missingTitle") }}</p>
        <p class="mt-1 text-sm text-muted-foreground">{{ t("qualityAgent.missingDescription") }}</p>
      </div>
      <div
        v-else
        class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_320px]"
      >
        <Card class="flex h-[36rem] min-h-0 flex-col">
          <CardHeader class="border-b">
            <CardTitle class="text-base">{{ t("qualityAgent.conversationTitle") }}</CardTitle>
            <CardDescription>{{ t("qualityAgent.conversationHint") }}</CardDescription>
          </CardHeader>
          <CardContent class="flex min-h-0 flex-1 flex-col gap-3 pt-4">
            <div class="relative flex min-h-0 flex-1 flex-col">
              <div
                :ref="bindScroller"
                class="min-h-0 flex-1 space-y-3 overflow-y-auto pr-1 [scrollbar-gutter:stable]"
                tabindex="0"
                role="region"
                :aria-label="t('qualityAgent.conversationTitle')"
                @scroll="onScroll"
              >
                <p
                  v-if="chatLoading && messages.length === 0"
                  class="text-sm text-muted-foreground"
                >
                  {{ t("common.loading") }}
                </p>
                <p
                  v-else-if="visibleMessages.length === 0"
                  class="text-sm text-muted-foreground"
                >
                  {{ t("qualityAgent.conversationEmpty") }}
                </p>
                <div
                  v-for="message in visibleMessages"
                  :key="message.name"
                  class="flex flex-col gap-1"
                  :class="message.role === QualityAgentMessageRole.USER ? 'items-end' : 'items-start'"
                >
                  <span class="text-[11px] text-muted-foreground">
                    {{ message.role === QualityAgentMessageRole.USER ? t("qualityAgent.you") : t("qualityAgent.agentReply") }}
                  </span>
                  <div
                    class="max-w-[85%] whitespace-pre-wrap break-words rounded-lg px-3 py-2 text-sm"
                    :class="message.role === QualityAgentMessageRole.USER ? 'bg-primary text-primary-foreground' : 'bg-muted'"
                  >
                    {{ message.content }}
                  </div>
                </div>
              </div>
              <Button
                v-if="!pinned && visibleMessages.length > 0"
                variant="secondary"
                size="sm"
                class="absolute bottom-2 left-1/2 z-10 -translate-x-1/2 rounded-full shadow-e2"
                @click="jumpToConversationLatest"
              >
                <ArrowDown class="mr-1 h-3.5 w-3.5" />
                {{
                  chatUnread > 0
                    ? t("qualityAgent.newMessages", { count: chatUnread })
                    : t("qualityAgent.jumpToLatest")
                }}
              </Button>
            </div>

            <p
              v-if="chatError"
              class="text-xs text-destructive"
            >
              {{ chatError }}
            </p>

            <div
              v-if="canChat"
              class="space-y-2 border-t pt-3"
            >
              <textarea
                v-model="composer"
                rows="3"
                class="w-full resize-none rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                :placeholder="t('qualityAgent.messagePlaceholder')"
                :disabled="sending"
                @keydown.enter.exact.prevent="onComposerEnter"
              />
              <div class="flex justify-end">
                <Button
                  size="sm"
                  :disabled="sending || composer.trim() === ''"
                  @click="send"
                >
                  <Loader2
                    v-if="sending"
                    class="mr-2 h-4 w-4 animate-spin"
                  />
                  {{ sending ? t("qualityAgent.sending") : t("qualityAgent.send") }}
                </Button>
              </div>
            </div>
            <p
              v-else
              class="border-t pt-3 text-xs text-muted-foreground"
            >
              {{ t("qualityAgent.readOnly") }}
            </p>
          </CardContent>
        </Card>

        <div class="space-y-4">
          <Card>
            <CardHeader class="border-b">
              <CardTitle class="text-base">{{ t("qualityAgent.statusTitle") }}</CardTitle>
            </CardHeader>
            <CardContent class="space-y-2 pt-4 text-sm">
              <div class="flex items-center justify-between">
                <span class="text-muted-foreground">{{ t("qualityAgents.columnState") }}</span>
                <Badge :variant="stateVariant(agent?.state)">
                  {{ stateLabel(agent?.state) }}
                </Badge>
              </div>
              <div class="flex items-center justify-between">
                <span class="text-muted-foreground">{{ t("qualityAgents.columnOwner") }}</span>
                <span class="truncate">{{ agent?.owner || "-" }}</span>
              </div>
              <div class="flex items-center justify-between">
                <span class="text-muted-foreground">{{ t("qualityAgents.language") }}</span>
                <span class="truncate">{{ agentLanguageLabel(agent?.language) }}</span>
              </div>
              <div class="flex items-center justify-between">
                <span class="text-muted-foreground">{{ t("qualityAgents.columnDatabases") }}</span>
                <span>{{ agent?.databases.length ?? 0 }}</span>
              </div>
              <div class="flex flex-wrap gap-1 pt-1">
                <Badge
                  v-for="capability in agent?.capabilities ?? []"
                  :key="capability"
                  variant="secondary"
                >
                  {{ capability }}
                </Badge>
              </div>
              <div
                v-if="agent?.adoptionStatus"
                class="space-y-1 border-t pt-2 text-xs"
              >
                <div class="flex items-center justify-between gap-2">
                  <span class="text-muted-foreground">{{ t("qualityAgent.adoptionStatusLabel") }}</span>
                  <span>
                    {{ t("qualityAgent.adoptionStatusSummary", {
                      adopted: agent.adoptionStatus.adoptedLast24h,
                      approved: agent.adoptionStatus.approvedLast24h,
                      failed: agent.adoptionStatus.failedLast24h,
                      pending: agent.adoptionStatus.pendingDraftCount,
                    }) }}
                  </span>
                </div>
                <p
                  v-if="agent.adoptionStatus.lastFailureReason"
                  class="break-words text-destructive"
                >
                  {{ t("qualityAgent.adoptionLastFailure", {
                    reason: agent.adoptionStatus.lastFailureReason,
                  }) }}
                  <span v-if="agent.adoptionStatus.lastFailureTime">
                    · {{ formatDateTime(agent.adoptionStatus.lastFailureTime, locale) }}
                  </span>
                </p>
              </div>
              <div
                v-if="canChat && agent?.capabilities.includes('adopt')"
                class="flex items-center justify-between gap-2 border-t pt-2"
              >
                <span class="text-muted-foreground">{{ t("qualityAgent.adoptionLabel") }}</span>
                <Button
                  size="sm"
                  variant="outline"
                  :disabled="busy"
                  @click="toggleAdoption"
                >
                  {{ adoptionEnabled ? t("qualityAgent.adoptionDisable") : t("qualityAgent.adoptionEnable") }}
                </Button>
              </div>
              <Button
                v-if="canApprove"
                size="sm"
                variant="secondary"
                class="w-full"
                :disabled="busy || (agent?.adoptionStatus?.pendingDraftCount ?? 0) === 0"
                @click="adoptPending()"
              >
                {{ t("qualityAgent.adoptPending") }}
              </Button>
            </CardContent>
          </Card>
        </div>
      </div>
    </PageState>

    <Card v-if="agent">
      <CardHeader class="border-b">
        <div class="flex flex-wrap items-center gap-2">
          <CardTitle class="text-base">{{ t("qualityAgent.attentionTitle") }}</CardTitle>
          <Badge
            v-if="attentions.length > 0"
            variant="warning"
          >
            {{ attentions.length }}
          </Badge>
        </div>
        <CardDescription>{{ t("qualityAgent.attentionHint") }}</CardDescription>
      </CardHeader>
      <CardContent class="space-y-2 pt-4">
        <p
          v-if="attentions.length === 0"
          class="text-sm text-muted-foreground"
        >
          {{ t("qualityAgent.attentionEmpty") }}
        </p>
        <QualityAgentAttentionItem
          v-for="attention in attentions"
          :key="attention.name"
          :attention="attention"
          :agent-name="agentName"
          :can-reply="canChat"
          :can-approve="canApprove"
          :approve-busy="busy"
          @approve="adoptPending"
          @resolved="onAttentionResolved"
        />
        <p
          v-if="!canChat && attentions.length > 0"
          class="text-xs text-muted-foreground"
        >
          {{ t("qualityAgent.attentionReadOnly") }}
        </p>
      </CardContent>
    </Card>

    <Card v-if="agent">
      <CardHeader class="border-b">
        <div class="flex flex-wrap items-center gap-2">
          <CardTitle class="text-base">{{ t("qualityAgent.casesTitle") }}</CardTitle>
          <Badge
            v-if="cases.length > 0"
            variant="secondary"
          >
            {{ cases.length }}
          </Badge>
        </div>
        <CardDescription>{{ t("qualityAgent.casesHint") }}</CardDescription>
      </CardHeader>
      <CardContent class="pt-4">
        <p
          v-if="cases.length === 0"
          class="text-sm text-muted-foreground"
        >
          {{ t("qualityAgent.casesEmpty") }}
        </p>
        <ul
          v-else
          class="space-y-2"
        >
          <li
            v-for="workItem in cases"
            :key="workItem.name"
            class="rounded-md border px-3 py-2"
          >
            <div class="flex flex-wrap items-center gap-2">
              <Badge>{{ qualityAgentCaseStateLabel(workItem.state) }}</Badge>
              <span
                v-if="workItem.class"
                class="text-xs uppercase text-muted-foreground"
              >
                {{ workItem.class }}
              </span>
              <span class="text-xs text-muted-foreground">
                {{ t("qualityAgent.caseAttempts", {
                  attempts: workItem.attempts,
                  max: workItem.maxAttempts,
                }) }}
              </span>
            </div>
            <p class="mt-1 text-sm">
              {{ workItem.summary }}
            </p>
          </li>
        </ul>
      </CardContent>
    </Card>

    <Card v-if="agent">
      <CardHeader class="border-b">
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div class="min-w-0">
            <CardTitle class="text-base">{{ t("qualityAgent.workSessionsTitle") }}</CardTitle>
            <CardDescription>{{ t("qualityAgent.workSessionsHint") }}</CardDescription>
          </div>
          <Button
            variant="outline"
            size="sm"
            as-child
          >
            <RouterLink :to="sessionsRoute">
              {{ t("qualityAgent.workSessionsViewAll") }}
            </RouterLink>
          </Button>
        </div>
      </CardHeader>
      <CardContent class="space-y-2 pt-4">
        <p
          v-if="sessionsLoading && sessions.length === 0"
          class="text-sm text-muted-foreground"
        >
          {{ t("common.loading") }}
        </p>
        <p
          v-else-if="sessionsError"
          class="text-sm text-destructive"
        >
          {{ sessionsError }}
        </p>
        <p
          v-else-if="sessions.length === 0"
          class="text-sm text-muted-foreground"
        >
          {{ t("qualityAgent.workSessionsEmpty") }}
        </p>
        <div
          v-for="session in sessions"
          :key="session.name"
          class="flex flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm"
        >
          <div class="min-w-0">
            <RouterLink
              :to="sessionRoute(session)"
              class="font-medium hover:underline"
            >
              {{ agentSessionTargetName(session) }}
            </RouterLink>
            <p class="truncate text-xs text-muted-foreground">
              {{ agentSessionTargetPath(session) }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-1">
            <Badge :variant="agentSessionStateVariant(session.state)">
              {{ t(agentSessionStateKey(session.state)) }}
            </Badge>
            <Badge :variant="agentSessionModeVariant(session.mode)">
              {{ t(agentSessionModeKey(session.mode)) }}
            </Badge>
            <Badge
              v-if="agentSessionHeartbeat(session).running"
              variant="info"
            >
              {{ t("qualityAgent.sessionRunning") }}
            </Badge>
            <span
              v-if="session.lastActivityTime"
              class="text-xs text-muted-foreground"
              :title="formatDateTime(session.lastActivityTime, locale)"
            >
              {{ formatRelativeTime(session.lastActivityTime, locale) }}
            </span>
          </div>
        </div>
      </CardContent>
    </Card>

    <Card v-if="agent">
      <CardHeader class="border-b">
        <CardTitle class="text-base">{{ t("qualityAgent.reportsTitle") }}</CardTitle>
        <CardDescription>{{ t("qualityAgent.reportsHint") }}</CardDescription>
      </CardHeader>
      <CardContent class="space-y-2 pt-4">
        <p
          v-if="reports.length === 0"
          class="text-sm text-muted-foreground"
        >
          {{ t("qualityAgent.reportsEmpty") }}
        </p>
        <div
          v-for="report in reports"
          :key="report.name"
          class="flex flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm"
        >
          <div class="min-w-0">
            <p class="font-medium">
              {{ formatDateTime(report.periodStart, locale) }} – {{ formatDateTime(report.periodEnd, locale) }}
            </p>
            <p class="text-xs text-muted-foreground">
              {{ t("qualityAgent.reportCounts", {
                targets: report.targetCount,
                rules: report.ruleCount,
                failed: report.failedCount,
                repaired: report.repairedCount,
                waiting: report.escalatedCount + report.openCaseCount,
              }) }}
            </p>
          </div>
          <Button
            variant="outline"
            size="sm"
            @click="openReport(report)"
          >
            {{ t("qualityAgent.reportView") }}
          </Button>
        </div>
      </CardContent>
    </Card>

    <Dialog
      :open="reportOpen"
      @update:open="reportOpen = $event"
    >
      <DialogContent class="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{{ t("qualityAgent.reportDialogTitle") }}</DialogTitle>
        </DialogHeader>
        <AsyncMarkdown
          v-if="selectedReport?.bodyMarkdown"
          mode="chat"
          :content="selectedReport.bodyMarkdown"
          :final="true"
        />
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { AlertTriangle, ArrowDown, Loader2 } from "lucide-vue-next";
import {
  type ComponentPublicInstance,
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  adoptQualityAgentPendingSuggestions,
  getQualityAgent,
  listQualityAgentAttentions,
  listQualityAgentCases,
  listQualityAgentConversationMessages,
  listQualityAgentReports,
  listQualityAgentSessionsPage,
  pauseQualityAgent,
  sendQualityAgentMessage,
  setQualityAgentAdoption,
  startQualityAgent,
} from "@/api/data-quality";
import { AsyncMarkdown } from "@/components/common/AsyncMarkdown";
import PageState from "@/components/common/PageState.vue";
import QualityAgentAttentionItem from "@/components/data-quality/QualityAgentAttentionItem.vue";
import QualityAgentBreadcrumb from "@/components/data-quality/QualityAgentBreadcrumb.vue";
import QualityAgentTabs from "@/components/data-quality/QualityAgentTabs.vue";
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
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useChatScroll } from "@/composables/useChatScroll";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityAgent,
  QualityAgentAdoptionMode,
  type QualityAgentAttention,
  QualityAgentAttentionStatus,
  type QualityAgentCase,
  QualityAgentCaseState,
  QualityAgentMessageRole,
  type QualityAgentReport,
  type QualityAgentSession,
  type QualityAgentSessionMessage,
  QualityAgentState,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import {
  qualityAgentName,
  qualityAgentSessionRoute,
  qualityAgentSessionsRoute,
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
  lastTranscriptSeq,
} from "@/utils/qualityAgentSession";

const { t, locale } = useI18n();
const route = useRoute();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();
const agent = ref<QualityAgent>();
const loading = ref(true);
const error = ref("");
const busy = ref(false);

const attentions = ref<QualityAgentAttention[]>([]);
const cases = ref<QualityAgentCase[]>([]);
const reports = ref<QualityAgentReport[]>([]);
const reportOpen = ref(false);
const selectedReport = ref<QualityAgentReport>();
const messages = ref<QualityAgentSessionMessage[]>([]);
const sessions = ref<QualityAgentSession[]>([]);
const sessionsLoading = ref(false);
const sessionsError = ref("");
const chatLoading = ref(true);
const chatError = ref("");
const composer = ref("");
const sending = ref(false);
const { scrollerRef, pinned, onScroll, scrollToLatest, settleAtLatest } =
  useChatScroll();

/** Bind the pane through a function ref; the composable owns the ref itself. */
function bindScroller(element: Element | ComponentPublicInstance | null) {
  scrollerRef.value = element instanceof HTMLElement ? element : null;
}

/** The newest conversation seq the owner has seen while at the bottom. */
const seenSeq = ref(0n);
const lastConversationSeq = computed(() => lastTranscriptSeq(messages.value));
const chatUnread = computed(() =>
  pinned.value ? 0 : countMessagesAfterSeq(messages.value, seenSeq.value)
);
let pollTimer: number | undefined;

// Following the newest reply means the owner has seen everything loaded.
watch(
  [pinned, lastConversationSeq],
  ([following, newest]) => {
    if (following) {
      seenSeq.value = newest;
    }
  },
  { immediate: true }
);

const canManage = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.agents.manage")
);
const canChat = computed(
  () =>
    agent.value !== undefined &&
    agent.value.owner !== "" &&
    agent.value.owner === authStore.user?.name
);
const adoptionEnabled = computed(
  () =>
    agent.value?.adoption?.mode === QualityAgentAdoptionMode.ACTIVE ||
    agent.value?.adoption?.mode === QualityAgentAdoptionMode.PAUSED
);
// The owner or an administrator may approve the agent's drafts. The approval runs
// through the internal create path, so a member owner needs no rule-write
// permission; it is attributed to the approver and the autonomous ceiling is not
// charged.
const canApprove = computed(() => canChat.value || canManage.value);
const visibleMessages = computed(() =>
  messages.value.filter(
    (message) =>
      (message.role === QualityAgentMessageRole.USER ||
        message.role === QualityAgentMessageRole.ASSISTANT) &&
      message.content.trim() !== ""
  )
);
const agentId = computed(() => String(route.params.agentId));
const agentName = computed(() =>
  qualityAgentName(String(route.params.workspace), agentId.value)
);
const sessionsRoute = computed(() =>
  qualityAgentSessionsRoute(agentName.value)
);

function sessionRoute(session: QualityAgentSession) {
  return qualityAgentSessionRoute(agentName.value, session.name);
}

function agentLanguageLabel(language: string | undefined): string {
  switch (language) {
    case "zh-CN":
      return t("qualityAgents.languageChinese");
    case "en-US":
      return t("qualityAgents.languageEnglish");
    default:
      return t("qualityAgents.languageFollowOwner");
  }
}

function stateLabel(state: QualityAgentState | undefined): string {
  switch (state) {
    case QualityAgentState.ACTIVE:
      return t("qualityAgents.stateActive");
    case QualityAgentState.PAUSED:
      return t("qualityAgents.statePaused");
    case QualityAgentState.DISABLED:
      return t("qualityAgents.stateDisabled");
    case QualityAgentState.DRAFT:
      return t("qualityAgents.stateDraft");
    default:
      return t("qualityAgents.stateUnspecified");
  }
}

function stateVariant(state: QualityAgentState | undefined) {
  switch (state) {
    case QualityAgentState.ACTIVE:
      return "success" as const;
    case QualityAgentState.PAUSED:
      return "warning" as const;
    case QualityAgentState.DISABLED:
      return "neutral" as const;
    default:
      return "outline" as const;
  }
}

async function loadAgent() {
  loading.value = true;
  error.value = "";
  try {
    agent.value = await getQualityAgent(agentName.value);
  } catch (err) {
    error.value = formatError(err);
  } finally {
    loading.value = false;
  }
}

async function loadConversation() {
  chatLoading.value = true;
  chatError.value = "";
  try {
    const response = await listQualityAgentConversationMessages(
      agentName.value,
      { pageSize: 200 }
    );
    messages.value = response.qualityAgentConversationMessages;
    await landAtLatest();
  } catch (err) {
    chatError.value = formatError(err);
  } finally {
    chatLoading.value = false;
  }
}

async function loadSessions() {
  sessionsLoading.value = true;
  sessionsError.value = "";
  try {
    const page = await listQualityAgentSessionsPage({
      agent: agentName.value,
      pageSize: 5,
    });
    sessions.value = page.sessions;
  } catch (err) {
    sessionsError.value = formatError(err);
  } finally {
    sessionsLoading.value = false;
  }
}

async function loadAttentions() {
  try {
    attentions.value = await listQualityAgentAttentions(
      agentName.value,
      QualityAgentAttentionStatus.OPEN
    );
  } catch {
    attentions.value = [];
  }
}

/** The agent's open work items: what it is carrying right now. */
async function loadCases() {
  try {
    cases.value = await listQualityAgentCases(agentName.value, [
      QualityAgentCaseState.OPEN,
      QualityAgentCaseState.DIAGNOSED,
      QualityAgentCaseState.ACTED,
      QualityAgentCaseState.AWAITING_VERIFICATION,
    ]);
  } catch {
    cases.value = [];
  }
}

/**
 * qualityAgentCaseStateLabel renders one work-item state. The keys are spelled out rather than
 * built from the enum value: the i18n checker must see every one of them, and a new state is a
 * product decision, not a translation the build should guess.
 */
function qualityAgentCaseStateLabel(state: QualityAgentCaseState): string {
  switch (state) {
    case QualityAgentCaseState.OPEN:
      return t("qualityAgent.caseState.open");
    case QualityAgentCaseState.DIAGNOSED:
      return t("qualityAgent.caseState.diagnosed");
    case QualityAgentCaseState.ACTED:
      return t("qualityAgent.caseState.acted");
    case QualityAgentCaseState.AWAITING_VERIFICATION:
      return t("qualityAgent.caseState.awaitingVerification");
    case QualityAgentCaseState.RESOLVED:
      return t("qualityAgent.caseState.resolved");
    case QualityAgentCaseState.FROZEN:
      return t("qualityAgent.caseState.frozen");
    case QualityAgentCaseState.ESCALATED:
      return t("qualityAgent.caseState.escalated");
    default:
      return t("qualityAgent.caseState.unspecified");
  }
}

async function onAttentionResolved(name: string) {
  attentions.value = attentions.value.filter(
    (attention) => attention.name !== name
  );
  await loadConversation();
}

async function loadReports() {
  try {
    reports.value = await listQualityAgentReports(agentName.value);
  } catch {
    reports.value = [];
  }
}

function openReport(report: QualityAgentReport) {
  selectedReport.value = report;
  reportOpen.value = true;
}

/** Show the newest reply, keeping up while lazily rendered content settles. */
async function landAtLatest() {
  await nextTick();
  seenSeq.value = lastConversationSeq.value;
  await settleAtLatest();
}

async function jumpToConversationLatest() {
  seenSeq.value = lastConversationSeq.value;
  await scrollToLatest({ smooth: true });
}

async function send() {
  const content = composer.value.trim();
  if (content === "" || sending.value) {
    return;
  }
  sending.value = true;
  chatError.value = "";
  try {
    const created = await sendQualityAgentMessage(agentName.value, content, {
      requestId: `chat-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`,
    });
    messages.value = [
      ...messages.value.filter((message) => message.seq !== created.seq),
      created,
    ];
    composer.value = "";
    seenSeq.value = lastConversationSeq.value;
    await nextTick();
    await scrollToLatest();
    schedulePoll();
  } catch (err) {
    chatError.value = formatError(err);
  } finally {
    sending.value = false;
  }
}

/** Poll for the agent's reply after a message. One request at a time, deduped by name. */
function schedulePoll() {
  clearPoll();
  let attempts = 0;
  let inFlight = false;
  pollTimer = window.setInterval(async () => {
    if (inFlight) {
      return;
    }
    inFlight = true;
    attempts++;
    try {
      const last = messages.value.reduce(
        (max, message) => (message.seq > max ? message.seq : max),
        0n
      );
      const response = await listQualityAgentConversationMessages(
        agentName.value,
        { afterSeq: last, pageSize: 200 }
      );
      if (response.qualityAgentConversationMessages.length > 0) {
        const known = new Set(messages.value.map((message) => message.name));
        const fresh = response.qualityAgentConversationMessages.filter(
          (message) => !known.has(message.name)
        );
        if (fresh.length > 0) {
          messages.value = [...messages.value, ...fresh];
          if (pinned.value) {
            await nextTick();
            await scrollToLatest();
          }
        }
      }
    } catch {
      // Polling is best effort; the next manual refresh still shows the reply.
    } finally {
      inFlight = false;
    }
    // A model wake can take up to the session deadline; poll for five minutes.
    if (attempts >= 150) {
      clearPoll();
    }
  }, 2000);
}

function onComposerEnter(event: KeyboardEvent) {
  if (event.isComposing) {
    return;
  }
  void send();
}

function clearPoll() {
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer);
    pollTimer = undefined;
  }
}

async function toggleAdoption() {
  if (!agent.value) {
    return;
  }
  busy.value = true;
  try {
    const mode = adoptionEnabled.value
      ? QualityAgentAdoptionMode.OFF
      : QualityAgentAdoptionMode.PAUSED;
    agent.value = await setQualityAgentAdoption(agentName.value, mode);
    notify.success(t("qualityAgent.adoptionUpdated"));
  } catch (err) {
    notify.error(formatError(err));
  } finally {
    busy.value = false;
  }
}

/**
 * Approve the agent's pending drafts as the viewer's decision. The approval is
 * attributed to the viewer and the autonomous adoption guardrails and daily ceiling
 * do not apply; an optional target narrows it to one target.
 */
async function adoptPending(targetGuid = "") {
  if (!agent.value || busy.value) {
    return;
  }
  busy.value = true;
  try {
    const result = await adoptQualityAgentPendingSuggestions(
      agentName.value,
      targetGuid
    );
    agent.value = await getQualityAgent(agentName.value);
    await loadAttentions();
    await loadSessions();
    const message = t("qualityAgent.adoptPendingSuccess", {
      adopted: result.adopted,
      skipped: result.skipped,
      failed: result.failed,
      remaining: result.remainingPending,
    });
    if (result.failed > 0) {
      notify.warning(message);
    } else {
      notify.success(message);
    }
  } catch (err) {
    notify.error(formatError(err));
  } finally {
    busy.value = false;
  }
}

async function pause() {
  busy.value = true;
  try {
    agent.value = await pauseQualityAgent(agentName.value);
    notify.success(t("qualityAgents.paused"));
  } catch (err) {
    notify.error(formatError(err));
  } finally {
    busy.value = false;
  }
}

async function start() {
  busy.value = true;
  try {
    agent.value = await startQualityAgent(agentName.value);
    notify.success(t("qualityAgents.started"));
  } catch (err) {
    notify.error(formatError(err));
  } finally {
    busy.value = false;
  }
}

onMounted(async () => {
  await loadAgent();
  if (agent.value) {
    await Promise.all([
      loadConversation(),
      loadReports(),
      loadAttentions(),
      loadCases(),
      loadSessions(),
    ]);
  }
});

onBeforeUnmount(clearPoll);
</script>
