<script setup lang="ts">
import { AvatarFallback, type AvatarFallbackProps } from "radix-vue";
import type { HTMLAttributes } from "vue";
import { cn } from "@/lib/utils";

interface Props extends AvatarFallbackProps {
  class?: HTMLAttributes["class"];
}

const props = defineProps<Props>();
</script>

<template>
  <AvatarFallback
    :class="
      cn(
        'flex h-full w-full items-center justify-center rounded-full bg-muted',
        props.class
      )
    "
  >
    <slot />
  </AvatarFallback>
</template>
