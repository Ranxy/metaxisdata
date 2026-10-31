<template>
  <div
    id="dq-config-expected-columns"
    class="space-y-3"
    tabindex="-1"
  >
    <div class="flex flex-wrap items-start justify-between gap-2">
      <div class="space-y-1">
        <Label>
          {{ t("dataQuality.editor.expectedColumns") }}
          <span class="text-destructive">*</span>
        </Label>
        <p
          v-if="errorFor(fieldErrors, 'expectedColumns')"
          class="text-sm text-destructive"
        >
          {{ errorFor(fieldErrors, "expectedColumns") }}
        </p>
        <p
          v-else
          class="text-sm text-muted-foreground"
        >
          {{ t("dataQuality.editor.expectedColumnsHint") }}
        </p>
      </div>
      <Button
        type="button"
        variant="outline"
        size="sm"
        :disabled="columnDetails.length === 0"
        @click="fillFromTarget"
      >
        {{ t("dataQuality.editor.fillFromTarget") }}
      </Button>
    </div>
    <div
      v-for="(column, index) in config.expectedColumns ?? []"
      :key="index"
      class="grid items-center gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto_auto]"
    >
      <Input
        :model-value="column.name"
        :aria-label="t('dataQuality.editor.columnName')"
        :placeholder="t('dataQuality.editor.columnName')"
        @update:model-value="(value) => setName(column, String(value))"
        @blur="commitName(column)"
      />
      <Input
        :model-value="column.type"
        :aria-label="t('dataQuality.editor.columnType')"
        :placeholder="t('dataQuality.editor.columnType')"
        @update:model-value="(value) => setType(column, String(value))"
      />
      <CheckboxField
        :model-value="column.nullable === true"
        :label="t('dataQuality.editor.nullable')"
        @update:model-value="(value) => setNullable(column, value)"
      />
      <Button
        type="button"
        variant="ghost"
        size="icon"
        class="h-8 w-8 text-muted-foreground hover:text-destructive"
        :aria-label="t('dataQuality.editor.removeColumn')"
        :title="t('dataQuality.editor.removeColumn')"
        @click="removeColumn(index)"
      >
        <Trash2 class="h-4 w-4" />
      </Button>
    </div>
    <p
      v-if="(config.expectedColumns ?? []).length > 0"
      class="text-xs text-muted-foreground"
    >
      {{ t("dataQuality.editor.nullableHint") }}
    </p>
    <Button
      type="button"
      variant="outline"
      size="sm"
      @click="addColumn"
    >
      <Plus class="mr-1 h-4 w-4" />
      {{ t("dataQuality.editor.addColumn") }}
    </Button>
    <div class="grid gap-4 sm:grid-cols-3">
      <CheckboxField
        v-model="config.checkTypes"
        :label="t('dataQuality.editor.checkTypes')"
      />
      <CheckboxField
        v-model="config.checkOrder"
        :label="t('dataQuality.editor.checkOrder')"
      />
      <CheckboxField
        v-model="config.allowExtraColumns"
        :label="t('dataQuality.editor.allowExtraColumns')"
      />
    </div>
    <div class="grid gap-4 sm:grid-cols-2">
      <FormField
        v-model="config.failThreshold"
        :id="fieldId('failThreshold')"
        :required="requiredFor(ruleType, 'failThreshold')"
        :error="errorFor(fieldErrors, 'failThreshold')"
        type="number"
        :label="t('dataQuality.editor.failThreshold')"
      />
      <FormField
        v-model="config.warnThreshold"
        :id="fieldId('warnThreshold')"
        :required="requiredFor(ruleType, 'warnThreshold')"
        :error="errorFor(fieldErrors, 'warnThreshold')"
        type="number"
        :label="t('dataQuality.editor.warnThreshold')"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { Plus, Trash2 } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useSchemaDriftColumns } from "@/composables/useSchemaDriftColumns";
import type { RuleConfigForm } from "@/utils/dataQualityForm";
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

const columnDetailsRef = computed(() => props.columnDetails);
const {
  addColumn,
  removeColumn,
  setName,
  commitName,
  setType,
  setNullable,
  fillFromTarget,
} = useSchemaDriftColumns(config, columnDetailsRef);
</script>
