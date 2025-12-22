<template>
  <Button
    :type="type"
    :disabled="disabled || loading"
    :variant="buttonVariant"
    :size="buttonSize"
    :class="props.class"
  >
    <Loader2
      v-if="loading"
      class="mr-2 h-4 w-4 animate-spin"
    />
    <slot />
  </Button>
</template>

<script setup lang="ts">
import { Loader2 } from "lucide-vue-next";
import { computed, type HTMLAttributes } from "vue";
import { Button } from "@/components/ui/button";

interface Props {
  type?: "button" | "submit" | "reset";
  variant?: "primary" | "secondary" | "danger" | "ghost";
  size?: "sm" | "md" | "lg";
  disabled?: boolean;
  loading?: boolean;
  class?: HTMLAttributes["class"];
}

const props = withDefaults(defineProps<Props>(), {
  type: "button",
  variant: "primary",
  size: "md",
  disabled: false,
  loading: false,
  class: "",
});

const buttonVariant = computed(() => {
  switch (props.variant) {
    case "secondary":
      return "secondary";
    case "danger":
      return "destructive";
    case "ghost":
      return "ghost";
    default:
      return "default";
  }
});

const buttonSize = computed(() => {
  switch (props.size) {
    case "sm":
      return "sm";
    case "lg":
      return "lg";
    default:
      return "default";
  }
});
</script>
