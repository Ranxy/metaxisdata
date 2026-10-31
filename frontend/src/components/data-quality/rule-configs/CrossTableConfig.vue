<template>
  <div class="grid gap-4 sm:grid-cols-2">
    <FormField
      v-model="config.rightTable"
      :id="fieldId('rightTable')"
      :required="requiredFor(ruleType, 'rightTable')"
      :error="errorFor(fieldErrors, 'rightTable')"
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
      :id="fieldId('leftAggColumn')"
      :required="requiredFor(ruleType, 'leftAggColumn')"
      :error="errorFor(fieldErrors, 'leftAggColumn')"
      :disabled="leftAggregateIsCount"
      :label="t('dataQuality.editor.leftColumn')"
      :hint="
        leftAggregateIsCount
          ? t('dataQuality.editor.countNeedsNoColumn')
          : t('dataQuality.editor.columnHint')
      "
      @update:model-value="(value) => (config.leftAggColumn = String(value))"
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
      :id="fieldId('rightAggColumn')"
      :required="requiredFor(ruleType, 'rightAggColumn')"
      :error="errorFor(fieldErrors, 'rightAggColumn')"
      :disabled="rightAggregateIsCount"
      :label="t('dataQuality.editor.rightColumn')"
      :hint="
        rightAggregateIsCount
          ? t('dataQuality.editor.countNeedsNoColumn')
          : t('dataQuality.editor.rightColumnHint')
      "
      @update:model-value="(value) => (config.rightAggColumn = String(value))"
    />
    <FormField
      v-model="config.tolerance"
      :id="fieldId('tolerance')"
      :required="requiredFor(ruleType, 'tolerance')"
      :error="errorFor(fieldErrors, 'tolerance')"
      type="number"
      class="sm:col-span-2"
      :label="t('dataQuality.editor.tolerance')"
      :hint="t('dataQuality.editor.toleranceHint')"
    />
    <FormField
      v-model="config.failThreshold"
      :id="fieldId('failThreshold')"
      :required="requiredFor(ruleType, 'failThreshold')"
      :error="errorFor(fieldErrors, 'failThreshold')"
      type="number"
      :label="t('dataQuality.editor.failThreshold')"
    />
    <FormField
      v-model="config.warnThreshold"
      :id="fieldId('warnThreshold')"
      :required="requiredFor(ruleType, 'warnThreshold')"
      :error="errorFor(fieldErrors, 'warnThreshold')"
      type="number"
      :label="t('dataQuality.editor.warnThreshold')"
    />
  </div>
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
  CROSS_TABLE_FUNCTIONS,
  type RuleConfigForm,
} from "@/utils/dataQualityForm";
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
</script>
