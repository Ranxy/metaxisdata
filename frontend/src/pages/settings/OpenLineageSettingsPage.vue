<template>
  <div class="space-y-6">
    <!-- Page Header -->
    <div>
      <h1 class="text-2xl font-bold tracking-tight">
        {{ t("openlineageSettings.title") }}
      </h1>
      <p class="text-muted-foreground">
        {{ t("openlineageSettings.description") }}
      </p>
    </div>

    <!-- Namespace Mappings Section -->
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle>{{
              t("openlineageSettings.namespaceMappings")
            }}</CardTitle>
            <CardDescription>{{
              t("openlineageSettings.namespaceMappingsDescription")
            }}</CardDescription>
          </div>
          <Button
            size="sm"
            @click="openCreateMappingModal"
          >
            <Plus class="h-4 w-4 mr-2" />
            {{ t("openlineageSettings.addMapping") }}
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div
          v-if="isLoadingMappings"
          class="p-8 flex justify-center"
        >
          <AppLoading />
        </div>
        <div
          v-else-if="mappings.length === 0"
          class="p-8 text-center text-muted-foreground"
        >
          <Network class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
          <p>{{ t("openlineageSettings.noMappings") }}</p>
        </div>
        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{
                t("openlineageSettings.namespace")
              }}</TableHead>
              <TableHead>{{
                t("openlineageSettings.instanceResourceId")
              }}</TableHead>
              <TableHead>{{
                t("openlineageSettings.databaseName")
              }}</TableHead>
              <TableHead class="text-right">
                {{ t("openlineageSettings.actions") }}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="m in mappings"
              :key="String(m.id)"
            >
              <TableCell class="font-mono text-sm">
                {{ m.namespace }}
              </TableCell>
              <TableCell>
                {{ getInstanceTitle(m.instanceResourceId) }}
              </TableCell>
              <TableCell class="text-muted-foreground">
                {{ m.databaseName || "-" }}
              </TableCell>
              <TableCell class="text-right">
                <div class="flex items-center justify-end gap-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    :title="t('common.edit')"
                    @click="openEditMappingModal(m)"
                  >
                    <Pencil class="h-4 w-4 text-muted-foreground" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    :title="t('common.delete')"
                    @click="confirmDeleteMapping(m)"
                  >
                    <Trash2 class="h-4 w-4 text-muted-foreground hover:text-destructive" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </CardContent>
    </Card>

    <!-- API Keys Section -->
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle>{{ t("openlineageSettings.apiKeys") }}</CardTitle>
            <CardDescription>{{
              t("openlineageSettings.apiKeysDescription")
            }}</CardDescription>
          </div>
          <Button
            size="sm"
            @click="openCreateKeyModal"
          >
            <Plus class="h-4 w-4 mr-2" />
            {{ t("openlineageSettings.createAPIKey") }}
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div
          v-if="isLoadingKeys"
          class="p-8 flex justify-center"
        >
          <AppLoading />
        </div>
        <div
          v-else-if="apiKeys.length === 0"
          class="p-8 text-center text-muted-foreground"
        >
          <KeyRound class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
          <p>{{ t("openlineageSettings.noAPIKeys") }}</p>
        </div>
        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{
                t("openlineageSettings.apiKeyDescription")
              }}</TableHead>
              <TableHead>{{
                t("openlineageSettings.createdBy")
              }}</TableHead>
              <TableHead>{{
                t("openlineageSettings.createdAt")
              }}</TableHead>
              <TableHead class="text-right">
                {{ t("openlineageSettings.actions") }}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="key in apiKeys"
              :key="String(key.id)"
              :class="{ 'opacity-60': key.revokedAt }"
            >
              <TableCell class="font-medium">
                {{ key.description }}
              </TableCell>
              <TableCell class="text-muted-foreground">
                {{ key.createdBy || "-" }}
              </TableCell>
              <TableCell class="text-muted-foreground">
                {{ formatTimestamp(key.createdAt) }}
              </TableCell>
              <TableCell class="text-right">
                <Button
                  v-if="!key.revokedAt"
                  variant="ghost"
                  size="sm"
                  class="text-destructive hover:text-destructive"
                  @click="confirmRevokeKey(key)"
                >
                  {{ t("openlineageSettings.revokeAPIKey") }}
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </CardContent>
    </Card>

    <!-- Create/Edit Mapping Modal -->
    <AppModal
      v-model="showMappingModal"
      :title="
        editingMapping
          ? t('openlineageSettings.editMapping')
          : t('openlineageSettings.addMapping')
      "
      size="md"
    >
      <form @submit.prevent="handleSaveMapping">
        <div class="space-y-4">
          <AppInput
            v-model="mappingForm.namespace"
            :label="t('openlineageSettings.namespace')"
            :placeholder="t('openlineageSettings.namespacePlaceholder')"
            required
          />
          <div>
            <label class="text-sm font-medium leading-none">
              {{ t("openlineageSettings.instanceResourceId") }}
            </label>
            <select
              v-model="mappingForm.instanceResourceId"
              class="mt-1.5 flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              required
            >
              <option
                value=""
                disabled
              >
                {{ t("openlineageSettings.instanceResourceIdPlaceholder") }}
              </option>
              <option
                v-for="inst in instances"
                :key="inst.name"
                :value="extractResourceId(inst.name)"
              >
                {{ inst.title || inst.name }}
              </option>
            </select>
          </div>
          <AppInput
            v-model="mappingForm.databaseName"
            :label="t('openlineageSettings.databaseName')"
            :placeholder="t('openlineageSettings.databaseNamePlaceholder')"
          />
        </div>
      </form>
      <template #footer>
        <Button
          variant="outline"
          @click="showMappingModal = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          :disabled="isSavingMapping"
          @click="handleSaveMapping"
        >
          {{ t("common.save") }}
        </Button>
      </template>
    </AppModal>

    <!-- Delete Mapping Confirmation -->
    <AppModal
      v-model="showDeleteMappingModal"
      :title="t('openlineageSettings.deleteMapping')"
      size="sm"
    >
      <div class="text-center">
        <div class="w-12 h-12 mx-auto mb-4 rounded-full bg-destructive/10 flex items-center justify-center">
          <Trash2 class="h-6 w-6 text-destructive" />
        </div>
        <p>{{ t("openlineageSettings.deleteMappingConfirm") }}</p>
        <p class="text-sm text-muted-foreground mt-2 font-mono">
          {{ mappingToDelete?.namespace }}
        </p>
      </div>
      <template #footer>
        <Button
          variant="outline"
          @click="showDeleteMappingModal = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          variant="destructive"
          :disabled="isDeletingMapping"
          @click="handleDeleteMapping"
        >
          {{ t("common.delete") }}
        </Button>
      </template>
    </AppModal>

    <!-- Create API Key Modal -->
    <AppModal
      v-model="showCreateKeyModal"
      :title="t('openlineageSettings.createAPIKey')"
      size="md"
    >
      <form @submit.prevent="handleCreateKey">
        <div class="space-y-4">
          <AppInput
            v-model="keyForm.description"
            :label="t('openlineageSettings.apiKeyDescription')"
            :placeholder="
              t('openlineageSettings.apiKeyDescriptionPlaceholder')
            "
            required
          />
        </div>
      </form>
      <template #footer>
        <Button
          variant="outline"
          @click="showCreateKeyModal = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          :disabled="isCreatingKey"
          @click="handleCreateKey"
        >
          {{ t("common.confirm") }}
        </Button>
      </template>
    </AppModal>

    <!-- Show Created Key Modal -->
    <AppModal
      v-model="showKeyResultModal"
      :title="t('openlineageSettings.apiKeyLabel')"
      size="md"
    >
      <div class="space-y-4">
        <div class="rounded-md bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-800 p-4">
          <p class="text-sm text-amber-800 dark:text-amber-200">
            {{ t("openlineageSettings.apiKeyCreated") }}
          </p>
        </div>
        <div class="flex items-center gap-2">
          <code class="flex-1 rounded-md bg-muted p-3 text-sm font-mono break-all select-all">
            {{ createdKeyValue }}
          </code>
          <Button
            variant="outline"
            size="sm"
            @click="copyKey"
          >
            <Copy class="h-4 w-4 mr-1" />
            {{ copied ? t("openlineageSettings.copied") : t("openlineageSettings.copyKey") }}
          </Button>
        </div>
      </div>
      <template #footer>
        <Button @click="showKeyResultModal = false">
          {{ t("common.confirm") }}
        </Button>
      </template>
    </AppModal>

    <!-- Revoke Key Confirmation -->
    <AppModal
      v-model="showRevokeKeyModal"
      :title="t('openlineageSettings.revokeAPIKey')"
      size="sm"
    >
      <div class="text-center">
        <div class="w-12 h-12 mx-auto mb-4 rounded-full bg-destructive/10 flex items-center justify-center">
          <KeyRound class="h-6 w-6 text-destructive" />
        </div>
        <p>{{ t("openlineageSettings.revokeAPIKeyConfirm") }}</p>
        <p class="text-sm text-muted-foreground mt-2">
          {{ keyToRevoke?.description }}
        </p>
      </div>
      <template #footer>
        <Button
          variant="outline"
          @click="showRevokeKeyModal = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          variant="destructive"
          :disabled="isRevokingKey"
          @click="handleRevokeKey"
        >
          {{ t("openlineageSettings.revokeAPIKey") }}
        </Button>
      </template>
    </AppModal>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Copy, KeyRound, Network, Pencil, Plus, Trash2 } from "lucide-vue-next";
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { listInstances } from "@/api/instance";
import {
  createAPIKey,
  createNamespaceMapping,
  deleteNamespaceMapping,
  listAPIKey,
  listNamespaceMapping,
  revokeAPIKey,
  updateNamespaceMapping,
} from "@/api/openlineage";
import AppInput from "@/components/common/AppInput.vue";
import AppLoading from "@/components/common/AppLoading.vue";
import AppModal from "@/components/common/AppModal.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";
import type {
  APIKeyResource,
  NamespaceMappingResource,
} from "@/types/proto-es/v1/openlineage_service_pb";

const { t, locale } = useI18n();
const { handleError, showSuccess } = useErrorHandler();

// State
const isLoadingMappings = ref(false);
const isLoadingKeys = ref(false);
const isSavingMapping = ref(false);
const isDeletingMapping = ref(false);
const isCreatingKey = ref(false);
const isRevokingKey = ref(false);

const mappings = ref<NamespaceMappingResource[]>([]);
const apiKeys = ref<APIKeyResource[]>([]);
const instances = ref<Instance[]>([]);

// Mapping modals
const showMappingModal = ref(false);
const showDeleteMappingModal = ref(false);
const editingMapping = ref<NamespaceMappingResource | null>(null);
const mappingToDelete = ref<NamespaceMappingResource | null>(null);
const mappingForm = ref({
  namespace: "",
  instanceResourceId: "",
  databaseName: "",
});

// API key modals
const showCreateKeyModal = ref(false);
const showKeyResultModal = ref(false);
const showRevokeKeyModal = ref(false);
const keyToRevoke = ref<APIKeyResource | null>(null);
const keyForm = ref({ description: "" });
const createdKeyValue = ref("");
const copied = ref(false);

function extractResourceId(name: string): string {
  return name.replace("instances/", "");
}

function getInstanceTitle(resourceId: string): string {
  const inst = instances.value.find(
    (i) => extractResourceId(i.name) === resourceId
  );
  return inst?.title || resourceId;
}

function formatTimestamp(ts: Timestamp | undefined): string {
  if (!ts?.seconds) return "-";
  const d = new Date(Number(ts.seconds) * 1000);
  return new Intl.DateTimeFormat(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(d);
}

// Fetch data
async function fetchMappings() {
  isLoadingMappings.value = true;
  try {
    const resp = await listNamespaceMapping();
    mappings.value = resp.mappings;
  } catch (e) {
    handleError(e);
  } finally {
    isLoadingMappings.value = false;
  }
}

async function fetchAPIKeys() {
  isLoadingKeys.value = true;
  try {
    const resp = await listAPIKey();
    apiKeys.value = resp.apiKeys;
  } catch (e) {
    handleError(e);
  } finally {
    isLoadingKeys.value = false;
  }
}

async function fetchInstances() {
  try {
    const resp = await listInstances({ pageSize: 100 });
    instances.value = resp.instances;
  } catch (e) {
    handleError(e);
  }
}

// Namespace mapping actions
function openCreateMappingModal() {
  editingMapping.value = null;
  mappingForm.value = {
    namespace: "",
    instanceResourceId: "",
    databaseName: "",
  };
  showMappingModal.value = true;
}

function openEditMappingModal(m: NamespaceMappingResource) {
  editingMapping.value = m;
  mappingForm.value = {
    namespace: m.namespace,
    instanceResourceId: m.instanceResourceId,
    databaseName: m.databaseName,
  };
  showMappingModal.value = true;
}

function confirmDeleteMapping(m: NamespaceMappingResource) {
  mappingToDelete.value = m;
  showDeleteMappingModal.value = true;
}

async function handleSaveMapping() {
  if (!mappingForm.value.namespace || !mappingForm.value.instanceResourceId)
    return;
  isSavingMapping.value = true;
  try {
    if (editingMapping.value) {
      await updateNamespaceMapping(editingMapping.value.id, {
        namespace: mappingForm.value.namespace,
        instanceResourceId: mappingForm.value.instanceResourceId,
        databaseName: mappingForm.value.databaseName,
      });
    } else {
      await createNamespaceMapping({
        namespace: mappingForm.value.namespace,
        instanceResourceId: mappingForm.value.instanceResourceId,
        databaseName: mappingForm.value.databaseName,
      });
    }
    showMappingModal.value = false;
    await fetchMappings();
  } catch (e) {
    handleError(e);
  } finally {
    isSavingMapping.value = false;
  }
}

async function handleDeleteMapping() {
  if (!mappingToDelete.value) return;
  isDeletingMapping.value = true;
  try {
    await deleteNamespaceMapping(mappingToDelete.value.id);
    showDeleteMappingModal.value = false;
    await fetchMappings();
  } catch (e) {
    handleError(e);
  } finally {
    isDeletingMapping.value = false;
  }
}

// API key actions
function openCreateKeyModal() {
  keyForm.value = { description: "" };
  showCreateKeyModal.value = true;
}

function confirmRevokeKey(key: APIKeyResource) {
  keyToRevoke.value = key;
  showRevokeKeyModal.value = true;
}

async function handleCreateKey() {
  if (!keyForm.value.description) return;
  isCreatingKey.value = true;
  try {
    const resp = await createAPIKey(keyForm.value.description);
    showCreateKeyModal.value = false;
    createdKeyValue.value = resp.key;
    copied.value = false;
    showKeyResultModal.value = true;
    await fetchAPIKeys();
  } catch (e) {
    handleError(e);
  } finally {
    isCreatingKey.value = false;
  }
}

async function handleRevokeKey() {
  if (!keyToRevoke.value) return;
  isRevokingKey.value = true;
  try {
    await revokeAPIKey(keyToRevoke.value.id);
    showRevokeKeyModal.value = false;
    showSuccess(t("openlineageSettings.revokeAPIKey"));
    await fetchAPIKeys();
  } catch (e) {
    handleError(e);
  } finally {
    isRevokingKey.value = false;
  }
}

async function copyKey() {
  await navigator.clipboard.writeText(createdKeyValue.value);
  copied.value = true;
  setTimeout(() => {
    copied.value = false;
  }, 2000);
}

onMounted(() => {
  fetchMappings();
  fetchAPIKeys();
  fetchInstances();
});
</script>
