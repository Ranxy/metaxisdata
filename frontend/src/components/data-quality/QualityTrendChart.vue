<template>
  <div class="space-y-3">
    <div class="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs">
      <span
        v-for="legend in LEGEND"
        :key="legend.key"
        class="inline-flex items-center gap-1.5 text-muted-foreground"
      >
        <span
          class="h-2.5 w-2.5 rounded-sm"
          :style="{ backgroundColor: legend.color }"
          aria-hidden="true"
        />
        {{ t(legend.labelKey) }}
      </span>
    </div>

    <svg
      :viewBox="`0 0 ${geometry.width} ${geometry.height}`"
      class="h-auto w-full"
      role="img"
      :aria-label="chartLabel"
      preserveAspectRatio="none"
    >
      <title>{{ chartLabel }}</title>

      <!-- Horizontal grid lines and their value labels. -->
      <g
        v-for="tick in geometry.ticks"
        :key="`tick-${tick.value}`"
      >
        <line
          :x1="geometry.paddingLeft"
          :x2="geometry.width - geometry.paddingRight"
          :y1="tick.y"
          :y2="tick.y"
          style="stroke: hsl(var(--border))"
          stroke-width="1"
          stroke-dasharray="2 3"
        />
        <text
          :x="geometry.paddingLeft - 6"
          :y="tick.y + 3"
          class="text-[10px]"
          style="fill: hsl(var(--muted-foreground))"
          text-anchor="end"
        >
          {{ tick.value }}
        </text>
      </g>

      <!-- One bar per day, stacked pass/warn/fail/error from the baseline. -->
      <g
        v-for="bar in geometry.bars"
        :key="bar.date"
      >
        <title>{{ barLabel(bar) }}</title>
        <rect
          v-for="segment in bar.segments"
          :key="segment.key"
          :x="bar.x"
          :y="segment.y"
          :width="bar.width"
          :height="segment.height"
          :style="{ fill: SEGMENT_COLORS[segment.key] }"
          rx="1"
        />
      </g>

      <!-- Baseline and the first/middle/last date labels. -->
      <line
        :x1="geometry.paddingLeft"
        :x2="geometry.width - geometry.paddingRight"
        :y1="geometry.baseline"
        :y2="geometry.baseline"
        style="stroke: hsl(var(--border))"
        stroke-width="1"
      />
      <text
        v-for="label in dateLabels"
        :key="label.date"
        :x="label.x"
        :y="geometry.height - 8"
        class="text-[10px]"
        style="fill: hsl(var(--muted-foreground))"
        :text-anchor="label.anchor"
      >
        {{ label.date }}
      </text>
    </svg>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  buildTrendGeometry,
  type ChartBar,
  type TrendDay,
  type TrendStackKey,
  trendDateLabels,
} from "@/utils/dataQualityOverview";

/**
 * The quality trend as stacked bars, drawn inline: this app has no charting
 * library, and a 7/30-day pass/warn/fail/error stack is a handful of rects.
 * The geometry lives in `utils/dataQualityOverview.ts` where it is unit-tested;
 * this component only maps it into SVG.
 *
 * The chart is a single `role="img"` with a sentence for its label, and every
 * bar carries a `<title>` so a pointer user gets the exact counts.
 */

const props = defineProps<{
  days: TrendDay[];
}>();

const { t } = useI18n();

// The theme tokens are HSL triplets, so they are used through `hsl(...)`.
const SEGMENT_COLORS: Record<TrendStackKey, string> = {
  pass: "hsl(var(--success))",
  warn: "hsl(var(--warning))",
  fail: "hsl(var(--destructive))",
  error: "hsl(var(--danger))",
};

const LEGEND = [
  {
    key: "pass",
    labelKey: "dataQuality.status.pass",
    color: "hsl(var(--success))",
  },
  {
    key: "warn",
    labelKey: "dataQuality.status.warn",
    color: "hsl(var(--warning))",
  },
  {
    key: "fail",
    labelKey: "dataQuality.status.fail",
    color: "hsl(var(--destructive))",
  },
  {
    key: "error",
    labelKey: "dataQuality.status.error",
    color: "hsl(var(--danger))",
  },
] as const;

const geometry = computed(() =>
  buildTrendGeometry(props.days, { width: 720, height: 200 })
);

const chartLabel = computed(() =>
  t("dataQuality.overview.chartLabel", {
    days: props.days.length,
    total: props.days.reduce((sum, day) => sum + day.total, 0),
  })
);

const dateLabels = computed(() => {
  const labels = trendDateLabels(geometry.value.dates);
  if (labels.length === 0) {
    return [];
  }
  const indexes = labels.map((label) => geometry.value.dates.indexOf(label));
  return labels.map((date, position) => {
    const index = indexes[position] ?? 0;
    const bar = geometry.value.bars[index];
    return {
      date,
      x: (bar?.x ?? 0) + (bar?.width ?? 0) / 2,
      anchor: "middle" as const,
    };
  });
});

function barLabel(bar: ChartBar): string {
  const parts = bar.segments.map(
    (segment) => `${t(labelKey(segment.key))} ${segmentTotal(bar, segment.key)}`
  );
  return `${bar.date}: ${parts.join(", ") || t("dataQuality.overview.noResults")}`;
}

function segmentTotal(bar: ChartBar, key: TrendStackKey): number {
  const day = props.days.find((item) => item.date === bar.date);
  return day?.[key] ?? 0;
}

function labelKey(key: TrendStackKey): string {
  return `dataQuality.status.${key}`;
}
</script>
