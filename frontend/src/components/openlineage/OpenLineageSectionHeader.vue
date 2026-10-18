<template>
  <PageHeader
    eyebrow="OpenLineage"
    :title="title"
    :description="description"
  >
    <template
      v-if="$slots['title-extra']"
      #title-extra
    >
      <slot name="title-extra" />
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
import PageHeader from "@/components/layout/PageHeader.vue";

// The product-area overline every OpenLineage page shares. It used to be a raw
// breadcrumb slot with its own letter-spacing; `eyebrow` is the page header's
// one overline style, so the section no longer hand-rolls it.
defineProps<{
  title: string;
  description: string;
}>();
</script>
