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
    <div class="flex flex-col gap-1.5">
      <Label for="dq-field-standard">
        {{ t("dataQuality.editor.codeStandard") }}
        <span class="text-destructive">*</span>
      </Label>
      <Select
        :model-value="String(config.standard ?? QualityStandardCode.UNSPECIFIED)"
        @update:model-value="setStandard"
      >
        <SelectTrigger
          id="dq-field-standard"
          :aria-required="true"
          :aria-invalid="errorFor(fieldErrors, 'standard') ? true : undefined"
        >
          <SelectValue
            :placeholder="t('dataQuality.editor.codeStandardPlaceholder')"
          />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="option in standardOptions"
            :key="option.value"
            :value="String(option.value)"
          >
            {{ t(option.key) }}
          </SelectItem>
        </SelectContent>
      </Select>
      <p
        v-if="errorFor(fieldErrors, 'standard')"
        class="text-sm text-destructive"
      >
        {{ errorFor(fieldErrors, "standard") }}
      </p>
      <p
        v-else
        class="text-sm text-muted-foreground"
      >
        {{ t("dataQuality.editor.codeStandardHint") }}
      </p>
    </div>
  </div>
  <div class="grid gap-4 sm:grid-cols-2">
    <!-- The sample bound is the check's own, like a CEL expression's: it is not
             the shared execution sampling control. -->
    <FormField
      v-model="config.sampleRows"
      :id="fieldId('sampleRows')"
      :required="requiredFor(ruleType, 'sampleRows')"
      :error="errorFor(fieldErrors, 'sampleRows')"
      type="number"
      :label="t('dataQuality.editor.sampleRows')"
      :hint="t('dataQuality.editor.sampleRowsHint')"
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
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { QualityStandardCode } from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityStandardCodeKey } from "@/utils/dataQuality";
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

const standardOptions = [
  QualityStandardCode.UNIFIED_SOCIAL_CREDIT_CODE,
  QualityStandardCode.ORGANIZATION_CODE,
  QualityStandardCode.RESIDENT_ID_CARD,
  QualityStandardCode.ADMINISTRATIVE_DIVISION,
  QualityStandardCode.INDUSTRY_CLASSIFICATION,
].map((value) => ({ value, key: qualityStandardCodeKey(value) }));

function setStandard(value: unknown) {
  config.value.standard = Number(value) as QualityStandardCode;
}
</script>
