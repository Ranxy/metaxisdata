<template>
  <div
    v-if="loading"
    class="flex justify-center p-8"
  >
    <AppLoading />
  </div>
  <div
    v-else-if="error"
    class="p-8 text-center text-sm text-destructive"
  >
    {{ error }}
  </div>
  <slot v-else />
</template>

<script setup lang="ts">
import AppLoading from "@/components/common/AppLoading.vue";

// The single loading/error gate for a data region, so pages stop re-declaring
// the same `p-8` blocks.
withDefaults(
  defineProps<{
    loading?: boolean;
    error?: string | null;
  }>(),
  { loading: false, error: null }
);
</script>
