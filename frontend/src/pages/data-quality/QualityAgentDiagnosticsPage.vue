<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('qualityAgent.diagnosticsTitle')"
      :description="t('qualityAgent.diagnosticsDescription')"
    >
      <template #actions>
        <Button
          variant="outline"
          size="sm"
          as-child
        >
          <RouterLink
            :to="{ name: 'QualityAgent', params: { workspace: String(route.params.workspace), agentId: String(route.params.agentId) } }"
          >
            {{ t("common.back") }}
          </RouterLink>
        </Button>
        <Button
          variant="outline"
          size="sm"
          :disabled="loading"
          @click="load"
        >
          <RefreshCcw class="mr-2 h-4 w-4" />
          {{ t("common.refresh") }}
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

    <Card>
      <CardHeader>
        <CardTitle class="text-base">{{ t("qualityAgent.diagnosticsSessions") }}</CardTitle>
        <CardDescription>{{ t("qualityAgent.diagnosticsSessionsHint") }}</CardDescription>
      </CardHeader>
      <CardContent>
        <PageState :loading="loading">
          <p
            v-if="sessions.length === 0"
            class="text-sm text-muted-foreground"
          >
            {{ t("qualityAgent.diagnosticsEmpty") }}
          </p>
          <Table v-else>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("qualityAgent.diagnosticsTarget") }}</TableHead>
                <TableHead>{{ t("qualityAgent.diagnosticsDatabase") }}</TableHead>
                <TableHead>{{ t("qualityAgent.diagnosticsState") }}</TableHead>
                <TableHead>{{ t("qualityAgent.diagnosticsNextWake") }}</TableHead>
                <TableHead class="text-right">{{ t("common.actions") }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="session in sessions"
                :key="session.name"
              >
                <TableCell class="max-w-[22rem] truncate font-mono text-xs">
                  {{ session.target?.metaGuid }}
                </TableCell>
                <TableCell class="max-w-[16rem] truncate font-mono text-xs">
                  {{ session.database }}
                </TableCell>
                <TableCell>
                  <Badge variant="outline">{{ stateLabel(session.state) }}</Badge>
                  <span
                    v-if="session.blockedCode"
                    class="ml-1 text-xs text-warning"
                  >
                    {{ session.blockedCode }}
                  </span>
                </TableCell>
                <TableCell class="text-xs text-muted-foreground">
                  {{ formatDateTime(session.nextWakeTime, locale) }}
                </TableCell>
                <TableCell class="text-right">
                  <div class="flex justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      @click="openTranscript(session)"
                    >
                      {{ t("qualityAgent.viewTranscript") }}
                    </Button>
                    <Button
                      v-if="session.state === QualityAgentSessionState.BLOCKED"
                      variant="ghost"
                      size="sm"
                      :disabled="busySession === session.name"
                      @click="resume(session)"
                    >
                      {{ t("qualityAgent.resumeSession") }}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </PageState>
      </CardContent>
    </Card>

    <Dialog
      :open="transcriptOpen"
      @update:open="transcriptOpen = $event"
    >
      <DialogContent class="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{{ t("qualityAgent.transcriptTitle") }}</DialogTitle>
          <DialogDescription>{{ t("qualityAgent.transcriptHint") }}</DialogDescription>
        </DialogHeader>
        <div class="space-y-2">
          <p
            v-if="transcript.length === 0"
            class="text-sm text-muted-foreground"
          >
            {{ t("qualityAgent.transcriptEmpty") }}
          </p>
          <div
            v-for="message in transcript"
            :key="message.name"
            class="rounded-md border p-2 text-xs"
          >
            <div class="mb-1 flex flex-wrap gap-2 text-muted-foreground">
              <span class="font-mono">#{{ message.seq }}</span>
              <span>{{ roleLabel(message.role) }}</span>
              <span>{{ originLabel(message.origin) }}</span>
            </div>
            <pre class="whitespace-pre-wrap break-words font-mono text-[11px]">{{ message.content }}</pre>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { AlertTriangle, RefreshCcw } from "lucide-vue-next";
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  listQualityAgentSessionMessages,
  listQualityAgentSessions,
  resumeQualityAgentSession,
} from "@/api/data-quality";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import {
  QualityAgentMessageOrigin,
  QualityAgentMessageRole,
  type QualityAgentSession,
  type QualityAgentSessionMessage,
  QualityAgentSessionState,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { qualityAgentName } from "@/utils/qualityAgent";

const { t, locale } = useI18n();
const route = useRoute();
const { formatError } = useErrorMessage();
const loading = ref(true);
const error = ref("");
const sessions = ref<QualityAgentSession[]>([]);
const busySession = ref("");
const transcriptOpen = ref(false);
const transcript = ref<QualityAgentSessionMessage[]>([]);

const agentName = qualityAgentName(
  String(route.params.workspace),
  String(route.params.agentId)
);

function stateLabel(state: QualityAgentSessionState): string {
  switch (state) {
    case QualityAgentSessionState.ENABLED:
      return t("qualityAgent.stateEnabled");
    case QualityAgentSessionState.BLOCKED:
      return t("qualityAgent.stateBlocked");
    case QualityAgentSessionState.ARCHIVED:
      return t("qualityAgent.stateArchived");
    default:
      return t("qualityAgents.stateUnspecified");
  }
}

function roleLabel(role: QualityAgentMessageRole): string {
  switch (role) {
    case QualityAgentMessageRole.USER:
      return t("qualityAgent.roleUser");
    case QualityAgentMessageRole.ASSISTANT:
      return t("qualityAgent.roleAssistant");
    case QualityAgentMessageRole.SYSTEM:
      return t("qualityAgent.roleSystem");
    case QualityAgentMessageRole.TOOL:
      return t("qualityAgent.roleTool");
    case QualityAgentMessageRole.ENGINE:
      return t("qualityAgent.roleEngine");
    case QualityAgentMessageRole.COMPACTION:
      return t("qualityAgent.roleCompaction");
    default:
      return t("qualityAgents.stateUnspecified");
  }
}

function originLabel(origin: QualityAgentMessageOrigin): string {
  switch (origin) {
    case QualityAgentMessageOrigin.WAKE:
      return t("qualityAgent.originWake");
    case QualityAgentMessageOrigin.EVENT:
      return t("qualityAgent.originEvent");
    case QualityAgentMessageOrigin.ASK:
      return t("qualityAgent.originAsk");
    case QualityAgentMessageOrigin.RUNNER:
      return t("qualityAgent.originRunner");
    case QualityAgentMessageOrigin.MODEL:
      return t("qualityAgent.originModel");
    case QualityAgentMessageOrigin.COMPACTION:
      return t("qualityAgent.originCompaction");
    default:
      return "";
  }
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    sessions.value = await listQualityAgentSessions({ agent: agentName });
  } catch (err) {
    error.value = formatError(err);
  } finally {
    loading.value = false;
  }
}

async function openTranscript(session: QualityAgentSession) {
  transcriptOpen.value = true;
  transcript.value = [];
  try {
    const response = await listQualityAgentSessionMessages(session.name, {
      pageSize: 200,
    });
    transcript.value = response.messages;
  } catch (err) {
    notify.error(formatError(err));
  }
}

async function resume(session: QualityAgentSession) {
  busySession.value = session.name;
  try {
    await resumeQualityAgentSession(session.name, session.revision);
    notify.success(t("qualityAgent.resumed"));
    await load();
  } catch (err) {
    notify.error(formatError(err));
  } finally {
    busySession.value = "";
  }
}

onMounted(load);
</script>
