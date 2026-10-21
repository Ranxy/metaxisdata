<template>
  <Dialog
    :open="open"
    @update:open="(value: boolean) => emit('update:open', value)"
  >
    <DialogContent
      class="left-auto right-0 top-0 h-dvh max-h-dvh w-full max-w-2xl translate-x-0 translate-y-0 gap-0 overflow-hidden rounded-none border-l p-0 sm:max-w-2xl sm:rounded-none"
    >
      <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle class="truncate">
            {{ t("dataQuality.llm.explain.title") }}
          </DialogTitle>
          <DialogDescription class="truncate">
            {{ executionTitle || t("dataQuality.llm.explain.description") }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-4 overflow-auto px-6 py-5">
          <div
            v-if="error"
            class="space-y-2 rounded-md border border-destructive/30 bg-destructive/5 p-3"
          >
            <p class="text-sm font-medium text-destructive">
              {{ t(explainErrorMessageKey(error)) }}
            </p>
            <p
              v-if="error.message"
              class="text-xs break-words text-muted-foreground"
            >
              {{ error.message }}
            </p>
          </div>

          <details
            v-if="state.progress.length > 0"
            :open="streaming"
            class="text-sm"
          >
            <summary class="cursor-pointer py-1 text-muted-foreground select-none">
              <Loader2
                v-if="streaming"
                class="mr-1.5 inline h-3 w-3 animate-spin"
              />
              {{ streaming ? t("dataQuality.llm.thinking") : t("dataQuality.llm.thinkingProcess") }}
            </summary>
            <div class="mt-2 ml-2 space-y-1 border-l-2 border-muted/60 pl-3">
              <div
                v-for="(step, index) in state.progress"
                :key="index"
                class="flex items-start gap-1.5 text-xs"
              >
                <template v-if="step.type === 'tool_start'">
                  <span class="font-mono text-primary">{{ step.toolName }}</span>
                  <span
                    v-if="step.toolInput"
                    class="truncate text-muted-foreground"
                  >
                    ({{ step.toolInput }})
                  </span>
                </template>
                <template v-else>
                  <span
                    v-if="step.toolError"
                    class="text-danger"
                  >
                    ✗ {{ step.toolName }}: {{ step.toolError }}
                  </span>
                  <template v-else>
                    <span class="text-success">✓ {{ step.toolName }}</span>
                    <span
                      v-if="step.toolOutput"
                      class="truncate text-muted-foreground"
                    >
                      — {{ step.toolOutput }}
                    </span>
                  </template>
                </template>
              </div>
            </div>
          </details>

          <div
            v-else-if="streaming && !state.content"
            class="flex items-center gap-2 py-1 text-sm text-muted-foreground"
          >
            <Loader2 class="h-3 w-3 animate-spin" />
            {{ t("dataQuality.llm.thinking") }}
          </div>

          <MarkdownRender
            v-if="state.content"
            mode="chat"
            :content="state.content"
            :final="!streaming"
            :max-live-nodes="0"
            :fade="false"
            :typewriter="false"
          />
        </div>

        <DialogFooter class="shrink-0 items-center justify-between border-t px-6 py-4 sm:justify-between">
          <div class="flex min-w-0 items-center gap-3 text-xs text-muted-foreground">
            <template v-if="state.metadata">
              <span class="truncate">{{ state.metadata.provider }} / {{ state.metadata.model }}</span>
              <Badge
                v-if="state.metadata.fromCache"
                variant="outline"
              >
                {{ t("dataQuality.llm.cached") }}
              </Badge>
            </template>
          </div>
          <div class="flex gap-2">
            <Button
              variant="outline"
              :disabled="streaming || !execution"
              @click="start(true)"
            >
              <RefreshCcw class="mr-2 h-4 w-4" />
              {{ t("dataQuality.llm.regenerate") }}
            </Button>
            <Button
              variant="outline"
              @click="emit('update:open', false)"
            >
              {{ t("common.close") }}
            </Button>
          </div>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { Loader2, RefreshCcw } from "lucide-vue-next";
import MarkdownRender from "markstream-vue";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { explainQualityExecution } from "@/api/data-quality";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { QualityExecution } from "@/types/proto-es/v1/data_quality_service_pb";
import {
  applyExplanationChunk,
  emptyExplanationState,
  explainErrorMessageKey,
  explanationComplete,
} from "@/utils/dataQualityLlm";

/**
 * The streaming explanation of one failed execution. It lives in a side sheet so
 * the execution's evidence stays readable, aggregates content frames into one
 * markdown document, and can force a fresh answer instead of the cached one.
 */

const props = defineProps<{
  open: boolean;
  execution?: QualityExecution;
  executionTitle?: string;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
}>();

const { t, locale } = useI18n();

const state = reactive(emptyExplanationState());
const error = ref<Error>();
const streaming = computed(
  () => !explanationComplete(state) && error.value === undefined
);

// `immediate` matters: the drawer is usually mounted and opened in one step.
watch(
  () => [props.open, props.execution?.name] as const,
  ([isOpen]) => {
    if (isOpen) {
      void start(false);
    }
  },
  { immediate: true }
);

function reset() {
  Object.assign(state, emptyExplanationState());
  error.value = undefined;
}

async function start(forceRegenerate: boolean) {
  const execution = props.execution;
  reset();
  if (!execution) {
    return;
  }
  try {
    const stream = explainQualityExecution({
      name: execution.name,
      language: locale.value,
      forceRegenerate,
    });
    for await (const chunk of stream) {
      applyExplanationChunk(state, chunk);
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e : new Error(String(e));
  }
}
</script>
