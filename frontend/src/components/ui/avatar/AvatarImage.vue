<script setup lang="ts">
import { AvatarImage, type AvatarImageProps, useForwardProps } from "radix-vue";
import type { HTMLAttributes } from "vue";
import { cn } from "@/lib/utils";

interface Props extends AvatarImageProps {
  class?: HTMLAttributes["class"];
}

const props = defineProps<Props>();

const forwarded = useForwardProps(props);
</script>

<template>
  <AvatarImage
    v-bind="forwarded"
    :class="cn('aspect-square h-full w-full', props.class)"
  />
</template>
