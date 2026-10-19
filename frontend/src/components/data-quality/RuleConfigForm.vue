<template>
  <div class="space-y-5">
    <!-- Shared by every threshold-carrying config: the unit the fail/warn values
         are read in. -->
    <div
      v-if="isCountRule"
      class="grid gap-4 sm:grid-cols-3"
    >
      <div class="flex flex-col gap-1.5">
        <Label>{{ t("dataQuality.editor.thresholdMetric") }}</Label>
        <Select v-model="thresholdMetricModel">
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem :value="String(QualityThresholdMetric.COUNT)">
              {{ t("dataQuality.thresholdMetric.count") }}
            </SelectItem>
            <SelectItem :value="String(QualityThresholdMetric.PERCENT)">
              {{ t("dataQuality.thresholdMetric.percent") }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <FormField
        v-model="config.failThreshold"
        type="number"
        :label="t('dataQuality.editor.failThreshold')"
        :hint="t('dataQuality.editor.thresholdHint')"
      />
      <FormField
        v-model="config.warnThreshold"
        type="number"
        :label="t('dataQuality.editor.warnThreshold')"
      />
    </div>

    <template v-if="ruleType === QualityRuleType.ROW_COUNT">
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField
          v-model="config.minRows"
          type="number"
          :label="t('dataQuality.editor.minRows')"
        />
        <FormField
          v-model="config.maxRows"
          type="number"
          :label="t('dataQuality.editor.maxRows')"
        />
        <FormField
          v-model="config.exactRows"
          type="number"
          :label="t('dataQuality.editor.exactRows')"
        />
      </div>
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField
          v-model="config.warnMinRows"
          type="number"
          :label="t('dataQuality.editor.warnMinRows')"
        />
        <FormField
          v-model="config.warnMaxRows"
          type="number"
          :label="t('dataQuality.editor.warnMaxRows')"
        />
      </div>
    </template>

    <div
      v-else-if="ruleType === QualityRuleType.ROW_COUNT_CHANGE"
      class="grid gap-4 sm:grid-cols-2"
    >
      <FormField
        v-model="config.maxIncreasePct"
        type="number"
        :label="t('dataQuality.editor.maxIncreasePct')"
        :hint="t('dataQuality.editor.percentHint')"
      />
      <FormField
        v-model="config.maxDecreasePct"
        type="number"
        :label="t('dataQuality.editor.maxDecreasePct')"
        :hint="t('dataQuality.editor.percentHint')"
      />
      <FormField
        v-model="config.warnIncreasePct"
        type="number"
        :label="t('dataQuality.editor.warnIncreasePct')"
      />
      <FormField
        v-model="config.warnDecreasePct"
        type="number"
        :label="t('dataQuality.editor.warnDecreasePct')"
      />
    </div>

    <template v-else-if="ruleType === QualityRuleType.NULL_PERCENT">
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField
          v-model="config.nullColumn"
          list="dq-target-columns"
          :label="t('dataQuality.editor.column')"
          :hint="t('dataQuality.editor.columnHint')"
        />
        <div class="flex flex-col gap-1.5">
          <Label>{{ t("dataQuality.editor.nullMetric") }}</Label>
          <Select v-model="config.nullMetric">
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="percent">
                {{ t("dataQuality.editor.nullMetricPercent") }}
              </SelectItem>
              <SelectItem value="count">
                {{ t("dataQuality.editor.nullMetricCount") }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
      <div
        v-if="config.nullMetric === 'count'"
        class="grid gap-4 sm:grid-cols-2"
      >
        <FormField
          v-model="config.maxNullCount"
          type="number"
          :label="t('dataQuality.editor.maxNullCount')"
        />
        <FormField
          v-model="config.warnNullCount"
          type="number"
          :label="t('dataQuality.editor.warnNullCount')"
        />
      </div>
      <div
        v-else
        class="grid gap-4 sm:grid-cols-2"
      >
        <FormField
          v-model="config.maxNullPct"
          type="number"
          :label="t('dataQuality.editor.maxNullPct')"
          :hint="t('dataQuality.editor.percentHint')"
        />
        <FormField
          v-model="config.warnNullPct"
          type="number"
          :label="t('dataQuality.editor.warnNullPct')"
        />
      </div>
    </template>

    <template v-else-if="ruleType === QualityRuleType.UNIQUE">
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField
          v-model="config.column"
          list="dq-target-columns"
          :label="t('dataQuality.editor.column')"
          :hint="t('dataQuality.editor.columnHint')"
        />
        <CheckboxField
          v-model="config.countNulls"
          :label="t('dataQuality.editor.countNulls')"
        />
      </div>
    </template>

    <template v-else-if="ruleType === QualityRuleType.UNIQUE_COMBINATION">
      <FormField
        v-model="config.columns"
        :label="t('dataQuality.editor.columns')"
        :hint="t('dataQuality.editor.columnsHint')"
        :disabled="config.allColumns === true"
      />
      <div class="grid gap-4 sm:grid-cols-2">
        <CheckboxField
          v-model="config.allColumns"
          :label="t('dataQuality.editor.allColumns')"
        />
        <CheckboxField
          v-model="config.countNulls"
          :label="t('dataQuality.editor.countNulls')"
        />
      </div>
    </template>

    <template v-else-if="ruleType === QualityRuleType.ACCEPTED_VALUES">
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField
          v-model="config.column"
          list="dq-target-columns"
          :label="t('dataQuality.editor.column')"
          :hint="t('dataQuality.editor.columnHint')"
        />
        <CheckboxField
          v-model="config.allowNull"
          :label="t('dataQuality.editor.allowNull')"
        />
      </div>
      <FormField
        v-model="config.values"
        :label="t('dataQuality.editor.values')"
        :hint="t('dataQuality.editor.valuesHint')"
      />
    </template>

    <template v-else-if="ruleType === QualityRuleType.RANGE">
      <div class="grid gap-4 sm:grid-cols-3">
        <FormField
          v-model="config.column"
          list="dq-target-columns"
          :label="t('dataQuality.editor.column')"
          :hint="t('dataQuality.editor.columnHint')"
        />
        <FormField
          v-model="config.minValue"
          :label="t('dataQuality.editor.minValue')"
        />
        <FormField
          v-model="config.maxValue"
          :label="t('dataQuality.editor.maxValue')"
        />
      </div>
      <CheckboxField
        v-model="config.inclusive"
        :label="t('dataQuality.editor.inclusive')"
      />
    </template>

    <div
      v-else-if="ruleType === QualityRuleType.REGEX"
      class="grid gap-4 sm:grid-cols-2"
    >
      <FormField
        v-model="config.column"
        :label="t('dataQuality.editor.column')"
        :hint="t('dataQuality.editor.columnHint')"
      />
      <FormField
        v-model="config.pattern"
        :label="t('dataQuality.editor.pattern')"
        :hint="t('dataQuality.editor.patternHint')"
      />
    </div>

    <div
      v-else-if="ruleType === QualityRuleType.STRING_LENGTH"
      class="grid gap-4 sm:grid-cols-3"
    >
      <FormField
        v-model="config.column"
        :label="t('dataQuality.editor.column')"
        :hint="t('dataQuality.editor.columnHint')"
      />
      <FormField
        v-model="config.minLen"
        type="number"
        :label="t('dataQuality.editor.minLen')"
      />
      <FormField
        v-model="config.maxLen"
        type="number"
        :label="t('dataQuality.editor.maxLen')"
      />
    </div>

    <template
      v-else-if="ruleType === QualityRuleType.REFERENTIAL_INTEGRITY"
    >
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField
          v-model="config.childColumn"
          list="dq-target-columns"
          :label="t('dataQuality.editor.childColumn')"
          :hint="t('dataQuality.editor.columnHint')"
        />
        <FormField
          v-model="config.parentTable"
          :label="t('dataQuality.editor.parentTable')"
          :hint="t('dataQuality.editor.parentTableHint')"
        />
        <FormField
          v-model="config.parentColumn"
          list="dq-target-columns"
          :label="t('dataQuality.editor.parentColumn')"
        />
        <CheckboxField
          v-model="config.allowNull"
          :label="t('dataQuality.editor.allowNull')"
        />
      </div>
    </template>

    <template v-else-if="ruleType === QualityRuleType.CROSS_COLUMN">
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField
          v-model="config.leftColumn"
          list="dq-target-columns"
          :label="t('dataQuality.editor.leftColumn')"
          :hint="t('dataQuality.editor.columnHint')"
        />
        <div class="flex flex-col gap-1.5">
          <Label>{{ t("dataQuality.editor.crossOperator") }}</Label>
          <Select v-model="crossOperatorModel">
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="operator in crossOperators"
                :key="operator"
                :value="String(operator)"
              >
                {{ t(qualityComparisonOperatorKey(operator)) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <FormField
          v-model="config.rightColumn"
          list="dq-target-columns"
          :label="t('dataQuality.editor.rightColumn')"
        />
        <FormField
          v-model="config.tolerance"
          type="number"
          :label="t('dataQuality.editor.tolerance')"
          :hint="t('dataQuality.editor.toleranceHint')"
        />
      </div>
    </template>

    <div
      v-else-if="ruleType === QualityRuleType.FRESHNESS"
      class="grid gap-4 sm:grid-cols-3"
    >
      <FormField
        v-model="config.timestampColumn"
        :label="t('dataQuality.editor.timestampColumn')"
        :hint="t('dataQuality.editor.columnHint')"
      />
      <FormField
        v-model="config.maxLagSeconds"
        type="number"
        :label="t('dataQuality.editor.maxLagSeconds')"
        :hint="t('dataQuality.editor.secondsHint')"
      />
      <FormField
        v-model="config.warnLagSeconds"
        type="number"
        :label="t('dataQuality.editor.warnLagSeconds')"
      />
    </div>

    <template v-else-if="ruleType === QualityRuleType.CUSTOM_SQL">
      <div class="space-y-2">
        <Label for="dq-config-sql">{{ t("dataQuality.editor.sql") }}</Label>
        <textarea
          id="dq-config-sql"
          v-model="config.sql"
          class="min-h-64 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
          :placeholder="t('dataQuality.editor.sqlPlaceholder')"
        />
        <p class="text-sm text-muted-foreground">
          {{ t("dataQuality.editor.sqlHint") }}
        </p>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  QualityComparisonOperator,
  QualityRuleType,
  QualityThresholdMetric,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityComparisonOperatorKey } from "@/utils/dataQuality";
import {
  isThresholdMetricType,
  type RuleConfigForm,
} from "@/utils/dataQualityForm";
import CheckboxField from "./CheckboxField.vue";

/**
 * The config fields of one rule type. The component is a controlled editor for
 * the nested `RuleConfigForm` object: the parent owns the state and the reset on
 * a rule-type change, so a stale arm can never travel with a new type.
 */

const props = defineProps<{
  ruleType: QualityRuleType;
}>();

const config = defineModel<RuleConfigForm>({ required: true });
const thresholdMetric = defineModel<QualityThresholdMetric>("thresholdMetric", {
  required: true,
});

const { t } = useI18n();

const isCountRule = computed(() => isThresholdMetricType(props.ruleType));

const crossOperators = [
  QualityComparisonOperator.EQ,
  QualityComparisonOperator.NE,
  QualityComparisonOperator.LT,
  QualityComparisonOperator.LTE,
  QualityComparisonOperator.GT,
  QualityComparisonOperator.GTE,
];

const crossOperatorModel = computed({
  get: () => String(config.value.crossOperator ?? QualityComparisonOperator.EQ),
  set: (value: string) => {
    config.value.crossOperator = Number(value) as QualityComparisonOperator;
  },
});

const thresholdMetricModel = computed({
  get: () => String(thresholdMetric.value),
  set: (value: string) => {
    thresholdMetric.value = Number(value) as QualityThresholdMetric;
  },
});
</script>
