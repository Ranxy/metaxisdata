<template>
  <div class="space-y-3">
    <!-- The legend doubles as the window summary: colour, status, count. -->
    <div class="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs">
      <span
        v-for="legend in legendItems"
        :key="legend.key"
        class="inline-flex items-center gap-1.5 text-muted-foreground"
      >
        <span
          class="h-2 w-2 shrink-0 rounded-full"
          :style="{ backgroundColor: legend.color }"
          aria-hidden="true"
        />
        {{ t(legend.labelKey) }}
        <span class="font-medium tabular-nums text-foreground">{{ legend.count }}</span>
      </span>
    </div>

    <div
      ref="container"
      class="relative"
    >
      <svg
        :viewBox="`0 0 ${geometry.width} ${geometry.height}`"
        :height="geometry.height"
        class="block w-full"
        role="img"
        :aria-label="chartLabel"
        @mouseleave="hovered = null"
      >
        <title>{{ chartLabel }}</title>

        <!-- One clip per day, so the stack silhouette keeps a rounded top. -->
        <defs>
          <clipPath
            v-for="bar in geometry.bars"
            :id="clipID(bar.date)"
            :key="`clip-${bar.date}`"
          >
            <path :d="barShape(bar)" />
          </clipPath>
        </defs>

        <!-- Horizontal grid and the count labels in the gutter. -->
        <g
          v-for="tick in geometry.ticks"
          :key="`tick-${tick.value}`"
        >
          <line
            :x1="geometry.paddingLeft"
            :x2="geometry.width - geometry.paddingRight"
            :y1="tick.y"
            :y2="tick.y"
            stroke-width="1"
            :style="gridStyle(tick)"
          />
          <text
            :x="geometry.paddingLeft - 10"
            :y="tick.y + 4"
            class="text-[11px]"
            style="fill: hsl(var(--muted-foreground)); font-variant-numeric: tabular-nums"
            text-anchor="end"
          >
            {{ formatAxisCount(tick.value, locale) }}
          </text>
        </g>

        <!-- One column per day: hover surface, highlight, then the stack. -->
        <g
          v-for="(bar, index) in geometry.bars"
          :key="bar.date"
        >
          <rect
            v-if="hovered === index"
            :x="bar.slotX"
            :y="geometry.paddingTop - 8"
            :width="bar.slotWidth"
            :height="geometry.plotHeight + 8"
            rx="6"
            aria-hidden="true"
            style="fill: hsl(var(--muted) / 0.55)"
          />
          <g
            v-if="bar.segments.length > 0"
            :clip-path="`url(#${clipID(bar.date)})`"
          >
            <rect
              v-for="segment in bar.segments"
              :key="segment.key"
              :x="bar.x"
              :y="segment.y"
              :width="bar.width"
              :height="segment.height"
              :style="{ fill: SEGMENT_COLORS[segment.key] }"
            />
            <!-- A hairline in the card colour separates two statuses of one bar. -->
            <line
              v-for="boundary in segmentBoundaries(bar)"
              :key="boundary"
              :x1="bar.x"
              :x2="bar.x + bar.width"
              :y1="boundary"
              :y2="boundary"
              stroke-width="1.5"
              style="stroke: hsl(var(--card))"
            />
          </g>
          <!-- A day that executed nothing still owns its slot. -->
          <rect
            v-else
            :x="bar.x"
            :y="geometry.baseline - 2"
            :width="bar.width"
            :height="2"
            rx="1"
            aria-hidden="true"
            style="fill: hsl(var(--muted-foreground) / 0.3)"
          />
          <!-- The hover surface sits on top of the bars: a transparent rect
               underneath them never sees the pointer. -->
          <rect
            :x="bar.slotX"
            :y="geometry.paddingTop"
            :width="bar.slotWidth"
            :height="geometry.plotHeight"
            fill="transparent"
            pointer-events="all"
            @mouseenter="hovered = index"
          />
        </g>

        <!-- Baseline and the date labels under it. -->
        <line
          :x1="geometry.paddingLeft"
          :x2="geometry.width - geometry.paddingRight"
          :y1="geometry.baseline"
          :y2="geometry.baseline"
          stroke-width="1"
          style="stroke: hsl(var(--border))"
        />
        <text
          v-for="label in dateLabels"
          :key="label.date"
          :x="label.x"
          :y="geometry.height - 10"
          class="text-[11px]"
          :style="{ fill: label.isLast ? 'hsl(var(--foreground))' : 'hsl(var(--muted-foreground))' }"
          :font-weight="label.isLast ? 500 : undefined"
          text-anchor="middle"
        >
          {{ label.text }}
        </text>
      </svg>

      <!-- The same numbers a pointer gets from the hover column. -->
      <div
        v-if="hoveredBar"
        class="pointer-events-none absolute z-10 w-44 rounded-lg border border-border/70 bg-popover px-3 py-2 text-xs text-popover-foreground shadow-e3"
        :style="tooltipStyle"
        aria-hidden="true"
      >
        <div class="flex items-baseline justify-between gap-2">
          <span class="font-medium">{{ formatDate(hoveredBar.date, locale) }}</span>
          <span class="tabular-nums text-muted-foreground">
            {{ t("dataQuality.overview.trendTooltipTotal", { total: hoveredBar.total }) }}
          </span>
        </div>
        <div
          v-if="hoveredRows.length > 0"
          class="mt-1.5 space-y-1"
        >
          <div
            v-for="row in hoveredRows"
            :key="row.key"
            class="flex items-center gap-1.5"
          >
            <span
              class="h-1.5 w-1.5 shrink-0 rounded-full"
              :style="{ backgroundColor: row.color }"
              aria-hidden="true"
            />
            <span class="text-muted-foreground">{{ t(row.labelKey) }}</span>
            <span class="ml-auto font-medium tabular-nums">{{ row.count }}</span>
          </div>
        </div>
        <p
          v-else
          class="mt-1 text-muted-foreground"
        >
          {{ t("dataQuality.overview.noResults") }}
        </p>
      </div>
    </div>

    <!-- The chart as a table, for a screen reader that cannot hover a bar. -->
    <table class="sr-only">
      <caption>{{ chartLabel }}</caption>
      <thead>
        <tr>
          <th scope="col">{{ t("dataQuality.overview.trendDate") }}</th>
          <th
            v-for="legend in LEGEND"
            :key="legend.key"
            scope="col"
          >
            {{ t(legend.labelKey) }}
          </th>
          <th scope="col">{{ t("dataQuality.overview.trendColumnTotal") }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="day in days"
          :key="day.date"
        >
          <th scope="row">{{ formatDate(day.date, locale) }}</th>
          <td
            v-for="legend in LEGEND"
            :key="legend.key"
          >
            {{ day[legend.key] }}
          </td>
          <td>{{ day.total }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  buildTrendGeometry,
  type ChartBar,
  formatAxisCount,
  formatTrendAxisDate,
  type TrendDay,
  type TrendStackKey,
  trendDateLabels,
} from "@/utils/dataQualityOverview";
import { formatDate } from "@/utils/datetime";

/**
 * The quality trend as stacked bars, drawn inline: this app has no charting
 * library, and a 7/30-day pass/warn/fail/error stack is a handful of rects.
 * The geometry lives in `utils/dataQualityOverview.ts` where it is unit-tested;
 * this component only maps it into SVG.
 *
 * The SVG is drawn at the container's measured width and a fixed height, and
 * never scaled: a stretched viewBox distorts the axis glyphs as much as the
 * bars. Hovering a day highlights its column and opens a tooltip with the exact
 * counts; the same table is available to a screen reader below the drawing.
 */

const props = defineProps<{
  days: TrendDay[];
}>();

const { t, locale } = useI18n();
const uid = useId();

// The theme tokens are HSL triplets, so they are used through `hsl(...)`.
const SEGMENT_COLORS: Record<TrendStackKey, string> = {
  pass: "hsl(var(--chart-pass))",
  warn: "hsl(var(--chart-warn))",
  fail: "hsl(var(--chart-fail))",
  error: "hsl(var(--chart-error))",
};

const LEGEND = [
  {
    key: "pass",
    labelKey: "dataQuality.status.pass",
    color: "hsl(var(--chart-pass))",
  },
  {
    key: "warn",
    labelKey: "dataQuality.status.warn",
    color: "hsl(var(--chart-warn))",
  },
  {
    key: "fail",
    labelKey: "dataQuality.status.fail",
    color: "hsl(var(--chart-fail))",
  },
  {
    key: "error",
    labelKey: "dataQuality.status.error",
    color: "hsl(var(--chart-error))",
  },
] as const;

/** Fallback width before the container has been measured. */
const FALLBACK_WIDTH = 720;
const TOOLTIP_WIDTH = 176;

const container = ref<HTMLElement | null>(null);
const chartWidth = ref(FALLBACK_WIDTH);
const hovered = ref<number | null>(null);
let observer: ResizeObserver | undefined;

const geometry = computed(() =>
  buildTrendGeometry(props.days, { width: chartWidth.value })
);

const chartLabel = computed(() =>
  t("dataQuality.overview.chartLabel", {
    days: props.days.length,
    total: props.days.reduce((sum, day) => sum + day.total, 0),
  })
);

const legendItems = computed(() =>
  LEGEND.map((legend) => ({
    ...legend,
    count: props.days.reduce((sum, day) => sum + day[legend.key], 0),
  }))
);

const hoveredBar = computed<ChartBar | null>(() => {
  const index = hovered.value;
  return index === null ? null : (geometry.value.bars[index] ?? null);
});

const hoveredRows = computed(() => {
  const index = hovered.value;
  const day = index === null ? undefined : props.days[index];
  if (!day) {
    return [];
  }
  return LEGEND.filter((legend) => day[legend.key] > 0).map((legend) => ({
    key: legend.key,
    labelKey: legend.labelKey,
    color: legend.color,
    count: day[legend.key],
  }));
});

const tooltipStyle = computed(() => {
  const bar = hoveredBar.value;
  if (!bar) {
    return {};
  }
  const center = bar.x + bar.width / 2;
  const left = Math.min(
    Math.max(center, TOOLTIP_WIDTH / 2 + 8),
    geometry.value.width - TOOLTIP_WIDTH / 2 - 8
  );
  const above = bar.top > 110;
  return {
    left: `${left}px`,
    top: above ? `${bar.top - 10}px` : `${geometry.value.paddingTop + 6}px`,
    transform: above ? "translate(-50%, -100%)" : "translate(-50%, 0)",
  };
});

const dateLabels = computed(() => {
  const chart = geometry.value;
  // Roughly one label per 64px of plot: seven days on a desktop, fewer on a phone.
  const maxLabels = Math.max(2, Math.min(7, Math.floor(chart.plotWidth / 64)));
  const labels = trendDateLabels(chart.dates, maxLabels);
  const lastDate = chart.dates[chart.dates.length - 1];
  return labels.map((date) => {
    const index = chart.dates.indexOf(date);
    const bar = chart.bars[index];
    return {
      date,
      x: (bar?.x ?? 0) + (bar?.width ?? 0) / 2,
      isLast: date === lastDate,
      text:
        date === lastDate
          ? t("dataQuality.overview.today")
          : formatTrendAxisDate(date, locale.value),
    };
  });
});

function clipID(date: string): string {
  return `trend-bar-${uid}-${date}`;
}

/** The stack silhouette: rounded at the top, square where it meets the axis. */
function barShape(bar: ChartBar): string {
  const bottom = geometry.value.baseline;
  const height = bottom - bar.top;
  if (height <= 0) {
    return "";
  }
  const { x, width, top } = bar;
  const radius = Math.min(4, width / 2, height);
  return [
    `M ${x} ${bottom}`,
    `L ${x} ${top + radius}`,
    `Q ${x} ${top} ${x + radius} ${top}`,
    `L ${x + width - radius} ${top}`,
    `Q ${x + width} ${top} ${x + width} ${top + radius}`,
    `L ${x + width} ${bottom}`,
    "Z",
  ].join(" ");
}

/** The y of every status boundary inside one bar. */
function segmentBoundaries(bar: ChartBar): number[] {
  return bar.segments.slice(0, -1).map((segment) => segment.y);
}

function gridStyle(tick: { value: number }): Record<string, string> {
  if (tick.value === 0) {
    return { stroke: "hsl(var(--border))" };
  }
  return {
    stroke: "hsl(var(--border))",
    "stroke-opacity": "0.7",
    "stroke-dasharray": "3 4",
  };
}

function measure() {
  const width = container.value?.clientWidth ?? 0;
  if (width > 0) {
    chartWidth.value = width;
  }
}

// A narrower window re-lays the bars; the hovered column may not exist any more.
watch(
  () => props.days,
  () => {
    hovered.value = null;
  }
);

onMounted(() => {
  measure();
  if (container.value && typeof ResizeObserver !== "undefined") {
    observer = new ResizeObserver(measure);
    observer.observe(container.value);
  }
  window.addEventListener("resize", measure);
});

onBeforeUnmount(() => {
  observer?.disconnect();
  window.removeEventListener("resize", measure);
});
</script>
