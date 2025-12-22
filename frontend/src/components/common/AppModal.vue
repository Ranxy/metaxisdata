<template>
  <Dialog
    :open="modelValue"
    @update:open="handleClose"
  >
    <DialogContent :class="sizeClass">
      <!-- Header -->
      <DialogHeader v-if="title || $slots.header">
        <slot name="header">
          <DialogTitle>{{ title }}</DialogTitle>
        </slot>
      </DialogHeader>

      <!-- Body -->
      <div class="py-2 overflow-y-auto max-h-[calc(90vh-200px)]">
        <slot />
      </div>

      <!-- Footer -->
      <DialogFooter v-if="$slots.footer">
        <slot name="footer" />
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, watch } from "vue";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

interface Props {
  modelValue: boolean;
  title?: string;
  size?: "sm" | "md" | "lg" | "xl";
  closable?: boolean;
  closeOnBackdrop?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  title: "",
  size: "md",
  closable: true,
  closeOnBackdrop: true,
});

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
}>();

const sizeClass = computed(() => {
  switch (props.size) {
    case "sm":
      return "max-w-sm";
    case "lg":
      return "max-w-2xl";
    case "xl":
      return "max-w-4xl";
    default:
      return "max-w-lg";
  }
});

function handleClose(open: boolean) {
  emit("update:modelValue", open);
}

// Prevent body scroll when modal is open
watch(
  () => props.modelValue,
  (isOpen) => {
    if (isOpen) {
      document.body.style.overflow = "hidden";
    } else {
      document.body.style.overflow = "";
    }
  }
);
</script>
