<template>
  <div class="rounded-md border px-3 py-3 text-sm">
    <div class="flex flex-wrap items-center gap-2">
      <Badge variant="warning">
        {{ t(qualityAgentAskKindKey(ask.kind)) }}
      </Badge>
      <Badge variant="secondary">
        {{ t(qualityAgentAskStatusKey(ask.status)) }}
      </Badge>
      <span
        v-if="ask.createTime"
        class="ml-auto text-xs text-muted-foreground"
        :title="formatDateTime(ask.createTime, locale)"
      >
        {{ formatRelativeTime(ask.createTime, locale) }}
      </span>
    </div>
    <p class="mt-2 font-medium">
      {{ ask.title || t(qualityAgentAskKindKey(ask.kind)) }}
    </p>
    <p class="mt-1 whitespace-pre-wrap break-words text-muted-foreground">
      {{ ask.question }}
    </p>
    <div
      v-if="ask.targets.length > 0 || ask.requestCount > 0"
      class="mt-2 flex flex-wrap items-center gap-1"
    >
      <Badge
        v-for="target in ask.targets"
        :key="target.metaGuid"
        variant="secondary"
      >
        {{ target.path?.object || target.metaGuid }}
      </Badge>
      <span
        v-if="ask.requestCount > 0"
        class="text-xs text-muted-foreground"
      >
        {{ t("qualityAgent.askRequestCount", { count: ask.requestCount }) }}
      </span>
    </div>
    <p
      v-if="ask.answeredBy"
      class="mt-2 text-xs text-muted-foreground"
    >
      {{ t("qualityAgent.askAnsweredBy", { user: ask.answeredBy }) }}
    </p>

    <div
      v-if="canApprove && qualityAgentAskIsApproval(ask)"
      class="mt-3 flex justify-end"
    >
      <Button
        size="sm"
        variant="secondary"
        :disabled="busy"
        @click="emit('approve', ask)"
      >
        {{ t("qualityAgent.askApprovePending") }}
      </Button>
    </div>

    <div
      v-if="canReply && ask.status === QualityAgentAskStatus.OPEN"
      class="mt-3 space-y-2 border-t pt-3"
    >
      <textarea
        v-model="reply"
        rows="2"
        class="w-full resize-none rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
        :placeholder="t('qualityAgent.askReplyPlaceholder')"
        :disabled="busy"
        @keydown.enter.exact.prevent="onEnter"
      />
      <div class="flex justify-end gap-2">
        <Button
          variant="ghost"
          size="sm"
          :disabled="busy"
          @click="emit('dismiss', ask)"
        >
          {{ t("qualityAgent.askDismiss") }}
        </Button>
        <Button
          size="sm"
          :disabled="busy || reply.trim() === ''"
          @click="send"
        >
          {{ busy ? t("qualityAgent.sending") : t("qualityAgent.askReply") }}
        </Button>
      </div>
    </div>
    <p
      v-else-if="ask.status === QualityAgentAskStatus.ANSWERED"
      class="mt-3 border-t pt-3 text-xs text-muted-foreground"
    >
      {{ t("qualityAgent.askAwaitingLead") }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  type QualityAgentAsk,
  QualityAgentAskStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import {
  qualityAgentAskIsApproval,
  qualityAgentAskKindKey,
  qualityAgentAskStatusKey,
} from "@/utils/qualityAgent";

const props = defineProps<{
  ask: QualityAgentAsk;
  canReply: boolean;
  canApprove: boolean;
  busy: boolean;
}>();

const emit = defineEmits<{
  reply: [ask: QualityAgentAsk, text: string];
  dismiss: [ask: QualityAgentAsk];
  approve: [ask: QualityAgentAsk];
}>();

const { t, locale } = useI18n();
const reply = ref("");

function onEnter(event: KeyboardEvent) {
  // An IME uses Enter to confirm a candidate; sending then would submit half a word.
  if (event.isComposing) {
    return;
  }
  send();
}

function send() {
  const text = reply.value.trim();
  if (text === "" || props.busy) {
    return;
  }
  emit("reply", props.ask, text);
  reply.value = "";
}
</script>
