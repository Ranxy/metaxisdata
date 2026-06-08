<template>
  <div class="flex flex-col gap-3 h-full min-h-0">
    <!-- Top bar -->
    <div class="flex items-center gap-2 shrink-0">
      <div class="flex-1">
        <AppInput
          v-model="searchQuery"
          :placeholder="t('llmProvider.searchPlaceholder')"
        >
          <template #suffix>
            <Search class="absolute right-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          </template>
        </AppInput>
      </div>
      <Button @click="startCreate">
        <Plus class="h-4 w-4 mr-2" />
        {{ t("llmProvider.addProfile") }}
      </Button>
    </div>

    <!-- Two-panel layout -->
    <div class="grid grid-cols-[260px_minmax(0,1fr)] min-h-0 flex-1 border border-border rounded-lg overflow-hidden">
      <!-- Left panel: profile list -->
      <div class="border-r border-border p-2 overflow-y-auto bg-muted/30">
        <div v-if="filteredProfiles.length === 0" class="p-4 text-center text-muted-foreground text-sm">
          <Database class="h-8 w-8 mx-auto mb-2 opacity-30" />
          <p>{{ searchQuery ? t("llmProvider.noMatch") : t("llmProvider.empty") }}</p>
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
          class="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-2"
        >
          <Sparkles class="h-12 w-12 opacity-30" />
          <p class="text-sm">{{ t("llmProvider.selectHint") }}</p>
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
            <select
              v-model="formData.providerType"
              :disabled="!!editingProfile"
              class="w-full bg-input-surface border border-border rounded-lg py-[7px] px-3 text-sm text-text outline-none focus:border-accent disabled:opacity-60"
            >
              <option
                v-for="d in BUILTIN_DEFS"
                :key="d.id"
                :value="d.enum"
              >
                {{ d.label }}
              </option>
            </select>
          </div>

          <!-- Title -->
          <div class="space-y-2">
            <Label>{{ t("llmProvider.profileTitle") }}</Label>
            <AppInput
              v-model="formData.title"
              :placeholder="autoTitlePreview"
            />
          </div>

          <!-- Base URL -->
          <div class="space-y-2">
            <Label>{{ t("llmProvider.baseUrl") }}</Label>
            <AppInput
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
            <AppInput
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
                <span v-if="availableModels.length > 0" class="text-muted-foreground font-normal">
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

            <div v-if="fetchedError" class="text-destructive text-sm">{{ fetchedError }}</div>

            <div v-if="availableModels.length > 0" class="space-y-2">
              <div class="relative">
                <Search class="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
                <input
                  v-model="modelSearch"
                  :placeholder="t('llmProvider.filterModels')"
                  type="search"
                  class="w-full bg-input-surface border border-border rounded-md py-[5px] pl-8 pr-3 text-sm outline-none focus:border-accent"
                />
              </div>
              <div class="max-h-52 overflow-y-auto border rounded-md p-2 space-y-0.5">
                <div
                  v-for="model in filteredModels"
                  :key="model"
                  class="flex items-center justify-between py-1.5 px-2 rounded hover:bg-accent/30"
                >
                  <span class="text-sm">{{ model }}</span>
                  <Checkbox
                    :checked="isModelEnabled(model)"
                    @update:checked="(checked: boolean) => toggleModel(model, checked)"
                  />
                </div>
                <div
                  v-if="filteredModels.length === 0"
                  class="text-muted-foreground text-xs text-center py-2"
                >
                  {{ t("llmProvider.noModelMatch") }}
                </div>
              </div>
            </div>

            <div
              v-else-if="!isFetchingModels"
              class="text-muted-foreground text-sm text-center py-4 border rounded-md"
            >
              {{ t("llmProvider.fetchHint") }}
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
import { Database, Plus, RefreshCw, Search, Sparkles } from "lucide-vue-next";
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  createProfile,
  deleteProfile,
  fetchModelsByKey,
  fetchModelsByProfile,
  listProfiles,
  updateProfile,
} from "@/api/llm";
import AppInput from "@/components/common/AppInput.vue";
import Badge from "@/components/ui/badge/Badge.vue";
import Button from "@/components/ui/button/Button.vue";
import Checkbox from "@/components/ui/checkbox/Checkbox.vue";
import Label from "@/components/ui/label/Label.vue";
import Separator from "@/components/ui/separator/Separator.vue";
import { useErrorHandler } from "@/composables/useErrorHandler";
import {
  LLMProviderType,
  LlmProviderModelSchema,
  type LlmProviderProfile,
} from "@/types/proto-es/v1/llm_service_pb";

// ---- static builtin catalog (mirrors backend builtinDefinitions) ----
interface BuiltinDef {
  id: string;
  label: string;
  description: string;
  defaultBaseUrl: string;
  enum: LLMProviderType;
}

const BUILTIN_DEFS: BuiltinDef[] = [
  {
    id: "openai",
    label: "OpenAI",
    description: "OpenAI GPT models.",
    defaultBaseUrl: "https://api.openai.com",
    enum: LLMProviderType.LLM_PROVIDER_TYPE_OPENAI,
  },
  {
    id: "deepseek",
    label: "DeepSeek",
    description: "DeepSeek AI models.",
    defaultBaseUrl: "https://api.deepseek.com",
    enum: LLMProviderType.LLM_PROVIDER_TYPE_DEEPSEEK,
  },
  {
    id: "openrouter",
    label: "OpenRouter",
    description: "OpenRouter aggregates hundreds of models.",
    defaultBaseUrl: "https://openrouter.ai/api",
    enum: LLMProviderType.LLM_PROVIDER_TYPE_OPENROUTER,
  },
  {
    id: "custom",
    label: "Custom",
    description: "OpenAI-compatible endpoint.",
    defaultBaseUrl: "",
    enum: LLMProviderType.LLM_PROVIDER_TYPE_CUSTOM,
  },
];

function getBuiltinByEnum(e: LLMProviderType): BuiltinDef | undefined {
  return BUILTIN_DEFS.find((d) => d.enum === e);
}

const { t } = useI18n();
const { handleError, showSuccess } = useErrorHandler();

// ---- state ----
const profiles = ref<LlmProviderProfile[]>([]);
const isCreating = ref(false);

// Left panel
const searchQuery = ref("");
const editingProfile = ref<LlmProviderProfile | null>(null);

// Right panel form
const formData = reactive({
  providerType: LLMProviderType.LLM_PROVIDER_TYPE_OPENAI,
  title: "",
  baseUrl: "",
  apiKey: "",
  enabledModels: [] as string[],
});

// Keep base_url in sync with provider type selection.
watch(
  () => formData.providerType,
  (t) => {
    const def = getBuiltinByEnum(t);
    formData.baseUrl = def?.defaultBaseUrl ?? "";
  },
);

const availableModels = ref<string[]>([]);
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
      (getBuiltinByEnum(p.type)?.label ?? "").toLowerCase().includes(q) ||
      p.models.some((m) => m.name.toLowerCase().includes(q))
  );
});

const enabledModelCount = computed(() => formData.enabledModels.length);

const filteredModels = computed(() => {
  const q = modelSearch.value.trim().toLowerCase();
  if (!q) return availableModels.value;
  return availableModels.value.filter((m) => m.toLowerCase().includes(q));
});

const canSave = computed(
  () => formData.providerType !== LLMProviderType.LLM_PROVIDER_TYPE_UNSPECIFIED
);

const autoTitlePreview = computed(() => {
  const def = getBuiltinByEnum(formData.providerType);
  const label = def?.label ?? "";
  const models = formData.enabledModels;
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
async function loadData() {
  try {
    const resp = await listProfiles();
    profiles.value = resp.profiles;
  } catch (e) {
    handleError(e, "llmProvider.fetchError");
  }
}

function profileDisplayTitle(p: LlmProviderProfile): string {
  return p.title || autoTitle(p);
}

function autoTitle(p: LlmProviderProfile): string {
  const def = getBuiltinByEnum(p.type);
  const label = def?.label ?? "";
  const models = p.models.filter((m) => m.enabled).map((m) => m.name);
  if (models.length === 0) return label;
  return `${label} — ${models.slice(0, 2).join(", ")}${models.length > 2 ? ` +${models.length - 2}` : ""}`;
}

function providerLabel(t: LLMProviderType): string {
  return getBuiltinByEnum(t)?.label ?? "Unknown";
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
  formData.enabledModels = p.models.filter((m) => m.enabled).map((m) => m.name);
  availableModels.value = p.models.map((m) => m.name);
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
  formData.enabledModels = [];
  availableModels.value = [];
  modelSearch.value = "";
  fetchedError.value = null;
}

function cancelForm() {
  isCreating.value = false;
  editingProfile.value = null;
  resetForm();
}

function isModelEnabled(modelId: string): boolean {
  return formData.enabledModels.includes(modelId);
}

function toggleModel(modelId: string, checked: boolean) {
  if (checked) {
    if (!formData.enabledModels.includes(modelId)) {
      formData.enabledModels.push(modelId);
    }
  } else {
    formData.enabledModels = formData.enabledModels.filter(
      (m) => m !== modelId
    );
  }
}

async function fetchModels() {
  isFetchingModels.value = true;
  fetchedError.value = null;

  try {
    let modelIds: string[];
    if (editingProfile.value && !formData.apiKey.trim()) {
      const resp = await fetchModelsByProfile(editingProfile.value.name);
      modelIds = resp.modelIds;
    } else {
      const apiKey = formData.apiKey.trim();
      if (!apiKey) {
        fetchedError.value = t("llmProvider.apiKeyRequired");
        isFetchingModels.value = false;
        return;
      }
      const resp = await fetchModelsByKey(formData.providerType, apiKey);
      modelIds = resp.modelIds;
    }

    availableModels.value = modelIds;
    formData.enabledModels = formData.enabledModels.filter((m) =>
      modelIds.includes(m)
    );
    showSuccess("llmProvider.modelsFetched");
  } catch (e) {
    fetchedError.value =
      handleError(e, "llmProvider.fetchModelsError") ?? String(e);
  } finally {
    isFetchingModels.value = false;
  }
}

async function saveProfile() {
  if (!canSave.value || isSaving.value) return;
  isSaving.value = true;

  const models = availableModels.value.map((name) =>
    create(LlmProviderModelSchema, {
      name,
      enabled: formData.enabledModels.includes(name),
    })
  );

  const payload: LlmProviderProfile = {
    $typeName: "metaxisdata.v1.LlmProviderProfile",
    name: editingProfile.value?.name ?? "",
    title: formData.title.trim(),
    type: formData.providerType,
    baseUrl: formData.baseUrl.trim(),
    apiKey: formData.apiKey,
    models,
    maskedApiKey: "",
    createTime: undefined,
    updateTime: undefined,
  };

  try {
    if (editingProfile.value) {
      const fields = ["title", "base_url", "models"];
      if (formData.apiKey) fields.push("api_key");
      await updateProfile(payload, fields);
      showSuccess("llmProvider.updated");
    } else {
      await createProfile(payload);
      showSuccess("llmProvider.created");
    }
    const resp = await listProfiles();
    profiles.value = resp.profiles;
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
    cancelForm();
  } catch (e) {
    handleError(e, "llmProvider.deleteError");
  } finally {
    isSaving.value = false;
  }
}
</script>
