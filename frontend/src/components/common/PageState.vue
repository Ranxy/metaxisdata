<template>
  <div
    v-if="loading"
    class="p-8"
  >
    <slot name="skeleton">
      <div class="flex justify-center">
        <AppLoading />
      </div>
    </slot>
  </div>
  <div
    v-else-if="error"
    class="flex flex-col items-center gap-3 p-8 text-center"
  >
    <span
      class="grid h-10 w-10 place-items-center rounded-full bg-danger-subtle text-danger"
    >
      <AlertTriangle class="h-5 w-5" />
    </span>
    <div class="max-w-sm space-y-1">
      <p class="text-sm font-semibold">
        {{ title || error }}
      </p>
      <p
        v-if="title && error !== title"
        class="break-words text-sm text-muted-foreground"
      >
        {{ error }}
      </p>
    </div>
    <div
      v-if="$slots.error"
      class="mt-1 flex flex-wrap items-center justify-center gap-2"
    >
      <slot name="error" />
    </div>
  </div>
  <slot v-else />
</template>

<script setup lang="ts">
import { AlertTriangle } from "lucide-vue-next";
import AppLoading from "@/components/common/AppLoading.vue";

// The single loading/error gate for a data region, so pages stop re-declaring
// the same `p-8` blocks. A region can replace the spinner with its own skeleton
// and add a retry action through the named slots.
withDefaults(
  defineProps<{
    loading?: boolean;
    error?: string | null;
    /** A short, localized headline; the raw message stays in `error`. */
    title?: string;
  }>(),
  { loading: false, error: null, title: "" }
);
</script>
