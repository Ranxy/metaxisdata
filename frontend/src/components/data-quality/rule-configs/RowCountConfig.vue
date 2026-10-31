<template>
  <template v-if="ruleType === QualityRuleType.ROW_COUNT">
    <div class="grid gap-4 sm:grid-cols-2">
      <FormField
        v-model="config.minRows"
        :id="fieldId('minRows')"
        :required="requiredFor(ruleType, 'minRows')"
        :error="errorFor(fieldErrors, 'minRows')"
        type="number"
        :label="t('dataQuality.editor.minRows')"
      />
      <FormField
        v-model="config.maxRows"
        :id="fieldId('maxRows')"
        :required="requiredFor(ruleType, 'maxRows')"
        :error="errorFor(fieldErrors, 'maxRows')"
        type="number"
        :label="t('dataQuality.editor.maxRows')"
      />
      <FormField
        v-model="config.exactRows"
        :id="fieldId('exactRows')"
        :required="requiredFor(ruleType, 'exactRows')"
        :error="errorFor(fieldErrors, 'exactRows')"
        type="number"
        :label="t('dataQuality.editor.exactRows')"
      />
    </div>
    <div class="grid gap-4 sm:grid-cols-2">
      <FormField
        v-model="config.warnMinRows"
        :id="fieldId('warnMinRows')"
        :required="requiredFor(ruleType, 'warnMinRows')"
        :error="errorFor(fieldErrors, 'warnMinRows')"
        type="number"
        :label="t('dataQuality.editor.warnMinRows')"
      />
      <FormField
        v-model="config.warnMaxRows"
        :id="fieldId('warnMaxRows')"
        :required="requiredFor(ruleType, 'warnMaxRows')"
        :error="errorFor(fieldErrors, 'warnMaxRows')"
        type="number"
        :label="t('dataQuality.editor.warnMaxRows')"
      />
    </div>
  </template>
  <div
    v-else
    class="grid gap-4 sm:grid-cols-2"
  >
    <FormField
      v-model="config.maxIncreasePct"
      :id="fieldId('maxIncreasePct')"
      :required="requiredFor(ruleType, 'maxIncreasePct')"
      :error="errorFor(fieldErrors, 'maxIncreasePct')"
      type="number"
      :label="t('dataQuality.editor.maxIncreasePct')"
      :hint="t('dataQuality.editor.percentHint')"
    />
    <FormField
      v-model="config.maxDecreasePct"
      :id="fieldId('maxDecreasePct')"
      :required="requiredFor(ruleType, 'maxDecreasePct')"
      :error="errorFor(fieldErrors, 'maxDecreasePct')"
      type="number"
      :label="t('dataQuality.editor.maxDecreasePct')"
      :hint="t('dataQuality.editor.percentHint')"
    />
    <FormField
      v-model="config.warnIncreasePct"
      :id="fieldId('warnIncreasePct')"
      :required="requiredFor(ruleType, 'warnIncreasePct')"
      :error="errorFor(fieldErrors, 'warnIncreasePct')"
      type="number"
      :label="t('dataQuality.editor.warnIncreasePct')"
    />
    <FormField
      v-model="config.warnDecreasePct"
      :id="fieldId('warnDecreasePct')"
      :required="requiredFor(ruleType, 'warnDecreasePct')"
      :error="errorFor(fieldErrors, 'warnDecreasePct')"
      type="number"
      :label="t('dataQuality.editor.warnDecreasePct')"
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import FormField from "@/components/ui/form-field/FormField.vue";
import { QualityRuleType } from "@/types/proto-es/v1/data_quality_service_pb";
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
