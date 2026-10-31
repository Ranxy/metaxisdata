<template>
  <div class="grid gap-4 sm:grid-cols-2">
    <ColumnSelect
      :model-value="config.leftColumn ?? ''"
      :columns="columnDetails"
      :id="fieldId('leftColumn')"
      :required="requiredFor(ruleType, 'leftColumn')"
      :error="errorFor(fieldErrors, 'leftColumn')"
      :label="t('dataQuality.editor.leftColumn')"
      :hint="t('dataQuality.editor.columnHint')"
      @update:model-value="(value) => (config.leftColumn = String(value))"
    />
    <div class="flex flex-col gap-1.5">
      <Label>{{ t("dataQuality.editor.crossOperator") }}</Label>
      <Select v-model="crossOperatorModel">
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="operator in CROSS_OPERATORS"
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
      :id="fieldId('rightColumn')"
      :required="requiredFor(ruleType, 'rightColumn')"
      :error="errorFor(fieldErrors, 'rightColumn')"
      :label="t('dataQuality.editor.rightColumn')"
      @update:model-value="(value) => (config.rightColumn = String(value))"
    />
    <FormField
      v-model="config.tolerance"
      :id="fieldId('tolerance')"
      :required="requiredFor(ruleType, 'tolerance')"
      :error="errorFor(fieldErrors, 'tolerance')"
      type="number"
      :label="t('dataQuality.editor.tolerance')"
      :hint="t('dataQuality.editor.toleranceHint')"
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
import { QualityComparisonOperator } from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityComparisonOperatorKey } from "@/utils/dataQuality";
import type { RuleConfigForm } from "@/utils/dataQualityForm";
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

const CROSS_OPERATORS = [
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
</script>
