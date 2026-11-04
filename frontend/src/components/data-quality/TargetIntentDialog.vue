<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent class="max-h-[90vh] gap-0 overflow-hidden p-0 sm:max-w-2xl">
      <div class="flex max-h-[90vh] min-h-0 flex-col">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>
            {{ t("dataQuality.intent.title") }}
          </DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.intent.description") }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-6 overflow-auto px-6 py-5">
          <p
            v-if="warning"
            class="text-sm text-destructive"
          >
            {{ warning }}
          </p>
          <Alert
            v-if="error"
            variant="destructive"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>{{ t("dataQuality.intent.saveFailed") }}</AlertTitle>
            <AlertDescription class="break-words">
              {{ error }}
            </AlertDescription>
          </Alert>

          <section class="space-y-2">
            <div class="space-y-1">
              <Label for="dq-intent-criticality">
                {{ t("dataQuality.intent.criticality") }}
              </Label>
              <p class="text-sm text-muted-foreground">
                {{ t("dataQuality.intent.criticalityHint") }}
              </p>
            </div>
            <Select
              :model-value="String(criticality)"
              @update:model-value="setCriticality"
            >
              <SelectTrigger
                id="dq-intent-criticality"
                class="sm:max-w-xs"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="option in criticalityOptions"
                  :key="option.value"
                  :value="String(option.value)"
                >
                  {{ t(option.key) }}
                </SelectItem>
              </SelectContent>
            </Select>
            <div class="flex flex-col gap-1.5 sm:max-w-xs">
              <Label for="dq-intent-effective">
                {{ t("dataQuality.intent.effectiveFrom") }}
              </Label>
              <Input
                id="dq-intent-effective"
                v-model="effectiveFrom"
                type="date"
              />
            </div>
          </section>

          <!-- Promises: the properties this target commits to. -->
          <section class="space-y-3 border-t pt-4">
            <div class="space-y-1">
              <Label>{{ t("dataQuality.intent.promises") }}</Label>
              <p class="text-sm text-muted-foreground">
                {{ t("dataQuality.intent.promisesHint") }}
              </p>
            </div>
            <div
              v-for="(promise, index) in promises"
              :key="promise.key"
              class="flex flex-wrap items-end gap-2 rounded-md border p-2"
            >
              <div class="min-w-40 flex-1 space-y-1">
                <Label :for="`dq-intent-promise-property-${promise.key}`">
                  {{ t("dataQuality.intent.promiseProperty") }}
                </Label>
                <Select
                  :model-value="String(promise.property)"
                  @update:model-value="(value) => setPromiseProperty(index, value)"
                >
                  <SelectTrigger :id="`dq-intent-promise-property-${promise.key}`">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem
                      v-for="option in propertyOptions"
                      :key="option.value"
                      :value="String(option.value)"
                    >
                      {{ t(option.key) }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div class="min-w-32 flex-1 space-y-1">
                <Label :for="`dq-intent-promise-column-${promise.key}`">
                  {{ t("dataQuality.intent.promiseColumn") }}
                </Label>
                <Input
                  :id="`dq-intent-promise-column-${promise.key}`"
                  v-model="promise.column"
                  :placeholder="t('dataQuality.intent.promiseColumnPlaceholder')"
                />
              </div>
              <div class="min-w-40 flex-1 space-y-1">
                <Label :for="`dq-intent-promise-tolerance-${promise.key}`">
                  {{ t("dataQuality.intent.promiseTolerance") }}
                </Label>
                <Input
                  :id="`dq-intent-promise-tolerance-${promise.key}`"
                  v-model="promise.tolerance"
                  :placeholder="t('dataQuality.intent.promiseTolerancePlaceholder')"
                />
              </div>
              <Button
                variant="ghost"
                size="icon"
                :title="t('dataQuality.intent.remove')"
                :aria-label="t('dataQuality.intent.remove')"
                @click="removePromise(index)"
              >
                <Trash2 class="h-4 w-4" />
              </Button>
            </div>
            <p
              v-if="promises.length === 0"
              class="text-xs text-muted-foreground"
            >
              {{ t("dataQuality.intent.emptyPromises") }}
            </p>
            <Button
              variant="outline"
              size="sm"
              :disabled="saving"
              @click="addPromise"
            >
              <Plus class="mr-1 h-4 w-4" />
              {{ t("dataQuality.intent.addPromise") }}
            </Button>
          </section>

          <!-- Dependents: downstream consumers, recorded for impact review. -->
          <section class="space-y-3 border-t pt-4">
            <div class="space-y-1">
              <Label>{{ t("dataQuality.intent.dependents") }}</Label>
              <p class="text-sm text-muted-foreground">
                {{ t("dataQuality.intent.dependentsHint") }}
              </p>
            </div>
            <div
              v-for="(dependent, index) in dependents"
              :key="dependent.key"
              class="flex items-center gap-2"
            >
              <Input
                v-model="dependent.value"
                :placeholder="t('dataQuality.intent.dependentPlaceholder')"
              />
              <Button
                variant="ghost"
                size="icon"
                :title="t('dataQuality.intent.remove')"
                :aria-label="t('dataQuality.intent.remove')"
                @click="removeDependent(index)"
              >
                <Trash2 class="h-4 w-4" />
              </Button>
            </div>
            <p
              v-if="dependents.length === 0"
              class="text-xs text-muted-foreground"
            >
              {{ t("dataQuality.intent.emptyDependents") }}
            </p>
            <Button
              variant="outline"
              size="sm"
              :disabled="saving"
              @click="addDependent"
            >
              <Plus class="mr-1 h-4 w-4" />
              {{ t("dataQuality.intent.addDependent") }}
            </Button>
          </section>

          <!-- Exemptions: requirements this target explicitly waives. -->
          <section class="space-y-3 border-t pt-4">
            <div class="space-y-1">
              <Label>{{ t("dataQuality.intent.exemptions") }}</Label>
              <p class="text-sm text-muted-foreground">
                {{ t("dataQuality.intent.exemptionsHint") }}
              </p>
            </div>
            <div
              v-for="(exemption, index) in exemptions"
              :key="exemption.key"
              class="space-y-2 rounded-md border p-2"
            >
              <div class="flex flex-wrap items-end gap-2">
                <div class="min-w-40 flex-1 space-y-1">
                  <Label :for="`dq-intent-exemption-property-${exemption.key}`">
                    {{ t("dataQuality.intent.exemptionProperty") }}
                  </Label>
                  <Select
                    :model-value="String(exemption.property)"
                    @update:model-value="(value) => setExemptionProperty(index, value)"
                  >
                    <SelectTrigger :id="`dq-intent-exemption-property-${exemption.key}`">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem
                        v-for="option in propertyOptions"
                        :key="option.value"
                        :value="String(option.value)"
                      >
                        {{ t(option.key) }}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div class="min-w-32 flex-1 space-y-1">
                  <Label :for="`dq-intent-exemption-column-${exemption.key}`">
                    {{ t("dataQuality.intent.exemptionColumn") }}
                  </Label>
                  <Input
                    :id="`dq-intent-exemption-column-${exemption.key}`"
                    v-model="exemption.column"
                    :placeholder="t('dataQuality.intent.promiseColumnPlaceholder')"
                  />
                </div>
                <div class="w-40 space-y-1">
                  <Label :for="`dq-intent-exemption-expires-${exemption.key}`">
                    {{ t("dataQuality.intent.exemptionExpires") }}
                  </Label>
                  <Input
                    :id="`dq-intent-exemption-expires-${exemption.key}`"
                    v-model="exemption.expiresAt"
                    type="date"
                  />
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  :title="t('dataQuality.intent.remove')"
                  :aria-label="t('dataQuality.intent.remove')"
                  @click="removeExemption(index)"
                >
                  <Trash2 class="h-4 w-4" />
                </Button>
              </div>
              <Input
                v-model="exemption.reason"
                :placeholder="t('dataQuality.intent.exemptionReasonPlaceholder')"
                :aria-label="t('dataQuality.intent.exemptionReason')"
              />
              <p
                v-if="exemption.approvedBy"
                class="text-xs text-muted-foreground"
              >
                {{
                  t("dataQuality.intent.exemptionApprover", {
                    name: emailOf(exemption.approvedBy) || exemption.approvedBy,
                  })
                }}
              </p>
            </div>
            <p
              v-if="exemptions.length === 0"
              class="text-xs text-muted-foreground"
            >
              {{ t("dataQuality.intent.emptyExemptions") }}
            </p>
            <Button
              variant="outline"
              size="sm"
              :disabled="saving"
              @click="addExemption"
            >
              <Plus class="mr-1 h-4 w-4" />
              {{ t("dataQuality.intent.addExemption") }}
            </Button>
          </section>
        </div>

        <DialogFooter class="shrink-0 border-t px-6 py-4">
          <Button
            variant="outline"
            :disabled="saving"
            @click="close"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="saving || !contract"
            @click="save"
          >
            {{ t("dataQuality.intent.save") }}
          </Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { AlertTriangle, Plus, Trash2 } from "lucide-vue-next";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { updateQualityContract } from "@/api/data-quality";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useUserDirectory } from "@/composables/useUserDirectory";
import { notify } from "@/lib/notify";
import {
  QualityAssetCriticality,
  type QualityContract,
  QualityContractIntentExemptionSchema,
  QualityContractIntentSchema,
  QualityContractIntentSource,
  QualityContractIntentStatementSchema,
  QualityContractSettingsSchema,
  QualityCoverageProperty,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { qualityCoveragePropertyKey } from "@/utils/dataQuality";

/**
 * The target's quality intent: how critical it is, the properties it promises, its
 * dependents and the requirements it explicitly waives.
 *
 * The intent lives in the contract settings, which the server replaces wholesale, so
 * this dialog sends a complete settings message (filter and severity preserved) and
 * keeps the fields it does not edit (source, domain_ref) intact. Nothing here is
 * evaluated by a check: the plan derives its requirements from these promises, and an
 * exemption removes a gap without counting as covered.
 */

const props = defineProps<{
  open: boolean;
  targetGuid: string;
  metaType: MetaType;
  contract?: QualityContract;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  updated: [contract: QualityContract];
}>();

interface PromiseRow {
  key: number;
  property: QualityCoverageProperty;
  column: string;
  tolerance: string;
}

interface DependentRow {
  key: number;
  value: string;
}

interface ExemptionRow {
  key: number;
  property: QualityCoverageProperty;
  column: string;
  reason: string;
  expiresAt: string;
  approvedBy: string;
}

const { t } = useI18n();
const { formatError } = useErrorMessage();
const { emailOf, load: loadDirectory } = useUserDirectory();

const criticality = ref(QualityAssetCriticality.UNSPECIFIED);
const effectiveFrom = ref("");
const promises = ref<PromiseRow[]>([]);
const dependents = ref<DependentRow[]>([]);
const exemptions = ref<ExemptionRow[]>([]);
const error = ref("");
const warning = ref("");
const saving = ref(false);
let nextKey = 1;

const criticalityOptions = [
  QualityAssetCriticality.UNSPECIFIED,
  QualityAssetCriticality.EXPERIMENTAL,
  QualityAssetCriticality.IMPORTANT,
  QualityAssetCriticality.CRITICAL,
].map((value) => ({ value, key: criticalityKey(value) }));

const propertyOptions = [
  QualityCoverageProperty.VOLUME,
  QualityCoverageProperty.FRESHNESS,
  QualityCoverageProperty.NULL_RATE,
  QualityCoverageProperty.UNIQUENESS,
  QualityCoverageProperty.ACCEPTED_VALUES,
  QualityCoverageProperty.FORMAT,
  QualityCoverageProperty.RANGE,
  QualityCoverageProperty.SCHEMA_STABILITY,
].map((value) => ({ value, key: qualityCoveragePropertyKey(value) }));

function criticalityKey(value: QualityAssetCriticality): string {
  switch (value) {
    case QualityAssetCriticality.CRITICAL:
      return "dataQuality.workspace.planCriticalityCritical";
    case QualityAssetCriticality.IMPORTANT:
      return "dataQuality.workspace.planCriticalityImportant";
    case QualityAssetCriticality.EXPERIMENTAL:
      return "dataQuality.workspace.planCriticalityExperimental";
    default:
      return "dataQuality.intent.criticalityUnset";
  }
}

/** A date input value from a stored millisecond instant; empty when unset. */
function dateInputValue(timestamp?: Timestamp): string {
  if (!timestamp || timestamp.seconds === 0n) {
    return "";
  }
  return new Date(Number(timestamp.seconds) * 1000).toISOString().slice(0, 10);
}

/** A stored instant from a date input value; undefined when unset. */
function timestampFromInput(value: string): Timestamp | undefined {
  if (!value) {
    return undefined;
  }
  return timestampFromDate(new Date(`${value}T00:00:00Z`));
}

function reset() {
  const intent = props.contract?.settings?.intent;
  criticality.value =
    intent?.criticality ?? QualityAssetCriticality.UNSPECIFIED;
  effectiveFrom.value = dateInputValue(intent?.effectiveFrom);
  promises.value = (intent?.statements ?? []).map((statement) => ({
    key: nextKey++,
    property: statement.property || QualityCoverageProperty.VOLUME,
    column: statement.column,
    tolerance: statement.tolerance,
  }));
  dependents.value = (intent?.dependents ?? []).map((value) => ({
    key: nextKey++,
    value,
  }));
  exemptions.value = (intent?.exemptions ?? []).map((exemption) => ({
    key: nextKey++,
    property: exemption.property || QualityCoverageProperty.VOLUME,
    column: exemption.column,
    reason: exemption.reason,
    expiresAt: dateInputValue(exemption.expiresAt),
    approvedBy: exemption.approvedBy,
  }));
  error.value = "";
  warning.value = "";
  saving.value = false;
}

function addPromise() {
  warning.value = "";
  promises.value.push({
    key: nextKey++,
    property: QualityCoverageProperty.VOLUME,
    column: "",
    tolerance: "",
  });
}

function removePromise(index: number) {
  warning.value = "";
  promises.value.splice(index, 1);
}

function setPromiseProperty(index: number, value: unknown) {
  warning.value = "";
  const row = promises.value[index];
  if (row) {
    row.property = Number(value) as QualityCoverageProperty;
  }
}

function addDependent() {
  dependents.value.push({ key: nextKey++, value: "" });
}

function removeDependent(index: number) {
  dependents.value.splice(index, 1);
}

function addExemption() {
  warning.value = "";
  exemptions.value.push({
    key: nextKey++,
    property: QualityCoverageProperty.VOLUME,
    column: "",
    reason: "",
    expiresAt: "",
    approvedBy: "",
  });
}

function removeExemption(index: number) {
  warning.value = "";
  exemptions.value.splice(index, 1);
}

function setExemptionProperty(index: number, value: unknown) {
  const row = exemptions.value[index];
  if (row) {
    row.property = Number(value) as QualityCoverageProperty;
  }
}

function setCriticality(value: unknown) {
  criticality.value = Number(value) as QualityAssetCriticality;
}

/** The same property and column promised twice; whitespace does not make it new. */
function duplicatePromise(): boolean {
  const seen = new Set<string>();
  for (const promise of promises.value) {
    const key = `${promise.property}|${promise.column.trim()}`;
    if (seen.has(key)) {
      return true;
    }
    seen.add(key);
  }
  return false;
}

watch(
  () => props.open,
  (isOpen) => {
    reset();
    if (isOpen) {
      void loadDirectory();
    }
  },
  { immediate: true }
);

function close() {
  emit("update:open", false);
}

function onOpenChange(value: boolean) {
  if (value) {
    return;
  }
  close();
}

function buildSettings() {
  const current = props.contract?.settings;
  const intent = props.contract?.settings?.intent;
  return create(QualityContractSettingsSchema, {
    filter: current?.filter,
    defaultSeverity: current?.defaultSeverity,
    intent: create(QualityContractIntentSchema, {
      criticality: criticality.value,
      statements: promises.value.map((promise) =>
        create(QualityContractIntentStatementSchema, {
          property: promise.property,
          column: promise.column.trim(),
          tolerance: promise.tolerance.trim(),
        })
      ),
      dependents: dependents.value
        .map((dependent) => dependent.value.trim())
        .filter((value) => value !== ""),
      exemptions: exemptions.value.map((exemption) =>
        create(QualityContractIntentExemptionSchema, {
          property: exemption.property,
          column: exemption.column.trim(),
          reason: exemption.reason.trim(),
          expiresAt: timestampFromInput(exemption.expiresAt),
        })
      ),
      effectiveFrom: timestampFromInput(effectiveFrom.value),
      // Fields this dialog does not edit are preserved rather than cleared by the
      // wholesale settings replacement.
      source: intent?.source ?? QualityContractIntentSource.UNSPECIFIED,
      domainRef: intent?.domainRef ?? "",
    }),
  });
}

async function save() {
  if (saving.value || !props.contract) {
    return;
  }
  if (duplicatePromise()) {
    warning.value = t("dataQuality.intent.duplicate");
    return;
  }
  if (exemptions.value.some((exemption) => exemption.reason.trim() === "")) {
    warning.value = t("dataQuality.intent.reasonRequired");
    return;
  }
  saving.value = true;
  error.value = "";
  warning.value = "";
  try {
    const updated = await updateQualityContract({
      targetGuid: props.targetGuid,
      metaType: props.metaType,
      settings: buildSettings(),
    });
    notify.success(t("dataQuality.intent.saved"));
    emit("updated", updated);
    emit("update:open", false);
  } catch (e: unknown) {
    error.value = formatError(e, "dataQuality.intent.saveFailed");
  } finally {
    saving.value = false;
  }
}
</script>
