<template>
  <div class="flex flex-col gap-3 h-full min-h-0">
    <div class="flex items-center gap-2 shrink-0">
      <h1 class="text-2xl font-bold tracking-tight">{{ t("explainSQL.title") }}</h1>
    </div>

    <div class="grid grid-cols-[380px_minmax(0,1fr)] min-h-0 flex-1 border border-border rounded-lg overflow-hidden">
      <!-- Left panel -->
      <div class="border-r border-border p-4 overflow-y-auto flex flex-col gap-3 bg-muted/30">
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
                  class="inline-flex items-center gap-1 text-xs text-blue-600 dark:text-blue-400 font-medium hover:underline mt-1"
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
              <div class="relative">
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
          <div class="flex-1 min-h-0 border rounded-md overflow-hidden">
            <MonacoEditor
              v-model:content="customSQL"
              language="sql"
              :options="{ fontSize: 13, minimap: { enabled: false } }"
            />
          </div>
        </template>

        <Button
          :disabled="!canExplain || isExplaining"
          class="mt-auto"
          @click="startExplain()"
        >
          <Sparkles class="h-4 w-4 mr-2" />
          {{ isExplaining ? t("explainSQL.explaining") : t("explainSQL.explain") }}
        </Button>
      </div>

      <!-- Right panel -->
      <div class="p-5 overflow-y-auto flex flex-col gap-4">
        <div
          v-if="!isExplaining && !resultText && !explainError"
          class="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-2"
        >
          <Sparkles class="h-12 w-12 opacity-30" />
          <p class="text-sm">{{ t("explainSQL.idleHint") }}</p>
        </div>

        <template v-else>
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
                    <span class="text-blue-500 font-mono">{{ step.toolName }}</span>
                    <span v-if="step.toolInput" class="text-muted-foreground">
                      ({{ formatToolArgsShort(step.toolInput) }})
                    </span>
                  </template>
                  <template v-else-if="step.type === 'tool_end'">
                    <template v-if="step.toolError">
                      <span class="text-red-500">&#10007; {{ step.toolName }}: {{ step.toolError }}</span>
                    </template>
                    <template v-else>
                      <span class="text-green-500">&#10003; {{ step.toolName }}</span>
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
            <Badge v-if="explainMeta.expired" variant="secondary" class="text-destructive">{{ t("explainSQL.expired") }}</Badge>
            <Button
              v-if="explainMeta.expired"
              variant="outline"
              size="sm"
              @click="startExplain(true)"
            >
              {{ t("explainSQL.regenerate") }}
            </Button>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ExternalLink, Loader2, Search, Sparkles } from "lucide-vue-next";
import MarkdownRender from "markstream-vue";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { getSchemaString, searchMetadata } from "@/api/database";
import { explainSQL } from "@/api/explain";
import DefinitionMonacoViewer from "@/components/metadata/DefinitionMonacoViewer.vue";
import MonacoEditor from "@/components/monaco-editor/MonacoEditor.vue";
import Badge from "@/components/ui/badge/Badge.vue";
import Button from "@/components/ui/button/Button.vue";
import Label from "@/components/ui/label/Label.vue";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { ExplainSQLProgress } from "@/types/proto-es/v1/explain_sql_service_pb";

const { t } = useI18n();
const route = useRoute();

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
const sourceMode = ref<"metadata" | "custom">(
  route.params.guid ? "metadata" : "metadata"
);
const customSQL = ref("");

// Metadata search
const metaSearch = ref("");
const metaSearching = ref(false);
const searchResults = ref<SearchItem[]>([]);
let searchTimer: ReturnType<typeof setTimeout> | null = null;

// Selected metadata
const selectedMeta = ref<SelectedMeta | null>(null);
const loadingSql = ref(false);

// Explanation
const isExplaining = ref(false);
const resultText = ref("");
const explainError = ref<string | null>(null);
const explainMeta = ref<{
  provider: string;
  model: string;
  fromCache: boolean;
  expired: boolean;
} | null>(null);

// Progress tracking
const progressSteps = ref<ExplainSQLProgress[]>([]);
const showProgress = ref(true);

// ---- computed ----
const canExplain = computed(() => {
  if (sourceMode.value === "metadata") return !!selectedMeta.value;
  return customSQL.value.trim() !== "";
});

const metadataBrowserUrl = computed(() => {
  const m = selectedMeta.value;
  if (!m) return "";
  const parts = m.guid.split(";").map((p) => p || "~");
  return `/metadata/${parts.join("/")}?metaType=${m.metaType}`;
});

// ---- lifecycle ----
onMounted(async () => {
  const guidFromRoute = getGuidFromRoute();
  if (guidFromRoute) {
    const metaTypeFromQuery = Number(route.query.metaType) || 0;
    await loadMetaByGuid(guidFromRoute, metaTypeFromQuery as MetaType);
  }
});

// ---- methods ----
function getGuidFromRoute(): string {
  const g = route.params.guid;
  if (Array.isArray(g)) return g.join("/");
  return (g as string) ?? "";
}

async function loadMetaByGuid(guid: string, metaType: MetaType) {
  const parts = guid.split(";");
  const name = parts[parts.length - 1] ?? guid;
  const path = parts.join(" / ");

  selectedMeta.value = {
    guid,
    name,
    type: metaTypeLabel(metaType),
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

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer);
  const q = metaSearch.value.trim();
  if (!q) {
    searchResults.value = [];
    return;
  }
  searchTimer = setTimeout(() => doSearch(q), 300);
}

async function doSearch(q: string) {
  metaSearching.value = true;
  try {
    const resp = await searchMetadata({ searchStr: q });
    const items: SearchItem[] = [];
    for (const r of (resp as any).results ?? []) {
      const guid = r.guid ?? "";
      const parts = guid.split(";");
      items.push({
        guid,
        name: r.name ?? parts[parts.length - 1] ?? guid,
        type: metaTypeLabel(r.metaType as MetaType),
        path: parts.join(" / "),
        metaType: r.metaType as MetaType,
      });
    }
    searchResults.value = items;
  } catch {
    searchResults.value = [];
  } finally {
    metaSearching.value = false;
  }
}

function metaTypeLabel(t: MetaType): string {
  const labels: Record<number, string> = {
    0: "UNSPECIFIED",
    1: "INSTANCE",
    2: "DATABASE",
    3: "SCHEMA",
    4: "TABLE",
    5: "VIEW",
    6: "MATERIALIZED_VIEW",
    7: "COLUMN",
    10: "PROCEDURE",
    11: "FUNCTION",
    18: "MANUAL_SQL",
  };
  return labels[t as number] ?? `TYPE_${t}`;
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
  isExplaining.value = true;
  resultText.value = "";
  explainError.value = null;
  explainMeta.value = null;
  progressSteps.value = [];
  showProgress.value = true;

  const input =
    sourceMode.value === "metadata" && selectedMeta.value
      ? { metaGuid: selectedMeta.value.guid, forceRegenerate: forceRegen }
      : { sqlText: customSQL.value.trim(), forceRegenerate: forceRegen };

  try {
    const stream = explainSQL(input);
    for await (const chunk of stream) {
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
          expired: m.expired,
        };
      } else if (chunk.payload?.case === "error" && chunk.payload.value) {
        explainError.value = chunk.payload.value;
      }
    }
  } catch (e) {
    explainError.value = String(e);
  } finally {
    isExplaining.value = false;
  }
}
</script>
