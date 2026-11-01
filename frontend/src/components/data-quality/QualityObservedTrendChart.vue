<template>
  <div class="space-y-3">
    <template v-if="!hasPoints">
      <p class="text-sm text-muted-foreground">
        {{ t(emptyKey) }}
      </p>
      <p
        v-if="trend.historyRestarted"
        class="text-xs text-muted-foreground"
      >
        {{ t("dataQuality.trend.historyRestarted") }}
      </p>
    </template>

    <template v-else>
      <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5 text-xs">
        <span class="text-muted-foreground">
          {{ t("dataQuality.trend.window", { count: trend.points.length }) }}
        </span>
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5">
          <span
            v-for="legend in statusLegend"
            :key="legend.status"
            class="inline-flex items-center gap-1.5 text-muted-foreground"
          >
            <span
              class="h-2 w-2 shrink-0 rounded-full"
              :style="{ backgroundColor: legend.color }"
              aria-hidden="true"
            />
            {{ t(legend.labelKey) }}
          </span>
          <span
            v-if="hasSampledPoint"
            class="inline-flex items-center gap-1.5 text-muted-foreground"
          >
            <span
              class="h-2.5 w-2.5 shrink-0 rounded-full border-2"
              :style="{ borderColor: 'hsl(var(--muted-foreground) / 0.7)' }"
              aria-hidden="true"
            />
            {{ t("dataQuality.sampled") }}
          </span>
          <!-- The dashed lines need a key as much as the markers do: their colour
               names the severity and the number is the bound itself. -->
          <span
            v-for="bound in thresholdLegend"
            :key="bound.key"
            class="inline-flex items-center gap-1.5 text-muted-foreground"
          >
            <span
              class="h-0 w-4 shrink-0 border-t-2 border-dashed"
              :style="{ borderColor: bound.color }"
              aria-hidden="true"
            />
            {{ t(bound.labelKey) }} {{ t(bound.operatorKey, { value: bound.value }) }}
          </span>
        </div>
      </div>

      <p
        v-if="trend.historyRestarted"
        class="text-xs text-muted-foreground"
      >
        {{ t("dataQuality.trend.historyRestarted") }}
      </p>

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

          <!-- The failing side of the band, shaded before anything else draws so
               the line and its points stay on top of it. -->
          <rect
            v-for="region in geometry.regions"
            :key="`region-${region.key}-${region.y}`"
            :x="geometry.paddingLeft"
            :y="region.y"
            :width="geometry.plotWidth"
            :height="region.height"
            :style="{ fill: regionFill(region.key) }"
            aria-hidden="true"
          />

          <!-- Horizontal grid and the value labels in the gutter. -->
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
              style="stroke: hsl(var(--border)); stroke-opacity: 0.7; stroke-dasharray: 3 4"
              aria-hidden="true"
            />
            <text
              :x="geometry.paddingLeft - 10"
              :y="tick.y + 4"
              class="text-[11px]"
              style="fill: hsl(var(--muted-foreground)); font-variant-numeric: tabular-nums"
              text-anchor="end"
            >
              {{ formatTrendAxisValue(tick.value, trend.unit, locale) }}
            </text>
          </g>

          <!-- The threshold band itself: a dashed line per bound. -->
          <line
            v-for="line in geometry.lines"
            :key="`threshold-${line.key}-${line.bound}`"
            :x1="geometry.paddingLeft"
            :x2="geometry.width - geometry.paddingRight"
            :y1="line.y"
            :y2="line.y"
            stroke-width="1.5"
            stroke-dasharray="5 4"
            :style="{ stroke: thresholdColor(line.key) }"
            aria-hidden="true"
          />

          <!-- The measured line. Points carry their own status colour, so the
               line itself stays neutral. -->
          <polyline
            :points="linePoints"
            fill="none"
            stroke-width="2"
            stroke-linejoin="round"
            stroke-linecap="round"
            style="stroke: hsl(var(--muted-foreground) / 0.55)"
            aria-hidden="true"
          />

          <!-- One slot per execution: the hover surface underneath, then the
               marker on top so a click on the marker wins. -->
          <rect
            v-for="entry in geometry.points"
            :key="`slot-${entry.point.execution}`"
            :x="entry.slotX"
            :y="geometry.paddingTop"
            :width="entry.slotWidth"
            :height="geometry.plotHeight"
            fill="transparent"
            pointer-events="all"
            @mouseenter="hovered = entry.index"
          />
          <g
            v-for="entry in geometry.points"
            :key="entry.point.execution"
            :data-sampled="entry.point.sampled ? 'true' : 'false'"
            tabindex="0"
            role="button"
            :aria-label="pointLabel(entry.point)"
            class="cursor-pointer outline-none"
            @mouseenter="hovered = entry.index"
            @focus="hovered = entry.index"
            @blur="clearHover(entry.index)"
            @click="selectPoint(entry.point)"
            @keydown.enter.prevent="selectPoint(entry.point)"
            @keydown.space.prevent="selectPoint(entry.point)"
          >
            <!-- A sampled point is hollow-ish: the ring says "judged from a
                 sample", which a filled dot would hide. -->
            <circle
              v-if="entry.point.sampled"
              :cx="entry.x"
              :cy="entry.y"
              r="7"
              fill="none"
              stroke-width="1.5"
              :style="{ stroke: pointColor(entry.point.status) }"
              aria-hidden="true"
            />
            <circle
              :cx="entry.x"
              :cy="entry.y"
              :r="entry.point.sampled ? 4 : 3.5"
              stroke-width="1.5"
              :style="{
                fill: pointColor(entry.point.status),
                stroke: 'hsl(var(--card))',
              }"
              aria-hidden="true"
            />
          </g>

          <!-- The x axis dates, on the first and last point plus enough in
               between to keep the window readable. -->
          <text
            v-for="index in geometry.axisIndices"
            :key="`axis-${index}`"
            :x="axisLabelX(index)"
            :y="geometry.height - 10"
            class="text-[11px]"
            style="fill: hsl(var(--muted-foreground))"
            text-anchor="middle"
          >
            {{ formatTrendPointDate(geometry.points[index]?.point ?? {}, locale) }}
          </text>
        </svg>

        <!-- The same numbers a pointer gets from a point. -->
        <div
          v-if="hoveredEntry"
          class="pointer-events-none absolute z-10 w-48 rounded-lg border border-border/70 bg-popover px-3 py-2 text-xs text-popover-foreground shadow-e3"
          :style="tooltipStyle"
          aria-hidden="true"
        >
          <div class="flex items-baseline justify-between gap-2">
            <span class="font-medium">
              {{ formatDateTime(hoveredEntry.point.startTime, locale) }}
            </span>
          </div>
          <div class="mt-1.5 space-y-1">
            <div class="flex items-center gap-1.5">
              <span
                class="h-1.5 w-1.5 shrink-0 rounded-full"
                :style="{ backgroundColor: pointColor(hoveredEntry.point.status) }"
                aria-hidden="true"
              />
              <span>{{ t(qualityStatusKey(hoveredEntry.point.status)) }}</span>
            </div>
            <div class="flex items-baseline justify-between gap-2">
              <span class="text-muted-foreground">{{ t("dataQuality.observed") }}</span>
              <span class="font-medium tabular-nums">{{ hoveredValue }}</span>
            </div>
            <div class="flex items-baseline justify-between gap-2">
              <span class="text-muted-foreground">{{ t("dataQuality.failedRows") }}</span>
              <span class="tabular-nums">{{ hoveredRows }}</span>
            </div>
            <div
              v-if="hoveredEntry.point.sampled"
              class="text-muted-foreground"
            >
              {{ t("dataQuality.sampled") }}
            </div>
          </div>
        </div>
      </div>

      <!-- Why there is no band, when the trend came back without one. Do not
           invent a threshold: the sentence names the reason instead. -->
      <p
        v-if="bandNoticeKey"
        class="text-xs text-muted-foreground"
      >
        {{ t(bandNoticeKey) }}
      </p>

      <!-- The chart as a table, for a screen reader that cannot hover a point. -->
      <table class="sr-only">
        <caption>{{ chartLabel }}</caption>
        <thead>
          <tr>
            <th scope="col">{{ t("dataQuality.startedAt") }}</th>
            <th scope="col">{{ t("dataQuality.resultStatus") }}</th>
            <th scope="col">{{ t("dataQuality.observed") }}</th>
            <th scope="col">{{ t("dataQuality.failedRows") }}</th>
            <th scope="col">{{ t("dataQuality.sampled") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="entry in geometry.points"
            :key="`row-${entry.point.execution}`"
          >
            <th scope="row">
              {{ formatDateTime(entry.point.startTime, locale) }}
            </th>
            <td>{{ t(qualityStatusKey(entry.point.status)) }}</td>
            <td>{{ formatTrendValue(entry.point.value, trend.unit, locale) }}</td>
            <td>
              {{ formatRowCount(entry.point.failedRows, locale) }} /
              {{ formatRowCount(entry.point.totalRows, locale) }}
            </td>
            <td>{{ entry.point.sampled ? t("dataQuality.sampled") : "-" }}</td>
          </tr>
        </tbody>
      </table>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  type QualityRuleTrend,
  type QualityRuleTrendPoint,
  type QualityRuleType,
  QualityStatus,
  QualityTrendUnit,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatRowCount, qualityStatusKey } from "@/utils/dataQuality";
import {
  buildObservedTrendGeometry,
  emptyTrendKey,
  formatTrendAxisValue,
  formatTrendPointDate,
  formatTrendValue,
  type TrendPointGeometry,
  trendBandNoticeKey,
} from "@/utils/dataQualityTrend";
import { formatDateTime } from "@/utils/datetime";

/**
 * One check's measured history as a line: no charting library, no dependencies.
 *
 * The geometry lives in `utils/dataQualityTrend.ts` where it is unit-tested; this
 * component only maps it into SVG. The drawing uses the container's measured
 * width 1:1 and never scales its viewBox, because a stretched viewBox distorts
 * the axis glyphs as much as the drawing. Every point is a status-coloured
 * marker over its own hover/click slot, the threshold band is shaded behind the
 * line, and the same data is mirrored as a visually hidden table so the chart is
 * readable without a pointer.
 */

const props = defineProps<{
  trend: QualityRuleTrend;
  /** The check's type: schema drift's empty history has no numeric value at all. */
  ruleType: QualityRuleType;
}>();

const emit = defineEmits<{
  /** Open one judged execution; the sheet decides whether the caller may. */
  select: [point: QualityRuleTrendPoint];
}>();

const { t, locale } = useI18n();

/** Fallback width before the container has been measured. */
const FALLBACK_WIDTH = 640;
/** Keep in step with `w-48` (12rem) in the tooltip's class list. */
const TOOLTIP_WIDTH = 192;
const TOOLTIP_EDGE = 8;
const TOOLTIP_GAP = 10;
/** Roughly the tooltip's height; it is pointer-events-none, so only the
 * position needs to be right on the first painted frame. */
const TOOLTIP_HEIGHT = 104;

const STATUS_COLORS: Array<{
  status: QualityStatus;
  labelKey: string;
  color: string;
}> = [
  {
    status: QualityStatus.PASS,
    labelKey: "dataQuality.status.pass",
    color: "hsl(var(--chart-pass))",
  },
  {
    status: QualityStatus.WARN,
    labelKey: "dataQuality.status.warn",
    color: "hsl(var(--chart-warn))",
  },
  {
    status: QualityStatus.FAIL,
    labelKey: "dataQuality.status.fail",
    color: "hsl(var(--chart-fail))",
  },
  {
    status: QualityStatus.ERROR,
    labelKey: "dataQuality.status.error",
    color: "hsl(var(--chart-error))",
  },
];

const STATUS_BY_POINT: Partial<Record<QualityStatus, string>> = {
  [QualityStatus.PASS]: "hsl(var(--chart-pass))",
  [QualityStatus.WARN]: "hsl(var(--chart-warn))",
  [QualityStatus.FAIL]: "hsl(var(--chart-fail))",
  [QualityStatus.ERROR]: "hsl(var(--chart-error))",
};

const THRESHOLD_COLORS: Record<"fail" | "warn", string> = {
  fail: "hsl(var(--chart-fail))",
  warn: "hsl(var(--chart-warn))",
};

const container = ref<HTMLElement | null>(null);
const chartWidth = ref(FALLBACK_WIDTH);
const hovered = ref<number | null>(null);
let observer: ResizeObserver | undefined;

const hasPoints = computed(() => props.trend.points.length > 0);

const emptyKey = computed(() =>
  emptyTrendKey(props.ruleType, props.trend.unit)
);

const geometry = computed(() =>
  buildObservedTrendGeometry(props.trend.points, props.trend.thresholds, {
    width: chartWidth.value,
    // A plain number has no natural zero to sit on: anchoring it at zero would
    // squash an aggregate whose values all live far from it.
    includeZero: props.trend.unit !== QualityTrendUnit.NUMBER,
  })
);

const chartLabel = computed(
  () =>
    `${t("dataQuality.trend.title")} — ${t("dataQuality.trend.window", { count: props.trend.points.length })}`
);

const bandNoticeKey = computed(() => {
  if (!hasPoints.value || props.trend.thresholds) {
    return undefined;
  }
  return trendBandNoticeKey(props.trend.bandNotice);
});

const statusLegend = computed(() =>
  STATUS_COLORS.filter((entry) =>
    props.trend.points.some((point) => point.status === entry.status)
  )
);

const hasSampledPoint = computed(() =>
  props.trend.points.some((point) => point.sampled)
);

const thresholdLegend = computed(() =>
  geometry.value.bounds.map((bound) => ({
    key: `${bound.key}-${bound.bound}`,
    labelKey:
      bound.key === "fail"
        ? "dataQuality.status.fail"
        : "dataQuality.status.warn",
    operatorKey:
      bound.bound === "upper"
        ? "dataQuality.thresholdGreaterThan"
        : "dataQuality.thresholdLessThan",
    value: formatTrendValue(bound.value, props.trend.unit, locale.value),
    color: thresholdColor(bound.key),
  }))
);

const linePoints = computed(() =>
  geometry.value.points.map((entry) => `${entry.x},${entry.y}`).join(" ")
);

const hoveredEntry = computed<TrendPointGeometry | null>(() => {
  const index = hovered.value;
  if (index === null) {
    return null;
  }
  return geometry.value.points[index] ?? null;
});

const hoveredValue = computed(() =>
  hoveredEntry.value
    ? formatTrendValue(
        hoveredEntry.value.point.value,
        props.trend.unit,
        locale.value
      )
    : "-"
);

const hoveredRows = computed(() =>
  hoveredEntry.value
    ? `${formatRowCount(hoveredEntry.value.point.failedRows, locale.value)} / ${formatRowCount(hoveredEntry.value.point.totalRows, locale.value)}`
    : "-"
);

const tooltipStyle = computed(() => {
  const entry = hoveredEntry.value;
  if (!entry) {
    return {};
  }
  const chart = geometry.value;
  const height = TOOLTIP_HEIGHT;
  const left = Math.min(
    Math.max(entry.x, TOOLTIP_WIDTH / 2 + TOOLTIP_EDGE),
    chart.width - TOOLTIP_WIDTH / 2 - TOOLTIP_EDGE
  );
  if (entry.y - height - TOOLTIP_GAP >= 0) {
    return {
      left: `${left}px`,
      top: `${entry.y - TOOLTIP_GAP}px`,
      transform: "translate(-50%, -100%)",
    };
  }
  // Not enough room above the marker (it sits near the top of the plot): put the
  // panel below it instead of letting it leave the card.
  return {
    left: `${left}px`,
    top: `${Math.min(entry.y + TOOLTIP_GAP, chart.height - height)}px`,
    transform: "translate(-50%, 0)",
  };
});

function pointColor(status: QualityStatus): string {
  return STATUS_BY_POINT[status] ?? "hsl(var(--muted-foreground) / 0.55)";
}

function thresholdColor(key: "fail" | "warn"): string {
  return THRESHOLD_COLORS[key];
}

function regionFill(key: "fail" | "warn"): string {
  return key === "fail"
    ? "hsl(var(--chart-fail) / 0.12)"
    : "hsl(var(--chart-warn) / 0.12)";
}

function axisLabelX(index: number): number {
  const entry = geometry.value.points[index];
  if (!entry) {
    return geometry.value.paddingLeft;
  }
  return Math.min(
    Math.max(entry.x, geometry.value.paddingLeft),
    geometry.value.width - geometry.value.paddingRight
  );
}

/** The marker's accessible name: its time, verdict and value. */
function pointLabel(point: QualityRuleTrendPoint): string {
  return [
    formatDateTime(point.startTime, locale.value),
    t(qualityStatusKey(point.status)),
    `${t("dataQuality.observed")} ${formatTrendValue(point.value, props.trend.unit, locale.value)}`,
  ].join(" · ");
}

/** A blur only clears the tooltip when the reader did not move to another point. */
function clearHover(index: number) {
  if (hovered.value === index) {
    hovered.value = null;
  }
}

function selectPoint(point: QualityRuleTrendPoint) {
  // A click while selecting text is a copy, not a navigation.
  if (window.getSelection()?.toString()) {
    return;
  }
  emit("select", point);
}

function measure() {
  const width = container.value?.clientWidth ?? 0;
  if (width > 0) {
    chartWidth.value = width;
  }
}

// A narrower window re-lays the points; the hovered slot may not exist any more.
watch(
  () => props.trend.points,
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
