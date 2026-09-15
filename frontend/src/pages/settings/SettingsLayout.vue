<template>
  <!-- The section is left-anchored, not centred. Centring was the source of two
       separate complaints: as soon as the window was wide enough for the cap to
       bite, every extra pixel pushed the navigation rail further right, and any
       change in the scroll container's width (a scrollbar appearing while a page
       loaded) nudged the whole column sideways.

       So the rail is chrome and stays pinned to the content edge, and only the
       reading column is capped. `min-h-full` keeps the section at least as tall
       as the viewport while still growing with a long form, which also gives the
       full-height LLM provider workbench a definite height. -->
  <div class="flex min-h-full w-full flex-col gap-4 md:flex-row md:gap-6">
    <!-- `self-start` is what lets the rail actually stick: a stretched flex item
         is already as tall as the row and has no room to move. `top-0` resolves
         against the scroll container's content edge, which is exactly where the
         rail already sits — a positive offset would push it below the title. -->
    <SettingsNav
      class="md:sticky md:top-0 md:w-52 md:shrink-0 md:self-start"
    />
    <div class="flex min-h-0 w-full max-w-[860px] min-w-0 flex-1 flex-col">
      <router-view />
    </div>
  </div>
</template>

<script setup lang="ts">
import SettingsNav from "@/components/layout/SettingsNav.vue";
</script>
