<template>
  <header
    class="h-14 bg-background border-b flex items-center px-4 sticky top-0 z-40"
  >
    <!-- Menu Toggle Button -->
    <Button
      variant="ghost"
      size="icon"
      class="mr-4"
      @click="appStore.toggleSidebar"
    >
      <Menu class="h-5 w-5" />
    </Button>

    <!-- Logo -->
    <div class="flex items-center">
      <span class="text-xl font-bold">MetaxisData</span>
    </div>

    <!-- Right Section -->
    <div class="ml-auto flex items-center space-x-2">
      <!-- Language Switcher -->
      <LanguageSwitcher />

      <!-- User Menu -->
      <UserMenu />
    </div>
  </header>
</template>

<script setup lang="ts">
import { Menu } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { useAppStore } from "@/store/modules/app";
import LanguageSwitcher from "./LanguageSwitcher.vue";
import UserMenu from "./UserMenu.vue";

const appStore = useAppStore();
</script>
