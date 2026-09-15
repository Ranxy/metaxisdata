<template>
  <PageHeader
    :title="title"
    :description="description"
  >
    <template #breadcrumb>
      <div class="text-xs font-semibold uppercase tracking-[0.24em] text-muted-foreground">
        {{ t("openlineage.title") }}
      </div>
    </template>
    <template
      v-if="$slots.actions"
      #actions
    >
      <slot name="actions" />
    </template>
  </PageHeader>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import PageHeader from "@/components/layout/PageHeader.vue";

// The Ingestion Settings shortcut that used to sit here on every OpenLineage
// page is gone: Settings already owns that destination in the sidebar, and the
// Overview surfaces it from its own empty state instead.
defineProps<{
  title: string;
  description: string;
}>();

const { t } = useI18n();
</script>
