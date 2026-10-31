<template>
  <div
    v-if="ruleType === QualityRuleType.REGEX"
    class="grid gap-4 sm:grid-cols-2"
  >
    <ColumnSelect
      :model-value="config.column ?? ''"
      :columns="columnDetails"
      :id="fieldId('column')"
      :required="requiredFor(ruleType, 'column')"
      :error="errorFor(fieldErrors, 'column')"
      :label="t('dataQuality.editor.column')"
      :hint="t('dataQuality.editor.columnHint')"
      @update:model-value="(value) => (config.column = String(value))"
    />
    <FormField
      v-model="config.pattern"
      :id="fieldId('pattern')"
      :required="requiredFor(ruleType, 'pattern')"
      :error="errorFor(fieldErrors, 'pattern')"
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
      :id="fieldId('column')"
      :required="requiredFor(ruleType, 'column')"
      :error="errorFor(fieldErrors, 'column')"
      :label="t('dataQuality.editor.column')"
      :hint="t('dataQuality.editor.columnHint')"
      @update:model-value="(value) => (config.column = String(value))"
    />
    <FormField
      v-model="config.minLen"
      :id="fieldId('minLen')"
      :required="requiredFor(ruleType, 'minLen')"
      :error="errorFor(fieldErrors, 'minLen')"
      type="number"
      :label="t('dataQuality.editor.minLen')"
    />
    <FormField
      v-model="config.maxLen"
      :id="fieldId('maxLen')"
      :required="requiredFor(ruleType, 'maxLen')"
      :error="errorFor(fieldErrors, 'maxLen')"
      type="number"
      :label="t('dataQuality.editor.maxLen')"
    />
  </div>
  <div
    v-else
    class="grid gap-4 sm:grid-cols-2"
  >
    <ColumnSelect
      :model-value="config.childColumn ?? ''"
      :columns="columnDetails"
      :id="fieldId('childColumn')"
      :required="requiredFor(ruleType, 'childColumn')"
      :error="errorFor(fieldErrors, 'childColumn')"
      :label="t('dataQuality.editor.childColumn')"
      :hint="t('dataQuality.editor.columnHint')"
      @update:model-value="(value) => (config.childColumn = String(value))"
    />
    <FormField
      v-model="config.parentTable"
      :id="fieldId('parentTable')"
      :required="requiredFor(ruleType, 'parentTable')"
      :error="errorFor(fieldErrors, 'parentTable')"
      :label="t('dataQuality.editor.parentTable')"
      :hint="t('dataQuality.editor.parentTableHint')"
    />
    <ColumnSelect
      :model-value="config.parentColumn ?? ''"
      :columns="columnDetails"
      :id="fieldId('parentColumn')"
      :required="requiredFor(ruleType, 'parentColumn')"
      :error="errorFor(fieldErrors, 'parentColumn')"
      :label="t('dataQuality.editor.parentColumn')"
      @update:model-value="(value) => (config.parentColumn = String(value))"
    />
    <CheckboxField
      v-model="config.allowNull"
      :label="t('dataQuality.editor.allowNull')"
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import FormField from "@/components/ui/form-field/FormField.vue";
import { QualityRuleType } from "@/types/proto-es/v1/data_quality_service_pb";
import type { RuleConfigForm } from "@/utils/dataQualityForm";
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
</script>
