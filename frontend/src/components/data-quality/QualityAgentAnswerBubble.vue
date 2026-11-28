<template>
  <div
    v-if="envelope"
    class="max-w-[85%] space-y-2 rounded-lg bg-muted px-3 py-2 text-sm"
  >
    <section
      v-for="question in envelope.questions"
      :key="question.id"
      class="space-y-1"
    >
      <p class="text-xs text-muted-foreground">
        {{ question.header || question.question }}
      </p>
      <p
        v-if="question.header"
        class="text-sm"
      >
        {{ question.question }}
      </p>
      <p
        v-if="valuesFor(question.id).length === 0"
        class="text-xs text-muted-foreground"
      >
        {{ t("qualityAgent.answerSkipped") }}
      </p>
      <div
        v-else
        class="flex flex-wrap gap-1.5"
      >
        <Badge
          v-for="value in valuesFor(question.id)"
          :key="value"
          variant="secondary"
        >
          {{ value }}
        </Badge>
      </div>
    </section>
  </div>
  <p
    v-else
    class="max-w-[85%] whitespace-pre-wrap break-words rounded-lg bg-primary px-3 py-2 text-sm text-primary-foreground"
  >
    {{ content }}
  </p>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";

interface AnswerOption {
  label: string;
  description?: string;
}

interface AnswerQuestion {
  id: string;
  header?: string;
  question: string;
  options?: AnswerOption[];
  multiSelect?: boolean;
}

interface AnswerItem {
  id: string;
  // Older envelopes encoded a skipped question as null instead of [].
  selected?: string[] | null;
  custom?: string;
}

interface AnswerEnvelope {
  kind: string;
  ask: string;
  questions: AnswerQuestion[];
  answers: AnswerItem[];
  requests?: string[];
}

const props = defineProps<{ content: string }>();
const { t } = useI18n();

// Pair by the stable question id. A payload that does not name every question is
// rendered as plain text rather than guessed into a Q&A card.
const envelope = computed<AnswerEnvelope | null>(() => {
  let parsed: unknown;
  try {
    parsed = JSON.parse(props.content);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) {
    return null;
  }
  const candidate = parsed as Partial<AnswerEnvelope>;
  if (
    candidate.kind !== "answer_to_ask" ||
    !Array.isArray(candidate.questions) ||
    !Array.isArray(candidate.answers)
  ) {
    return null;
  }
  const ids = new Set<string>();
  for (const question of candidate.questions) {
    if (
      !question ||
      typeof question.id !== "string" ||
      typeof question.question !== "string" ||
      ids.has(question.id)
    ) {
      return null;
    }
    ids.add(question.id);
  }
  const answered = new Set<string>();
  for (const answer of candidate.answers) {
    if (
      !answer ||
      typeof answer.id !== "string" ||
      (answer.selected != null && !Array.isArray(answer.selected))
    ) {
      return null;
    }
    if (!ids.has(answer.id) || answered.has(answer.id)) {
      return null;
    }
    answered.add(answer.id);
  }
  if (answered.size !== ids.size) {
    return null;
  }
  return candidate as AnswerEnvelope;
});

function valuesFor(id: string): string[] {
  const answer = envelope.value?.answers.find((item) => item.id === id);
  if (!answer) {
    return [];
  }
  const values = [...(answer.selected ?? [])];
  if ((answer.custom ?? "").trim() !== "") {
    values.push((answer.custom ?? "").trim());
  }
  return values;
}
</script>
