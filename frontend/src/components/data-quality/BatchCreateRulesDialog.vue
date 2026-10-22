<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent class="max-h-[90vh] gap-0 overflow-hidden p-0 sm:max-w-3xl">
      <div class="flex max-h-[90vh] min-h-0 flex-col">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>
            {{ t("dataQuality.suggestions.batchCreate.title") }}
          </DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.suggestions.batchCreate.description") }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-5 overflow-auto px-6 py-5">
          <Alert
            v-if="unsupportedCount > 0"
            variant="warning"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>{{ t("dataQuality.suggestions.batchCreate.unsupportedTitle") }}</AlertTitle>
            <AlertDescription>
              {{ t("dataQuality.suggestions.batchCreate.unsupported", { count: unsupportedCount }) }}
            </AlertDescription>
          </Alert>

          <Alert
            v-if="error"
            variant="destructive"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>{{ t("dataQuality.suggestions.batchCreate.failed") }}</AlertTitle>
            <AlertDescription class="break-words">
              {{ error }}
            </AlertDescription>
          </Alert>

          <!-- Common settings: applied to every selected rule unless a row is
               expanded and overrides its own title, severity or config. -->
          <section
            v-if="!summary"
            class="space-y-3 rounded-md border p-3"
          >
            <div class="text-sm font-medium">
              {{ t("dataQuality.suggestions.batchCreate.commonTitle") }}
            </div>
            <div class="grid gap-3 sm:grid-cols-3">
              <div class="flex flex-col gap-1.5">
                <Label for="dq-batch-interval">
                  {{ t("dataQuality.suggestions.batchCreate.interval") }}
                </Label>
                <Select v-model="interval">
                  <SelectTrigger id="dq-batch-interval">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem
                      v-for="value in INTERVAL_VALUES"
                      :key="value"
                      :value="value"
                    >
                      {{ intervalLabel(value) }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div class="flex flex-col gap-1.5">
                <Label for="dq-batch-severity">
                  {{ t("dataQuality.suggestions.batchCreate.severity") }}
                </Label>
                <Select v-model="severity">
                  <SelectTrigger id="dq-batch-severity">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="keep">
                      {{ t("dataQuality.suggestions.batchCreate.severityKeep") }}
                    </SelectItem>
                    <SelectItem :value="String(Severity.WARNING)">
                      {{ t("dataQuality.severity.warning") }}
                    </SelectItem>
                    <SelectItem :value="String(Severity.ERROR)">
                      {{ t("dataQuality.severity.error") }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <UserSelect
                id="dq-batch-owner"
                v-model="owner"
                :label="t('dataQuality.suggestions.batchCreate.owner')"
                :hint="t('dataQuality.suggestions.batchCreate.ownerHint')"
              />
            </div>
          </section>

          <!-- Success/failure panel: the server is the authority on compile
               errors, so failures carry its own message. -->
          <section
            v-if="summary"
            class="space-y-3"
          >
            <Alert :variant="summary.failed.length > 0 ? 'warning' : 'success'">
              <AlertTriangle
                v-if="summary.failed.length > 0"
                class="h-4 w-4"
              />
              <AlertTitle>
                {{
                  summary.failed.length > 0
                    ? t("dataQuality.suggestions.batchCreate.partial", {
                        succeeded: summary.succeeded,
                        failed: summary.failed.length,
                      })
                    : t("dataQuality.suggestions.batchCreate.success", {
                        count: summary.succeeded,
                      })
                }}
              </AlertTitle>
            </Alert>
            <div
              v-if="createdRules.length > 0"
              class="space-y-2"
            >
              <div class="text-sm font-medium">
                {{ t("dataQuality.suggestions.batchCreate.createdRules") }}
              </div>
              <ul class="space-y-1 text-sm">
                <li
                  v-for="rule in createdRules"
                  :key="rule.name"
                >
                  <RouterLink
                    :to="ruleRoute(rule.name)"
                    class="text-primary hover:underline"
                  >
                    {{ rule.title || rule.name.split("/").pop() }}
                  </RouterLink>
                </li>
              </ul>
            </div>
            <div
              v-if="summary.failed.length > 0"
              class="space-y-2"
            >
              <div class="text-sm font-medium">
                {{ t("dataQuality.suggestions.batchCreate.failures") }}
              </div>
              <ul class="space-y-1 text-sm">
                <li
                  v-for="failure in summary.failed"
                  :key="failure.index"
                  class="break-words"
                >
                  <span class="font-medium">{{ rowTitle(failure.index) }}</span>
                  <span class="text-muted-foreground">— {{ failure.message }}</span>
                </li>
              </ul>
            </div>
          </section>

          <section
            v-if="!summary"
            class="space-y-2"
          >
            <div class="flex items-center justify-between gap-2">
              <div class="text-sm font-medium">
                {{ t("dataQuality.suggestions.batchCreate.rowsTitle", { count: rows.length }) }}
              </div>
              <p class="text-xs text-muted-foreground">
                {{ t("dataQuality.suggestions.batchCreate.configHint") }}
              </p>
            </div>
            <div
              v-for="(row, index) in rows"
              :key="row.suggestion.name"
              class="rounded-md border"
            >
              <div class="flex flex-wrap items-start justify-between gap-2 p-3">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <Badge
                      v-if="rowTypeKey(row)"
                      variant="secondary"
                    >
                      {{ t(rowTypeKey(row) ?? "") }}
                    </Badge>
                    <span class="truncate text-sm font-medium">
                      {{ row.title.trim() || t("dataQuality.suggestions.batchCreate.titleRequired") }}
                    </span>
                    <Badge :variant="qualitySeverityVariant(row.severity)">
                      {{ t(severityKey(row.severity)) }}
                    </Badge>
                  </div>
                  <div class="truncate text-xs text-muted-foreground">
                    {{ rowTargetLabel(row) }}
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  @click="row.expanded = !row.expanded"
                >
                  <ChevronDown
                    class="mr-1 h-4 w-4 transition-transform"
                    :class="{ 'rotate-180': row.expanded }"
                  />
                  {{
                    row.expanded
                      ? t("dataQuality.suggestions.batchCreate.collapse")
                      : t("dataQuality.suggestions.batchCreate.expand")
                  }}
                </Button>
              </div>
              <div
                v-if="row.expanded"
                class="space-y-4 border-t p-3"
              >
                <FormField
                  :id="`dq-batch-title-${index}`"
                  v-model="row.title"
                  required
                  :label="t('dataQuality.suggestions.batchCreate.rowTitle')"
                />
                <div class="flex max-w-xs flex-col gap-1.5">
                  <Label :for="`dq-batch-row-severity-${index}`">
                    {{ t("dataQuality.suggestions.batchCreate.rowSeverity") }}
                  </Label>
                  <Select
                    :model-value="String(row.severity)"
                    @update:model-value="(value: unknown) => setRowSeverity(row, value)"
                  >
                    <SelectTrigger :id="`dq-batch-row-severity-${index}`">
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
                <RuleConfigForm
                  v-model="row.form.config"
                  v-model:threshold-metric="row.form.thresholdMetric"
                  :rule-type="row.form.ruleType"
                  :column-details="row.columns"
                />
              </div>
            </div>
          </section>
        </div>

        <DialogFooter class="shrink-0 border-t px-6 py-4">
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
                ? t("dataQuality.suggestions.batchCreate.submitting")
                : t("dataQuality.suggestions.batchCreate.submit", {
                    count: rows.length,
                  })
            }}
          </Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { AlertTriangle, ChevronDown } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { batchCreateQualityRules } from "@/api/data-quality";
import { listMetadata } from "@/api/database";
import UserSelect from "@/components/common/UserSelect.vue";
import RuleConfigForm from "@/components/data-quality/RuleConfigForm.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
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
import { notify } from "@/lib/notify";
import {
  type BatchCreateQualityRuleResult,
  type QualityRule,
  type QualitySeverity,
  type QualitySuggestion,
  QualitySeverity as Severity,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  qualityRuleTypeKey,
  qualitySeverityKey,
  qualitySeverityVariant,
} from "@/utils/dataQuality";
import type {
  RuleFormState,
  TargetColumnDetail,
} from "@/utils/dataQualityForm";
import {
  buildRuleFromSuggestion,
  MAX_SUGGESTION_BATCH,
  type SuggestionBatchOverrides,
  suggestionTargetLabel,
  suggestionToEditorPrefill,
  summarizeBatchResults,
} from "@/utils/dataQualitySuggestion";

/**
 * Create rules from several suggestions in one submit.
 *
 * Common settings cover the usual batch; a row can be expanded to edit its own
 * title, severity and typed config before submitting. The server validates and
 * compiles each rule independently, so partial failures are reported per row
 * instead of failing the whole batch.
 */

const props = withDefaults(
  defineProps<{
    open: boolean;
    /** The rule-draft suggestions to turn into rules. */
    suggestions?: QualitySuggestion[];
  }>(),
  { suggestions: () => [] }
);

const emit = defineEmits<{
  "update:open": [value: boolean];
  /** The accepted suggestions, after at least one rule was created. */
  done: [suggestionNames: string[]];
}>();

const { t } = useI18n();
const { formatError } = useErrorMessage();

type ScheduleInterval =
  | "default"
  | "900"
  | "1800"
  | "3600"
  | "21600"
  | "43200"
  | "86400"
  | "604800";

const INTERVAL_VALUES: ScheduleInterval[] = [
  "default",
  "900",
  "1800",
  "3600",
  "21600",
  "43200",
  "86400",
  "604800",
];

/** The interval select's label; static t() keys so both i18n checks see them. */
function intervalLabel(value: ScheduleInterval): string {
  switch (value) {
    case "default":
      return t("dataQuality.suggestions.batchCreate.intervalDefault");
    case "900":
      return t("dataQuality.suggestions.batchCreate.interval15m");
    case "1800":
      return t("dataQuality.suggestions.batchCreate.interval30m");
    case "3600":
      return t("dataQuality.suggestions.batchCreate.interval1h");
    case "21600":
      return t("dataQuality.suggestions.batchCreate.interval6h");
    case "43200":
      return t("dataQuality.suggestions.batchCreate.interval12h");
    case "86400":
      return t("dataQuality.suggestions.batchCreate.interval1d");
    case "604800":
      return t("dataQuality.suggestions.batchCreate.interval7d");
  }
}

const severityOptions: Array<{ value: QualitySeverity; key: string }> = [
  { value: Severity.WARNING, key: "dataQuality.severity.warning" },
  { value: Severity.ERROR, key: "dataQuality.severity.error" },
  { value: Severity.CRITICAL, key: "dataQuality.severity.critical" },
];

interface BatchRow {
  suggestion: QualitySuggestion;
  form: RuleFormState;
  title: string;
  severity: QualitySeverity;
  /** Whether the row's own severity was picked, so it beats the common one. */
  severityTouched: boolean;
  expanded: boolean;
  columns: TargetColumnDetail[];
}

const interval = ref<ScheduleInterval>("default");
const severity = ref("keep");
const owner = ref("");
const rows = ref<BatchRow[]>([]);
const unsupportedCount = ref(0);
const submitting = ref(false);
const error = ref("");
const results = ref<BatchCreateQualityRuleResult[]>([]);

const summary = computed(() =>
  results.value.length > 0 ? summarizeBatchResults(results.value) : undefined
);

const createdRules = computed<QualityRule[]>(() =>
  results.value
    .map((result) => result.rule)
    .filter((rule): rule is QualityRule => rule !== undefined)
);

const canSubmit = computed(
  () =>
    !submitting.value &&
    unsupportedCount.value === 0 &&
    rows.value.length > 0 &&
    rows.value.every((row) => row.title.trim() !== "")
);

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
  interval.value = "default";
  severity.value = "keep";
  owner.value = "";
  rows.value = [];
  unsupportedCount.value = 0;
  submitting.value = false;
  error.value = "";
  results.value = [];
  for (const suggestion of props.suggestions.slice(0, MAX_SUGGESTION_BATCH)) {
    const prefill = suggestionToEditorPrefill(suggestion);
    if (!prefill?.target) {
      unsupportedCount.value += 1;
      continue;
    }
    rows.value.push({
      suggestion,
      form: prefill.form,
      title: prefill.form.title,
      severity:
        prefill.form.severity === Severity.UNSPECIFIED
          ? Severity.WARNING
          : prefill.form.severity,
      severityTouched: false,
      expanded: false,
      columns: [],
    });
  }
  void loadColumns();
}

/**
 * Column details make the config form's column pickers and the schema-drift
 * snapshot usable; a failure is silent because free text still works.
 */
async function loadColumns() {
  const guids = [
    ...new Set(
      rows.value
        .map((row) => row.suggestion.target?.metaGuid ?? "")
        .filter(Boolean)
    ),
  ];
  for (const guid of guids) {
    try {
      const response = await listMetadata({ parentGuid: guid, pageSize: 200 });
      const group = response.typesStoredMetadata.find(
        (item) => item.metaType === MetaType.COLUMN
      );
      const details: TargetColumnDetail[] =
        group?.list.map((item) => {
          const value = item.type.value as
            | { name?: string; type?: string; nullable?: boolean }
            | undefined;
          return {
            name: value?.name ?? item.guid,
            type: value?.type ?? "",
            nullable: value?.nullable ?? true,
          };
        }) ?? [];
      for (const row of rows.value) {
        if ((row.suggestion.target?.metaGuid ?? "") === guid) {
          row.columns = details;
        }
      }
    } catch {
      // The form still accepts typed column names.
    }
  }
}

function rowTypeKey(row: BatchRow): string {
  // The rule type is the form's, which the prefill resolved from the config arm.
  return qualityRuleTypeKey(row.form.ruleType);
}

function rowTargetLabel(row: BatchRow): string {
  return suggestionTargetLabel(row.suggestion.target?.metaGuid ?? "");
}

function rowTitle(index: number): string {
  const row = rows.value[index];
  return row?.title.trim() || row?.suggestion.name.split("/").pop() || "-";
}

function severityKey(value: QualitySeverity): string {
  return qualitySeverityKey(value);
}

function setRowSeverity(row: BatchRow, value: unknown) {
  row.severity = Number(value) as QualitySeverity;
  row.severityTouched = true;
}

/** The common overrides, minus severity (a row's own choice wins). */
function commonOverrides(): SuggestionBatchOverrides {
  const overrides: SuggestionBatchOverrides = { owner: owner.value };
  if (interval.value !== "default") {
    overrides.scheduleIntervalSeconds = Number(interval.value);
  }
  if (severity.value !== "keep") {
    overrides.severity = Number(severity.value);
  }
  return overrides;
}

function submit() {
  if (!canSubmit.value) {
    return;
  }
  const overrides = commonOverrides();
  const items = rows.value
    .map((row) => {
      const rule = buildRuleFromSuggestion({
        suggestion: row.suggestion,
        overrides,
        form: { ...row.form, title: row.title.trim() },
      });
      if (!rule) {
        return undefined;
      }
      if (row.severityTouched) {
        rule.severity = row.severity;
      }
      return { rule, suggestion: row.suggestion.name };
    })
    .filter(
      (item): item is { rule: QualityRule; suggestion: string } =>
        item !== undefined
    );
  if (items.length === 0) {
    return;
  }
  submitting.value = true;
  error.value = "";
  results.value = [];
  batchCreateQualityRules(items)
    .then((created) => {
      results.value = created;
      const succeeded = summarizeBatchResults(created).succeeded;
      if (succeeded > 0) {
        notify.success(
          t("dataQuality.suggestions.batchCreate.success", {
            count: succeeded,
          })
        );
      }
    })
    .catch((e: unknown) => {
      error.value = formatError(
        e,
        "dataQuality.suggestions.batchCreate.failed"
      );
    })
    .finally(() => {
      submitting.value = false;
    });
}

function ruleRoute(name: string) {
  return {
    name: "DataQualityRuleDetail",
    params: { ruleId: name.split("/") },
  };
}

function onOpenChange(value: boolean) {
  if (value) {
    return;
  }
  if (submitting.value) {
    return;
  }
  close();
}

/** Only the suggestions that actually became rules leave the selection. */
function succeededSuggestionNames(): string[] {
  return results.value.flatMap((result, index) => {
    if (result.error) {
      return [];
    }
    const name = rows.value[index]?.suggestion.name;
    return name ? [name] : [];
  });
}

function close() {
  const succeeded = succeededSuggestionNames();
  emit("update:open", false);
  if (succeeded.length > 0) {
    emit("done", succeeded);
  }
}
</script>
