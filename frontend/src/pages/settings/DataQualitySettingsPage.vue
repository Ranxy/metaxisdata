<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQualitySettings.title')"
      :description="t('dataQualitySettings.description')"
    >
      <template #actions>
        <span
          v-if="!canUpdate"
          class="text-sm text-muted-foreground"
        >
          {{ t("dataQualitySettings.readOnlyHint") }}
        </span>
        <span
          v-else-if="isDirty && !isLoading && !error"
          class="text-sm text-muted-foreground"
        >
          {{ t("dataQualitySettings.unsavedHint") }}
        </span>
        <Button
          :disabled="isLoading || !!error || isSaving || !canUpdate"
          @click="handleSave"
        >
          {{ t("common.save") }}
        </Button>
      </template>
    </PageHeader>

    <PageState
      :loading="isLoading"
      :error="error"
    >
      <template #error>
        <Button
          variant="outline"
          size="sm"
          @click="load"
        >
          {{ t("common.refresh") }}
        </Button>
      </template>

      <div class="flex flex-col gap-4">
        <Alert
          v-if="saveError"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>{{ t("dataQualitySettings.saveFailed") }}</AlertTitle>
          <AlertDescription class="break-words">
            {{ saveError }}
          </AlertDescription>
        </Alert>

        <Alert
          v-if="issues.length > 0"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>{{ t("dataQualitySettings.validationTitle") }}</AlertTitle>
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

        <Card>
          <CardHeader>
            <CardTitle>{{ t("dataQualitySettings.llmSection") }}</CardTitle>
            <CardDescription>
              {{ t("dataQualitySettings.llmSectionDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-5">
            <div class="flex items-start gap-3">
              <Checkbox
                id="dq-setting-enable-llm"
                :checked="form.enableLlmAnalysis"
                :disabled="!canUpdate"
                @update:checked="form.enableLlmAnalysis = $event === true"
              />
              <div class="grid gap-1">
                <Label for="dq-setting-enable-llm">
                  {{ t("dataQualitySettings.enableLlmAnalysis") }}
                </Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("dataQualitySettings.enableLlmAnalysisHint") }}
                </p>
              </div>
            </div>

            <div class="space-y-1.5">
              <Label for="dq-setting-llm-profile">
                {{ t("dataQualitySettings.llmProfile") }}
              </Label>
              <Select
                :model-value="form.llmProviderProfile || AUTO_PROFILE"
                :disabled="!canUpdate || llmDisabled(form)"
                @update:model-value="setLlmProfile"
              >
                <SelectTrigger id="dq-setting-llm-profile">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem :value="AUTO_PROFILE">
                    {{ t("dataQualitySettings.llmProfileAuto") }}
                  </SelectItem>
                  <SelectItem
                    v-for="profile in profileOptions"
                    :key="profile.name"
                    :value="profile.name"
                  >
                    {{ profile.title }}
                  </SelectItem>
                </SelectContent>
              </Select>
              <p class="text-sm text-muted-foreground">
                {{ llmDisabled(form) ? t("dataQualitySettings.llmDisabledHint") : t("dataQualitySettings.llmProfileHint") }}
              </p>
            </div>

            <div class="flex items-start gap-3">
              <Checkbox
                id="dq-setting-sample-values"
                :checked="form.sendSampleValuesToLlm"
                :disabled="!canUpdate || llmDisabled(form)"
                @update:checked="form.sendSampleValuesToLlm = $event === true"
              />
              <div class="grid gap-1">
                <Label for="dq-setting-sample-values">
                  {{ t("dataQualitySettings.sendSampleValues") }}
                </Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("dataQualitySettings.sendSampleValuesHint") }}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("dataQualitySettings.samplesSection") }}</CardTitle>
            <CardDescription>
              {{ t("dataQualitySettings.samplesSectionDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-5">
            <div class="flex items-start gap-3">
              <Checkbox
                id="dq-setting-store-samples"
                :checked="form.storeFailureSamples"
                :disabled="!canUpdate"
                @update:checked="form.storeFailureSamples = $event === true"
              />
              <div class="grid gap-1">
                <Label for="dq-setting-store-samples">
                  {{ t("dataQualitySettings.storeFailureSamples") }}
                </Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("dataQualitySettings.storeFailureSamplesHint") }}
                </p>
              </div>
            </div>
            <FormField
              id="dq-setting-sample-limit"
              v-model="form.failureSampleLimit"
              type="number"
              class="max-w-xs"
              :disabled="!canUpdate"
              :label="t('dataQualitySettings.failureSampleLimit')"
              :hint="t('dataQualitySettings.failureSampleLimitHint')"
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("dataQualitySettings.limitsSection") }}</CardTitle>
            <CardDescription>
              {{ t("dataQualitySettings.limitsSectionDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-5">
            <div class="space-y-1.5">
              <Label for="dq-setting-schedule">
                {{ t("dataQualitySettings.defaultSchedule") }}
              </Label>
              <div class="flex max-w-xs gap-2">
                <FormField
                  id="dq-setting-schedule"
                  v-model="form.defaultSchedule.value"
                  type="number"
                  class="min-w-0 flex-1"
                  :disabled="!canUpdate"
                />
                <Select
                  :model-value="form.defaultSchedule.unit"
                  :disabled="!canUpdate"
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
                {{ t("dataQualitySettings.defaultScheduleHint") }}
              </p>
            </div>

            <div class="grid gap-4 sm:grid-cols-3">
              <FormField
                id="dq-setting-daily-executions"
                v-model="form.dailyExecutionLimit"
                type="number"
                :disabled="!canUpdate"
                :label="t('dataQualitySettings.dailyExecutionLimit')"
                :hint="t('dataQualitySettings.dailyExecutionLimitHint')"
              />
              <FormField
                id="dq-setting-daily-columns"
                v-model="form.dailyColumnAnalysisLimit"
                type="number"
                :disabled="!canUpdate"
                :label="t('dataQualitySettings.dailyColumnAnalysisLimit')"
                :hint="t('dataQualitySettings.dailyColumnAnalysisLimitHint')"
              />
              <FormField
                id="dq-setting-daily-llm-analyses"
                v-model="form.dailyLlmAnalysisLimit"
                type="number"
                :disabled="!canUpdate"
                :label="t('dataQualitySettings.dailyLlmAnalysisLimit')"
                :hint="t('dataQualitySettings.dailyLlmAnalysisLimitHint')"
              />
              <FormField
                id="dq-setting-llm-concurrency"
                v-model="form.llmConcurrencyLimit"
                type="number"
                :disabled="!canUpdate"
                :label="t('dataQualitySettings.llmConcurrencyLimit')"
                :hint="t('dataQualitySettings.llmConcurrencyLimitHint')"
              />
              <FormField
                id="dq-setting-max-columns"
                v-model="form.maxColumnsPerAnalysis"
                type="number"
                :disabled="!canUpdate"
                :label="t('dataQualitySettings.maxColumnsPerAnalysis')"
                :hint="t('dataQualitySettings.maxColumnsPerAnalysisHint')"
              />
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("dataQualitySettings.usageSection") }}</CardTitle>
            <CardDescription>
              {{ t("dataQualitySettings.usageSectionDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p
              v-if="!canSuggest"
              class="text-sm text-muted-foreground"
            >
              {{ t("dataQualitySettings.usageNoPermission") }}
            </p>
            <div
              v-else-if="usageLoading"
              class="flex items-center gap-2 text-sm text-muted-foreground"
            >
              <Loader2 class="h-4 w-4 animate-spin" />
              {{ t("common.loading") }}
            </div>
            <Alert
              v-else-if="usageError"
              variant="warning"
            >
              <AlertTriangle class="h-4 w-4" />
              <AlertTitle>{{ t("dataQualitySettings.usageFetchFailed") }}</AlertTitle>
              <AlertDescription class="space-y-2">
                <p class="break-words">{{ usageError }}</p>
                <Button
                  variant="outline"
                  size="sm"
                  @click="loadUsage"
                >
                  {{ t("common.refresh") }}
                </Button>
              </AlertDescription>
            </Alert>
            <div
              v-else-if="usage"
              class="grid gap-4 sm:grid-cols-2"
            >
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQualitySettings.usageDailyAnalyses") }}
                </div>
                <div class="text-sm font-medium">
                  {{ ratio(usage.dailyAnalysisUsed, usage.dailyAnalysisLimit) }}
                </div>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQualitySettings.usageDailyColumnAnalyses") }}
                </div>
                <div class="text-sm font-medium">
                  {{ ratio(usage.dailyColumnAnalysisUsed, usage.dailyColumnAnalysisLimit) }}
                </div>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQualitySettings.usageConcurrency") }}
                </div>
                <div class="text-sm font-medium">
                  {{ ratio(usage.inFlight, usage.concurrencyLimit) }}
                </div>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQualitySettings.usageResetsAt") }}
                </div>
                <div
                  class="text-sm font-medium"
                  :title="formatDateTime(usage.resetsAt, locale)"
                >
                  {{ formatRelativeTime(usage.resetsAt, locale) }}
                </div>
              </div>
            </div>
            <p
              v-else
              class="text-sm text-muted-foreground"
            >
              -
            </p>
          </CardContent>
        </Card>
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import { AlertTriangle, Loader2 } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { getQualityLLMUsage } from "@/api/data-quality";
import { listProfiles } from "@/api/llm";
import {
  getWorkspaceProfileSetting,
  updateDataQualitySetting,
} from "@/api/setting";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
import type { QualityLLMUsage } from "@/types/proto-es/v1/data_quality_service_pb";
import { formatRowCount } from "@/utils/dataQuality";
import type { ScheduleUnit } from "@/utils/dataQualityForm";
import {
  type DataQualitySettingsForm,
  formToSetting,
  llmDisabled,
  settingsToForm,
  validateSettingsForm,
} from "@/utils/dataQualitySettings";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { isProviderAllowed } from "@/utils/llmProvider";

/**
 * The data quality subsystem's workspace configuration. Every field is part of
 * `WorkspaceProfileSetting.data_quality`; the server returns the effective
 * values (its defaults filled in), and a save writes only that field.
 */

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const form = ref<DataQualitySettingsForm>(settingsToForm(undefined));
const profileOptions = ref<Array<{ name: string; title: string }>>([]);
const issues = ref<ReturnType<typeof validateSettingsForm>>([]);
const isLoading = ref(true);
const isSaving = ref(false);
const error = ref("");
const saveError = ref("");
const original = ref("");
const usage = ref<QualityLLMUsage>();
const usageLoading = ref(false);
const usageError = ref("");

const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.settings.update")
);
// The usage RPC is part of the data-quality suggest surface, not settings.
const canSuggest = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.suggest")
);

const isDirty = computed(() => JSON.stringify(form.value) !== original.value);

/**
 * Radix's Select cannot hold an empty string item, so "the first allowed
 * profile" needs a sentinel; it is mapped back to "" on the way into the form.
 */
const AUTO_PROFILE = "-";

function setLlmProfile(value: unknown) {
  form.value.llmProviderProfile =
    String(value) === AUTO_PROFILE ? "" : String(value);
}

function setScheduleUnit(value: unknown) {
  form.value.defaultSchedule.unit = String(value) as ScheduleUnit;
}

async function load() {
  isLoading.value = true;
  error.value = "";
  saveError.value = "";
  try {
    const profile = await getWorkspaceProfileSetting();
    form.value = settingsToForm(profile.dataQuality);
    original.value = JSON.stringify(form.value);
    issues.value = [];
    await loadProfiles(profile.allowedLlmProviderProfiles ?? []);
    void loadUsage();
  } catch (e: unknown) {
    error.value = formatError(e, "dataQualitySettings.fetchError");
  } finally {
    isLoading.value = false;
  }
}

/** The profiles the workspace allows and that have an enabled model. */
async function loadProfiles(allowed: string[]) {
  try {
    const response = await listProfiles({ pageSize: 100 });
    profileOptions.value = response.profiles
      .filter((profile) => profile.models.some((model) => model.enabled))
      .filter((profile) => isProviderAllowed(profile.name, allowed))
      .map((profile) => ({
        name: profile.name,
        title: profile.title || profile.name,
      }));
    // A configured profile the workspace no longer allows would render as an
    // empty select; clear it so the effective choice stays visible and honest.
    if (
      form.value.llmProviderProfile &&
      !profileOptions.value.some(
        (profile) => profile.name === form.value.llmProviderProfile
      )
    ) {
      form.value.llmProviderProfile = "";
    }
  } catch {
    profileOptions.value = [];
  }
}

/**
 * Read-only counters for the process-wide analysis quota. They are process
 * state, so they are refetched on load and again after a save applies new
 * limits (the save path calls `load`, which calls this).
 */
async function loadUsage() {
  if (!canSuggest.value) {
    usage.value = undefined;
    usageError.value = "";
    usageLoading.value = false;
    return;
  }
  usageLoading.value = true;
  usageError.value = "";
  try {
    usage.value = await getQualityLLMUsage();
  } catch (e: unknown) {
    usage.value = undefined;
    usageError.value = formatError(e, "dataQualitySettings.usageFetchFailed");
  } finally {
    usageLoading.value = false;
  }
}

/** `used / limit`, both localised. */
function ratio(used: number, limit: number): string {
  return t("dataQualitySettings.usageRatio", {
    used: formatRowCount(used, locale.value),
    limit: formatRowCount(limit, locale.value),
  });
}

async function handleSave() {
  issues.value = validateSettingsForm(form.value);
  saveError.value = "";
  if (issues.value.length > 0) {
    return;
  }
  isSaving.value = true;
  try {
    await updateDataQualitySetting(formToSetting(form.value));
    notify.success(t("dataQualitySettings.saved"));
    await load();
  } catch (e: unknown) {
    saveError.value = formatError(e, "dataQualitySettings.saveFailed");
  } finally {
    isSaving.value = false;
  }
}

onMounted(load);
</script>
