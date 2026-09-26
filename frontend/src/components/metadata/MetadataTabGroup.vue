<template>
  <div class="inline-flex flex-wrap rounded-lg border bg-muted/30 p-1">
    <button
      v-for="tab in tabs"
      :key="tab.value"
      type="button"
      class="rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
      :class="
        modelValue === tab.value
          ? 'bg-background text-foreground shadow-sm'
          : 'text-muted-foreground hover:text-foreground'
      "
      @click="emit('update:modelValue', tab.value)"
    >
      {{ tab.label }}
      <span
        v-if="tab.count !== undefined"
        class="ml-1.5 text-xs text-muted-foreground"
      >
        {{ tab.count }}
      </span>
    </button>
  </div>
</template>

<script setup lang="ts" generic="T extends string">
/**
 * The segmented control the metadata details switch their sections with. It
 * replaces four byte-identical hand-written strips (and the table detail's
 * longer variant). `T` keeps each caller's tab values a closed union.
 */
defineProps<{
  modelValue: T;
  tabs: Array<{ value: T; label: string; count?: number }>;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: T];
}>();
</script>
