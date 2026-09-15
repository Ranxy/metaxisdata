<template>
  <!-- At the root there is nothing to navigate back to, so the crumb collapses
       to nothing instead of rendering an empty bordered bar. -->
  <nav
    v-if="items.length > 0"
    class="flex min-w-0 flex-wrap items-center gap-x-1 text-sm"
    :aria-label="t('metadataBrowser.title')"
  >
    <Button
      variant="ghost"
      size="icon"
      class="h-7 w-7"
      :aria-label="t('menu.home')"
      @click="$emit('navigate', -1)"
    >
      <Home class="h-4 w-4" />
    </Button>

    <template
      v-for="item in items"
      :key="item.guidIndex"
    >
      <ChevronRight class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <button
        type="button"
        class="max-w-[16rem] truncate rounded px-1 py-0.5 transition-colors hover:text-primary"
        :class="{
          'font-medium text-foreground': item.guidIndex === lastGuidIndex,
        }"
        @click="handleClick(item.guidIndex)"
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

const emit = defineEmits<{
  navigate: [guidIndex: number];
}>();

const { t } = useI18n();

const lastGuidIndex = computed(() => {
  const last = props.items[props.items.length - 1];
  return last?.guidIndex ?? -1;
});

function handleClick(guidIndex: number) {
  // Clicking the current (last) breadcrumb item is a no-op.
  if (guidIndex === lastGuidIndex.value) return;
  emit("navigate", guidIndex);
}
</script>
