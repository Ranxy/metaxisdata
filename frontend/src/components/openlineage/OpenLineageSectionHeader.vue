<template>
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
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { Button } from "@/components/ui/button";

defineProps<{
  title: string;
  description: string;
}>();

const { t } = useI18n();
</script>