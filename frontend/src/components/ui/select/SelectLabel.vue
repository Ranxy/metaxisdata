<script setup lang="ts">
import { SelectLabel, type SelectLabelProps, useForwardProps } from "radix-vue";
import type { HTMLAttributes } from "vue";
import { cn } from "@/lib/utils";

const props = defineProps<
  SelectLabelProps & { class?: HTMLAttributes["class"] }
>();

const forwarded = useForwardProps(props);
</script>

<template>
  <SelectLabel
    v-bind="forwarded"
    :class="
      cn('px-2 py-1.5 text-xs font-medium text-muted-foreground', props.class)
    "
  >
    <slot />
  </SelectLabel>
</template>
