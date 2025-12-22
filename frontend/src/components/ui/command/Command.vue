<script setup lang="ts">
import {
  ComboboxRoot,
  type ComboboxRootEmits,
  type ComboboxRootProps,
  useForwardPropsEmits,
} from "radix-vue";
import { cn } from "@/lib/utils";

const props = withDefaults(
  defineProps<ComboboxRootProps & { class?: string }>(),
  {
    open: true,
    modelValue: "",
    class: "",
  }
);

const emits = defineEmits<ComboboxRootEmits>();

const forwarded = useForwardPropsEmits(props, emits);
</script>

<template>
  <ComboboxRoot
    v-bind="forwarded"
    :class="
      cn(
        'flex h-full w-full flex-col overflow-hidden rounded-md bg-popover text-popover-foreground',
        props.class,
      )
    "
  >
    <slot />
  </ComboboxRoot>
</template>
