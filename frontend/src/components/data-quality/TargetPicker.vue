<template>
  <Dialog
    :open="open"
    @update:open="(value: boolean) => emit('update:open', value)"
  >
    <DialogContent class="max-w-2xl">
      <DialogHeader>
        <DialogTitle>
          {{ title }}
        </DialogTitle>
        <DialogDescription>
          {{ description }}
        </DialogDescription>
      </DialogHeader>

      <div class="space-y-3">
        <div class="flex flex-wrap items-center gap-1">
          <Button
            v-if="step !== 'instance'"
            variant="ghost"
            size="icon"
            class="h-8 w-8"
            :aria-label="t('common.back')"
            :title="t('common.back')"
            @click="goBack"
          >
            <ArrowLeft class="h-4 w-4" />
          </Button>
          <Badge
            v-for="crumb in breadcrumb"
            :key="crumb.key"
            :variant="crumb.current ? 'default' : 'secondary'"
          >
            {{ crumb.label }}
          </Badge>
        </div>

        <Input
          v-model="filter"
          :placeholder="searchPlaceholder"
          :aria-label="searchPlaceholder"
          @update:model-value="onFilterChanged"
        />

        <div
          v-if="displayError"
          class="flex items-center justify-between gap-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive"
        >
          <span class="min-w-0 flex-1">{{ displayError }}</span>
          <Button
            variant="outline"
            size="sm"
            @click="retryCurrent"
          >
            {{ t("common.retry") }}
          </Button>
        </div>

        <div
          v-else-if="displayLoading"
          class="flex min-h-40 items-center justify-center"
        >
          <AppLoading />
        </div>

        <div
          v-else
          class="max-h-80 overflow-y-auto rounded-md border"
        >
          <div
            v-if="visibleItems.length === 0"
            class="p-6 text-center text-sm text-muted-foreground"
          >
            {{ emptyLabel }}
          </div>
          <button
            v-for="item in visibleItems"
            :key="item.guid"
            type="button"
            class="flex w-full items-center justify-between gap-3 border-b px-3 py-2.5 text-left text-sm last:border-b-0 hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            :class="{ 'bg-accent/40': isMultiSelect && isSelected(item) }"
            :aria-pressed="
              isMultiSelect && step === 'table' ? isSelected(item) : undefined
            "
            @click="choose(item)"
          >
            <span class="min-w-0 flex-1 truncate font-medium">
              {{ item.name }}
            </span>
            <span class="shrink-0 text-xs text-muted-foreground">
              {{ metaTypeSingularLabel(item.metaType, t) }}
            </span>
            <!-- Multi-select marks table rows; the other levels still drill in. -->
            <span
              v-if="isMultiSelect && step === 'table'"
              class="flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border border-input"
              aria-hidden="true"
            >
              <Check
                v-if="isSelected(item)"
                class="h-3 w-3"
              />
            </span>
            <ChevronRight
              v-else
              class="h-4 w-4 shrink-0 text-muted-foreground"
            />
          </button>

          <!-- A level can run past one page; the footer asks for the rest
               instead of hiding the tail behind a guessed page size. -->
          <div
            v-if="showFooter"
            class="border-t p-2"
          >
            <div
              v-if="moreError"
              class="flex items-center justify-between gap-2 text-sm text-destructive"
            >
              <span class="min-w-0 flex-1 truncate">{{ moreError }}</span>
              <Button
                variant="outline"
                size="sm"
                @click="loadMore"
              >
                {{ t("common.retry") }}
              </Button>
            </div>
            <div
              v-else
              class="flex justify-center"
            >
              <Button
                variant="secondary"
                size="sm"
                :disabled="loadingMore"
                @click="loadMore"
              >
                <Loader2
                  v-if="loadingMore"
                  class="mr-2 h-4 w-4 animate-spin"
                />
                {{ t("dataQuality.target.loadMore") }}
              </Button>
            </div>
          </div>
        </div>
      </div>

      <DialogFooter class="gap-2">
        <Button
          variant="outline"
          @click="emit('update:open', false)"
        >
          {{ t("common.cancel") }}
        </Button>
        <!-- Multi-select applies the accumulated tables in one go; the selection
             survives drilling into another database or schema. -->
        <Button
          v-if="isMultiSelect"
          :disabled="selected.length === 0"
          @click="addSelected"
        >
          {{ t("dataQuality.target.addSelected", { count: selected.length }) }}
        </Button>
        <!-- In scope mode the picked path itself is the answer: the reader can
             stop at an instance, database, schema or drill all the way into a
             table. A table row applies directly, like it selects a target. -->
        <Button
          v-if="isScopeMode"
          :disabled="!currentScope.instance"
          @click="applyCurrentScope"
        >
          {{ t("dataQuality.targetScope.apply") }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { ArrowLeft, Check, ChevronRight, Loader2 } from "lucide-vue-next";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import AppLoading from "@/components/common/AppLoading.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useErrorMessage } from "@/composables/useErrorHandler";
import {
  type MetadataChild,
  useMetadataChildren,
} from "@/composables/useMetadataChildren";
import { useInstanceStore } from "@/store/modules/instance";
import type { Engine } from "@/types/proto-es/v1/common_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { QualityTargetScope } from "@/utils/dataQuality";
import {
  TARGET_META_TYPES,
  type TargetSelection,
} from "@/utils/dataQualityForm";
import { isSchemaLessEngine } from "@/utils/engine";
import { metaTypeSingularLabel } from "@/utils/metaType";

/**
 * The target browser: instance → database → schema → table, using the metadata
 * registry the rest of the product reads. A table (or view) is the target of a
 * rule; the column it checks is chosen in the rule form, because that is part of
 * the typed config rather than of the target identity.
 *
 * Every level below the instance can be far larger than one page in a real
 * estate, so the browser follows page tokens instead of reading a fixed first
 * page, and the search box queries the server scoped to the level being browsed
 * instead of filtering the rows already fetched. Instances are the exception:
 * the app-wide store already walks every page and the list is small enough to
 * filter locally.
 *
 * `mode="scope"` reuses the same browser as the rules list's target-scope
 * filter: instead of only a table, the picked path itself is the answer, so the
 * footer applies whatever level the reader has reached (instance, database,
 * schema) and a table row applies that table.
 *
 * The list is a dialog so it keeps the form's scroll position, and so the
 * layout audit's nested-scroller rule (which measures the page) never sees it.
 */

const props = withDefaults(
  defineProps<{
    open: boolean;
    /** `scope` applies the picked path prefix instead of requiring a table. */
    mode?: "target" | "scope";
    /**
     * In target mode, marks table rows instead of choosing immediately and
     * accumulates a selection that survives drilling into another database or
     * schema. Off by default: every other caller keeps the one-click behavior.
     */
    multiple?: boolean;
  }>(),
  { mode: "target", multiple: false }
);

const emit = defineEmits<{
  "update:open": [value: boolean];
  select: [selection: TargetSelection];
  selectMany: [selections: TargetSelection[]];
  selectScope: [scope: QualityTargetScope];
}>();

const SEARCH_DELAY_MS = 300;
const PAGE_SIZE = 50;
const INSTANCE_BATCH = 50;

const isScopeMode = computed(() => props.mode === "scope");
const isMultiSelect = computed(() => props.multiple && !isScopeMode.value);
const title = computed(() => {
  if (isScopeMode.value) {
    return t("dataQuality.targetScope.title");
  }
  return isMultiSelect.value
    ? t("dataQuality.target.multiTitle")
    : t("dataQuality.target.title");
});
const description = computed(() => {
  if (isScopeMode.value) {
    return t("dataQuality.targetScope.description");
  }
  return isMultiSelect.value
    ? t("dataQuality.target.multiDescription")
    : t("dataQuality.target.description");
});

type Step = "instance" | "database" | "schema" | "table";

const { t } = useI18n();
const { formatError } = useErrorMessage();
const instanceStore = useInstanceStore();

const {
  items: browseItems,
  loading: browseLoading,
  loadingMore,
  hasNext,
  error: browseError,
  moreError: browseMoreError,
  query: browseQuery,
  load: loadChildren,
  search: searchChildren,
  loadMore: loadMoreChildren,
  reset: resetBrowser,
} = useMetadataChildren({ pageSize: PAGE_SIZE });

const step = ref<Step>("instance");
const instanceId = ref("");
const database = ref("");
const databaseGuid = ref("");
const schema = ref("");
const schemaGuid = ref("");
const filter = ref("");
const instancesLoading = ref(false);
const instancesError = ref("");
const instanceTitle = ref("");
const instanceEngine = ref<Engine>();
/** Tables marked in multi-select mode, in the order they were marked. */
const selected = ref<TargetSelection[]>([]);

let filterTimer: ReturnType<typeof setTimeout> | undefined;

const instances = computed(() => instanceStore.active);
/** Instances are cached in full, so "more" only grows the rendered slice. */
const instanceLimit = ref(INSTANCE_BATCH);

const breadcrumb = computed(() => {
  const crumbs: Array<{ key: string; label: string; current: boolean }> = [
    {
      key: "instance",
      label: instanceTitle.value,
      current: step.value === "instance",
    },
  ];
  if (database.value) {
    crumbs.push({
      key: "database",
      label: database.value,
      current: step.value === "schema",
    });
  }
  if (schema.value) {
    crumbs.push({
      key: "schema",
      label: schema.value,
      current: false,
    });
  }
  // The MySQL family has no schema level, so the last breadcrumb is the table
  // level itself rather than a blank chip.
  if (step.value === "table") {
    crumbs.push({
      key: "table",
      label: t("dataQuality.target.tables"),
      current: true,
    });
  }
  return crumbs;
});

/**
 * The deepest path already chosen while browsing. The level on screen is the one
 * below it, so a reader who applies here narrows to this subtree.
 */
const currentScope = computed<Omit<QualityTargetScope, "label">>(() => ({
  instance: instanceId.value,
  // A database chip exists from the schema step on; the schema from the table
  // step on, and it may legitimately be empty for the MySQL family.
  database:
    step.value === "schema" || step.value === "table" ? database.value : "",
  schema: step.value === "table" ? schema.value : "",
  table: "",
}));

const currentScopeLabel = computed(() =>
  [instanceTitle.value, currentScope.value.database, currentScope.value.schema]
    .filter(Boolean)
    .join(" > ")
);

/** The parent GUID whose children the current step lists. */
function currentParent(): string {
  switch (step.value) {
    case "database":
      return instanceId.value;
    case "schema":
      // The database's own GUID is authoritative; the fallback keeps the
      // name-built path for a registry row that carried no GUID.
      return databaseGuid.value || `${instanceId.value};${database.value}`;
    case "table":
      // The schema's own GUID when there is one; otherwise the MySQL-family
      // database GUID plus the trailing empty-schema separator.
      return schemaGuid.value ? schemaGuid.value : `${databaseGuid.value};`;
    default:
      return "";
  }
}

/** The meta types the current step lists. */
function currentTypes(): MetaType[] {
  switch (step.value) {
    case "database":
      return [MetaType.DATABASE];
    case "schema":
      return [MetaType.SCHEMA];
    case "table":
      return TARGET_META_TYPES;
    default:
      return [];
  }
}

/** The cached instance list, filtered locally by the search box. */
const filteredInstances = computed<MetadataChild[]>(() => {
  const term = filter.value.trim().toLowerCase();
  const list = instances.value.map((instance) => ({
    guid: instance.name.replace("instances/", ""),
    name: instance.title || instance.name.replace("instances/", ""),
    metaType: MetaType.INSTANCE,
  }));
  return term
    ? list.filter((item) => item.name.toLowerCase().includes(term))
    : list;
});

const visibleItems = computed<MetadataChild[]>(() => {
  if (step.value !== "instance") {
    // The server already narrowed the level to this search term.
    return browseItems.value;
  }
  return filteredInstances.value.slice(0, instanceLimit.value);
});

/** Whether another local instance batch would render more rows. */
const hasMoreInstances = computed(
  () =>
    step.value === "instance" &&
    filteredInstances.value.length > instanceLimit.value
);

/** Whether the list footer (next page or its retry) is shown. */
const showFooter = computed(
  () =>
    hasMoreInstances.value ||
    (step.value !== "instance" && (hasNext.value || moreError.value !== ""))
);

const displayLoading = computed(() =>
  step.value === "instance" ? instancesLoading.value : browseLoading.value
);

const displayError = computed(() => {
  if (step.value === "instance") {
    return instancesError.value;
  }
  return browseError.value
    ? formatError(browseError.value, "dataQuality.target.fetchError")
    : "";
});

const moreError = computed(() =>
  browseMoreError.value
    ? formatError(browseMoreError.value, "dataQuality.target.fetchError")
    : ""
);

/** Instances are matched by name only; the server also matches comments. */
const searchPlaceholder = computed(() =>
  step.value === "instance"
    ? t("dataQuality.target.searchInstancePlaceholder")
    : t("dataQuality.target.searchPlaceholder")
);

const emptyLabel = computed(() => {
  // A level or the cached instance list can be empty because of the term the
  // reader typed; saying "no tables" then would be a lie.
  const searching =
    step.value === "instance"
      ? filter.value.trim() !== ""
      : browseQuery.value !== "";
  if (searching) {
    return t("dataQuality.target.noMatches");
  }
  switch (step.value) {
    case "instance":
      return t("dataQuality.target.noInstances");
    case "database":
      return t("dataQuality.target.noDatabases");
    case "schema":
      return t("dataQuality.target.noSchemas");
    default:
      return t("dataQuality.target.noTables");
  }
});

// `props` is read in the watcher; keep the binding named so the linter sees it.
// `immediate` matters when the picker is mounted already open (tests, dialogs
// restored from state); the normal flow opens it from closed.
watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      reset();
      void loadInstances();
      return;
    }
    // A closed dialog must not keep a debounced search or a level request alive.
    cancelFilterTimer();
    resetBrowser();
  },
  { immediate: true }
);

onBeforeUnmount(() => {
  cancelFilterTimer();
});

function reset() {
  cancelFilterTimer();
  instanceLimit.value = INSTANCE_BATCH;
  step.value = "instance";
  instanceId.value = "";
  instanceTitle.value = "";
  instanceEngine.value = undefined;
  database.value = "";
  databaseGuid.value = "";
  schema.value = "";
  schemaGuid.value = "";
  filter.value = "";
  instancesError.value = "";
  selected.value = [];
  resetBrowser();
}

async function loadInstances() {
  instancesLoading.value = true;
  instancesError.value = "";
  try {
    await instanceStore.ensureLoaded();
  } catch (e: unknown) {
    instancesError.value = formatError(e, "dataQuality.target.fetchError");
  } finally {
    instancesLoading.value = false;
  }
}

function cancelFilterTimer() {
  if (filterTimer !== undefined) {
    clearTimeout(filterTimer);
    filterTimer = undefined;
  }
}

/**
 * Replace the shown level with its first page. The search box is cleared with
 * the level: a term typed for the previous level has no meaning here.
 */
async function loadLevel(): Promise<MetadataChild[] | null> {
  cancelFilterTimer();
  filter.value = "";
  return await loadChildren(currentParent(), currentTypes());
}

/** Re-run whatever the failed first page or search was showing. */
async function retryCurrent() {
  if (step.value === "instance") {
    void loadInstances();
    return;
  }
  if (browseQuery.value !== "") {
    void searchChildren(currentParent(), currentTypes(), browseQuery.value);
    return;
  }
  if (step.value === "schema") {
    // Retrying the schema level keeps the empty-schema fallback the first load
    // would have taken.
    const schemas = await loadChildren(currentParent(), currentTypes());
    if (
      schemas !== null &&
      step.value === "schema" &&
      schemas.length === 0 &&
      !browseError.value
    ) {
      await showTables();
    }
    return;
  }
  void loadChildren(currentParent(), currentTypes());
}

/** The search box: filter the cached instances locally, query the server below. */
function onFilterChanged(value: string | number) {
  if (step.value === "instance") {
    instanceLimit.value = INSTANCE_BATCH;
    return;
  }
  const term = String(value).trim();
  const parent = currentParent();
  const types = currentTypes();
  cancelFilterTimer();
  filterTimer = setTimeout(() => {
    filterTimer = undefined;
    // The reader may have drilled away while the delay ran; that level's own
    // load already owns the list.
    if (step.value === "instance" || parent !== currentParent()) {
      return;
    }
    if (term === browseQuery.value && !browseError.value) {
      return;
    }
    if (term === "") {
      void loadChildren(parent, types);
    } else {
      void searchChildren(parent, types, term);
    }
  }, SEARCH_DELAY_MS);
}

function loadMore() {
  if (step.value === "instance") {
    instanceLimit.value += INSTANCE_BATCH;
    return;
  }
  void loadMoreChildren();
}

async function chooseInstance(id: string, title: string) {
  instanceId.value = id;
  instanceTitle.value = title;
  instanceEngine.value = instanceStore.byName(`instances/${id}`)?.engine;
  database.value = "";
  databaseGuid.value = "";
  schema.value = "";
  schemaGuid.value = "";
  step.value = "database";
  await loadLevel();
}

async function chooseDatabase(name: string, guid: string) {
  database.value = name;
  databaseGuid.value = guid;
  schema.value = "";
  schemaGuid.value = "";
  // The MySQL family has no schema level: the tables live directly under the
  // database, and their parent GUID keeps a trailing semicolon for the empty
  // schema segment. Asking for a schema would only show a blank level.
  if (isSchemaLessEngine(instanceEngine.value)) {
    await showTables();
    return;
  }
  step.value = "schema";
  const schemas = await loadLevel();
  // A newer request (a debounced search, or Back) superseded this load; its
  // answer says nothing about whether the database has schemas.
  if (schemas === null) {
    return;
  }
  // The reader can press Back while the first page is in flight. The level that
  // owned this load is then gone, so neither its answer nor the empty-schema
  // fallback below may navigate them somewhere else.
  if (step.value !== "schema" || databaseGuid.value !== guid) {
    return;
  }
  // PostgreSQL/MSSQL deployments may still have no schema rows; the tables then
  // live directly under the database, so keep that path working.
  if (schemas.length === 0 && !browseError.value) {
    await showTables();
  }
}

async function chooseSchema(name: string, guid: string) {
  schema.value = name;
  schemaGuid.value = guid;
  await showTables();
}

async function showTables() {
  step.value = "table";
  await loadLevel();
}

function goBack() {
  cancelFilterTimer();
  filter.value = "";
  if (step.value === "table" && schema.value) {
    // Returning to a level has to reload it: the items on screen are the level
    // below, and showing tables under a "schema" breadcrumb would be a lie.
    schema.value = "";
    schemaGuid.value = "";
    step.value = "schema";
    void loadLevel();
    return;
  }
  if (step.value === "table" || step.value === "schema") {
    database.value = "";
    databaseGuid.value = "";
    schema.value = "";
    schemaGuid.value = "";
    step.value = "database";
    void loadLevel();
    return;
  }
  step.value = "instance";
  instanceId.value = "";
  instanceTitle.value = "";
  instanceEngine.value = undefined;
  instanceLimit.value = INSTANCE_BATCH;
  resetBrowser();
}

function choose(item: MetadataChild) {
  switch (step.value) {
    case "instance":
      void chooseInstance(item.guid, item.name);
      return;
    case "database":
      void chooseDatabase(item.name, item.guid);
      return;
    case "schema":
      void chooseSchema(item.name, item.guid);
      return;
    default:
      if (isScopeMode.value) {
        emit("selectScope", {
          instance: instanceId.value,
          database: database.value,
          schema: schema.value,
          table: item.name,
          label: [instanceTitle.value, database.value, schema.value, item.name]
            .filter(Boolean)
            .join(" > "),
        });
        emit("update:open", false);
        return;
      }
      if (isMultiSelect.value) {
        toggleSelected(item);
        return;
      }
      emit("select", selectionFor(item));
      emit("update:open", false);
  }
}

/** The TargetSelection one table row stands for, with the current path. */
function selectionFor(item: MetadataChild): TargetSelection {
  return {
    guid: item.guid,
    metaType: item.metaType,
    path: {
      instance: instanceId.value,
      database: database.value,
      schema: schema.value,
      object: item.name,
    },
    columns: [],
  };
}

function isSelected(item: MetadataChild): boolean {
  return selected.value.some((selection) => selection.guid === item.guid);
}

function toggleSelected(item: MetadataChild) {
  selected.value = isSelected(item)
    ? selected.value.filter((selection) => selection.guid !== item.guid)
    : [...selected.value, selectionFor(item)];
}

/** Add every marked table in one go; the selection can span several schemas. */
function addSelected() {
  if (selected.value.length === 0) {
    return;
  }
  emit("selectMany", [...selected.value]);
  emit("update:open", false);
}

/** Apply the path prefix the reader has reached, without choosing another level. */
function applyCurrentScope() {
  if (!currentScope.value.instance) {
    return;
  }
  emit("selectScope", {
    ...currentScope.value,
    label: currentScopeLabel.value,
  });
  emit("update:open", false);
}
</script>
