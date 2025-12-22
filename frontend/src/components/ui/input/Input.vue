<script setup lang="ts">
import { useVModel } from "@vueuse/core";
import type { HTMLAttributes } from "vue";
import { cn } from "@/lib/utils";

const props = defineProps<{
  defaultValue?: string | number;
  modelValue?: string | number;
  class?: HTMLAttributes["class"];
}>();

const emits = defineEmits<{
  "update:modelValue": [payload: string | number];
}>();

const modelValue = useVModel(props, "modelValue", emits, {
  passive: true,
  defaultValue: props.defaultValue,
});
</script>

<template>
  <input
    v-model="modelValue"
    :class="
      cn(
        'flex h-10 w-full rounded-md border-0 bg-background px-3 py-2 text-sm shadow-[inset_0_0_0_1px_rgb(0_0_0/0.1)] file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:shadow-[inset_0_0_0_1.5px_rgb(0_0_0/0.2)] disabled:cursor-not-allowed disabled:opacity-50 transition-shadow',
        props.class
      )
    "
  >
</template>
