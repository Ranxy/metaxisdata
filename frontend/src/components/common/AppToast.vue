<template>
  <Toaster
    position="top-right"
    :theme="appStore.theme"
  />
</template>

<script setup lang="ts">
import { onMounted, watch } from "vue";
import { toast } from "vue-sonner";
import { Toaster } from "@/components/ui/sonner";
import { useAppStore } from "@/store/modules/app";
import { type ToastType, useToastStore } from "@/store/modules/toast";

const appStore = useAppStore();
const toastStore = useToastStore();

function showToast(message: string, type: ToastType, duration?: number) {
  // Without an explicit duration sonner always uses its own 4s default, which
  // is too short for a wrapped connection error; the store already picks a
  // longer one for errors.
  const options = { duration };
  switch (type) {
    case "success":
      toast.success(message, options);
      break;
    case "error":
      toast.error(message, options);
      break;
    case "warning":
      toast.warning(message, options);
      break;
    case "info":
    default:
      toast.info(message, options);
      break;
  }
}

// Watch for new toasts and display them
watch(
  () => toastStore.toasts,
  (toasts) => {
    if (toasts.length > 0) {
      const latestToast = toasts[toasts.length - 1];
      showToast(latestToast.message, latestToast.type, latestToast.duration);
      // Remove from store after showing
      toastStore.removeToast(latestToast.id);
    }
  },
  { deep: true }
);

// Handle any existing toasts on mount
onMounted(() => {
  toastStore.toasts.forEach((t) => {
    showToast(t.message, t.type, t.duration);
    toastStore.removeToast(t.id);
  });
});
</script>
