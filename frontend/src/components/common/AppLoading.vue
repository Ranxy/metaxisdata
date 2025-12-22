<template>
  <div
    v-if="fullscreen"
    class="fixed inset-0 flex items-center justify-center bg-background/80 z-50"
  >
    <div class="flex flex-col items-center">
      <Loader2 class="h-10 w-10 animate-spin text-primary" />
      <p
        v-if="text"
        class="mt-4 text-muted-foreground"
      >
        {{ text }}
      </p>
    </div>
  </div>
  <div
    v-else
    class="flex items-center justify-center p-4"
  >
    <Loader2 :class="['animate-spin text-primary', sizeClass]" />
    <span
      v-if="text"
      class="ml-2 text-muted-foreground"
    >{{ text }}</span>
  </div>
</template>

<script setup lang="ts">
import { Loader2 } from "lucide-vue-next";
import { computed } from "vue";

interface Props {
  size?: "sm" | "md" | "lg";
  text?: string;
  fullscreen?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  size: "md",
  text: "",
  fullscreen: false,
});

const sizeClass = computed(() => {
  switch (props.size) {
    case "sm":
      return "h-4 w-4";
    case "lg":
      return "h-8 w-8";
    default:
      return "h-6 w-6";
  }
});
</script>
