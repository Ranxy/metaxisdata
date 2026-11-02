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
                <span class="text-muted-foreground">— {{ failure.message }}</span>
              </li>
            </ul>
          </div>
        </template>

        <!-- One field at a time: every rule keeps its parameters, and the update
             mask names only the field the operator picked. -->
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
                <SelectValue />
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
                <SelectValue />
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
                <SelectValue />
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
                <SelectValue />
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
                <SelectValue />
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
              id="dq-batch-edit-fail-threshold"
              v-model="failThreshold"
              type="number"
              required
              :label="t('dataQuality.editor.failThreshold')"
              :hint="t('dataQuality.editor.thresholdHint')"
              :error="
                failThreshold.trim() !== '' && failValue === undefined
                  ? t('dataQuality.checks.batchEdit.thresholdInvalid')
                  : ''
              "
            />
            <FormField
              id="dq-batch-edit-warn-threshold"
              v-model="warnThreshold"
              type="number"
              :label="t('dataQuality.editor.warnThreshold')"
              :error="
                warnThreshold.trim() !== '' && warnValue === undefined
                  ? t('dataQuality.checks.batchEdit.thresholdInvalid')
                  : ''
              "
            />
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
import { AlertTriangle } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { batchUpdateQualityRules } from "@/api/data-quality";
import UserSelect from "@/components/common/UserSelect.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
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

/**
 * Edit one field across several checks at once.
 *
 * Every item names its own update mask and its rule's current revision, so a
 * check someone else changed is rejected instead of overwritten, and one
 * rejected item does not block the rest. The empty-result behavior only exists
 * for some rule types; those rows are reported as not applicable and left alone
 * rather than rewritten.
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
  /** The checks whose update succeeded, so the page can refresh. */
  done: [ruleNames: string[]];
}>();

const { t } = useI18n();
const { formatError } = useErrorMessage();

const snapshot = ref<QualityRule[]>([]);
const field = ref<BatchEditField>("severity");
const severityValue = ref(String(QualitySeverity.WARNING));
const dimensionValue = ref(String(QualityDimension.COMPLETENESS));
const stateValue = ref(String(QualityRuleState.ENABLED));
const ownerValue = ref("");
const onEmptyValue = ref(String(QualityEmptyBehavior.NOT_EVALUATED));
const metricValue = ref(String(QualityThresholdMetric.COUNT));
const failThreshold = ref("");
const warnThreshold = ref("");
const submitting = ref(false);
const error = ref("");
const results = ref<BatchUpdateQualityRuleResult[]>([]);
/** The rules this submit actually sent, so result names resolve to titles. */
const submittedRules = ref<QualityRule[]>([]);

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

const failValue = computed(() => parseThreshold(failThreshold.value));
const warnValue = computed(() => parseThreshold(warnThreshold.value));

const requestCount = computed(() =>
  field.value === "on_empty" ? applicable.value.length : snapshot.value.length
);

const canSubmit = computed(() => {
  if (submitting.value || snapshot.value.length === 0) {
    return false;
  }
  if (field.value === "on_empty") {
    return applicable.value.length > 0;
  }
  if (field.value === "owner") {
    return ownerValue.value !== "";
  }
  if (field.value === "threshold") {
    if (!thresholdAvailable.value || failValue.value === undefined) {
      return false;
    }
    return warnThreshold.value.trim() === "" || warnValue.value !== undefined;
  }
  return true;
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

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      reset();
    }
  },
  { immediate: true }
);

function reset() {
  snapshot.value = props.rules.slice(0, MAX_BATCH);
  field.value = "severity";
  severityValue.value = String(QualitySeverity.WARNING);
  dimensionValue.value = String(QualityDimension.COMPLETENESS);
  stateValue.value = String(QualityRuleState.ENABLED);
  ownerValue.value = "";
  onEmptyValue.value = String(QualityEmptyBehavior.NOT_EVALUATED);
  metricValue.value = String(QualityThresholdMetric.COUNT);
  failThreshold.value = "";
  warnThreshold.value = "";
  submitting.value = false;
  error.value = "";
  results.value = [];
  submittedRules.value = [];
}

/** A finite, non-negative number, or undefined when the text is not one. */
function parseThreshold(text: string): number | undefined {
  const trimmed = text.trim();
  if (trimmed === "") {
    return undefined;
  }
  const value = Number(trimmed);
  return Number.isFinite(value) && value >= 0 ? value : undefined;
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
    return {
      rules: [...snapshot.value],
      requests: snapshot.value.map((rule) => {
        const form = ruleToForm(rule);
        form.thresholdMetric = metric;
        form.config.failThreshold = fail;
        form.config.warnThreshold = warn;
        return configUpdate(rule, buildRuleConfig(form));
      }),
    };
  }
  const path = field.value;
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

function onOpenChange(value: boolean) {
  if (value || submitting.value) {
    return;
  }
  close();
}

function close() {
  const succeeded = succeededNames();
  emit("update:open", false);
  if (succeeded.length > 0) {
    emit("done", succeeded);
  }
}
</script>
