<template>
  <div class="space-y-6">
    <section
      v-for="group in groups"
      :key="group.wake"
      class="space-y-3"
    >
      <div class="flex flex-wrap items-center gap-2 border-b pb-2">
        <Badge variant="outline">
          {{
            group.wake === 0
              ? t("dataQuality.session.wakeSystem")
              : t("dataQuality.session.wake", { wake: group.wake })
          }}
        </Badge>
        <span
          v-if="group.startTime"
          class="text-xs text-muted-foreground"
          :title="formatDateTime(group.startTime, locale)"
        >
          {{ formatRelativeTime(group.startTime, locale) }}
        </span>
        <Badge
          v-if="group.origin !== QualityAgentMessageOrigin.UNSPECIFIED"
          variant="secondary"
        >
          {{ t(agentMessageOriginKey(group.origin)) }}
        </Badge>
      </div>

      <p
        v-if="group.summary"
        class="rounded-md bg-muted/40 px-3 py-1.5 text-xs text-muted-foreground"
      >
        {{ group.summary }}
      </p>

      <template
        v-for="message in group.messages"
        :key="message.name"
      >
        <!-- The system prompt is fixed boilerplate; it stays folded. -->
        <Collapsible
          v-if="message.role === QualityAgentMessageRole.SYSTEM"
          class="rounded-md border"
        >
          <CollapsibleTrigger
            class="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-xs text-muted-foreground hover:bg-accent/40"
          >
            <span class="font-medium">
              {{ t(agentMessageRoleKey(message.role)) }}
            </span>
            <ChevronDown class="h-4 w-4" />
          </CollapsibleTrigger>
          <CollapsibleContent class="border-t px-3 py-2">
            <pre
              class="whitespace-pre-wrap break-words font-mono text-[11px]"
            >{{ message.content }}</pre>
          </CollapsibleContent>
        </Collapsible>

        <!-- A person's question, an event the runner injected, or the wake note. -->
        <div
          v-else-if="message.role === QualityAgentMessageRole.USER"
          :class="userMessageClass(message)"
        >
          <div class="flex flex-wrap items-center gap-2">
            <Badge
              v-if="message.origin !== QualityAgentMessageOrigin.UNSPECIFIED"
              :variant="message.origin === QualityAgentMessageOrigin.EVENT ? 'warning' : 'secondary'"
            >
              {{ t(agentMessageOriginKey(message.origin)) }}
            </Badge>
            <span
              v-if="message.createTime"
              class="text-[11px] text-muted-foreground"
              :title="formatDateTime(message.createTime, locale)"
            >
              {{ formatRelativeTime(message.createTime, locale) }}
            </span>
          </div>
          <p class="mt-1.5 whitespace-pre-wrap break-words text-sm">
            {{ message.content }}
          </p>
        </div>

        <div
          v-else-if="message.role === QualityAgentMessageRole.ASSISTANT"
          class="space-y-2"
        >
          <AsyncMarkdown
            v-if="message.content"
            mode="chat"
            :content="message.content"
            final
          />
          <AgentToolCard
            v-for="call in message.toolCalls"
            :key="call.id"
            :call="call"
            :result="toolResultForCall(group.messages, call.id)"
          />
        </div>

        <!-- A result whose call row is missing still shows; the card says so. -->
        <AgentToolCard
          v-else-if="
            message.role === QualityAgentMessageRole.TOOL &&
            isOrphanResult(group, message)
          "
          :result="message"
        />

        <!-- Runner bookkeeping is for troubleshooting, so it stays folded. -->
        <Collapsible
          v-else-if="message.role === QualityAgentMessageRole.ENGINE"
          class="rounded-md border border-dashed"
        >
          <CollapsibleTrigger
            class="flex w-full items-start justify-between gap-2 px-3 py-2 text-left text-xs text-muted-foreground hover:bg-accent/40"
          >
            <div class="min-w-0">
              <span class="font-medium">
                {{ t(agentMessageRoleKey(message.role)) }}
                <template v-if="message.turn > 0">
                  · {{ t("dataQuality.session.turn", { turn: message.turn }) }}
                </template>
              </span>
              <span class="block truncate">{{ truncateText(message.content, 120) }}</span>
            </div>
            <ChevronDown class="mt-0.5 h-4 w-4 shrink-0" />
          </CollapsibleTrigger>
          <CollapsibleContent class="border-t px-3 py-2">
            <pre
              class="whitespace-pre-wrap break-words font-mono text-[11px] text-muted-foreground"
            >{{ message.content }}</pre>
          </CollapsibleContent>
        </Collapsible>

        <!-- A compaction checkpoint replaces the covered messages in the model
             context; the UI keeps the original rows visible above it. -->
        <Collapsible
          v-else-if="message.role === QualityAgentMessageRole.COMPACTION"
          class="rounded-md border"
        >
          <CollapsibleTrigger
            class="flex w-full items-start justify-between gap-2 bg-muted/30 px-3 py-2 text-left text-xs hover:bg-accent/40"
          >
            <div class="min-w-0">
              <span class="font-medium">
                {{ t(agentMessageRoleKey(message.role)) }}
              </span>
              <span class="block text-muted-foreground">
                {{ t("dataQuality.session.historyCompactedHint") }}
              </span>
            </div>
            <ChevronDown class="mt-0.5 h-4 w-4 shrink-0" />
          </CollapsibleTrigger>
          <CollapsibleContent class="border-t px-3 py-2">
            <pre
              class="whitespace-pre-wrap break-words text-xs"
            >{{ message.content }}</pre>
          </CollapsibleContent>
        </Collapsible>
      </template>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ChevronDown } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { AsyncMarkdown } from "@/components/common/AsyncMarkdown";
import AgentToolCard from "@/components/data-quality/AgentToolCard.vue";
import { Badge } from "@/components/ui/badge";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  QualityAgentMessageOrigin,
  QualityAgentMessageRole,
  type QualityAgentSessionMessage,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  type AgentWakeGroup,
  agentMessageOriginKey,
  agentMessageRoleKey,
  orphanToolResults,
  toolResultForCall,
  truncateText,
} from "@/utils/agentSession";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * A session's transcript, grouped into wakes.
 *
 * The conversation is the product: it shows what woke the agent, what it said,
 * which tools it called with which arguments and results, and how the runner
 * closed each wake. ENGINE rows are the runner's own notes and COMPACTION rows
 * describe a context rewrite, so both stay folded; the system prompt is folded
 * for the same reason. Every payload is rendered as text in a `<pre>` — never as
 * HTML — and pairing a tool call with its result tolerates a missing result.
 */

const props = defineProps<{
  groups: AgentWakeGroup[];
}>();

const { t, locale } = useI18n();

/**
 * The result rows that have no call row to pair with, indexed by wake. A result
 * without its call is shown on its own; a paired one is rendered under the
 * assistant message that called it, so it must not appear twice.
 */
const orphanSeqsByWake = computed(
  (): Map<number, Set<string>> =>
    new Map(
      props.groups.map((group) => [
        group.wake,
        new Set(
          orphanToolResults(group.messages).map((message) =>
            message.seq.toString()
          )
        ),
      ])
    )
);

function isOrphanResult(
  group: AgentWakeGroup,
  message: QualityAgentSessionMessage
): boolean {
  return (
    orphanSeqsByWake.value.get(group.wake)?.has(message.seq.toString()) ?? false
  );
}

/** A user row's surface: an event is a warning, a question is an accent. */
function userMessageClass(message: QualityAgentSessionMessage): string {
  const base = "rounded-md border px-3 py-2";
  switch (message.origin) {
    case QualityAgentMessageOrigin.EVENT:
      return `${base} border-warning/40 bg-warning-subtle/30`;
    case QualityAgentMessageOrigin.ASK:
      return `${base} border-primary/30 bg-primary-subtle/30`;
    default:
      return `${base} border-transparent bg-muted/40`;
  }
}
</script>
