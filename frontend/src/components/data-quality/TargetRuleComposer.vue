<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent class="max-h-[90vh] gap-0 overflow-hidden p-0 sm:max-w-3xl">
      <div class="flex max-h-[90vh] min-h-0 flex-col">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>{{ title }}</DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.composer.description") }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-5 overflow-auto px-6 py-5">
          <!-- The target is fixed by the page; an unusable GUID can only come from
               a malformed route, so the dialog says so and keeps saving disabled. -->
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

          <!-- Basics: the fields a rule needs to mean anything. The target is not
               one of them — it is carried by the page this dialog opens from. -->
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

            <div class="flex flex-col gap-1.5">
              <Label for="dq-composer-rule-type">
                {{ t("dataQuality.editor.ruleType") }}
              </Label>
              <Select
                :model-value="String(form.ruleType)"
                @update:model-value="changeRuleType"
              >
                <SelectTrigger id="dq-composer-rule-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup
                    v-for="group in ruleTypeGroups"
                    :key="group.key"
                  >
                    <SelectLabel>
                      {{ t(group.key) }}
                    </SelectLabel>
                    <SelectItem
                      v-for="ruleType in group.types"
                      :key="ruleType"
                      :value="String(ruleType)"
                    >
                      {{ t(qualityRuleTypeKey(ruleType)) }}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
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

            <div class="flex flex-col gap-1.5">
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
          </section>

          <!-- The typed parameters, through the same controlled form the editor
               uses; the threshold unit it renders appears only where the rule type
               actually has a row total to measure against. -->
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
        </div>

        <DialogFooter class="shrink-0 items-center gap-2 border-t px-6 py-4 sm:justify-between">
          <!-- Filters, owner and tags stay in the full editor; a rule that needs
               them is one click from here with its target already filled in. -->
          <RouterLink
            :to="advancedTo"
            target="_blank"
            rel="noopener"
            class="text-sm text-primary hover:underline"
          >
            {{ t("dataQuality.composer.advanced") }}
          </RouterLink>
          <div class="flex flex-col-reverse gap-2 sm:flex-row">
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
          </div>
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
import { RouterLink } from "vue-router";
import {
  createQualityRule,
  previewQualityRule,
  updateQualityRule,
} from "@/api/data-quality";
import { listAllMetadata } from "@/api/database";
import RuleConfigForm from "@/components/data-quality/RuleConfigForm.vue";
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
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityDimension,
  type QualityRule,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityThresholdMetric,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  qualityDimensionKey,
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
  isThresholdMetricType,
  RULE_TYPE_GROUPS,
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

/**
 * Create or edit one rule on the target the page already names.
 *
 * The workspace's "New rule" used to hand off to the full editor, which made the
 * reader pick the target they were already looking at. This dialog fixes the
 * target and keeps the common case inline: title, type, parameters and a dry run.
 * Saving emits the rule the server returned so the page can render it in place;
 * the full editor stays one link away for filters, owner and tags.
 *
 * With `rule` set the dialog edits that rule instead: the form starts from it and
 * saving sends only the fields that actually changed, so a rename does not rewrite
 * the config, and an edit with no change sends nothing at all.
 */

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
    /**
     * The contract's default schedule for new rules, in seconds. 0 keeps the form's
     * own default; existing rules are never affected, only this new one.
     */
    defaultScheduleSeconds?: number;
    /** The contract's default severity for new rules; UNSPECIFIED keeps the form's. */
    defaultSeverity?: QualitySeverity;
  }>(),
  {
    defaultScheduleSeconds: 0,
    defaultSeverity: QualitySeverity.UNSPECIFIED,
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
// it can succeed — same rule the editor follows.
const canPreview = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.run")
);

/** The fixed target: the edited rule's own, or the page's GUID and meta type. */
const target = computed<TargetSelection | undefined>(() =>
  props.rule
    ? targetSelectionFromRule(props.rule)
    : targetFromQuery(props.targetGuid, String(props.metaType))
);

/** The metadata GUID whose columns the config fields suggest. */
const columnGuid = computed(() => target.value?.guid ?? props.targetGuid);

const form = ref<RuleFormState>(emptyRuleForm());
const columns = ref<TargetColumnDetail[]>([]);
const columnNames = computed(() => columns.value.map((column) => column.name));
const issues = ref<ValidationIssue[]>([]);
const saveError = ref("");
const saving = ref(false);
/** The edit baseline the update mask is computed against. */
let originalForm: RuleFormState | undefined;
let originalTarget: TargetSelection | undefined;

/** The rule-type sections, with the types this target cannot watch removed. */
const ruleTypeGroups = computed(() => {
  const allowed = allowedRuleTypes(props.metaType);
  return RULE_TYPE_GROUPS.map((group) => ({
    key: group.key,
    types: group.types.filter((ruleType) => allowed.includes(ruleType)),
  })).filter((group) => group.types.length > 0);
});

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
  if (!rule) {
    return t("dataQuality.composer.title", { target: props.targetLabel });
  }
  return t("dataQuality.composer.editTitle", {
    title: rule.title || rule.name.split("/").pop() || rule.name,
  });
});

/** The full editor for what the dialog leaves out; the rule's own page on edit. */
const advancedTo = computed(() =>
  props.rule
    ? {
        name: "DataQualityRuleEdit",
        params: { ruleId: props.rule.name.split("/") },
      }
    : {
        name: "DataQualityRuleNew",
        query: {
          table_guid: props.targetGuid,
          meta_type: String(props.metaType),
        },
      }
);

/** A new rule starts on a type this target can watch. */
function defaultRuleType(): QualityRuleType {
  const allowed = allowedRuleTypes(props.metaType);
  return allowed.includes(QualityRuleType.ROW_COUNT)
    ? QualityRuleType.ROW_COUNT
    : (allowed[0] ?? QualityRuleType.ROW_COUNT);
}

/** The form a new rule starts from: the contract's defaults, else the built-ins. */
function initialForm(): RuleFormState {
  const form = emptyRuleForm(defaultRuleType());
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

/**
 * The form each opening starts from: the rule being edited, or a blank new one
 * carrying the contract's defaults. Closing resets too, so a reopened dialog
 * never shows the previous session's values or error.
 */
function reset() {
  const rule = props.rule;
  form.value = rule ? ruleToForm(rule) : initialForm();
  originalForm = rule ? ruleToForm(rule) : undefined;
  originalTarget = rule ? targetSelectionFromRule(rule) : undefined;
  columns.value = [];
  issues.value = [];
  saveError.value = "";
  saving.value = false;
}

function changeRuleType(value: unknown) {
  const ruleType = Number(value) as QualityRuleType;
  if (ruleType === form.value.ruleType) {
    return;
  }
  // A new type is a new config arm: the old values belong to controls that are
  // no longer rendered, so they are cleared rather than sent along.
  form.value.ruleType = ruleType;
  form.value.config = emptyConfig();
  form.value.dimension = suggestedDimension(ruleType);
  if (!isThresholdMetricType(ruleType)) {
    form.value.thresholdMetric = QualityThresholdMetric.COUNT;
  }
  issues.value = [];
  saveError.value = "";
}

function setDimension(value: unknown) {
  form.value.dimension = Number(value) as QualityDimension;
}

function setSeverity(value: unknown) {
  form.value.severity = Number(value) as QualitySeverity;
}

function setScheduleUnit(value: unknown) {
  form.value.schedule.unit = String(value) as ScheduleUnit;
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
  () => [props.open, props.rule?.name] as const,
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
  saving.value = true;
  try {
    if (canPreview.value && !(await runPreview(rule))) {
      return;
    }
    let created: QualityRule;
    try {
      created = await createQualityRule(rule);
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
