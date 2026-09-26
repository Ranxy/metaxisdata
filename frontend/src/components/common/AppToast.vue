<template>
  <Toaster
    position="top-right"
    :theme="appStore.theme"
  />
</template>

<script setup lang="ts">
import { Toaster } from "@/components/ui/sonner";
import { useAppStore } from "@/store/modules/app";

const appStore = useAppStore();
</script>
