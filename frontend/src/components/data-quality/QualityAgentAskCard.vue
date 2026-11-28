<template>
  <div class="rounded-lg border bg-card px-3 py-3 text-sm">
    <div class="flex flex-wrap items-center gap-2">
      <Badge variant="secondary">
        {{ t(qualityAgentAskKindKey(ask.kind)) }}
      </Badge>
      <Badge :variant="isOpen ? 'warning' : 'neutral'">
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
    <div class="mt-1.5">
      <div
        class="relative"
        :class="isLong && !expanded ? 'max-h-40 overflow-hidden' : ''"
      >
        <AsyncMarkdown
          mode="chat"
          :content="ask.question"
          :final="true"
        />
        <div
          v-if="isLong && !expanded"
          class="pointer-events-none absolute inset-x-0 bottom-0 h-10 bg-gradient-to-t from-card to-transparent"
        />
      </div>
      <Button
        v-if="isLong"
        variant="link"
        size="xs"
        class="mt-1 h-auto p-0 text-xs"
        @click="expanded = !expanded"
      >
        {{ expanded ? t("qualityAgent.askShowLess") : t("qualityAgent.askShowFull") }}
      </Button>
    </div>
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
      class="mt-2 flex justify-end"
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
      v-if="canReply && isOpen"
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
      class="mt-2 border-t pt-2 text-xs text-muted-foreground"
    >
      {{ t("qualityAgent.askAwaitingLead") }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { AsyncMarkdown } from "@/components/common/AsyncMarkdown";
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
const expanded = ref(false);

const isOpen = computed(() => props.ask.status === QualityAgentAskStatus.OPEN);

// A question is prose, often a screenful of Markdown. It stays clamped so the
// answer box is reachable without scrolling the whole question first; the
// length test is a heuristic because the Markdown renderer is async and
// measuring it would make the first paint jump.
const isLong = computed(() => {
  const question = props.ask.question;
  return question.length > 180 || question.split("\n").length > 6;
});

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
