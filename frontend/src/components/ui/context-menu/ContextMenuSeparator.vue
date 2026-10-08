<script setup lang="ts">
import {
  ContextMenuSeparator,
  type ContextMenuSeparatorProps,
} from "radix-vue";
import type { HTMLAttributes } from "vue";
import { cn } from "@/lib/utils";

const props = defineProps<
  ContextMenuSeparatorProps & { class?: HTMLAttributes["class"] }
>();
</script>

<template>
  <ContextMenuSeparator :class="cn('-mx-1 my-1 h-px bg-muted', props.class)" />
</template>
