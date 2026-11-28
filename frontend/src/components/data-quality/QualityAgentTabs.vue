<template>
  <div
    class="inline-flex items-center gap-0.5 rounded-lg bg-muted p-0.5"
    role="group"
    :aria-label="t('qualityAgent.tabsLabel')"
  >
    <RouterLink
      v-for="tab in tabs"
      :key="tab.value"
      :to="tab.to"
      class="rounded-md px-3 py-1 text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:outline-none"
      :class="
        active === tab.value
          ? 'bg-card text-foreground shadow-e1'
          : 'text-muted-foreground hover:text-foreground'
      "
      :aria-current="active === tab.value ? 'page' : undefined"
    >
      {{ tab.label }}
    </RouterLink>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import {
  qualityAgentRoute,
  qualityAgentSessionsRoute,
} from "@/utils/qualityAgent";

/** The two product views of one agent: the owner conversation and the fixed
 * subagents. Both pages and the transcript detail carry the switch, so moving
 * between them never depends on a page-level back button. */
const props = defineProps<{
  agentName: string;
  active: "conversation" | "sessions";
}>();

const { t } = useI18n();
const tabs = computed(() => [
  {
    value: "conversation",
    label: t("qualityAgent.tabConversation"),
    to: qualityAgentRoute(props.agentName),
  },
  {
    value: "sessions",
    label: t("qualityAgent.tabSessions"),
    to: qualityAgentSessionsRoute(props.agentName),
  },
]);
</script>
