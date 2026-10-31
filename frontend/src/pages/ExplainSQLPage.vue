<template>
  <!-- Below `lg` the workbench stacks: the page itself grows inside the
       shell's single scroller, so the output pane gets width and height instead
       of the 0px column a fixed two-column grid produced on a phone. -->
  <div class="flex flex-col gap-3 lg:h-full lg:min-h-0">
    <PageHeader
      :title="t('explainSQL.title')"
      class="shrink-0"
    />

    <div class="grid grid-cols-1 overflow-hidden rounded-lg border border-border lg:min-h-0 lg:flex-1 lg:grid-cols-[24rem_minmax(0,1fr)]">
      <!-- Left panel -->
      <div class="flex flex-col gap-3 border-b border-border bg-muted/30 p-4 lg:overflow-y-auto lg:border-b-0 lg:border-r">
        <!-- Source tabs -->
        <div class="flex gap-1 border-b border-border pb-2">
          <button
            :class="[
              'flex-1 py-1.5 text-sm rounded-md transition-colors',
              sourceMode === 'metadata'
                ? 'bg-accent text-accent-foreground'
                : 'hover:bg-accent/50',
            ]"
            @click="sourceMode = 'metadata'"
          >
            {{ t("explainSQL.fromMetadata") }}
          </button>
          <button
            :class="[
              'flex-1 py-1.5 text-sm rounded-md transition-colors',
              sourceMode === 'custom'
                ? 'bg-accent text-accent-foreground'
                : 'hover:bg-accent/50',
            ]"
            @click="sourceMode = 'custom'"
          >
            {{ t("explainSQL.customSQL") }}
          </button>
        </div>

        <!-- Metadata mode -->
        <template v-if="sourceMode === 'metadata'">
          <!-- Selected metadata display -->
          <template v-if="selectedMeta">
            <div class="space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-sm font-medium">{{ t("explainSQL.selectedObject") }}</span>
                <button class="text-xs text-muted-foreground hover:text-foreground" @click="clearSelection">
                  {{ t("explainSQL.change") }}
                </button>
              </div>
              <div class="border rounded-md p-3 space-y-1.5 bg-background">
                <div class="flex items-center gap-2">
                  <span class="font-medium text-sm">{{ selectedMeta.name }}</span>
                  <Badge variant="secondary" class="text-[10px]">{{ selectedMeta.type }}</Badge>
                </div>
                <div class="text-xs text-muted-foreground font-mono break-all">{{ selectedMeta.path }}</div>
                <router-link
                  :to="metadataBrowserUrl"
                  class="mt-1 inline-flex items-center gap-1 text-xs font-medium text-primary hover:underline"
                >
                  <ExternalLink class="h-3 w-3" />
                  {{ t("explainSQL.viewInBrowser") }}
                </router-link>
              </div>
              <div class="space-y-1">
                <span class="text-xs text-muted-foreground">{{ t("explainSQL.sqlContent") }}</span>
                <div v-if="loadingSql" class="text-xs text-muted-foreground text-center py-4 border rounded-md">
                  {{ t("explainSQL.loading") }}
                </div>
                <DefinitionMonacoViewer
                  v-else-if="selectedMeta.sqlPreview"
                  :content="selectedMeta.sqlPreview"
                  language="sql"
                  :min-height="120"
                  :max-height="320"
                />
                <div v-else class="text-xs text-muted-foreground text-center py-4 border rounded-md">
                  {{ t("explainSQL.noSqlContent") }}
                </div>
              </div>
            </div>
          </template>

          <!-- Search box -->
          <template v-else>
            <div class="space-y-2">
              <Label>{{ t("explainSQL.searchMetadata") }}</Label>
          <div id="scope-dropdown" class="relative">
                <Search class="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                <input
                  v-model="metaSearch"
                  :placeholder="t('explainSQL.searchPlaceholder')"
                  type="search"
                  class="w-full bg-input-surface border border-border rounded-md py-[7px] pl-8 pr-3 text-sm outline-none focus:border-accent"
                  @input="onSearchInput"
                />
              </div>
            </div>

            <!-- Search results -->
            <div v-if="searchResults.length > 0" class="max-h-64 overflow-y-auto border rounded-md bg-background">
              <button
                v-for="item in searchResults"
                :key="item.guid"
                class="w-full text-left py-2 px-3 hover:bg-accent/30 text-sm border-b border-border last:border-0 transition-colors"
                @click="selectSearchResult(item)"
              >
                <div class="font-medium truncate">{{ item.name }}</div>
                <div class="text-xs text-muted-foreground flex items-center gap-1.5 mt-0.5">
                  <Badge variant="secondary" class="text-[10px] px-1 py-0">{{ item.type }}</Badge>
                  <span class="truncate font-mono text-[10px]">{{ item.path }}</span>
                </div>
              </button>
            </div>
            <div v-else-if="metaSearch.trim() && !metaSearching" class="text-xs text-muted-foreground text-center py-2">
              {{ t("explainSQL.noResults") }}
            </div>
          </template>
        </template>

        <!-- Custom SQL mode -->
        <template v-else>
          <!-- Scope selector -->
          <div class="relative">
            <button
              type="button"
              class="w-full bg-input-surface border border-border rounded-md py-1.5 px-3 text-xs text-left outline-none focus:border-accent flex items-center gap-1"
              @click="scopeOpen = !scopeOpen"
            >
              <span v-if="scopeDisplay" class="truncate text-foreground">{{ scopeDisplay }}</span>
              <span v-else class="text-muted-foreground">{{ t("explainSQL.selectScope") }}</span>
              <ChevronDown class="h-3 w-3 text-muted-foreground shrink-0 ml-auto" />
            </button>
            <div
              v-if="scopeOpen"
              class="absolute z-10 left-0 right-0 top-full mt-1 bg-background border border-border rounded-md shadow-md overflow-hidden"
            >
              <div
                v-if="scopeStep !== 'instance'"
                class="flex items-center gap-2 border-b border-border px-2 py-1.5"
              >
                <button
                  class="text-muted-foreground hover:text-foreground shrink-0"
                  :aria-label="t('common.back')"
                  @click="scopeStep = scopeStep === 'schema' ? 'database' : 'instance'"
                >
                  <ArrowLeft class="h-3.5 w-3.5" />
                </button>
                <span class="text-xs text-muted-foreground truncate">{{ scopeStepLabel }}</span>
              </div>
              <div class="max-h-48 overflow-y-auto">
                <button
                  v-for="item in scopeStepItems"
                  :key="item.value"
                  class="w-full text-left px-3 py-1.5 text-xs hover:bg-accent/30 transition-colors"
                  @click="scopeSelectItem(item.value)"
                >
                  {{ item.label }}
                </button>
                <div
                  v-if="scopeStepItems.length === 0 && !scopeStepLoading"
                  class="px-3 py-2 text-xs text-muted-foreground"
                >
                  {{ scopeStepEmpty }}
                </div>
              </div>
            </div>
          </div>
          <div class="h-72 overflow-hidden rounded-md border lg:h-auto lg:min-h-0 lg:flex-1">
            <MonacoEditor
              v-model:content="customSQL"
              language="sql"
              :options="{ fontSize: 13, minimap: { enabled: false } }"
            />
          </div>
        </template>

        <div class="flex items-center gap-3">
          <Select
            v-if="providerOptions.length > 1"
            v-model="selectedProvider"
          >
            <SelectTrigger class="w-56">
              <SelectValue :placeholder="t('explainSQL.provider')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="option in providerOptions"
                :key="option.name"
                :value="option.name"
              >
                {{ option.title }}
              </SelectItem>
            </SelectContent>
          </Select>
          <Button
            :disabled="!canExplain || isExplaining"
            @click="startExplain(resultFromCache)"
          >
            <Sparkles class="h-4 w-4 mr-2" />
            {{ isExplaining ? t("explainSQL.explaining") : resultFromCache ? t("explainSQL.regenerate") : t("explainSQL.explain") }}
          </Button>
        </div>
      </div>

      <!-- Right panel -->
      <div class="flex min-h-[18rem] flex-col gap-4 p-5 lg:min-h-0 lg:overflow-y-auto">
        <EmptyState
          v-if="!isExplaining && !resultText && !explainError"
          :icon="Sparkles"
          :title="t('explainSQL.emptyResultTitle')"
          :description="t('explainSQL.emptyResultDescription')"
          class="flex-1"
        />

        <template v-else>
          <div
            v-if="resultFromCache && explainMeta?.cacheCreatedAt"
            class="shrink-0 text-xs text-muted-foreground bg-muted/40 rounded-md px-3 py-2 flex items-center gap-2"
          >
            <Clock class="h-3.5 w-3.5 shrink-0" />
            {{ t("explainSQL.cacheHint", { time: formatCacheTime(explainMeta.cacheCreatedAt) }) }}
          </div>
          <div class="flex-1">
            <details
              v-if="progressSteps.length > 0"
              :open="showProgress"
              class="mb-3"
            >
              <summary class="text-sm text-muted-foreground cursor-pointer py-1 select-none">
                <Loader2 v-if="showProgress" class="h-3 w-3 animate-spin inline mr-1.5" />
                {{ showProgress ? t("explainSQL.thinking") : t("explainSQL.thinkingProcess") }}
              </summary>
              <div class="mt-2 ml-2 border-l-2 border-muted/60 pl-3 space-y-1">
                <div
                  v-for="(step, i) in progressSteps"
                  :key="i"
                  class="text-xs flex items-start gap-1.5"
                >
                  <template v-if="step.type === 'tool_start'">
                    <span class="font-mono text-primary">{{ step.toolName }}</span>
                    <span v-if="step.toolInput" class="text-muted-foreground">
                      ({{ formatToolArgsShort(step.toolInput) }})
                    </span>
                  </template>
                  <template v-else-if="step.type === 'tool_end'">
                    <template v-if="step.toolError">
                      <span class="text-danger">&#10007; {{ step.toolName }}: {{ step.toolError }}</span>
                    </template>
                    <template v-else>
                      <span class="text-success">&#10003; {{ step.toolName }}</span>
                      <span v-if="step.toolOutput" class="text-muted-foreground truncate max-w-md">
                        &mdash; {{ formatToolOutputShort(step.toolOutput) }}
                      </span>
                    </template>
                  </template>
                </div>
              </div>
            </details>
            <div
              v-else-if="isExplaining && !resultText"
              class="flex items-center gap-2 text-sm text-muted-foreground py-1 mb-3"
            >
              <Loader2 class="h-3 w-3 animate-spin" />
              {{ t("explainSQL.thinking") }}
            </div>

            <MarkdownRender
              v-if="resultText"
              mode="chat"
              :content="resultText"
              :final="!isExplaining"
              :max-live-nodes="0"
              :fade="false"
              :typewriter="true"
            />
            <div v-if="explainError" class="text-destructive text-sm p-4 border border-destructive/30 rounded-md">
              {{ explainError }}
            </div>
          </div>

          <div v-if="explainMeta" class="flex items-center gap-3 text-xs text-muted-foreground pt-3 border-t border-border">
            <span>{{ explainMeta.provider }} / {{ explainMeta.model }}</span>
            <Badge v-if="explainMeta.fromCache" variant="outline">{{ t("explainSQL.cached") }}</Badge>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import {
  ArrowLeft,
  ChevronDown,
  Clock,
  ExternalLink,
  Loader2,
  Search,
  Sparkles,
} from "lucide-vue-next";
import MarkdownRender from "markstream-vue";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { type RouteLocationRaw, useRoute } from "vue-router";
import { getSchemaString, listMetadata, searchMetadata } from "@/api/database";
import { explainSQL } from "@/api/explain";
import { listProfiles } from "@/api/llm";
import { getWorkspaceProfileSetting } from "@/api/setting";
import EmptyState from "@/components/common/EmptyState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import DefinitionMonacoViewer from "@/components/metadata/DefinitionMonacoViewer.vue";
import MonacoEditor from "@/components/monaco-editor/MonacoEditor.vue";
import Badge from "@/components/ui/badge/Badge.vue";
import Button from "@/components/ui/button/Button.vue";
import Label from "@/components/ui/label/Label.vue";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useDebounced } from "@/composables/useTimers";
import { useAuthStore } from "@/store/modules/auth";
import { useInstanceStore } from "@/store/modules/instance";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { ExplainSQLProgress } from "@/types/proto-es/v1/explain_sql_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { isAborted } from "@/utils/error";
import { guidToRouteParams, routeParamToGuid } from "@/utils/guid";
import { isProviderAllowed } from "@/utils/llmProvider";
import { metaTypeLabel, parseMetaType } from "@/utils/metaType";

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const route = useRoute();
const authStore = useAuthStore();
const instanceStore = useInstanceStore();

// ---- types ----
interface SearchItem {
  guid: string;
  name: string;
  type: string;
  path: string;
  metaType: MetaType;
}

interface SelectedMeta {
  guid: string;
  name: string;
  type: string;
  path: string;
  sqlPreview: string;
  metaType: MetaType;
}

// ---- state ----
const sourceMode = ref<"metadata" | "custom">("metadata");
const customSQL = ref("");

// Scope for custom SQL
interface ScopeInstance {
  id: string;
  title: string;
}
interface ScopeItem {
  value: string;
  label: string;
}
const scopeInstances = ref<ScopeInstance[]>([]);
const scopeDatabases = ref<string[]>([]);
const scopeSchemas = ref<string[]>([]);
const scopeInstance = ref("");
const scopeDatabase = ref("");
const scopeSchema = ref("");
const scopeOpen = ref(false);
const scopeStep = ref<"instance" | "database" | "schema">("instance");
const scopeStepLoading = ref(false);

const scopeDisplay = computed(() => {
  const parts: string[] = [];
  const inst = scopeInstances.value.find((i) => i.id === scopeInstance.value);
  if (inst) parts.push(inst.title);
  if (scopeDatabase.value) {
    parts.push(scopeDatabase.value);
    if (scopeSchema.value) parts.push(scopeSchema.value);
  }
  return parts.length > 0 ? parts.join(" \u203A ") : "";
});

const scopeStepLabel = computed(() => {
  if (scopeStep.value === "database") return t("explainSQL.selectDatabase");
  return t("explainSQL.selectSchema");
});

const scopeStepItems = computed<ScopeItem[]>(() => {
  if (scopeStep.value === "instance") {
    return scopeInstances.value.map((i) => ({ value: i.id, label: i.title }));
  }
  if (scopeStep.value === "database") {
    return scopeDatabases.value.map((db) => ({ value: db, label: db }));
  }
  return scopeSchemas.value.map((sch) => ({ value: sch, label: sch }));
});

const scopeStepEmpty = computed(() => {
  if (scopeStep.value === "database") return t("explainSQL.noDatabases");
  return t("explainSQL.noSchemas");
});

const scopePrefix = computed(() => {
  if (!scopeInstance.value) return "";
  const parts = [scopeInstance.value];
  if (scopeDatabase.value) {
    parts.push(scopeDatabase.value);
    if (scopeSchema.value) parts.push(scopeSchema.value);
  }
  return parts.join(";");
});

watch(scopeInstance, async (val) => {
  scopeDatabase.value = "";
  scopeSchema.value = "";
  scopeDatabases.value = [];
  scopeSchemas.value = [];
  if (!val) {
    scopeStep.value = "instance";
    return;
  }
  scopeStepLoading.value = true;
  try {
    const resp = await listMetadata({ parentGuid: val, pageSize: 100 });
    const dbGroup = resp.typesStoredMetadata.find(
      (g) => g.metaType === MetaType.DATABASE
    );
    scopeDatabases.value = dbGroup
      ? dbGroup.list.map((item) => item.type.value?.name ?? "").filter(Boolean)
      : [];
    scopeStep.value = "database";
  } catch {
    scopeDatabases.value = [];
  } finally {
    scopeStepLoading.value = false;
  }
});

watch(scopeDatabase, async (val) => {
  scopeSchema.value = "";
  scopeSchemas.value = [];
  if (!val) return;
  scopeStepLoading.value = true;
  try {
    const parentGuid = `${scopeInstance.value};${val}`;
    const resp = await listMetadata({ parentGuid, pageSize: 100 });
    const schemaGroup = resp.typesStoredMetadata.find(
      (g) => g.metaType === MetaType.SCHEMA
    );
    scopeSchemas.value = schemaGroup
      ? schemaGroup.list
          .map((item) => item.type.value?.name ?? "")
          .filter(Boolean)
      : [];
    if (scopeSchemas.value.length === 0) {
      scopeOpen.value = false;
    } else {
      scopeStep.value = "schema";
    }
  } catch {
    scopeSchemas.value = [];
  } finally {
    scopeStepLoading.value = false;
  }
});

watch(scopeSchema, (val) => {
  if (val) {
    scopeOpen.value = false;
  }
});

function scopeSelectItem(value: string) {
  if (scopeStep.value === "instance") {
    scopeInstance.value = value;
  } else if (scopeStep.value === "database") {
    scopeDatabase.value = value;
  } else {
    scopeSchema.value = value;
  }
}

function handleClickOutside(e: MouseEvent) {
  const el = document.getElementById("scope-dropdown");
  if (el && !el.contains(e.target as Node)) {
    scopeOpen.value = false;
  }
}

// Metadata search
const metaSearch = ref("");
const metaSearching = ref(false);
const searchResults = ref<SearchItem[]>([]);

// Selected metadata
const selectedMeta = ref<SelectedMeta | null>(null);
const loadingSql = ref(false);

// Explanation
const isExplaining = ref(false);
const resultText = ref("");
const explainError = ref<string | null>(null);
/**
 * Identifies the newest explanation stream. The page is a route, but a reader
 * who navigates away must stop both the cost and any late frame; a superseded
 * stream may not write the document the next one owns.
 */
let explainGeneration = 0;
let explainStream: AbortController | undefined;

// ExplainSQL may be restricted to an admin-configured set of provider profiles;
// the picker lists the allowed profiles that have an enabled model.
interface ProviderOption {
  name: string;
  title: string;
}
const providerOptions = ref<ProviderOption[]>([]);
const selectedProvider = ref("");
const explainMeta = ref<{
  provider: string;
  model: string;
  fromCache: boolean;
  cacheCreatedAt: string;
} | null>(null);

// Progress tracking
const progressSteps = ref<ExplainSQLProgress[]>([]);
const showProgress = ref(true);

// ---- computed ----
const canExplain = computed(() => {
  if (sourceMode.value === "metadata") return !!selectedMeta.value;
  return customSQL.value.trim() !== "";
});

const resultFromCache = computed(() => !!explainMeta.value?.fromCache);

const metadataBrowserUrl = computed<RouteLocationRaw>(() => {
  const m = selectedMeta.value;
  // The link only renders with a selection; the browser root is the safe target
  // for the unreachable branch.
  if (!m) return { name: "MetadataBrowser" };
  return {
    name: "MetadataDetail",
    params: { guid: guidToRouteParams(m.guid) },
    query: { metaType: String(m.metaType) },
  };
});

// ---- lifecycle ----
onMounted(async () => {
  loadScopeInstances();
  loadProviders();
  document.addEventListener("mousedown", handleClickOutside);
  const guidFromRoute = routeParamToGuid(route.params.guid);
  if (guidFromRoute) {
    await loadMetaByGuid(
      guidFromRoute,
      parseMetaType(route.query.metaType) ?? MetaType.UNSPECIFIED
    );
  }
});

onUnmounted(() => {
  document.removeEventListener("mousedown", handleClickOutside);
  explainGeneration += 1;
  explainStream?.abort();
  explainStream = undefined;
});

// ---- methods ----
async function loadProviders() {
  try {
    const [setting, resp] = await Promise.all([
      getWorkspaceProfileSetting(),
      listProfiles({ pageSize: 100 }),
    ]);
    const allowed = setting.allowedLlmProviderProfiles;
    providerOptions.value = resp.profiles
      .filter((profile) => profile.models.some((model) => model.enabled))
      .filter((profile) => isProviderAllowed(profile.name, allowed))
      .map((profile) => ({
        name: profile.name,
        title: profile.title || profile.name,
      }));
    if (
      providerOptions.value.length > 0 &&
      !providerOptions.value.some((o) => o.name === selectedProvider.value)
    ) {
      selectedProvider.value = providerOptions.value[0].name;
    }
  } catch {
    // A failed lookup leaves the server-side default provider in place.
    providerOptions.value = [];
  }
}

async function loadMetaByGuid(guid: string, metaType: MetaType) {
  const parts = guid.split(";");
  const name = parts[parts.length - 1] ?? guid;
  const path = parts.join(" / ");

  selectedMeta.value = {
    guid,
    name,
    type: metaTypeLabel(metaType, t),
    path,
    sqlPreview: "",
    metaType,
  };

  // Fetch DDL asynchronously.
  await fetchMetaSQL(guid, metaType);
}

async function fetchMetaSQL(guid: string, metaType: MetaType) {
  loadingSql.value = true;
  try {
    const schemaResp = await getSchemaString({ guid, metaType });
    const sql = schemaResp.schema ?? "";
    if (sql && selectedMeta.value && selectedMeta.value.guid === guid) {
      selectedMeta.value = { ...selectedMeta.value, sqlPreview: sql };
    }
  } catch {
    // Silently ignore — show "no SQL content" state.
  } finally {
    loadingSql.value = false;
  }
}

function clearSelection() {
  selectedMeta.value = null;
  metaSearch.value = "";
  searchResults.value = [];
}

/** The box emits per keystroke; one request answers a pause in typing. */
const debouncedSearch = useDebounced((q: string) => void doSearch(q), 300);

function onSearchInput() {
  debouncedSearch.cancel();
  const q = metaSearch.value.trim();
  if (!q) {
    searchResults.value = [];
    return;
  }
  void debouncedSearch(q);
}

async function doSearch(q: string) {
  metaSearching.value = true;
  try {
    const resp = await searchMetadata({ searchStr: q });
    const items: SearchItem[] = [];
    for (const r of resp.results) {
      const guid = r.guid ?? "";
      const parts = guid.split(";");
      items.push({
        guid,
        // SearchMetadataResult carries no name; the last GUID segment is the
        // object's own name.
        name: parts[parts.length - 1] ?? guid,
        type: metaTypeLabel(r.metaType, t),
        path: parts.join(" / "),
        metaType: r.metaType,
      });
    }
    searchResults.value = items;
  } catch {
    searchResults.value = [];
  } finally {
    metaSearching.value = false;
  }
}

function formatToolArgsShort(jsonArgs: string): string {
  try {
    const obj = JSON.parse(jsonArgs);
    return Object.values(obj)
      .map((v) => String(v))
      .join(", ");
  } catch {
    return "";
  }
}

function formatToolOutputShort(output: string): string {
  const maxLen = 120;
  if (output.length <= maxLen) return output;
  return output.slice(0, maxLen) + "...";
}

function formatCacheTime(iso: string): string {
  return formatDateTime(iso, locale.value, { seconds: true, fallback: iso });
}

async function loadScopeInstances() {
  try {
    await instanceStore.ensureLoaded();
    scopeInstances.value = instanceStore.active.map((instance) => ({
      id: instance.name.replace("instances/", ""),
      title: instance.title || instance.name.replace("instances/", ""),
    }));
  } catch {
    scopeInstances.value = [];
  }
}

async function selectSearchResult(item: SearchItem) {
  metaSearch.value = "";
  searchResults.value = [];

  selectedMeta.value = {
    guid: item.guid,
    name: item.name,
    type: item.type,
    path: item.path,
    sqlPreview: "",
    metaType: item.metaType,
  };

  await fetchMetaSQL(item.guid, item.metaType);
}

async function startExplain(forceRegen = false) {
  if (isExplaining.value) return;
  const mine = ++explainGeneration;
  explainStream?.abort();
  const controller = new AbortController();
  explainStream = controller;
  isExplaining.value = true;
  resultText.value = "";
  explainError.value = null;
  explainMeta.value = null;
  progressSteps.value = [];
  showProgress.value = true;

  // The server writes the answer in the language stored on the profile, so a
  // switch that is still in flight — or one whose write failed — is settled
  // first: explaining right after switching must not answer in the old language.
  await authStore.ensureLanguagePersisted();

  const input =
    sourceMode.value === "metadata" && selectedMeta.value
      ? {
          metaGuid: selectedMeta.value.guid,
          forceRegenerate: forceRegen,
          providerName: selectedProvider.value,
        }
      : {
          sqlText: customSQL.value.trim(),
          forceRegenerate: forceRegen,
          scopePrefix: scopePrefix.value,
          providerName: selectedProvider.value,
        };

  try {
    const stream = explainSQL(input, { signal: controller.signal });
    for await (const chunk of stream) {
      if (mine !== explainGeneration) {
        return;
      }
      if (chunk.payload?.case === "content" && chunk.payload.value) {
        showProgress.value = false;
        resultText.value += chunk.payload.value;
      } else if (chunk.payload?.case === "progress" && chunk.payload.value) {
        const p = chunk.payload.value;
        if (p.type === "tool_start" || p.type === "tool_end") {
          progressSteps.value.push(p);
        }
      } else if (chunk.payload?.case === "metadata" && chunk.payload.value) {
        const m = chunk.payload.value;
        explainMeta.value = {
          provider: m.provider,
          model: m.model,
          fromCache: m.fromCache,
          cacheCreatedAt: m.cacheCreatedAt,
        };
      }
    }
  } catch (e) {
    if (mine !== explainGeneration || isAborted(e)) {
      return;
    }
    explainError.value = formatError(e);
  } finally {
    if (mine === explainGeneration) {
      isExplaining.value = false;
      explainStream = undefined;
    }
  }
}
</script>
