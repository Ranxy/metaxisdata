<template>
  <div class="space-y-4">
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
        <Label for="dq-field-acceptedSource">
          {{ t("dataQuality.editor.acceptedSource") }}
        </Label>
        <Select v-model="sourceModel">
          <SelectTrigger id="dq-field-acceptedSource">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="option in SOURCE_OPTIONS"
              :key="option.value"
              :value="option.value"
            >
              {{ t(option.labelKey) }}
            </SelectItem>
          </SelectContent>
        </Select>
        <p class="text-sm text-muted-foreground">
          {{ t("dataQuality.editor.acceptedSourceHint") }}
        </p>
      </div>
    </div>

    <!-- Exactly one source is sent: the backend refuses a config that names
         several, so the fields of the unselected sources are not rendered. -->
    <FormField
      v-if="source === 'list'"
      v-model="config.values"
      :id="fieldId('values')"
      :required="source === 'list'"
      :error="errorFor(fieldErrors, 'values')"
      :label="t('dataQuality.editor.values')"
      :hint="t('dataQuality.editor.valuesHint')"
    />
    <ColumnSelect
      v-else-if="source === 'field'"
      :model-value="config.acceptedField ?? ''"
      :columns="referenceColumns"
      :id="fieldId('acceptedField')"
      :required="true"
      :error="errorFor(fieldErrors, 'acceptedField')"
      :label="t('dataQuality.editor.acceptedField')"
      :hint="t('dataQuality.editor.acceptedFieldHint')"
      @update:model-value="
        (value) => (config.acceptedField = String(value))
      "
    />
    <div
      v-else
      class="space-y-4"
    >
      <div class="space-y-2">
        <Label for="dq-field-acceptedQuery">
          {{ t("dataQuality.editor.acceptedQuery") }}
          <span class="text-destructive">*</span>
        </Label>
        <textarea
          id="dq-field-acceptedQuery"
          v-model="config.acceptedQuery"
          class="min-h-32 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
          :aria-required="true"
          :aria-invalid="
            errorFor(fieldErrors, 'acceptedQuery') ? true : undefined
          "
          :class="
            errorFor(fieldErrors, 'acceptedQuery')
              ? 'border-destructive'
              : ''
          "
          :placeholder="t('dataQuality.editor.acceptedQueryPlaceholder')"
        />
        <p
          v-if="errorFor(fieldErrors, 'acceptedQuery')"
          class="text-sm text-destructive"
        >
          {{ errorFor(fieldErrors, "acceptedQuery") }}
        </p>
        <p
          v-else
          class="text-sm text-muted-foreground"
        >
          {{ t("dataQuality.editor.acceptedQueryHint") }}
        </p>
      </div>
      <FormField
        v-model="config.acceptedQueryColumn"
        :id="fieldId('acceptedQueryColumn')"
        :required="true"
        :error="errorFor(fieldErrors, 'acceptedQueryColumn')"
        :label="t('dataQuality.editor.acceptedQueryColumn')"
        :hint="t('dataQuality.editor.acceptedQueryColumnHint')"
      />
    </div>

    <CheckboxField
      v-model="config.allowNull"
      :label="t('dataQuality.editor.allowNull')"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
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
import {
  type AcceptedValuesSource,
  type RuleConfigForm,
} from "@/utils/dataQualityForm";
import CheckboxField from "../CheckboxField.vue";
import {
  errorFor,
  fieldId,
  type RuleConfigSectionProps,
  requiredFor,
} from "./ruleConfigField";

const props = withDefaults(defineProps<RuleConfigSectionProps>(), {
  columnDetails: () => [],
  fieldErrors: () => ({}),
});

const config = defineModel<RuleConfigForm>({ required: true });
const { t } = useI18n();

/** The three value sources, in the order the select lists them. */
const SOURCE_OPTIONS: Array<{
  value: AcceptedValuesSource;
  labelKey: string;
}> = [
  { value: "list", labelKey: "dataQuality.editor.acceptedSourceList" },
  { value: "field", labelKey: "dataQuality.editor.acceptedSourceField" },
  { value: "query", labelKey: "dataQuality.editor.acceptedSourceQuery" },
];

const source = computed<AcceptedValuesSource>(
  () => config.value.acceptedSource ?? "list"
);

const sourceModel = computed({
  get: () => source.value,
  set: (value: string) => {
    config.value.acceptedSource = value as AcceptedValuesSource;
  },
});

/**
 * The reference column cannot be the checked column: the backend refuses a
 * field source that names the same column, so it is filtered out rather than
 * offered and then rejected. A stored selection that names it still renders.
 */
const referenceColumns = computed(() => {
  const checked = (config.value.column ?? "").trim();
  return checked === ""
    ? props.columnDetails
    : props.columnDetails.filter((column) => column.name !== checked);
});
</script>
