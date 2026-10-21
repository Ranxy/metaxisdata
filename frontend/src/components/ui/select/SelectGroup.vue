<script setup lang="ts">
import { SelectGroup, type SelectGroupProps, useForwardProps } from "radix-vue";
import type { HTMLAttributes } from "vue";

const props = defineProps<
  SelectGroupProps & { class?: HTMLAttributes["class"] }
>();

const forwarded = useForwardProps(props);
</script>

<template>
  <SelectGroup v-bind="forwarded">
    <slot />
  </SelectGroup>
</template>
