<template>
  <Card>
    <CardHeader class="border-b">
      <CardTitle class="text-base">
        {{ t("dataQuality.agent.cardTitle") }}
      </CardTitle>
      <CardDescription>{{ t("dataQuality.agent.cardDescription") }}</CardDescription>
    </CardHeader>
    <CardContent class="space-y-3 pt-4">
      <p
        v-if="loading && !session"
        class="text-sm text-muted-foreground"
      >
        {{ t("common.loading") }}
      </p>
      <Alert
        v-else-if="error"
        variant="destructive"
      >
        <AlertTriangle class="h-4 w-4" />
        <AlertDescription class="flex flex-wrap items-center gap-2">
          {{ error }}
          <Button
            variant="outline"
            size="sm"
            @click="load"
          >
            {{ t("common.retry") }}
          </Button>
        </AlertDescription>
      </Alert>

      <template v-else-if="session">
        <div class="flex flex-wrap items-center gap-2">
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
        </div>

        <p
          v-if="session.blockedReason || session.blockedCode"
          class="rounded-md border border-warning/40 bg-warning-subtle/30 px-2.5 py-1.5 text-xs text-warning"
        >
          {{ session.blockedCode
          }}<template v-if="session.blockedReason"> · {{ session.blockedReason }}</template>
        </p>

        <dl class="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
          <dt class="text-muted-foreground">
            {{ t("dataQuality.agent.lastActivity") }}
          </dt>
          <dd
            class="tabular-nums"
            :title="formatDateTime(session.lastActivityTime, locale)"
          >
            {{ lastActivityLabel }}
          </dd>
          <dt class="text-muted-foreground">
            {{ t("dataQuality.agent.columnWakes") }}
          </dt>
          <dd class="tabular-nums">
            {{ session.wakeCount }}
          </dd>
        </dl>

        <div class="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            as-child
          >
            <RouterLink :to="conversationTo">
              {{ t("dataQuality.agent.viewConversation") }}
            </RouterLink>
          </Button>
        </div>
        <AgentSessionActions
          :session="session"
          :can-update="canUpdate"
          @updated="onUpdated"
          @archived="load"
          @reload="load"
        />
      </template>

      <template v-else>
        <p class="text-sm text-muted-foreground">
          {{ t("dataQuality.agent.cardEmpty") }}
        </p>
        <p class="text-xs text-muted-foreground">
          {{ t("dataQuality.agent.cardEmptyHint") }}
        </p>
        <Button
          v-if="canUpdate"
          :disabled="starting"
          @click="start"
        >
          <component
            :is="starting ? Loader2 : Play"
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': starting }"
          />
          {{ t("dataQuality.agent.start") }}
        </Button>
        <p
          v-if="startError"
          class="text-xs text-destructive"
        >
          {{ startError }}
        </p>
      </template>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { AlertTriangle, Loader2, Play } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import {
  createQualityAgentSession,
  listQualityAgentSessions,
} from "@/api/data-quality";
import AgentSessionActions from "@/components/data-quality/AgentSessionActions.vue";
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
import { notify } from "@/lib/notify";
import type { QualityAgentSession } from "@/types/proto-es/v1/data_quality_service_pb";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  agentSessionHeartbeat,
  agentSessionStateKey,
  agentSessionStateVariant,
} from "@/utils/agentSession";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * The target page's agent-session card: the one long-lived conversation this
 * table has with the built-in agent, or the invitation to start it.
 *
 * It is deliberately a card, not a conversation view — the full transcript lives
 * on `/data-quality/agent/...` — so the target workspace keeps its own scope.
 * The session is read by target, so an archived session does not block starting
 * a new one (the server enforces one live session per target).
 */

const props = defineProps<{
  targetGuid: string;
  metaType: MetaType;
  canUpdate: boolean;
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();

const session = ref<QualityAgentSession>();
const loading = ref(false);
const error = ref("");
const starting = ref(false);
const startError = ref("");
let generation = 0;

const heartbeat = computed(() =>
  session.value
    ? agentSessionHeartbeat(session.value)
    : { running: false, stale: false }
);

const lastActivityLabel = computed(() =>
  session.value?.lastActivityTime
    ? formatRelativeTime(session.value.lastActivityTime, locale.value)
    : t("dataQuality.agent.noActivity")
);

/** The resource name travels one path segment per name segment. */
const conversationTo = computed(() => ({
  name: "DataQualityAgentSession",
  params: {
    sessionId: (session.value?.name ?? "").split("/"),
  },
}));

async function load() {
  if (!props.targetGuid) {
    session.value = undefined;
    return;
  }
  const mine = ++generation;
  loading.value = true;
  error.value = "";
  try {
    const sessions = await listQualityAgentSessions({
      targetGuid: props.targetGuid,
    });
    if (mine !== generation) {
      return;
    }
    session.value = sessions[0];
  } catch (e: unknown) {
    if (mine !== generation) {
      return;
    }
    error.value = formatError(e, "dataQuality.agent.cardLoadFailed");
  } finally {
    if (mine === generation) {
      loading.value = false;
    }
  }
}

async function start() {
  if (starting.value) {
    return;
  }
  starting.value = true;
  startError.value = "";
  try {
    const created = await createQualityAgentSession({
      targetGuid: props.targetGuid,
      metaType: props.metaType,
    });
    session.value = created;
    notify.success(t("dataQuality.agent.started"));
  } catch (e: unknown) {
    startError.value = formatError(e, "dataQuality.agent.startFailed");
  } finally {
    starting.value = false;
  }
}

function onUpdated(updated: QualityAgentSession) {
  session.value = updated;
}

watch(() => props.targetGuid, load, { immediate: true });
</script>
