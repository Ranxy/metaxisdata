<template>
  <div class="flex min-h-0 flex-1 flex-col gap-3">
    <PageHeader
      :title="t('llmProvider.sidebar')"
      class="shrink-0"
    />

    <!-- Top bar -->
    <div class="flex items-center gap-2 shrink-0">
      <div class="flex-1">
        <FormField
          v-model="searchQuery"
          :placeholder="t('llmProvider.searchPlaceholder')"
        >
          <template #suffix>
            <Search class="absolute right-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          </template>
        </FormField>
      </div>
      <Button @click="startCreate">
        <Plus class="h-4 w-4 mr-2" />
        {{ t("llmProvider.addProfile") }}
      </Button>
    </div>

    <!-- Two-panel layout -->
    <div class="grid grid-cols-[16rem_minmax(0,1fr)] min-h-0 flex-1 border border-border rounded-lg overflow-hidden">
      <!-- Left panel: profile list -->
      <div class="border-r border-border p-2 overflow-y-auto bg-muted/30">
        <!-- Only the search case is explained here: with no profiles at all,
             the detail pane already says what to do, and two empty messages for
             the same condition read as two different problems. -->
        <div
          v-if="filteredProfiles.length === 0 && searchQuery"
          class="p-4 text-center text-sm text-muted-foreground"
        >
          <p>{{ t("llmProvider.noMatch") }}</p>
        </div>

        <div v-else class="space-y-0.5">
          <button
            v-for="p in filteredProfiles"
            :key="p.name"
            :class="[
              'flex items-center gap-2.5 w-full text-left py-2 px-2.5 rounded-md text-sm transition-colors',
              editingProfile?.name === p.name
                ? 'bg-accent text-accent-foreground'
                : 'hover:bg-accent/50',
            ]"
            @click="editProfile(p)"
          >
            <div class="flex-1 min-w-0">
              <div class="truncate font-medium">{{ profileDisplayTitle(p) }}</div>
              <div class="text-xs text-muted-foreground flex items-center gap-1.5 mt-0.5">
                <Badge variant="secondary" class="text-[10px] px-1 py-0">
                  {{ providerLabel(p.type) }}
                </Badge>
                <span v-if="p.baseUrl" class="truncate font-mono text-[10px]">{{ p.baseUrl }}</span>
              </div>
            </div>
            <Badge variant="outline" class="text-xs shrink-0">
              {{ enabledCount(p) }}
            </Badge>
          </button>
        </div>
      </div>

      <!-- Right panel -->
      <div class="p-5 overflow-y-auto flex flex-col gap-4">
        <!-- Empty / banner -->
        <div
          v-if="!editingProfile && !isCreating"
          class="flex flex-1 flex-col items-center justify-center gap-3 text-center text-muted-foreground"
        >
          <Sparkles class="h-12 w-12 opacity-30" />
          <p class="max-w-sm text-sm">
            {{ profiles.length === 0 ? t("llmProvider.empty") : t("llmProvider.selectHint") }}
          </p>
          <Button
            v-if="profiles.length === 0"
            @click="startCreate"
          >
            <Plus class="mr-2 h-4 w-4" />
            {{ t("llmProvider.addProfile") }}
          </Button>
        </div>

        <!-- Form -->
        <template v-else>
          <div class="flex flex-col gap-2.5">
            <h2 class="text-lg font-semibold">
              {{ editingProfile ? t("llmProvider.editProfile") : t("llmProvider.newProfile") }}
            </h2>
          </div>
          <Separator />

          <!-- Provider Type -->
          <div class="space-y-2">
            <Label>{{ t("llmProvider.providerType") }}</Label>
            <Select
              :model-value="String(formData.providerType)"
              :disabled="!!editingProfile"
              @update:model-value="setProviderType"
            >
              <SelectTrigger class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="d in BUILTIN_DEFS"
                  :key="d.id"
                  :value="String(d.enum)"
                >
                  {{ builtinLabel(d, t) }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <!-- Title -->
          <div class="space-y-2">
            <Label>{{ t("llmProvider.profileTitle") }}</Label>
            <FormField
              v-model="formData.title"
              :placeholder="autoTitlePreview"
            />
          </div>

          <!-- Base URL -->
          <div class="space-y-2">
            <Label>{{ t("llmProvider.baseUrl") }}</Label>
            <FormField
              v-model="formData.baseUrl"
              :placeholder="t('llmProvider.baseUrlPlaceholder')"
            />
          </div>

          <!-- API Key -->
          <div class="space-y-2">
            <div class="flex items-center justify-between">
              <Label>{{ t("llmProvider.apiKey") }}</Label>
              <a
                v-if="apiKeyLink"
                :href="apiKeyLink"
                target="_blank"
                class="text-xs text-accent hover:underline"
              >
                {{ t("llmProvider.getKey") }}
              </a>
            </div>
            <FormField
              v-model="formData.apiKey"
              type="password"
              :placeholder="editingProfile?.maskedApiKey || t('llmProvider.apiKeyPlaceholder')"
            />
          </div>

          <Separator />

          <!-- Models section -->
          <div class="space-y-2">
            <div class="flex items-center justify-between">
              <Label>
                {{ t("llmProvider.models") }}
                <span v-if="modelRows.length > 0" class="text-muted-foreground font-normal">
                  ({{ enabledModelCount }} {{ t("llmProvider.enabled") }})
                </span>
              </Label>
              <Button
                variant="outline"
                size="sm"
                :disabled="isFetchingModels"
                @click="fetchModels"
              >
                <RefreshCw :class="['h-4 w-4 mr-1', isFetchingModels && 'animate-spin']" />
                {{ isFetchingModels ? t("llmProvider.fetching") : t("llmProvider.fetchModels") }}
              </Button>
            </div>

            <p
              v-if="limitsError"
              role="alert"
              class="text-destructive text-sm"
            >
              {{ t("llmProvider.limitsUnavailable") }}
            </p>
            <div v-if="fetchedError" role="alert" class="text-destructive text-sm">{{ fetchedError }}</div>

            <template v-if="modelRows.length > 0">
              <div class="relative">
                <Search class="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
                <input
                  v-model="modelSearch"
                  :placeholder="t('llmProvider.filterModels')"
                  type="search"
                  class="w-full bg-input-surface border border-border rounded-md py-[5px] pl-8 pr-3 text-sm outline-none focus:border-accent"
                />
              </div>

              <div class="max-h-96 overflow-y-auto border rounded-md p-2 space-y-1">
                <Collapsible
                  v-for="entry in filteredModelRows"
                  :key="entry.index"
                  :open="entry.row.expanded"
                  class="rounded-md border border-border/60 px-2 py-1.5"
                  @update:open="(open: boolean) => setRowExpanded(entry.index, open)"
                >
                  <div class="flex items-center gap-2">
                    <Checkbox
                      :checked="entry.row.enabled"
                      :aria-label="t('llmProvider.enableModel', { name: entry.row.name || `#${entry.index + 1}` })"
                      @update:checked="(checked: boolean) => setRowEnabled(entry.index, checked)"
                    />
                    <Input
                      v-model="entry.row.name"
                      :placeholder="t('llmProvider.modelNamePlaceholder')"
                      :aria-label="`${t('llmProvider.modelName')} ${entry.index + 1}`"
                      class="h-8 min-w-0 flex-1 border-0 bg-transparent px-1 focus-visible:ring-0"
                    />
                    <span class="text-xs text-muted-foreground whitespace-nowrap">
                      {{ capacitySummary(entry.row) }}
                    </span>
                    <CollapsibleTrigger as-child>
                      <Button
                        variant="ghost"
                        size="icon"
                        class="h-7 w-7"
                        :aria-label="`${t('llmProvider.modelAdvanced')} ${entry.index + 1}`"
                        :title="t('llmProvider.modelAdvanced')"
                      >
                        <ChevronDown v-if="entry.row.expanded" class="h-4 w-4" />
                        <ChevronRight v-else class="h-4 w-4" />
                      </Button>
                    </CollapsibleTrigger>
                    <Button
                      variant="ghost"
                      size="icon"
                      class="h-7 w-7 text-destructive hover:bg-destructive/10"
                      :aria-label="`${t('llmProvider.removeModel')} ${entry.index + 1}`"
                      :title="t('llmProvider.removeModel')"
                      @click="removeModel(entry.index)"
                    >
                      <Trash2 class="h-4 w-4" />
                    </Button>
                  </div>

                  <p v-if="nameError(entry.index)" class="pl-7 pt-1 text-xs text-destructive">
                    {{ nameError(entry.index) }}
                  </p>
                  <p
                    v-if="!entry.row.expanded && capacityError(entry.index)"
                    class="pl-7 pt-1 text-xs text-destructive"
                  >
                    {{ capacityError(entry.index) }}
                  </p>

                  <CollapsibleContent>
                    <div class="grid grid-cols-2 gap-3 pl-7 pt-2">
                      <FormField
                        :label="t('llmProvider.contextWindow')"
                        :model-value="entry.row.contextWindow"
                        :placeholder="contextWindowPlaceholder"
                        :hint="limits ? t('llmProvider.contextWindowHint', { value: defaultContextWindowLabel }) : ''"
                        :error="fieldError(entry.index, 'contextWindowInvalid')"
                        inputmode="numeric"
                        @update:model-value="(value: string | number) => setRowContextWindow(entry.index, String(value))"
                      />
                      <FormField
                        :label="t('llmProvider.maxOutputTokens')"
                        :model-value="entry.row.maxOutputTokens"
                        :placeholder="maxOutputTokensPlaceholder"
                        :hint="limits ? t('llmProvider.maxOutputTokensHint', { value: defaultMaxOutputTokensLabel, min: minMaxOutputTokens, max: maxMaxOutputTokens }) : ''"
                        :error="fieldError(entry.index, 'modelMaxOutputTokensInvalid') || fieldError(entry.index, 'modelCapacityMismatch')"
                        inputmode="numeric"
                        @update:model-value="(value: string | number) => setRowMaxOutputTokens(entry.index, String(value))"
                      />
                    </div>
                  </CollapsibleContent>
                </Collapsible>

                <div
                  v-if="filteredModelRows.length === 0"
                  class="text-muted-foreground text-xs text-center py-2"
                >
                  {{ t("llmProvider.noModelMatch") }}
                </div>
              </div>

              <p v-if="invalidRowCount > 0" class="text-xs text-destructive">
                {{ t("llmProvider.rowsNeedFix", { count: invalidRowCount }) }}
              </p>

              <Button
                variant="outline"
                size="sm"
                @click="addManualModel"
              >
                <Plus class="h-4 w-4 mr-1" />
                {{ t("llmProvider.addModel") }}
              </Button>
            </template>

            <div
              v-else-if="!isFetchingModels"
              class="flex flex-col items-center gap-2 text-muted-foreground text-sm text-center py-4 border rounded-md"
            >
              <p>{{ t("llmProvider.fetchHint") }}</p>
              <Button
                variant="outline"
                size="sm"
                @click="addManualModel"
              >
                <Plus class="h-4 w-4 mr-1" />
                {{ t("llmProvider.addModel") }}
              </Button>
            </div>
          </div>

          <!-- Actions -->
          <div class="flex items-center justify-between mt-auto pt-4 border-t border-border">
            <Button
              v-if="editingProfile"
              variant="outline"
              class="text-destructive hover:bg-destructive/10"
              :disabled="isSaving"
              @click="deleteCurrent"
            >
              {{ t("common.delete") }}
            </Button>
            <div v-else />
            <div class="flex gap-2">
              <Button variant="outline" @click="cancelForm">
                {{ t("common.cancel") }}
              </Button>
              <Button :disabled="!canSave || isSaving" @click="saveProfile">
                {{ isSaving ? t("llmProvider.saving") : t("common.save") }}
              </Button>
            </div>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import {
  ChevronDown,
  ChevronRight,
  Plus,
  RefreshCw,
  Search,
  Sparkles,
  Trash2,
} from "lucide-vue-next";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  createProfile,
  deleteProfile,
  fetchModelsByKey,
  fetchModelsByProfile,
  listProfiles,
  updateProfile,
} from "@/api/llm";
import PageHeader from "@/components/layout/PageHeader.vue";
import Badge from "@/components/ui/badge/Badge.vue";
import Button from "@/components/ui/button/Button.vue";
import Checkbox from "@/components/ui/checkbox/Checkbox.vue";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { FormField } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import Label from "@/components/ui/label/Label.vue";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import Separator from "@/components/ui/separator/Separator.vue";
import { useErrorHandler } from "@/composables/useErrorHandler";
import {
  LLMProviderType,
  type LlmProviderLimits,
  LlmProviderModelSchema,
  type LlmProviderProfile,
} from "@/types/proto-es/v1/llm_service_pb";
import type { Translate } from "@/utils/i18n";
import {
  type DiscoveredModelCapacity,
  formatTokenCapacity,
  type ModelCapacityError,
  type ModelCapacityLimits,
  type ModelCapacityRow,
  mergeDiscoveredModels,
  parseTokenCapacity,
  validateModelRows,
} from "@/utils/llmProvider";

// ---- static builtin catalog (mirrors backend builtinDefinitions) ----
interface BuiltinDef {
  id: string;
  defaultBaseUrl: string;
  enum: LLMProviderType;
}

const BUILTIN_DEFS: BuiltinDef[] = [
  {
    id: "openai",
    defaultBaseUrl: "https://api.openai.com",
    enum: LLMProviderType.LLM_PROVIDER_TYPE_OPENAI,
  },
  {
    id: "deepseek",
    defaultBaseUrl: "https://api.deepseek.com",
    enum: LLMProviderType.LLM_PROVIDER_TYPE_DEEPSEEK,
  },
  {
    id: "openrouter",
    defaultBaseUrl: "https://openrouter.ai/api",
    enum: LLMProviderType.LLM_PROVIDER_TYPE_OPENROUTER,
  },
  {
    id: "custom",
    defaultBaseUrl: "",
    enum: LLMProviderType.LLM_PROVIDER_TYPE_CUSTOM,
  },
];

function getBuiltinByEnum(e: LLMProviderType): BuiltinDef | undefined {
  return BUILTIN_DEFS.find((d) => d.enum === e);
}

/**
 * The provider's display name. Brand names read the same in every locale, but
 * they are catalogued like any other display text so a translator can see them
 * and so "Custom" is not the one English word left in the UI.
 */
function builtinLabel(def: BuiltinDef, t: Translate): string {
  switch (def.id) {
    case "openai":
      return t("llmProvider.builtinOpenai");
    case "deepseek":
      return t("llmProvider.builtinDeepseek");
    case "openrouter":
      return t("llmProvider.builtinOpenrouter");
    default:
      return t("llmProvider.builtinCustom");
  }
}

/** One editable model row: the shared capacity state plus its disclosure. */
type EditableModelRow = ModelCapacityRow & { expanded: boolean };

const { t } = useI18n();
const { formatError, handleError, showSuccess } = useErrorHandler();

// ---- state ----
const profiles = ref<LlmProviderProfile[]>([]);
const isCreating = ref(false);
const limits = ref<ModelCapacityLimits | undefined>(undefined);
const limitsError = ref(false);

// Left panel
const searchQuery = ref("");
const editingProfile = ref<LlmProviderProfile | null>(null);

// Right panel form
const formData = reactive({
  providerType: LLMProviderType.LLM_PROVIDER_TYPE_OPENAI,
  title: "",
  baseUrl: "",
  apiKey: "",
});

const modelRows = ref<EditableModelRow[]>([]);
const modelSearch = ref("");
const isFetchingModels = ref(false);
const fetchedError = ref<string | null>(null);
const isSaving = ref(false);

// ---- computed ----
const filteredProfiles = computed(() => {
  const q = searchQuery.value.trim().toLowerCase();
  if (!q) return profiles.value;
  return profiles.value.filter(
    (p) =>
      p.title.toLowerCase().includes(q) ||
      providerLabel(p.type).toLowerCase().includes(q) ||
      p.models.some((m) => m.name.toLowerCase().includes(q))
  );
});

const enabledModelCount = computed(
  () => modelRows.value.filter((row) => row.enabled).length
);

const filteredModelRows = computed(() => {
  const q = modelSearch.value.trim().toLowerCase();
  return modelRows.value
    .map((row, index) => ({ row, index }))
    .filter(({ row }) => !q || row.name.toLowerCase().includes(q));
});

const defaultContextWindow = computed(
  () => limits.value?.defaultContextWindow ?? 0
);
const defaultMaxOutputTokens = computed(
  () => limits.value?.defaultMaxOutputTokens ?? 0
);
const minMaxOutputTokens = computed(
  () => limits.value?.minMaxOutputTokens ?? 0
);
const maxMaxOutputTokens = computed(
  () => limits.value?.maxMaxOutputTokens ?? 0
);
const defaultContextWindowLabel = computed(() =>
  defaultContextWindow.value > 0
    ? formatTokenCapacity(defaultContextWindow.value)
    : ""
);
const defaultMaxOutputTokensLabel = computed(() =>
  defaultMaxOutputTokens.value > 0
    ? formatTokenCapacity(defaultMaxOutputTokens.value)
    : ""
);
const contextWindowPlaceholder = computed(() =>
  limits.value ? String(limits.value.defaultContextWindow) : undefined
);
const maxOutputTokensPlaceholder = computed(() =>
  limits.value ? String(limits.value.defaultMaxOutputTokens) : undefined
);

const modelRowErrors = computed<(ModelCapacityError | undefined)[]>(() => {
  if (!limits.value) {
    return modelRows.value.map(() => undefined);
  }
  return validateModelRows(modelRows.value, limits.value);
});

const rowsValid = computed(() =>
  modelRowErrors.value.every((error) => error === undefined)
);

const invalidRowCount = computed(
  () => modelRowErrors.value.filter((error) => error !== undefined).length
);

const canSave = computed(
  () =>
    formData.providerType !== LLMProviderType.LLM_PROVIDER_TYPE_UNSPECIFIED &&
    limits.value !== undefined &&
    rowsValid.value
);

const autoTitlePreview = computed(() => {
  const def = getBuiltinByEnum(formData.providerType);
  const label = def ? builtinLabel(def, t) : "";
  const models = modelRows.value
    .filter((row) => row.enabled && row.name.trim() !== "")
    .map((row) => row.name.trim());
  if (models.length === 0) return label;
  return `${label} — ${models.slice(0, 3).join(", ")}${models.length > 3 ? "..." : ""}`;
});

const apiKeyLink = computed(() => {
  const def = getBuiltinByEnum(formData.providerType);
  if (!def) return null;
  switch (def.id) {
    case "openai":
      return "https://platform.openai.com/api-keys";
    case "deepseek":
      return "https://platform.deepseek.com/api_keys";
    case "openrouter":
      return "https://openrouter.ai/settings/keys";
    default:
      return null;
  }
});

// ---- lifecycle ----
onMounted(async () => {
  await loadData();
});

// ---- methods ----
function toCapacityLimits(
  pbLimits: LlmProviderLimits | undefined
): ModelCapacityLimits | undefined {
  if (!pbLimits) {
    return undefined;
  }
  const limits: ModelCapacityLimits = {
    defaultContextWindow: pbLimits.defaultContextWindow,
    minContextWindow: pbLimits.minContextWindow,
    maxContextWindow: pbLimits.maxContextWindow,
    defaultMaxOutputTokens: pbLimits.defaultMaxOutputTokens,
    minMaxOutputTokens: pbLimits.minMaxOutputTokens,
    maxMaxOutputTokens: pbLimits.maxMaxOutputTokens,
  };
  const values = Object.values(limits);
  if (values.some((value) => !Number.isInteger(value) || value <= 0)) {
    return undefined;
  }
  if (
    limits.minContextWindow > limits.maxContextWindow ||
    limits.defaultContextWindow < limits.minContextWindow ||
    limits.defaultContextWindow > limits.maxContextWindow ||
    limits.minMaxOutputTokens > limits.maxMaxOutputTokens ||
    limits.defaultMaxOutputTokens < limits.minMaxOutputTokens ||
    limits.defaultMaxOutputTokens > limits.maxMaxOutputTokens
  ) {
    return undefined;
  }
  return limits;
}

async function loadData() {
  try {
    const resp = await listProfiles();
    profiles.value = resp.profiles;
    limits.value = toCapacityLimits(resp.limits);
    limitsError.value = limits.value === undefined;
  } catch (e) {
    limitsError.value = true;
    handleError(e, "llmProvider.fetchError");
  }
}

function profileDisplayTitle(p: LlmProviderProfile): string {
  return p.title || autoTitle(p);
}

function autoTitle(p: LlmProviderProfile): string {
  const def = getBuiltinByEnum(p.type);
  const label = def ? builtinLabel(def, t) : "";
  const models = p.models.filter((m) => m.enabled).map((m) => m.name);
  if (models.length === 0) return label;
  return `${label} — ${models.slice(0, 2).join(", ")}${models.length > 2 ? ` +${models.length - 2}` : ""}`;
}

/** Radix hands back a string; the form stores the enum. */
function setProviderType(value: unknown) {
  formData.providerType = Number(value) as LLMProviderType;
  const def = getBuiltinByEnum(formData.providerType);
  formData.baseUrl = def?.defaultBaseUrl ?? "";
}

function providerLabel(type: LLMProviderType): string {
  const def = getBuiltinByEnum(type);
  return def ? builtinLabel(def, t) : t("llmProvider.providerUnknown");
}

function enabledCount(p: LlmProviderProfile): number {
  return p.models.filter((m) => m.enabled).length;
}

function editProfile(p: LlmProviderProfile) {
  isCreating.value = false;
  editingProfile.value = p;
  formData.providerType = p.type;
  formData.title = p.title;
  formData.baseUrl = p.baseUrl;
  formData.apiKey = "";
  modelRows.value = p.models.map((model) => ({
    name: model.name,
    enabled: model.enabled,
    contextWindow:
      model.contextWindow > 0 ? formatTokenCapacity(model.contextWindow) : "",
    maxOutputTokens:
      model.maxOutputTokens > 0
        ? formatTokenCapacity(model.maxOutputTokens)
        : "",
    expanded: false,
  }));
  modelSearch.value = "";
  fetchedError.value = null;
}

function startCreate() {
  isCreating.value = true;
  editingProfile.value = null;
  resetForm();
  const def = BUILTIN_DEFS[0];
  formData.providerType = def.enum;
  formData.baseUrl = def.defaultBaseUrl;
}

function resetForm() {
  formData.title = "";
  formData.apiKey = "";
  modelRows.value = [];
  modelSearch.value = "";
  fetchedError.value = null;
}

function cancelForm() {
  isCreating.value = false;
  editingProfile.value = null;
  resetForm();
}

function setRowEnabled(index: number, checked: boolean) {
  const row = modelRows.value[index];
  if (row) {
    row.enabled = checked;
  }
}

function setRowContextWindow(index: number, value: string) {
  const row = modelRows.value[index];
  if (row) {
    row.contextWindow = value;
  }
}

function setRowMaxOutputTokens(index: number, value: string) {
  const row = modelRows.value[index];
  if (row) {
    row.maxOutputTokens = value;
  }
}

function setRowExpanded(index: number, open: boolean) {
  const row = modelRows.value[index];
  if (row) {
    row.expanded = open;
  }
}

function addManualModel() {
  modelRows.value.push({
    name: "",
    enabled: true,
    contextWindow: "",
    maxOutputTokens: "",
    expanded: true,
  });
  modelSearch.value = "";
}

function removeModel(index: number) {
  modelRows.value.splice(index, 1);
}

/** The collapsed row summary: effective capacities, or the server default. */
function capacitySummary(row: ModelCapacityRow): string {
  return t("llmProvider.capacitySummary", {
    context: displayCapacity(row.contextWindow),
    output: displayCapacity(row.maxOutputTokens),
  });
}

function displayCapacity(text: string): string {
  const trimmed = text.trim();
  if (trimmed === "") {
    return t("llmProvider.serverDefault");
  }
  const parsed = parseTokenCapacity(trimmed);
  if (parsed === undefined || Number.isNaN(parsed)) {
    return "—";
  }
  if (parsed === 0) {
    return t("llmProvider.serverDefault");
  }
  return formatTokenCapacity(parsed);
}

function rowErrorText(error: ModelCapacityError, index: number): string {
  const params = { index: index + 1, ...error.params };
  switch (error.key) {
    case "modelNameRequired":
      return t("llmProvider.modelNameRequired", params);
    case "modelNameDuplicate":
      return t("llmProvider.modelNameDuplicate", params);
    case "contextWindowInvalid":
      return t("llmProvider.contextWindowInvalid", params);
    case "modelMaxOutputTokensInvalid":
      return t("llmProvider.modelMaxOutputTokensInvalid", params);
    case "modelCapacityMismatch":
      return t("llmProvider.modelCapacityMismatch", params);
  }
}

function nameError(index: number): string {
  const error = modelRowErrors.value[index];
  if (
    error?.key === "modelNameRequired" ||
    error?.key === "modelNameDuplicate"
  ) {
    return rowErrorText(error, index);
  }
  return "";
}

function capacityError(index: number): string {
  const error = modelRowErrors.value[index];
  if (
    error === undefined ||
    error.key === "modelNameRequired" ||
    error.key === "modelNameDuplicate"
  ) {
    return "";
  }
  return rowErrorText(error, index);
}

function fieldError(index: number, key: ModelCapacityError["key"]): string {
  const error = modelRowErrors.value[index];
  if (error?.key !== key) {
    return "";
  }
  return rowErrorText(error, index);
}

async function fetchModels() {
  isFetchingModels.value = true;
  fetchedError.value = null;

  try {
    let discovered: DiscoveredModelCapacity[] = [];
    if (editingProfile.value && !formData.apiKey.trim()) {
      // The stored key only authenticates the stored endpoint: fetching from a
      // changed Base URL has to carry the caller's own key.
      if (formData.baseUrl.trim() !== editingProfile.value.baseUrl.trim()) {
        fetchedError.value = t("llmProvider.apiKeyRequiredForBaseUrl");
        isFetchingModels.value = false;
        return;
      }
      const resp = await fetchModelsByProfile(editingProfile.value.name);
      discovered = discoveredModels(resp);
    } else {
      const apiKey = formData.apiKey.trim();
      if (!apiKey) {
        fetchedError.value = t("llmProvider.apiKeyRequired");
        isFetchingModels.value = false;
        return;
      }
      const resp = await fetchModelsByKey(
        formData.providerType,
        apiKey,
        formData.baseUrl.trim()
      );
      discovered = discoveredModels(resp);
    }

    if (limits.value === undefined) {
      fetchedError.value = t("llmProvider.fetchError");
      return;
    }

    const expandedByName = new Map(
      modelRows.value.map((row) => [row.name.trim(), row.expanded])
    );
    modelRows.value = mergeDiscoveredModels(
      modelRows.value,
      discovered,
      limits.value
    ).map((row) => ({
      ...row,
      expanded: expandedByName.get(row.name.trim()) ?? false,
    }));
    showSuccess("llmProvider.modelsFetched");
  } catch (e) {
    handleError(e, "llmProvider.fetchModelsError");
    fetchedError.value = formatError(e);
  } finally {
    isFetchingModels.value = false;
  }
}

/** Drop the proto wrapper shape; the page only needs the plain capacities. */
function discoveredModels(resp: {
  models: { name: string; contextWindow: number; maxOutputTokens: number }[];
}): DiscoveredModelCapacity[] {
  return resp.models.map((model) => ({
    name: model.name,
    contextWindow: model.contextWindow,
    maxOutputTokens: model.maxOutputTokens,
  }));
}

function parsedCapacity(text: string): number {
  const parsed = parseTokenCapacity(text);
  if (parsed === undefined || Number.isNaN(parsed)) {
    return 0;
  }
  return parsed;
}

async function saveProfile() {
  if (!canSave.value || isSaving.value) return;
  isSaving.value = true;

  const apiKey = formData.apiKey.trim();
  const models = modelRows.value.map((row) =>
    create(LlmProviderModelSchema, {
      name: row.name.trim(),
      enabled: row.enabled,
      contextWindow: parsedCapacity(row.contextWindow),
      maxOutputTokens: parsedCapacity(row.maxOutputTokens),
    })
  );

  const payload: LlmProviderProfile = {
    $typeName: "metaxisdata.v1.LlmProviderProfile",
    name: editingProfile.value?.name ?? "",
    title: formData.title.trim(),
    type: formData.providerType,
    baseUrl: formData.baseUrl.trim(),
    apiKey,
    models,
    maskedApiKey: "",
    createTime: undefined,
    updateTime: undefined,
  };

  try {
    if (editingProfile.value) {
      const fields = ["title", "base_url", "models"];
      if (apiKey) fields.push("api_key");
      await updateProfile(payload, fields);
      showSuccess("llmProvider.updated");
    } else {
      await createProfile(payload);
      showSuccess("llmProvider.created");
    }
    const resp = await listProfiles();
    profiles.value = resp.profiles;
    limits.value = toCapacityLimits(resp.limits);
    limitsError.value = limits.value === undefined;
    cancelForm();
  } catch (e) {
    handleError(e, "llmProvider.saveError");
  } finally {
    isSaving.value = false;
  }
}

async function deleteCurrent() {
  if (!editingProfile.value || isSaving.value) return;
  isSaving.value = true;
  try {
    await deleteProfile(editingProfile.value.name);
    showSuccess("llmProvider.deleted");
    const resp = await listProfiles();
    profiles.value = resp.profiles;
    limits.value = toCapacityLimits(resp.limits);
    limitsError.value = limits.value === undefined;
    cancelForm();
  } catch (e) {
    handleError(e, "llmProvider.deleteError");
  } finally {
    isSaving.value = false;
  }
}
</script>
