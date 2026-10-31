<template>
  <div class="grid gap-4 sm:grid-cols-3">
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
      v-model="config.minValue"
      :id="fieldId('minValue')"
      :required="requiredFor(ruleType, 'minValue')"
      :error="errorFor(fieldErrors, 'minValue')"
      :label="t('dataQuality.editor.minValue')"
    />
    <FormField
      v-model="config.maxValue"
      :id="fieldId('maxValue')"
      :required="requiredFor(ruleType, 'maxValue')"
      :error="errorFor(fieldErrors, 'maxValue')"
      :label="t('dataQuality.editor.maxValue')"
    />
  </div>
  <CheckboxField
    v-model="config.inclusive"
    :label="t('dataQuality.editor.inclusive')"
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
