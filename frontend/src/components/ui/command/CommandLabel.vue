<script setup lang="ts">
import { ComboboxLabel, type ComboboxLabelProps } from "radix-vue";
import { cn } from "@/lib/utils";

const props = defineProps<ComboboxLabelProps & { class?: string }>();
</script>

<template>
  <ComboboxLabel
    v-bind="props"
    :class="
      cn(
        'px-2 py-1.5 text-xs font-medium text-muted-foreground',
        props.class,
      )
    "
  >
    <slot />
  </ComboboxLabel>
</template>
