<template>
  <div class="min-h-screen flex flex-col bg-muted/40">
    <!-- Top Header -->
    <AppHeader />

    <div class="flex flex-1 overflow-hidden">
      <!-- Left Sidebar -->
      <AppSidebar />

      <!-- Main Content -->
      <main class="flex-1 overflow-auto p-6">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import AppHeader from "@/components/layout/AppHeader.vue";
import AppSidebar from "@/components/layout/AppSidebar.vue";
</script>
