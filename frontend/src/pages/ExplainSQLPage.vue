<template>
  <div class="flex flex-col gap-3 h-full min-h-0">
    <div class="flex items-center gap-2 shrink-0">
      <h1 class="text-2xl font-bold tracking-tight">{{ t("explainSQL.title") }}</h1>
    </div>

    <div class="grid grid-cols-[360px_minmax(0,1fr)] min-h-0 flex-1 border border-border rounded-lg overflow-hidden">
      <!-- Left panel -->
      <div class="border-r border-border p-4 overflow-y-auto flex flex-col gap-3 bg-muted/30">
        <div class="flex gap-1 border-b border-border pb-2">
          <button
            :class="[
              'flex-1 py-1.5 text-sm rounded-md transition-colors',
              sourceMode === 'metadata'
                ? 'bg-accent text-accent-foreground'
                : 'hover:bg-accent/50',
            ]"
            @click="sourceMode = 'metadata'"
          >
            {{ t("explainSQL.fromMetadata") }}
          </button>
          <button
            :class="[
              'flex-1 py-1.5 text-sm rounded-md transition-colors',
              sourceMode === 'custom'
                ? 'bg-accent text-accent-foreground'
                : 'hover:bg-accent/50',
            ]"
            @click="sourceMode = 'custom'"
          >
            {{ t("explainSQL.customSQL") }}
          </button>
        </div>

        <template v-if="sourceMode === 'metadata'">
          <div class="space-y-2">
            <Label>{{ t("explainSQL.metaGuid") }}</Label>
            <AppInput
              v-model="metaGuid"
              :placeholder="t('explainSQL.metaGuidPlaceholder')"
            />
          </div>
          <p class="text-xs text-muted-foreground">
            {{ t("explainSQL.metaGuidHint") }}
          </p>
        </template>

        <template v-else>
          <textarea
            v-model="customSQL"
            :placeholder="t('explainSQL.sqlPlaceholder')"
            class="flex-1 min-h-[200px] bg-input-surface border border-border rounded-md p-3 text-sm font-mono outline-none focus:border-accent resize-none"
          />
        </template>

        <Button
          :disabled="!canExplain || isExplaining"
          class="mt-auto"
          @click="startExplain()"
        >
          <Sparkles class="h-4 w-4 mr-2" />
          {{ isExplaining ? t("explainSQL.explaining") : t("explainSQL.explain") }}
        </Button>
      </div>

      <!-- Right panel -->
      <div class="p-5 overflow-y-auto flex flex-col gap-4">
        <div
          v-if="!isExplaining && !resultText && !explainError"
          class="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-2"
        >
          <Sparkles class="h-12 w-12 opacity-30" />
          <p class="text-sm">{{ t("explainSQL.idleHint") }}</p>
        </div>

        <template v-else>
          <div class="flex-1">
            <div
              v-if="isExplaining || resultText"
              class="prose prose-sm dark:prose-invert max-w-none whitespace-pre-wrap"
            >
              {{ resultText }}
              <span v-if="isExplaining" class="inline-block w-2 h-4 bg-accent animate-pulse align-middle ml-0.5" />
            </div>
            <div v-if="explainError" class="text-destructive text-sm p-4 border border-destructive/30 rounded-md">
              {{ explainError }}
            </div>
          </div>

          <div v-if="explainMeta" class="flex items-center gap-3 text-xs text-muted-foreground pt-3 border-t border-border">
            <span>{{ explainMeta.provider }} / {{ explainMeta.model }}</span>
            <Badge v-if="explainMeta.fromCache" variant="outline">{{ t("explainSQL.cached") }}</Badge>
            <Badge v-if="explainMeta.expired" variant="secondary" class="text-destructive">{{ t("explainSQL.expired") }}</Badge>
            <Button
              v-if="explainMeta.expired"
              variant="outline"
              size="sm"
              @click="startExplain(true)"
            >
              {{ t("explainSQL.regenerate") }}
            </Button>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Sparkles } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

import { explainSQL } from "@/api/explain";

import AppInput from "@/components/common/AppInput.vue";
import Badge from "@/components/ui/badge/Badge.vue";
import Button from "@/components/ui/button/Button.vue";
import Label from "@/components/ui/label/Label.vue";

const { t } = useI18n();
const route = useRoute();

// ---- state ----
const sourceMode = ref<"metadata" | "custom">("metadata");
const metaGuid = ref((route.params.guid as string) ?? "");
const customSQL = ref("");
const isExplaining = ref(false);
const resultText = ref("");
const explainError = ref<string | null>(null);
const explainMeta = ref<{
  provider: string;
  model: string;
  fromCache: boolean;
  expired: boolean;
} | null>(null);

// ---- computed ----
const canExplain = computed(() => {
  if (sourceMode.value === "metadata") return metaGuid.value.trim() !== "";
  return customSQL.value.trim() !== "";
});

// ---- methods ----
async function startExplain(forceRegen = false) {
  if (isExplaining.value) return;
  isExplaining.value = true;
  resultText.value = "";
  explainError.value = null;
  explainMeta.value = null;

  const input = sourceMode.value === "metadata"
    ? { metaGuid: metaGuid.value.trim(), forceRegenerate: forceRegen }
    : { sqlText: customSQL.value.trim(), forceRegenerate: forceRegen };

  try {
    const stream = explainSQL(input);
    for await (const chunk of stream) {
      if (chunk.payload?.case === "content" && chunk.payload.value) {
        resultText.value += chunk.payload.value;
      } else if (chunk.payload?.case === "metadata" && chunk.payload.value) {
        const m = chunk.payload.value;
        explainMeta.value = {
          provider: m.provider,
          model: m.model,
          fromCache: m.fromCache,
          expired: m.expired,
        };
      } else if (chunk.payload?.case === "error" && chunk.payload.value) {
        explainError.value = chunk.payload.value;
      }
    }
  } catch (e) {
    explainError.value = String(e);
  } finally {
    isExplaining.value = false;
  }
}
</script>
