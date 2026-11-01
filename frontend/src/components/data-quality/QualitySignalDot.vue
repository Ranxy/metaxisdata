<template>
  <span
    :class="['inline-block size-2 shrink-0 rounded-full', TONE_CLASS[tone]]"
    :role="label ? 'img' : undefined"
    :aria-label="label || undefined"
    :aria-hidden="label ? undefined : 'true'"
    :title="label || undefined"
  />
</template>

<script setup lang="ts">
import type { QualitySignalTone } from "@/utils/dataQualitySignal";

/**
 * The one status point a quality signal draws. The colour is a semantic token,
 * so it survives the dark theme; it is never the only carrier of the meaning —
 * a caller that renders no word passes `label` so the state reaches both a
 * screen reader and the point's own tooltip.
 */
withDefaults(defineProps<{ tone?: QualitySignalTone; label?: string }>(), {
  tone: "neutral",
  label: "",
});

const TONE_CLASS: Record<QualitySignalTone, string> = {
  success: "bg-success",
  warning: "bg-warning",
  danger: "bg-danger",
  destructive: "bg-destructive",
  info: "bg-info",
  neutral: "bg-muted-foreground/50",
};
</script>
