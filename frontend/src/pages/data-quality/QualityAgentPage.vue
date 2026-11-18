<template>
  <div class="space-y-4">
    <PageHeader
      :title="agent?.displayName || t('qualityAgents.title')"
      :description="agent?.description || ''"
    >
      <template #actions>
        <Button
          variant="outline"
          size="sm"
          as-child
        >
          <RouterLink :to="{ name: 'QualityAgents' }">
            {{ t("common.back") }}
          </RouterLink>
        </Button>
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
        <Button
          v-if="canManage"
          size="sm"
          variant="outline"
          as-child
        >
          <RouterLink
            :to="{ name: 'QualityAgentDiagnostics', params: { workspace: String(route.params.workspace), agentId: String(route.params.agentId) } }"
          >
            {{ t("qualityAgent.openDiagnostics") }}
          </RouterLink>
        </Button>
      </template>
    </PageHeader>

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
        <Card class="flex min-h-[28rem] flex-col">
          <CardHeader class="border-b">
            <CardTitle class="text-base">{{ t("qualityAgent.conversationTitle") }}</CardTitle>
            <CardDescription>{{ t("qualityAgent.conversationHint") }}</CardDescription>
          </CardHeader>
          <CardContent class="flex min-h-0 flex-1 flex-col gap-3 pt-4">
            <div
              ref="timeline"
              class="min-h-0 flex-1 space-y-3 overflow-y-auto pr-1"
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
            </CardContent>
          </Card>
        </div>
      </div>
    </PageState>

    <Card v-if="agent">
      <CardHeader class="border-b">
        <CardTitle class="text-base">{{ t("qualityAgent.attentionTitle") }}</CardTitle>
        <CardDescription>{{ t("qualityAgent.attentionHint") }}</CardDescription>
      </CardHeader>
      <CardContent class="space-y-2 pt-4">
        <p
          v-if="attentions.length === 0"
          class="text-sm text-muted-foreground"
        >
          {{ t("qualityAgent.attentionEmpty") }}
        </p>
        <div
          v-for="attention in attentions"
          :key="attention.name"
          class="rounded-md border px-3 py-2 text-sm"
        >
          <div class="flex flex-wrap items-start justify-between gap-2">
            <div class="min-w-0">
              <p class="font-medium">{{ attention.title }}</p>
              <p class="whitespace-pre-wrap break-words text-xs text-muted-foreground">
                {{ attention.question }}
              </p>
            </div>
            <div
              v-if="canChat"
              class="flex gap-1"
            >
              <Button
                size="sm"
                @click="openReply(attention)"
              >
                {{ t("qualityAgent.attentionReply") }}
              </Button>
              <Button
                size="sm"
                variant="outline"
                :disabled="busyAttention === attention.name"
                @click="dismissAttention(attention)"
              >
                {{ t("qualityAgent.attentionDismiss") }}
              </Button>
            </div>
          </div>
        </div>
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
                issues: report.openIssueCount,
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
      :open="replyOpen"
      @update:open="replyOpen = $event"
    >
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("qualityAgent.attentionDialogTitle") }}</DialogTitle>
          <DialogDescription>{{ selectedAttention?.title }}</DialogDescription>
        </DialogHeader>
        <p class="whitespace-pre-wrap break-words text-sm text-muted-foreground">
          {{ selectedAttention?.question }}
        </p>
        <textarea
          v-model="replyBody"
          rows="4"
          class="mt-2 w-full resize-none rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          :placeholder="t('qualityAgent.attentionDialogHint')"
          :disabled="replying"
        />
        <div class="flex justify-end gap-2">
          <Button
            variant="outline"
            :disabled="replying"
            @click="replyOpen = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="replying || replyBody.trim() === ''"
            @click="submitReply"
          >
            <Loader2
              v-if="replying"
              class="mr-2 h-4 w-4 animate-spin"
            />
            {{ t("qualityAgent.attentionSend") }}
          </Button>
        </div>
      </DialogContent>
    </Dialog>

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
import { AlertTriangle, Loader2 } from "lucide-vue-next";
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  dismissQualityAgentAttention,
  getQualityAgent,
  listQualityAgentAttentions,
  listQualityAgentConversationMessages,
  listQualityAgentReports,
  pauseQualityAgent,
  replyQualityAgentAttention,
  sendQualityAgentMessage,
  setQualityAgentAdoption,
  startQualityAgent,
} from "@/api/data-quality";
import { AsyncMarkdown } from "@/components/common/AsyncMarkdown";
import PageState from "@/components/common/PageState.vue";
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
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityAgent,
  QualityAgentAdoptionMode,
  type QualityAgentAttention,
  QualityAgentAttentionStatus,
  QualityAgentMessageRole,
  type QualityAgentReport,
  type QualityAgentSessionMessage,
  QualityAgentState,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { qualityAgentName } from "@/utils/qualityAgent";

const { t, locale } = useI18n();
const route = useRoute();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();
const agent = ref<QualityAgent>();
const loading = ref(true);
const error = ref("");
const busy = ref(false);

const attentions = ref<QualityAgentAttention[]>([]);
const replyOpen = ref(false);
const selectedAttention = ref<QualityAgentAttention>();
const replyBody = ref("");
const replying = ref(false);
const busyAttention = ref("");
const reports = ref<QualityAgentReport[]>([]);
const reportOpen = ref(false);
const selectedReport = ref<QualityAgentReport>();
const messages = ref<QualityAgentSessionMessage[]>([]);
const chatLoading = ref(true);
const chatError = ref("");
const composer = ref("");
const sending = ref(false);
const timeline = ref<HTMLElement>();
let pollTimer: number | undefined;

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
const visibleMessages = computed(() =>
  messages.value.filter(
    (message) =>
      (message.role === QualityAgentMessageRole.USER ||
        message.role === QualityAgentMessageRole.ASSISTANT) &&
      message.content.trim() !== ""
  )
);
const agentName = computed(() =>
  qualityAgentName(String(route.params.workspace), String(route.params.agentId))
);

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
    await scrollToBottom();
  } catch (err) {
    chatError.value = formatError(err);
  } finally {
    chatLoading.value = false;
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

function openReply(attention: QualityAgentAttention) {
  selectedAttention.value = attention;
  replyBody.value = "";
  replyOpen.value = true;
}

async function submitReply() {
  const attention = selectedAttention.value;
  if (!attention || replyBody.value.trim() === "") {
    return;
  }
  replying.value = true;
  try {
    await replyQualityAgentAttention(attention.name, replyBody.value.trim(), {
      requestId: `reply-${Date.now().toString(36)}`,
    });
    replyOpen.value = false;
    notify.success(t("qualityAgent.attentionSent"));
    await Promise.all([loadAttentions(), loadConversation()]);
  } catch (err) {
    notify.error(formatError(err));
  } finally {
    replying.value = false;
  }
}

async function dismissAttention(attention: QualityAgentAttention) {
  busyAttention.value = attention.name;
  try {
    await dismissQualityAgentAttention(attention.name);
    notify.success(t("qualityAgent.attentionDismissed"));
    await loadAttentions();
  } catch (err) {
    notify.error(formatError(err));
  } finally {
    busyAttention.value = "";
  }
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

async function scrollToBottom() {
  await nextTick();
  const element = timeline.value;
  if (element) {
    element.scrollTop = element.scrollHeight;
  }
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
    await scrollToBottom();
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
          await scrollToBottom();
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
    await Promise.all([loadConversation(), loadReports(), loadAttentions()]);
  }
});

onBeforeUnmount(clearPoll);
</script>
