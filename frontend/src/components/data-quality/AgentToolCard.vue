<template>
  <Collapsible class="rounded-md border">
    <CollapsibleTrigger
      class="flex w-full items-start gap-2 px-3 py-2 text-left hover:bg-accent/40"
    >
      <Wrench class="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-center gap-2">
          <span class="truncate font-mono text-xs font-medium">
            {{ toolName }}
          </span>
          <Badge
            v-if="result?.error"
            variant="danger"
          >
            {{ t("dataQuality.session.toolError") }}
          </Badge>
          <Badge
            v-if="result?.truncated"
            variant="warning"
            :title="t('dataQuality.session.toolTruncated')"
          >
            {{ t("dataQuality.session.toolTruncatedShort") }}
          </Badge>
        </div>
        <p
          v-if="argumentsPreview"
          class="mt-0.5 truncate font-mono text-[11px] text-muted-foreground"
        >
          {{ argumentsPreview }}
        </p>
      </div>
      <ChevronDown class="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
    </CollapsibleTrigger>
    <CollapsibleContent class="space-y-3 border-t px-3 py-3">
      <div v-if="call">
        <p class="text-xs font-medium text-muted-foreground">
          {{ t("dataQuality.session.toolArguments") }}
        </p>
        <pre
          v-if="argumentsText"
          class="mt-1 whitespace-pre-wrap break-all rounded-md border bg-muted/30 p-2 font-mono text-[11px]"
        >{{ argumentsText }}</pre>
        <p
          v-else
          class="mt-1 text-xs text-muted-foreground"
        >
          {{ t("dataQuality.session.toolNoArguments") }}
        </p>
      </div>

      <div>
        <p class="text-xs font-medium text-muted-foreground">
          {{ t("dataQuality.session.toolResult") }}
        </p>
        <p
          v-if="result?.error"
          class="mt-1 whitespace-pre-wrap break-words text-xs text-destructive"
        >
          {{ result.error }}
        </p>
        <pre
          v-if="result?.content"
          class="mt-1 whitespace-pre-wrap break-all rounded-md border bg-muted/30 p-2 font-mono text-[11px]"
        >{{ result.content }}</pre>
        <p
          v-else-if="result && !result.error"
          class="mt-1 text-xs text-muted-foreground"
        >
          {{ t("dataQuality.session.toolEmptyResult") }}
        </p>
        <p
          v-if="!result"
          class="mt-1 text-xs text-muted-foreground"
        >
          {{ t("dataQuality.session.toolNoResult") }}
        </p>
        <p
          v-if="result?.truncated"
          class="mt-1 text-xs text-warning"
        >
          {{ t("dataQuality.session.toolTruncated") }}
        </p>
      </div>
    </CollapsibleContent>
  </Collapsible>
</template>

<script setup lang="ts">
import { ChevronDown, Wrench } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import type {
  QualityAgentSessionMessage,
  QualityToolCall,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { toolCallArgumentsPreview } from "@/utils/agentSession";

/**
 * One tool call from the agent's conversation, collapsed to its name and
 * argument summary and expanded into the full argument and result payloads.
 *
 * The call row and the result row are separate transcript entries; pairing them
 * is the caller's job (a missing result is a wake that was interrupted, which
 * the card says instead of hiding the call). Payloads stay in a `<pre>` and are
 * never rendered as markdown or HTML.
 */

const props = defineProps<{
  call?: QualityToolCall;
  result?: QualityAgentSessionMessage;
}>();

const { t } = useI18n();

const toolName = computed(
  () =>
    props.call?.name ||
    props.result?.toolName ||
    t("dataQuality.session.toolFallback")
);

const argumentsText = computed(() => props.call?.arguments.trim() ?? "");

const argumentsPreview = computed(() =>
  props.call ? toolCallArgumentsPreview(props.call) : ""
);
</script>
