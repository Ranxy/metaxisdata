<template>
  <div
    class="flex items-center gap-0.5 rounded-lg bg-muted p-0.5"
    role="group"
    :aria-label="t('dataQuality.agent.tabsLabel')"
  >
    <RouterLink
      v-for="tab in tabs"
      :key="tab.name"
      :to="{ name: tab.name }"
      class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:outline-none"
      :class="
        route.name === tab.name
          ? 'bg-card text-foreground shadow-e1'
          : 'text-muted-foreground hover:text-foreground'
      "
      :aria-current="route.name === tab.name ? 'page' : undefined"
    >
      {{ tab.label }}
    </RouterLink>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";

/**
 * The two views of the agent surface: the session list and the enrollment queue.
 * One sidebar entry opens both, so each page carries this switch; it mirrors the
 * segmented view switch the executions page uses.
 */

const { t } = useI18n();
const route = useRoute();

const tabs = computed(() => [
  { name: "DataQualityAgent", label: t("dataQuality.agent.tabSessions") },
  {
    name: "DataQualityAgentEnrollment",
    label: t("dataQuality.agent.tabEnrollment"),
  },
]);
</script>
