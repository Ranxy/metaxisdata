<template>
  <div class="space-y-5">
    <p
      v-for="(hint, index) in oneOfHints"
      :key="index"
      class="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground"
    >
      {{ t("dataQuality.schema.atLeastOneHint", { fields: hint }) }}
    </p>

    <!-- Shared by every threshold-carrying config: the unit the fail/warn values
         are read in, and the values themselves. custom SQL has no row total, so
         it keeps the fail/warn pair but never the unit. -->
    <div
      v-if="isCountRule || ruleType === QualityRuleType.CUSTOM_SQL"
      class="grid gap-4 sm:grid-cols-3"
    >
      <div
        v-if="isCountRule"
        class="flex flex-col gap-1.5"
      >
        <Label>{{ t("dataQuality.editor.thresholdMetric") }}</Label>
        <Select v-model="thresholdMetricModel">
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem :value="String(QualityThresholdMetric.COUNT)">
              {{ t("dataQuality.thresholdMetric.count") }}
            </SelectItem>
            <SelectItem :value="String(QualityThresholdMetric.PERCENT)">
              {{ t("dataQuality.thresholdMetric.percent") }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <FormField
        v-model="config.failThreshold"
        :id="fieldId('failThreshold')"
        :required="requiredFor('failThreshold')"
        :error="errorFor('failThreshold')"
        type="number"
        :label="t('dataQuality.editor.failThreshold')"
        :hint="
          isCountRule ? t('dataQuality.editor.thresholdHint') : undefined
        "
      />
      <FormField
        v-model="config.warnThreshold"
        :id="fieldId('warnThreshold')"
        :required="requiredFor('warnThreshold')"
        :error="errorFor('warnThreshold')"
        type="number"
        :label="t('dataQuality.editor.warnThreshold')"
      />
    </div>

    <RowCountConfig
      v-if="isRowCountRule"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <NullPercentConfig
      v-else-if="ruleType === QualityRuleType.NULL_PERCENT"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <UniquenessConfig
      v-else-if="
        ruleType === QualityRuleType.UNIQUE ||
        ruleType === QualityRuleType.UNIQUE_COMBINATION
      "
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <AcceptedValuesConfig
      v-else-if="ruleType === QualityRuleType.ACCEPTED_VALUES"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <RangeConfig
      v-else-if="ruleType === QualityRuleType.RANGE"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <ScalarConfig
      v-else-if="
        ruleType === QualityRuleType.REGEX ||
        ruleType === QualityRuleType.STRING_LENGTH ||
        ruleType === QualityRuleType.REFERENTIAL_INTEGRITY
      "
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <CrossColumnConfig
      v-else-if="ruleType === QualityRuleType.CROSS_COLUMN"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <FreshnessConfig
      v-else-if="ruleType === QualityRuleType.FRESHNESS"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <CustomSQLConfig
      v-else-if="ruleType === QualityRuleType.CUSTOM_SQL"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <SchemaDriftConfig
      v-else-if="ruleType === QualityRuleType.SCHEMA_DRIFT"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <GroupCheckConfig
      v-else-if="ruleType === QualityRuleType.GROUP_CHECK"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <AggregateConfig
      v-else-if="ruleType === QualityRuleType.AGGREGATE"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <CrossTableConfig
      v-else-if="ruleType === QualityRuleType.CROSS_TABLE"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
    <CelConfig
      v-else-if="ruleType === QualityRuleType.CEL_EXPRESSION"
      v-model="config"
      :rule-type="ruleType"
      :column-details="columnDetails"
      :field-errors="fieldErrors"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
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
  QualityRuleType,
  QualityThresholdMetric,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  isThresholdMetricType,
  type RuleConfigForm,
  type TargetColumnDetail,
} from "@/utils/dataQualityForm";
import { oneOfGroups } from "@/utils/dataQualitySchema";
import AcceptedValuesConfig from "./rule-configs/AcceptedValuesConfig.vue";
import AggregateConfig from "./rule-configs/AggregateConfig.vue";
import CelConfig from "./rule-configs/CelConfig.vue";
import CrossColumnConfig from "./rule-configs/CrossColumnConfig.vue";
import CrossTableConfig from "./rule-configs/CrossTableConfig.vue";
import CustomSQLConfig from "./rule-configs/CustomSQLConfig.vue";
import FreshnessConfig from "./rule-configs/FreshnessConfig.vue";
import GroupCheckConfig from "./rule-configs/GroupCheckConfig.vue";
import NullPercentConfig from "./rule-configs/NullPercentConfig.vue";
import RangeConfig from "./rule-configs/RangeConfig.vue";
import RowCountConfig from "./rule-configs/RowCountConfig.vue";
import {
  errorFor as fieldError,
  fieldId,
  requiredFor as fieldRequired,
} from "./rule-configs/ruleConfigField";
import ScalarConfig from "./rule-configs/ScalarConfig.vue";
import SchemaDriftConfig from "./rule-configs/SchemaDriftConfig.vue";
import UniquenessConfig from "./rule-configs/UniquenessConfig.vue";

/**
 * The config fields of one rule type: the shared threshold block, then the
 * section that owns this rule family. Each family is a component of its own
 * (`rule-configs/`), so adding a rule type touches one small file instead of a
 * thousand-line template.
 */

const props = withDefaults(
  defineProps<{
    ruleType: QualityRuleType;
    /** Used by the schema-drift snapshot's "fill from target" action. */
    columnDetails?: TargetColumnDetail[];
    /** Inline errors keyed by config field, from the editor's pre-flight check. */
    fieldErrors?: Record<string, string>;
  }>(),
  { columnDetails: () => [], fieldErrors: () => ({}) }
);

const config = defineModel<RuleConfigForm>({ required: true });
const thresholdMetric = defineModel<QualityThresholdMetric>("thresholdMetric", {
  required: true,
});

const { t } = useI18n();

function requiredFor(field: string): boolean {
  return fieldRequired(props.ruleType, field);
}

function errorFor(field: string): string {
  return fieldError(props.fieldErrors, field);
}

/** One line per "at least one of" group, so the group's rule is visible. */
const oneOfHints = computed(() =>
  [...oneOfGroups(props.ruleType).values()].map((fields) =>
    fields.map((field) => t(field.labelKey)).join(" / ")
  )
);

const isCountRule = computed(() => isThresholdMetricType(props.ruleType));
const isRowCountRule = computed(
  () =>
    props.ruleType === QualityRuleType.ROW_COUNT ||
    props.ruleType === QualityRuleType.ROW_COUNT_CHANGE
);

const thresholdMetricModel = computed({
  get: () => String(thresholdMetric.value),
  set: (value: string) => {
    thresholdMetric.value = Number(value) as QualityThresholdMetric;
  },
});
</script>
