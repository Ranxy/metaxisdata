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
                  <ColumnSelect
            :model-value="config.nullColumn ?? ''"
            :columns="columnDetails"
            :label="t('dataQuality.editor.column')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('nullColumn', value)"
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
      <FormField
        v-model="config.nullValues"
        :label="t('dataQuality.editor.nullValues')"
        :hint="t('dataQuality.editor.nullValuesHint')"
      />
    </template>

    <template v-else-if="ruleType === QualityRuleType.UNIQUE">
      <div class="grid gap-4 sm:grid-cols-2">
                  <ColumnSelect
            :model-value="config.column ?? ''"
            :columns="columnDetails"
            :label="t('dataQuality.editor.column')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('column', value)"
          />
        <CheckboxField
          v-model="config.countNulls"
          :label="t('dataQuality.editor.countNulls')"
        />
      </div>
    </template>

    <template v-else-if="ruleType === QualityRuleType.UNIQUE_COMBINATION">
      <ColumnSelect
        multiple
        :model-value="csvColumns(config.columns)"
        :columns="columnDetails"
        :disabled="config.allColumns === true"
        :label="t('dataQuality.editor.columns')"
        :hint="t('dataQuality.editor.columnsHint')"
        @update:model-value="(value) => setCsvColumns('columns', value)"
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
                  <ColumnSelect
            :model-value="config.column ?? ''"
            :columns="columnDetails"
            :label="t('dataQuality.editor.column')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('column', value)"
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
                  <ColumnSelect
            :model-value="config.column ?? ''"
            :columns="columnDetails"
            :label="t('dataQuality.editor.column')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('column', value)"
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
      <ColumnSelect
        :model-value="config.column ?? ''"
        :columns="columnDetails"
        :label="t('dataQuality.editor.column')"
        :hint="t('dataQuality.editor.columnHint')"
        @update:model-value="(value) => setColumn('column', value)"
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
      <ColumnSelect
        :model-value="config.column ?? ''"
        :columns="columnDetails"
        :label="t('dataQuality.editor.column')"
        :hint="t('dataQuality.editor.columnHint')"
        @update:model-value="(value) => setColumn('column', value)"
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
                  <ColumnSelect
            :model-value="config.childColumn ?? ''"
            :columns="columnDetails"
            :label="t('dataQuality.editor.childColumn')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('childColumn', value)"
          />
        <FormField
          v-model="config.parentTable"
          :label="t('dataQuality.editor.parentTable')"
          :hint="t('dataQuality.editor.parentTableHint')"
        />
                  <ColumnSelect
            :model-value="config.parentColumn ?? ''"
            :columns="columnDetails"
            :label="t('dataQuality.editor.parentColumn')"
            @update:model-value="(value) => setColumn('parentColumn', value)"
          />
        <CheckboxField
          v-model="config.allowNull"
          :label="t('dataQuality.editor.allowNull')"
        />
      </div>
    </template>

    <template v-else-if="ruleType === QualityRuleType.CROSS_COLUMN">
      <div class="grid gap-4 sm:grid-cols-2">
                  <ColumnSelect
            :model-value="config.leftColumn ?? ''"
            :columns="columnDetails"
            :label="t('dataQuality.editor.leftColumn')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('leftColumn', value)"
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
                  <ColumnSelect
            :model-value="config.rightColumn ?? ''"
            :columns="columnDetails"
            :label="t('dataQuality.editor.rightColumn')"
            @update:model-value="(value) => setColumn('rightColumn', value)"
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
      <ColumnSelect
        :model-value="config.timestampColumn ?? ''"
        :columns="columnDetails"
        :label="t('dataQuality.editor.timestampColumn')"
        :hint="t('dataQuality.editor.columnHint')"
        @update:model-value="(value) => setColumn('timestampColumn', value)"
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

    <template v-else-if="ruleType === QualityRuleType.SCHEMA_DRIFT">
      <div class="space-y-3">
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div class="space-y-1">
            <Label>{{ t("dataQuality.editor.expectedColumns") }}</Label>
            <p class="text-sm text-muted-foreground">
              {{ t("dataQuality.editor.expectedColumnsHint") }}
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            :disabled="columnDetails.length === 0"
            @click="fillExpectedColumns"
          >
            {{ t("dataQuality.editor.fillFromTarget") }}
          </Button>
        </div>
        <div
          v-for="(column, index) in config.expectedColumns ?? []"
          :key="index"
          class="grid items-center gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto_auto]"
        >
          <Input
            v-model="column.name"
            :aria-label="t('dataQuality.editor.columnName')"
            :placeholder="t('dataQuality.editor.columnName')"
          />
          <Input
            v-model="column.type"
            :aria-label="t('dataQuality.editor.columnType')"
            :placeholder="t('dataQuality.editor.columnType')"
          />
          <CheckboxField
            v-model="column.nullable"
            :label="t('dataQuality.editor.nullable')"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            class="h-8 w-8 text-muted-foreground hover:text-destructive"
            :aria-label="t('dataQuality.editor.removeColumn')"
            :title="t('dataQuality.editor.removeColumn')"
            @click="removeExpectedColumn(index)"
          >
            <Trash2 class="h-4 w-4" />
          </Button>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          @click="addExpectedColumn"
        >
          <Plus class="mr-1 h-4 w-4" />
          {{ t("dataQuality.editor.addColumn") }}
        </Button>
        <div class="grid gap-4 sm:grid-cols-3">
          <CheckboxField
            v-model="config.checkTypes"
            :label="t('dataQuality.editor.checkTypes')"
          />
          <CheckboxField
            v-model="config.checkOrder"
            :label="t('dataQuality.editor.checkOrder')"
          />
          <CheckboxField
            v-model="config.allowExtraColumns"
            :label="t('dataQuality.editor.allowExtraColumns')"
          />
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          <FormField
            v-model="config.failThreshold"
            type="number"
            :label="t('dataQuality.editor.failThreshold')"
          />
          <FormField
            v-model="config.warnThreshold"
            type="number"
            :label="t('dataQuality.editor.warnThreshold')"
          />
        </div>
      </div>
    </template>

    <div
      v-else-if="ruleType === QualityRuleType.GROUP_CHECK"
      class="grid gap-4 sm:grid-cols-3"
    >
      <ColumnSelect
        multiple
        :model-value="csvColumns(config.groupColumns)"
        :columns="columnDetails"
        :label="t('dataQuality.editor.groupColumns')"
        :hint="t('dataQuality.editor.groupColumnsHint')"
        @update:model-value="(value) => setCsvColumns('groupColumns', value)"
      />
      <FormField
        v-model="config.minRowsPerGroup"
        type="number"
        :label="t('dataQuality.editor.minRowsPerGroup')"
      />
      <FormField
        v-model="config.maxRowsPerGroup"
        type="number"
        :label="t('dataQuality.editor.maxRowsPerGroup')"
      />
    </div>

    <template v-else-if="ruleType === QualityRuleType.AGGREGATE">
      <div class="grid gap-4 sm:grid-cols-2">
                  <ColumnSelect
            :model-value="config.aggregateColumn ?? ''"
            :columns="columnDetails"
            :label="t('dataQuality.editor.column')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('aggregateColumn', value)"
          />
        <div class="flex flex-col gap-1.5">
          <Label>{{ t("dataQuality.editor.aggregateFunction") }}</Label>
          <Select v-model="aggregateFunctionModel">
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="fn in AGGREGATE_FUNCTIONS"
                :key="fn"
                :value="String(fn)"
              >
                {{ t(qualityAggregateFunctionKey(fn)) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <FormField
          v-if="aggregateFunctionModel === String(QualityAggregateFunction.PERCENTILE)"
          v-model="config.percentile"
          type="number"
          :label="t('dataQuality.editor.percentile')"
          :hint="t('dataQuality.editor.percentileHint')"
        />
        <FormField
          v-model="config.aggMin"
          type="number"
          :label="t('dataQuality.editor.minValue')"
        />
        <FormField
          v-model="config.aggMax"
          type="number"
          :label="t('dataQuality.editor.maxValue')"
        />
        <FormField
          v-model="config.aggWarnMin"
          type="number"
          :label="t('dataQuality.editor.warnMin')"
        />
        <FormField
          v-model="config.aggWarnMax"
          type="number"
          :label="t('dataQuality.editor.warnMax')"
        />
      </div>
      <CheckboxField
        v-model="config.aggInclusive"
        :label="t('dataQuality.editor.inclusive')"
      />
    </template>

    <div
      v-else-if="ruleType === QualityRuleType.CROSS_TABLE"
      class="grid gap-4 sm:grid-cols-2"
    >
      <FormField
        v-model="config.rightTable"
        class="sm:col-span-2"
        :label="t('dataQuality.editor.rightTable')"
        :hint="t('dataQuality.editor.rightTableHint')"
      />
      <div class="flex flex-col gap-1.5">
        <Label>{{ t("dataQuality.editor.leftAggregate") }}</Label>
        <Select
          :model-value="String(config.leftAggFunction ?? QualityAggregateFunction.COUNT)"
          @update:model-value="setLeftFunction"
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="fn in CROSS_TABLE_FUNCTIONS"
              :key="fn"
              :value="String(fn)"
            >
              {{ t(qualityAggregateFunctionKey(fn)) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <ColumnSelect
        :model-value="config.leftAggColumn ?? ''"
        :columns="columnDetails"
        :disabled="leftAggregateIsCount"
        :label="t('dataQuality.editor.leftColumn')"
        :hint="leftAggregateIsCount ? t('dataQuality.editor.countNeedsNoColumn') : t('dataQuality.editor.columnHint')"
        @update:model-value="(value) => setColumn('leftAggColumn', value)"
      />
      <div class="flex flex-col gap-1.5">
        <Label>{{ t("dataQuality.editor.rightAggregate") }}</Label>
        <Select
          :model-value="String(config.rightAggFunction ?? QualityAggregateFunction.COUNT)"
          @update:model-value="setRightFunction"
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="fn in CROSS_TABLE_FUNCTIONS"
              :key="fn"
              :value="String(fn)"
            >
              {{ t(qualityAggregateFunctionKey(fn)) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <ColumnSelect
        :model-value="config.rightAggColumn ?? ''"
        :columns="columnDetails"
        :disabled="rightAggregateIsCount"
        :label="t('dataQuality.editor.rightColumn')"
        :hint="rightAggregateIsCount ? t('dataQuality.editor.countNeedsNoColumn') : t('dataQuality.editor.rightColumnHint')"
        @update:model-value="(value) => setColumn('rightAggColumn', value)"
      />
      <FormField
        v-model="config.tolerance"
        type="number"
        class="sm:col-span-2"
        :label="t('dataQuality.editor.tolerance')"
        :hint="t('dataQuality.editor.toleranceHint')"
      />
    </div>
    <template v-else-if="ruleType === QualityRuleType.CEL_EXPRESSION">
      <div class="space-y-2">
        <Label for="dq-config-cel">
          {{ t("dataQuality.editor.celExpression") }}
        </Label>
        <textarea
          id="dq-config-cel"
          v-model="config.celExpression"
          class="min-h-48 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
          :placeholder="t('dataQuality.editor.celExpressionPlaceholder')"
        />
        <p class="text-sm text-muted-foreground">
          {{ t("dataQuality.editor.celExpressionHint") }}
        </p>
      </div>
      <div class="grid gap-4 sm:grid-cols-2">
        <ColumnSelect
          multiple
          :model-value="csvColumns(config.celColumns)"
          :columns="columnDetails"
          :label="t('dataQuality.editor.celColumns')"
          :hint="t('dataQuality.editor.celColumnsHint')"
          @update:model-value="(value) => setCsvColumns('celColumns', value)"
        />
        <FormField
          v-model="config.celSampleRows"
          type="number"
          :label="t('dataQuality.editor.celSampleRows')"
          :hint="t('dataQuality.editor.celSampleRowsHint')"
        />
      </div>
      <p class="text-xs text-muted-foreground">
        {{ t("dataQuality.editor.celFunctionsHint") }}
      </p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { Plus, Trash2 } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import { Button } from "@/components/ui/button";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  QualityAggregateFunction,
  QualityComparisonOperator,
  QualityRuleType,
  QualityThresholdMetric,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  qualityAggregateFunctionKey,
  qualityComparisonOperatorKey,
} from "@/utils/dataQuality";
import {
  AGGREGATE_FUNCTIONS,
  CROSS_TABLE_FUNCTIONS,
  isThresholdMetricType,
  type RuleConfigForm,
  type SchemaDriftColumnForm,
  type TargetColumnDetail,
} from "@/utils/dataQualityForm";
import CheckboxField from "./CheckboxField.vue";

/**
 * The config fields of one rule type. The component is a controlled editor for
 * the nested `RuleConfigForm` object: the parent owns the state and the reset on
 * a rule-type change, so a stale arm can never travel with a new type.
 */

const props = withDefaults(
  defineProps<{
    ruleType: QualityRuleType;
    /** Used by the schema-drift snapshot's "fill from target" action. */
    columnDetails?: TargetColumnDetail[];
  }>(),
  { columnDetails: () => [] }
);

const config = defineModel<RuleConfigForm>({ required: true });
const thresholdMetric = defineModel<QualityThresholdMetric>("thresholdMetric", {
  required: true,
});

const { t } = useI18n();

/** A CSV column field as the picker's array form. */
function csvColumns(value: string | undefined): string[] {
  return (value ?? "")
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean);
}

/** The picker's array back into the CSV field the config builder parses. */
function setCsvColumns(
  field: "columns" | "groupColumns" | "celColumns",
  value: string | string[]
) {
  config.value[field] = Array.isArray(value) ? value.join(", ") : value;
}

function setColumn(
  field:
    | "nullColumn"
    | "column"
    | "childColumn"
    | "parentColumn"
    | "leftColumn"
    | "rightColumn"
    | "timestampColumn"
    | "aggregateColumn"
    | "leftAggColumn"
    | "rightAggColumn",
  value: string | string[]
) {
  config.value[field] = String(value);
}

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

const aggregateFunctionModel = computed({
  get: () =>
    String(config.value.aggregateFunction ?? QualityAggregateFunction.SUM),
  set: (value: string) => {
    config.value.aggregateFunction = Number(value) as QualityAggregateFunction;
  },
});

const leftAggregateIsCount = computed(
  () =>
    (config.value.leftAggFunction ?? QualityAggregateFunction.COUNT) ===
    QualityAggregateFunction.COUNT
);
const rightAggregateIsCount = computed(
  () =>
    (config.value.rightAggFunction ?? QualityAggregateFunction.COUNT) ===
    QualityAggregateFunction.COUNT
);

// COUNT takes no column. Clearing it when the function becomes COUNT keeps the
// form and the compiled request in agreement; the server also rejects a stray.
function setLeftFunction(value: unknown) {
  const fn = Number(value) as QualityAggregateFunction;
  config.value.leftAggFunction = fn;
  if (fn === QualityAggregateFunction.COUNT) {
    config.value.leftAggColumn = "";
  }
}

function setRightFunction(value: unknown) {
  const fn = Number(value) as QualityAggregateFunction;
  config.value.rightAggFunction = fn;
  if (fn === QualityAggregateFunction.COUNT) {
    config.value.rightAggColumn = "";
  }
}

function addExpectedColumn() {
  config.value.expectedColumns = [
    ...(config.value.expectedColumns ?? []),
    { name: "", type: "", nullable: true },
  ];
}

function removeExpectedColumn(index: number) {
  config.value.expectedColumns = (config.value.expectedColumns ?? []).filter(
    (_, position) => position !== index
  );
}

function fillExpectedColumns() {
  config.value.expectedColumns = props.columnDetails.map(
    (column): SchemaDriftColumnForm => ({
      name: column.name,
      type: column.type,
      nullable: column.nullable,
    })
  );
}
</script>
