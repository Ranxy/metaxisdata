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

    <div class="mt-1.5 space-y-3">
      <section
        v-for="question in questions"
        :key="question.id"
        class="space-y-1.5"
      >
        <p
          v-if="question.header"
          class="text-xs font-medium text-muted-foreground"
        >
          {{ question.header }}
        </p>
        <div
          class="relative"
          :class="isLong(question) && !expanded[question.id] ? 'max-h-40 overflow-hidden' : ''"
        >
          <AsyncMarkdown
            mode="chat"
            :content="question.question"
            :final="true"
          />
          <div
            v-if="isLong(question) && !expanded[question.id]"
            class="pointer-events-none absolute inset-x-0 bottom-0 h-10 bg-gradient-to-t from-card to-transparent"
          />
        </div>
        <Button
          v-if="isLong(question)"
          variant="link"
          size="xs"
          class="mt-1 h-auto p-0 text-xs"
          @click="expanded[question.id] = !expanded[question.id]"
        >
          {{ expanded[question.id] ? t("qualityAgent.askShowLess") : t("qualityAgent.askShowFull") }}
        </Button>
        <p
          v-if="question.detail"
          class="whitespace-pre-wrap text-xs text-muted-foreground"
        >
          {{ question.detail }}
        </p>
        <p
          v-if="question.requests.length > 0"
          class="text-xs text-muted-foreground"
        >
          {{ questionScope(question) }}
        </p>

        <div
          v-if="questions.length > 1 && isOpen && canReply"
          class="flex items-center justify-end gap-2"
        >
          <span
            v-if="isSkipped(question)"
            class="text-xs text-muted-foreground"
          >
            {{ t("qualityAgent.answerSkippedHint") }}
          </span>
          <Button
            variant="link"
            size="xs"
            class="h-auto p-0 text-xs"
            :disabled="busy"
            @click="toggleSkip(question)"
          >
            {{ isSkipped(question) ? t("qualityAgent.answerUnskip") : t("qualityAgent.answerSkip") }}
          </Button>
        </div>

        <div
          v-if="question.options.length > 0 && !isSkipped(question)"
          class="space-y-1.5"
        >
          <p class="text-xs text-muted-foreground">
            {{ question.multiSelect ? t("qualityAgent.answerChooseAny") : t("qualityAgent.answerChooseOne") }}
          </p>
          <div class="flex flex-col gap-1.5">
            <button
              v-for="option in question.options"
              :key="option.label"
              type="button"
              class="rounded-md border px-3 py-2 text-left transition-colors"
              :class="optionClass(question, option.label)"
              :aria-pressed="isSelected(question, option.label)"
              :disabled="!canReply || !isOpen || busy"
              @click="toggleOption(question, option.label)"
            >
              <span class="block text-sm">{{ option.label }}</span>
              <span
                v-if="option.description"
                class="mt-0.5 block text-xs text-muted-foreground"
              >
                {{ option.description }}
              </span>
            </button>
          </div>
        </div>

        <div
          v-if="isOpen && canReply && !isSkipped(question)"
          class="space-y-1"
        >
          <textarea
            v-model="custom[question.id]"
            rows="2"
            class="w-full resize-none rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
            :aria-label="question.header || question.question"
            :placeholder="question.options.length > 0 ? t('qualityAgent.answerOtherPlaceholder') : t('qualityAgent.askReplyPlaceholder')"
            :disabled="busy"
            @input="onCustomInput(question)"
            @keydown.enter.exact.prevent="onEnter"
          />
        </div>
        <p
          v-else-if="isOpen && !isSkipped(question)"
          class="text-xs text-muted-foreground"
        >
          {{ t("qualityAgent.asksReadOnly") }}
        </p>
        <div
          v-else-if="ask.status !== QualityAgentAskStatus.DISMISSED && ask.answers.length > 0"
          class="rounded-md bg-muted/40 px-3 py-2"
        >
          <p class="text-xs text-muted-foreground">
            {{ t("qualityAgent.answerLabel") }}
          </p>
          <p
            v-if="answerValues(question).length === 0"
            class="text-sm text-muted-foreground"
          >
            {{ t("qualityAgent.answerSkipped") }}
          </p>
          <div
            v-else
            class="flex flex-wrap gap-1.5"
          >
            <Badge
              v-for="value in answerValues(question)"
              :key="value"
              variant="secondary"
            >
              {{ value }}
            </Badge>
          </div>
        </div>
        <p
          v-else-if="ask.status === QualityAgentAskStatus.DISMISSED"
          class="text-xs text-muted-foreground"
        >
          {{ t("qualityAgent.askDismissed") }}
        </p>
        <p
          v-else-if="!isSkipped(question) && ask.answerMessageSeq > 0n"
          class="text-xs text-muted-foreground"
        >
          {{ t("qualityAgent.answerDelivered") }}
        </p>
      </section>
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
      <p
        v-if="!canSend"
        class="text-xs text-muted-foreground"
      >
        {{ t("qualityAgent.answerRequired") }}
      </p>
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
          :disabled="busy || !canSend"
          @click="send"
        >
          {{ busy ? t("qualityAgent.sending") : t("qualityAgent.askReply") }}
        </Button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { computed, reactive, watch } from "vue";
import { useI18n } from "vue-i18n";
import { AsyncMarkdown } from "@/components/common/AsyncMarkdown";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  type QualityAgentAnswer,
  QualityAgentAnswerSchema,
  type QualityAgentAsk,
  QualityAgentAskStatus,
  type QualityAgentQuestion,
  QualityAgentQuestionSchema,
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
  reply: [ask: QualityAgentAsk, answers: QualityAgentAnswer[]];
  dismiss: [ask: QualityAgentAsk];
  approve: [ask: QualityAgentAsk];
}>();

const { t, locale } = useI18n();
const selected = reactive<Record<string, string[]>>({});
const custom = reactive<Record<string, string>>({});
const skipped = reactive<Record<string, boolean>>({});
const expanded = reactive<Record<string, boolean>>({});

const isOpen = computed(() => props.ask.status === QualityAgentAskStatus.OPEN);

// A legacy ask has only the text question; present it as one free-text item so
// the card, the answer payload and the server all speak the same shape.
const questions = computed<QualityAgentQuestion[]>(() => {
  if (props.ask.questions.length > 0) {
    return props.ask.questions;
  }
  return [
    create(QualityAgentQuestionSchema, {
      id: "q1",
      question: props.ask.question,
    }),
  ];
});

function isSelected(question: QualityAgentQuestion, label: string): boolean {
  if (!isOpen.value) {
    return (answerFor(question)?.selected ?? []).includes(label);
  }
  return (selected[question.id] ?? []).includes(label);
}

function toggleOption(question: QualityAgentQuestion, label: string) {
  const current = selected[question.id] ?? [];
  if (question.multiSelect) {
    selected[question.id] = current.includes(label)
      ? current.filter((value) => value !== label)
      : [...current, label];
    return;
  }
  selected[question.id] = current.includes(label) ? [] : [label];
  // A single-select custom answer replaces the choice; clear it so the card and
  // the server agree on which one the owner meant.
  custom[question.id] = "";
}

function onCustomInput(question: QualityAgentQuestion) {
  if (!question.multiSelect && (custom[question.id] ?? "").trim() !== "") {
    selected[question.id] = [];
  }
}

function isSkipped(question: QualityAgentQuestion): boolean {
  return skipped[question.id] === true;
}

// A batch may leave a question unanswered on purpose. Only a batch shows this: a
// single-question ask is dismissed instead, so "send an empty answer" stays
// unambiguous.
function toggleSkip(question: QualityAgentQuestion) {
  if (isSkipped(question)) {
    skipped[question.id] = false;
    return;
  }
  skipped[question.id] = true;
  selected[question.id] = [];
  custom[question.id] = "";
}

function answerComplete(question: QualityAgentQuestion): boolean {
  return (
    isSkipped(question) ||
    (selected[question.id]?.length ?? 0) > 0 ||
    (custom[question.id] ?? "").trim() !== ""
  );
}

const canSend = computed(
  () => isOpen.value && questions.value.every(answerComplete)
);

watch(
  questions,
  (list) => {
    for (const question of list) {
      selected[question.id] ??= [];
      custom[question.id] ??= "";
      skipped[question.id] ??= false;
    }
  },
  { immediate: true }
);

function optionClass(question: QualityAgentQuestion, label: string): string {
  return isSelected(question, label)
    ? "border-primary bg-primary/10"
    : "border-input hover:bg-muted/60";
}

// A question is prose, often a screenful of Markdown. The clamp keeps the answer
// controls reachable; the length test is a heuristic because the Markdown renderer
// is async and measuring it would make the first paint jump.
function isLong(question: QualityAgentQuestion): boolean {
  return (
    question.question.length > 180 || question.question.split("\n").length > 6
  );
}

function questionScope(question: QualityAgentQuestion): string {
  const count = question.requests.length;
  const target = props.ask.targets[0];
  if (props.ask.targets.length === 1 && target) {
    return t("qualityAgent.askQuestionTarget", {
      target: target.path?.object || target.metaGuid,
      count,
    });
  }
  return t("qualityAgent.askQuestionRequestCount", { count });
}

function answerFor(question: QualityAgentQuestion) {
  return props.ask.answers.find((answer) => answer.id === question.id);
}

function answerValues(question: QualityAgentQuestion): string[] {
  const answer = answerFor(question);
  if (!answer) {
    return [];
  }
  const values = [...answer.selected];
  if (answer.custom.trim() !== "") {
    values.push(answer.custom);
  }
  return values;
}

function onEnter(event: KeyboardEvent) {
  // An IME uses Enter to confirm a candidate; sending then would submit half a word.
  if (event.isComposing) {
    return;
  }
  send();
}

function send() {
  if (!canSend.value || props.busy) {
    return;
  }
  const answers = questions.value.map((question) => {
    const text = isSkipped(question) ? "" : (custom[question.id] ?? "").trim();
    const picks =
      isSkipped(question) || (text !== "" && !question.multiSelect)
        ? []
        : [...(selected[question.id] ?? [])];
    return create(QualityAgentAnswerSchema, {
      id: question.id,
      selected: picks,
      custom: text,
    });
  });
  emit("reply", props.ask, answers);
  for (const question of questions.value) {
    custom[question.id] = "";
    selected[question.id] = [];
    skipped[question.id] = false;
  }
}
</script>
