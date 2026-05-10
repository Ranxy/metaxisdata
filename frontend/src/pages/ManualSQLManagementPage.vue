<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ t("manualSqlManagement.title") }}
        </h1>
        <p class="text-muted-foreground">
          {{ t("manualSqlManagement.description") }}
        </p>
      </div>
      <Button
        :disabled="availableDatabases.length === 0 || isSaving"
        @click="openCreateModal"
      >
        <Plus class="mr-2 h-4 w-4" />
        {{ t("manualSqlManagement.create") }}
      </Button>
    </div>

    <Card>
      <CardContent class="grid gap-4 p-6 md:grid-cols-4">
        <div class="space-y-2 md:col-span-2">
          <Label for="manual-sql-database">{{ t("manualSqlManagement.database") }}</Label>
          <select
            id="manual-sql-database"
            v-model="selectedParent"
            class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
          >
            <option value="">
              {{ t("manualSqlManagement.selectDatabase") }}
            </option>
            <option
              v-for="database in availableDatabases"
              :key="database.name"
              :value="database.name"
            >
              {{ formatDatabaseOption(database) }}
            </option>
          </select>
        </div>

        <AppInput
          v-model="searchQuery"
          :label="t('manualSqlManagement.search')"
          :placeholder="t('manualSqlManagement.searchPlaceholder')"
        />

        <AppInput
          v-model="schemaFilter"
          :label="t('manualSqlManagement.schema')"
          :placeholder="t('manualSqlManagement.schemaPlaceholder')"
        />

        <div class="space-y-2 md:col-span-3">
          <Label for="manual-sql-tags">{{ t("manualSqlManagement.tags") }}</Label>
          <Input
            id="manual-sql-tags"
            v-model="tagsFilterInput"
            :placeholder="t('manualSqlManagement.tagsPlaceholder')"
          />
          <p class="text-xs text-muted-foreground">
            {{ t("manualSqlManagement.tagsHint") }}
          </p>
        </div>

        <div class="flex items-end gap-2">
          <Button
            class="w-full"
            :disabled="!selectedParent || isLoading"
            @click="handleSearch"
          >
            <Search class="mr-2 h-4 w-4" />
            {{ t("manualSqlManagement.applyFilters") }}
          </Button>
        </div>
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

      <div
        v-else-if="!selectedParent"
        class="p-8 text-center text-muted-foreground"
      >
        <FileCode2 class="mx-auto mb-4 h-12 w-12 text-muted-foreground/50" />
        <p>{{ t("manualSqlManagement.selectDatabaseFirst") }}</p>
      </div>

      <div
        v-else-if="manualSqls.length === 0"
        class="p-8 text-center text-muted-foreground"
      >
        <FileCode2 class="mx-auto mb-4 h-12 w-12 text-muted-foreground/50" />
        <p>{{ t("manualSqlManagement.empty") }}</p>
      </div>

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
                <div class="mt-1 text-xs text-muted-foreground">{{ extractManualSqlId(item.name) }}</div>
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
                    @click="openEditModal(item)"
                  >
                    <Pencil class="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    class="text-destructive"
                    @click="openDeleteModal(item)"
                  >
                    <Trash2 class="h-4 w-4" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>

        <div
          v-if="nextPageToken || previousPageTokens.length > 0"
          class="flex items-center justify-between border-t p-4"
        >
          <div class="text-sm text-muted-foreground">
            {{ t("manualSqlManagement.showingResults", { total: manualSqls.length }) }}
          </div>
          <div class="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              :disabled="previousPageTokens.length === 0"
              @click="goToPreviousPage"
            >
              {{ t("common.previous") }}
            </Button>
            <Button
              variant="outline"
              size="sm"
              :disabled="!nextPageToken"
              @click="goToNextPage"
            >
              {{ t("common.next") }}
            </Button>
          </div>
        </div>
      </div>
    </Card>

    <AppModal
      v-model="showFormModal"
      :title="isEditing ? t('manualSqlManagement.editTitle') : t('manualSqlManagement.createTitle')"
      size="xl"
    >
      <form @submit.prevent="handleSave">
        <div class="grid gap-4 md:grid-cols-2">
          <div class="space-y-2">
            <Label for="manual-sql-form-database">{{ t("manualSqlManagement.database") }}</Label>
            <select
              id="manual-sql-form-database"
              v-model="form.parent"
              class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              :disabled="isEditing"
            >
              <option value="">
                {{ t("manualSqlManagement.selectDatabase") }}
              </option>
              <option
                v-for="database in availableDatabases"
                :key="database.name"
                :value="database.name"
              >
                {{ formatDatabaseOption(database) }}
              </option>
            </select>
            <p
              v-if="formErrors.parent"
              class="text-sm text-destructive"
            >
              {{ formErrors.parent }}
            </p>
          </div>
          <div class="space-y-2">
            <Label for="manual-sql-form-schema">{{ t("manualSqlManagement.schema") }}</Label>
            <select
              id="manual-sql-form-schema"
              v-model="form.schemaName"
              class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              :disabled="!form.parent || isLoadingSchemas"
            >
              <option value="">
                {{ t("manualSqlManagement.optionalSchema") }}
              </option>
              <option
                v-for="schemaName in schemaOptions"
                :key="schemaName"
                :value="schemaName"
              >
                {{ schemaName }}
              </option>
            </select>
            <p class="text-xs text-muted-foreground">
              {{ isLoadingSchemas ? t("manualSqlManagement.loadingSchemas") : t("manualSqlManagement.schemaSelectHint") }}
            </p>
          </div>
          <AppInput
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
          <AppInput
            v-model="form.title"
            :label="t('manualSqlManagement.titleField')"
            :placeholder="t('manualSqlManagement.titlePlaceholder')"
          />
          <AppInput
            v-model="form.comment"
            :label="t('manualSqlManagement.comment')"
            :placeholder="t('manualSqlManagement.commentPlaceholder')"
          />
          <div class="space-y-2 md:col-span-2">
            <Label for="manual-sql-form-tags">{{ t("manualSqlManagement.tags") }}</Label>
            <Input
              id="manual-sql-form-tags"
              v-model="form.tagsInput"
              :placeholder="t('manualSqlManagement.tagsPlaceholder')"
            />
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
      <template #footer>
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
      </template>
    </AppModal>

    <AppModal
      v-model="showDeleteModal"
      :title="t('manualSqlManagement.deleteTitle')"
      size="sm"
    >
      <div class="space-y-3 text-center">
        <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10">
          <Trash2 class="h-6 w-6 text-destructive" />
        </div>
        <p>{{ t("manualSqlManagement.deleteConfirm") }}</p>
        <p class="text-sm text-muted-foreground">
          {{ deletingItem?.title || deletingItem?.name }}
        </p>
      </div>
      <template #footer>
        <Button
          variant="outline"
          @click="showDeleteModal = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          variant="destructive"
          :disabled="isDeleting"
          @click="handleDelete"
        >
          {{ t("common.delete") }}
        </Button>
      </template>
    </AppModal>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { FileCode2, Pencil, Plus, Search, Trash2 } from "lucide-vue-next";
import { computed, onMounted, reactive, ref, watch } from "vue";
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
import AppInput from "@/components/common/AppInput.vue";
import AppLoading from "@/components/common/AppLoading.vue";
import AppModal from "@/components/common/AppModal.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useToastStore } from "@/store/modules/toast";
import type {
  Database,
  ManualSQL,
  MetaType,
} from "@/types/proto-es/v1/database_service_pb";

const { t, locale } = useI18n();
const router = useRouter();
const toastStore = useToastStore();

const databases = ref<Database[]>([]);
const manualSqls = ref<ManualSQL[]>([]);
const selectedParent = ref("");
const searchQuery = ref("");
const schemaFilter = ref("");
const tagsFilterInput = ref("");
const isLoading = ref(false);
const isSaving = ref(false);
const isDeleting = ref(false);
const error = ref("");
const nextPageToken = ref("");
const currentPageToken = ref("");
const previousPageTokens = ref<string[]>([]);
const showFormModal = ref(false);
const showDeleteModal = ref(false);
const editingItem = ref<ManualSQL | null>(null);
const deletingItem = ref<ManualSQL | null>(null);
const schemaOptions = ref<string[]>([]);
const isLoadingSchemas = ref(false);
const isCheckingManualSqlId = ref(false);
const manualSqlIdConflict = ref("");
let schemaLoadSequence = 0;
let manualSqlIdCheckSequence = 0;
let manualSqlIdCheckTimer: ReturnType<typeof setTimeout> | null = null;

const form = reactive({
  parent: "",
  manualSqlId: "",
  title: "",
  schemaName: "",
  comment: "",
  tagsInput: "",
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

function parseTagList(value: string): string[] {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
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
  if (!ts) {
    return "-";
  }
  return new Intl.DateTimeFormat(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(Number(ts.seconds) * 1000));
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

function extractManualSqlParent(name: string): string {
  return name.split("/").slice(0, 4).join("/");
}

function buildDatabaseGuid(name: string): string {
  return `${extractInstanceId(name)};${extractDatabaseName(name)}`;
}

function guidToRoutePath(guid: string): string {
  return guid
    .split(";")
    .map((segment) => (segment === "" ? "~" : encodeURIComponent(segment)))
    .join("/");
}

function resetForm() {
  form.parent = "";
  form.manualSqlId = "";
  form.title = "";
  form.schemaName = "";
  form.comment = "";
  form.tagsInput = "";
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
  form.tagsInput = item.tags.join(", ");
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
      metaType: 3 as MetaType,
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
      const message = e instanceof Error ? e.message : String(e);
      toastStore.error(message || t("manualSqlManagement.fetchSchemasError"));
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
    const message = e instanceof Error ? e.message : String(e);
    manualSqlIdConflict.value =
      message || t("manualSqlManagement.idCheckError");
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
    if (!selectedParent.value && response.databases.length > 0) {
      selectedParent.value = response.databases[0].name;
    }
  } catch (e: unknown) {
    const message = e instanceof Error ? e.message : String(e);
    toastStore.error(message || t("manualSqlManagement.fetchDatabasesError"));
  }
}

async function fetchManualSQL(pageToken = "") {
  if (!selectedParent.value) {
    manualSqls.value = [];
    nextPageToken.value = "";
    currentPageToken.value = "";
    return;
  }

  isLoading.value = true;
  error.value = "";
  currentPageToken.value = pageToken;

  try {
    const tags = parseTagList(tagsFilterInput.value);
    const schemaName = schemaFilter.value.trim();
    const query = searchQuery.value.trim();

    const response = query
      ? await searchManualSQL({
          parent: selectedParent.value,
          query,
          pageSize: 50,
          pageToken,
          schemaName,
          tags,
        })
      : await listManualSQL({
          parent: selectedParent.value,
          pageSize: 50,
          pageToken,
          schemaName,
          tags,
        });

    manualSqls.value = response.manualSqls;
    nextPageToken.value = response.nextPageToken;
  } catch (e: unknown) {
    const message = e instanceof Error ? e.message : String(e);
    error.value = message || t("manualSqlManagement.fetchError");
    toastStore.error(error.value);
  } finally {
    isLoading.value = false;
  }
}

function handleSearch() {
  previousPageTokens.value = [];
  fetchManualSQL();
}

function goToNextPage() {
  if (!nextPageToken.value) {
    return;
  }
  previousPageTokens.value.push(currentPageToken.value);
  fetchManualSQL(nextPageToken.value);
}

function goToPreviousPage() {
  if (previousPageTokens.value.length === 0) {
    return;
  }
  const token = previousPageTokens.value.pop() || "";
  fetchManualSQL(token);
}

async function handleSave() {
  if (!validateForm() || isCheckingManualSqlId.value) {
    return;
  }

  isSaving.value = true;
  try {
    const payload: ManualSQLInput = {
      title: form.title.trim(),
      schemaName: form.schemaName.trim(),
      comment: form.comment.trim(),
      sqlText: form.sqlText,
      tags: parseTagList(form.tagsInput),
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
      toastStore.success(t("manualSqlManagement.updateSuccess"));
    } else {
      await createManualSQL({
        parent: form.parent,
        manualSqlId: form.manualSqlId.trim(),
        manualSql: payload,
      });
      toastStore.success(t("manualSqlManagement.createSuccess"));
    }

    const createdParent = form.parent;
    showFormModal.value = false;
    resetForm();
    if (createdParent && selectedParent.value !== createdParent) {
      previousPageTokens.value = [];
      selectedParent.value = createdParent;
    }
    await fetchManualSQL(currentPageToken.value);
  } catch (e: unknown) {
    const message = e instanceof Error ? e.message : String(e);
    toastStore.error(message || t("manualSqlManagement.saveError"));
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
    toastStore.success(t("manualSqlManagement.deleteSuccess"));
    showDeleteModal.value = false;
    deletingItem.value = null;
    await fetchManualSQL(currentPageToken.value);
  } catch (e: unknown) {
    const message = e instanceof Error ? e.message : String(e);
    toastStore.error(message || t("manualSqlManagement.deleteError"));
  } finally {
    isDeleting.value = false;
  }
}

function openMetadata(guid: string) {
  router.push({
    name: "MetadataDetail",
    params: { guid: guidToRoutePath(guid) },
    query: { metaType: "18" },
  });
}

function openLineage(guid: string) {
  router.push({
    name: "LineageGraph",
    params: { guid: guidToRoutePath(guid) },
    query: { metaType: "18" },
  });
}

watch(selectedParent, () => {
  previousPageTokens.value = [];
  fetchManualSQL();
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
  await fetchManualSQL();
});
</script>