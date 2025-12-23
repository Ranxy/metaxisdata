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
      v-for="(segment, index) in pathSegments"
      :key="index"
    >
      <ChevronRight class="h-4 w-4 text-muted-foreground" />
      <button
        class="hover:text-primary transition-colors"
        :class="{
          'font-medium text-foreground': index === pathSegments.length - 1,
        }"
        @click="$emit('navigate', index)"
      >
        {{ segment || t("metadataBrowser.defaultSchema") }}
      </button>
    </template>
  </nav>
</template>

<script setup lang="ts">
import { ChevronRight, Home } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";

defineProps<{
  pathSegments: string[];
}>();

defineEmits<{
  navigate: [index: number];
}>();

const { t } = useI18n();
</script>
