<template>
  <div class="space-y-3">
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

    <QualityAgentContextUsage
      :usage="session.contextUsage"
      :agent-model="agentModel"
      :agent-model-known="agentModelKnown"
      :session-mode="session.mode"
    />

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
      <template v-if="session.blockedReason">
        <p
          ref="reasonRef"
          class="whitespace-pre-wrap break-words text-warning"
          :class="{ 'line-clamp-4': !reasonExpanded }"
        >
          {{ session.blockedReason }}
        </p>
        <Button
          v-if="reasonOverflows"
          variant="link"
          size="xs"
          class="h-auto p-0 text-xs"
          :aria-expanded="reasonExpanded"
          @click="reasonExpanded = !reasonExpanded"
        >
          {{
            reasonExpanded
              ? t("qualityAgent.collapseReason")
              : t("qualityAgent.expandReason")
          }}
        </Button>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import QualityAgentContextUsage from "@/components/data-quality/QualityAgentContextUsage.vue";
import { Button } from "@/components/ui/button";
import type { QualityAgentSession } from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime } from "@/utils/datetime";
import type { AgentSessionHeartbeat } from "@/utils/qualityAgentSession";

/**
 * The durable state of one work session: whether it is running, how many wakes
 * it has taken, when it wakes next, and why it is blocked.
 *
 * The page renders it twice — as the right-hand column on wide screens and as a
 * disclosure above the transcript below `xl` — so the card chrome and the
 * placement stay with the page while the content lives here. The blocked reason
 * is model-written and can run to hundreds of characters; it is clamped so a long
 * reason cannot push the column past the height of the transcript pane, and the
 * expand control only appears when the clamp actually hides text.
 */
const props = defineProps<{
  session: QualityAgentSession;
  heartbeat: AgentSessionHeartbeat;
  statusLabel: string;
  nextWakeLabel: string;
  lastActivityLabel: string;
  currentTurn?: number;
  pollError: string;
  /** The model the agent is configured for now; used to flag a stale context sample. */
  agentModel?: string;
  /** False while the agent read is pending or failed; distinguishes "no model" from "unknown". */
  agentModelKnown?: boolean;
}>();

const { t, locale } = useI18n();
const reasonExpanded = ref(false);
const reasonOverflows = ref(false);
const reasonRef = ref<HTMLElement | null>(null);

let resizeObserver: ResizeObserver | undefined;

/** Whether the four-line clamp currently hides part of the reason. */
function measureReason(): void {
  // While expanded the clamp is gone; keep the control so the reader can collapse.
  if (reasonExpanded.value) {
    return;
  }
  const element = reasonRef.value;
  reasonOverflows.value = element
    ? element.scrollHeight > element.clientHeight + 1
    : false;
}

watch(
  () => props.session.blockedReason,
  async () => {
    reasonExpanded.value = false;
    await nextTick();
    measureReason();
  }
);

onMounted(() => {
  measureReason();
  if (typeof ResizeObserver !== "undefined" && reasonRef.value !== null) {
    resizeObserver = new ResizeObserver(() => {
      // A sidebar width change re-wraps the text, so the clamp may start or stop
      // hiding lines without the reason changing.
      measureReason();
    });
    resizeObserver.observe(reasonRef.value);
  }
});

onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  resizeObserver = undefined;
});
</script>
