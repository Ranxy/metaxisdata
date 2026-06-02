<template>
  <div class="space-y-4">
    <div>
      <h1 class="text-2xl font-bold tracking-tight">
        {{ t("metadataBrowser.title") }}
      </h1>
    </div>

    <!-- Search Bar -->
    <div class="relative">
      <div
        class="flex items-center flex-wrap gap-2 min-h-[42px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm transition-colors hover:border-ring focus-within:outline-hidden focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2"
      >
        <!-- Active Filter Pills -->
        <Badge
          v-if="scopeFilterDisplay"
          variant="secondary"
          class="flex items-center gap-1 px-2 py-1"
        >
          <span class="text-xs font-medium">{{ t("metadataBrowser.scopeFilter") }}:</span>
          <span class="text-xs">{{ scopeFilterDisplay }}</span>
          <button
            type="button"
            class="ml-1 rounded-full hover:bg-secondary-foreground/20 transition-colors"
            @click="removeScopeFilter"
          >
            <X class="h-3 w-3" />
          </button>
        </Badge>
        <Badge
          v-if="typeFilter != null"
          variant="secondary"
          class="flex items-center gap-1 px-2 py-1"
        >
          <span class="text-xs font-medium">{{ t("metadataBrowser.typeFilter") }}:</span>
          <span class="text-xs">{{ getMetaTypeLabel(typeFilter) }}</span>
          <button
            type="button"
            class="ml-1 rounded-full hover:bg-secondary-foreground/20 transition-colors"
            @click="removeTypeFilter"
          >
            <X class="h-3 w-3" />
          </button>
        </Badge>

        <Search class="h-4 w-4 mr-1 text-muted-foreground shrink-0" />
        <input
          ref="searchInputRef"
          v-model="searchQuery"
          type="text"
          :placeholder="t('metadataBrowser.searchPlaceholder')"
          class="flex-1 min-w-[120px] bg-transparent outline-hidden placeholder:text-muted-foreground"
          @focus="showSearchResults = true"
        >
        <button
          v-if="searchQuery"
          type="button"
          class="ml-2 rounded-full p-0.5 hover:bg-secondary transition-colors"
          @click="clearSearch"
        >
          <X class="h-3.5 w-3.5 text-muted-foreground" />
        </button>

        <!-- Add Filter Button -->
        <Popover v-model:open="showFilterMenu">
          <PopoverTrigger as-child>
            <Button
              v-if="availableFilterTypes.length > 0"
              variant="ghost"
              size="sm"
              class="h-6 px-2 text-xs shrink-0"
            >
              <Plus class="h-3 w-3 mr-1" />
              {{ t("metadataBrowser.addFilter") }}
            </Button>
          </PopoverTrigger>
          <PopoverContent
            class="w-[300px] p-0"
            align="start"
          >
            <!-- Filter Type Selection -->
            <Command
              v-if="!selectedFilterType"
            >
              <CommandInput :placeholder="t('metadataBrowser.searchFilters')" />
              <CommandList>
                <CommandEmpty>{{ t("metadataBrowser.noFiltersFound") }}</CommandEmpty>
                <CommandGroup>
                  <CommandItem
                    v-for="ft in availableFilterTypes"
                    :key="ft.type"
                    :value="ft.type"
                    @select="selectFilterType(ft.type)"
                  >
                    <span class="mr-2">{{ ft.icon }}</span>
                    <span>{{ ft.label }}</span>
                    <ChevronDown class="ml-auto h-4 w-4 -rotate-90" />
                  </CommandItem>
                </CommandGroup>
              </CommandList>
            </Command>

            <!-- Scope Filter: Cascading Instance → Database → Schema -->
            <Command
              v-else-if="selectedFilterType === 'scope'"
            >
              <div class="flex items-center border-b px-3">
                <Button
                  variant="ghost"
                  size="sm"
                  class="h-8 px-2"
                  @click="handleScopeBack"
                >
                  <ChevronDown class="h-4 w-4 rotate-90" />
                </Button>
                <CommandInput
                  v-model="filterSearchQuery"
                  :placeholder="scopeStep === 'instance'
                    ? t('metadataBrowser.selectInstance')
                    : scopeStep === 'database'
                      ? t('metadataBrowser.selectDatabase')
                      : t('metadataBrowser.selectSchema')"
                  class="border-0"
                />
              </div>
              <!-- Step indicator -->
              <div class="flex items-center gap-1 px-3 py-1.5 text-xs text-muted-foreground border-b">
                <span :class="{ 'font-semibold text-foreground': scopeStep === 'instance' }">
                  {{ t("metadataBrowser.scopeStepInstance") }}
                </span>
                <span>›</span>
                <span :class="{ 'font-semibold text-foreground': scopeStep === 'database' }">
                  {{ t("metadataBrowser.scopeStepDatabase") }}
                </span>
                <span>›</span>
                <span :class="{ 'font-semibold text-foreground': scopeStep === 'schema' }">
                  {{ t("metadataBrowser.scopeStepSchema") }}
                </span>
              </div>
              <CommandList>
                <!-- Instance step -->
                <template v-if="scopeStep === 'instance'">
                  <CommandEmpty>{{ t("metadataBrowser.noInstancesFound") }}</CommandEmpty>
                  <CommandGroup>
                    <CommandItem
                      v-for="inst in filteredScopeInstances"
                      :key="inst.name"
                      :value="inst.name"
                      @select="handleScopeSelectInstance(inst)"
                    >
                      {{ inst.title || inst.name.replace('instances/', '') }}
                    </CommandItem>
                  </CommandGroup>
                </template>

                <!-- Database step -->
                <template v-else-if="scopeStep === 'database'">
                  <div
                    v-if="isLoadingScopeOptions"
                    class="p-3 text-center text-sm text-muted-foreground"
                  >
                    {{ t("metadataBrowser.searching") }}
                  </div>
                  <template v-else>
                    <CommandEmpty>{{ t("metadataBrowser.noDatabasesFound") }}</CommandEmpty>
                    <CommandGroup>
                      <!-- Apply scope at instance level without picking a database -->
                      <CommandItem
                        value="__done__"
                        class="font-medium"
                        @select="applyScopeFilter"
                      >
                        ✓ {{ t("metadataBrowser.done") }}
                      </CommandItem>
                      <CommandItem
                        v-for="db in filteredScopeDatabases"
                        :key="db"
                        :value="db"
                        @select="handleScopeSelectDatabase(db)"
                      >
                        {{ db }}
                      </CommandItem>
                    </CommandGroup>
                  </template>
                </template>

                <!-- Schema step -->
                <template v-else-if="scopeStep === 'schema'">
                  <div
                    v-if="isLoadingScopeOptions"
                    class="p-3 text-center text-sm text-muted-foreground"
                  >
                    {{ t("metadataBrowser.searching") }}
                  </div>
                  <template v-else>
                    <CommandEmpty>{{ t("metadataBrowser.noSchemasFound") }}</CommandEmpty>
                    <CommandGroup>
                      <CommandItem
                        value="__done__"
                        class="font-medium"
                        @select="applyScopeFilter"
                      >
                        ✓ {{ t("metadataBrowser.done") }}
                      </CommandItem>
                      <CommandItem
                        v-for="schema in filteredScopeSchemas"
                        :key="schema"
                        :value="schema"
                        @select="handleScopeSelectSchema(schema)"
                      >
                        {{ schema }}
                      </CommandItem>
                    </CommandGroup>
                  </template>
                </template>
              </CommandList>
            </Command>

            <!-- Type Filter: MetaType selection -->
            <Command
              v-else-if="selectedFilterType === 'type'"
            >
              <div class="flex items-center border-b px-3">
                <Button
                  variant="ghost"
                  size="sm"
                  class="h-8 px-2"
                  @click="backToFilterTypes"
                >
                  <ChevronDown class="h-4 w-4 rotate-90" />
                </Button>
                <CommandInput
                  v-model="filterSearchQuery"
                  :placeholder="t('metadataBrowser.selectType')"
                  class="border-0"
                />
              </div>
              <CommandList>
                <CommandEmpty>{{ t("metadataBrowser.noFiltersFound") }}</CommandEmpty>
                <CommandGroup>
                  <CommandItem
                    v-for="mt in filteredMetaTypes"
                    :key="mt"
                    :value="String(mt)"
                    @select="handleSelectTypeFilter(mt)"
                  >
                    {{ getMetaTypeLabel(mt) }}
                  </CommandItem>
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </div>
      <!-- Search Results Dropdown -->
      <div
        v-if="showSearchResults && searchQuery.trim()"
        class="absolute z-50 top-full mt-1 w-full rounded-md border bg-popover shadow-lg max-h-[400px] overflow-auto"
      >
        <div
          v-if="isSearching"
          class="p-4 text-center text-sm text-muted-foreground"
        >
          {{ t("metadataBrowser.searching") }}
        </div>
        <div
          v-else-if="searchError"
          class="p-4 text-center text-sm text-destructive"
        >
          {{ searchError }}
        </div>
        <div
          v-else-if="searchResults.length === 0"
          class="p-4 text-center text-sm text-muted-foreground"
        >
          {{ t("metadataBrowser.noSearchResults") }}
        </div>
        <div
          v-else
          class="py-1"
        >
          <button
            v-for="result in searchResults"
            :key="result.guid"
            type="button"
            class="w-full text-left px-3 py-2 text-sm hover:bg-accent hover:text-accent-foreground transition-colors flex items-center gap-3"
            @click="handleSelectSearchResult(result)"
          >
            <Badge
              variant="outline"
              class="shrink-0 text-xs"
            >
              {{ getMetaTypeLabel(result.metaType) }}
            </Badge>
            <div class="min-w-0 flex-1">
              <div class="font-medium truncate">
                {{ getSearchResultName(result) }}
              </div>
              <div class="text-xs text-muted-foreground truncate">
                {{ result.guid }}
              </div>
            </div>
          </button>
        </div>
      </div>
    </div>

    <Card>
      <CardContent class="py-3 px-4">
        <MetadataBreadcrumb
          :items="breadcrumbItems"
          @navigate="handleNavigate"
        />
      </CardContent>
    </Card>

    <Card>
      <div
        v-if="isLoading"
        class="p-8 flex justify-center"
      >
        <AppLoading />
      </div>

      <div
        v-else-if="error"
        class="p-8 text-center text-destructive"
      >
        {{ error }}
      </div>

      <div v-else-if="isRootPath">
        <CardHeader>
          <CardTitle>{{ t("metadataBrowser.instances") }}</CardTitle>
        </CardHeader>
        <InstanceList
          :instances="instances"
          :is-loading="isLoadingInstances"
          @select="handleSelectInstance"
        />
      </div>

      <div v-else>
        <template v-if="isTableDetailView && leafTable">
          <CardContent class="p-0 max-h-[calc(100vh-16rem)] overflow-auto">
            <TableMetadataDetail
              :table="leafTable"
              :instance-engine="currentInstanceEngine"
              :guid="currentGuid"
              :selected-column-name="selectedColumnName"
            />
          </CardContent>
        </template>

        <template v-else-if="isViewDetailView && leafView">
          <CardContent class="p-0 max-h-[calc(100vh-16rem)] overflow-auto">
            <ViewMetadataDetail
              :view="leafView"
              :guid="currentGuid"
            />
          </CardContent>
        </template>

        <template v-else-if="isMaterializedViewDetailView && leafMaterializedView">
          <CardContent class="p-0 max-h-[calc(100vh-16rem)] overflow-auto">
            <MaterializedViewMetadataDetail
              :view="leafMaterializedView"
              :guid="currentGuid"
            />
          </CardContent>
        </template>

        <template v-else-if="isFunctionDetailView && leafFunction">
          <CardContent class="p-0 max-h-[calc(100vh-16rem)] overflow-auto">
            <FunctionMetadataDetail :fn="leafFunction" />
          </CardContent>
        </template>

        <template v-else-if="isProcedureDetailView && leafProcedure">
          <CardContent class="p-0 max-h-[calc(100vh-16rem)] overflow-auto">
            <ProcedureMetadataDetail :proc="leafProcedure" />
          </CardContent>
        </template>

        <template v-else-if="isSequenceDetailView && leafSequence">
          <CardContent class="p-0 max-h-[calc(100vh-16rem)] overflow-auto">
            <SequenceMetadataDetail :seq="leafSequence" />
          </CardContent>
        </template>

        <template v-else-if="isManualSQLDetailView && leafManualSQL">
          <CardContent class="p-0 max-h-[calc(100vh-16rem)] overflow-auto">
            <ManualSQLMetadataDetail
              :manual-sql="leafManualSQL"
              :guid="currentGuid"
            />
          </CardContent>
        </template>

        <template v-else-if="isExternalDatasetDetailView && externalDatasetDetail">
          <CardContent class="p-0 max-h-[calc(100vh-16rem)] overflow-auto">
            <ExternalDatasetMetadataDetail
              :guid="currentGuid"
              :name="externalDatasetDetail.name"
              :namespace="externalDatasetDetail.namespace"
              :dataset-type="externalDatasetDetail.datasetType"
            />
          </CardContent>
        </template>

        <template v-else>
          <CardHeader class="border-b">
            <MetadataTabNav
              :groups="metadataGroups"
              :active="activeMetaType"
              @select="handleSelectTab"
            />
          </CardHeader>

          <CardContent class="p-0">
            <MetadataList
              v-if="activeGroup"
              :meta-type="activeGroup.metaType"
              :items="activeGroup.list"
              :current-guid="currentGuid"
              :is-mysql="isMySQLInstance"
              @select="handleSelectMetadata"
            />
            <div
              v-else
              class="p-8 text-center text-muted-foreground"
            >
              {{ t("metadataBrowser.empty") }}
            </div>
          </CardContent>

          <div
            v-if="selectedMetaType && selectedNextPageToken"
            class="p-4 border-t"
          >
            <MetadataPagination
              :has-next="!!selectedNextPageToken"
              :is-loading="isLoadingMore"
              @load-more="loadMoreMetadata"
            />
          </div>
        </template>
      </div>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { ChevronDown, Plus, Search, X } from "lucide-vue-next";
import {
  computed,
  defineAsyncComponent,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { getMetadata, listMetadata, searchMetadata } from "@/api/database";
import { getInstance, listInstances } from "@/api/instance";
import AppLoading from "@/components/common/AppLoading.vue";
import InstanceList from "@/components/metadata/InstanceList.vue";
import MetadataBreadcrumb from "@/components/metadata/MetadataBreadcrumb.vue";
import MetadataList from "@/components/metadata/MetadataList.vue";
import MetadataPagination from "@/components/metadata/MetadataPagination.vue";
import MetadataTabNav from "@/components/metadata/MetadataTabNav.vue";

// Detail components lazy-loaded — only needed in leaf views, not on the root page.
const ExternalDatasetMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/ExternalDatasetMetadataDetail.vue")
);
const FunctionMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/FunctionMetadataDetail.vue")
);
const ManualSQLMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/ManualSQLMetadataDetail.vue")
);
const MaterializedViewMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/MaterializedViewMetadataDetail.vue")
);
const ProcedureMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/ProcedureMetadataDetail.vue")
);
const SequenceMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/SequenceMetadataDetail.vue")
);
const TableMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/TableMetadataDetail.vue")
);
const ViewMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/ViewMetadataDetail.vue")
);

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Engine, State } from "@/types/proto-es/v1/common_pb";
import {
  type FunctionMetadata,
  type ManualSQLMetadata,
  type MaterializedViewMetadata,
  type MetadataResponse_MetadataList,
  MetaType,
  type ProcedureMetadata,
  type SearchMetadataResult,
  type SequenceMetadata,
  type StoredMetadata,
  type TableMetadata,
  type ViewMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";
import { extractErrorMessage } from "@/utils/error";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const isLoading = ref(false);
const isLoadingInstances = ref(false);
const isLoadingMore = ref(false);
const error = ref<string | null>(null);

// Search state
const searchQuery = ref("");
const searchInputRef = ref<HTMLInputElement>();
const isSearching = ref(false);
const searchError = ref<string | null>(null);
const searchResults = ref<SearchMetadataResult[]>([]);
const showSearchResults = ref(false);
let searchDebounceTimer: ReturnType<typeof setTimeout> | null = null;

// Filter state
const showFilterMenu = ref(false);
const selectedFilterType = ref<"scope" | "type" | null>(null);
const filterSearchQuery = ref("");

// Scope filter: cascading instance → database → schema
const scopeFilterInstance = ref<string | null>(null); // instance id (e.g. "my-pg")
const scopeFilterDatabase = ref<string | null>(null);
const scopeFilterSchema = ref<string | null>(null);
const scopeStepDatabases = ref<string[]>([]);
const scopeStepSchemas = ref<string[]>([]);
const scopeStep = ref<"instance" | "database" | "schema">("instance");
const isLoadingScopeOptions = ref(false);

// Type filter
const typeFilter = ref<MetaType | null>(null);

// Computed: active filter pills to display
const scopeFilterDisplay = computed(() => {
  if (!scopeFilterInstance.value) return null;
  const parts = [scopeFilterInstance.value];
  if (scopeFilterDatabase.value) parts.push(scopeFilterDatabase.value);
  if (scopeFilterSchema.value) parts.push(scopeFilterSchema.value);
  return parts.join(" > ");
});

const scopeFilterGuidPrefix = computed(() => {
  if (!scopeFilterInstance.value) return undefined;
  const parts = [scopeFilterInstance.value];
  if (scopeFilterDatabase.value) parts.push(scopeFilterDatabase.value);
  if (scopeFilterSchema.value) parts.push(scopeFilterSchema.value);
  return parts.join(";");
});

// Available filter types (hide ones already active)
const availableFilterTypes = computed(() => {
  const types: Array<{ type: "scope" | "type"; label: string; icon: string }> =
    [];
  if (!scopeFilterInstance.value) {
    types.push({
      type: "scope",
      label: t("metadataBrowser.scopeFilter"),
      icon: "📍",
    });
  }
  if (typeFilter.value == null) {
    types.push({
      type: "type",
      label: t("metadataBrowser.typeFilter"),
      icon: "🏷️",
    });
  }
  return types;
});

// Searchable meta types for type filter
const searchableMetaTypes = [
  MetaType.DATABASE,
  MetaType.SCHEMA,
  MetaType.TABLE,
  MetaType.COLUMN,
  MetaType.VIEW,
  MetaType.MATERIALIZED_VIEW,
  MetaType.FUNCTION,
  MetaType.PROCEDURE,
  MetaType.SEQUENCE,
  MetaType.MANUAL_SQL,
];

const filteredMetaTypes = computed(() => {
  if (!filterSearchQuery.value) return searchableMetaTypes;
  const q = filterSearchQuery.value.toLowerCase();
  return searchableMetaTypes.filter((mt) =>
    getMetaTypeLabel(mt).toLowerCase().includes(q)
  );
});

// Filtered scope options
const filteredScopeInstances = computed(() => {
  if (!filterSearchQuery.value) return instances.value;
  const q = filterSearchQuery.value.toLowerCase();
  return instances.value.filter((i) => {
    const id = i.name.replace("instances/", "");
    return (
      id.toLowerCase().includes(q) || (i.title ?? "").toLowerCase().includes(q)
    );
  });
});

const filteredScopeDatabases = computed(() => {
  if (!filterSearchQuery.value) return scopeStepDatabases.value;
  const q = filterSearchQuery.value.toLowerCase();
  return scopeStepDatabases.value.filter((d) => d.toLowerCase().includes(q));
});

const filteredScopeSchemas = computed(() => {
  if (!filterSearchQuery.value) return scopeStepSchemas.value;
  const q = filterSearchQuery.value.toLowerCase();
  return scopeStepSchemas.value.filter((s) => s.toLowerCase().includes(q));
});

const instances = ref<Instance[]>([]);
const metadataGroups = ref<MetadataResponse_MetadataList[]>([]);

const activeMetaType = ref<MetaType | null>(null);
const selectedMetaType = ref<MetaType | null>(null);

// The backend returns a nextPageToken per metaType (on each MetadataList).
// Track them separately to avoid token mix-ups when switching tabs.
const nextPageTokenByMetaType = reactive(new Map<MetaType, string>());

const selectedNextPageToken = computed(() => {
  if (!selectedMetaType.value) return "";
  return nextPageTokenByMetaType.get(selectedMetaType.value) ?? "";
});

const currentInstanceEngine = ref<Engine | null>(null);

const leafTable = ref<TableMetadata | null>(null);
const leafView = ref<ViewMetadata | null>(null);
const leafMaterializedView = ref<MaterializedViewMetadata | null>(null);
const leafFunction = ref<FunctionMetadata | null>(null);
const leafProcedure = ref<ProcedureMetadata | null>(null);
const leafSequence = ref<SequenceMetadata | null>(null);
const leafManualSQL = ref<ManualSQLMetadata | null>(null);

type ExternalDatasetDetail = {
  name: string;
  namespace: string;
  datasetType: string;
};

const requestedLeafMetaType = computed(() => {
  const q = route.query.metaType;
  if (!q) return null;
  const raw = Array.isArray(q) ? q[0] : q;
  const value = Number(raw);
  if (!Number.isFinite(value)) return null;
  return value as MetaType;
});

const selectedColumnName = computed(() => getQueryString("column"));

const isTableDetailView = computed(() => {
  return (
    requestedLeafMetaType.value === MetaType.TABLE || leafTable.value != null
  );
});

const isViewDetailView = computed(() => {
  return (
    requestedLeafMetaType.value === MetaType.VIEW || leafView.value != null
  );
});

const isMaterializedViewDetailView = computed(() => {
  return (
    requestedLeafMetaType.value === MetaType.MATERIALIZED_VIEW ||
    leafMaterializedView.value != null
  );
});

const isFunctionDetailView = computed(() => {
  return (
    requestedLeafMetaType.value === MetaType.FUNCTION ||
    leafFunction.value != null
  );
});

const isProcedureDetailView = computed(() => {
  return (
    requestedLeafMetaType.value === MetaType.PROCEDURE ||
    leafProcedure.value != null
  );
});

const isSequenceDetailView = computed(() => {
  return (
    requestedLeafMetaType.value === MetaType.SEQUENCE ||
    leafSequence.value != null
  );
});

const isManualSQLDetailView = computed(() => {
  return (
    requestedLeafMetaType.value === MetaType.MANUAL_SQL ||
    leafManualSQL.value != null
  );
});

const externalDatasetDetail = computed<ExternalDatasetDetail | null>(() => {
  if (!currentGuid.value.startsWith("external:")) {
    return null;
  }

  return {
    name: getQueryString("externalName"),
    namespace: getQueryString("externalNamespace"),
    datasetType: getQueryString("externalDatasetType"),
  };
});

const isExternalDatasetDetailView = computed(() => {
  return externalDatasetDetail.value != null;
});

const currentGuid = computed(() => {
  const guidParam = route.params.guid;
  if (!guidParam) return "";

  const guidStr = Array.isArray(guidParam) ? guidParam.join("/") : guidParam;
  const segments = guidStr
    .split("/")
    .map((s) => decodeURIComponent(s))
    .map((s) => (s === "~" ? "" : s));

  return segments.join(";");
});

const isRootPath = computed(() => !currentGuid.value);

const guidSegments = computed(() => {
  if (!currentGuid.value) return [];
  // keep empty string segment (MySQL empty schema)
  return currentGuid.value.split(";");
});

const instanceIdFromGuid = computed(() => {
  const first = guidSegments.value[0];
  return first || "";
});

const breadcrumbItems = computed(() => {
  if (isExternalDatasetDetailView.value) {
    return [
      {
        label:
          externalDatasetDetail.value?.name ||
          externalDatasetDetail.value?.namespace ||
          currentGuid.value,
        guidIndex: 0,
      },
    ];
  }

  // Hide MySQL empty schema segment from breadcrumb display, but keep
  // navigation working by preserving original guid indices.
  const segments = guidSegments.value;
  if (segments.length === 0)
    return [] as Array<{ label: string; guidIndex: number }>;

  // Do not show trailing empty segment.
  const trimmed = [...segments];
  while (trimmed.length > 0 && trimmed[trimmed.length - 1] === "") {
    trimmed.pop();
  }

  const items = trimmed.map((label, guidIndex) => ({ label, guidIndex }));

  if (!isMySQLInstance.value) return items;

  // MySQL-family has no real schemas; the implicit empty schema should not be shown.
  // The schema position is index 2: instance;database;schema;...
  return items.filter((it) => !(it.guidIndex === 2 && it.label === ""));
});

const isMySQLInstance = computed(() => {
  if (!currentInstanceEngine.value) return false;
  return (
    currentInstanceEngine.value === Engine.MYSQL ||
    currentInstanceEngine.value === Engine.MARIADB ||
    currentInstanceEngine.value === Engine.TIDB
  );
});

const activeGroup = computed(() => {
  if (!activeMetaType.value) return null;
  return (
    metadataGroups.value.find((g) => g.metaType === activeMetaType.value) ??
    null
  );
});

function getEffectiveParentGuidForListing(): string {
  // MySQL-family special case: objects are under an "empty schema".
  // The backend expects the parent guid to include the trailing semicolon
  // (e.g. "instances/x;databases/y;") to list tables/views directly.
  const base = currentGuid.value;
  if (!base) return "";
  if (!isMySQLInstance.value) return base;
  if (guidSegments.value.length === 2 && !base.endsWith(";")) {
    return `${base};`;
  }
  return base;
}

function getQueryString(key: string): string {
  const value = route.query[key];
  if (!value) return "";
  return Array.isArray(value) ? value[0] || "" : value;
}

async function fetchInstances() {
  isLoadingInstances.value = true;
  error.value = null;

  try {
    const response = await listInstances({ pageSize: 100 });
    instances.value = response.instances.filter(
      (i) => i.state !== State.DELETED
    );
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoadingInstances.value = false;
  }
}

async function fetchCurrentInstanceEngineIfNeeded() {
  if (!instanceIdFromGuid.value) return;
  if (currentInstanceEngine.value) return;

  try {
    const inst = await getInstance(`instances/${instanceIdFromGuid.value}`);
    currentInstanceEngine.value = inst.engine;
  } catch {
    // Non-fatal: engine detection only affects MySQL empty schema handling.
  }
}

async function fetchMetadataGroups() {
  isLoading.value = true;
  error.value = null;
  leafTable.value = null;
  leafView.value = null;
  leafMaterializedView.value = null;
  leafFunction.value = null;
  leafProcedure.value = null;
  leafSequence.value = null;

  try {
    await fetchCurrentInstanceEngineIfNeeded();

    const response = await listMetadata({
      parentGuid: getEffectiveParentGuidForListing(),
      pageSize: 20,
    });

    metadataGroups.value = response.typesStoredMetadata;

    nextPageTokenByMetaType.clear();
    for (const group of response.typesStoredMetadata) {
      nextPageTokenByMetaType.set(group.metaType, group.nextPageToken);
    }

    if (!activeMetaType.value && response.typesStoredMetadata.length > 0) {
      activeMetaType.value = response.typesStoredMetadata[0].metaType;
    }

    // Enable pagination for the currently active tab immediately.
    // The backend returns a nextPageToken per metaType in the initial response.
    if (activeMetaType.value && !selectedMetaType.value) {
      selectedMetaType.value = activeMetaType.value;
    }

    // If we are at a leaf object (e.g. table/view), ListMetadata may return
    // non-empty children (columns).  When the caller explicitly requests a leaf
    // type via ?metaType, still call GetMetadata so the detail view can render.
    const explicitLeaf = requestedLeafMetaType.value != null;
    if (response.typesStoredMetadata.length === 0 || explicitLeaf) {
      try {
        const preferred = requestedLeafMetaType.value;

        if (preferred === MetaType.TABLE) {
          const detail = await getMetadata({
            guid: currentGuid.value,
            metaType: MetaType.TABLE,
          });
          if (detail.metadata?.type?.case === "tableMetadata") {
            leafTable.value = detail.metadata.type.value;
            return;
          }
          if (!explicitLeaf) return;
        }

        if (preferred === MetaType.VIEW) {
          const detail = await getMetadata({
            guid: currentGuid.value,
            metaType: MetaType.VIEW,
          });
          if (detail.metadata?.type?.case === "viewMetadata") {
            leafView.value = detail.metadata.type.value;
            return;
          }
          if (!explicitLeaf) return;
        }

        if (preferred === MetaType.MATERIALIZED_VIEW) {
          const detail = await getMetadata({
            guid: currentGuid.value,
            metaType: MetaType.MATERIALIZED_VIEW,
          });
          if (detail.metadata?.type?.case === "materializedViewMetadata") {
            leafMaterializedView.value = detail.metadata.type.value;
            return;
          }
          if (!explicitLeaf) return;
        }

        if (preferred === MetaType.FUNCTION) {
          const detail = await getMetadata({
            guid: currentGuid.value,
            metaType: MetaType.FUNCTION,
          });
          if (detail.metadata?.type?.case === "functionMetadata") {
            leafFunction.value = detail.metadata.type.value;
            return;
          }
          if (!explicitLeaf) return;
        }

        if (preferred === MetaType.PROCEDURE) {
          const detail = await getMetadata({
            guid: currentGuid.value,
            metaType: MetaType.PROCEDURE,
          });
          if (detail.metadata?.type?.case === "procedureMetadata") {
            leafProcedure.value = detail.metadata.type.value;
            return;
          }
          if (!explicitLeaf) return;
        }

        if (preferred === MetaType.SEQUENCE) {
          const detail = await getMetadata({
            guid: currentGuid.value,
            metaType: MetaType.SEQUENCE,
          });
          if (detail.metadata?.type?.case === "sequenceMetadata") {
            leafSequence.value = detail.metadata.type.value;
            return;
          }
          if (!explicitLeaf) return;
        }

        // No hint from route: try common leaf types.
        const tableDetail = await getMetadata({
          guid: currentGuid.value,
          metaType: MetaType.TABLE,
        });
        if (tableDetail.metadata?.type?.case === "tableMetadata") {
          leafTable.value = tableDetail.metadata.type.value;
          return;
        }

        const viewDetail = await getMetadata({
          guid: currentGuid.value,
          metaType: MetaType.VIEW,
        });
        if (viewDetail.metadata?.type?.case === "viewMetadata") {
          leafView.value = viewDetail.metadata.type.value;
          return;
        }

        const mvDetail = await getMetadata({
          guid: currentGuid.value,
          metaType: MetaType.MATERIALIZED_VIEW,
        });
        if (mvDetail.metadata?.type?.case === "materializedViewMetadata") {
          leafMaterializedView.value = mvDetail.metadata.type.value;
          return;
        }

        const fnDetail = await getMetadata({
          guid: currentGuid.value,
          metaType: MetaType.FUNCTION,
        });
        if (fnDetail.metadata?.type?.case === "functionMetadata") {
          leafFunction.value = fnDetail.metadata.type.value;
          return;
        }

        const procDetail = await getMetadata({
          guid: currentGuid.value,
          metaType: MetaType.PROCEDURE,
        });
        if (procDetail.metadata?.type?.case === "procedureMetadata") {
          leafProcedure.value = procDetail.metadata.type.value;
          return;
        }

        const seqDetail = await getMetadata({
          guid: currentGuid.value,
          metaType: MetaType.SEQUENCE,
        });
        if (seqDetail.metadata?.type?.case === "sequenceMetadata") {
          leafSequence.value = seqDetail.metadata.type.value;
        }
      } catch {
        // Ignore; keep empty state for unhandled leaf objects.
      }
    }
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function fetchSequenceDetail() {
  isLoading.value = true;
  error.value = null;
  leafTable.value = null;
  leafView.value = null;
  leafMaterializedView.value = null;
  leafFunction.value = null;
  leafProcedure.value = null;
  leafSequence.value = null;
  leafManualSQL.value = null;
  metadataGroups.value = [];
  nextPageTokenByMetaType.clear();
  activeMetaType.value = null;
  selectedMetaType.value = null;

  try {
    await fetchCurrentInstanceEngineIfNeeded();

    const detail = await getMetadata({
      guid: currentGuid.value,
      metaType: MetaType.SEQUENCE,
    });

    if (detail.metadata?.type?.case !== "sequenceMetadata") {
      throw new Error("unexpected metadata type");
    }

    leafSequence.value = detail.metadata.type.value;
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function fetchManualSQLDetail() {
  isLoading.value = true;
  error.value = null;
  leafTable.value = null;
  leafView.value = null;
  leafMaterializedView.value = null;
  leafFunction.value = null;
  leafProcedure.value = null;
  leafSequence.value = null;
  leafManualSQL.value = null;
  metadataGroups.value = [];
  nextPageTokenByMetaType.clear();
  activeMetaType.value = null;
  selectedMetaType.value = null;

  try {
    await fetchCurrentInstanceEngineIfNeeded();

    const detail = await getMetadata({
      guid: currentGuid.value,
      metaType: MetaType.MANUAL_SQL,
    });

    if (detail.metadata?.type?.case !== "manualSqlMetadata") {
      throw new Error("unexpected metadata type");
    }

    leafManualSQL.value = detail.metadata.type.value;
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function fetchProcedureDetail() {
  isLoading.value = true;
  error.value = null;
  leafTable.value = null;
  leafView.value = null;
  leafMaterializedView.value = null;
  leafFunction.value = null;
  leafProcedure.value = null;
  leafSequence.value = null;
  metadataGroups.value = [];
  nextPageTokenByMetaType.clear();
  activeMetaType.value = null;
  selectedMetaType.value = null;

  try {
    await fetchCurrentInstanceEngineIfNeeded();

    const detail = await getMetadata({
      guid: currentGuid.value,
      metaType: MetaType.PROCEDURE,
    });

    if (detail.metadata?.type?.case !== "procedureMetadata") {
      throw new Error("unexpected metadata type");
    }

    leafProcedure.value = detail.metadata.type.value;
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function fetchFunctionDetail() {
  isLoading.value = true;
  error.value = null;
  leafTable.value = null;
  leafView.value = null;
  leafMaterializedView.value = null;
  leafFunction.value = null;
  leafProcedure.value = null;
  leafSequence.value = null;
  metadataGroups.value = [];
  nextPageTokenByMetaType.clear();
  activeMetaType.value = null;
  selectedMetaType.value = null;

  try {
    await fetchCurrentInstanceEngineIfNeeded();

    const detail = await getMetadata({
      guid: currentGuid.value,
      metaType: MetaType.FUNCTION,
    });

    if (detail.metadata?.type?.case !== "functionMetadata") {
      throw new Error("unexpected metadata type");
    }

    leafFunction.value = detail.metadata.type.value;
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function fetchMaterializedViewDetail() {
  isLoading.value = true;
  error.value = null;
  leafTable.value = null;
  leafView.value = null;
  leafMaterializedView.value = null;
  metadataGroups.value = [];
  nextPageTokenByMetaType.clear();
  activeMetaType.value = null;
  selectedMetaType.value = null;

  try {
    await fetchCurrentInstanceEngineIfNeeded();

    const detail = await getMetadata({
      guid: currentGuid.value,
      metaType: MetaType.MATERIALIZED_VIEW,
    });

    if (detail.metadata?.type?.case !== "materializedViewMetadata") {
      throw new Error("unexpected metadata type");
    }

    leafMaterializedView.value = detail.metadata.type.value;
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function fetchTableDetail() {
  isLoading.value = true;
  error.value = null;
  leafTable.value = null;
  leafView.value = null;
  metadataGroups.value = [];
  nextPageTokenByMetaType.clear();
  activeMetaType.value = null;
  selectedMetaType.value = null;

  try {
    await fetchCurrentInstanceEngineIfNeeded();

    const detail = await getMetadata({
      guid: currentGuid.value,
      metaType: MetaType.TABLE,
    });

    if (detail.metadata?.type?.case !== "tableMetadata") {
      throw new Error("unexpected metadata type");
    }

    leafTable.value = detail.metadata.type.value;
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function fetchViewDetail() {
  isLoading.value = true;
  error.value = null;
  leafTable.value = null;
  leafView.value = null;
  metadataGroups.value = [];
  nextPageTokenByMetaType.clear();
  activeMetaType.value = null;
  selectedMetaType.value = null;

  try {
    await fetchCurrentInstanceEngineIfNeeded();

    const detail = await getMetadata({
      guid: currentGuid.value,
      metaType: MetaType.VIEW,
    });

    if (detail.metadata?.type?.case !== "viewMetadata") {
      throw new Error("unexpected metadata type");
    }

    leafView.value = detail.metadata.type.value;
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function fetchMetaTypeFirstPage(metaType: MetaType) {
  isLoading.value = true;
  error.value = null;

  try {
    const response = await listMetadata({
      parentGuid: getEffectiveParentGuidForListing(),
      pageSize: 20,
      pageToken: "",
      metaType,
    });

    const returned = response.typesStoredMetadata[0];
    if (returned) {
      const group = metadataGroups.value.find((g) => g.metaType === metaType);
      if (group) {
        group.list = [...returned.list];
      } else {
        // Fall back to response payload (has correct protobuf message shape).
        metadataGroups.value = response.typesStoredMetadata;
      }
    }

    nextPageTokenByMetaType.set(metaType, returned?.nextPageToken ?? "");
    selectedMetaType.value = metaType;
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function loadMoreMetadata() {
  if (!selectedMetaType.value) return;
  const pageToken = selectedNextPageToken.value;
  if (!pageToken) return;

  isLoadingMore.value = true;

  try {
    const response = await listMetadata({
      parentGuid: getEffectiveParentGuidForListing(),
      pageSize: 20,
      pageToken,
      metaType: selectedMetaType.value,
    });

    const returned = response.typesStoredMetadata[0];
    if (returned) {
      const group = metadataGroups.value.find(
        (g) => g.metaType === selectedMetaType.value
      );
      if (group) {
        group.list.push(...returned.list);
      }
    }

    nextPageTokenByMetaType.set(
      selectedMetaType.value,
      returned?.nextPageToken ?? ""
    );
  } finally {
    isLoadingMore.value = false;
  }
}

function toGuidPath(segments: string[]): string {
  return segments
    .map((s) => (s === "" ? "~" : encodeURIComponent(s)))
    .join("/");
}

function handleNavigate(guidIndex: number) {
  if (guidIndex < 0) {
    router.push({ name: "MetadataBrowser" });
    return;
  }

  const segments = guidSegments.value.slice(0, guidIndex + 1);
  router.push({
    name: "MetadataDetail",
    params: { guid: toGuidPath(segments) },
  });
}

function handleSelectInstance(instance: Instance) {
  const instanceId = instance.name.replace("instances/", "");
  currentInstanceEngine.value = instance.engine;
  router.push({
    name: "MetadataDetail",
    params: { guid: toGuidPath([instanceId]) },
  });
}

function getMetadataName(item: StoredMetadata): string {
  switch (item.type.case) {
    case "databaseSchemaMetadata":
    case "schemaMetadata":
    case "tableMetadata":
    case "viewMetadata":
    case "externalTableMetadata":
    case "materializedViewMetadata":
    case "functionMetadata":
    case "procedureMetadata":
    case "packageMetadata":
    case "sequenceMetadata":
    case "streamMetadata":
    case "taskMetadata":
      return item.type.value.name;
    case "columnMetadata":
      return item.type.value.name;
    case "manualSqlMetadata":
      return item.type.value.title || item.type.value.name;
    default:
      return "";
  }
}

function handleSelectMetadata(item: StoredMetadata, metaType: MetaType) {
  if (item.type.case === "manualSqlMetadata") {
    const manualSQL = item.type.value;
    const segments = [
      guidSegments.value[0] || "",
      guidSegments.value[1] || "",
      manualSQL.schemaName || "",
      `__manual_sql__/${manualSQL.manualSqlId}`,
    ];

    router.push({
      name: "MetadataDetail",
      params: { guid: toGuidPath(segments) },
      query: { metaType: String(MetaType.MANUAL_SQL) },
    });
    return;
  }

  const name = getMetadataName(item);
  const newSegments = [...guidSegments.value];

  // Preserve MySQL implicit empty schema segment.
  const hadMySQLEmptySchema =
    isMySQLInstance.value &&
    guidSegments.value.length >= 3 &&
    guidSegments.value[2] === "";

  // Normalize by trimming only trailing empties; for MySQL we re-add the implicit
  // schema placeholder if it was part of the current guid.
  while (newSegments.length > 0 && newSegments[newSegments.length - 1] === "") {
    newSegments.pop();
  }
  if (hadMySQLEmptySchema && newSegments.length === 2) {
    newSegments.push("");
  }

  // If we're at MySQL database-level (instance;database) but listing children of
  // the implicit empty schema, we must include the empty schema segment in the URL
  // so subsequent navigation has a correct guid.
  if (
    isMySQLInstance.value &&
    guidSegments.value.length === 2 &&
    metaType !== MetaType.DATABASE
  ) {
    newSegments.push("");
  }

  newSegments.push(name);

  // MySQL special: when navigating from database -> children, add empty schema.
  if (metaType === MetaType.DATABASE && isMySQLInstance.value) {
    newSegments.push("");
  }

  const query =
    metaType === MetaType.TABLE ||
    metaType === MetaType.VIEW ||
    metaType === MetaType.MATERIALIZED_VIEW ||
    metaType === MetaType.FUNCTION ||
    metaType === MetaType.PROCEDURE ||
    metaType === MetaType.SEQUENCE ||
    metaType === MetaType.MANUAL_SQL
      ? { metaType: String(metaType) }
      : undefined;

  router.push({
    name: "MetadataDetail",
    params: { guid: toGuidPath(newSegments) },
    query,
  });
}

async function handleSelectTab(metaType: MetaType) {
  activeMetaType.value = metaType;

  // Selecting a tab enables pagination for that metaType.
  // Always fetch the first page with metaType to obtain a valid nextPageToken.
  await fetchMetaTypeFirstPage(metaType);
}

watch(
  () => [
    route.params.guid,
    route.query.metaType,
    route.query.externalNamespace,
    route.query.externalName,
    route.query.externalDatasetType,
  ],
  async () => {
    activeMetaType.value = null;
    selectedMetaType.value = null;
    nextPageTokenByMetaType.clear();
    currentInstanceEngine.value = null;
    leafTable.value = null;
    leafView.value = null;
    leafMaterializedView.value = null;
    leafFunction.value = null;
    leafProcedure.value = null;
    leafSequence.value = null;
    leafManualSQL.value = null;

    if (isRootPath.value) {
      await fetchInstances();
    } else if (isExternalDatasetDetailView.value) {
      error.value = null;
      isLoading.value = false;
      metadataGroups.value = [];
    } else {
      if (requestedLeafMetaType.value === MetaType.TABLE) {
        await fetchTableDetail();
      } else if (requestedLeafMetaType.value === MetaType.VIEW) {
        await fetchViewDetail();
      } else if (requestedLeafMetaType.value === MetaType.MATERIALIZED_VIEW) {
        await fetchMaterializedViewDetail();
      } else if (requestedLeafMetaType.value === MetaType.FUNCTION) {
        await fetchFunctionDetail();
      } else if (requestedLeafMetaType.value === MetaType.PROCEDURE) {
        await fetchProcedureDetail();
      } else if (requestedLeafMetaType.value === MetaType.SEQUENCE) {
        await fetchSequenceDetail();
      } else if (requestedLeafMetaType.value === MetaType.MANUAL_SQL) {
        await fetchManualSQLDetail();
      } else {
        await fetchMetadataGroups();
      }
    }
  },
  { immediate: true }
);

// Search logic
function getMetaTypeLabel(type: MetaType): string {
  const labels: Partial<Record<MetaType, string>> = {
    [MetaType.DATABASE]: t("metadataBrowser.databases"),
    [MetaType.SCHEMA]: t("metadataBrowser.schemas"),
    [MetaType.TABLE]: t("metadataBrowser.tables"),
    [MetaType.COLUMN]: t("metadataBrowser.columns"),
    [MetaType.EXTERNAL_TABLE]: t("metadataBrowser.externalTables"),
    [MetaType.VIEW]: t("metadataBrowser.views"),
    [MetaType.MATERIALIZED_VIEW]: t("metadataBrowser.materializedViews"),
    [MetaType.FUNCTION]: t("metadataBrowser.functions"),
    [MetaType.PROCEDURE]: t("metadataBrowser.procedures"),
    [MetaType.SEQUENCE]: t("metadataBrowser.sequences"),
    [MetaType.MANUAL_SQL]: t("metadataBrowser.manualSqls"),
  };
  return labels[type] || t("metadataBrowser.other");
}

function getSearchResultName(result: SearchMetadataResult): string {
  if (result.metadata) {
    return getMetadataName(result.metadata);
  }
  const segments = result.guid.split(";");
  return segments[segments.length - 1] || result.guid;
}

async function performSearch(query: string) {
  if (!query.trim()) {
    searchResults.value = [];
    return;
  }

  isSearching.value = true;
  searchError.value = null;

  try {
    const response = await searchMetadata({
      searchStr: query.trim(),
      parentGuidPrefix: scopeFilterGuidPrefix.value,
      metaType: typeFilter.value ?? undefined,
    });
    searchResults.value = response.results;
  } catch {
    searchError.value = t("metadataBrowser.searchError");
    searchResults.value = [];
  } finally {
    isSearching.value = false;
  }
}

const leafMetaTypes = new Set<MetaType>([
  MetaType.TABLE,
  MetaType.VIEW,
  MetaType.MATERIALIZED_VIEW,
  MetaType.FUNCTION,
  MetaType.PROCEDURE,
  MetaType.SEQUENCE,
  MetaType.MANUAL_SQL,
]);

function handleSelectSearchResult(result: SearchMetadataResult) {
  showSearchResults.value = false;
  searchQuery.value = "";
  searchResults.value = [];

  if (result.metaType === MetaType.COLUMN) {
    const segments = result.guid.split(";");
    const columnName =
      result.metadata?.type.case === "columnMetadata"
        ? result.metadata.type.value.name
        : segments[segments.length - 1] || "";

    router.push({
      name: "MetadataDetail",
      params: { guid: toGuidPath(segments.slice(0, -1)) },
      query: {
        metaType: String(MetaType.TABLE),
        column: columnName,
      },
    });
    return;
  }

  const segments = result.guid.split(";");
  const query = leafMetaTypes.has(result.metaType)
    ? { metaType: String(result.metaType) }
    : undefined;

  router.push({
    name: "MetadataDetail",
    params: { guid: toGuidPath(segments) },
    query,
  });
}

function clearSearch() {
  searchQuery.value = "";
  searchResults.value = [];
  searchError.value = null;
  showSearchResults.value = false;
}

// Filter management
function selectFilterType(type: "scope" | "type") {
  selectedFilterType.value = type;
  filterSearchQuery.value = "";
  if (type === "scope") {
    scopeStep.value = "instance";
    scopeStepDatabases.value = [];
    scopeStepSchemas.value = [];
    // Ensure instances are loaded for scope picker
    if (instances.value.length === 0) {
      fetchInstances();
    }
  }
}

function backToFilterTypes() {
  selectedFilterType.value = null;
  filterSearchQuery.value = "";
}

function handleScopeBack() {
  filterSearchQuery.value = "";
  if (scopeStep.value === "schema") {
    scopeStep.value = "database";
    scopeFilterSchema.value = null;
    scopeStepSchemas.value = [];
  } else if (scopeStep.value === "database") {
    scopeStep.value = "instance";
    scopeFilterDatabase.value = null;
    scopeStepDatabases.value = [];
  } else {
    backToFilterTypes();
  }
}

async function handleScopeSelectInstance(inst: Instance) {
  const instId = inst.name.replace("instances/", "");
  scopeFilterInstance.value = instId;
  scopeFilterDatabase.value = null;
  scopeFilterSchema.value = null;
  scopeStep.value = "database";
  filterSearchQuery.value = "";

  // Load databases under this instance
  isLoadingScopeOptions.value = true;
  try {
    const response = await listMetadata({
      parentGuid: instId,
      pageSize: 100,
    });
    const dbGroup = response.typesStoredMetadata.find(
      (g) => g.metaType === MetaType.DATABASE
    );
    scopeStepDatabases.value = dbGroup
      ? dbGroup.list.map((item) => getMetadataName(item)).filter(Boolean)
      : [];
  } catch {
    scopeStepDatabases.value = [];
  } finally {
    isLoadingScopeOptions.value = false;
  }
}

async function handleScopeSelectDatabase(db: string) {
  scopeFilterDatabase.value = db;
  scopeFilterSchema.value = null;
  scopeStep.value = "schema";
  filterSearchQuery.value = "";

  // Load schemas under this database
  isLoadingScopeOptions.value = true;
  try {
    const parentGuid = `${scopeFilterInstance.value};${db}`;
    const response = await listMetadata({
      parentGuid,
      pageSize: 100,
    });
    const schemaGroup = response.typesStoredMetadata.find(
      (g) => g.metaType === MetaType.SCHEMA
    );
    scopeStepSchemas.value = schemaGroup
      ? schemaGroup.list.map((item) => getMetadataName(item)).filter(Boolean)
      : [];
  } catch {
    scopeStepSchemas.value = [];
  } finally {
    isLoadingScopeOptions.value = false;
  }
}

function handleScopeSelectSchema(schema: string) {
  scopeFilterSchema.value = schema;
  applyScopeFilter();
}

function applyScopeFilter() {
  showFilterMenu.value = false;
  selectedFilterType.value = null;
  filterSearchQuery.value = "";
  triggerSearchWithCurrentQuery();
}

function removeScopeFilter() {
  scopeFilterInstance.value = null;
  scopeFilterDatabase.value = null;
  scopeFilterSchema.value = null;
  scopeStep.value = "instance";
  scopeStepDatabases.value = [];
  scopeStepSchemas.value = [];
  triggerSearchWithCurrentQuery();
}

function handleSelectTypeFilter(mt: MetaType) {
  typeFilter.value = mt;
  showFilterMenu.value = false;
  selectedFilterType.value = null;
  filterSearchQuery.value = "";
  triggerSearchWithCurrentQuery();
}

function removeTypeFilter() {
  typeFilter.value = null;
  triggerSearchWithCurrentQuery();
}

function triggerSearchWithCurrentQuery() {
  if (searchQuery.value.trim()) {
    if (searchDebounceTimer) clearTimeout(searchDebounceTimer);
    performSearch(searchQuery.value);
  }
}

function handleClickOutside(e: MouseEvent) {
  const target = e.target as HTMLElement;
  if (!target.closest(".relative")) {
    showSearchResults.value = false;
  }
}

watch(searchQuery, (val) => {
  if (searchDebounceTimer) clearTimeout(searchDebounceTimer);
  if (!val.trim()) {
    searchResults.value = [];
    searchError.value = null;
    return;
  }
  searchDebounceTimer = setTimeout(() => performSearch(val), 300);
});

onMounted(() => {
  document.addEventListener("click", handleClickOutside);
});

onBeforeUnmount(() => {
  document.removeEventListener("click", handleClickOutside);
  if (searchDebounceTimer) clearTimeout(searchDebounceTimer);
});
</script>
