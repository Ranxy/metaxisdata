<template>
  <div class="space-y-2">
    <p
      v-if="parameterHint"
      class="text-sm"
    >
      {{ t("dataQuality.editor.parameterMismatchHint") }}
    </p>
    <p
      v-if="missingLabels.length > 0"
      class="text-sm"
    >
      {{ t("dataQuality.editor.parameterCandidateFields", { fields: missingLabels.join("、") }) }}
    </p>

    <details
      v-if="parsed.sql || parsed.parameters"
      class="rounded-md border bg-muted/30 px-3 py-2"
    >
      <summary class="cursor-pointer text-sm font-medium select-none">
        {{ t("dataQuality.editor.compiledQuery") }}
      </summary>
      <div
        v-if="parsed.sql"
        class="mt-2 space-y-1"
      >
        <div class="flex items-center justify-between gap-2">
          <span class="text-xs font-medium text-muted-foreground">
            {{ t("dataQuality.editor.compiledSql") }}
          </span>
          <Button
            variant="outline"
            size="sm"
            @click="copy(parsed.sql ?? '')"
          >
            {{ t("dataQuality.copySql") }}
          </Button>
        </div>
        <pre class="rounded-md border bg-background p-2 font-mono text-xs whitespace-pre-wrap break-all">{{ parsed.sql }}</pre>
      </div>
      <div
        v-if="parsed.parameters"
        class="mt-2 space-y-1"
      >
        <div class="flex items-center justify-between gap-2">
          <span class="text-xs font-medium text-muted-foreground">
            {{ t("dataQuality.editor.parameters") }}
          </span>
          <Button
            variant="outline"
            size="sm"
            @click="copy(parsed.parameters ?? '')"
          >
            {{ t("common.copy") }}
          </Button>
        </div>
        <pre class="rounded-md border bg-background p-2 font-mono text-xs whitespace-pre-wrap break-all">{{ parsed.parameters }}</pre>
      </div>
      <p class="mt-2 text-xs text-muted-foreground">
        {{ t("dataQuality.editor.checkParametersHint") }}
      </p>
    </details>

    <details v-if="raw">
      <summary class="cursor-pointer text-xs text-muted-foreground select-none">
        {{ t("dataQuality.editor.rawError") }}
      </summary>
      <pre class="mt-1 max-h-40 overflow-auto rounded-md border bg-muted/30 p-2 text-xs whitespace-pre-wrap break-words">{{ raw }}</pre>
    </details>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import { notify } from "@/lib/notify";
import type { ParsedRuleError } from "@/utils/dataQualitySchema";

/**
 * The readable part of a failed rule query: the sentence, the compiled SQL and
 * its parameters in copyable blocks, and the raw server message folded away.
 * The server appends the query to the error; rendering it as one line was what
 * made "expected 2 arguments, got 0" unactionable.
 */

withDefaults(
  defineProps<{
    parsed: ParsedRuleError;
    raw?: string;
    parameterHint?: boolean;
    missingLabels?: string[];
  }>(),
  { raw: "", parameterHint: false, missingLabels: () => [] }
);

const { t } = useI18n();

async function copy(text: string) {
  if (!text || !navigator.clipboard) {
    return;
  }
  await navigator.clipboard.writeText(text);
  notify.success(t("common.copied"));
}
</script>
