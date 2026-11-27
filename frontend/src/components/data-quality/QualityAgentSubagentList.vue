<template>
  <p
    v-if="loading && sessions.length === 0"
    class="text-sm text-muted-foreground"
  >
    {{ t("common.loading") }}
  </p>
  <p
    v-else-if="error"
    class="text-sm text-destructive"
  >
    {{ error }}
  </p>
  <p
    v-else-if="sessions.length === 0"
    class="text-sm text-muted-foreground"
  >
    {{ t("qualityAgent.teamEmpty") }}
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
        {{ agentSessionDisplayName(session, t("qualityAgent.roleLead")) }}
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
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import type { RouteLocationRaw } from "vue-router";
import { Badge } from "@/components/ui/badge";
import type { QualityAgentSession } from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import {
  agentSessionDisplayName,
  agentSessionHeartbeat,
  agentSessionModeKey,
  agentSessionModeVariant,
  agentSessionStateKey,
  agentSessionStateVariant,
  agentSessionTargetPath,
} from "@/utils/qualityAgentSession";

defineProps<{
  sessions: QualityAgentSession[];
  loading: boolean;
  error: string;
  sessionRoute: (session: QualityAgentSession) => RouteLocationRaw;
}>();

const { t, locale } = useI18n();
</script>
