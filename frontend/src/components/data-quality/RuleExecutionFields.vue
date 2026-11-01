<template>
  <div
    v-if="showsTimeout || showsSampling"
    class="space-y-3"
  >
    <div class="space-y-1">
      <h3 class="text-base font-medium">
        {{ t("dataQuality.editor.executionTitle") }}
      </h3>
      <p class="text-sm text-muted-foreground">
        {{ t("dataQuality.editor.executionHint") }}
      </p>
    </div>

    <!-- A metadata check (schema drift) runs no target query, so it has no query
         budget and the field would promise a limit that does not exist. -->
    <FormField
      v-if="showsTimeout"
      id="dq-config-timeout"
      v-model="config.timeoutSeconds"
      type="number"
      class="max-w-xs"
      :min="MIN_TIMEOUT_SECONDS"
      :max="MAX_TIMEOUT_SECONDS"
      :label="t('dataQuality.editor.timeoutSeconds')"
      :hint="
        t('dataQuality.editor.timeoutSecondsHint', {
          seconds: DEFAULT_TIMEOUT_SECONDS,
        })
      "
    />

    <!-- One abstract control: the guarantee and the cost of each method, never
         the SQL clause an engine uses to implement it. -->
    <div
      v-if="showsSampling"
      class="space-y-3"
    >
      <div class="grid items-start gap-4 sm:grid-cols-2">
        <div class="space-y-2.5">
          <Label for="dq-config-sampling">
            {{ t("dataQuality.editor.sampling") }}
          </Label>
          <Select
            :model-value="selectedMethod"
            @update:model-value="setMethod"
          >
            <SelectTrigger id="dq-config-sampling">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="option in renderedOptions"
                :key="option.method"
                :value="option.method"
                :disabled="option.disabled"
              >
                {{ methodLabel(option.method) }}
              </SelectItem>
            </SelectContent>
          </Select>
          <p class="text-sm text-muted-foreground">
            {{ t("dataQuality.editor.samplingHint") }}
          </p>
        </div>

        <FormField
          v-if="config.sampleMethod === 'percent'"
          id="dq-config-sample-percent"
          v-model="config.samplePercent"
          type="number"
          :min="MIN_SAMPLE_PERCENT"
          :max="MAX_SAMPLE_PERCENT"
          :disabled="!percentAvailable"
          :label="t('dataQuality.editor.samplePercent')"
          :hint="
            percentAvailable ? t('dataQuality.editor.samplePercentHint') : ''
          "
          class="pr-8"
        >
          <template #suffix>
            <span
              class="absolute top-1/2 right-3 -translate-y-1/2 text-sm text-muted-foreground"
            >
              %
            </span>
          </template>
        </FormField>

        <FormField
          v-else-if="config.sampleMethod === 'maxRows'"
          id="dq-config-sample-max-rows"
          v-model="config.sampleMaxRows"
          type="number"
          :min="MIN_SAMPLE_MAX_ROWS"
          :max="MAX_SAMPLE_MAX_ROWS"
          :label="t('dataQuality.editor.sampleMaxRows')"
          :hint="t('dataQuality.editor.sampleMaxRowsHint')"
        />
      </div>

      <!-- A saved percentage the current target cannot express stays selected
           but is not editable, and the save is refused until another method is
           chosen; it is never converted to a row limit behind the reader's back. -->
      <p
        v-if="config.sampleMethod === 'percent' && !percentAvailable"
        class="text-sm text-destructive"
      >
        {{ t("dataQuality.editor.samplePercentUnsupported") }}
      </p>
    </div>
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
import type { Engine } from "@/types/proto-es/v1/common_pb";
import { QualityRuleType } from "@/types/proto-es/v1/data_quality_service_pb";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  canSampleRuleType,
  DEFAULT_TIMEOUT_SECONDS,
  MAX_SAMPLE_MAX_ROWS,
  MAX_SAMPLE_PERCENT,
  MAX_TIMEOUT_SECONDS,
  MIN_SAMPLE_MAX_ROWS,
  MIN_SAMPLE_PERCENT,
  MIN_TIMEOUT_SECONDS,
  type RuleConfigForm,
  type SampleMethod,
  samplingMethodsFor,
} from "@/utils/dataQualityForm";

/**
 * The execution section of the check dialog: the query timeout and how much of
 * the target one execution reads.
 *
 * The sampling control exposes the two user intents only ("read a share" and
 * "read at most this many rows"); which query clause implements a method is an
 * engine detail and never appears in the copy. A stored percentage the current
 * target cannot express stays visible but disabled, and validation refuses the
 * save until the reader picks a method the target supports.
 */

const props = defineProps<{
  /** Some checks read the registry, not rows; sampling does not apply to them. */
  ruleType: QualityRuleType;
  /** The target's engine; undefined until the instance list has loaded. */
  engine?: Engine;
  metaType?: MetaType;
}>();

const config = defineModel<RuleConfigForm>({ required: true });
const { t } = useI18n();

/**
 * The label of a sampling method. Written as direct translation calls rather
 * than a key table so the i18n gates can see every key from the source.
 */
function methodLabel(method: SampleMethod): string {
  switch (method) {
    case "percent":
      return t("dataQuality.editor.samplingPercent");
    case "maxRows":
      return t("dataQuality.editor.samplingMaxRows");
    default:
      return t("dataQuality.editor.samplingNone");
  }
}

const selectedMethod = computed<SampleMethod>(
  () => config.value.sampleMethod ?? "none"
);

/** The methods the target can express, in display order. */
const availableMethods = computed(() =>
  samplingMethodsFor(props.engine, props.metaType)
);
const percentAvailable = computed(() =>
  availableMethods.value.includes("percent")
);

/** The options to render; a stored unsupported percentage stays selected. */
const renderedOptions = computed(() => {
  const options = availableMethods.value.map((method) => ({
    method,
    disabled: false,
  }));
  if (!percentAvailable.value && selectedMethod.value === "percent") {
    // Keep a stored method visible (and selected) so it can be inspected, but
    // not selectable again on a target that cannot express it.
    options.splice(1, 0, { method: "percent" as const, disabled: true });
  }
  return options;
});

/**
 * A check whose verdict describes the relation as a whole (or that reads two
 * relations, metadata, or its own bounded sample) has nothing to sample with the
 * server's methods, so the control is hidden for it — unless the check already
 * carries a method, which stays visible so it can be inspected or cleared
 * instead of silently riding along on every save.
 */
const showsSampling = computed(
  () => canSampleRuleType(props.ruleType) || selectedMethod.value !== "none"
);

/** Metadata checks read the registry, so they have no query to time out. */
const showsTimeout = computed(
  () => props.ruleType !== QualityRuleType.SCHEMA_DRIFT
);

function setMethod(value: unknown) {
  const method = String(value);
  if (method !== "none" && method !== "percent" && method !== "maxRows") {
    return;
  }
  config.value.sampleMethod = method;
}
</script>
