<template>
  <header class="flex flex-col gap-3">
    <slot name="breadcrumb" />

    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0 flex-1 space-y-1.5">
        <p
          v-if="eyebrow"
          class="text-[11px] font-semibold uppercase tracking-[0.08em] text-primary"
        >
          {{ eyebrow }}
        </p>
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
          <h1 class="truncate text-2xl font-semibold tracking-tight">
            {{ title }}
          </h1>
          <slot name="title-extra" />
        </div>

        <p
          v-if="description"
          class="max-w-2xl text-sm text-muted-foreground"
        >
          {{ description }}
        </p>

        <slot name="meta" />
      </div>

      <div
        v-if="$slots.actions"
        class="flex w-full flex-wrap items-center gap-2 sm:w-auto sm:shrink-0"
      >
        <slot name="actions" />
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
// Every page renders its title through this component so the heading level,
// type scale and action placement stay identical across the app. The page title
// must outrank every card title on the page, hence the 24px step.
defineProps<{
  title: string;
  description?: string;
  /** A short overline naming the product area, e.g. "OpenLineage". */
  eyebrow?: string;
}>();
</script>
