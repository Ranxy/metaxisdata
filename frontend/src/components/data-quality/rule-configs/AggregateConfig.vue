<template>
  <div class="grid gap-4 sm:grid-cols-2">
    <ColumnSelect
      :model-value="config.aggregateColumn ?? ''"
      :columns="columnDetails"
      :id="fieldId('aggregateColumn')"
      :required="requiredFor(ruleType, 'aggregateColumn')"
      :error="errorFor(fieldErrors, 'aggregateColumn')"
      :label="t('dataQuality.editor.column')"
      :hint="t('dataQuality.editor.columnHint')"
      @update:model-value="(value) => (config.aggregateColumn = String(value))"
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
      :id="fieldId('aggMin')"
      :required="requiredFor(ruleType, 'aggMin')"
      :error="errorFor(fieldErrors, 'aggMin')"
      type="number"
      :label="t('dataQuality.editor.minValue')"
    />
    <FormField
      v-model="config.aggMax"
      :id="fieldId('aggMax')"
      :required="requiredFor(ruleType, 'aggMax')"
      :error="errorFor(fieldErrors, 'aggMax')"
      type="number"
      :label="t('dataQuality.editor.maxValue')"
    />
    <FormField
      v-model="config.aggWarnMin"
      :id="fieldId('aggWarnMin')"
      :required="requiredFor(ruleType, 'aggWarnMin')"
      :error="errorFor(fieldErrors, 'aggWarnMin')"
      type="number"
      :label="t('dataQuality.editor.warnMin')"
    />
    <FormField
      v-model="config.aggWarnMax"
      :id="fieldId('aggWarnMax')"
      :required="requiredFor(ruleType, 'aggWarnMax')"
      :error="errorFor(fieldErrors, 'aggWarnMax')"
      type="number"
      :label="t('dataQuality.editor.warnMax')"
    />
  </div>
  <CheckboxField
    v-model="config.aggInclusive"
    :label="t('dataQuality.editor.inclusive')"
  />
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { QualityAggregateFunction } from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityAggregateFunctionKey } from "@/utils/dataQuality";
import {
  AGGREGATE_FUNCTIONS,
  type RuleConfigForm,
} from "@/utils/dataQualityForm";
import CheckboxField from "../CheckboxField.vue";
import {
  errorFor,
  fieldId,
  type RuleConfigSectionProps,
  requiredFor,
} from "./ruleConfigField";

withDefaults(defineProps<RuleConfigSectionProps>(), {
  columnDetails: () => [],
  fieldErrors: () => ({}),
});

const config = defineModel<RuleConfigForm>({ required: true });
const { t } = useI18n();

const aggregateFunctionModel = computed({
  get: () =>
    String(config.value.aggregateFunction ?? QualityAggregateFunction.SUM),
  set: (value: string) => {
    config.value.aggregateFunction = Number(value) as QualityAggregateFunction;
  },
});
</script>
