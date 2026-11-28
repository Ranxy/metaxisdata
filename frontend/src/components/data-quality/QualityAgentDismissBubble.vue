<template>
  <div
    class="max-w-[85%] space-y-2 rounded-lg border border-dashed bg-muted/50 px-3 py-2 text-sm"
  >
    <Badge variant="neutral">
      {{ t("qualityAgent.askDismissed") }}
    </Badge>
    <section
      v-for="question in questions"
      :key="question.id"
      class="space-y-0.5"
    >
      <p
        v-if="question.header"
        class="text-xs font-medium text-muted-foreground"
      >
        {{ question.header }}
      </p>
      <p class="whitespace-pre-wrap break-words">
        {{ question.question }}
      </p>
    </section>
    <p
      v-if="questions.length === 0 && text"
      class="whitespace-pre-wrap break-words"
    >
      {{ text }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";

interface DismissQuestion {
  id: string;
  header?: string;
  question: string;
}

interface DismissEnvelope {
  kind: string;
  ask: string;
  questions: DismissQuestion[];
  text?: string;
}

const props = defineProps<{ content: string }>();
const { t } = useI18n();

// A dismissal is a decision, not chat. It names the questions it closed; an older
// plain-text dismissal falls back to its text inside the same bubble.
const envelope = computed<DismissEnvelope | null>(() => {
  let parsed: unknown;
  try {
    parsed = JSON.parse(props.content);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) {
    return null;
  }
  const candidate = parsed as Partial<DismissEnvelope>;
  if (candidate.kind !== "dismiss_ask" || !Array.isArray(candidate.questions)) {
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
  return candidate as DismissEnvelope;
});

const questions = computed(() => envelope.value?.questions ?? []);
const text = computed(() => (envelope.value ? "" : props.content.trim()));
</script>
