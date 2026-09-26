<template>
  <div class="space-y-4">
    <PageHeader :title="t('openlineageSettings.title')" />

    <Card>
      <CardHeader>
        <div class="flex items-center justify-between gap-4">
          <div>
            <CardTitle>{{ t("openlineage.title") }}</CardTitle>
            <CardDescription>{{
              t("openlineage.browseFromSettings")
            }}</CardDescription>
          </div>
          <div class="flex items-center gap-2">
            <Button size="sm" variant="outline" @click="router.push({ name: 'OpenLineageOverview' })">
              {{ t("openlineage.openOverview") }}
            </Button>
            <Button size="sm" @click="router.push({ name: 'OpenLineageTasks' })">
            <ScrollText class="h-4 w-4 mr-2" />
              {{ t("openlineage.openJobs") }}
            </Button>
          </div>
        </div>
      </CardHeader>
    </Card>

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
        <PageState :loading="isLoadingMappings">
          <EmptyState
            v-if="mappings.length === 0"
            :icon="Network"
            :title="t('openlineageSettings.noMappings')"
          />
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
                :key="m.name"
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
        </PageState>
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
        <PageState :loading="isLoadingKeys">
          <EmptyState
            v-if="apiKeys.length === 0"
            :icon="KeyRound"
            :title="t('openlineageSettings.noAPIKeys')"
          />
          <Table v-else>
            <TableHeader>
              <TableRow>
                <TableHead>{{
                  t("openlineageSettings.apiKeyDescription")
                }}</TableHead>
                <TableHead>{{
                  t("openlineageSettings.maskedAPIKey")
                }}</TableHead>
                <TableHead>{{
                  t("openlineageSettings.createdBy")
                }}</TableHead>
                <TableHead>{{
                  t("openlineageSettings.createdAt")
                }}</TableHead>
                <TableHead>{{
                  t("openlineageSettings.lastUsedAt")
                }}</TableHead>
                <TableHead>{{
                  t("openlineageSettings.keyScope")
                }}</TableHead>
                <TableHead class="text-right">
                  {{ t("openlineageSettings.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="key in apiKeys"
                :key="key.name"
                :class="{ 'opacity-60': key.revokedAt }"
              >
                <TableCell class="font-medium">
                  {{ key.description }}
                </TableCell>
                <TableCell class="font-mono text-sm">
                  {{ key.maskedKey || "-" }}
                </TableCell>
                <TableCell class="text-muted-foreground">
                  {{ key.createdBy || "-" }}
                </TableCell>
                <TableCell class="text-muted-foreground">
                  {{ formatTimestamp(key.createdAt) }}
                </TableCell>
                <TableCell class="text-muted-foreground">
                  {{ formatTimestamp(key.lastUsedAt) }}
                </TableCell>
                <TableCell class="font-mono text-sm">
                  {{ key.scopeNamespace || t("openlineageSettings.keyScopeAll") }}
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
        </PageState>
      </CardContent>
    </Card>

    <!-- Create/Edit Mapping Modal -->
    <Dialog v-model:open="showMappingModal">
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ editingMapping ? t("openlineageSettings.editMapping") : t("openlineageSettings.addMapping") }}</DialogTitle>
        </DialogHeader>

        <form @submit.prevent="handleSaveMapping">
          <div class="space-y-4">
            <FormField
              v-model="mappingForm.namespace"
              :label="t('openlineageSettings.namespace')"
              :placeholder="t('openlineageSettings.namespacePlaceholder')"
              required
            />
            <div>
              <label
                id="mapping-instance-label"
                class="text-sm font-medium leading-none"
              >
                {{ t("openlineageSettings.instanceResourceId") }}
              </label>
              <Select v-model="mappingForm.instanceResourceId">
                <SelectTrigger
                  aria-labelledby="mapping-instance-label"
                  class="mt-1.5 w-full"
                >
                  <SelectValue :placeholder="t('openlineageSettings.instanceResourceIdPlaceholder')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="inst in instanceStore.active"
                    :key="inst.name"
                    :value="extractResourceId(inst.name)"
                  >
                    {{ inst.title || inst.name }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <FormField
              v-model="mappingForm.databaseName"
              :label="t('openlineageSettings.databaseName')"
              :placeholder="t('openlineageSettings.databaseNamePlaceholder')"
            />
          </div>
        </form>
        <DialogFooter>
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
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Delete Mapping Confirmation -->
    <Dialog v-model:open="showDeleteMappingModal">
      <DialogContent class="max-w-sm">
        <DialogHeader>
          <DialogTitle>{{ t("openlineageSettings.deleteMapping") }}</DialogTitle>
        </DialogHeader>

        <div class="text-center">
          <div class="w-12 h-12 mx-auto mb-4 rounded-full bg-destructive/10 flex items-center justify-center">
            <Trash2 class="h-6 w-6 text-destructive" />
          </div>
          <p>{{ t("openlineageSettings.deleteMappingConfirm") }}</p>
          <p class="text-sm text-muted-foreground mt-2 font-mono">
            {{ mappingToDelete?.namespace }}
          </p>
        </div>
        <DialogFooter>
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
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Create API Key Modal -->
    <Dialog v-model:open="showCreateKeyModal">
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("openlineageSettings.createAPIKey") }}</DialogTitle>
        </DialogHeader>

        <form @submit.prevent="handleCreateKey">
          <div class="space-y-4">
            <FormField
              v-model="keyForm.description"
              :label="t('openlineageSettings.apiKeyDescription')"
              :placeholder="
                t('openlineageSettings.apiKeyDescriptionPlaceholder')
              "
              required
            />
            <FormField
              v-model="keyForm.scopeNamespace"
              :label="t('openlineageSettings.keyScope')"
              :placeholder="t('openlineageSettings.keyScopePlaceholder')"
            />
          </div>
        </form>
        <DialogFooter>
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
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Show Created Key Modal -->
    <Dialog v-model:open="showKeyResultModal">
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("openlineageSettings.apiKeyLabel") }}</DialogTitle>
        </DialogHeader>

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
        <DialogFooter>
          <Button @click="showKeyResultModal = false">
            {{ t("common.confirm") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Revoke Key Confirmation -->
    <Dialog v-model:open="showRevokeKeyModal">
      <DialogContent class="max-w-sm">
        <DialogHeader>
          <DialogTitle>{{ t("openlineageSettings.revokeAPIKey") }}</DialogTitle>
        </DialogHeader>

        <div class="text-center">
          <div class="w-12 h-12 mx-auto mb-4 rounded-full bg-destructive/10 flex items-center justify-center">
            <KeyRound class="h-6 w-6 text-destructive" />
          </div>
          <p>{{ t("openlineageSettings.revokeAPIKeyConfirm") }}</p>
          <p class="text-sm text-muted-foreground mt-2">
            {{ keyToRevoke?.description }}
          </p>
        </div>
        <DialogFooter>
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
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  Copy,
  KeyRound,
  Network,
  Pencil,
  Plus,
  ScrollText,
  Trash2,
} from "lucide-vue-next";
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import {
  createAPIKey,
  createNamespaceMapping,
  deleteNamespaceMapping,
  listAPIKeys,
  listNamespaceMappings,
  revokeAPIKey,
  updateNamespaceMapping,
} from "@/api/openlineage";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { FormField } from "@/components/ui/form-field";
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
import { useInstanceStore } from "@/store/modules/instance";
import type {
  APIKey,
  NamespaceMapping,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { formatDateTime } from "@/utils/datetime";

const { t, locale } = useI18n();
const { handleError, showSuccess } = useErrorHandler();
const router = useRouter();
const instanceStore = useInstanceStore();

// State
const isLoadingMappings = ref(false);
const isLoadingKeys = ref(false);
const isSavingMapping = ref(false);
const isDeletingMapping = ref(false);
const isCreatingKey = ref(false);
const isRevokingKey = ref(false);

const mappings = ref<NamespaceMapping[]>([]);
const apiKeys = ref<APIKey[]>([]);

// Mapping modals
const showMappingModal = ref(false);
const showDeleteMappingModal = ref(false);
const editingMapping = ref<NamespaceMapping | null>(null);
const mappingToDelete = ref<NamespaceMapping | null>(null);
const mappingForm = ref({
  namespace: "",
  instanceResourceId: "",
  databaseName: "",
});

// API key modals
const showCreateKeyModal = ref(false);
const showKeyResultModal = ref(false);
const showRevokeKeyModal = ref(false);
const keyToRevoke = ref<APIKey | null>(null);
const keyForm = ref({ description: "", scopeNamespace: "" });
const createdKeyValue = ref("");
const copied = ref(false);

function extractResourceId(name: string): string {
  return name.replace("instances/", "");
}

function getInstanceTitle(resourceId: string): string {
  return instanceStore.titleOf(resourceId);
}

function formatTimestamp(ts: Timestamp | undefined): string {
  return formatDateTime(ts, locale.value);
}

// Fetch data
async function fetchMappings() {
  isLoadingMappings.value = true;
  try {
    const resp = await listNamespaceMappings();
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
    const resp = await listAPIKeys();
    apiKeys.value = resp.apiKeys;
  } catch (e) {
    handleError(e);
  } finally {
    isLoadingKeys.value = false;
  }
}

async function fetchInstances() {
  try {
    await instanceStore.ensureLoaded();
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

function openEditMappingModal(m: NamespaceMapping) {
  editingMapping.value = m;
  mappingForm.value = {
    namespace: m.namespace,
    instanceResourceId: m.instanceResourceId,
    databaseName: m.databaseName,
  };
  showMappingModal.value = true;
}

function confirmDeleteMapping(m: NamespaceMapping) {
  mappingToDelete.value = m;
  showDeleteMappingModal.value = true;
}

async function handleSaveMapping() {
  if (!mappingForm.value.namespace || !mappingForm.value.instanceResourceId)
    return;
  isSavingMapping.value = true;
  try {
    if (editingMapping.value) {
      await updateNamespaceMapping(editingMapping.value.name, {
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
    await deleteNamespaceMapping(mappingToDelete.value.name);
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
  keyForm.value = { description: "", scopeNamespace: "" };
  showCreateKeyModal.value = true;
}

function confirmRevokeKey(key: APIKey) {
  keyToRevoke.value = key;
  showRevokeKeyModal.value = true;
}

async function handleCreateKey() {
  if (!keyForm.value.description) return;
  isCreatingKey.value = true;
  try {
    const resp = await createAPIKey(
      keyForm.value.description,
      keyForm.value.scopeNamespace
    );
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
    await revokeAPIKey(keyToRevoke.value.name);
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
