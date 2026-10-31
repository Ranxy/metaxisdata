<template>
  <div class="grid gap-4 sm:grid-cols-3">
    <ColumnSelect
      :model-value="config.timestampColumn ?? ''"
      :columns="columnDetails"
      :id="fieldId('timestampColumn')"
      :required="requiredFor(ruleType, 'timestampColumn')"
      :error="errorFor(fieldErrors, 'timestampColumn')"
      :label="t('dataQuality.editor.timestampColumn')"
      :hint="t('dataQuality.editor.columnHint')"
      @update:model-value="(value) => (config.timestampColumn = String(value))"
    />
    <FormField
      v-model="config.maxLagSeconds"
      :id="fieldId('maxLagSeconds')"
      :required="requiredFor(ruleType, 'maxLagSeconds')"
      :error="errorFor(fieldErrors, 'maxLagSeconds')"
      type="number"
      :label="t('dataQuality.editor.maxLagSeconds')"
      :hint="t('dataQuality.editor.secondsHint')"
    />
    <FormField
      v-model="config.warnLagSeconds"
      :id="fieldId('warnLagSeconds')"
      :required="requiredFor(ruleType, 'warnLagSeconds')"
      :error="errorFor(fieldErrors, 'warnLagSeconds')"
      type="number"
      :label="t('dataQuality.editor.warnLagSeconds')"
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import FormField from "@/components/ui/form-field/FormField.vue";
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
</script>
