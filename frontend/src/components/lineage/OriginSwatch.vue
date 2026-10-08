<template>
  <!-- A sample of an edge's stroke: the colour and the dash pattern together are
       what name a lineage origin, so the legend has to show both. -->
  <svg class="shrink-0" :width="width" height="10" aria-hidden="true">
    <line
      x1="1"
      y1="5"
      :x2="width - 1"
      y2="5"
      stroke-width="2"
      :stroke="originColor(origin)"
      :stroke-dasharray="LINEAGE_ORIGIN_DASH[origin]"
    />
  </svg>
</template>

<script setup lang="ts">
import {
  LINEAGE_ORIGIN_DASH,
  type LineageOrigin,
  originColor,
} from "@/lib/lineageOrigin";

withDefaults(
  defineProps<{
    origin: LineageOrigin;
    /** The sample's width; a chip in the detail panel is narrower than a row. */
    width?: number;
  }>(),
  { width: 26 }
);
</script>
