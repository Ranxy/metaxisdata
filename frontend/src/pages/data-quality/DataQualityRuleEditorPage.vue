<template>
  <div class="space-y-4">
    <PageHeader
      :title="pageTitle"
      :description="t('dataQuality.editor.pageDescription')"
    >
      <template #breadcrumb>
        <RouterLink
          :to="{ name: 'DataQualityRules' }"
          class="inline-flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft class="h-3.5 w-3.5" />
          {{ t("dataQuality.rulesTitle") }}
        </RouterLink>
      </template>
      <template #actions>
        <Button
          variant="outline"
          :disabled="saving || previewing"
          @click="cancel"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          v-if="canPreview"
          variant="outline"
          :disabled="saving || previewing || !canSave"
          @click="dryRun"
        >
          <PlayCircle class="mr-2 h-4 w-4" />
          {{ t("dataQuality.editor.dryRun") }}
        </Button>
        <Button
          :disabled="saving || previewing || !canSave"
          @click="save"
        >
          <Save class="mr-2 h-4 w-4" />
          {{ isEdit ? t("dataQuality.editor.saveChanges") : t("dataQuality.editor.create") }}
        </Button>
      </template>
    </PageHeader>

    <PageState
      :loading="loadingRule"
      :error="loadError"
    >
      <template #error>
        <Button
          variant="outline"
          size="sm"
          @click="loadRule"
        >
          {{ t("common.refresh") }}
        </Button>
      </template>

      <form
        class="mx-auto w-full max-w-[960px] space-y-4"
        @submit.prevent="save"
      >
        <!-- The columns of the selected target, offered by every column field. -->
        <datalist id="dq-target-columns">
          <option
            v-for="column in targetColumns"
            :key="column"
            :value="column"
          />
        </datalist>

        <Alert
          v-if="issues.length > 0"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>{{ t("dataQuality.editor.validationTitle") }}</AlertTitle>
          <AlertDescription>
            <ul class="list-disc space-y-1 pl-5">
              <li
                v-for="issue in issues"
                :key="issue.key"
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

        <!-- Target -->
        <Card>
          <CardHeader>
            <CardTitle class="text-base">
              {{ t("dataQuality.editor.targetTitle") }}
            </CardTitle>
            <CardDescription>
              {{ t("dataQuality.editor.targetDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div
              v-if="target"
              class="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3"
            >
              <div class="min-w-0">
                <div class="truncate font-mono text-sm">
                  {{ targetLabel }}
                </div>
                <div class="text-xs text-muted-foreground">
                  {{ metaTypeSingularLabel(target.metaType, t) }}
                </div>
              </div>
              <Button
                variant="outline"
                size="sm"
                @click="targetOpen = true"
              >
                {{ t("dataQuality.editor.changeTarget") }}
              </Button>
            </div>
            <div
              v-else
              class="flex flex-col items-start gap-3 rounded-md border border-dashed p-4"
            >
              <p class="text-sm text-muted-foreground">
                {{ t("dataQuality.editor.targetEmpty") }}
              </p>
              <Button
                variant="outline"
                size="sm"
                @click="targetOpen = true"
              >
                <Database class="mr-2 h-4 w-4" />
                {{ t("dataQuality.editor.selectTarget") }}
              </Button>
            </div>
          </CardContent>
        </Card>

        <!-- Basics -->
        <Card>
          <CardHeader>
            <CardTitle class="text-base">
              {{ t("dataQuality.editor.basicsTitle") }}
            </CardTitle>
          </CardHeader>
          <CardContent class="grid gap-4 md:grid-cols-2">
            <FormField
              id="dq-rule-title"
              v-model="form.title"
              required
              :label="t('dataQuality.editor.title')"
              :placeholder="t('dataQuality.editor.titlePlaceholder')"
            />
            <FormField
              id="dq-rule-owner"
              v-model="form.owner"
              :label="t('dataQuality.editor.owner')"
              :hint="t('dataQuality.editor.ownerHint')"
              placeholder="users/123"
            />
            <div class="space-y-2 md:col-span-2">
              <Label for="dq-rule-description">
                {{ t("dataQuality.editor.description") }}
              </Label>
              <textarea
                id="dq-rule-description"
                v-model="form.description"
                class="min-h-20 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                :placeholder="t('dataQuality.editor.descriptionPlaceholder')"
              />
            </div>

            <div class="flex flex-col gap-1.5">
              <Label for="dq-rule-type">
                {{ t("dataQuality.editor.ruleType") }}
              </Label>
              <Select
                :model-value="String(form.ruleType)"
                @update:model-value="changeRuleType"
              >
                <SelectTrigger id="dq-rule-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="ruleType in EDITABLE_RULE_TYPES"
                    :key="ruleType"
                    :value="String(ruleType)"
                  >
                    {{ t(qualityRuleTypeKey(ruleType)) }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div class="flex flex-col gap-1.5">
              <Label for="dq-rule-dimension">
                {{ t("dataQuality.filterDimension") }}
              </Label>
              <Select
                :model-value="String(form.dimension)"
                @update:model-value="setDimension"
              >
                <SelectTrigger id="dq-rule-dimension">
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
              <Label for="dq-rule-severity">
                {{ t("dataQuality.filterSeverity") }}
              </Label>
              <Select
                :model-value="String(form.severity)"
                @update:model-value="setSeverity"
              >
                <SelectTrigger id="dq-rule-severity">
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
              <Label for="dq-rule-state">
                {{ t("dataQuality.filterState") }}
              </Label>
              <Select
                :model-value="String(form.state)"
                @update:model-value="setState"
              >
                <SelectTrigger id="dq-rule-state">
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

            <div class="flex flex-col gap-1.5">
              <Label for="dq-rule-schedule">
                {{ t("dataQuality.editor.schedule") }}
              </Label>
              <div class="flex gap-2">
                <FormField
                  id="dq-rule-schedule"
                  v-model="form.schedule.value"
                  type="number"
                  class="min-w-0 flex-1"
                />
                <Select
                  :model-value="form.schedule.unit"
                  @update:model-value="setScheduleUnit"
                >
                  <SelectTrigger class="w-32">
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

            <FormField
              id="dq-rule-tags"
              v-model="form.tags"
              :label="t('dataQuality.editor.tags')"
              :hint="t('dataQuality.editor.tagsHint')"
            />
          </CardContent>
        </Card>

        <!-- Config -->
        <Card>
          <CardHeader>
            <CardTitle class="text-base">
              {{ t("dataQuality.editor.configTitle") }}
            </CardTitle>
            <CardDescription>
              {{ t("dataQuality.editor.configDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-5">
            <RuleConfigForm
              v-model="form.config"
              v-model:threshold-metric="form.thresholdMetric"
              :rule-type="form.ruleType"
            />
            <div class="max-w-sm space-y-1.5 border-t pt-4">
              <Label for="dq-rule-on-empty">
                {{ t("dataQuality.editor.onEmpty") }}
              </Label>
              <Select
                :model-value="String(form.onEmpty)"
                @update:model-value="setOnEmpty"
              >
                <SelectTrigger id="dq-rule-on-empty">
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
          </CardContent>
        </Card>

        <!-- Pre-filter -->
        <Card>
          <CardContent class="pt-6">
            <RuleFilterEditor v-model="form.filter" />
          </CardContent>
        </Card>

        <!-- LLM draft import stays a placeholder until the suggestion API ships. -->
        <Card>
          <CardContent class="flex flex-wrap items-center justify-between gap-3 pt-6">
            <div class="min-w-0">
              <p class="font-medium">
                {{ t("dataQuality.editor.llmImport") }}
              </p>
              <p class="text-sm text-muted-foreground">
                {{ t("dataQuality.editor.llmImportHint") }}
              </p>
            </div>
            <Button
              variant="outline"
              :disabled="true"
              :title="t('dataQuality.editor.llmImportHint')"
            >
              <Sparkles class="mr-2 h-4 w-4" />
              {{ t("dataQuality.editor.llmImportButton") }}
            </Button>
          </CardContent>
        </Card>

        <!-- Preview / dry run -->
        <Card v-if="previewing || preview || previewError">
          <CardHeader>
            <CardTitle class="text-base">
              {{ t("dataQuality.editor.previewTitle") }}
            </CardTitle>
            <CardDescription>
              {{ t("dataQuality.editor.previewDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-3">
            <div
              v-if="previewing"
              class="flex items-center gap-2 text-sm text-muted-foreground"
            >
              <Loader2 class="h-4 w-4 animate-spin" />
              {{ t("dataQuality.editor.previewing") }}
            </div>
            <div
              v-else-if="previewError"
              class="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm break-words text-destructive"
            >
              {{ previewError }}
            </div>
            <template v-else-if="preview">
              <div v-if="preview.referencedColumns.length > 0">
                <div class="text-xs font-medium text-muted-foreground">
                  {{ t("dataQuality.editor.referencedColumns") }}
                </div>
                <div class="mt-1 flex flex-wrap gap-1">
                  <Badge
                    v-for="column in preview.referencedColumns"
                    :key="column"
                    variant="secondary"
                  >
                    {{ column }}
                  </Badge>
                </div>
              </div>
              <div v-if="preview.parameters.length > 0">
                <div class="text-xs font-medium text-muted-foreground">
                  {{ t("dataQuality.editor.parameters") }}
                </div>
                <pre class="mt-1 rounded-md border bg-muted p-3 text-xs whitespace-pre-wrap wrap-anywhere">{{ preview.parameters.join(", ") }}</pre>
              </div>
              <div>
                <div class="text-xs font-medium text-muted-foreground">
                  {{ t("dataQuality.editor.compiledSql") }}
                </div>
                <pre class="mt-1 rounded-md border bg-muted p-3 text-xs whitespace-pre-wrap wrap-anywhere">{{ preview.compiledSql }}</pre>
              </div>
            </template>
          </CardContent>
        </Card>
      </form>
    </PageState>

    <TargetPicker
      v-model:open="targetOpen"
      @select="onTargetSelected"
    />
  </div>
</template>

<script setup lang="ts">
import {
  AlertTriangle,
  ArrowLeft,
  Database,
  Loader2,
  PlayCircle,
  Save,
  Sparkles,
} from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute, useRouter } from "vue-router";
import {
  createQualityRule,
  getQualityRule,
  previewQualityRule,
  updateQualityRule,
} from "@/api/data-quality";
import { listMetadata } from "@/api/database";
import PageState from "@/components/common/PageState.vue";
import RuleConfigForm from "@/components/data-quality/RuleConfigForm.vue";
import RuleFilterEditor from "@/components/data-quality/RuleFilterEditor.vue";
import TargetPicker from "@/components/data-quality/TargetPicker.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
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
  QualityDimension as Dimension,
  QualityEmptyBehavior as EmptyBehavior,
  type QualityDimension,
  type QualityEmptyBehavior,
  type QualityRule,
  type QualityRuleState,
  QualityRuleType,
  type QualitySeverity,
  QualityRuleState as RuleState,
  QualitySeverity as Severity,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  qualityEmptyBehaviorKey,
  qualityRuleTypeKey,
  targetScopeLabel,
} from "@/utils/dataQuality";
import {
  computeUpdateMask,
  EDITABLE_RULE_TYPES,
  emptyConfig,
  emptyRuleForm,
  formToRule,
  type RuleFormState,
  ruleToForm,
  type ScheduleUnit,
  suggestedDimension,
  type TargetSelection,
  targetFromQuery,
  targetSelectionFromRule,
  validateRuleForm,
} from "@/utils/dataQualityForm";
import { metaTypeSingularLabel } from "@/utils/metaType";

/**
 * The rule editor: create at `/data-quality/rules/new`, edit at
 * `/data-quality/rules/{name...}/edit`. The page owns the form state and every
 * request; the config field set is a child component, and the pure conversions
 * live in `utils/dataQualityForm.ts`.
 *
 * Saving is two-step on purpose: the server compiles and probes the rule through
 * PreviewQualityRule first, so a rule that cannot compile is reported inline
 * rather than as a create failure.
 */

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const isEdit = computed(() => route.name === "DataQualityRuleEdit");
const canSave = computed(() =>
  authStore.hasPermission(
    isEdit.value
      ? "metaxisdata.dataQuality.rules.update"
      : "metaxisdata.dataQuality.rules.create"
  )
);
// Preview is a separate permission on the service (`rules.run`). A custom role
// may be allowed to author a rule without being allowed to run one; saving must
// still work there, so the dry run is only attempted when it can succeed.
const canPreview = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.run")
);

const pageTitle = computed(() =>
  isEdit.value
    ? t("dataQuality.editor.editTitle")
    : t("dataQuality.editor.newTitle")
);

const form = ref<RuleFormState>(emptyRuleForm());
const target = ref<TargetSelection>();
const targetOpen = ref(false);
const targetColumns = ref<string[]>([]);

const loadingRule = ref(false);
const loadError = ref("");
const saving = ref(false);
const previewing = ref(false);
const preview = ref<
  | { compiledSql: string; referencedColumns: string[]; parameters: string[] }
  | undefined
>();
const previewError = ref("");
const saveError = ref("");
const issues = ref<ReturnType<typeof validateRuleForm>>([]);

/** The rule as loaded, for the update mask; undefined when creating. */
let originalRule: QualityRule | undefined;
let originalForm: RuleFormState | undefined;
let originalTarget: TargetSelection | undefined;

const dimensionOptions: Array<{ value: QualityDimension; key: string }> = [
  { value: Dimension.COMPLETENESS, key: "dataQuality.dimension.completeness" },
  { value: Dimension.VALIDITY, key: "dataQuality.dimension.validity" },
  { value: Dimension.UNIQUENESS, key: "dataQuality.dimension.uniqueness" },
  { value: Dimension.CONSISTENCY, key: "dataQuality.dimension.consistency" },
  { value: Dimension.TIMELINESS, key: "dataQuality.dimension.timeliness" },
  { value: Dimension.ACCURACY, key: "dataQuality.dimension.accuracy" },
];

const severityOptions: Array<{ value: QualitySeverity; key: string }> = [
  { value: Severity.WARNING, key: "dataQuality.severity.warning" },
  { value: Severity.ERROR, key: "dataQuality.severity.error" },
  { value: Severity.CRITICAL, key: "dataQuality.severity.critical" },
];

const stateOptions: Array<{ value: QualityRuleState; key: string }> = [
  { value: RuleState.ENABLED, key: "dataQuality.ruleState.enabled" },
  { value: RuleState.PAUSED, key: "dataQuality.ruleState.paused" },
  { value: RuleState.ARCHIVED, key: "dataQuality.ruleState.archived" },
];

const EMPTY_BEHAVIORS = [
  EmptyBehavior.NOT_EVALUATED,
  EmptyBehavior.PASS,
  EmptyBehavior.FAIL,
];

const targetLabel = computed(() => {
  if (!target.value) {
    return "";
  }
  const path = target.value.path;
  return [targetScopeLabel(target.value.path), path.object]
    .filter((part) => part && part !== "-")
    .join(" · ");
});

/** The rule resource name the edit route carries, one segment per name segment. */
const ruleName = computed(() => {
  const param = route.params.ruleId;
  return Array.isArray(param) ? param.join("/") : String(param ?? "");
});

function changeRuleType(value: unknown) {
  const ruleType = Number(value) as QualityRuleType;
  if (ruleType === form.value.ruleType) {
    return;
  }
  // A new type is a new config arm: keeping the old values would leave fields
  // that the new arm cannot carry (and a stale filter is still shared, so it
  // stays).
  form.value.ruleType = ruleType;
  form.value.config = emptyConfig();
  form.value.dimension = suggestedDimension(ruleType);
  preview.value = undefined;
  previewError.value = "";
  issues.value = [];
}

function setDimension(value: unknown) {
  form.value.dimension = Number(value) as QualityDimension;
}

function setSeverity(value: unknown) {
  form.value.severity = Number(value) as QualitySeverity;
}

function setState(value: unknown) {
  form.value.state = Number(value) as QualityRuleState;
}

function setScheduleUnit(value: unknown) {
  form.value.schedule.unit = String(value) as ScheduleUnit;
}

function setOnEmpty(value: unknown) {
  form.value.onEmpty = Number(value) as QualityEmptyBehavior;
}

function onTargetSelected(selection: TargetSelection) {
  target.value = selection;
  void refreshTargetColumns(selection.guid);
}

/** Column suggestions for the config fields; best effort, free text still works. */
async function refreshTargetColumns(guid: string) {
  try {
    const response = await listMetadata({ parentGuid: guid, pageSize: 200 });
    const columns = response.typesStoredMetadata.find(
      (group) => group.metaType === MetaType.COLUMN
    );
    targetColumns.value =
      columns?.list.map((item) => {
        const value = item.type.value as { name?: string } | undefined;
        return value?.name ?? item.guid;
      }) ?? [];
  } catch {
    targetColumns.value = [];
  }
}

async function loadRule() {
  if (!isEdit.value) {
    applyPrefill();
    return;
  }
  loadingRule.value = true;
  loadError.value = "";
  try {
    const loaded = await getQualityRule(ruleName.value);
    originalRule = loaded;
    originalForm = ruleToForm(loaded);
    originalTarget = targetSelectionFromRule(loaded);
    form.value = ruleToForm(loaded);
    target.value = originalTarget ? { ...originalTarget } : undefined;
    if (target.value) {
      void refreshTargetColumns(target.value.guid);
    }
  } catch (e: unknown) {
    loadError.value = formatError(e, "dataQuality.editor.loadFailed");
  } finally {
    loadingRule.value = false;
  }
}

/** `?table_guid=...&meta_type=...` lets an asset page open the editor prefilled. */
function applyPrefill() {
  const query = route.query;
  const guid = typeof query.table_guid === "string" ? query.table_guid : "";
  const metaType =
    typeof query.meta_type === "string" ? query.meta_type : undefined;
  const prefilled = targetFromQuery(guid, metaType);
  if (prefilled) {
    target.value = prefilled;
    void refreshTargetColumns(prefilled.guid);
  }
}

function currentRule(): QualityRule {
  return formToRule(form.value, {
    base: originalRule,
    target: target.value,
  });
}

function runValidation(): boolean {
  issues.value = validateRuleForm(form.value, target.value);
  saveError.value = "";
  return issues.value.length === 0;
}

/**
 * Compile and probe the rule without saving it. Returns true when the server
 * accepted it; the compiled SQL is kept for the preview panel.
 */
async function runPreview(): Promise<boolean> {
  previewing.value = true;
  previewError.value = "";
  try {
    const result = await previewQualityRule(currentRule());
    preview.value = {
      compiledSql: result.compiledSql,
      referencedColumns: result.referencedColumns,
      parameters: result.parameters,
    };
    return true;
  } catch (e: unknown) {
    preview.value = undefined;
    previewError.value = formatError(e, "dataQuality.editor.previewFailed");
    return false;
  } finally {
    previewing.value = false;
  }
}

async function dryRun() {
  if (!runValidation()) {
    return;
  }
  if (await runPreview()) {
    notify.success(t("dataQuality.editor.previewOk"));
  }
}

async function save() {
  if (!canSave.value || saving.value) {
    return;
  }
  if (!runValidation()) {
    return;
  }
  if (canPreview.value && !(await runPreview())) {
    return;
  }
  saving.value = true;
  try {
    const rule = currentRule();
    if (isEdit.value) {
      const mask = computeUpdateMask(
        { form: originalForm ?? form.value, target: originalTarget },
        { form: form.value, target: target.value }
      );
      if (mask.length === 0) {
        notify.info(t("dataQuality.editor.noChanges"));
        await router.push({ name: "DataQualityRules" });
        return;
      }
      await updateQualityRule(rule, mask);
      notify.success(t("dataQuality.editor.updated"));
    } else {
      await createQualityRule(rule);
      notify.success(t("dataQuality.editor.created"));
    }
    await router.push({ name: "DataQualityRules" });
  } catch (e: unknown) {
    // The server's Invalid message names the field or the compiled-SQL problem;
    // inline is where the user can act on it.
    saveError.value = formatError(e, "dataQuality.editor.saveFailed");
  } finally {
    saving.value = false;
  }
}

function cancel() {
  void router.push({ name: "DataQualityRules" });
}

onMounted(() => {
  void loadRule();
});
</script>
