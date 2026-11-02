<template>
  <div class="grid gap-4 sm:grid-cols-2">
    <!-- The reference is a registry target, not a table name: any instance,
         database or schema, and any table, external table or view. -->
    <div class="space-y-1.5 sm:col-span-2">
      <Label :for="fieldId('referenceGuid')">
        {{ t("dataQuality.editor.referenceTable") }}
        <span class="text-destructive">*</span>
      </Label>
      <div class="flex min-w-0 items-center gap-2">
        <Button
          :id="fieldId('referenceGuid')"
          variant="outline"
          class="h-10 min-w-0 flex-1 justify-between px-3 font-normal"
          :title="referencePathText"
          :aria-label="t('dataQuality.editor.referenceTable')"
          :aria-invalid="referenceError ? true : undefined"
          @click="pickerOpen = true"
        >
          <span
            class="truncate"
            :class="{ 'text-muted-foreground': !hasReference }"
          >
            {{ referencePathText }}
          </span>
          <ChevronDown class="ml-1 h-4 w-4 shrink-0 opacity-60" />
        </Button>
        <Button
          v-if="hasReference"
          variant="ghost"
          size="icon"
          class="h-8 w-8 shrink-0"
          :title="t('dataQuality.editor.referenceTableClear')"
          :aria-label="t('dataQuality.editor.referenceTableClear')"
          @click="clearReference"
        >
          <X class="h-4 w-4" />
        </Button>
      </div>
      <p
        v-if="referenceError"
        class="text-sm text-destructive"
      >
        {{ referenceError }}
      </p>
      <p
        v-else
        class="text-sm text-muted-foreground"
      >
        {{ t("dataQuality.editor.referenceTableHint") }}
      </p>
      <p
        v-if="hasReference"
        class="text-sm text-muted-foreground"
      >
        {{ t("dataQuality.editor.referenceTableScopeHint") }}
      </p>
    </div>

    <TargetPicker
      v-model:open="pickerOpen"
      mode="target"
      @select="applyReference"
    />

    <div class="flex flex-col gap-1.5">
      <Label>{{ t("dataQuality.editor.targetAggregate") }}</Label>
      <Select
        :model-value="String(config.leftAggFunction ?? QualityAggregateFunction.COUNT)"
        @update:model-value="setLeftFunction"
      >
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="fn in CROSS_TABLE_FUNCTIONS"
            :key="fn"
            :value="String(fn)"
          >
            {{ t(qualityAggregateFunctionKey(fn)) }}
          </SelectItem>
        </SelectContent>
      </Select>
    </div>
    <ColumnSelect
      :model-value="config.leftAggColumn ?? ''"
      :columns="columnDetails"
      :id="fieldId('leftAggColumn')"
      :required="requiredFor(ruleType, 'leftAggColumn')"
      :error="errorFor(fieldErrors, 'leftAggColumn')"
      :disabled="leftAggregateIsCount"
      :label="t('dataQuality.editor.targetColumn')"
      :hint="
        leftAggregateIsCount
          ? t('dataQuality.editor.countNeedsNoColumn')
          : t('dataQuality.editor.columnHint')
      "
      @update:model-value="(value) => (config.leftAggColumn = String(value))"
    />
    <div class="flex flex-col gap-1.5">
      <Label>{{ t("dataQuality.editor.referenceAggregate") }}</Label>
      <Select
        :model-value="String(config.rightAggFunction ?? QualityAggregateFunction.COUNT)"
        @update:model-value="setRightFunction"
      >
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="fn in CROSS_TABLE_FUNCTIONS"
            :key="fn"
            :value="String(fn)"
          >
            {{ t(qualityAggregateFunctionKey(fn)) }}
          </SelectItem>
        </SelectContent>
      </Select>
    </div>
    <ColumnSelect
      :model-value="config.rightAggColumn ?? ''"
      :columns="referenceColumns"
      :id="fieldId('rightAggColumn')"
      :required="requiredFor(ruleType, 'rightAggColumn')"
      :error="errorFor(fieldErrors, 'rightAggColumn')"
      :disabled="rightAggregateIsCount || !hasReference"
      :label="t('dataQuality.editor.referenceColumn')"
      :hint="referenceColumnHint"
      @update:model-value="(value) => (config.rightAggColumn = String(value))"
    />
    <p
      v-if="referenceColumnsError"
      class="text-sm text-destructive sm:col-span-2"
    >
      {{ referenceColumnsError }}
      <button
        type="button"
        class="ml-1 font-medium underline underline-offset-4"
        @click="loadReferenceColumns"
      >
        {{ t("common.retry") }}
      </button>
    </p>

    <div class="rounded-md border p-3 sm:col-span-2">
      <RuleFilterEditor
        v-model="rightFilterModel"
        :columns="referenceColumns"
        scope="reference"
        :disabled="!hasReference"
      />
    </div>

    <FormField
      v-model="config.tolerance"
      :id="fieldId('tolerance')"
      :required="requiredFor(ruleType, 'tolerance')"
      :error="errorFor(fieldErrors, 'tolerance')"
      type="number"
      class="sm:col-span-2"
      :label="t('dataQuality.editor.tolerance')"
      :hint="t('dataQuality.editor.toleranceHint')"
    />
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
</template>

<script setup lang="ts">
import { ChevronDown, X } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { getQualityTarget } from "@/api/data-quality";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import RuleFilterEditor from "@/components/data-quality/RuleFilterEditor.vue";
import TargetPicker from "@/components/data-quality/TargetPicker.vue";
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
import { useErrorMessage } from "@/composables/useErrorHandler";
import { QualityAggregateFunction } from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { qualityAggregateFunctionKey } from "@/utils/dataQuality";
import {
  CROSS_TABLE_FUNCTIONS,
  type FilterClauseForm,
  type RuleConfigForm,
  type TargetColumnDetail,
  type TargetSelection,
  trimmed,
} from "@/utils/dataQualityForm";
import {
  errorFor,
  fieldId,
  type RuleConfigSectionProps,
  requiredFor,
} from "./ruleConfigField";

/**
 * The cross-table reconciliation config: one aggregate of the target against one
 * aggregate of a reference relation, which may live in another instance or
 * database. The reference is picked through the shared target browser and its
 * identity is the registry GUID; the path shown beside it is display metadata.
 *
 * The right column and the reference-side filter are validated against the
 * *reference* table's columns, which are read from the registry (views and
 * external tables have no column rows to list any other way). Changing the
 * reference clears both, because a column or predicate from the previous
 * reference says nothing about the new one.
 */

const props = withDefaults(defineProps<RuleConfigSectionProps>(), {
  columnDetails: () => [],
  fieldErrors: () => ({}),
});

const config = defineModel<RuleConfigForm>({ required: true });
const { t } = useI18n();
const { formatError } = useErrorMessage();

const pickerOpen = ref(false);
/** The reference relation's columns, as the rule editor offers them. */
const referenceColumns = ref<TargetColumnDetail[]>([]);
const referenceColumnsLoading = ref(false);
const referenceColumnsError = ref("");

const hasReference = computed(() => trimmed(config.value.referenceGuid) !== "");

/** "instance > database > schema > object", the picked path for display. */
const referencePathText = computed(() => {
  const path = config.value.referencePath;
  const label = [path?.instance, path?.database, path?.schema, path?.object]
    .filter(Boolean)
    .join(" > ");
  if (label) {
    return label;
  }
  // A rule created through the API may carry the identity without the display
  // snapshot; the GUID still names the place, so its segments are shown instead of
  // the "select a table" prompt next to a live clear button.
  const guid = trimmed(config.value.referenceGuid);
  return (
    guid.split(";").filter(Boolean).join(" > ") ||
    t("dataQuality.editor.referenceTablePlaceholder")
  );
});

const referenceError = computed(() =>
  errorFor(props.fieldErrors, "referenceGuid")
);

const leftAggregateIsCount = computed(
  () =>
    (config.value.leftAggFunction ?? QualityAggregateFunction.COUNT) ===
    QualityAggregateFunction.COUNT
);
const rightAggregateIsCount = computed(
  () =>
    (config.value.rightAggFunction ?? QualityAggregateFunction.COUNT) ===
    QualityAggregateFunction.COUNT
);

const referenceColumnHint = computed(() => {
  if (rightAggregateIsCount.value) {
    return t("dataQuality.editor.countNeedsNoColumn");
  }
  if (!hasReference.value) {
    return t("dataQuality.editor.referenceTablePlaceholder");
  }
  return referenceColumnsLoading.value
    ? t("common.loading")
    : t("dataQuality.editor.referenceColumnHint");
});

/** The reference filter, owned by the config arm like the target's is by the form. */
const rightFilterModel = computed<FilterClauseForm[]>({
  get: () => config.value.rightFilter ?? [],
  set: (clauses) => {
    config.value.rightFilter = clauses;
  },
});

// COUNT takes no column. Clearing it when the function becomes COUNT keeps the
// form and the compiled request in agreement; the server also rejects a stray.
function setLeftFunction(value: unknown) {
  const fn = Number(value) as QualityAggregateFunction;
  config.value.leftAggFunction = fn;
  if (fn === QualityAggregateFunction.COUNT) {
    config.value.leftAggColumn = "";
  }
}

function setRightFunction(value: unknown) {
  const fn = Number(value) as QualityAggregateFunction;
  config.value.rightAggFunction = fn;
  if (fn === QualityAggregateFunction.COUNT) {
    config.value.rightAggColumn = "";
  }
}

/** The picker's answer: the reference identity, its path, and a clean right side. */
function applyReference(selection: TargetSelection) {
  config.value.referenceGuid = selection.guid;
  config.value.referenceMetaType = selection.metaType;
  config.value.referencePath = { ...selection.path };
  resetRightSide();
}

function clearReference() {
  config.value.referenceGuid = "";
  config.value.referenceMetaType = MetaType.TABLE;
  config.value.referencePath = {
    instance: "",
    database: "",
    schema: "",
    object: "",
  };
  resetRightSide();
}

/** Drop the assertions that were made about the previous reference relation. */
function resetRightSide() {
  config.value.rightAggColumn = "";
  config.value.rightFilter = [];
  referenceColumns.value = [];
  referenceColumnsError.value = "";
}

let loadTicket = 0;

/**
 * Read the reference relation's columns. A superseded read (the reference changed
 * while it was in flight) is dropped; a failed one leaves the pickers empty and
 * says why, with a retry, rather than blocking the form.
 */
async function loadReferenceColumns() {
  const guid = trimmed(config.value.referenceGuid);
  const metaType = config.value.referenceMetaType ?? MetaType.TABLE;
  const ticket = ++loadTicket;
  referenceColumns.value = [];
  referenceColumnsError.value = "";
  if (!guid) {
    referenceColumnsLoading.value = false;
    return;
  }
  referenceColumnsLoading.value = true;
  try {
    const response = await getQualityTarget({ targetGuid: guid, metaType });
    if (ticket !== loadTicket) {
      return;
    }
    referenceColumns.value = response.targetColumns.map((column) => ({
      name: column.name,
      type: column.type,
      nullable: column.nullable,
    }));
  } catch (error: unknown) {
    if (ticket !== loadTicket) {
      return;
    }
    referenceColumnsError.value = formatError(
      error,
      "dataQuality.editor.referenceColumnsLoadFailed"
    );
  } finally {
    if (ticket === loadTicket) {
      referenceColumnsLoading.value = false;
    }
  }
}

// The reference identity decides which columns the right side may use, so a
// change (a pick, a clear, or a rule loaded into the form) re-reads them.
watch(
  () => [config.value.referenceGuid, config.value.referenceMetaType] as const,
  () => {
    void loadReferenceColumns();
  },
  { immediate: true }
);
</script>
