<script setup lang="ts">
import { Search } from "lucide-vue-next";
import { ComboboxInput, type ComboboxInputProps } from "radix-vue";
import { cn } from "@/lib/utils";

const props = defineProps<ComboboxInputProps & { class?: string }>();
// An undeclared attribute — `placeholder` is the one callers pass — belongs on the input,
// not on the wrapper: left inheritable it landed on the div and the search box showed no
// hint text.
defineOptions({ inheritAttrs: false });
</script>

<template>
  <div class="flex items-center border-b px-3">
    <Search class="mr-2 h-4 w-4 shrink-0 opacity-50" />
    <ComboboxInput
      v-bind="{ ...$attrs, ...props }"
      :class="
        cn(
          'flex h-10 w-full rounded-md bg-transparent py-3 text-sm outline-hidden disabled:cursor-not-allowed disabled:opacity-50',
          props.class,
        )
      "
    />
  </div>
</template>
