<template>
  <div class="grid gap-4 sm:grid-cols-3">
    <ColumnSelect
      multiple
      :model-value="csvColumns(config.segmentColumns)"
      :columns="columnDetails"
      :id="fieldId('segmentColumns')"
      :required="requiredFor(ruleType, 'segmentColumns')"
      :error="errorFor(fieldErrors, 'segmentColumns')"
      :label="t('dataQuality.editor.segmentColumns')"
      :hint="t('dataQuality.editor.segmentColumnsHint')"
      @update:model-value="
        (value) => (config.segmentColumns = joinCsvColumns(value))
      "
    />
    <FormField
      v-model="config.segmentBaselineRuns"
      :id="fieldId('segmentBaselineRuns')"
      :required="requiredFor(ruleType, 'segmentBaselineRuns')"
      :error="errorFor(fieldErrors, 'segmentBaselineRuns')"
      type="number"
      :label="t('dataQuality.editor.segmentBaselineRuns')"
      :hint="t('dataQuality.editor.segmentBaselineRunsHint')"
    />
    <FormField
      v-model="config.segmentMinOccurrences"
      :id="fieldId('segmentMinOccurrences')"
      :required="requiredFor(ruleType, 'segmentMinOccurrences')"
      :error="errorFor(fieldErrors, 'segmentMinOccurrences')"
      type="number"
      :label="t('dataQuality.editor.segmentMinOccurrences')"
      :hint="t('dataQuality.editor.segmentMinOccurrencesHint')"
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
