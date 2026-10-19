<template>
  <Dialog
    :open="open"
    @update:open="(value: boolean) => emit('update:open', value)"
  >
    <DialogContent class="max-w-2xl">
      <DialogHeader>
        <DialogTitle>{{ t("dataQuality.target.title") }}</DialogTitle>
        <DialogDescription>
          {{ t("dataQuality.target.description") }}
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
          :placeholder="t('dataQuality.target.filterPlaceholder')"
          :aria-label="t('dataQuality.target.filterPlaceholder')"
        />

        <div
          v-if="error"
          class="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive"
        >
          {{ error }}
        </div>

        <div
          v-else-if="loading"
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
            @click="choose(item)"
          >
            <span class="min-w-0 flex-1 truncate font-medium">
              {{ item.name }}
            </span>
            <span class="shrink-0 text-xs text-muted-foreground">
              {{ metaTypeSingularLabel(item.metaType, t) }}
            </span>
            <ChevronRight class="h-4 w-4 shrink-0 text-muted-foreground" />
          </button>
        </div>
      </div>

      <DialogFooter>
        <Button
          variant="outline"
          @click="emit('update:open', false)"
        >
          {{ t("common.cancel") }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { ArrowLeft, ChevronRight } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listMetadata } from "@/api/database";
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
import { useInstanceStore } from "@/store/modules/instance";
import type { StoredMetadata } from "@/types/proto-es/v1/database_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  TARGET_META_TYPES,
  type TargetSelection,
} from "@/utils/dataQualityForm";
import { metaTypeSingularLabel } from "@/utils/metaType";

/**
 * The target browser: instance → database → schema → table, using the metadata
 * registry the rest of the product reads. A table (or view) is the target of a
 * rule; the column it checks is chosen in the rule form, because that is part of
 * the typed config rather than of the target identity.
 *
 * The list is a dialog so it keeps the form's scroll position, and so the
 * layout audit's nested-scroller rule (which measures the page) never sees it.
 */

const props = defineProps<{
  open: boolean;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  select: [selection: TargetSelection];
}>();

type Step = "instance" | "database" | "schema" | "table";

interface BrowserItem {
  guid: string;
  name: string;
  metaType: MetaType;
}

const { t } = useI18n();
const { formatError } = useErrorMessage();
const instanceStore = useInstanceStore();

const step = ref<Step>("instance");
const instanceId = ref("");
const database = ref("");
const databaseGuid = ref("");
const schema = ref("");
const schemaGuid = ref("");
const filter = ref("");
const loading = ref(false);
const error = ref("");
const items = ref<BrowserItem[]>([]);
const instanceTitle = ref("");

const instances = computed(() => instanceStore.active);

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
      current: step.value === "table",
    });
  }
  return crumbs;
});

const visibleItems = computed(() => {
  const term = filter.value.trim().toLowerCase();
  const list =
    step.value === "instance"
      ? instances.value.map((instance) => ({
          guid: instance.name.replace("instances/", ""),
          name: instance.title || instance.name.replace("instances/", ""),
          metaType: MetaType.INSTANCE,
        }))
      : items.value;
  return term
    ? list.filter((item) => item.name.toLowerCase().includes(term))
    : list;
});

const emptyLabel = computed(() => {
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
watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      reset();
      void loadInstances();
    }
  }
);

function reset() {
  step.value = "instance";
  instanceId.value = "";
  instanceTitle.value = "";
  database.value = "";
  databaseGuid.value = "";
  schema.value = "";
  schemaGuid.value = "";
  filter.value = "";
  error.value = "";
  items.value = [];
}

async function loadInstances() {
  loading.value = true;
  error.value = "";
  try {
    await instanceStore.ensureLoaded();
  } catch (e: unknown) {
    error.value = formatError(e, "dataQuality.target.fetchError");
  } finally {
    loading.value = false;
  }
}

async function loadGroups(
  parentGuid: string,
  wanted: MetaType[]
): Promise<BrowserItem[]> {
  const response = await listMetadata({ parentGuid, pageSize: 100 });
  return response.typesStoredMetadata
    .filter((group) => wanted.includes(group.metaType))
    .flatMap((group) =>
      group.list.map((item) => ({
        guid: item.guid,
        name: storedMetadataName(item),
        metaType: group.metaType,
      }))
    );
}

/** The display name of a listed metadata row, whichever arm carries it. */
function storedMetadataName(item: StoredMetadata): string {
  const value = item.type.value;
  return (value as { name?: string } | undefined)?.name ?? item.guid;
}

async function chooseInstance(id: string, title: string) {
  instanceId.value = id;
  instanceTitle.value = title;
  database.value = "";
  databaseGuid.value = "";
  schema.value = "";
  schemaGuid.value = "";
  step.value = "database";
  await browse(async () => loadGroups(id, [MetaType.DATABASE]));
}

async function chooseDatabase(name: string, guid: string) {
  database.value = name;
  databaseGuid.value = guid;
  schema.value = "";
  schemaGuid.value = "";
  step.value = "schema";
  await browse(async () => await loadGroups(guid, [MetaType.SCHEMA]));
  // MySQL-family instances have no real schema level. When the schema list is
  // empty the tables live directly under the database, whose parent GUID keeps a
  // trailing semicolon for the empty schema segment.
  if (items.value.length === 0 && !error.value) {
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
  await browse(async () => {
    // The schema's own GUID when there is one; otherwise the MySQL-family
    // database GUID plus the trailing empty-schema separator.
    const parent = schemaGuid.value
      ? schemaGuid.value
      : `${databaseGuid.value};`;
    return await loadGroups(parent, TARGET_META_TYPES);
  });
}

async function browse(load: () => Promise<BrowserItem[]>) {
  loading.value = true;
  error.value = "";
  filter.value = "";
  try {
    items.value = await load();
  } catch (e: unknown) {
    items.value = [];
    error.value = formatError(e, "dataQuality.target.fetchError");
  } finally {
    loading.value = false;
  }
}

function goBack() {
  error.value = "";
  filter.value = "";
  if (step.value === "table" && schema.value) {
    // Returning to a level has to reload it: the items on screen are the level
    // below, and showing tables under a "schema" breadcrumb would be a lie.
    step.value = "schema";
    void browse(() =>
      loadGroups(`${instanceId.value};${database.value}`, [MetaType.SCHEMA])
    );
    return;
  }
  if (step.value === "table" || step.value === "schema") {
    database.value = "";
    schema.value = "";
    step.value = "database";
    void browse(() => loadGroups(instanceId.value, [MetaType.DATABASE]));
    return;
  }
  step.value = "instance";
  items.value = [];
}

function choose(item: BrowserItem) {
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
      emit("select", {
        guid: item.guid,
        metaType: item.metaType,
        path: {
          instance: instanceId.value,
          database: database.value,
          schema: schema.value,
          object: item.name,
        },
        columns: [],
      });
      emit("update:open", false);
  }
}
</script>
