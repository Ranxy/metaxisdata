<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <slot
        name="trigger"
        :is-open="false"
      />
    </DropdownMenuTrigger>
    <DropdownMenuContent
      :align="align === 'right' ? 'end' : 'start'"
      :class="widthClass"
    >
      <slot />
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<script setup lang="ts">
import { computed } from "vue";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

interface Props {
  align?: "left" | "right";
  width?: "auto" | "sm" | "md" | "lg";
}

const props = withDefaults(defineProps<Props>(), {
  align: "left",
  width: "auto",
});

const widthClass = computed(() => {
  switch (props.width) {
    case "sm":
      return "w-32";
    case "md":
      return "w-48";
    case "lg":
      return "w-64";
    default:
      return "min-w-max";
  }
});
</script>
