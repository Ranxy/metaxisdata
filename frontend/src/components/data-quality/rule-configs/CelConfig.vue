<template>
  <div class="space-y-2">
    <Label for="dq-config-cel">
      {{ t("dataQuality.editor.celExpression") }}
      <span class="text-destructive">*</span>
    </Label>
    <textarea
      id="dq-config-cel"
      v-model="config.celExpression"
      class="min-h-48 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
      :aria-required="true"
      :aria-invalid="errorFor(fieldErrors, 'celExpression') ? true : undefined"
      :class="errorFor(fieldErrors, 'celExpression') ? 'border-destructive' : ''"
      :placeholder="t('dataQuality.editor.celExpressionPlaceholder')"
    />
    <p
      v-if="errorFor(fieldErrors, 'celExpression')"
      class="text-sm text-destructive"
    >
      {{ errorFor(fieldErrors, "celExpression") }}
    </p>
    <p
      v-else
      class="text-sm text-muted-foreground"
    >
      {{ t("dataQuality.editor.celExpressionHint") }}
    </p>
  </div>
  <div class="grid gap-4 sm:grid-cols-2">
    <ColumnSelect
      multiple
      :model-value="csvColumns(config.celColumns)"
      :columns="columnDetails"
      :id="fieldId('celColumns')"
      :required="requiredFor(ruleType, 'celColumns')"
      :error="errorFor(fieldErrors, 'celColumns')"
      :label="t('dataQuality.editor.celColumns')"
      :hint="t('dataQuality.editor.celColumnsHint')"
      @update:model-value="(value) => (config.celColumns = joinCsvColumns(value))"
    />
    <FormField
      v-model="config.celSampleRows"
      :id="fieldId('celSampleRows')"
      :required="requiredFor(ruleType, 'celSampleRows')"
      :error="errorFor(fieldErrors, 'celSampleRows')"
      type="number"
      :label="t('dataQuality.editor.celSampleRows')"
      :hint="t('dataQuality.editor.celSampleRowsHint')"
    />
  </div>
  <p class="text-xs text-muted-foreground">
    {{ t("dataQuality.editor.celFunctionsHint") }}
  </p>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Label } from "@/components/ui/label";
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
