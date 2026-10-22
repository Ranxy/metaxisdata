<template>
  <Dialog
    :open="open"
    @update:open="(value: boolean) => emit('update:open', value)"
  >
    <DialogContent class="max-h-[85vh] gap-0 overflow-hidden p-0 sm:max-w-4xl">
      <div class="flex max-h-[85vh] min-h-0 flex-col">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>
            {{ t("dataQuality.suggestions.diagnostic.payload.title") }}
          </DialogTitle>
          <DialogDescription class="space-y-1">
            <span>{{ t("dataQuality.suggestions.diagnostic.payload.description") }}</span>
            <span
              v-if="correlationId"
              class="block font-mono"
            >
              {{ t("dataQuality.suggestions.diagnostic.facts.correlationId") }}: {{ correlationId }}
            </span>
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-4 overflow-auto px-6 py-5">
          <div
            v-if="loading"
            class="flex items-center gap-2 text-sm text-muted-foreground"
          >
            <Loader2 class="h-4 w-4 animate-spin" />
            {{ t("common.loading") }}
          </div>

          <Alert
            v-else-if="error"
            variant="destructive"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>{{ t("dataQuality.suggestions.diagnostic.payload.fetchFailed") }}</AlertTitle>
            <AlertDescription class="space-y-2">
              <p class="break-words">{{ error }}</p>
              <Button
                variant="outline"
                size="sm"
                @click="load"
              >
                {{ t("common.refresh") }}
              </Button>
            </AlertDescription>
          </Alert>

          <Alert
            v-else-if="response && !response.available"
            variant="warning"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>{{ t("dataQuality.suggestions.diagnostic.payload.unavailableTitle") }}</AlertTitle>
            <AlertDescription>
              {{ t("dataQuality.suggestions.diagnostic.payload.unavailableHint") }}
            </AlertDescription>
          </Alert>

          <template v-else-if="response">
            <section class="space-y-2">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span class="text-sm font-medium">
                  {{ t("dataQuality.suggestions.diagnostic.payload.request") }}
                </span>
                <div class="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    :disabled="!response.requestBody"
                    @click="copy(response.requestBody)"
                  >
                    {{ t("common.copy") }}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    :disabled="!response.requestBody"
                    @click="download(response.requestBody, 'request')"
                  >
                    {{ t("dataQuality.suggestions.diagnostic.payload.download") }}
                  </Button>
                </div>
              </div>
              <pre class="max-h-64 overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap break-all">{{ response.requestBody || "-" }}</pre>
            </section>

            <section class="space-y-2">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span class="text-sm font-medium">
                  {{ t("dataQuality.suggestions.diagnostic.payload.response") }}
                </span>
                <div class="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    :disabled="!response.responseBody"
                    @click="copy(response.responseBody)"
                  >
                    {{ t("common.copy") }}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    :disabled="!response.responseBody"
                    @click="download(response.responseBody, 'response')"
                  >
                    {{ t("dataQuality.suggestions.diagnostic.payload.download") }}
                  </Button>
                </div>
              </div>
              <pre class="max-h-64 overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap break-all">{{ response.responseBody || "-" }}</pre>
            </section>
          </template>
        </div>

        <DialogFooter class="shrink-0 border-t px-6 py-4">
          <Button
            variant="outline"
            @click="emit('update:open', false)"
          >
            {{ t("common.close") }}
          </Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { AlertTriangle, Loader2 } from "lucide-vue-next";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { getQualitySuggestionJobTargetDebug } from "@/api/data-quality";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import type { GetQualitySuggestionJobTargetDebugResponse } from "@/types/proto-es/v1/data_quality_service_pb";

/**
 * The raw request/response bodies behind one failed drafting target.
 *
 * They are captured only while runtime LLM debug is enabled and kept for seven
 * days, so the dialog has three states: loading, captured, and "not available
 * (debug off or pruned)". The bodies are read-only and copyable.
 */

const props = defineProps<{
  open: boolean;
  /** The job resource name; empty while the dialog has not been opened yet. */
  jobName: string;
  /** The zero-based target index inside the job. */
  targetIndex: number;
  correlationId: string;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
}>();

const { t } = useI18n();
const { formatError } = useErrorMessage();

const loading = ref(false);
const error = ref("");
const response = ref<GetQualitySuggestionJobTargetDebugResponse>();

watch(
  () => props.open,
  (open) => {
    if (open) {
      void load();
    }
  },
  { immediate: true }
);

async function load() {
  if (loading.value) {
    return;
  }
  loading.value = true;
  error.value = "";
  response.value = undefined;
  try {
    response.value = await getQualitySuggestionJobTargetDebug(
      props.jobName,
      props.targetIndex
    );
  } catch (e: unknown) {
    error.value = formatError(
      e,
      "dataQuality.suggestions.diagnostic.payload.fetchFailed"
    );
  } finally {
    loading.value = false;
  }
}

async function copy(text: string) {
  if (!text || !navigator.clipboard) {
    return;
  }
  await navigator.clipboard.writeText(text);
  notify.success(t("common.copied"));
}

/** Save one body as a text file, named so the correlation id stays traceable. */
function download(content: string, kind: "request" | "response") {
  if (!content) {
    return;
  }
  const blob = new Blob([content], { type: "text/plain;charset=utf-8;" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `llm-${props.correlationId || "call"}-${kind}.txt`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}
</script>
