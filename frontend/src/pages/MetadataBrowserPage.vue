<template>
  <div class="flex flex-col gap-3">
    <!-- Breadcrumb, title and the list-state search stay pinned, so scrolling a
         long result set never scrolls the user's location away. -->
    <div
      class="sticky -top-4 z-30 -mx-4 -mt-4 flex flex-col gap-2 border-b bg-background/95 px-4 pb-3 pt-4 backdrop-blur sm:-top-6 sm:-mx-6 sm:-mt-6 sm:px-6 sm:pt-6"
    >
      <MetadataBreadcrumb
        :items="breadcrumbItems"
        @navigate="handleNavigate"
      />

      <!-- A leaf detail view is titled by the entity it shows, so the list-page
           title and its global search bar are list-only chrome. Below the root
           the heading names the node being browsed and the description carries
           the path its breadcrumb sits on; the root keeps the browser's name. -->
      <PageHeader
        v-if="showListChrome"
        :eyebrow="isRootPath ? undefined : t('menu.data')"
        :title="pageTitle"
        :description="pageDescription"
      >
        <template
          v-if="levelMetaType !== null"
          #title-extra
        >
          <Badge variant="secondary">
            {{ metaTypeSingularLabel(levelMetaType, t) }}
          </Badge>
        </template>
      </PageHeader>

      <!-- Search Bar -->
      <div
        v-if="showListChrome"
        ref="searchBarRef"
        class="relative"
      >
      <AdvancedSearchBar
        ref="advancedSearchRef"
        :filter-categories="filterCategories"
        :search-placeholder="t('metadataBrowser.searchPlaceholder')"
        @update:filters="handleFiltersUpdate"
      />

      <!-- Search Results Dropdown -->
      <div
        v-if="showSearchResults && searchQuery.trim()"
        class="absolute z-50 top-full mt-1 w-full rounded-md border bg-popover shadow-lg max-h-96 overflow-auto"
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
        <EmptyState
          v-else-if="searchResults.length === 0"
          compact
          :icon="SearchX"
          :title="t('metadataBrowser.noSearchResults')"
        >
          <Button
            variant="outline"
            size="sm"
            @click="clearSearch"
          >
            {{ t("metadataBrowser.clearSearch") }}
          </Button>
        </EmptyState>
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
              {{ metaTypeLabel(result.metaType, t) }}
            </Badge>
            <div class="min-w-0 flex-1">
              <div class="font-medium truncate">
                {{ result.name }}
              </div>
              <div class="text-xs text-muted-foreground truncate">
                {{ formatGuidLabel(result.guid) }}
              </div>
            </div>
          </button>
        </div>
      </div>
      </div>
    </div>

    <Card>
      <div
        v-if="isLoading"
        class="p-8 flex justify-center"
      >
        <AppLoading />
      </div>

      <div
        v-else-if="error"
        class="p-4"
      >
        <!-- The backend message is an internal resource path, so it is the
             disclosure's content: the reader gets a localized explanation and
             a way out, the developer keeps the exact string. -->
        <EmptyState
          :icon="AlertTriangle"
          :title="errorTitle"
          :description="errorDescription"
        >
          <Button
            v-if="!isRootPath"
            variant="outline"
            size="sm"
            @click="goToParent"
          >
            <ArrowLeft class="mr-1 h-3.5 w-3.5" />
            {{ t("metadataBrowser.backToParent") }}
          </Button>
          <Button
            variant="outline"
            size="sm"
            @click="reload"
          >
            <RefreshCw class="mr-1 h-3.5 w-3.5" />
            {{ t("common.refresh") }}
          </Button>
        </EmptyState>

        <details
          class="mx-auto mt-1 flex max-w-xl flex-col items-center [&[open]>summary>svg]:rotate-180"
        >
          <summary
            class="inline-flex cursor-pointer list-none items-center gap-1 rounded-md border border-border/70 bg-background px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <ChevronDown class="size-3 transition-transform" />
            {{ t("metadataBrowser.errorDetails") }}
          </summary>
          <pre
            class="mt-2 w-full whitespace-pre-wrap break-words rounded-md bg-muted px-3 py-2 text-left text-xs text-muted-foreground"
          >{{ error }}</pre>
        </details>
      </div>

      <div v-else-if="isRootPath">
        <!-- No card title here. It said "Database Instances" under an h1 that
             says "Metadata Browser" — the same list under a second name — and
             no deeper level of this hierarchy has one either. The table's own
             column headers say what is listed. -->
        <InstanceList
          :instances="instances"
          :is-loading="isLoadingInstances"
          @select="handleSelectInstance"
          @refresh="reload"
        />
      </div>

      <div v-else>
        <template v-if="leaf?.metaType === MetaType.TABLE">
          <CardContent class="p-0">
            <TableMetadataDetail
              :table="leaf.table"
              :instance-engine="currentInstanceEngine"
              :guid="currentGuid"
              :selected-column-name="selectedColumnName"
            />
          </CardContent>
        </template>

        <template v-else-if="leaf?.metaType === MetaType.EXTERNAL_TABLE">
          <CardContent class="p-0">
            <ExternalTableMetadataDetail
              :table="leaf.table"
              :guid="currentGuid"
            />
          </CardContent>
        </template>

        <template v-else-if="leaf?.metaType === MetaType.VIEW">
          <CardContent class="p-0">
            <ViewMetadataDetail
              :view="leaf.view"
              :guid="currentGuid"
            />
          </CardContent>
        </template>

        <template v-else-if="leaf?.metaType === MetaType.MATERIALIZED_VIEW">
          <CardContent class="p-0">
            <MaterializedViewMetadataDetail
              :view="leaf.view"
              :guid="currentGuid"
            />
          </CardContent>
        </template>

        <template v-else-if="leaf?.metaType === MetaType.FUNCTION">
          <CardContent class="p-0">
            <FunctionMetadataDetail :fn="leaf.fn" />
          </CardContent>
        </template>

        <template v-else-if="leaf?.metaType === MetaType.PROCEDURE">
          <CardContent class="p-0">
            <ProcedureMetadataDetail :proc="leaf.proc" />
          </CardContent>
        </template>

        <template v-else-if="leaf?.metaType === MetaType.SEQUENCE">
          <CardContent class="p-0">
            <SequenceMetadataDetail :seq="leaf.seq" />
          </CardContent>
        </template>

        <template v-else-if="leaf?.metaType === MetaType.MANUAL_SQL">
          <CardContent class="p-0">
            <ManualSQLMetadataDetail
              :manual-sql="leaf.manualSql"
              :guid="currentGuid"
            />
          </CardContent>
        </template>

        <template v-else-if="leaf?.metaType === MetaType.APPLICATION_SQL">
          <CardContent class="p-0">
            <ApplicationSQLMetadataDetail
              :application-sql="leaf.applicationSql"
              :guid="currentGuid"
            />
          </CardContent>
        </template>

        <template v-else-if="isExternalDatasetDetailView && externalDatasetDetail">
          <CardContent class="p-0">
            <ExternalDatasetMetadataDetail
              :guid="currentGuid"
              :name="externalDatasetDetail.name"
              :namespace="externalDatasetDetail.namespace"
              :dataset-type="externalDatasetDetail.datasetType"
            />
          </CardContent>
        </template>

        <template v-else>
          <!-- The tab strip is a control, not a titled section, so it does not
               need the CardHeader's default `p-6` around a single row. -->
          <CardHeader class="border-b px-4 py-2">
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
              :items="activeGroup.items"
              @select="handleSelectMetadata"
              @refresh="reload"
            />
            <EmptyState
              v-else
              :icon="SearchX"
              :title="t('metadataBrowser.empty')"
            />
          </CardContent>

          <div
            v-if="activeMetaType && hasMoreForActive"
            class="p-4 border-t"
          >
            <MetadataPagination
              :has-next="hasMoreForActive"
              :is-loading="isLoadingMore"
              @load-more="handleLoadMore"
            />
          </div>
        </template>
      </div>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { Code } from "@connectrpc/connect";
import { onClickOutside } from "@vueuse/core";
import {
  AlertTriangle,
  ArrowLeft,
  ChevronDown,
  RefreshCw,
  SearchX,
} from "lucide-vue-next";

import {
  computed,
  defineAsyncComponent,
  onBeforeUnmount,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { getMetadata, listMetadata } from "@/api/database";
import { getInstance } from "@/api/instance";
import AppLoading from "@/components/common/AppLoading.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import InstanceList from "@/components/metadata/InstanceList.vue";
import MetadataBreadcrumb from "@/components/metadata/MetadataBreadcrumb.vue";
import MetadataList from "@/components/metadata/MetadataList.vue";
import MetadataPagination from "@/components/metadata/MetadataPagination.vue";
import MetadataTabNav from "@/components/metadata/MetadataTabNav.vue";

// Detail components lazy-loaded — only needed in leaf views, not on the root page.
const ExternalDatasetMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/ExternalDatasetMetadataDetail.vue")
);
const ExternalTableMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/ExternalTableMetadataDetail.vue")
);
const FunctionMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/FunctionMetadataDetail.vue")
);
const ManualSQLMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/ManualSQLMetadataDetail.vue")
);
const ApplicationSQLMetadataDetail = defineAsyncComponent(
  () => import("@/components/metadata/ApplicationSQLMetadataDetail.vue")
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

import AdvancedSearchBar, {
  type ActiveFilter,
  type FilterCategory,
  type FilterOption,
} from "@/components/common/AdvancedSearchBar.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { useErrorMessage } from "@/composables/useErrorHandler";
import {
  type MetadataChild,
  useMetadataChildren,
} from "@/composables/useMetadataChildren";
import { useInstanceStore } from "@/store/modules/instance";
import { Engine } from "@/types/proto-es/v1/common_pb";
import {
  type ApplicationSQLMetadata,
  type ExternalTableMetadata,
  type FunctionMetadata,
  type ManualSQLMetadata,
  type MaterializedViewMetadata,
  MetaType,
  type ProcedureMetadata,
  type SequenceMetadata,
  type StoredMetadata,
  type TableMetadata,
  type ViewMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";
import { errorCode } from "@/utils/error";
import { guidSegmentsToRouteParams, routeParamToGuid } from "@/utils/guid";
import { lineageAssetView } from "@/utils/lineageAsset";
import {
  metaTypeLabel,
  metaTypeSingularLabel,
  parseMetaType,
} from "@/utils/metaType";

const { t } = useI18n();
const { formatError } = useErrorMessage();
const route = useRoute();
const router = useRouter();
const instanceStore = useInstanceStore();

// The list and the search are separate levels: a search replaces its own rows,
// never the list the reader returns to when the dropdown closes.
const level = useMetadataChildren({ pageSize: 20 });
const search = useMetadataChildren({ pageSize: 20 });

const isLoadingInstances = computed(() => instanceStore.loading);
const leafLoading = ref(false);
const instancesError = ref<unknown>(undefined);
const leafError = ref<unknown>(undefined);
const isLoading = computed(() => leafLoading.value || level.loading.value);
const isLoadingMore = computed(() => level.loadingMore.value);
const activeError = computed(
  () => leafError.value ?? level.error.value ?? instancesError.value
);
const error = computed(() =>
  activeError.value
    ? formatError(activeError.value, "metadataBrowser.fetchError")
    : null
);
// A NotFound is the one error that means "this object is gone"; anything else
// (network, permission, server) is a load failure the reader should retry.
const notFound = computed(() => errorCode(activeError.value) === Code.NotFound);

// Search state
const searchQuery = ref("");
const searchBarRef = ref<HTMLElement | null>(null);
const advancedSearchRef = ref<InstanceType<typeof AdvancedSearchBar> | null>(
  null
);
const searchResults = computed(() => search.items.value);
const isSearching = computed(() => search.loading.value);
const searchError = computed(() =>
  search.error.value ? t("metadataBrowser.searchError") : null
);
const showSearchResults = ref(false);
let searchDebounceTimer: ReturnType<typeof setTimeout> | null = null;

// Filter state, owned by AdvancedSearchBar and read back from its pills.
const typeFilter = ref<MetaType | null>(null);
const scopeFilterGuidPrefix = ref<string | undefined>(undefined);

interface ScopeLevel {
  key: string;
  label: string;
  options: (picked: Record<string, string>) => Promise<FilterOption[]>;
}

/** The children of `parentGuid` whose type is `metaType`, as filter options. */
async function listChildNames(
  parentGuid: string,
  metaType: MetaType
): Promise<FilterOption[]> {
  try {
    const response = await listMetadata({ parentGuid, pageSize: 100 });
    const group = response.typesStoredMetadata.find(
      (candidate) => candidate.metaType === metaType
    );
    return (group?.list ?? [])
      .map((item) => getMetadataName(item))
      .filter(Boolean)
      .map((name) => ({ value: name, label: name }));
  } catch {
    return [];
  }
}

const scopeLevels = computed<ScopeLevel[]>(() => [
  {
    key: "instance",
    label: t("metadataBrowser.scopeStepInstance"),
    options: async () => {
      if (instances.value.length === 0) {
        await fetchInstances();
      }
      return instances.value.map((instance) => ({
        value: instance.name.replace("instances/", ""),
        label: instance.title || instance.name,
      }));
    },
  },
  {
    key: "database",
    label: t("metadataBrowser.scopeStepDatabase"),
    options: async (picked) =>
      await listChildNames(picked.instance ?? "", MetaType.DATABASE),
  },
  {
    key: "schema",
    label: t("metadataBrowser.scopeStepSchema"),
    options: async (picked) =>
      await listChildNames(
        `${picked.instance ?? ""};${picked.database ?? ""}`,
        MetaType.SCHEMA
      ),
  },
]);

const filterCategories = computed<FilterCategory[]>(() => [
  {
    type: "scope",
    label: t("metadataBrowser.scopeFilter"),
    icon: "📍",
    kind: "cascade",
    levels: scopeLevels.value,
  },
  {
    type: "type",
    label: t("metadataBrowser.typeFilter"),
    icon: "🏷️",
    options: searchableMetaTypes.map((metaType) => ({
      value: String(metaType),
      label: metaTypeLabel(metaType, t),
    })),
  },
]);

function handleFiltersUpdate(filters: ActiveFilter[]) {
  const valueOf = (type: string) =>
    filters.find((filter) => filter.type === type)?.value ?? "";
  // The search text drives its own debounced lookup; a scope or type change has
  // to re-run the search with the new prefix.
  searchQuery.value = valueOf("name");

  const nextScope = valueOf("scope") || undefined;
  const nextType = parseMetaType(valueOf("type"));
  if (
    nextScope !== scopeFilterGuidPrefix.value ||
    nextType !== typeFilter.value
  ) {
    scopeFilterGuidPrefix.value = nextScope;
    typeFilter.value = nextType;
    triggerSearchWithCurrentQuery();
  }
}

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
  MetaType.APPLICATION_SQL,
];

const instances = computed(() => instanceStore.active);

/** One tab's rows, rebuilt from the level's children. */
interface MetadataGroup {
  metaType: MetaType;
  items: StoredMetadata[];
}

// Children arrive in the order the backend returned the groups; the first
// appearance of a type fixes its position in the tab strip.
const metadataGroups = computed<MetadataGroup[]>(() => {
  const byType = new Map<MetaType, StoredMetadata[]>();
  for (const child of level.items.value) {
    if (!child.metadata) continue;
    const items = byType.get(child.metaType) ?? [];
    items.push(child.metadata);
    byType.set(child.metaType, items);
  }
  return [...byType].map(([metaType, items]) => ({ metaType, items }));
});

const activeMetaType = ref<MetaType | null>(null);
const hasMoreForActive = computed(
  () => activeMetaType.value != null && level.hasNextFor(activeMetaType.value)
);

const currentInstanceEngine = ref<Engine | null>(null);

/**
 * The object a leaf route resolved to. The union is discriminated by `metaType`
 * so the template can hand each detail component a typed prop without casts, and
 * `metaType` decides which loader and which component a route needs.
 */
type Leaf =
  | { metaType: MetaType.TABLE; table: TableMetadata }
  | { metaType: MetaType.EXTERNAL_TABLE; table: ExternalTableMetadata }
  | { metaType: MetaType.VIEW; view: ViewMetadata }
  | { metaType: MetaType.MATERIALIZED_VIEW; view: MaterializedViewMetadata }
  | { metaType: MetaType.FUNCTION; fn: FunctionMetadata }
  | { metaType: MetaType.PROCEDURE; proc: ProcedureMetadata }
  | { metaType: MetaType.SEQUENCE; seq: SequenceMetadata }
  | { metaType: MetaType.MANUAL_SQL; manualSql: ManualSQLMetadata }
  | {
      metaType: MetaType.APPLICATION_SQL;
      applicationSql: ApplicationSQLMetadata;
    };

const leaf = ref<Leaf | null>(null);

type LeafType = Leaf["metaType"];

/** Each metadata oneof case name paired with the message it carries. */
interface LeafValueMap {
  tableMetadata: TableMetadata;
  externalTableMetadata: ExternalTableMetadata;
  viewMetadata: ViewMetadata;
  materializedViewMetadata: MaterializedViewMetadata;
  functionMetadata: FunctionMetadata;
  procedureMetadata: ProcedureMetadata;
  sequenceMetadata: SequenceMetadata;
  manualSqlMetadata: ManualSQLMetadata;
  applicationSqlMetadata: ApplicationSQLMetadata;
}

/**
 * One metadata read, narrowed to the type the route named. The single cast is
 * what couples a oneof case name to its message; every loader is typed by it.
 */
async function getLeafValue<C extends keyof LeafValueMap>(
  guid: string,
  metaType: MetaType,
  expected: C
): Promise<LeafValueMap[C]> {
  const detail = await getMetadata({ guid, metaType });
  const type = detail.metadata?.type;
  if (type?.case !== expected) {
    throw new Error(`expected ${expected} for ${guid}`);
  }
  return type.value as LeafValueMap[C];
}

/** How each leaf type is fetched. A route without a hint never loads a leaf. */
const LEAF_LOADERS: Record<LeafType, (guid: string) => Promise<Leaf>> = {
  [MetaType.TABLE]: async (guid) => ({
    metaType: MetaType.TABLE,
    table: await getLeafValue(guid, MetaType.TABLE, "tableMetadata"),
  }),
  [MetaType.EXTERNAL_TABLE]: async (guid) => ({
    metaType: MetaType.EXTERNAL_TABLE,
    table: await getLeafValue(
      guid,
      MetaType.EXTERNAL_TABLE,
      "externalTableMetadata"
    ),
  }),
  [MetaType.VIEW]: async (guid) => ({
    metaType: MetaType.VIEW,
    view: await getLeafValue(guid, MetaType.VIEW, "viewMetadata"),
  }),
  [MetaType.MATERIALIZED_VIEW]: async (guid) => ({
    metaType: MetaType.MATERIALIZED_VIEW,
    view: await getLeafValue(
      guid,
      MetaType.MATERIALIZED_VIEW,
      "materializedViewMetadata"
    ),
  }),
  [MetaType.FUNCTION]: async (guid) => ({
    metaType: MetaType.FUNCTION,
    fn: await getLeafValue(guid, MetaType.FUNCTION, "functionMetadata"),
  }),
  [MetaType.PROCEDURE]: async (guid) => ({
    metaType: MetaType.PROCEDURE,
    proc: await getLeafValue(guid, MetaType.PROCEDURE, "procedureMetadata"),
  }),
  [MetaType.SEQUENCE]: async (guid) => ({
    metaType: MetaType.SEQUENCE,
    seq: await getLeafValue(guid, MetaType.SEQUENCE, "sequenceMetadata"),
  }),
  [MetaType.MANUAL_SQL]: async (guid) => ({
    metaType: MetaType.MANUAL_SQL,
    manualSql: await getLeafValue(
      guid,
      MetaType.MANUAL_SQL,
      "manualSqlMetadata"
    ),
  }),
  [MetaType.APPLICATION_SQL]: async (guid) => ({
    metaType: MetaType.APPLICATION_SQL,
    applicationSql: await getLeafValue(
      guid,
      MetaType.APPLICATION_SQL,
      "applicationSqlMetadata"
    ),
  }),
};

/** The name a SQL object is known by; other leaves are already named after their segment. */
function leafDisplayName(value: Leaf): string {
  if (value.metaType === MetaType.MANUAL_SQL) {
    return value.manualSql.title || value.manualSql.name;
  }
  if (value.metaType === MetaType.APPLICATION_SQL) {
    return value.applicationSql.name;
  }
  return "";
}

function isLeafType(metaType: MetaType | null): metaType is LeafType {
  return metaType != null && metaType in LEAF_LOADERS;
}

type ExternalDatasetDetail = {
  name: string;
  namespace: string;
  datasetType: string;
};

const requestedLeafMetaType = computed(() =>
  parseMetaType(route.query.metaType)
);

const selectedColumnName = computed(() => getQueryString("column"));

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

// A leaf detail view titles itself from the entity it resolved, so the page
// drops the list title and the global search bar. Checked against the resolved
// leaves rather than the requested meta type, because the same `metaType` value
// drives both a list ("show the Tables tab") and a leaf ("show this table").
// `isLoading` is part of the condition so the chrome never flashes the list
// header while a detail request is still in flight.
const isLeafDetailView = computed(() => {
  return leaf.value != null || externalDatasetDetail.value != null;
});

const showListChrome = computed(
  () => !isLeafDetailView.value && !isLoading.value
);

const currentGuid = computed(() => routeParamToGuid(route.params.guid));

const isRootPath = computed(() => !currentGuid.value);

/** The GUID segments without the trailing empties that mean "no level". */
const nodeSegments = computed(() => {
  const segments = [...guidSegments.value];
  while (segments.length > 0 && segments[segments.length - 1] === "") {
    segments.pop();
  }
  return segments;
});

/** Every known instance id -> title, for humanizing GUIDs. */
const instanceTitles = computed(() => {
  const titles = new Map<string, string>();
  for (const instance of instanceStore.instances) {
    const id = instance.name.replace("instances/", "");
    titles.set(id, instance.title || id);
  }
  return titles;
});

/**
 * The page heading. The root names the browser; every level below it names the
 * object being browsed, which is what the breadcrumb's last crumb already says.
 */
const pageTitle = computed(() => {
  if (isRootPath.value) {
    return t("metadataBrowser.title");
  }
  const segments = nodeSegments.value;
  const name = segments[segments.length - 1] ?? "";
  if (!name) {
    return t("metadataBrowser.title");
  }
  return segments.length === 1 ? instanceStore.titleOf(name) : name;
});

/**
 * The ancestor path under the heading. The current node is the heading, so the
 * description starts one level up and stops at the instance.
 */
const pageDescription = computed(() => {
  const segments = nodeSegments.value;
  if (segments.length <= 1) {
    return "";
  }
  return segments
    .slice(0, -1)
    .map((segment, index) =>
      index === 0 ? instanceStore.titleOf(segment) : segment
    )
    .filter(Boolean)
    .join(" \u203A ");
});

/**
 * The type of the node this page shows below the root, used for the singular
 * badge next to its title. A MySQL-family instance has no schema segment in the
 * URL, so the third segment is already an object there and the badge is left off
 * rather than mislabelling a table as a schema.
 */
const levelMetaType = computed<MetaType | null>(() => {
  if (isRootPath.value) return null;
  const depth = nodeSegments.value.length;
  if (depth === 1) return MetaType.INSTANCE;
  if (depth === 2) return MetaType.DATABASE;
  if (depth === 3 && !isMySQLInstance.value) return MetaType.SCHEMA;
  return null;
});

const errorTitle = computed(() => {
  if (isRootPath.value) {
    return t("metadataBrowser.fetchError");
  }
  return notFound.value
    ? t("metadataBrowser.notFoundTitle")
    : t("metadataBrowser.fetchError");
});

const errorDescription = computed(() => {
  if (isRootPath.value || !notFound.value) {
    return "";
  }
  return t("metadataBrowser.notFoundDescription");
});

/** `postgres-warehouse · warehouse.ods.orders`, never a raw `;`-joined GUID. */
function formatGuidLabel(guid: string): string {
  const view = lineageAssetView(guid, instanceTitles.value);
  return view.qualifier
    ? `${view.scopeLabel} \u00B7 ${view.qualifier}.${view.name}`
    : `${view.scopeLabel} \u00B7 ${view.name}`;
}

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

  // A SQL object's last segment is generated (`__manual_sql__/<id>`), so the crumb reads
  // the object's name once the leaf has loaded rather than the identifier.
  const leafName = leaf.value ? leafDisplayName(leaf.value) : "";
  if (leafName && items.length > 0) {
    items[items.length - 1].label = leafName;
  }

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
    currentInstanceEngine.value === Engine.TIDB ||
    currentInstanceEngine.value === Engine.STARROCKS ||
    currentInstanceEngine.value === Engine.DORIS
  );
});

const activeGroup = computed(() => {
  if (!activeMetaType.value) return null;
  return (
    metadataGroups.value.find(
      (group) => group.metaType === activeMetaType.value
    ) ?? null
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
  instancesError.value = undefined;
  try {
    await instanceStore.fetch(true);
  } catch (e) {
    instancesError.value = e;
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

/** Clears every trace of the previously shown view before a new fetch. */
function resetViewState() {
  leaf.value = null;
  leafError.value = undefined;
  instancesError.value = undefined;
  level.reset();
  activeMetaType.value = null;
}

/**
 * Loads the leaf object the route named with `?metaType`. A route without that
 * hint is a list route by definition — the app's own links always carry the hint,
 * so a leaf is never guessed by probing every type (that cost up to seven serial
 * round trips).
 */
async function fetchLeafDetail(metaType: LeafType) {
  resetViewState();
  leafLoading.value = true;
  try {
    await fetchCurrentInstanceEngineIfNeeded();
    leaf.value = await LEAF_LOADERS[metaType](currentGuid.value);
  } catch (e) {
    leafError.value = e;
  } finally {
    leafLoading.value = false;
  }
}

async function fetchMetadataGroups() {
  const requestedLeaf = requestedLeafMetaType.value;
  if (isLeafType(requestedLeaf)) {
    await fetchLeafDetail(requestedLeaf);
    return;
  }

  resetViewState();
  await fetchCurrentInstanceEngineIfNeeded();
  const children = await level.load(getEffectiveParentGuidForListing(), []);
  if (children && children.length > 0) {
    activeMetaType.value = children[0].metaType;
  }
}

async function handleLoadMore() {
  if (!activeMetaType.value) return;
  await level.loadMore(activeMetaType.value);
}

function handleNavigate(guidIndex: number) {
  if (guidIndex < 0) {
    router.push({ name: "MetadataBrowser" });
    return;
  }

  const segments = guidSegments.value.slice(0, guidIndex + 1);
  router.push({
    name: "MetadataDetail",
    params: { guid: guidSegmentsToRouteParams(segments) },
  });
}

function handleSelectInstance(instance: Instance) {
  const instanceId = instance.name.replace("instances/", "");
  currentInstanceEngine.value = instance.engine;
  router.push({
    name: "MetadataDetail",
    params: { guid: guidSegmentsToRouteParams([instanceId]) },
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
    case "sequenceMetadata":
      return item.type.value.name;
    case "columnMetadata":
      return item.type.value.name;
    case "manualSqlMetadata":
      return item.type.value.title || item.type.value.name;
    case "applicationSqlMetadata":
      return item.type.value.name;
    default:
      return "";
  }
}

function handleSelectMetadata(item: StoredMetadata, metaType: MetaType) {
  // Both SQL object types end in a generated segment (`__manual_sql__/<id>`,
  // `__application_sql__/<id>`), so the route cannot be derived from the name the way a
  // table's can: it is the scope being browsed plus that segment.
  if (item.type.case === "applicationSqlMetadata") {
    const statement = item.type.value;
    router.push({
      name: "MetadataDetail",
      params: {
        guid: guidSegmentsToRouteParams([
          guidSegments.value[0] || "",
          guidSegments.value[1] || "",
          statement.schemaName || "",
          `__application_sql__/${statement.applicationSqlId}`,
        ]),
      },
      query: { metaType: String(MetaType.APPLICATION_SQL) },
    });
    return;
  }

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
      params: { guid: guidSegmentsToRouteParams(segments) },
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
    metaType === MetaType.EXTERNAL_TABLE ||
    metaType === MetaType.VIEW ||
    metaType === MetaType.MATERIALIZED_VIEW ||
    metaType === MetaType.FUNCTION ||
    metaType === MetaType.PROCEDURE ||
    metaType === MetaType.SEQUENCE ||
    metaType === MetaType.MANUAL_SQL ||
    metaType === MetaType.APPLICATION_SQL
      ? { metaType: String(metaType) }
      : undefined;

  router.push({
    name: "MetadataDetail",
    params: { guid: guidSegmentsToRouteParams(newSegments) },
    query,
  });
}

function handleSelectTab(metaType: MetaType) {
  activeMetaType.value = metaType;
}

/** Loads whatever the current route names: instance list, group lists or a leaf. */
async function loadRouteState() {
  // Instance titles humanize the heading and every GUID the page prints; the
  // store caches them, so a deep link pays for this at most once.
  void instanceStore.ensureLoaded().catch(() => {});
  currentInstanceEngine.value = null;

  if (isRootPath.value) {
    await fetchInstances();
  } else if (isExternalDatasetDetailView.value) {
    resetViewState();
  } else {
    await fetchMetadataGroups();
  }
}

/** Retry after a failed load without leaving the page. */
async function reload() {
  await loadRouteState();
}

/** Back to the parent node; the root when the level above is the root. */
function goToParent() {
  const segments = nodeSegments.value;
  if (segments.length <= 1) {
    router.push({ name: "MetadataBrowser" });
    return;
  }
  handleNavigate(segments.length - 2);
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
    await loadRouteState();
  },
  { immediate: true }
);

// Search logic. The dropdown is a level of its own, so the composable owns the
// cancellation: a slow answer for a term or scope the reader has left can never
// overwrite the current one.
async function performSearch(query: string) {
  if (!query.trim()) {
    search.reset();
    return;
  }
  await search.search(
    scopeFilterGuidPrefix.value ?? "",
    typeFilter.value == null ? [] : [typeFilter.value],
    query
  );
}

const leafMetaTypes = new Set<MetaType>([
  MetaType.TABLE,
  MetaType.EXTERNAL_TABLE,
  MetaType.VIEW,
  MetaType.MATERIALIZED_VIEW,
  MetaType.FUNCTION,
  MetaType.PROCEDURE,
  MetaType.SEQUENCE,
  MetaType.MANUAL_SQL,
  MetaType.APPLICATION_SQL,
]);

function handleSelectSearchResult(result: MetadataChild) {
  showSearchResults.value = false;
  searchQuery.value = "";
  search.reset();

  if (result.metaType === MetaType.COLUMN) {
    const segments = result.guid.split(";");
    const columnName =
      result.metadata?.type.case === "columnMetadata"
        ? result.metadata.type.value.name
        : result.name || segments[segments.length - 1] || "";

    router.push({
      name: "MetadataDetail",
      params: { guid: guidSegmentsToRouteParams(segments.slice(0, -1)) },
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
    params: { guid: guidSegmentsToRouteParams(segments) },
    query,
  });
}

/** Drops the query and the dropdown, the way Escape or a clear button should.
 *  The bar owns the visible search box and its pills, so it is cleared too —
 *  otherwise the field would still show the text the button just dismissed. */
function clearSearch() {
  if (searchDebounceTimer) clearTimeout(searchDebounceTimer);
  advancedSearchRef.value?.clear();
  searchQuery.value = "";
  search.reset();
  showSearchResults.value = false;
}

function triggerSearchWithCurrentQuery() {
  if (!searchQuery.value.trim()) {
    return;
  }
  if (searchDebounceTimer) clearTimeout(searchDebounceTimer);
  // A filter change re-runs the search, so its results open again.
  showSearchResults.value = true;
  void performSearch(searchQuery.value);
}

watch(searchQuery, (val) => {
  if (searchDebounceTimer) clearTimeout(searchDebounceTimer);
  showSearchResults.value = Boolean(val.trim());
  if (!val.trim()) {
    search.reset();
    return;
  }
  searchDebounceTimer = setTimeout(() => performSearch(val), 300);
});

onClickOutside(searchBarRef, () => {
  showSearchResults.value = false;
});

onBeforeUnmount(() => {
  if (searchDebounceTimer) clearTimeout(searchDebounceTimer);
});
</script>
