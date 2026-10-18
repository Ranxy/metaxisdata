<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { cn } from "@/lib/utils";

const props = defineProps<{
  class?: HTMLAttributes["class"];
}>();
</script>

<template>
  <div :class="cn('rounded-lg border border-border/70 bg-card text-card-foreground shadow-e1', props.class)">
    <slot />
  </div>
</template>
