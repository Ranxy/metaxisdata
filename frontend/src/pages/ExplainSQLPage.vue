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
                  :to="`/metadata/${selectedMeta.guid}`"
                  class="inline-flex items-center gap-1 text-xs text-accent hover:underline mt-1"
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
                <pre
                  v-else-if="selectedMeta.sqlPreview"
                  class="text-xs font-mono bg-background border rounded-md p-3 max-h-48 overflow-y-auto whitespace-pre-wrap"
                >{{ selectedMeta.sqlPreview }}</pre>
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
          <textarea
            v-model="customSQL"
            :placeholder="t('explainSQL.sqlPlaceholder')"
            class="flex-1 min-h-[200px] bg-input-surface border border-border rounded-md p-3 text-sm font-mono outline-none focus:border-accent resize-none"
          />
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
            <div
              v-if="isExplaining || resultText"
              class="prose prose-sm dark:prose-invert max-w-none whitespace-pre-wrap"
            >
              {{ resultText }}
              <span v-if="isExplaining" class="inline-block w-2 h-4 bg-accent animate-pulse align-middle ml-0.5" />
            </div>
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
import { ExternalLink, Search, Sparkles } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { getSchemaString, searchMetadata } from "@/api/database";
import { explainSQL } from "@/api/explain";
import Badge from "@/components/ui/badge/Badge.vue";
import Button from "@/components/ui/button/Button.vue";
import Label from "@/components/ui/label/Label.vue";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";

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

// ---- computed ----
const canExplain = computed(() => {
  if (sourceMode.value === "metadata") return !!selectedMeta.value;
  return customSQL.value.trim() !== "";
});

// ---- lifecycle ----
onMounted(async () => {
  const guidFromRoute = getGuidFromRoute();
  if (guidFromRoute) {
    await loadMetaByGuid(guidFromRoute);
  }
});

// ---- methods ----
function getGuidFromRoute(): string {
  const g = route.params.guid;
  if (Array.isArray(g)) return g.join("/");
  return (g as string) ?? "";
}

async function loadMetaByGuid(guid: string) {
  const parts = guid.split(";");
  const name = parts[parts.length - 1] ?? guid;
  const path = parts.join(" / ");

  selectedMeta.value = {
    guid,
    name,
    type: guessTypeFromGuid(guid),
    path,
    sqlPreview: "",
  };

  // Fetch DDL asynchronously.
  await fetchMetaSQL(guid, 0 as MetaType);
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

function guessTypeFromGuid(_guid: string): string {
  // Heuristic: look at the prefix pattern for type clues.
  // Most GUIDs follow: instance;database;schema;name
  // The type is determined by the meta_registry_resource entry.
  return "Object";
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

async function selectSearchResult(item: SearchItem) {
  metaSearch.value = "";
  searchResults.value = [];

  selectedMeta.value = {
    guid: item.guid,
    name: item.name,
    type: item.type,
    path: item.path,
    sqlPreview: "",
  };

  await fetchMetaSQL(item.guid, item.metaType);
}

async function startExplain(forceRegen = false) {
  if (isExplaining.value) return;
  isExplaining.value = true;
  resultText.value = "";
  explainError.value = null;
  explainMeta.value = null;

  const input =
    sourceMode.value === "metadata" && selectedMeta.value
      ? { metaGuid: selectedMeta.value.guid, forceRegenerate: forceRegen }
      : { sqlText: customSQL.value.trim(), forceRegenerate: forceRegen };

  try {
    const stream = explainSQL(input);
    for await (const chunk of stream) {
      if (chunk.payload?.case === "content" && chunk.payload.value) {
        resultText.value += chunk.payload.value;
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
