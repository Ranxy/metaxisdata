<template>
  <div class="grid gap-4 sm:grid-cols-3">
    <ColumnSelect
      multiple
      :model-value="csvColumns(config.groupColumns)"
      :columns="columnDetails"
      :id="fieldId('groupColumns')"
      :required="requiredFor(ruleType, 'groupColumns')"
      :error="errorFor(fieldErrors, 'groupColumns')"
      :label="t('dataQuality.editor.groupColumns')"
      :hint="t('dataQuality.editor.groupColumnsHint')"
      @update:model-value="
        (value) => (config.groupColumns = joinCsvColumns(value))
      "
    />
    <FormField
      v-model="config.minRowsPerGroup"
      :id="fieldId('minRowsPerGroup')"
      :required="requiredFor(ruleType, 'minRowsPerGroup')"
      :error="errorFor(fieldErrors, 'minRowsPerGroup')"
      type="number"
      :label="t('dataQuality.editor.minRowsPerGroup')"
    />
    <FormField
      v-model="config.maxRowsPerGroup"
      :id="fieldId('maxRowsPerGroup')"
      :required="requiredFor(ruleType, 'maxRowsPerGroup')"
      :error="errorFor(fieldErrors, 'maxRowsPerGroup')"
      type="number"
      :label="t('dataQuality.editor.maxRowsPerGroup')"
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import FormField from "@/components/ui/form-field/FormField.vue";
import type { RuleConfigForm } from "@/utils/dataQualityForm";
import {
  csvColumns,
  errorFor,
  fieldId,
  joinCsvColumns,
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
