<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('openlineageSettings.title')"
      :description="t('openlineageSettings.pageDescription')"
    />

    <!-- The ingestion address. It comes from the configured external URL, so a
         deployment behind a proxy advertises the address its producers reach. -->
    <Card>
      <CardHeader>
        <CardTitle>{{ t("openlineageSettings.endpoint") }}</CardTitle>
        <CardDescription>{{
          t("openlineageSettings.endpointDescription")
        }}</CardDescription>
      </CardHeader>
      <CardContent class="space-y-3">
        <div class="flex items-center gap-2">
          <code
            class="min-w-0 flex-1 truncate rounded-md bg-muted px-3 py-2 font-mono text-sm"
            :title="endpointUrl"
          >
            {{ endpointUrl }}
          </code>
          <Button
            variant="outline"
            size="sm"
            class="shrink-0"
            @click="copySnippet(endpointUrl, 'endpoint')"
          >
            <Check
              v-if="copiedSnippet === 'endpoint'"
              class="h-4 w-4 mr-1.5 text-green-600"
            />
            <Copy
              v-else
              class="h-4 w-4 mr-1.5"
            />
            {{
              copiedSnippet === "endpoint"
                ? t("common.copied")
                : t("common.copy")
            }}
          </Button>
        </div>

        <!-- `pre-wrap`, not a horizontal scroller: a nested scroll container is
             exactly what the layout audit forbids. -->
        <div class="relative">
          <pre
            class="whitespace-pre-wrap break-all rounded-md border bg-muted/50 p-3 pr-12 font-mono text-xs leading-5"
          >{{ curlSnippet }}</pre>
          <Button
            variant="ghost"
            size="icon"
            class="absolute right-1 top-1 h-8 w-8"
            :title="t('common.copy')"
            @click="copySnippet(curlSnippet, 'curl')"
          >
            <Check
              v-if="copiedSnippet === 'curl'"
              class="h-4 w-4 text-green-600"
            />
            <Copy
              v-else
              class="h-4 w-4 text-muted-foreground"
            />
          </Button>
        </div>

        <p
          v-if="externalUrlMissing"
          class="text-xs text-muted-foreground"
        >
          {{ t("openlineageSettings.endpointOriginFallback") }}
          <router-link
            class="font-medium text-primary underline underline-offset-4"
            to="/settings/general"
          >
            {{ t("openlineageSettings.configureExternalUrl") }}
          </router-link>
        </p>
      </CardContent>
    </Card>

    <!-- Namespace Mappings Section -->
    <Card>
      <CardHeader>
        <div class="flex flex-wrap items-start justify-between gap-3">
          <!-- `space-y-1.5` is repeated here because CardHeader only spaces its
               direct children, and these two are nested one level down. -->
          <div class="space-y-1.5">
            <CardTitle>{{
              t("openlineageSettings.namespaceMappings")
            }}</CardTitle>
            <CardDescription>{{
              t("openlineageSettings.namespaceMappingsDescription")
            }}</CardDescription>
          </div>
          <Button
            size="sm"
            class="shrink-0"
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
            :description="t('openlineageSettings.noMappingsHint')"
          />
          <!-- `table-fixed` with proportional columns: a namespace is a long
               unbreakable URL and an instance title is free text, so auto
               layout has no way to bound the table below its content width.
               Fixed columns cannot overflow the card, whatever the values. -->
          <Table
            v-else
            class="table-fixed"
          >
            <TableHeader>
              <TableRow>
                <TableHead class="w-[42%]">{{
                  t("openlineageSettings.namespace")
                }}</TableHead>
                <TableHead class="w-[24%]">{{
                  t("openlineageSettings.instanceResourceId")
                }}</TableHead>
                <TableHead class="w-[18%]">{{
                  t("openlineageSettings.databaseName")
                }}</TableHead>
                <TableHead class="w-[16%] text-right">
                  {{ t("openlineageSettings.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="m in mappings"
                :key="m.name"
              >
                <!-- The ellipsis lives on a block inside the cell: `text-overflow`
                     does not apply to a table cell, so `truncate` on the `<td>`
                     would clip the value mid-character instead. -->
                <TableCell>
                  <div
                    class="truncate font-mono text-sm"
                    :title="m.namespace"
                  >
                    {{ m.namespace }}
                  </div>
                </TableCell>
                <TableCell>
                  <div
                    class="truncate"
                    :title="getInstanceTitle(m.instanceResourceId)"
                  >
                    {{ getInstanceTitle(m.instanceResourceId) }}
                  </div>
                </TableCell>
                <TableCell class="text-muted-foreground">
                  <div
                    class="truncate"
                    :title="m.databaseName"
                  >
                    {{ m.databaseName || "-" }}
                  </div>
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
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="space-y-1.5">
            <CardTitle>{{ t("openlineageSettings.apiKeys") }}</CardTitle>
            <CardDescription>{{
              t("openlineageSettings.apiKeysDescription")
            }}</CardDescription>
          </div>
          <Button
            size="sm"
            class="shrink-0"
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
            :description="t('openlineageSettings.noAPIKeysHint')"
          />
          <!--
            A row list, not a table: a real mask is 67 characters, and as a table
            cell it widened the table to 1203px inside an 810px column, pushing
            Namespace scope and the revoke action behind a horizontal scrollbar
            (820px showed two of the seven columns).
          -->
          <ul
            v-else
            class="divide-y"
          >
            <li
              v-for="key in apiKeys"
              :key="key.name"
              class="flex items-start justify-between gap-4 py-4 first:pt-0 last:pb-0"
            >
              <div class="min-w-0 space-y-1">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="truncate font-medium">{{ key.description }}</span>
                  <Badge
                    v-if="key.revokedAt"
                    variant="secondary"
                    class="shrink-0"
                  >
                    {{ t("openlineageSettings.revoked") }}
                  </Badge>
                </div>

                <p
                  v-if="key.maskedKey"
                  class="truncate font-mono text-xs text-muted-foreground"
                  :title="key.maskedKey"
                >
                  {{ compactMask(key.maskedKey) }}
                </p>

                <div
                  class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground"
                >
                  <Badge
                    variant="outline"
                    class="shrink-0 font-normal"
                  >
                    {{ key.scopeNamespace || t("openlineageSettings.keyScopeAll") }}
                  </Badge>
                  <span>
                    {{ t("openlineageSettings.createdBy") }}:
                    {{ key.createdBy || "-" }}
                  </span>
                  <span>
                    {{ t("openlineageSettings.createdAt") }}:
                    {{ formatTimestamp(key.createdAt) }}
                  </span>
                  <span>
                    {{ t("openlineageSettings.lastUsedAt") }}:
                    {{
                      key.lastUsedAt
                        ? formatTimestamp(key.lastUsedAt)
                        : t("openlineageSettings.neverUsed")
                    }}
                  </span>
                  <span v-if="key.revokedAt">
                    {{ t("openlineageSettings.revokedAt") }}:
                    {{ formatTimestamp(key.revokedAt) }}
                  </span>
                </div>
              </div>

              <!-- The revoked state keeps the action slot occupied so the row's
                   affordance column stays aligned, and a `span` carries the
                   tooltip because a disabled button does not receive the
                   pointer events a native tooltip needs. -->
              <span
                v-if="key.revokedAt"
                class="flex h-10 w-10 shrink-0 items-center justify-center text-muted-foreground/30"
                :title="t('openlineageSettings.alreadyRevoked')"
              >
                <Ban class="h-4 w-4" />
              </span>
              <Button
                v-else
                variant="ghost"
                size="icon"
                class="shrink-0"
                :title="t('openlineageSettings.revokeAPIKey')"
                @click="confirmRevokeKey(key)"
              >
                <Ban class="h-4 w-4 text-muted-foreground hover:text-destructive" />
              </Button>
            </li>
          </ul>
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
            <!-- `update:model-value`, not `blur`: the message appears once the
                 field has been changed, and a listener on the wrapper component
                 is the one that reliably reaches the input inside. -->
            <FormField
              v-model="mappingForm.namespace"
              :label="t('openlineageSettings.namespace')"
              :placeholder="t('openlineageSettings.namespacePlaceholder')"
              :error="mappingNamespaceError"
              required
              @update:model-value="mappingNamespaceTouched = true"
            />
            <div class="w-full space-y-2.5">
              <Label
                id="mapping-instance-label"
                for="mapping-instance"
              >
                {{ t("openlineageSettings.instanceResourceId") }}
                <span class="text-destructive">*</span>
              </Label>
              <Select
                v-model="mappingForm.instanceResourceId"
                @update:model-value="mappingInstanceTouched = true"
              >
                <SelectTrigger
                  id="mapping-instance"
                  aria-labelledby="mapping-instance-label"
                  :aria-required="true"
                  :aria-invalid="mappingInstanceError ? true : undefined"
                  class="w-full"
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
              <!-- An empty workspace leaves the required select with nothing to
                   pick, which reads as a broken dialog: say why Save is off and
                   where instances come from. -->
              <p
                v-if="instanceStore.active.length === 0"
                class="text-sm text-muted-foreground"
              >
                {{ t("openlineageSettings.noInstancesAvailable") }}
                <router-link
                  class="font-medium text-primary underline underline-offset-4"
                  to="/instances"
                >
                  {{ t("openlineageSettings.addInstance") }}
                </router-link>
              </p>
              <p
                v-else-if="mappingInstanceError"
                class="text-sm text-destructive"
              >
                {{ mappingInstanceError }}
              </p>
            </div>
            <FormField
              v-model="mappingForm.databaseName"
              :label="t('openlineageSettings.databaseName')"
              :placeholder="t('openlineageSettings.databaseNamePlaceholder')"
              :hint="t('openlineageSettings.databaseNameHint')"
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
            :disabled="isSavingMapping || !canSaveMapping"
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
          <p class="text-sm text-muted-foreground mt-2 font-mono break-all">
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
              :hint="t('openlineageSettings.keyScopeHint')"
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
            :disabled="isCreatingKey || !keyForm.description.trim()"
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
            <code class="flex-1 min-w-0 rounded-md bg-muted p-3 text-sm font-mono break-all select-all">
              {{ createdKeyValue }}
            </code>
            <Button
              variant="outline"
              size="sm"
              class="shrink-0"
              @click="copyKey"
            >
              <Copy class="h-4 w-4 mr-1" />
              {{ copied ? t("common.copied") : t("common.copy") }}
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
          <p class="text-sm text-muted-foreground mt-2 break-all">
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
  Ban,
  Check,
  Copy,
  KeyRound,
  Network,
  Pencil,
  Plus,
  Trash2,
} from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  createAPIKey,
  createNamespaceMapping,
  deleteNamespaceMapping,
  listAPIKeys,
  listNamespaceMappings,
  revokeAPIKey,
  updateNamespaceMapping,
} from "@/api/openlineage";
import { getWorkspaceProfileSetting } from "@/api/setting";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
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
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { FormField } from "@/components/ui/form-field";
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
import { useInstanceStore } from "@/store/modules/instance";
import type {
  APIKey,
  NamespaceMapping,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { formatDateTime } from "@/utils/datetime";

const { t, locale } = useI18n();
const { handleError, showSuccess } = useErrorHandler();
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
const mappingNamespaceTouched = ref(false);
const mappingInstanceTouched = ref(false);

// API key modals
const showCreateKeyModal = ref(false);
const showKeyResultModal = ref(false);
const showRevokeKeyModal = ref(false);
const keyToRevoke = ref<APIKey | null>(null);
const keyForm = ref({ description: "", scopeNamespace: "" });
const createdKeyValue = ref("");
const copied = ref(false);

// Ingestion endpoint
const externalUrl = ref("");
const copiedSnippet = ref("");

const endpointBase = computed(() =>
  (externalUrl.value || window.location.origin).replace(/\/+$/, "")
);
const endpointUrl = computed(() => `${endpointBase.value}/api/v1/lineage`);
const externalUrlMissing = computed(() => !externalUrl.value);
const curlSnippet = computed(() =>
  [
    `curl -X POST '${endpointUrl.value}' \\`,
    "  -H 'Content-Type: application/json' \\",
    "  -H 'Authorization: Bearer <API_KEY>' \\",
    "  -d @event.json",
  ].join("\n")
);

const mappingNamespaceError = computed(() =>
  mappingNamespaceTouched.value && !mappingForm.value.namespace.trim()
    ? t("openlineageSettings.namespaceRequired")
    : ""
);
const mappingInstanceError = computed(() =>
  mappingInstanceTouched.value && !mappingForm.value.instanceResourceId
    ? t("openlineageSettings.instanceRequired")
    : ""
);
const canSaveMapping = computed(
  () =>
    Boolean(mappingForm.value.namespace.trim()) &&
    Boolean(mappingForm.value.instanceResourceId)
);

function extractResourceId(name: string): string {
  return name.replace("instances/", "");
}

function getInstanceTitle(resourceId: string): string {
  return instanceStore.titleOf(resourceId);
}

function formatTimestamp(ts: Timestamp | undefined): string {
  return formatDateTime(ts, locale.value);
}

/**
 * The mask keeps eight leading and nine trailing characters of a 67-character
 * key. Rows show the compact form and keep the full mask in the tooltip.
 */
function compactMask(masked: string): string {
  const visible = 8 + 9;
  if (masked.length <= visible + 1) {
    return masked;
  }
  return `${masked.slice(0, 8)}…${masked.slice(-9)}`;
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

// The endpoint address is supplementary: a member who may manage ingestion keys
// does not necessarily hold the settings permission, so a failure to read the
// external URL falls back to the browser origin instead of surfacing an error.
async function fetchExternalUrl() {
  try {
    const setting = await getWorkspaceProfileSetting();
    externalUrl.value = setting.externalUrl;
  } catch {
    externalUrl.value = "";
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
  mappingNamespaceTouched.value = false;
  mappingInstanceTouched.value = false;
  showMappingModal.value = true;
}

function openEditMappingModal(m: NamespaceMapping) {
  editingMapping.value = m;
  mappingForm.value = {
    namespace: m.namespace,
    instanceResourceId: m.instanceResourceId,
    databaseName: m.databaseName,
  };
  mappingNamespaceTouched.value = false;
  mappingInstanceTouched.value = false;
  showMappingModal.value = true;
}

function confirmDeleteMapping(m: NamespaceMapping) {
  mappingToDelete.value = m;
  showDeleteMappingModal.value = true;
}

async function handleSaveMapping() {
  mappingNamespaceTouched.value = true;
  mappingInstanceTouched.value = true;
  if (!canSaveMapping.value) return;
  isSavingMapping.value = true;
  try {
    const mapping = {
      namespace: mappingForm.value.namespace.trim(),
      instanceResourceId: mappingForm.value.instanceResourceId,
      databaseName: mappingForm.value.databaseName.trim(),
    };
    if (editingMapping.value) {
      await updateNamespaceMapping(editingMapping.value.name, mapping);
    } else {
      await createNamespaceMapping(mapping);
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
  if (!keyForm.value.description.trim()) return;
  isCreatingKey.value = true;
  try {
    const resp = await createAPIKey(
      keyForm.value.description.trim(),
      keyForm.value.scopeNamespace.trim()
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

async function copySnippet(text: string, id: string) {
  await navigator.clipboard.writeText(text);
  copiedSnippet.value = id;
  setTimeout(() => {
    if (copiedSnippet.value === id) {
      copiedSnippet.value = "";
    }
  }, 2000);
}

onMounted(() => {
  fetchMappings();
  fetchAPIKeys();
  fetchInstances();
  fetchExternalUrl();
});
</script>
