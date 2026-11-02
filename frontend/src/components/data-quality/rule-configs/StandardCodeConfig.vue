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

    <!-- A workspace code table: required for a custom standard, optional for a
         built-in one, where it narrows the built-in table. -->
    <div class="flex flex-col gap-1.5 sm:col-span-2">
      <Label for="dq-field-codeTable">
        {{ t("dataQuality.editor.codeTable") }}
        <span
          v-if="codeTableRequired"
          class="text-destructive"
        >*</span>
      </Label>
      <Select
        :model-value="codeTableSelectValue"
        :disabled="!canListCodeTables || codeTablesLoading"
        @update:model-value="setCodeTable"
      >
        <SelectTrigger
          id="dq-field-codeTable"
          :aria-required="codeTableRequired ? true : undefined"
          :aria-invalid="errorFor(fieldErrors, 'codeTable') ? true : undefined"
        >
          <SelectValue
            :placeholder="t('dataQuality.editor.codeTablePlaceholder')"
          />
        </SelectTrigger>
        <SelectContent>
          <SelectItem :value="NO_CODE_TABLE">
            {{ t("dataQuality.editor.codeTableNone") }}
          </SelectItem>
          <SelectItem
            v-for="table in selectableCodeTables"
            :key="table.name"
            :value="table.name"
          >
            {{
              t("dataQuality.editor.codeTableOption", {
                title: table.title || table.name,
                count: table.valueCount,
              })
            }}
          </SelectItem>
        </SelectContent>
      </Select>
      <p
        v-if="errorFor(fieldErrors, 'codeTable')"
        class="text-sm text-destructive"
      >
        {{ errorFor(fieldErrors, "codeTable") }}
      </p>
      <p
        v-else-if="!canListCodeTables"
        class="text-sm text-muted-foreground"
      >
        {{ t("dataQuality.editor.codeTableNoPermission") }}
      </p>
      <div
        v-else-if="codeTablesFailed"
        class="flex flex-wrap items-center gap-2 text-sm text-muted-foreground"
      >
        <span>{{ t("dataQuality.editor.codeTableLoadFailed") }}</span>
        <Button
          variant="link"
          size="sm"
          class="h-auto p-0"
          @click="loadCodeTables"
        >
          {{ t("common.retry") }}
        </Button>
      </div>
      <p
        v-else-if="!codeTablesLoading && codeTables.length === 0"
        class="text-sm text-muted-foreground"
      >
        {{ t("dataQuality.editor.codeTableEmpty") }}
        <router-link
          class="font-medium text-primary underline underline-offset-4"
          to="/settings/data-quality"
        >
          {{ t("dataQuality.editor.codeTableEmptyAction") }}
        </router-link>
      </p>
      <p
        v-else-if="selectedCodeTable"
        class="text-sm text-muted-foreground"
      >
        {{
          t("dataQuality.editor.codeTableValueCount", {
            count: selectedCodeTable.valueCount,
          })
        }}
      </p>
      <p
        v-else
        class="text-sm text-muted-foreground"
      >
        {{ t("dataQuality.editor.codeTableHint") }}
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
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { listAllQualityCodeTables } from "@/api/data-quality";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import { Button } from "@/components/ui/button";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityCodeTable,
  QualityStandardCode,
} from "@/types/proto-es/v1/data_quality_service_pb";
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
const authStore = useAuthStore();

const standardOptions = [
  QualityStandardCode.UNIFIED_SOCIAL_CREDIT_CODE,
  QualityStandardCode.ORGANIZATION_CODE,
  QualityStandardCode.RESIDENT_ID_CARD,
  QualityStandardCode.ADMINISTRATIVE_DIVISION,
  QualityStandardCode.INDUSTRY_CLASSIFICATION,
  QualityStandardCode.CUSTOM_CODE_TABLE,
].map((value) => ({ value, key: qualityStandardCodeKey(value) }));

/**
 * Radix's Select cannot hold an empty string item, so "no workspace table"
 * needs a sentinel; it is mapped back to "" on the way into the form.
 */
const NO_CODE_TABLE = "-";

const codeTables = ref<QualityCodeTable[]>([]);
const codeTablesLoading = ref(false);
const codeTablesFailed = ref(false);
const canListCodeTables = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.codeTables.list")
);
// A custom standard cannot validate without a table; a built-in one uses its
// own bundled table unless a workspace table narrows it.
const codeTableRequired = computed(
  () => config.value.standard === QualityStandardCode.CUSTOM_CODE_TABLE
);
const codeTableSelectValue = computed(
  () => config.value.codeTable || NO_CODE_TABLE
);
const selectedCodeTable = computed(() =>
  codeTables.value.find((table) => table.name === config.value.codeTable)
);

/**
 * An empty table cannot be referenced at save time, so it is not offered. The table
 * a stored check already points at stays listed even if it was emptied, so the
 * reader can see and clear the dangling reference instead of it vanishing.
 */
const selectableCodeTables = computed(() => {
  const selected = config.value.codeTable ?? "";
  return codeTables.value.filter(
    (table) => table.valueCount > 0 || table.name === selected
  );
});

function setStandard(value: unknown) {
  config.value.standard = Number(value) as QualityStandardCode;
}

function setCodeTable(value: unknown) {
  config.value.codeTable = String(value) === NO_CODE_TABLE ? "" : String(value);
}

/**
 * The summaries are enough for the picker: a title, a value count and the
 * resource name the check stores. A failed read leaves the control on "no
 * table" with a retry, so a check that does not need one still saves.
 */
async function loadCodeTables() {
  if (!canListCodeTables.value || codeTablesLoading.value) {
    return;
  }
  codeTablesLoading.value = true;
  codeTablesFailed.value = false;
  try {
    codeTables.value = await listAllQualityCodeTables();
  } catch {
    codeTables.value = [];
    codeTablesFailed.value = true;
  } finally {
    codeTablesLoading.value = false;
  }
}

onMounted(() => {
  void loadCodeTables();
});
</script>
