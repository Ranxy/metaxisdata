<template>
  <!-- Below `lg` the sidebar is an overlay drawer, so the shell keeps a thin bar
       whose only job is to open it. On desktop the sidebar owns the brand and
       the account controls, and no top bar is rendered at all. -->
  <header
    class="flex h-12 shrink-0 items-center gap-2 border-b bg-background px-2 lg:hidden"
  >
    <Button
      variant="ghost"
      size="icon"
      :aria-label="t('header.openNav')"
      @click="appStore.setMobileNavOpen(true)"
    >
      <Menu class="h-5 w-5" />
    </Button>
    <span class="truncate text-sm font-bold tracking-tight">
      {{ brandName }}
    </span>
  </header>
</template>

<script setup lang="ts">
import { Menu } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import { useAppStore } from "@/store/modules/app";

const { t } = useI18n();
const appStore = useAppStore();

const brandName = "MetaxisData";
</script>
