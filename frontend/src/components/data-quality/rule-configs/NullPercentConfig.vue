<template>
  <div class="grid gap-4 sm:grid-cols-2">
    <ColumnSelect
      :model-value="config.nullColumn ?? ''"
      :columns="columnDetails"
      :id="fieldId('nullColumn')"
      :required="requiredFor(ruleType, 'nullColumn')"
      :error="errorFor(fieldErrors, 'nullColumn')"
      :label="t('dataQuality.editor.column')"
      :hint="t('dataQuality.editor.columnHint')"
      @update:model-value="(value) => (config.nullColumn = String(value))"
    />
    <div class="flex flex-col gap-1.5">
      <Label>{{ t("dataQuality.editor.nullMetric") }}</Label>
      <Select v-model="config.nullMetric">
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="percent">
            {{ t("dataQuality.editor.nullMetricPercent") }}
          </SelectItem>
          <SelectItem value="count">
            {{ t("dataQuality.editor.nullMetricCount") }}
          </SelectItem>
        </SelectContent>
      </Select>
    </div>
  </div>
  <div
    v-if="config.nullMetric === 'count'"
    class="grid gap-4 sm:grid-cols-2"
  >
    <FormField
      v-model="config.maxNullCount"
      :id="fieldId('maxNullCount')"
      :required="requiredFor(ruleType, 'maxNullCount')"
      :error="errorFor(fieldErrors, 'maxNullCount')"
      type="number"
      :label="t('dataQuality.editor.maxNullCount')"
    />
    <FormField
      v-model="config.warnNullCount"
      :id="fieldId('warnNullCount')"
      :required="requiredFor(ruleType, 'warnNullCount')"
      :error="errorFor(fieldErrors, 'warnNullCount')"
      type="number"
      :label="t('dataQuality.editor.warnNullCount')"
    />
  </div>
  <div
    v-else
    class="grid gap-4 sm:grid-cols-2"
  >
    <FormField
      v-model="config.maxNullPct"
      :id="fieldId('maxNullPct')"
      :required="requiredFor(ruleType, 'maxNullPct')"
      :error="errorFor(fieldErrors, 'maxNullPct')"
      type="number"
      :label="t('dataQuality.editor.maxNullPct')"
      :hint="t('dataQuality.editor.percentHint')"
    />
    <FormField
      v-model="config.warnNullPct"
      :id="fieldId('warnNullPct')"
      :required="requiredFor(ruleType, 'warnNullPct')"
      :error="errorFor(fieldErrors, 'warnNullPct')"
      type="number"
      :label="t('dataQuality.editor.warnNullPct')"
    />
  </div>
  <FormField
    v-model="config.nullValues"
    :id="fieldId('nullValues')"
    :required="requiredFor(ruleType, 'nullValues')"
    :error="errorFor(fieldErrors, 'nullValues')"
    :label="t('dataQuality.editor.nullValues')"
    :hint="t('dataQuality.editor.nullValuesHint')"
  />
</template>

<script setup lang="ts">
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
