<template>
  <div class="space-y-4">
    <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
      <div class="space-y-2">
        <div class="text-xs font-semibold uppercase tracking-[0.24em] text-muted-foreground">
          {{ t("openlineage.title") }}
        </div>
        <div>
          <h1 class="text-2xl font-bold tracking-tight">{{ title }}</h1>
          <p class="max-w-3xl text-sm text-muted-foreground">
            {{ description }}
          </p>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <slot name="actions" />
        <Button variant="outline" asChild>
          <RouterLink :to="{ name: 'OpenLineageSettings' }">
            {{ t("openlineage.ingestionSettings") }}
          </RouterLink>
        </Button>
      </div>
    </div>

    <div class="flex flex-wrap gap-2 rounded-lg border bg-muted/20 p-2">
      <RouterLink
        v-for="item in navItems"
        :key="item.path"
        :to="item.path"
        :class="[
          'rounded-md px-3 py-2 text-sm font-medium transition-colors',
          isActive(item.matchPrefixes)
            ? 'bg-background text-foreground shadow-sm'
            : 'text-muted-foreground hover:bg-background/70 hover:text-foreground',
        ]"
      >
        {{ item.label }}
      </RouterLink>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import { Button } from "@/components/ui/button";

defineProps<{
  title: string;
  description: string;
}>();

const { t } = useI18n();
const route = useRoute();

const navItems = computed(() => [
  {
    label: t("openlineage.overview"),
    path: "/openlineage/overview",
    matchPrefixes: ["/openlineage", "/openlineage/overview"],
  },
  {
    label: t("openlineage.jobs"),
    path: "/openlineage/jobs",
    matchPrefixes: ["/openlineage/jobs", "/openlineage/tasks"],
  },
  {
    label: t("openlineage.datasets"),
    path: "/openlineage/datasets",
    matchPrefixes: ["/openlineage/datasets"],
  },
  {
    label: t("openlineage.events"),
    path: "/openlineage/events",
    matchPrefixes: ["/openlineage/events", "/openlineage/runs"],
  },
]);

function isActive(prefixes: string[]): boolean {
  return prefixes.some((prefix) => {
    if (prefix === "/openlineage") {
      return route.path === prefix || route.path === "/openlineage/overview";
    }

    return route.path === prefix || route.path.startsWith(`${prefix}/`);
  });
}
</script>