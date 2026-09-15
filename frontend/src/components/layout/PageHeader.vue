<template>
  <header class="flex flex-col gap-2">
    <slot name="breadcrumb" />

    <div class="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
      <div class="min-w-0 flex-1 space-y-1">
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
          <h1 class="truncate text-xl font-semibold tracking-tight">
            {{ title }}
          </h1>
          <slot name="title-extra" />
        </div>

        <p
          v-if="description"
          class="text-sm text-muted-foreground"
        >
          {{ description }}
        </p>

        <slot name="meta" />
      </div>

      <div
        v-if="$slots.actions"
        class="flex shrink-0 flex-wrap items-center gap-2"
      >
        <slot name="actions" />
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
// Every page renders its title through this component so the heading level,
// type scale and action placement stay identical across the app.
defineProps<{
  title: string;
  description?: string;
}>();
</script>
