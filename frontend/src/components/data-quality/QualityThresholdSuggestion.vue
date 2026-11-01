<template>
  <div
    v-if="suggestion"
    class="space-y-2 rounded-md border bg-muted/20 p-4"
  >
    <div class="space-y-1">
      <h4 class="text-sm font-medium">
        {{ t("dataQuality.trend.suggestion.title") }}
      </h4>
      <p class="text-xs text-muted-foreground">
        {{ t("dataQuality.trend.suggestion.description") }}
      </p>
    </div>

    <!-- The suggestion is derived from the same judged history the sheet draws;
         when there is not enough of it, the panel names the reason instead of
         showing an empty number. -->
    <p
      v-if="noticeKey"
      class="text-sm text-muted-foreground"
    >
      {{ t(noticeKey) }}
    </p>
    <template v-else>
      <p class="text-sm">
        {{ t("dataQuality.trend.suggestion.sample", { count: suggestion.sampleSize }) }}
        <span
          v-if="methodKey"
          class="text-muted-foreground"
        >
          · {{ t(methodKey) }}
        </span>
      </p>
      <ul class="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        <li
          v-for="row in suggestionRows"
          :key="row.key"
          class="inline-flex items-center gap-1.5"
        >
          <span
            class="h-2 w-2 shrink-0 rounded-full"
            :style="{ backgroundColor: row.color }"
            aria-hidden="true"
          />
          <span>
            {{ t(row.labelKey) }}
            {{ t(row.operatorKey, { value: row.value }) }}
          </span>
        </li>
      </ul>
    </template>

    <p
      v-if="unitChanged"
      class="text-xs text-warning"
    >
      {{ t("dataQuality.trend.suggestion.unitChanged") }}
    </p>

    <Button
      v-if="!noticeKey"
      size="sm"
      variant="outline"
      :disabled="unitChanged"
      @click="apply"
    >
      {{ t("dataQuality.trend.suggestion.apply") }}
    </Button>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { getQualityRuleTrend } from "@/api/data-quality";
import { Button } from "@/components/ui/button";
import {
  type QualityRule,
  type QualityRuleTrend,
  QualityRuleType,
  QualityThresholdMetric,
  type QualityThresholdSuggestion,
  QualityTrendUnit,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  isThresholdMetricType,
  type NullMetric,
} from "@/utils/dataQualityForm";
import {
  formatTrendValue,
  trendSuggestionMethodKey,
  trendSuggestionNoticeKey,
  trendThresholdBounds,
} from "@/utils/dataQualityTrend";

/**
 * A threshold derived from the check's own judged history, offered next to the
 * form. Reading it writes nothing: the button fills the form, and a person still
 * has to save the check. That is why the panel sits inside the editor and never
 * appears while creating a check — there is no history to derive from yet.
 *
 * The suggestion describes the saved rule. Changing the check type or the
 * measurement unit in the form makes the numbers incomparable with what is being
 * edited, so the button is disabled and says why instead of copying a bound
 * expressed in the wrong unit.
 */

const props = defineProps<{
  /** The saved check; absent means the composer is creating one. */
  rule?: QualityRule;
  /** The check type currently selected in the form. */
  ruleType: QualityRuleType;
  /** The shared COUNT/PERCENT metric currently selected in the form. */
  thresholdMetric: QualityThresholdMetric;
  /** null_percent's own count/percent choice, which replaces the shared metric. */
  nullMetric?: NullMetric;
}>();

const emit = defineEmits<{
  /** The reader asked to fill the form from the suggestion; nothing is saved. */
  apply: [suggestion: QualityThresholdSuggestion];
}>();

const { t, locale } = useI18n();

/** How many judged executions the suggestion panel asks for, matching the sheet. */
const TREND_LIMIT = 30;

const trend = ref<QualityRuleTrend>();
let ticket = 0;
let controller: AbortController | undefined;

const suggestion = computed(() => trend.value?.suggestion);

const noticeKey = computed(() => {
  const value = suggestion.value;
  if (!value || value.thresholds) {
    return undefined;
  }
  return trendSuggestionNoticeKey(value.notice);
});

const methodKey = computed(() =>
  suggestion.value
    ? trendSuggestionMethodKey(suggestion.value.method)
    : undefined
);

const suggestionRows = computed(() => {
  const thresholds = suggestion.value?.thresholds;
  if (!thresholds) {
    return [];
  }
  const unit = trend.value?.unit;
  return trendThresholdBounds(thresholds).map((bound) => ({
    key: `${bound.key}-${bound.bound}`,
    labelKey:
      bound.key === "fail"
        ? "dataQuality.status.fail"
        : "dataQuality.status.warn",
    operatorKey:
      bound.bound === "upper"
        ? "dataQuality.thresholdGreaterThan"
        : "dataQuality.thresholdLessThan",
    value: formatTrendValue(
      bound.value,
      unit ?? QualityTrendUnit.UNSPECIFIED,
      locale.value
    ),
    color:
      bound.key === "fail"
        ? "hsl(var(--chart-fail))"
        : "hsl(var(--chart-warn))",
  }));
});

/** The saved check's normalized metric, read the same way `ruleToForm` reads it. */
const savedThresholdMetric = computed<QualityThresholdMetric>(() => {
  const rule = props.rule;
  if (!rule) {
    return QualityThresholdMetric.COUNT;
  }
  return rule.config?.thresholdMetric === QualityThresholdMetric.PERCENT &&
    isThresholdMetricType(rule.ruleType)
    ? QualityThresholdMetric.PERCENT
    : QualityThresholdMetric.COUNT;
});

/** The saved null_percent check's own count/percent choice. */
const savedNullMetric = computed<NullMetric>(() => {
  const config = props.rule?.config?.config;
  if (config?.case !== "nullPercent") {
    return "percent";
  }
  return config.value.maxNullCount !== undefined ||
    config.value.warnNullCount !== undefined
    ? "count"
    : "percent";
});

const unitChanged = computed(() => {
  const rule = props.rule;
  if (!rule) {
    return false;
  }
  if (rule.ruleType !== props.ruleType) {
    return true;
  }
  if (savedThresholdMetric.value !== props.thresholdMetric) {
    return true;
  }
  // null_percent measures in its own count/percent unit, so switching that
  // selector changes the unit just as much as the shared metric does.
  return (
    props.ruleType === QualityRuleType.NULL_PERCENT &&
    props.nullMetric !== undefined &&
    savedNullMetric.value !== props.nullMetric
  );
});

async function load() {
  const rule = props.rule;
  const current = ++ticket;
  controller?.abort();
  if (!rule) {
    trend.value = undefined;
    return;
  }
  controller = new AbortController();
  try {
    const loaded = await getQualityRuleTrend(rule.name, {
      limit: TREND_LIMIT,
      signal: controller.signal,
    });
    if (current !== ticket) {
      return;
    }
    trend.value = loaded;
  } catch {
    // The sheet already reports an unreachable trend; a suggestion is optional,
    // so a failed read simply leaves the panel out rather than raising a second
    // error inside the form.
    if (current === ticket) {
      trend.value = undefined;
    }
  }
}

function apply() {
  const value = suggestion.value;
  if (!value || unitChanged.value || !value.thresholds) {
    return;
  }
  emit("apply", value);
}

watch(
  () => props.rule?.name,
  () => {
    void load();
  },
  { immediate: true }
);

onBeforeUnmount(() => {
  controller?.abort();
});
</script>
