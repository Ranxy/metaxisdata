<template>
  <div class="grid gap-4 sm:grid-cols-2">
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
    <CheckboxField
      v-model="config.allowNull"
      :label="t('dataQuality.editor.allowNull')"
    />
  </div>
  <FormField
    v-model="config.values"
    :id="fieldId('values')"
    :required="requiredFor(ruleType, 'values')"
    :error="errorFor(fieldErrors, 'values')"
    :label="t('dataQuality.editor.values')"
    :hint="t('dataQuality.editor.valuesHint')"
  />
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import FormField from "@/components/ui/form-field/FormField.vue";
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
