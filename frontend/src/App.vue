<template>
  <component :is="layoutComponent">
    <router-view />
  </component>
  <AppToast />
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import AppToast from "@/components/common/AppToast.vue";
import AuthLayout from "@/layouts/AuthLayout.vue";
import DefaultLayout from "@/layouts/DefaultLayout.vue";

const route = useRoute();

const layoutComponent = computed(() => {
  const layout = route.meta.layout as string;
  if (layout === "auth") {
    return AuthLayout;
  }
  return DefaultLayout;
});
</script>
