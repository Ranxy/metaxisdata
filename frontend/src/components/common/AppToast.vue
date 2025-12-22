<template>
  <Toaster position="top-right" />
</template>

<script setup lang="ts">
import { onMounted, watch } from "vue";
import { toast } from "vue-sonner";
import { Toaster } from "@/components/ui/sonner";
import { type ToastType, useToastStore } from "@/store/modules/toast";

const toastStore = useToastStore();

function showToast(message: string, type: ToastType) {
  switch (type) {
    case "success":
      toast.success(message);
      break;
    case "error":
      toast.error(message);
      break;
    case "warning":
      toast.warning(message);
      break;
    case "info":
    default:
      toast.info(message);
      break;
  }
}

// Watch for new toasts and display them
watch(
  () => toastStore.toasts,
  (toasts) => {
    if (toasts.length > 0) {
      const latestToast = toasts[toasts.length - 1];
      showToast(latestToast.message, latestToast.type);
      // Remove from store after showing
      toastStore.removeToast(latestToast.id);
    }
  },
  { deep: true }
);

// Handle any existing toasts on mount
onMounted(() => {
  toastStore.toasts.forEach((t) => {
    showToast(t.message, t.type);
    toastStore.removeToast(t.id);
  });
});
</script>
