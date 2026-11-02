<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>{{ t("dataQuality.checks.batchEdit.title") }}</DialogTitle>
        <DialogDescription>
          {{
            t("dataQuality.checks.batchEdit.description", {
              count: snapshot.length,
            })
          }}
        </DialogDescription>
      </DialogHeader>

      <div class="space-y-4">
        <Alert
          v-if="error"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>{{ t("dataQuality.checks.batchEdit.failed") }}</AlertTitle>
          <AlertDescription class="break-words">
            {{ error }}
          </AlertDescription>
        </Alert>

        <template v-if="summary">
          <Alert :variant="summary.failed.length > 0 ? 'warning' : 'success'">
            <AlertTriangle
              v-if="summary.failed.length > 0"
              class="h-4 w-4"
            />
            <AlertTitle>
              {{
                summary.failed.length > 0
                  ? t("dataQuality.checks.batchEdit.partial", {
                      succeeded: summary.succeeded,
                      failed: summary.failed.length,
                    })
                  : t("dataQuality.checks.batchEdit.success", {
                      count: summary.succeeded,
                    })
              }}
            </AlertTitle>
          </Alert>

          <div
            v-if="summary.failed.length > 0"
            class="space-y-2"
          >
            <div class="text-sm font-medium">
              {{ t("dataQuality.checks.batchEdit.failures") }}
            </div>
            <ul class="space-y-1 text-sm">
              <li
                v-for="failure in summary.failed"
                :key="failure.index"
                class="break-words"
              >
                <span class="font-medium">{{ failureTitle(failure.index) }}</span>
                <span class="text-muted-foreground">
                  — {{ failureMessage(failure.message) }}
                </span>
              </li>
            </ul>
          </div>
        </template>

        <!-- One field at a time: every rule keeps its parameters, and the update
             mask names only the field the operator picked. No field starts
             chosen, so an untouched submit can never write anything. -->
        <template v-if="!summary">
          <div class="flex flex-col gap-1.5">
            <Label for="dq-batch-edit-field">
              {{ t("dataQuality.checks.batchEdit.field") }}
            </Label>
            <Select v-model="field">
              <SelectTrigger
                id="dq-batch-edit-field"
                class="w-full"
              >
                <SelectValue
                  :placeholder="t('dataQuality.checks.batchEdit.fieldPlaceholder')"
                />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="severity">
                  {{ t("dataQuality.filterSeverity") }}
                </SelectItem>
                <SelectItem value="dimension">
                  {{ t("dataQuality.filterDimension") }}
                </SelectItem>
                <SelectItem value="state">
                  {{ t("dataQuality.filterState") }}
                </SelectItem>
                <SelectItem value="owner">
                  {{ t("dataQuality.filterOwner") }}
                </SelectItem>
                <SelectItem value="on_empty">
                  {{ t("dataQuality.checks.batchEdit.onEmpty") }}
                </SelectItem>
                <SelectItem
                  value="threshold"
                  :disabled="!thresholdAvailable"
                >
                  {{ t("dataQuality.checks.batchEdit.threshold") }}
                </SelectItem>
              </SelectContent>
            </Select>
            <!-- A threshold update is shared, so it only makes sense over one
                 threshold family; say why the option is disabled. -->
            <p
              v-if="!thresholdAvailable"
              class="text-sm text-muted-foreground"
            >
              <template v-if="sharedRuleType !== undefined">
                {{
                  t("dataQuality.checks.batchEdit.thresholdTypeHint", {
                    type: t(qualityRuleTypeKey(sharedRuleType)),
                  })
                }}
              </template>
              <template v-else>
                {{ t("dataQuality.checks.batchEdit.thresholdHint") }}
              </template>
            </p>
          </div>

          <div
            v-if="field === 'severity'"
            class="flex flex-col gap-1.5"
          >
            <Label for="dq-batch-edit-severity">
              {{ t("dataQuality.filterSeverity") }}
            </Label>
            <Select v-model="severityValue">
              <SelectTrigger
                id="dq-batch-edit-severity"
                class="w-full"
              >
                <SelectValue
                  :placeholder="t('dataQuality.checks.batchEdit.mixedPlaceholder')"
                />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="option in severityOptions"
                  :key="option.value"
                  :value="String(option.value)"
                >
                  {{ t(option.key) }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div
            v-if="field === 'dimension'"
            class="flex flex-col gap-1.5"
          >
            <Label for="dq-batch-edit-dimension">
              {{ t("dataQuality.filterDimension") }}
            </Label>
            <Select v-model="dimensionValue">
              <SelectTrigger
                id="dq-batch-edit-dimension"
                class="w-full"
              >
                <SelectValue
                  :placeholder="t('dataQuality.checks.batchEdit.mixedPlaceholder')"
                />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="option in dimensionOptions"
                  :key="option.value"
                  :value="String(option.value)"
                >
                  {{ t(option.key) }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div
            v-if="field === 'state'"
            class="flex flex-col gap-1.5"
          >
            <Label for="dq-batch-edit-state">
              {{ t("dataQuality.filterState") }}
            </Label>
            <Select v-model="stateValue">
              <SelectTrigger
                id="dq-batch-edit-state"
                class="w-full"
              >
                <SelectValue
                  :placeholder="t('dataQuality.checks.batchEdit.mixedPlaceholder')"
                />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="option in stateOptions"
                  :key="option.value"
                  :value="String(option.value)"
                >
                  {{ t(option.key) }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <UserSelect
            v-if="field === 'owner'"
            id="dq-batch-edit-owner"
            v-model="ownerValue"
            :label="t('dataQuality.filterOwner')"
            :placeholder="t('dataQuality.checks.batchEdit.ownerPlaceholder')"
            :no-owner-value="UNASSIGNED_OWNER"
            :no-owner-label="t('dataQuality.unassignedOwner')"
            :hint="t('dataQuality.checks.batchEdit.ownerHint')"
            :preloaded-users="users"
          />

          <div
            v-if="field === 'on_empty'"
            class="flex flex-col gap-1.5"
          >
            <Label for="dq-batch-edit-on-empty">
              {{ t("dataQuality.checks.batchEdit.onEmpty") }}
            </Label>
            <Select v-model="onEmptyValue">
              <SelectTrigger
                id="dq-batch-edit-on-empty"
                class="w-full"
              >
                <SelectValue
                  :placeholder="t('dataQuality.checks.batchEdit.mixedPlaceholder')"
                />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="option in onEmptyOptions"
                  :key="option.value"
                  :value="String(option.value)"
                >
                  {{ t(option.key) }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div
            v-if="field === 'threshold'"
            class="space-y-4"
          >
            <div class="flex flex-col gap-1.5">
              <Label for="dq-batch-edit-metric">
                {{ t("dataQuality.editor.thresholdMetric") }}
              </Label>
              <Select v-model="metricValue">
                <SelectTrigger
                  id="dq-batch-edit-metric"
                  class="w-full"
                >
                  <SelectValue
                    :placeholder="t('dataQuality.checks.batchEdit.mixedPlaceholder')"
                  />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="option in metricOptions"
                    :key="option.value"
                    :value="String(option.value)"
                  >
                    {{ t(option.key) }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <FormField
              id="dq-batch-edit-fail-threshold"
              v-model="failThreshold"
              type="number"
              required
              :label="t('dataQuality.editor.failThreshold')"
              :hint="t('dataQuality.editor.thresholdHint')"
              :error="thresholdValidation.fail"
            />
            <!-- The required-field error would hide the FormField hint, so the
                 mixed-fail explanation sits beside the control. -->
            <p
              v-if="failPrefillMixed"
              class="text-xs text-muted-foreground"
            >
              {{ t("dataQuality.checks.batchEdit.thresholdFailMixedHint") }}
            </p>
            <FormField
              id="dq-batch-edit-warn-threshold"
              v-model="warnThreshold"
              type="number"
              :disabled="clearWarn"
              :label="t('dataQuality.editor.warnThreshold')"
              :hint="t('dataQuality.checks.batchEdit.thresholdWarnHint')"
              :error="thresholdValidation.warn"
            />
            <div class="flex items-center gap-2">
              <Checkbox
                id="dq-batch-edit-clear-warn"
                :checked="clearWarn"
                @update:checked="
                  (checked: boolean | 'indeterminate') =>
                    setClearWarn(checked === true)
                "
              />
              <Label
                for="dq-batch-edit-clear-warn"
                class="text-sm font-normal"
              >
                {{ t("dataQuality.checks.batchEdit.clearWarn") }}
              </Label>
            </div>
          </div>
        </template>

        <!-- Rules whose type ignores the empty-result behavior stay untouched:
             rewriting their config would only append a revision for nothing. -->
        <Alert
          v-if="field === 'on_empty' && skipped.length > 0"
          variant="warning"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>
            {{ t("dataQuality.checks.batchEdit.notApplicableTitle") }}
          </AlertTitle>
          <AlertDescription>
            <p>
              {{
                t("dataQuality.checks.batchEdit.notApplicable", {
                  count: skipped.length,
                })
              }}
            </p>
            <ul class="mt-1 list-disc pl-4">
              <li
                v-for="rule in skipped"
                :key="rule.name"
                class="break-words"
              >
                {{ ruleLabel(rule) }}
              </li>
            </ul>
          </AlertDescription>
        </Alert>
      </div>

      <DialogFooter>
        <Button
          variant="outline"
          :disabled="submitting"
          @click="close"
        >
          {{ t(summary ? "common.close" : "common.cancel") }}
        </Button>
        <Button
          v-if="!summary"
          :disabled="!canSubmit"
          @click="submit"
        >
          {{
            submitting
              ? t("dataQuality.checks.batchEdit.submitting")
              : t("dataQuality.checks.batchEdit.submit", { count: requestCount })
          }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import { Code } from "@connectrpc/connect";
import { AlertTriangle } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { batchUpdateQualityRules } from "@/api/data-quality";
import UserSelect from "@/components/common/UserSelect.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import {
  type BatchUpdateQualityRuleResult,
  QualityDimension,
  QualityEmptyBehavior,
  type QualityRule,
  type QualityRuleConfig,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityThresholdMetric,
  type UpdateQualityRuleRequest,
  UpdateQualityRuleRequestSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import {
  qualityDimensionKey,
  qualityEmptyBehaviorKey,
  qualityRuleStateKey,
  qualityRuleTypeKey,
  qualitySeverityKey,
} from "@/utils/dataQuality";
import {
  buildRuleConfig,
  isOnEmptyRelevant,
  isThresholdMetricType,
  ruleToForm,
} from "@/utils/dataQualityForm";
import { parseBatchItemError } from "@/utils/error";

/**
 * Edit one field across several checks at once.
 *
 * A batch item names its own update mask and its rule's current revision, but
 * the revision is an optimistic-concurrency guard only for a *definition* edit
 * (dimension or config): those are semantic, the store appends a revision, and a
 * check someone else changed is rejected instead of overwritten. Severity, owner
 * and state are head-only last-write-wins edits — the store does not append a
 * revision for them, so they cannot conflict, and their outcome is only reported
 * per item. The empty-result behavior exists for some rule types only; those rows
 * are reported as not applicable and left alone rather than rewritten.
 *
 * Nothing starts selected: the operator has to choose the field and, whenever
 * the selection disagrees, the value too. A field's value is prefilled only from
 * a uniform selection, so an untouched submit cannot rewrite anything.
 */

/** The server refuses more than 200 items in one batch. */
const MAX_BATCH = 200;

/**
 * The owner picker's "remove the owner" choice. It is dialog-local: the request
 * boundary turns it into an empty owner, and the empty model value means the
 * operator has not chosen anything yet, so it cannot clear owners by default.
 */
const UNASSIGNED_OWNER = "__unassigned__";

type BatchEditField =
  | "severity"
  | "dimension"
  | "state"
  | "owner"
  | "on_empty"
  | "threshold";

/** The chosen field, or "" while nothing has been chosen yet. */
type FieldChoice = BatchEditField | "";

const props = withDefaults(
  defineProps<{
    open: boolean;
    /** The selected checks, all from the current page. */
    rules?: QualityRule[];
    /** The member directory, when the page already loaded it. */
    users?: User[];
  }>(),
  { rules: () => [] }
);

const emit = defineEmits<{
  "update:open": [value: boolean];
  /** The checks the batch call updated, so the page can refresh. */
  done: [ruleNames: string[]];
}>();

const { t } = useI18n();
const { formatError } = useErrorMessage();

const snapshot = ref<QualityRule[]>([]);
const field = ref<FieldChoice>("");
const severityValue = ref("");
const dimensionValue = ref("");
const stateValue = ref("");
const ownerValue = ref("");
const onEmptyValue = ref("");
const metricValue = ref("");
const failThreshold = ref("");
const warnThreshold = ref("");
const clearWarn = ref(false);
/** True when the selection's stored failure thresholds differ, so a warn-only
 *  edit has to supply a failure threshold explicitly. */
const failPrefillMixed = ref(false);
/** The metric the selection prefilled, so a caller's unit switch can be told
 *  apart from the prefill itself. */
let prefilledMetric = "";
const submitting = ref(false);
const error = ref("");
const results = ref<BatchUpdateQualityRuleResult[]>([]);
/** The rules this submit actually sent, so result names resolve to titles. */
const submittedRules = ref<QualityRule[]>([]);
/** The refresh event fires once per batch response, not again on close. */
const doneEmitted = ref(false);

const severityOptions: Array<{ value: QualitySeverity; key: string }> = [
  QualitySeverity.WARNING,
  QualitySeverity.ERROR,
  QualitySeverity.CRITICAL,
].map((value) => ({ value, key: qualitySeverityKey(value) }));

const dimensionOptions: Array<{ value: QualityDimension; key: string }> = [
  QualityDimension.COMPLETENESS,
  QualityDimension.VALIDITY,
  QualityDimension.UNIQUENESS,
  QualityDimension.CONSISTENCY,
  QualityDimension.TIMELINESS,
  QualityDimension.ACCURACY,
].map((value) => ({ value, key: qualityDimensionKey(value) }));

/** Archiving is DeleteQualityRule's job, so a batch state edit only pauses or resumes. */
const stateOptions: Array<{ value: QualityRuleState; key: string }> = [
  QualityRuleState.ENABLED,
  QualityRuleState.PAUSED,
].map((value) => ({ value, key: qualityRuleStateKey(value) }));

const onEmptyOptions: Array<{ value: QualityEmptyBehavior; key: string }> = [
  QualityEmptyBehavior.NOT_EVALUATED,
  QualityEmptyBehavior.PASS,
  QualityEmptyBehavior.FAIL,
].map((value) => ({ value, key: qualityEmptyBehaviorKey(value) }));

const metricOptions: Array<{ value: QualityThresholdMetric; key: string }> = [
  {
    value: QualityThresholdMetric.COUNT,
    key: "dataQuality.thresholdMetric.count",
  },
  {
    value: QualityThresholdMetric.PERCENT,
    key: "dataQuality.thresholdMetric.percent",
  },
];

/** The one rule type all selected checks share, or undefined when they differ. */
const sharedRuleType = computed<QualityRuleType | undefined>(() => {
  const types = new Set(snapshot.value.map((rule) => rule.ruleType));
  if (types.size !== 1) {
    return undefined;
  }
  const [type] = [...types];
  return type;
});

const thresholdAvailable = computed(
  () =>
    sharedRuleType.value !== undefined &&
    isThresholdMetricType(sharedRuleType.value)
);

const applicable = computed(() =>
  snapshot.value.filter((rule) => isOnEmptyRelevant(rule.ruleType))
);
const skipped = computed(() =>
  snapshot.value.filter((rule) => !isOnEmptyRelevant(rule.ruleType))
);

const requestCount = computed(() =>
  field.value === "on_empty" ? applicable.value.length : snapshot.value.length
);

const thresholdValidation = computed(() => {
  if (field.value !== "threshold") {
    return { fail: "", warn: "", valid: false };
  }
  const percent = metricValue.value === String(QualityThresholdMetric.PERCENT);
  let fail = "";
  const failText = failThreshold.value.trim();
  const failNumber = parseNumber(failText);
  if (failText === "") {
    fail = t("dataQuality.checks.batchEdit.thresholdRequired");
  } else if (failNumber === undefined || failNumber < 0) {
    fail = t("dataQuality.checks.batchEdit.thresholdNumberInvalid");
  } else if (percent && failNumber > 100) {
    fail = t("dataQuality.checks.batchEdit.thresholdPercentMax");
  }

  // A blank warning threshold means "keep each check's current one"; the
  // explicit clear checkbox is the only way to remove it.
  let warn = "";
  let warnNumber: number | undefined;
  const warnText = warnThreshold.value.trim();
  if (clearWarn.value) {
    warnNumber = undefined;
  } else if (warnText !== "") {
    warnNumber = parseNumber(warnText);
    if (warnNumber === undefined || warnNumber < 0) {
      warn = t("dataQuality.checks.batchEdit.thresholdNumberInvalid");
    } else if (percent && warnNumber > 100) {
      warn = t("dataQuality.checks.batchEdit.thresholdPercentMax");
    }
  } else {
    // Keeping the shared value; when the selection disagrees there is nothing
    // to compare, so the server reports any per-item conflict.
    warnNumber = keptWarnValue.value;
  }
  if (
    warn === "" &&
    fail === "" &&
    failNumber !== undefined &&
    warnNumber !== undefined &&
    warnNumber > failNumber
  ) {
    warn = t("dataQuality.checks.batchEdit.thresholdWarnAbove");
  }

  return {
    fail,
    warn,
    valid: metricValue.value !== "" && fail === "" && warn === "",
  };
});

const canSubmit = computed(() => {
  if (submitting.value || snapshot.value.length === 0) {
    return false;
  }
  switch (field.value) {
    case "severity":
      return severityValue.value !== "";
    case "dimension":
      return dimensionValue.value !== "";
    case "state":
      return stateValue.value !== "";
    case "owner":
      return ownerValue.value !== "";
    case "on_empty":
      return applicable.value.length > 0 && onEmptyValue.value !== "";
    case "threshold":
      return thresholdAvailable.value && thresholdValidation.value.valid;
    default:
      return false;
  }
});

const summary = computed(() => {
  if (results.value.length === 0) {
    return undefined;
  }
  const failed = results.value.flatMap((result, index) =>
    result.error ? [{ index, message: result.error }] : []
  );
  return { succeeded: results.value.length - failed.length, failed };
});

/** The uniform value of a selection, or undefined when the values differ. */
function agreedValue<T>(values: readonly T[]): T | undefined {
  if (values.length === 0) {
    return undefined;
  }
  const [first] = values;
  return values.every((value) => value === first) ? first : undefined;
}

/** The shared value as a select model, or "" when it is mixed or not offered. */
function optionValue<T>(
  agreed: T | undefined,
  options: ReadonlyArray<{ value: T }>
): string {
  if (agreed === undefined) {
    return "";
  }
  return options.some((option) => option.value === agreed)
    ? String(agreed)
    : "";
}

/** A finite number, or undefined when the text is empty or not a number. */
function parseNumber(text: string): number | undefined {
  const trimmed = text.trim();
  if (trimmed === "") {
    return undefined;
  }
  const value = Number(trimmed);
  return Number.isFinite(value) ? value : undefined;
}

/** The warning value a blank input keeps, when every check agrees on one. */
const keptWarnValue = computed<number | undefined>(() => {
  const agreed = agreedValue(
    snapshot.value.map((rule) =>
      (ruleToForm(rule).config.warnThreshold ?? "").trim()
    )
  );
  if (agreed === undefined || agreed === "") {
    return undefined;
  }
  return parseNumber(agreed);
});

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      reset();
    }
  },
  { immediate: true }
);

// A field switch never carries the previous field's value: the new editor is
// prefilled from the selection, or left for the operator when it disagrees.
watch(field, () => applyFieldDefaults());

// The unit and the amounts belong together: switching the metric drops the
// prefilled amounts instead of carrying them over as a silent reinterpretation.
// The prefill itself is not a switch.
watch(metricValue, (value, previous) => {
  if (
    field.value !== "threshold" ||
    value === previous ||
    value === prefilledMetric
  ) {
    return;
  }
  failThreshold.value = "";
  warnThreshold.value = "";
});

function reset() {
  snapshot.value = props.rules.slice(0, MAX_BATCH);
  fieldsReset();
  field.value = "";
  submitting.value = false;
  error.value = "";
  results.value = [];
  submittedRules.value = [];
  doneEmitted.value = false;
}

/** Every value control back to "nothing chosen". */
function fieldsReset() {
  severityValue.value = "";
  dimensionValue.value = "";
  stateValue.value = "";
  ownerValue.value = "";
  onEmptyValue.value = "";
  metricValue.value = "";
  prefilledMetric = "";
  failThreshold.value = "";
  warnThreshold.value = "";
  clearWarn.value = false;
  failPrefillMixed.value = false;
}

/**
 * Prefill the chosen field's value from the selection: a uniform value becomes
 * the current choice, a disagreement leaves the control unset so submit stays
 * disabled until the operator picks deliberately.
 */
function applyFieldDefaults() {
  fieldsReset();
  switch (field.value) {
    case "severity":
      severityValue.value = optionValue(
        agreedValue(snapshot.value.map((rule) => rule.severity)),
        severityOptions
      );
      break;
    case "dimension":
      dimensionValue.value = optionValue(
        agreedValue(snapshot.value.map((rule) => rule.dimension)),
        dimensionOptions
      );
      break;
    case "state":
      stateValue.value = optionValue(
        agreedValue(snapshot.value.map((rule) => rule.state)),
        stateOptions
      );
      break;
    case "owner": {
      const agreed = agreedValue(snapshot.value.map((rule) => rule.owner));
      ownerValue.value =
        agreed === undefined ? "" : agreed === "" ? UNASSIGNED_OWNER : agreed;
      break;
    }
    case "on_empty":
      onEmptyValue.value = optionValue(
        agreedValue(applicable.value.map((rule) => ruleToForm(rule).onEmpty)),
        onEmptyOptions
      );
      break;
    case "threshold": {
      const forms = snapshot.value.map((rule) => ruleToForm(rule));
      metricValue.value = optionValue(
        agreedValue(forms.map((form) => form.thresholdMetric)),
        metricOptions
      );
      prefilledMetric = metricValue.value;
      const agreedFail = agreedValue(
        forms.map((form) => (form.config.failThreshold ?? "").trim())
      );
      failPrefillMixed.value = agreedFail === undefined;
      failThreshold.value = agreedFail ?? "";
      // A blank warning means "keep each check's current one", so a mixed
      // selection prefills blank by design.
      warnThreshold.value =
        agreedValue(
          forms.map((form) => (form.config.warnThreshold ?? "").trim())
        ) ?? "";
      break;
    }
  }
}

function setClearWarn(value: boolean) {
  clearWarn.value = value;
  if (value) {
    warnThreshold.value = "";
  }
}

function ruleLabel(rule: QualityRule): string {
  return rule.title || rule.name.split("/").pop() || rule.name;
}

function failureTitle(index: number): string {
  const name = results.value[index]?.name;
  const rule =
    (name
      ? submittedRules.value.find((item) => item.name === name)
      : undefined) ?? submittedRules.value[index];
  return rule ? ruleLabel(rule) : "-";
}

/**
 * The sentence for one failed item. A concurrent definition edit is worth
 * naming as a retryable race; a rule that disappeared is worth naming as gone.
 * Every other code keeps the server's own wording, without its wire prefix.
 */
function failureMessage(message: string): string {
  const parsed = parseBatchItemError(message);
  if (parsed.code === Code.Aborted) {
    return t("dataQuality.checks.batchEdit.itemChanged");
  }
  if (parsed.code === Code.NotFound) {
    return t("dataQuality.checks.batchEdit.itemGone");
  }
  return parsed.message;
}

/** The scalar fields a batch can write through their own mask. */
interface ScalarPatch {
  severity?: QualitySeverity;
  dimension?: QualityDimension;
  state?: QualityRuleState;
  owner?: string;
}

/** One request item that writes one scalar field through its own mask. */
function scalarUpdate(
  rule: QualityRule,
  patch: ScalarPatch,
  path: string
): UpdateQualityRuleRequest {
  return create(UpdateQualityRuleRequestSchema, {
    rule: create(QualityRuleSchema, {
      name: rule.name,
      revision: rule.revision,
      ...patch,
    }),
    updateMask: create(FieldMaskSchema, { paths: [path] }),
  });
}

/** One request item that writes the whole config through the `config` mask. */
function configUpdate(
  rule: QualityRule,
  config: QualityRuleConfig
): UpdateQualityRuleRequest {
  return create(UpdateQualityRuleRequestSchema, {
    rule: create(QualityRuleSchema, {
      name: rule.name,
      revision: rule.revision,
      config,
    }),
    updateMask: create(FieldMaskSchema, { paths: ["config"] }),
  });
}

function scalarPatch(): ScalarPatch {
  switch (field.value) {
    case "severity":
      return {
        severity: Number(severityValue.value) as QualitySeverity,
      };
    case "dimension":
      return {
        dimension: Number(dimensionValue.value) as QualityDimension,
      };
    case "state":
      return { state: Number(stateValue.value) as QualityRuleState };
    case "owner":
      return {
        owner: ownerValue.value === UNASSIGNED_OWNER ? "" : ownerValue.value,
      };
    default:
      return {};
  }
}

function buildRequests(): {
  requests: UpdateQualityRuleRequest[];
  rules: QualityRule[];
} {
  if (field.value === "on_empty") {
    const rules = applicable.value;
    return {
      rules,
      requests: rules.map((rule) => {
        const form = ruleToForm(rule);
        form.onEmpty = Number(onEmptyValue.value) as QualityEmptyBehavior;
        return configUpdate(rule, buildRuleConfig(form));
      }),
    };
  }
  if (field.value === "threshold") {
    const metric = Number(metricValue.value) as QualityThresholdMetric;
    const fail = failThreshold.value.trim();
    const warn = warnThreshold.value.trim();
    // Keep, set or clear: a blank warn without the checkbox leaves each rule's
    // stored warning threshold untouched rather than dropping it to absent.
    const warnMode = clearWarn.value ? "clear" : warn === "" ? "keep" : "set";
    return {
      rules: [...snapshot.value],
      requests: snapshot.value.map((rule) => {
        const form = ruleToForm(rule);
        form.thresholdMetric = metric;
        form.config.failThreshold = fail;
        if (warnMode === "clear") {
          form.config.warnThreshold = "";
        } else if (warnMode === "set") {
          form.config.warnThreshold = warn;
        }
        return configUpdate(rule, buildRuleConfig(form));
      }),
    };
  }
  const path = field.value as Exclude<BatchEditField, "on_empty" | "threshold">;
  const patch = scalarPatch();
  return {
    rules: [...snapshot.value],
    requests: snapshot.value.map((rule) => scalarUpdate(rule, patch, path)),
  };
}

function submit() {
  if (!canSubmit.value) {
    return;
  }
  const { requests, rules } = buildRequests();
  if (requests.length === 0) {
    return;
  }
  submittedRules.value = rules;
  submitting.value = true;
  error.value = "";
  results.value = [];
  batchUpdateQualityRules(requests)
    .then((created) => {
      results.value = created;
      // Refresh as soon as the call settles: an all-fail batch changed nothing,
      // but its stale revisions still need replacing on the page behind.
      emitRefresh();
    })
    .catch((cause: unknown) => {
      error.value = formatError(cause, "dataQuality.checks.batchEdit.failed");
    })
    .finally(() => {
      submitting.value = false;
    });
}

/** The checks this submit updated; a failed item stays out of it. */
function succeededNames(): string[] {
  return results.value.flatMap((result, index) => {
    if (result.error) {
      return [];
    }
    const name = result.name || submittedRules.value[index]?.name;
    return name ? [name] : [];
  });
}

/** Tell the page to refresh, once; closing the dialog is not a second refresh. */
function emitRefresh() {
  if (doneEmitted.value) {
    return;
  }
  doneEmitted.value = true;
  emit("done", succeededNames());
}

function onOpenChange(value: boolean) {
  if (value || submitting.value) {
    return;
  }
  close();
}

function close() {
  emit("update:open", false);
}
</script>
