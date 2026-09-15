<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { cn } from "@/lib/utils";

const props = defineProps<{
  class?: HTMLAttributes["class"];
}>();
</script>

<template>
  <!-- h2, not the upstream shadcn h3: this app's outline is page h1 -> card
       section. Cards are always the top-level regions of a page here, so an h3
       would make every card page skip a heading level. -->
  <h2 :class="cn('text-2xl font-semibold leading-none tracking-tight', props.class)">
    <slot />
  </h2>
</template>
