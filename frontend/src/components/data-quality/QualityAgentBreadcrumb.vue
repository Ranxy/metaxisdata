<template>
  <nav
    class="flex flex-wrap items-center gap-1 text-sm text-muted-foreground"
    :aria-label="t('qualityAgent.breadcrumbLabel')"
  >
    <RouterLink
      :to="{ name: 'QualityAgents' }"
      class="transition-colors hover:text-foreground"
    >
      {{ t("qualityAgent.breadcrumbAgents") }}
    </RouterLink>
    <ChevronRight class="h-3.5 w-3.5 shrink-0" />
    <RouterLink
      v-if="variant !== 'home'"
      :to="agentRoute"
      class="transition-colors hover:text-foreground"
    >
      {{ agentLabel }}
    </RouterLink>
    <span
      v-else
      class="text-foreground"
    >
      {{ agentLabel }}
    </span>
    <template v-if="variant !== 'home'">
      <ChevronRight class="h-3.5 w-3.5 shrink-0" />
      <RouterLink
        v-if="variant === 'session'"
        :to="sessionsRoute"
        class="transition-colors hover:text-foreground"
      >
        {{ t("qualityAgent.tabSessions") }}
      </RouterLink>
      <span
        v-else
        class="text-foreground"
      >
        {{ t("qualityAgent.tabSessions") }}
      </span>
    </template>
    <template v-if="variant === 'session' && sessionLabel">
      <ChevronRight class="h-3.5 w-3.5 shrink-0" />
      <span class="truncate text-foreground">{{ sessionLabel }}</span>
    </template>
  </nav>
</template>

<script setup lang="ts">
import { ChevronRight } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import {
  qualityAgentRoute,
  qualityAgentSessionsRoute,
} from "@/utils/qualityAgent";

/**
 * The agent-level location trail. The agent home, the work-session list and a
 * session detail all sit under the same agent, so the breadcrumb names that
 * agent instead of leaving the reader to infer it from the page body.
 */
const props = defineProps<{
  agentName: string;
  agentLabel: string;
  variant: "home" | "sessions" | "session";
  sessionLabel?: string;
}>();

const { t } = useI18n();
const agentRoute = computed(() => qualityAgentRoute(props.agentName));
const sessionsRoute = computed(() =>
  qualityAgentSessionsRoute(props.agentName)
);
</script>
