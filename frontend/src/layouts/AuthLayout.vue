<template>
  <div class="min-h-screen bg-background text-foreground lg:grid lg:grid-cols-2">
    <!-- Brand panel. It exists only where there is room for it: below `lg` the
         header carries the same mark, so the form keeps the full width. -->
    <aside
      class="relative hidden overflow-hidden bg-gradient-to-br from-primary to-primary-hover text-primary-foreground lg:flex lg:flex-col lg:p-12"
    >
      <!-- Decorative layers: two soft glows and a masked grid give the flat
           brand fill depth without introducing a second colour. The dark-mode
           veil keeps the fixed white copy readable, because the dark theme's
           primary is a lighter blue than the light theme's. -->
      <div
        aria-hidden="true"
        class="pointer-events-none absolute inset-0"
      >
        <div class="absolute -left-24 -top-32 h-[26rem] w-[26rem] rounded-full bg-primary-foreground/15 blur-3xl" />
        <div class="absolute -bottom-40 -right-24 h-[30rem] w-[30rem] rounded-full bg-primary-foreground/10 blur-3xl" />
        <div class="auth-grid absolute inset-0" />
        <div class="absolute inset-0 hidden bg-black/40 dark:block" />
      </div>

      <!-- One centered reading column, so the brand content lines up with the
           form card across the gutter on wide screens instead of hugging the
           panel's left edge. -->
      <div class="relative mx-auto flex w-full max-w-md flex-1 flex-col">
        <div class="flex items-center gap-3">
          <span
            class="grid h-10 w-10 place-items-center rounded-xl bg-primary-foreground text-lg font-extrabold text-primary shadow-e2"
          >
            M
          </span>
          <span class="text-xl font-semibold tracking-tight">{{ brandName }}</span>
        </div>

        <div class="flex flex-1 flex-col justify-center py-12">
          <h2
            class="text-balance text-4xl font-semibold leading-[1.15] tracking-tight"
          >
            {{ t("login.subtitle") }}
          </h2>
          <ul class="mt-10 space-y-5">
            <li class="flex items-start gap-3.5">
              <span
                class="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-primary-foreground/15"
              >
                <Database class="h-4 w-4" />
              </span>
              <span class="pt-1.5 text-sm font-medium leading-snug">
                {{ t("login.featureRegistry") }}
              </span>
            </li>
            <li class="flex items-start gap-3.5">
              <span
                class="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-primary-foreground/15"
              >
                <GitBranch class="h-4 w-4" />
              </span>
              <span class="pt-1.5 text-sm font-medium leading-snug">
                {{ t("login.featureLineage") }}
              </span>
            </li>
            <li class="flex items-start gap-3.5">
              <span
                class="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-primary-foreground/15"
              >
                <ShieldCheck class="h-4 w-4" />
              </span>
              <span class="pt-1.5 text-sm font-medium leading-snug">
                {{ t("login.featureQuality") }}
              </span>
            </li>
          </ul>
        </div>
      </div>
    </aside>

    <!-- Form column. -->
    <div class="flex min-h-screen flex-col">
      <header class="flex items-center px-5 py-5 sm:px-8">
        <div class="mr-auto flex items-center gap-2.5 lg:hidden">
          <span
            class="grid h-8 w-8 place-items-center rounded-lg bg-primary text-sm font-bold text-primary-foreground shadow-e1"
          >
            M
          </span>
          <span class="text-base font-bold tracking-tight">{{ brandName }}</span>
        </div>
        <div
          class="ml-auto inline-flex items-center gap-0.5 rounded-full border border-border/70 bg-card p-0.5 shadow-e1"
        >
          <Button
            v-for="item in locales"
            :key="item.value"
            variant="ghost"
            size="xs"
            class="rounded-full px-3"
            :class="
              currentLocale === item.value
                ? 'bg-primary text-primary-foreground hover:bg-primary-hover'
                : 'text-muted-foreground hover:text-foreground'
            "
            :aria-pressed="currentLocale === item.value"
            @click="appStore.setLocale(item.value)"
          >
            {{ item.label }}
          </Button>
        </div>
      </header>

      <main class="flex flex-1 items-center justify-center px-4 pb-6 sm:px-8">
        <div class="w-full max-w-[420px]">
          <h1 class="sr-only">
            {{ brandName }}
          </h1>
          <slot />
        </div>
      </main>

      <footer class="px-6 pb-6">
        <p class="text-center text-xs text-muted-foreground">
          {{ t("login.footerNote") }}
        </p>
      </footer>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Database, GitBranch, ShieldCheck } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import type { AppLocale } from "@/locales";
import { useAppStore } from "@/store/modules/app";

const { t } = useI18n();
const appStore = useAppStore();

// The brand is the product name, not translatable copy: the same string is
// hardcoded in the sidebar and the mobile header.
const brandName = "MetaxisData";

const locales: Array<{ value: AppLocale; label: string }> = [
  { value: "zh-CN", label: "简体中文" },
  { value: "en-US", label: "English" },
];

const currentLocale = computed(() => appStore.locale);
</script>

<style scoped>
/* A fine grid fading out from the upper-left corner. Pure decoration: it must
   stay behind the copy and never capture a pointer. */
.auth-grid {
  background-image:
    linear-gradient(to right, rgb(255 255 255 / 0.1) 1px, transparent 1px),
    linear-gradient(to bottom, rgb(255 255 255 / 0.1) 1px, transparent 1px);
  background-size: 48px 48px;
  -webkit-mask-image: radial-gradient(
    ellipse 85% 75% at 30% 30%,
    #000 25%,
    transparent 85%
  );
  mask-image: radial-gradient(
    ellipse 85% 75% at 30% 30%,
    #000 25%,
    transparent 85%
  );
}
</style>
