<template>
  <template v-if="ruleType === QualityRuleType.UNIQUE">
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
        v-model="config.countNulls"
        :label="t('dataQuality.editor.countNulls')"
      />
    </div>
  </template>
  <template v-else>
    <ColumnSelect
      multiple
      :model-value="csvColumns(config.columns)"
      :columns="columnDetails"
      :id="fieldId('columns')"
      :required="requiredFor(ruleType, 'columns')"
      :error="errorFor(fieldErrors, 'columns')"
      :disabled="config.allColumns === true"
      :label="t('dataQuality.editor.columns')"
      :hint="t('dataQuality.editor.columnsHint')"
      @update:model-value="(value) => (config.columns = joinCsvColumns(value))"
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
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import { QualityRuleType } from "@/types/proto-es/v1/data_quality_service_pb";
import type { RuleConfigForm } from "@/utils/dataQualityForm";
import CheckboxField from "../CheckboxField.vue";
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
