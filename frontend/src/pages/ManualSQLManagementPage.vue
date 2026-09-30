<template>
  <div class="space-y-4">
    <PageHeader :title="t('manualSqlManagement.title')">
      <template #actions>
        <Button
          :disabled="availableDatabases.length === 0 || isSaving"
          @click="openCreateModal"
        >
          <Plus class="mr-2 h-4 w-4" />
          {{ t("manualSqlManagement.create") }}
        </Button>
      </template>
    </PageHeader>

    <AdvancedSearchBar
      :filter-categories="filterCategories"
      :search-placeholder="t('manualSqlManagement.searchPlaceholder')"
      @update:filters="handleFiltersUpdate"
    />

    <Card>
      <PageState
        :loading="isLoading"
        :error="error"
      >
        <EmptyState
          v-if="manualSqls.length === 0"
          :icon="FileCode2"
          :title="t('manualSqlManagement.empty')"
        />

        <div v-else>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("manualSqlManagement.titleColumn") }}</TableHead>
                <TableHead>{{ t("manualSqlManagement.schema") }}</TableHead>
                <TableHead>{{ t("manualSqlManagement.tags") }}</TableHead>
                <TableHead>{{ t("manualSqlManagement.updatedAt") }}</TableHead>
                <TableHead class="w-44 text-right">{{ t("manualSqlManagement.actions") }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="item in manualSqls"
                :key="item.name"
              >
                <TableCell>
                  <div class="font-medium">{{ item.title || extractManualSqlId(item.name) }}</div>
                  <div
                    v-if="showsIdentifier(item)"
                    class="mt-1 text-xs text-muted-foreground"
                  >
                    {{ extractManualSqlId(item.name) }}
                  </div>
                  <div
                    v-if="item.comment"
                    class="mt-2 line-clamp-2 max-w-xl text-xs text-muted-foreground"
                  >
                    {{ item.comment }}
                  </div>
                </TableCell>
                <TableCell>
                  {{ item.schemaName || t("metadataBrowser.defaultSchema") }}
                </TableCell>
                <TableCell>
                  <div class="flex flex-wrap gap-2">
                    <Badge
                      v-for="tag in item.tags"
                      :key="tag"
                      variant="secondary"
                    >
                      {{ tag }}
                    </Badge>
                    <span
                      v-if="item.tags.length === 0"
                      class="text-muted-foreground"
                    >
                      -
                    </span>
                  </div>
                </TableCell>
                <TableCell>
                  {{ formatTimestamp(item.updatedAt) }}
                </TableCell>
                <TableCell class="text-right">
                  <div class="flex items-center justify-end gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      @click="openMetadata(item.guid)"
                    >
                      {{ t("manualSqlManagement.metadata") }}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      @click="openLineage(item.guid)"
                    >
                      {{ t("manualSqlManagement.lineage") }}
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      :aria-label="t('common.edit')"
                      @click="openEditModal(item)"
                    >
                      <Pencil class="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      class="text-destructive"
                      :aria-label="t('common.delete')"
                      @click="openDeleteModal(item)"
                    >
                      <Trash2 class="h-4 w-4" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>

          <TablePager
            v-model:page-size="pageSize"
            :has-previous="hasPrevious"
            :has-next="hasNext"
            :disabled="isLoading"
            @previous="goToPreviousPage"
            @next="goToNextPage"
          />
        </div>
      </PageState>
    </Card>

    <Dialog v-model:open="showFormModal">
      <DialogContent class="max-w-4xl">
        <DialogHeader>
          <DialogTitle>{{ isEditing ? t("manualSqlManagement.editTitle") : t("manualSqlManagement.createTitle") }}</DialogTitle>
        </DialogHeader>

        <form @submit.prevent="handleSave">
          <div class="grid gap-4 md:grid-cols-2">
            <div class="space-y-2">
              <Label for="manual-sql-form-database">{{ t("manualSqlManagement.database") }}</Label>
              <Select
                v-model="form.parent"
                :disabled="isEditing"
              >
                <SelectTrigger id="manual-sql-form-database">
                  <SelectValue :placeholder="t('manualSqlManagement.selectDatabase')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="database in availableDatabases"
                    :key="database.name"
                    :value="database.name"
                  >
                    {{ formatDatabaseOption(database) }}
                  </SelectItem>
                </SelectContent>
              </Select>
              <p
                v-if="formErrors.parent"
                class="text-sm text-destructive"
              >
                {{ formErrors.parent }}
              </p>
            </div>
            <div class="space-y-2">
              <Label for="manual-sql-form-schema">{{ t("manualSqlManagement.schema") }}</Label>
              <Select
                v-model="schemaSelection"
                :disabled="!form.parent || isLoadingSchemas"
              >
                <SelectTrigger id="manual-sql-form-schema">
                  <SelectValue :placeholder="t('manualSqlManagement.optionalSchema')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem :value="DEFAULT_SCHEMA_VALUE">
                    {{ t("manualSqlManagement.optionalSchema") }}
                  </SelectItem>
                  <SelectItem
                    v-for="schemaName in schemaOptions"
                    :key="schemaName"
                    :value="schemaName"
                  >
                    {{ schemaName }}
                  </SelectItem>
                </SelectContent>
              </Select>
              <p class="text-xs text-muted-foreground">
                {{ isLoadingSchemas ? t("manualSqlManagement.loadingSchemas") : t("manualSqlManagement.schemaSelectHint") }}
              </p>
            </div>
            <FormField
              v-model="form.manualSqlId"
              :label="t('manualSqlManagement.id')"
              :placeholder="t('manualSqlManagement.idPlaceholder')"
              :disabled="isEditing"
              :error="formErrors.manualSqlId"
              required
            />
            <div class="space-y-2">
              <Label>{{ t("manualSqlManagement.idStatus") }}</Label>
              <div class="flex min-h-10 items-center rounded-md border border-dashed px-3 text-sm">
                <span v-if="isEditing" class="text-muted-foreground">
                  {{ t("manualSqlManagement.idFixedOnEdit") }}
                </span>
                <span v-else-if="!form.parent.trim() || !form.manualSqlId.trim()" class="text-muted-foreground">
                  {{ t("manualSqlManagement.idIdle") }}
                </span>
                <span v-else-if="isCheckingManualSqlId" class="text-muted-foreground">
                  {{ t("manualSqlManagement.idChecking") }}
                </span>
                <span v-else-if="manualSqlIdConflict" class="text-destructive">
                  {{ manualSqlIdConflict }}
                </span>
                <span v-else class="text-emerald-600">
                  {{ t("manualSqlManagement.idAvailable") }}
                </span>
              </div>
            </div>
            <FormField
              v-model="form.title"
              :label="t('manualSqlManagement.titleField')"
              :placeholder="t('manualSqlManagement.titlePlaceholder')"
            />
            <FormField
              v-model="form.comment"
              :label="t('manualSqlManagement.comment')"
              :placeholder="t('manualSqlManagement.commentPlaceholder')"
            />
            <div class="space-y-2 md:col-span-2">
              <Label for="manual-sql-form-tags">{{ t("manualSqlManagement.tags") }}</Label>
              <div class="flex min-h-10 w-full flex-wrap items-center gap-2 rounded-md border border-input bg-background px-3 py-2">
                <Badge
                  v-for="tag in form.tags"
                  :key="tag"
                  variant="secondary"
                  class="flex items-center gap-1"
                >
                  <span>{{ tag }}</span>
                  <button
                    type="button"
                    class="rounded-full p-0.5 text-muted-foreground transition-colors hover:text-foreground"
                    :aria-label="t('manualSqlManagement.removeTag', { tag })"
                    @click="removeTag(tag)"
                  >
                    <X class="h-3 w-3" />
                  </button>
                </Badge>
                <Input
                  id="manual-sql-form-tags"
                  v-model="form.tagsDraft"
                  :placeholder="t('manualSqlManagement.tagsPlaceholder')"
                  class="h-auto min-w-40 flex-1 border-0 bg-transparent px-0 py-0 shadow-none focus-visible:shadow-none"
                  @keydown="handleTagInputKeydown"
                  @blur="commitTagDraft"
                />
              </div>
              <p class="text-xs text-muted-foreground">
                {{ t("manualSqlManagement.tagsInputHint") }}
              </p>
            </div>
            <div class="space-y-2 md:col-span-2">
              <Label for="manual-sql-form-attributes">{{ t("manualSqlManagement.attributes") }}</Label>
              <textarea
                id="manual-sql-form-attributes"
                v-model="form.attributesInput"
                class="min-h-28 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                :placeholder="t('manualSqlManagement.attributesPlaceholder')"
              />
              <p class="text-xs text-muted-foreground">
                {{ t("manualSqlManagement.attributesHint") }}
              </p>
            </div>
            <div class="space-y-2 md:col-span-2">
              <Label for="manual-sql-form-sql">{{ t("manualSqlManagement.sqlText") }}</Label>
              <textarea
                id="manual-sql-form-sql"
                v-model="form.sqlText"
                class="min-h-64 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
                :placeholder="t('manualSqlManagement.sqlPlaceholder')"
              />
              <p
                v-if="formErrors.sqlText"
                class="text-sm text-destructive"
              >
                {{ formErrors.sqlText }}
              </p>
            </div>
          </div>
        </form>

        <DialogFooter>
          <Button
            variant="outline"
            @click="showFormModal = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="isSaving"
            @click="handleSave"
          >
            {{ isEditing ? t("common.save") : t("common.create") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <ConfirmDeleteDialog
      v-model="showDeleteModal"
      :title="t('manualSqlManagement.deleteTitle')"
      :message="t('manualSqlManagement.deleteConfirm')"
      :item-name="deletingItem?.title || deletingItem?.name"
      :loading="isDeleting"
      @confirm="handleDelete"
    />
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { FileCode2, Pencil, Plus, Trash2, X } from "lucide-vue-next";
import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import {
  createManualSQL,
  deleteManualSQL,
  getManualSQL,
  listDatabases,
  listManualSQL,
  listMetadata,
  type ManualSQLInput,
  searchManualSQL,
  updateManualSQL,
} from "@/api/database";
import AdvancedSearchBar, {
  type ActiveFilter,
  type FilterCategory,
} from "@/components/common/AdvancedSearchBar.vue";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { FormField } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { notify } from "@/lib/notify";
import {
  type Database,
  type ManualSQL,
  MetaType,
} from "@/types/proto-es/v1/database_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";

const { t, locale } = useI18n();
const { formatError, handleError } = useErrorHandler();
const router = useRouter();
const GLOBAL_MANUAL_SQL_PARENT = "instances/-/databases/-";

const databases = ref<Database[]>([]);

const selectedParent = ref("");
const searchQuery = ref("");
const schemaFilter = ref("");
const tagsFilterInput = ref<string[]>([]);

const isSaving = ref(false);
const isDeleting = ref(false);
const error = ref("");
const pageSize = ref(50);
const showFormModal = ref(false);
const showDeleteModal = ref(false);
const editingItem = ref<ManualSQL | null>(null);
const deletingItem = ref<ManualSQL | null>(null);
const schemaOptions = ref<string[]>([]);

/**
 * Radix's Select refuses an empty-string item value, so the "default schema"
 * entry carries this sentinel and the empty string the form actually stores.
 */
const DEFAULT_SCHEMA_VALUE = "__default__";

const schemaSelection = computed({
  get: () => (form.schemaName === "" ? DEFAULT_SCHEMA_VALUE : form.schemaName),
  set: (value: string) => {
    form.schemaName = value === DEFAULT_SCHEMA_VALUE ? "" : value;
  },
});
const isLoadingSchemas = ref(false);
const isCheckingManualSqlId = ref(false);
const manualSqlIdConflict = ref("");
let schemaLoadSequence = 0;
let manualSqlIdCheckSequence = 0;
let manualSqlIdCheckTimer: ReturnType<typeof setTimeout> | null = null;
let filterChangeTimer: ReturnType<typeof setTimeout> | null = null;

const form = reactive({
  parent: "",
  manualSqlId: "",
  title: "",
  schemaName: "",
  comment: "",
  tags: [] as string[],
  tagsDraft: "",
  attributesInput: "",
  sqlText: "",
});

const formErrors = reactive({
  parent: "",
  manualSqlId: "",
  sqlText: "",
});

const isEditing = computed(() => editingItem.value != null);
const availableDatabases = computed(() =>
  databases.value.filter((item) => !!item.name)
);
const databaseFilterOptions = computed(() =>
  availableDatabases.value.map((database) => ({
    value: database.name,
    label: formatDatabaseOption(database),
  }))
);

/**
 * The three filter dimensions this page offers. The filter bar owns their UI; the
 * page only maps the emitted pills back onto the request parameters.
 */
const filterCategories = computed<FilterCategory[]>(() => [
  {
    type: "database",
    label: t("manualSqlManagement.databaseFilter"),
    icon: "🗄️",
    options: databaseFilterOptions.value,
  },
  {
    type: "schema",
    label: t("manualSqlManagement.schemaFilter"),
    icon: "#",
    kind: "input",
    placeholder: t("manualSqlManagement.schemaPlaceholder"),
  },
  {
    type: "tags",
    label: t("manualSqlManagement.tagsFilter"),
    icon: "🏷️",
    kind: "tags",
    placeholder: t("manualSqlManagement.tagsPlaceholder"),
    hint: t("manualSqlManagement.tagsHint"),
    note: t("manualSqlManagement.tagsInputHint"),
  },
]);

function handleFiltersUpdate(filters: ActiveFilter[]) {
  const valueOf = (type: string) =>
    filters.find((filter) => filter.type === type)?.value ?? "";
  selectedParent.value = valueOf("database");
  searchQuery.value = valueOf("name");
  schemaFilter.value = valueOf("schema");
  const tags = filters.find((filter) => filter.type === "tags")?.value;
  tagsFilterInput.value = tags ? tags.split(",") : [];
}

function parseTagList(value: string): string[] {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function addTag(tag: string) {
  const normalizedTag = tag.trim();
  if (!normalizedTag || form.tags.includes(normalizedTag)) {
    return;
  }
  form.tags.push(normalizedTag);
}

function commitTagDraft() {
  const draft = form.tagsDraft.trim();
  if (!draft) {
    form.tagsDraft = "";
    return;
  }

  for (const tag of parseTagList(draft)) {
    addTag(tag);
  }

  form.tagsDraft = "";
}

function removeTag(tagToRemove: string) {
  form.tags = form.tags.filter((tag) => tag !== tagToRemove);
}

function handleTagInputKeydown(event: KeyboardEvent) {
  if (event.key === "Enter" || event.key === ",") {
    event.preventDefault();
    commitTagDraft();
    return;
  }

  if (event.key === "Backspace" && !form.tagsDraft && form.tags.length > 0) {
    form.tags.pop();
  }
}

function parseAttributes(value: string): Record<string, string> {
  const result: Record<string, string> = {};
  for (const line of value.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    const separator = trimmed.indexOf("=");
    if (separator === -1) {
      result[trimmed] = "";
      continue;
    }
    const key = trimmed.slice(0, separator).trim();
    const attrValue = trimmed.slice(separator + 1).trim();
    if (key) {
      result[key] = attrValue;
    }
  }
  return result;
}

function formatAttributes(attributes: Record<string, string>): string {
  return Object.entries(attributes)
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");
}

function formatTimestamp(ts: Timestamp | undefined): string {
  return formatDateTime(ts, locale.value);
}

function formatDatabaseOption(database: Database): string {
  const instanceName =
    database.instanceResource?.title || extractInstanceId(database.name);
  return `${instanceName} / ${extractDatabaseName(database.name)}`;
}

function extractInstanceId(name: string): string {
  return name.split("/")[1] || name;
}

function extractDatabaseName(name: string): string {
  return name.split("/")[3] || name;
}

function extractManualSqlId(name: string): string {
  return name.split("/").pop() || name;
}

/**
 * The identifier under the title is only worth a line when the title above it
 * says something else: without a title the heading already is the identifier,
 * so rendering both printed the same string twice.
 */
function showsIdentifier(item: ManualSQL): boolean {
  return item.title !== "" && item.title !== extractManualSqlId(item.name);
}

function extractManualSqlParent(name: string): string {
  return name.split("/").slice(0, 4).join("/");
}

function buildDatabaseGuid(name: string): string {
  return `${extractInstanceId(name)};${extractDatabaseName(name)}`;
}

function resetForm() {
  form.parent = "";
  form.manualSqlId = "";
  form.title = "";
  form.schemaName = "";
  form.comment = "";
  form.tags = [];
  form.tagsDraft = "";
  form.attributesInput = "";
  form.sqlText = "";
  formErrors.parent = "";
  formErrors.manualSqlId = "";
  formErrors.sqlText = "";
  schemaOptions.value = [];
  manualSqlIdConflict.value = "";
  isCheckingManualSqlId.value = false;
}

function openCreateModal() {
  editingItem.value = null;
  resetForm();
  form.parent = selectedParent.value || availableDatabases.value[0]?.name || "";
  showFormModal.value = true;
}

function openEditModal(item: ManualSQL) {
  editingItem.value = item;
  form.parent = extractManualSqlParent(item.name);
  form.manualSqlId = extractManualSqlId(item.name);
  form.title = item.title;
  form.schemaName = item.schemaName;
  form.comment = item.comment;
  form.tags = [...item.tags];
  form.tagsDraft = "";
  form.attributesInput = formatAttributes(item.attributes ?? {});
  form.sqlText = item.sqlText;
  formErrors.parent = "";
  formErrors.manualSqlId = "";
  formErrors.sqlText = "";
  manualSqlIdConflict.value = "";
  showFormModal.value = true;
}

function openDeleteModal(item: ManualSQL) {
  deletingItem.value = item;
  showDeleteModal.value = true;
}

function validateForm(): boolean {
  formErrors.parent = form.parent.trim()
    ? ""
    : t("manualSqlManagement.databaseRequired");
  formErrors.manualSqlId = form.manualSqlId.trim()
    ? ""
    : t("manualSqlManagement.idRequired");
  if (!formErrors.manualSqlId && manualSqlIdConflict.value) {
    formErrors.manualSqlId = manualSqlIdConflict.value;
  }
  formErrors.sqlText = form.sqlText.trim()
    ? ""
    : t("manualSqlManagement.sqlRequired");
  return !formErrors.parent && !formErrors.manualSqlId && !formErrors.sqlText;
}

async function fetchSchemas(parent: string) {
  const currentSequence = ++schemaLoadSequence;
  if (!parent) {
    schemaOptions.value = [];
    return;
  }

  isLoadingSchemas.value = true;
  try {
    const response = await listMetadata({
      parentGuid: buildDatabaseGuid(parent),
      pageSize: 500,
      metaType: MetaType.SCHEMA,
    });
    if (currentSequence !== schemaLoadSequence) {
      return;
    }

    const nextSchemaOptions = response.typesStoredMetadata
      .flatMap((group) => group.list)
      .flatMap((item) =>
        item.type.case === "schemaMetadata" && item.type.value.name
          ? [item.type.value.name]
          : []
      )
      .sort((left, right) => left.localeCompare(right));

    schemaOptions.value = [...new Set(nextSchemaOptions)];
    if (form.schemaName && !schemaOptions.value.includes(form.schemaName)) {
      form.schemaName = "";
    }
  } catch (e: unknown) {
    if (currentSequence === schemaLoadSequence) {
      schemaOptions.value = [];
      handleError(e, "manualSqlManagement.fetchSchemasError");
    }
  } finally {
    if (currentSequence === schemaLoadSequence) {
      isLoadingSchemas.value = false;
    }
  }
}

async function checkManualSqlIdConflict() {
  const currentSequence = ++manualSqlIdCheckSequence;
  if (!showFormModal.value || isEditing.value) {
    manualSqlIdConflict.value = "";
    isCheckingManualSqlId.value = false;
    return;
  }

  const parent = form.parent.trim();
  const manualSqlId = form.manualSqlId.trim();
  if (!parent || !manualSqlId) {
    manualSqlIdConflict.value = "";
    isCheckingManualSqlId.value = false;
    return;
  }

  isCheckingManualSqlId.value = true;
  try {
    const existing = await getManualSQL(`${parent}/manualSqls/${manualSqlId}`);
    if (currentSequence !== manualSqlIdCheckSequence) {
      return;
    }
    const existingSchema =
      existing.schemaName || t("metadataBrowser.defaultSchema");
    manualSqlIdConflict.value = t("manualSqlManagement.idExists", {
      schema: existingSchema,
    });
  } catch (e: unknown) {
    if (currentSequence !== manualSqlIdCheckSequence) {
      return;
    }
    if (e instanceof ConnectError && e.code === Code.NotFound) {
      manualSqlIdConflict.value = "";
      return;
    }
    manualSqlIdConflict.value = formatError(
      e,
      "manualSqlManagement.idCheckError"
    );
  } finally {
    if (currentSequence === manualSqlIdCheckSequence) {
      isCheckingManualSqlId.value = false;
    }
  }
}

async function fetchDatabases() {
  try {
    const response = await listDatabases({
      parent: "workspaces/-",
      pageSize: 1000,
      showDeleted: false,
    });
    databases.value = response.databases;
  } catch (e: unknown) {
    handleError(e, "manualSqlManagement.fetchDatabasesError");
  }
}

const {
  items: manualSqls,
  isLoading,
  hasNext,
  hasPrevious,
  reset: resetManualSQLPage,
  refresh: refreshManualSQLPage,
  goNext: goToNextPage,
  goPrevious: goToPreviousPage,
} = usePagedFetch<ManualSQL>({
  fetchPage: async (pageToken, signal) => {
    const parent = selectedParent.value || GLOBAL_MANUAL_SQL_PARENT;
    const tags = [...tagsFilterInput.value];
    const schemaName = schemaFilter.value.trim();
    const query = searchQuery.value.trim();

    const response = query
      ? await searchManualSQL({
          parent,
          query,
          pageSize: pageSize.value,
          pageToken,
          schemaName,
          tags,
          signal,
        })
      : await listManualSQL({
          parent,
          pageSize: pageSize.value,
          pageToken,
          schemaName,
          tags,
          signal,
        });

    return {
      items: response.manualSqls,
      nextPageToken: response.nextPageToken,
    };
  },
  onError: (e) => {
    error.value = handleError(e, "manualSqlManagement.fetchError");
  },
});

// A bigger page restarts the walk: a cursor belongs to the query that produced it.
watch(pageSize, () => void resetManualSQLPage());

async function handleSave() {
  if (!validateForm() || isCheckingManualSqlId.value) {
    return;
  }

  commitTagDraft();

  isSaving.value = true;
  try {
    const payload: ManualSQLInput = {
      title: form.title.trim(),
      schemaName: form.schemaName.trim(),
      comment: form.comment.trim(),
      sqlText: form.sqlText,
      tags: [...form.tags],
      attributes: parseAttributes(form.attributesInput),
    };

    if (editingItem.value) {
      await updateManualSQL({
        manualSql: {
          ...payload,
          name: editingItem.value.name,
          guid: editingItem.value.guid,
        },
      });
      notify.success(t("manualSqlManagement.updateSuccess"));
    } else {
      await createManualSQL({
        parent: form.parent,
        manualSqlId: form.manualSqlId.trim(),
        manualSql: payload,
      });
      notify.success(t("manualSqlManagement.createSuccess"));
    }

    const createdParent = form.parent;
    showFormModal.value = false;
    resetForm();
    if (createdParent && selectedParent.value !== createdParent) {
      // The scope watch reloads the first page of the new parent.
      selectedParent.value = createdParent;
    } else {
      await refreshManualSQLPage();
    }
  } catch (e: unknown) {
    handleError(e, "manualSqlManagement.saveError");
  } finally {
    isSaving.value = false;
  }
}

async function handleDelete() {
  if (!deletingItem.value) {
    return;
  }
  isDeleting.value = true;
  try {
    await deleteManualSQL(deletingItem.value.name);
    notify.success(t("manualSqlManagement.deleteSuccess"));
    showDeleteModal.value = false;
    deletingItem.value = null;
    await refreshManualSQLPage();
  } catch (e: unknown) {
    handleError(e, "manualSqlManagement.deleteError");
  } finally {
    isDeleting.value = false;
  }
}

function openMetadata(guid: string) {
  router.push({
    name: "MetadataDetail",
    params: { guid: guidToRouteParams(guid) },
    query: { metaType: "18" },
  });
}

function openLineage(guid: string) {
  router.push({
    name: "LineageGraph",
    params: { guid: guidToRouteParams(guid) },
    query: { metaType: "18" },
  });
}

watch(selectedParent, () => {
  if (filterChangeTimer) {
    clearTimeout(filterChangeTimer);
    filterChangeTimer = null;
  }
  void resetManualSQLPage();
});

watch([searchQuery, schemaFilter, tagsFilterInput], () => {
  if (filterChangeTimer) {
    clearTimeout(filterChangeTimer);
  }

  filterChangeTimer = setTimeout(() => {
    void resetManualSQLPage();
    filterChangeTimer = null;
  }, 250);
});

watch(
  () => form.parent,
  async (parent, previousParent) => {
    if (!showFormModal.value) {
      return;
    }
    if (!isEditing.value && previousParent && previousParent !== parent) {
      form.schemaName = "";
    }
    await fetchSchemas(parent);
  }
);

watch(
  () =>
    [
      showFormModal.value,
      isEditing.value,
      form.parent,
      form.manualSqlId,
    ] as const,
  ([isVisible, editing, parent, manualSqlId]) => {
    if (manualSqlIdCheckTimer) {
      clearTimeout(manualSqlIdCheckTimer);
      manualSqlIdCheckTimer = null;
    }

    if (!isVisible || editing || !parent.trim() || !manualSqlId.trim()) {
      manualSqlIdConflict.value = "";
      isCheckingManualSqlId.value = false;
      return;
    }

    manualSqlIdCheckTimer = setTimeout(() => {
      checkManualSqlIdConflict();
    }, 300);
  }
);

onMounted(async () => {
  await fetchDatabases();
  await resetManualSQLPage();
});

onBeforeUnmount(() => {
  if (manualSqlIdCheckTimer) {
    clearTimeout(manualSqlIdCheckTimer);
  }

  if (filterChangeTimer) {
    clearTimeout(filterChangeTimer);
  }
});
</script>