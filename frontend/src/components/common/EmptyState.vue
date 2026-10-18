<template>
  <div
    :class="[
      'flex flex-col items-center justify-center gap-3 px-6 text-center',
      compact ? 'py-8' : 'py-12',
    ]"
  >
    <span
      v-if="icon"
      class="grid h-10 w-10 place-items-center rounded-full bg-muted text-muted-foreground"
    >
      <component
        :is="icon"
        class="h-5 w-5"
      />
    </span>
    <div class="max-w-sm space-y-1">
      <p class="text-sm font-semibold text-foreground">
        {{ title }}
      </p>
      <p
        v-if="description"
        class="text-sm text-muted-foreground"
      >
        {{ description }}
      </p>
    </div>
    <div
      v-if="$slots.default"
      class="mt-1 flex flex-wrap items-center justify-center gap-2"
    >
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Component } from "vue";

// The one empty-state shape: a quiet icon tile, a title, an explanation and an
// optional next step. An empty region without a next step is a dead end.
withDefaults(
  defineProps<{
    icon?: Component;
    title: string;
    description?: string;
    /** Tighter padding for an empty state inside a table cell or narrow panel. */
    compact?: boolean;
  }>(),
  { icon: undefined, description: "", compact: false }
);
</script>
