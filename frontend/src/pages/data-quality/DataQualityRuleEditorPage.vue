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

    <!-- Save/validation feedback sits above the first card: the form is long,
         and an error rendered at its bottom is an error nobody sees. -->
    <div
      ref="feedbackRef"
      class="mx-auto w-full max-w-[960px] space-y-3"
    >
      <Alert
        v-if="missingFields.length > 0"
        variant="destructive"
      >
        <AlertTriangle class="h-4 w-4" />
        <AlertTitle>{{ t("dataQuality.editor.missingRequiredTitle") }}</AlertTitle>
        <AlertDescription>
          <ul class="list-disc space-y-1 pl-5">
            <li
              v-for="(label, index) in missingFieldLabels"
              :key="index"
            >
              {{ label }}
            </li>
          </ul>
        </AlertDescription>
      </Alert>
      <Alert
        v-if="dryRunError.message"
        variant="destructive"
      >
        <AlertTriangle class="h-4 w-4" />
        <AlertTitle>{{ t("dataQuality.editor.dryRunFailed") }}</AlertTitle>
        <AlertDescription class="space-y-2">
          <p class="break-words">{{ dryRunError.message }}</p>
          <RuleErrorDetails
            :parsed="dryRunError"
            :raw="dryRunErrorRaw"
            :parameter-hint="isParameterError(dryRunError.message)"
            :missing-labels="unboundFilterColumns"
          />
        </AlertDescription>
      </Alert>
      <Alert
        v-if="saveError"
        variant="destructive"
      >
        <AlertTriangle class="h-4 w-4" />
        <AlertTitle>{{ t("dataQuality.editor.saveFailed") }}</AlertTitle>
        <AlertDescription class="space-y-2">
          <div class="flex items-start justify-between gap-3">
            <span class="break-words">{{ saveError }}</span>
            <button
              type="button"
              class="shrink-0 rounded-sm p-0.5 text-destructive hover:opacity-70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              :aria-label="t('common.close')"
              :title="t('common.close')"
              @click="saveError = ''"
            >
              <X class="h-4 w-4" />
            </button>
          </div>
          <RuleErrorDetails
            :parsed="saveErrorDetails"
            :parameter-hint="isParameterError(saveError)"
            :missing-labels="unboundFilterColumns"
          />
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
              v-for="issue in issues"
              :key="issue.key"
            >
              {{ t(issue.key, issue.params ?? {}) }}
            </li>
          </ul>
        </AlertDescription>
      </Alert>
    </div>

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
        <Alert
          v-if="suggestionInfo"
          variant="default"
        >
          <Lightbulb class="h-4 w-4" />
          <AlertTitle>{{ t("dataQuality.editor.suggestionPrefilled") }}</AlertTitle>
          <AlertDescription>
            {{ t("dataQuality.editor.suggestionPrefilledHint", { title: suggestionToEditorPrefill(suggestionInfo)?.form.title || suggestionInfo.name.split("/").pop() }) }}
            <RouterLink
              :to="{ name: 'DataQualitySuggestions' }"
              class="ml-1 text-primary hover:underline"
            >
              {{ t("dataQuality.suggestions.title") }}
            </RouterLink>
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

            <Alert
              v-if="showReadOnlyWarning"
              variant="warning"
              class="mt-3"
            >
              <AlertTriangle class="h-4 w-4" />
              <AlertTitle>{{ t("dataQuality.editor.readOnlyMissingTitle") }}</AlertTitle>
              <AlertDescription class="space-y-1">
                <p>{{ t("dataQuality.editor.readOnlyMissingHint") }}</p>
                <RouterLink
                  v-if="targetInstanceName"
                  :to="{ name: 'InstanceDetail', params: { instanceId: targetInstanceName } }"
                  class="text-primary hover:underline"
                >
                  {{ t("dataQuality.editor.openInstance") }}
                </RouterLink>
              </AlertDescription>
            </Alert>
          </CardContent>
        </Card>

        <!-- Model-drafted rules sit right after the target: without a target
             there is nothing to draft, and the dialog only queues suggestions. -->
        <Card>
          <CardContent class="flex flex-wrap items-center justify-between gap-3 pt-6">
            <div class="min-w-0">
              <p class="font-medium">
                {{ t("dataQuality.editor.llmImport") }}
              </p>
              <p class="text-sm text-muted-foreground">
                <template v-if="!target">
                  {{ t("dataQuality.llm.draft.selectTargetFirst") }}
                </template>
                <template v-else>
                  {{ t(llmAvailability.reasonKey ?? "dataQuality.editor.llmImportHint") }}
                  <RouterLink
                    v-if="!llmEnabled && canSuggest"
                    :to="{ name: 'DataQualitySettings' }"
                    class="ml-1 text-primary hover:underline"
                  >
                    {{ t("dataQuality.llm.enableInSettings") }}
                  </RouterLink>
                </template>
                <RouterLink
                  :to="{ name: 'DataQualitySuggestions' }"
                  class="ml-1 text-primary hover:underline"
                >
                  {{ t("dataQuality.suggestions.openCenter") }}
                </RouterLink>
              </p>
            </div>
            <Button
              variant="outline"
              :disabled="!target || !llmAvailability.available"
              :title="
                !target
                  ? t('dataQuality.llm.draft.selectTargetFirst')
                  : t(llmAvailability.reasonKey ?? 'dataQuality.editor.llmImportHint')
              "
              @click="llmDialogOpen = true"
            >
              <Sparkles class="mr-2 h-4 w-4" />
              {{ t("dataQuality.llm.draft.openButton") }}
            </Button>
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
            <UserSelect
              id="dq-rule-owner"
              v-model="form.owner"
              :label="t('dataQuality.editor.owner')"
              :hint="t('dataQuality.editor.ownerHint')"
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
                  <SelectGroup
                    v-for="group in ruleTypeGroups"
                    :key="group.key"
                  >
                    <SelectLabel v-if="group.key">
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
              :column-details="targetColumnDetails"
              :field-errors="fieldErrors"
            />
            <div
              v-if="isOnEmptyRelevant(form.ruleType)"
              class="max-w-sm space-y-1.5 border-t pt-4"
            >
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
            <RuleFilterEditor
              v-model="form.filter"
              :columns="targetColumnDetails"
            />
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

    <SuggestionWorkbench
      v-if="target"
      v-model:open="llmDialogOpen"
      :llm-enabled="llmEnabled"
      :fixed-target="workbenchTarget"
      @accepted="applyAcceptedDraft"
    />
  </div>
</template>

<script setup lang="ts">
import {
  AlertTriangle,
  ArrowLeft,
  Database,
  Lightbulb,
  Loader2,
  PlayCircle,
  Save,
  Sparkles,
  X,
} from "lucide-vue-next";
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute, useRouter } from "vue-router";
import {
  createQualityRule,
  getQualityRule,
  getQualitySuggestion,
  previewQualityRule,
  updateQualityRule,
} from "@/api/data-quality";
import { listAllMetadata } from "@/api/database";
import { getInstance } from "@/api/instance";
import { getWorkspaceProfileSetting } from "@/api/setting";
import PageState from "@/components/common/PageState.vue";
import UserSelect from "@/components/common/UserSelect.vue";
import RuleConfigForm from "@/components/data-quality/RuleConfigForm.vue";
import RuleErrorDetails from "@/components/data-quality/RuleErrorDetails.vue";
import RuleFilterEditor from "@/components/data-quality/RuleFilterEditor.vue";
import SuggestionWorkbench from "@/components/data-quality/SuggestionWorkbench.vue";
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
  QualityDimension as Dimension,
  QualityEmptyBehavior as EmptyBehavior,
  type QualityDimension,
  type QualityEmptyBehavior,
  type QualityRule,
  type QualityRuleState,
  QualityRuleType,
  type QualitySeverity,
  type QualitySuggestion,
  QualityThresholdMetric,
  QualityRuleState as RuleState,
  QualitySeverity as Severity,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { DataSourceType } from "@/types/proto-es/v1/instance_service_pb";
import {
  qualityEmptyBehaviorKey,
  qualityRuleTypeDescriptionKey,
  qualityRuleTypeKey,
  targetScopeLabel,
} from "@/utils/dataQuality";
import {
  allowedRuleTypes,
  computeUpdateMask,
  emptyConfig,
  emptyRuleForm,
  formToRule,
  isOnEmptyRelevant,
  isThresholdMetricType,
  RULE_TYPE_GROUPS,
  type RuleFormState,
  resolveExpectedColumnNullability,
  ruleToForm,
  type ScheduleUnit,
  suggestedDimension,
  type TargetColumnDetail,
  type TargetSelection,
  targetFromQuery,
  targetSelectionFromRule,
  validateRuleForm,
} from "@/utils/dataQualityForm";
import { llmFeatureAvailability } from "@/utils/dataQualityLlm";
import {
  isParameterError,
  type MissingField,
  missingFieldElementId,
  missingRequiredFields,
  type ParsedRuleError,
  parseRuleError,
} from "@/utils/dataQualitySchema";
import { suggestionToEditorPrefill } from "@/utils/dataQualitySuggestion";
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

/** The inline messages RuleConfigForm renders under the offending fields. */
function missingMessage(field: MissingField): string {
  if (field.oneOfKeys) {
    return t("dataQuality.schema.atLeastOne", {
      fields: field.oneOfKeys.map((key) => t(key)).join(" / "),
    });
  }
  return t(field.labelKey ?? "");
}

const fieldErrors = computed(() => {
  const errors: Record<string, string> = {};
  for (const field of missingFields.value) {
    errors[field.field] = missingMessage(field);
  }
  return errors;
});

const missingFieldLabels = computed(() =>
  missingFields.value.map((field) => missingMessage(field))
);

/** Filter clauses that name a column but bind no value: a likely arg mismatch. */
const unboundFilterColumns = computed(() =>
  form.value.filter
    .filter(
      (clause) => clause.column.trim() !== "" && clause.values.trim() === ""
    )
    .map((clause) => clause.column)
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
/** The suggestion the page was opened from, for the prefill banner. */
const suggestionInfo = ref<QualitySuggestion>();
const llmEnabled = ref(false);
const llmDialogOpen = ref(false);
const canSuggest = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.suggest")
);
const llmAvailability = computed(() =>
  llmFeatureAvailability(llmEnabled.value, canSuggest.value)
);
const targetColumnDetails = ref<TargetColumnDetail[]>([]);

/** The fixed target the drafting workbench runs against in editor mode. */
const workbenchTarget = computed(() =>
  target.value
    ? {
        tableGuid: target.value.guid,
        metaType: target.value.metaType ?? MetaType.TABLE,
        columns: targetColumns.value,
      }
    : undefined
);

/** The selected rule type's one-line explanation, shown under the select. */
const selectedTypeDescription = computed(() => {
  const key = qualityRuleTypeDescriptionKey(form.value.ruleType);
  return key ? t(key) : "";
});

/** Views and materialized views accept table-level rules only. */
const isViewTarget = computed(
  () =>
    target.value?.metaType === MetaType.VIEW ||
    target.value?.metaType === MetaType.MATERIALIZED_VIEW
);

/** The rule-type sections, with the types this target cannot watch removed. */
const ruleTypeGroups = computed(() => {
  const allowed = allowedRuleTypes(target.value?.metaType);
  const groups = RULE_TYPE_GROUPS.map((group) => ({
    key: group.key,
    types: group.types.filter((ruleType) => allowed.includes(ruleType)),
  })).filter((group) => group.types.length > 0);
  // A stored rule can name a type this target filters out: a legacy row, or an
  // enum this build knows but the target does not accept. Keep the value visible
  // in its own unlabeled group so the select and its description still render; it
  // is never rewritten here.
  if (!allowed.includes(form.value.ruleType)) {
    groups.unshift({ key: "", types: [form.value.ruleType] });
  }
  return groups;
});

/** The save/validation feedback block, scrolled into view when it appears. */
const feedbackRef = ref<HTMLElement>();
/** Whether the target instance has a READ_ONLY data source; undefined = unknown. */
const readOnlyMissing = ref<boolean>();

const targetInstanceName = computed(() => {
  const selection = target.value;
  if (!selection) {
    return "";
  }
  return selection.path.instance || selection.guid.split(";")[0] || "";
});

/** Schema drift is metadata-only, so it needs no query data source. */
const showReadOnlyWarning = computed(
  () =>
    readOnlyMissing.value === true &&
    target.value !== undefined &&
    form.value.ruleType !== QualityRuleType.SCHEMA_DRIFT
);

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
/** The save/preview error split into its readable parts. */
const saveErrorDetails = ref<ParsedRuleError>({ message: "" });
const dryRunError = ref<ParsedRuleError>({ message: "" });
const dryRunErrorRaw = ref("");
const missingFields = ref<MissingField[]>([]);
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
  const label = [targetScopeLabel(target.value.path), path.object]
    .filter((part) => part && part !== "-")
    .join(" · ");
  // A suggestion names the target by GUID only; show it rather than nothing.
  return label || target.value.guid;
});

/** The rule resource name the edit route carries, one segment per name segment. */
const ruleName = computed(() => {
  const param = route.params.ruleId;
  return Array.isArray(param) ? param.join("/") : String(param ?? "");
});

/** The rule's own page, addressed by its whole resource name. */
function ruleDetailRoute(name: string) {
  return {
    name: "DataQualityRuleDetail",
    params: { ruleId: name.split("/") },
  };
}

watch(
  () => [form.value, target.value] as const,
  () => {
    if (missingFields.value.length > 0) {
      missingFields.value = missingRequiredFields(form.value, target.value);
    }
  },
  { deep: true }
);

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
  // The unit select is hidden for a type without a shared threshold metric, so a
  // percentage left over from the previous type would be sent with no control to
  // correct it. Normalize it here instead.
  if (!isThresholdMetricType(ruleType)) {
    form.value.thresholdMetric = QualityThresholdMetric.COUNT;
  }
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
  // A suggestion is accepted only for the target it names. Replacing the target
  // detaches the draft: it stays pending in the suggestion center and the save
  // creates a plain rule for the target the reader picked.
  if (
    suggestionInfo.value &&
    selection.guid !== suggestionInfo.value.target?.metaGuid
  ) {
    suggestionInfo.value = undefined;
    notify.info(t("dataQuality.editor.suggestionDetached"));
  }
  target.value = selection;
  // A target can change the set of legal types; switching to the first allowed
  // type keeps the form saveable rather than failing at the dry run.
  const allowed = allowedRuleTypes(selection.metaType);
  if (!allowed.includes(form.value.ruleType) && allowed[0] !== undefined) {
    changeRuleType(String(allowed[0]));
  }
  void refreshTargetColumns(selection.guid);
  void checkReadOnly(selection);
}

async function focusFeedback() {
  await nextTick();
  feedbackRef.value?.scrollIntoView?.({ behavior: "smooth", block: "center" });
}

/** Scroll to and focus the first field the rule still needs. */
async function focusFirstMissing() {
  await nextTick();
  const first = missingFields.value[0];
  if (!first) {
    return;
  }
  const element = document.getElementById(missingFieldElementId(first.field));
  element?.scrollIntoView?.({ behavior: "smooth", block: "center" });
  (element as HTMLElement | null)?.focus?.();
}

/**
 * The pre-flight check both Dry run and Save run first: a missing required
 * field is reported in place, and no request is sent for a rule that cannot
 * compile. This is what keeps "expected 2 arguments, got 0" from being the
 * first thing the user hears about an incomplete rule.
 */
function ensureComplete(): boolean {
  missingFields.value = missingRequiredFields(form.value, target.value);
  if (missingFields.value.length === 0) {
    return true;
  }
  dryRunError.value = { message: "" };
  dryRunErrorRaw.value = "";
  saveError.value = "";
  issues.value = [];
  void focusFirstMissing();
  return false;
}

/**
 * Query rules run through the instance's READ_ONLY data source. Detect the gap
 * as soon as a target is chosen instead of letting the save fail with a server
 * message; the instance page owns adding the data source.
 */
async function checkReadOnly(selection: TargetSelection | undefined) {
  if (!selection) {
    readOnlyMissing.value = undefined;
    return;
  }
  const instanceId =
    selection.path.instance || selection.guid.split(";")[0] || "";
  if (!instanceId) {
    readOnlyMissing.value = undefined;
    return;
  }
  try {
    const instance = await getInstance(`instances/${instanceId}`);
    readOnlyMissing.value = !instance.dataSources.some(
      (dataSource) => dataSource.type === DataSourceType.READ_ONLY
    );
  } catch {
    // An instance the caller cannot read is not evidence of a missing data
    // source; the server check on save remains the authority.
    readOnlyMissing.value = undefined;
  }
}

/** Column suggestions for the config fields; best effort, free text still works. */
async function refreshTargetColumns(guid: string) {
  try {
    const columns = await listAllMetadata({
      parentGuid: guid,
      metaType: MetaType.COLUMN,
    });
    targetColumnDetails.value = columns.map((item) => {
      const value = item.type.value as
        | { name?: string; type?: string; nullable?: boolean }
        | undefined;
      return {
        name: value?.name ?? item.guid,
        type: value?.type ?? "",
        nullable: value?.nullable ?? true,
      };
    });
    targetColumns.value = targetColumnDetails.value.map(
      (column) => column.name
    );
  } catch {
    targetColumns.value = [];
    targetColumnDetails.value = [];
  }
}

/**
 * A schema-drift snapshot saved before nullability was compared leaves the bit
 * unset. Resolve those rows from the target's current columns after loading, both
 * on the form and on the baseline it is diffed against, so saving writes explicit
 * values without a spurious config change; a column the target no longer has
 * stays unset and is not judged.
 */
function resolveLoadedSchemaDriftNullability() {
  if (form.value.ruleType !== QualityRuleType.SCHEMA_DRIFT) {
    return;
  }
  form.value.config.expectedColumns = resolveExpectedColumnNullability(
    form.value.config.expectedColumns,
    targetColumnDetails.value
  );
  if (originalForm) {
    originalForm.config.expectedColumns = resolveExpectedColumnNullability(
      originalForm.config.expectedColumns,
      targetColumnDetails.value
    );
  }
}

async function loadRule() {
  if (!isEdit.value) {
    await loadNewPage();
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
      // The columns are needed to resolve a snapshot saved before nullability was
      // compared, so resolve after they are on hand.
      await refreshTargetColumns(target.value.guid);
      resolveLoadedSchemaDriftNullability();
      void checkReadOnly(target.value);
    }
  } catch (e: unknown) {
    loadError.value = formatError(e, "dataQuality.editor.loadFailed");
  } finally {
    loadingRule.value = false;
  }
}

/**
 * A new page prefills from a suggestion (`?suggestion=<name>`) or from a target
 * (`?table_guid=...`). The suggestion's draft is converted to the same form the
 * user would have typed, so nothing is created until they save.
 */
async function loadNewPage() {
  const name =
    typeof route.query.suggestion === "string" ? route.query.suggestion : "";
  if (name) {
    try {
      const suggestion = await getQualitySuggestion(name);
      const prefill = suggestionToEditorPrefill(suggestion);
      if (prefill) {
        suggestionInfo.value = suggestion;
        form.value = prefill.form;
        target.value = prefill.target;
        if (target.value) {
          void refreshTargetColumns(target.value.guid);
          void checkReadOnly(target.value);
        }
      }
    } catch {
      // A missing suggestion is not fatal: the page behaves like a blank new rule.
    }
  }
  if (!target.value) {
    applyPrefill();
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
    void checkReadOnly(prefilled);
  }
}

function currentRule(): QualityRule {
  return formToRule(form.value, {
    base: originalRule,
    target: target.value,
  });
}

function runValidation(): boolean {
  issues.value = validateRuleForm(
    form.value,
    target.value,
    targetColumns.value
  );
  saveError.value = "";
  if (issues.value.length > 0) {
    void focusFeedback();
  }
  return issues.value.length === 0;
}

/**
 * Compile and probe the rule without saving it. Returns true when the server
 * accepted it; the compiled SQL is kept for the preview panel.
 */
async function runPreview(): Promise<boolean> {
  previewing.value = true;
  previewError.value = "";
  dryRunError.value = { message: "" };
  dryRunErrorRaw.value = "";
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
    const raw = formatError(e, "dataQuality.editor.previewFailed");
    dryRunErrorRaw.value = raw;
    dryRunError.value = parseRuleError(raw);
    void focusFeedback();
    return false;
  } finally {
    previewing.value = false;
  }
}

async function dryRun() {
  if (!ensureComplete()) {
    return;
  }
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
  if (!ensureComplete()) {
    return;
  }
  if (!runValidation()) {
    return;
  }
  if (canPreview.value && !(await runPreview())) {
    return;
  }
  saving.value = true;
  let savedName = isEdit.value ? ruleName.value : "";
  try {
    const rule = currentRule();
    if (isEdit.value) {
      const mask = computeUpdateMask(
        { form: originalForm ?? form.value, target: originalTarget },
        { form: form.value, target: target.value }
      );
      if (mask.length === 0) {
        notify.info(t("dataQuality.editor.noChanges"));
        await router.push(ruleDetailRoute(ruleName.value));
        return;
      }
      await updateQualityRule(rule, mask);
      notify.success(t("dataQuality.editor.updated"));
    } else {
      // A rule opened from a suggestion is accepted by the same request that
      // creates it: the server takes the target from the suggestion, saves the
      // rule and decides the suggestion.
      const created = await createQualityRule(
        rule,
        suggestionInfo.value?.name ?? ""
      );
      savedName = created.name;
      notify.success(t("dataQuality.editor.created"));
    }
    // Save lands on the rule's own page: it is where the first result and the
    // history will appear, so the writer sees what they just created instead of
    // a list they then have to search.
    await router.push(
      savedName ? ruleDetailRoute(savedName) : { name: "DataQualityRules" }
    );
  } catch (e: unknown) {
    // The server's Invalid message names the field or the compiled-SQL problem;
    // show it at the top where the reader lands, not under the whole form.
    const raw = formatError(e, "dataQuality.editor.saveFailed");
    saveError.value = parseRuleError(raw).message;
    saveErrorDetails.value = parseRuleError(raw);
    void focusFeedback();
  } finally {
    saving.value = false;
  }
}

function cancel() {
  void router.push({ name: "DataQualityRules" });
}

/** Only read the workspace switch; the settings page owns changing it. */
async function loadLlmSetting() {
  try {
    const profile = await getWorkspaceProfileSetting();
    llmEnabled.value = profile.dataQuality?.enableLlmAnalysis ?? false;
  } catch {
    llmEnabled.value = false;
  }
}

/** An accepted draft replaces the form in place, without creating anything. */
function applyAcceptedDraft(suggestion: QualitySuggestion) {
  const prefill = suggestionToEditorPrefill(suggestion);
  if (!prefill) {
    notify.info(t("dataQuality.llm.draft.cannotPrefill"));
    return;
  }
  form.value = prefill.form;
  target.value = prefill.target;
  suggestionInfo.value = suggestion;
  if (target.value) {
    void refreshTargetColumns(target.value.guid);
  }
  notify.success(t("dataQuality.llm.draft.applied"));
}

onMounted(() => {
  void loadLlmSetting();
  void loadRule();
});
</script>
