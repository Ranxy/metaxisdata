<template>
  <div class="flex h-dvh overflow-hidden bg-muted/40">
    <!-- The drawer is modal, so a click on the backdrop dismisses it. -->
    <div
      v-if="appStore.mobileNavOpen"
      class="fixed inset-0 z-40 bg-black/40 lg:hidden"
      @click="appStore.setMobileNavOpen(false)"
    />

    <AppSidebar />

    <div class="flex min-w-0 flex-1 flex-col">
      <AppHeader />

      <!-- The single scroll container for the whole shell. Pages never nest
           their own scrollers, so the window and the content area can never
           scroll independently.

           The scroller always spans the full remaining width: capping the
           scroller itself (the previous shape) parked the scrollbar at the edge
           of the capped column, leaving a dead gutter between the scrollbar and
           the window. Capped pages now constrain their own content instead.
           Full-width pages get no wrapper at all, which keeps `h-full` working
           for the full-height workbenches (Explain SQL, LLM providers).

           `scrollbar-gutter: stable` reserves the scrollbar track whether or not
           a bar is showing, so a page that grows taller than the viewport while
           it loads cannot shrink the content box and shift everything sideways. -->
      <main
        ref="mainRef"
        :class="[
          'min-h-0 flex-1 overflow-y-auto [scrollbar-gutter:stable]',
          capped ? '' : 'p-4 sm:p-6',
        ]"
      >
        <div
          v-if="capped"
          class="mx-auto w-full max-w-[1400px] p-4 sm:p-6"
        >
          <slot />
        </div>
        <slot v-else />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import AppHeader from "@/components/layout/AppHeader.vue";
import AppSidebar from "@/components/layout/AppSidebar.vue";
import { useAppStore } from "@/store/modules/app";

const route = useRoute();
const appStore = useAppStore();
const mainRef = ref<HTMLElement | null>(null);

// Form and reading pages sit in a capped column; data tables and the
// full-height workbenches opt out through route meta.
const capped = computed(() => route.meta.contentWidth !== "full");

// The window never scrolls, so the router's own scroll restoration cannot reach
// this container: without an explicit reset, a new page opens at the previous
// page's offset. Keying on the path (not the full path) keeps in-page query
// changes — tabs, a highlighted column — from yanking the view to the top.
watch(
  () => route.path,
  () => {
    mainRef.value?.scrollTo({ top: 0 });
  }
);
</script>
