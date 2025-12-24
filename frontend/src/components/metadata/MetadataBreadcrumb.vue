<template>
  <nav class="flex items-center space-x-1 text-sm">
    <Button
      variant="ghost"
      size="icon"
      class="h-8 w-8"
      @click="$emit('navigate', -1)"
    >
      <Home class="h-4 w-4" />
    </Button>

    <template
      v-for="item in items"
      :key="item.guidIndex"
    >
      <ChevronRight class="h-4 w-4 text-muted-foreground" />
      <button
        class="hover:text-primary transition-colors"
        :class="{
          'font-medium text-foreground': item.guidIndex === lastGuidIndex,
        }"
        @click="$emit('navigate', item.guidIndex)"
      >
        {{ item.label || t("metadataBrowser.defaultSchema") }}
      </button>
    </template>
  </nav>
</template>

<script setup lang="ts">
import { ChevronRight, Home } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";

const props = defineProps<{
  items: Array<{ label: string; guidIndex: number }>;
}>();

defineEmits<{
  navigate: [guidIndex: number];
}>();

const { t } = useI18n();

const lastGuidIndex = computed(() => {
  const last = props.items[props.items.length - 1];
  return last?.guidIndex ?? -1;
});
</script>
