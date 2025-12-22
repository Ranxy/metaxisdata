<script setup lang="ts">
import { ComboboxGroup, type ComboboxGroupProps } from "radix-vue";
import { cn } from "@/lib/utils";

const props = defineProps<ComboboxGroupProps & { class?: string }>();
</script>

<template>
  <ComboboxGroup
    v-bind="props"
    :class="
      cn(
        'overflow-hidden p-1 text-foreground',
        props.class,
      )
    "
  >
    <slot />
  </ComboboxGroup>
</template>
