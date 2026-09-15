<template>
  <div class="flex flex-col items-center justify-center gap-3 px-6 py-10 text-center">
    <component
      :is="icon"
      v-if="icon"
      class="h-7 w-7 text-muted-foreground/50"
    />
    <div class="space-y-1">
      <p class="text-sm font-medium">
        {{ title }}
      </p>
      <p
        v-if="description"
        class="text-sm text-muted-foreground"
      >
        {{ description }}
      </p>
    </div>
    <slot />
  </div>
</template>

<script setup lang="ts">
import type { Component } from "vue";

defineProps<{
  icon?: Component;
  title: string;
  description?: string;
}>();
</script>
