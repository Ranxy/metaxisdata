<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent class="max-h-[90vh] gap-0 overflow-hidden p-0 sm:max-w-4xl">
      <div class="flex max-h-[90vh] min-h-0 flex-col">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>{{ title }}</DialogTitle>
          <DialogDescription>
            {{
              suggestion
                ? t("dataQuality.editor.suggestionPrefilledHint", { title: suggestionTitle })
                : t("dataQuality.composer.description")
            }}
          </DialogDescription>
        </DialogHeader>

        <!-- Two panes inside the dialog: the check catalogue on the left, the
             check's form on the right. Each scrolls on its own, so the type list
             stays reachable while a long parameter form is scrolled. -->
        <div class="flex min-h-0 flex-1 overflow-hidden">
          <aside class="w-64 shrink-0 space-y-4 overflow-auto border-r px-3 py-4">
            <p class="px-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
              {{ t("dataQuality.composer.chooseType") }}
            </p>
            <div
              v-for="group in checkGroups"
              :key="group.id"
              class="space-y-1"
            >
              <p class="px-2 text-xs font-medium text-muted-foreground">
                {{ groupLabels[group.id] }}
              </p>
              <button
                v-for="ruleType in group.types"
                :key="ruleType"
                type="button"
                :data-rule-type="ruleType"
                :aria-pressed="ruleType === form.ruleType"
                class="w-full rounded-md px-2 py-1.5 text-left transition-colors"
                :class="
                  ruleType === form.ruleType
                    ? 'bg-primary/10 ring-1 ring-primary/40'
                    : 'hover:bg-muted'
                "
                @click="changeRuleType(ruleType)"
              >
                <span class="block text-sm font-medium">
                  {{ t(qualityRuleTypeKey(ruleType)) }}
                </span>
                <span class="mt-0.5 block text-xs text-muted-foreground">
                  {{ t(qualityRuleTypeDescriptionKey(ruleType)) }}
                </span>
              </button>
            </div>
          </aside>

          <div class="min-h-0 flex-1 space-y-5 overflow-auto px-6 py-5">
            <!-- The target is fixed by the page; an unusable GUID can only come
                 from a malformed route, so the dialog says so and disables save. -->
            <Alert
              v-if="!target"
              variant="destructive"
            >
              <AlertTriangle class="h-4 w-4" />
              <AlertTitle>{{ t("dataQuality.editor.targetTitle") }}</AlertTitle>
              <AlertDescription>
                {{ t("dataQuality.editor.targetEmpty") }}
              </AlertDescription>
            </Alert>

            <Alert
              v-if="issues.length > 0"
              variant="destructive"
            >
              <AlertTriangle class="h-4 w-4" />
              <AlertTitle>{{ t("dataQuality.editor.validationTitle") }}</AlertTitle>
              <AlertDescription>
                <ul class="list-disc space-y-1 pl-5">
                  <li
                    v-for="(issue, index) in issues"
                    :key="`${issue.key}-${index}`"
                  >
                    {{ t(issue.key, issue.params ?? {}) }}
                  </li>
                </ul>
              </AlertDescription>
            </Alert>

            <Alert
              v-if="saveError"
              variant="destructive"
            >
              <AlertTriangle class="h-4 w-4" />
              <AlertTitle>{{ t("dataQuality.editor.saveFailed") }}</AlertTitle>
              <AlertDescription class="break-words">
                {{ saveError }}
              </AlertDescription>
            </Alert>

            <div class="space-y-1">
              <h3 class="text-base font-medium">
                {{ t(qualityRuleTypeKey(form.ruleType)) }}
              </h3>
              <p
                v-if="selectedTypeDescription"
                class="text-sm text-muted-foreground"
              >
                {{ selectedTypeDescription }}
              </p>
              <p
                v-if="isViewTarget"
                class="text-sm text-muted-foreground"
              >
                {{ t("dataQuality.editor.ruleTypeViewHint") }}
              </p>
            </div>

            <section class="grid gap-4 sm:grid-cols-2">
              <div class="sm:col-span-2">
                <FormField
                  id="dq-composer-title"
                  v-model="form.title"
                  required
                  :label="t('dataQuality.editor.title')"
                  :placeholder="t('dataQuality.editor.titlePlaceholder')"
                />
              </div>
              <div class="space-y-2 sm:col-span-2">
                <Label for="dq-composer-description">
                  {{ t("dataQuality.editor.description") }}
                </Label>
                <textarea
                  id="dq-composer-description"
                  v-model="form.description"
                  class="min-h-20 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                  :placeholder="t('dataQuality.editor.descriptionPlaceholder')"
                />
              </div>
            </section>

            <!-- How the check is scheduled and graded: the fields every check has,
                 independent of its type. -->
            <section class="space-y-3 border-t pt-4">
              <h3 class="text-base font-medium">
                {{ t("dataQuality.editor.basicsTitle") }}
              </h3>
              <div class="grid gap-4 sm:grid-cols-2">
                <div class="flex flex-col gap-1.5">
                  <Label for="dq-composer-severity">
                    {{ t("dataQuality.filterSeverity") }}
                  </Label>
                  <Select
                    :model-value="String(form.severity)"
                    @update:model-value="setSeverity"
                  >
                    <SelectTrigger id="dq-composer-severity">
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

                <div class="flex flex-col gap-1.5">
                  <Label for="dq-composer-dimension">
                    {{ t("dataQuality.filterDimension") }}
                  </Label>
                  <Select
                    :model-value="String(form.dimension)"
                    @update:model-value="setDimension"
                  >
                    <SelectTrigger id="dq-composer-dimension">
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

                <div class="flex flex-col gap-1.5 sm:col-span-2">
                  <Label for="dq-composer-schedule">
                    {{ t("dataQuality.editor.schedule") }}
                  </Label>
                  <div class="flex gap-2">
                    <FormField
                      id="dq-composer-schedule"
                      v-model="form.schedule.value"
                      type="number"
                      class="min-w-0 flex-1"
                    />
                    <Select
                      :model-value="form.schedule.unit"
                      @update:model-value="setScheduleUnit"
                    >
                      <SelectTrigger
                        id="dq-composer-schedule-unit"
                        class="w-32"
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="minutes">
                          {{ t("dataQuality.editor.unitMinutes") }}
                        </SelectItem>
                        <SelectItem value="hours">
                          {{ t("dataQuality.editor.unitHours") }}
                        </SelectItem>
                        <SelectItem value="days">
                          {{ t("dataQuality.editor.unitDays") }}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <p class="text-sm text-muted-foreground">
                    {{ t("dataQuality.editor.scheduleHint") }}
                  </p>
                </div>
              </div>
            </section>

            <!-- The typed parameters, through the same controlled form the
                 editor used; its threshold-unit select appears only where the
                 check type actually has a row total to measure against. -->
            <section class="space-y-3 border-t pt-4">
              <div class="space-y-1">
                <h3 class="text-base font-medium">
                  {{ t("dataQuality.editor.configTitle") }}
                </h3>
                <p class="text-sm text-muted-foreground">
                  {{ t("dataQuality.editor.configDescription") }}
                </p>
              </div>
              <RuleConfigForm
                v-model="form.config"
                v-model:threshold-metric="form.thresholdMetric"
                :rule-type="form.ruleType"
                :column-details="columns"
              />
            </section>

            <!-- Everything that is not a parameter of the check itself. Kept
                 collapsed so the common case stays short; an edit that already
                 carries any of them opens it. -->
            <details
              class="rounded-md border"
              :open="extraOpen"
              @toggle="onDetailsToggle"
            >
              <summary class="cursor-pointer px-4 py-3 text-sm font-medium select-none">
                {{ t("common.details") }}
              </summary>
              <div class="space-y-5 border-t px-4 py-4">
                <RuleFilterEditor
                  v-model="form.filter"
                  :columns="columns"
                />

                <UserSelect
                  id="dq-composer-owner"
                  v-model="form.owner"
                  :label="t('dataQuality.editor.owner')"
                  :hint="t('dataQuality.editor.ownerHint')"
                />

                <FormField
                  id="dq-composer-tags"
                  v-model="form.tags"
                  :label="t('dataQuality.editor.tags')"
                  :hint="t('dataQuality.editor.tagsHint')"
                />

                <div
                  v-if="isOnEmptyRelevant(form.ruleType)"
                  class="max-w-sm space-y-1.5"
                >
                  <Label for="dq-composer-on-empty">
                    {{ t("dataQuality.editor.onEmpty") }}
                  </Label>
                  <Select
                    :model-value="String(form.onEmpty)"
                    @update:model-value="setOnEmpty"
                  >
                    <SelectTrigger id="dq-composer-on-empty">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem
                        v-for="behavior in EMPTY_BEHAVIORS"
                        :key="behavior"
                        :value="String(behavior)"
                      >
                        {{ t(qualityEmptyBehaviorKey(behavior)) }}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <p class="text-sm text-muted-foreground">
                    {{ t("dataQuality.editor.onEmptyHint") }}
                  </p>
                </div>
              </div>
            </details>
          </div>
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
            :disabled="saving || !target || !canSave"
            @click="save"
          >
            {{
              saving
                ? t("dataQuality.composer.saving")
                : isEdit
                  ? t("dataQuality.editor.saveChanges")
                  : t("dataQuality.editor.create")
            }}
          </Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { AlertTriangle } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  createQualityRule,
  previewQualityRule,
  updateQualityRule,
} from "@/api/data-quality";
import { listAllMetadata } from "@/api/database";
import UserSelect from "@/components/common/UserSelect.vue";
import RuleConfigForm from "@/components/data-quality/RuleConfigForm.vue";
import RuleFilterEditor from "@/components/data-quality/RuleFilterEditor.vue";
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
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityDimension,
  QualityEmptyBehavior,
  type QualityRule,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  type QualitySuggestion,
  QualityThresholdMetric,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  qualityDimensionKey,
  qualityEmptyBehaviorKey,
  qualityRuleTypeDescriptionKey,
  qualityRuleTypeKey,
  qualitySeverityKey,
} from "@/utils/dataQuality";
import {
  allowedRuleTypes,
  computeUpdateMask,
  durationToSchedule,
  emptyConfig,
  emptyRuleForm,
  formToRule,
  isOnEmptyRelevant,
  isThresholdMetricType,
  type RuleFormState,
  ruleToForm,
  type ScheduleUnit,
  suggestedDimension,
  type TargetColumnDetail,
  type TargetSelection,
  targetFromQuery,
  targetSelectionFromRule,
  type ValidationIssue,
  validateRuleForm,
} from "@/utils/dataQualityForm";
import { parseRuleError } from "@/utils/dataQualitySchema";
import { suggestionToEditorPrefill } from "@/utils/dataQualitySuggestion";

/**
 * The one authoring surface for a quality check: a dialog fixed to the target the
 * workspace already names.
 *
 * It creates and edits checks, and it accepts an AI draft (the suggestion's
 * prefill becomes the form, and saving accepts the suggestion in the same
 * request). The target is never picked here; filters, owner, tags and the empty
 * result behaviour live in the collapsible details section rather than a separate
 * editor page.
 *
 * With `rule` set the dialog edits that rule instead: the form starts from it and
 * saving sends only the fields that actually changed, so a rename does not rewrite
 * the config, and an edit with no change sends nothing at all.
 */

/** The pane the workspace opened the dialog from, which seeds the check type. */
type ComposerScope = "dataset" | "column";

/** The single-column config field each check type fills from `initialColumn`. */
const SINGLE_COLUMN_FIELD: Partial<
  Record<QualityRuleType, keyof RuleFormState["config"]>
> = {
  [QualityRuleType.NULL_PERCENT]: "nullColumn",
  [QualityRuleType.UNIQUE]: "column",
  [QualityRuleType.ACCEPTED_VALUES]: "column",
  [QualityRuleType.RANGE]: "column",
  [QualityRuleType.REGEX]: "column",
  [QualityRuleType.STRING_LENGTH]: "column",
  [QualityRuleType.FRESHNESS]: "timestampColumn",
  [QualityRuleType.REFERENTIAL_INTEGRITY]: "childColumn",
  [QualityRuleType.AGGREGATE]: "aggregateColumn",
  [QualityRuleType.CROSS_COLUMN]: "leftColumn",
};

/** The types a reader reaches for first, in the order the left pane lists them. */
const COMMON_TYPES = [
  QualityRuleType.ROW_COUNT,
  QualityRuleType.NULL_PERCENT,
  QualityRuleType.UNIQUE,
  QualityRuleType.ACCEPTED_VALUES,
  QualityRuleType.FRESHNESS,
  QualityRuleType.CUSTOM_SQL,
];

/** Dataset-level checks that are not in the common group. */
const DATASET_TYPES = [
  QualityRuleType.ROW_COUNT_CHANGE,
  QualityRuleType.SCHEMA_DRIFT,
];

/** Checks on one or several columns. */
const COLUMN_TYPES = [
  QualityRuleType.UNIQUE_COMBINATION,
  QualityRuleType.RANGE,
  QualityRuleType.REGEX,
  QualityRuleType.STRING_LENGTH,
  QualityRuleType.REFERENTIAL_INTEGRITY,
];

/** The remaining types: relations, grouping, aggregates and expressions. */
const ADVANCED_TYPES = [
  QualityRuleType.CROSS_COLUMN,
  QualityRuleType.GROUP_CHECK,
  QualityRuleType.AGGREGATE,
  QualityRuleType.CROSS_TABLE,
  QualityRuleType.CEL_EXPRESSION,
];

type CheckGroupId = "common" | "dataset" | "column" | "advanced";

/** The catalogue's sections, in the order the left pane renders them. */
const CHECK_GROUPS: Array<{ id: CheckGroupId; types: QualityRuleType[] }> = [
  { id: "common", types: COMMON_TYPES },
  { id: "dataset", types: DATASET_TYPES },
  { id: "column", types: COLUMN_TYPES },
  { id: "advanced", types: ADVANCED_TYPES },
];

const EMPTY_BEHAVIORS = [
  QualityEmptyBehavior.NOT_EVALUATED,
  QualityEmptyBehavior.PASS,
  QualityEmptyBehavior.FAIL,
];

const props = withDefaults(
  defineProps<{
    open: boolean;
    /** The metadata GUID of the target the rule belongs to. */
    targetGuid: string;
    metaType: MetaType;
    /** The target's display name, used in the create title. */
    targetLabel: string;
    /** The rule to edit; absent means the dialog creates a new one. */
    rule?: QualityRule;
    /** An AI draft to prefill from; saving accepts it in the same request. */
    suggestion?: QualitySuggestion;
    /**
     * The contract's default schedule for new rules, in seconds. 0 keeps the form's
     * own default; existing rules are never affected, only this new one.
     */
    defaultScheduleSeconds?: number;
    /** The contract's default severity for new rules; UNSPECIFIED keeps the form's. */
    defaultSeverity?: QualitySeverity;
    /** Where the workspace opened the dialog from, which seeds the check type. */
    scope?: ComposerScope;
    /** A column the workspace named, written into the first single-column field. */
    initialColumn?: string;
  }>(),
  {
    defaultScheduleSeconds: 0,
    defaultSeverity: QualitySeverity.UNSPECIFIED,
    initialColumn: "",
  }
);

const emit = defineEmits<{
  "update:open": [open: boolean];
  saved: [rule: QualityRule];
}>();

const { t } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const isEdit = computed(() => props.rule !== undefined);
const canSave = computed(() =>
  authStore.hasPermission(
    isEdit.value
      ? "metaxisdata.dataQuality.rules.update"
      : "metaxisdata.dataQuality.rules.create"
  )
);
// Preview is a separate permission on the service (`rules.run`): a role allowed
// to author a rule may still not run one, so the dry run is only attempted when
// it can succeed — same rule the old editor followed.
const canPreview = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.run")
);

/** The suggestion's draft, as the form it would open as. */
const suggestionPrefill = computed(() =>
  props.suggestion ? suggestionToEditorPrefill(props.suggestion) : undefined
);

/**
 * The fixed target: the edited rule's own, the draft's own, or the page's GUID and
 * meta type. `rule` wins over `suggestion`; the two are never set together.
 */
const target = computed<TargetSelection | undefined>(() => {
  if (props.rule) {
    return targetSelectionFromRule(props.rule);
  }
  if (suggestionPrefill.value?.target) {
    return suggestionPrefill.value.target;
  }
  return targetFromQuery(props.targetGuid, String(props.metaType));
});

/** The metadata GUID whose columns the config fields suggest. */
const columnGuid = computed(() => target.value?.guid ?? props.targetGuid);

const form = ref<RuleFormState>(emptyRuleForm());
const columns = ref<TargetColumnDetail[]>([]);
const columnNames = computed(() => columns.value.map((column) => column.name));
const issues = ref<ValidationIssue[]>([]);
const saveError = ref("");
const saving = ref(false);
/** Whether the details section starts open. */
const extraOpen = ref(false);
/** The column the workspace named, kept across type changes. */
const prefillColumn = ref("");
/** The edit baseline the update mask is computed against. */
let originalForm: RuleFormState | undefined;
let originalTarget: TargetSelection | undefined;

/** The check types the target can watch, in catalogue order. */
const checkGroups = computed(() => {
  const allowed = allowedRuleTypes(props.metaType);
  return CHECK_GROUPS.map((group) => ({
    id: group.id,
    types: group.types.filter((ruleType) => allowed.includes(ruleType)),
  })).filter((group) => group.types.length > 0);
});

/** The catalogue section headings; one literal key per group, translated. */
const groupLabels = computed<Record<CheckGroupId, string>>(() => ({
  common: t("dataQuality.composer.commonGroup"),
  dataset: t("dataQuality.composer.datasetGroup"),
  column: t("dataQuality.composer.columnGroup"),
  advanced: t("dataQuality.composer.advancedGroup"),
}));

const selectedTypeDescription = computed(() => {
  const key = qualityRuleTypeDescriptionKey(form.value.ruleType);
  return key ? t(key) : "";
});

/** Views and materialized views accept table-level rules only. */
const isViewTarget = computed(
  () =>
    props.metaType === MetaType.VIEW ||
    props.metaType === MetaType.MATERIALIZED_VIEW
);

const dimensionOptions = [
  QualityDimension.COMPLETENESS,
  QualityDimension.VALIDITY,
  QualityDimension.UNIQUENESS,
  QualityDimension.CONSISTENCY,
  QualityDimension.TIMELINESS,
  QualityDimension.ACCURACY,
].map((value) => ({ value, key: qualityDimensionKey(value) }));

const severityOptions = [
  QualitySeverity.WARNING,
  QualitySeverity.ERROR,
  QualitySeverity.CRITICAL,
].map((value) => ({ value, key: qualitySeverityKey(value) }));

const title = computed(() => {
  const rule = props.rule;
  if (rule) {
    return t("dataQuality.composer.editTitle", {
      title: rule.title || rule.name.split("/").pop() || rule.name,
    });
  }
  return t("dataQuality.composer.title", { target: props.targetLabel });
});

/** The draft's own title, for the prefill hint above the form. */
const suggestionTitle = computed(
  () =>
    suggestionPrefill.value?.form.title ||
    props.suggestion?.name.split("/").pop() ||
    ""
);

/** The type a create starts on: the workspace's scope, else the target default. */
function initialRuleType(): QualityRuleType {
  const allowed = allowedRuleTypes(props.metaType);
  const preferred =
    props.scope === "column"
      ? QualityRuleType.NULL_PERCENT
      : props.scope === "dataset"
        ? QualityRuleType.ROW_COUNT
        : undefined;
  if (preferred !== undefined && allowed.includes(preferred)) {
    return preferred;
  }
  return allowed.includes(QualityRuleType.ROW_COUNT)
    ? QualityRuleType.ROW_COUNT
    : (allowed[0] ?? QualityRuleType.ROW_COUNT);
}

/** A detached copy of a prefill form, so the cached draft cannot be mutated. */
function cloneForm(source: RuleFormState): RuleFormState {
  return {
    ...source,
    schedule: { ...source.schedule },
    filter: source.filter.map((clause) => ({ ...clause })),
    config: {
      ...source.config,
      expectedColumns:
        source.config.expectedColumns?.map((column) => ({ ...column })) ?? [],
    },
  };
}

/** The form a new rule starts from: the contract's defaults, else the built-ins. */
function initialForm(): RuleFormState {
  const form = emptyRuleForm(initialRuleType());
  if (props.defaultSeverity !== QualitySeverity.UNSPECIFIED) {
    form.severity = props.defaultSeverity;
  }
  if (props.defaultScheduleSeconds > 0) {
    form.schedule = durationToSchedule(
      create(DurationSchema, {
        seconds: BigInt(Math.round(props.defaultScheduleSeconds)),
        nanos: 0,
      })
    );
  }
  return form;
}

/** Writes a column into the config field its check type reads. */
function applyColumn(ruleType: QualityRuleType, column: string) {
  const field = SINGLE_COLUMN_FIELD[ruleType];
  if (!field || column === "") {
    return;
  }
  // Every mapped field is a string-valued column name.
  (form.value.config as Record<string, string>)[field] = column;
}

/** The single column the current config already names, if it names one. */
function currentSingleColumn(): string {
  const field = SINGLE_COLUMN_FIELD[form.value.ruleType];
  if (!field) {
    return "";
  }
  const value = form.value.config[field];
  return typeof value === "string" ? value.trim() : "";
}

/** True when the details section already carries something worth showing. */
function hasDetails(form: RuleFormState): boolean {
  return form.owner !== "" || form.tags !== "" || form.filter.length > 0;
}

/**
 * The form each opening starts from: the rule being edited, the AI draft, or a
 * blank new one carrying the contract's defaults and the workspace's scope.
 * Closing resets too, so a reopened dialog never shows stale values or errors.
 */
function reset() {
  const rule = props.rule;
  const prefill = suggestionPrefill.value;
  prefillColumn.value = "";
  originalForm = undefined;
  originalTarget = undefined;
  if (rule) {
    form.value = ruleToForm(rule);
    originalForm = ruleToForm(rule);
    originalTarget = targetSelectionFromRule(rule);
  } else if (prefill) {
    form.value = cloneForm(prefill.form);
  } else {
    prefillColumn.value = props.initialColumn.trim();
    form.value = initialForm();
    applyColumn(form.value.ruleType, prefillColumn.value);
  }
  extraOpen.value = hasDetails(form.value);
  columns.value = [];
  issues.value = [];
  saveError.value = "";
  saving.value = false;
}

function changeRuleType(ruleType: QualityRuleType) {
  if (ruleType === form.value.ruleType) {
    return;
  }
  // A new type is a new config arm: the old values belong to controls that are
  // no longer rendered, so they are cleared rather than sent along. The column
  // the workspace named (or the one already typed) follows the reader.
  const column = currentSingleColumn() || prefillColumn.value;
  form.value.ruleType = ruleType;
  form.value.config = emptyConfig();
  form.value.dimension = suggestedDimension(ruleType);
  if (!isThresholdMetricType(ruleType)) {
    form.value.thresholdMetric = QualityThresholdMetric.COUNT;
  }
  applyColumn(ruleType, column);
  issues.value = [];
  saveError.value = "";
}

function setDimension(value: unknown) {
  form.value.dimension = Number(value) as QualityDimension;
}

function setSeverity(value: unknown) {
  form.value.severity = Number(value) as QualitySeverity;
}

function setOnEmpty(value: unknown) {
  form.value.onEmpty = Number(value) as QualityEmptyBehavior;
}

function setScheduleUnit(value: unknown) {
  form.value.schedule.unit = String(value) as ScheduleUnit;
}

function onDetailsToggle(event: Event) {
  extraOpen.value = (event.target as HTMLDetailsElement).open;
}

/**
 * Column suggestions for the config fields. A failed read is not fatal: every
 * column control accepts free text, so the dialog keeps working without it.
 */
async function loadColumns() {
  const guid = columnGuid.value;
  try {
    const items = await listAllMetadata({
      parentGuid: guid,
      metaType: MetaType.COLUMN,
    });
    if (!props.open || guid !== columnGuid.value) {
      return;
    }
    columns.value = items.map((item) => {
      const value = item.type.value as
        | { name?: string; type?: string; nullable?: boolean }
        | undefined;
      return {
        name: value?.name ?? item.guid,
        type: value?.type ?? "",
        nullable: value?.nullable ?? true,
      };
    });
  } catch {
    if (props.open && guid === columnGuid.value) {
      columns.value = [];
    }
  }
}

watch(
  () => [props.open, props.rule?.name, props.suggestion?.name] as const,
  ([isOpen]) => {
    reset();
    if (isOpen) {
      void loadColumns();
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

/** The server's own wording for a rejected rule, rendered in place. */
function saveFailure(e: unknown) {
  saveError.value = parseRuleError(
    formatError(e, "dataQuality.editor.saveFailed")
  ).message;
}

/**
 * Compile and probe the rule without saving it. Returns false when the server
 * rejected it; the message is already in the dialog.
 */
async function runPreview(rule: QualityRule): Promise<boolean> {
  try {
    await previewQualityRule(rule);
    return true;
  } catch (e: unknown) {
    saveFailure(e);
    return false;
  }
}

/**
 * Validate, dry-run when allowed, then create or update. The dry run turns a rule
 * that cannot compile into an inline message instead of a write failure; a caller
 * without `rules.run` skips it and lets the write carry the verdict.
 */
async function save() {
  if (!canSave.value || saving.value) {
    return;
  }
  saveError.value = "";
  issues.value = validateRuleForm(form.value, target.value, columnNames.value);
  if (issues.value.length > 0 || !target.value) {
    return;
  }
  if (props.rule) {
    await saveEdit(props.rule, target.value);
  } else {
    await saveCreate(target.value);
  }
}

async function saveCreate(selection: TargetSelection) {
  const rule = formToRule(form.value, { target: selection });
  rule.state = QualityRuleState.ENABLED;
  // A rule created from a draft accepts the suggestion in the same request: the
  // server takes the target from the suggestion and decides it server-side.
  const suggestionName = props.suggestion?.name ?? "";
  saving.value = true;
  try {
    if (canPreview.value && !(await runPreview(rule))) {
      return;
    }
    let created: QualityRule;
    try {
      created = await createQualityRule(rule, suggestionName);
    } catch (e: unknown) {
      // The server's Invalid message names the field or the compiled-SQL
      // problem; showing it here keeps the form and its input intact.
      saveFailure(e);
      return;
    }
    notify.success(t("dataQuality.editor.created"));
    emit("saved", created);
    emit("update:open", false);
  } finally {
    saving.value = false;
  }
}

/**
 * An edit sends only the fields that changed. The local mask runs before the dry
 * run, so a save with nothing to record closes the dialog instead of sending a
 * request whose only possible answer is "no changes".
 */
async function saveEdit(original: QualityRule, selection: TargetSelection) {
  const rule = formToRule(form.value, { base: original, target: selection });
  const mask = computeUpdateMask(
    { form: originalForm ?? form.value, target: originalTarget },
    { form: form.value, target: selection }
  );
  if (mask.length === 0) {
    notify.info(t("dataQuality.editor.noChanges"));
    emit("update:open", false);
    return;
  }
  saving.value = true;
  try {
    if (canPreview.value && !(await runPreview(rule))) {
      return;
    }
    let updated: QualityRule;
    try {
      updated = await updateQualityRule(rule, mask);
    } catch (e: unknown) {
      saveFailure(e);
      return;
    }
    notify.success(t("dataQuality.editor.updated"));
    emit("saved", updated);
    emit("update:open", false);
  } finally {
    saving.value = false;
  }
}
</script>
