<template>
  <div class="space-y-4">
    <PageHeader :title="t('instanceManagement.title')">
      <template #actions>
        <Button
          v-if="canCreate"
          @click="openCreateModal"
        >
          <Plus class="h-4 w-4 mr-2" />
          {{ t("instanceManagement.addInstance") }}
        </Button>
      </template>
    </PageHeader>

    <!-- Search Bar -->
    <div class="flex items-center gap-4">
      <div class="flex-1">
        <AppInput
          v-model="searchQuery"
          :placeholder="t('instanceManagement.searchPlaceholder')"
          @update:model-value="debouncedSearch"
        >
          <template #suffix>
            <Search class="absolute right-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          </template>
        </AppInput>
      </div>
    </div>

    <!-- Instances Table -->
    <Card>
      <PageState
        :loading="isLoading"
        :error="error"
      >
        <!-- Empty State -->
        <EmptyState
          v-if="activeInstances.length === 0"
          :icon="Database"
          :title="t('instanceManagement.noInstances')"
        />

        <!-- Instances List -->
        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("instanceManagement.instance") }}</TableHead>
              <TableHead>{{ t("instanceManagement.engine") }}</TableHead>
              <TableHead>{{ t("instanceManagement.host") }}</TableHead>
              <TableHead>{{ t("instanceManagement.environment") }}</TableHead>
              <TableHead>{{ t("instanceManagement.status") }}</TableHead>
              <TableHead>{{ t("instanceManagement.lastSync") }}</TableHead>
              <TableHead class="text-right">
                {{ t("instanceManagement.actions") }}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="instance in activeInstances"
              :key="instance.name"
              class="cursor-pointer hover:bg-muted/50"
              @click="navigateToInstanceDetail(instance)"
            >
              <TableCell>
                <div class="flex items-center">
                  <div
                    class="w-10 h-10 rounded-full flex items-center justify-center"
                    :class="engineBgClass(instance.engine)"
                  >
                    <span
                      class="font-semibold text-sm"
                      :class="engineTextClass(instance.engine)"
                    >
                      {{ engineIcon(instance.engine) }}
                    </span>
                  </div>
                  <div class="ml-4">
                    <div class="font-medium">
                      {{ instance.title || "-" }}
                    </div>
                    <div class="text-sm text-muted-foreground">
                      {{ getInstanceId(instance.name) }}
                    </div>
                  </div>
                </div>
              </TableCell>
              <TableCell>
                <Badge
                  variant="secondary"
                  :class="engineBadgeClass(instance.engine)"
                >
                  {{ engineLabel(instance.engine) }}
                </Badge>
              </TableCell>
              <TableCell class="text-muted-foreground">
                {{ getHostInfo(instance) }}
              </TableCell>
              <TableCell class="text-muted-foreground">
                {{ getEnvironmentLabel(instance.environment) }}
              </TableCell>
              <TableCell>
                <Badge :variant="instance.activation ? 'success' : 'secondary'">
                  {{ instance.activation ? t("instanceManagement.active") : t("instanceManagement.inactive") }}
                </Badge>
              </TableCell>
              <TableCell class="text-muted-foreground">
                {{ formatLastSync(instance.lastSyncTime) }}
              </TableCell>
              <TableCell class="text-right">
                <Button
                  v-if="canDelete"
                  variant="ghost"
                  size="icon"
                  :title="t('common.delete')"
                  @click.stop="confirmDelete(instance)"
                >
                  <Trash2 class="h-4 w-4 text-muted-foreground hover:text-destructive" />
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </PageState>
    </Card>

    <!-- Deleted Instances (Recycle Bin) -->
    <Collapsible
      v-model:open="showRecycleBin"
      class="w-full"
    >
      <Card>
        <CollapsibleTrigger class="w-full">
          <div class="flex items-center justify-between px-6 py-4 cursor-pointer hover:bg-muted/50 transition-colors">
            <div class="flex items-center gap-3">
              <Trash2 class="h-5 w-5 text-muted-foreground" />
              <h2 class="text-lg font-semibold">
                {{ t("instanceManagement.recycleBin") }}
              </h2>
            </div>
            <ChevronDown
              :class="[
                'h-5 w-5 text-muted-foreground transition-transform',
                showRecycleBin ? 'rotate-180' : '',
              ]"
            />
          </div>
        </CollapsibleTrigger>

        <CollapsibleContent>
          <PageState :loading="isLoadingDeleted">
            <!-- Empty State -->
            <EmptyState
              v-if="deletedInstances.length === 0"
              :icon="RotateCcw"
              :title="t('instanceManagement.noDeletedInstances')"
            />

            <!-- Deleted Instances List -->
            <Table v-else>
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t("instanceManagement.instance") }}</TableHead>
                  <TableHead>{{ t("instanceManagement.engine") }}</TableHead>
                  <TableHead>{{ t("instanceManagement.host") }}</TableHead>
                  <TableHead class="text-right">
                    {{ t("instanceManagement.actions") }}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow
                  v-for="instance in deletedInstances"
                  :key="instance.name"
                  class="opacity-60"
                >
                  <TableCell>
                    <div class="flex items-center">
                      <div class="w-10 h-10 rounded-full bg-muted flex items-center justify-center">
                        <span class="text-muted-foreground font-semibold text-sm">
                          {{ engineIcon(instance.engine) }}
                        </span>
                      </div>
                      <div class="ml-4">
                        <div class="font-medium text-muted-foreground">
                          {{ instance.title || "-" }}
                        </div>
                        <div class="text-sm text-muted-foreground/70">
                          {{ getInstanceId(instance.name) }}
                        </div>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="secondary">
                      {{ engineLabel(instance.engine) }}
                    </Badge>
                  </TableCell>
                  <TableCell class="text-muted-foreground">
                    {{ getHostInfo(instance) }}
                  </TableCell>
                  <TableCell class="text-right">
                    <Button
                      variant="secondary"
                      size="sm"
                      :disabled="restoringInstance === instance.name"
                      @click="restoreInstance(instance)"
                    >
                      <RotateCcw class="h-4 w-4 mr-1" />
                      {{ t("instanceManagement.restore") }}
                    </Button>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </PageState>
        </CollapsibleContent>
      </Card>
    </Collapsible>

    <!-- Delete Confirmation Modal -->
    <AppModal
      v-model="showDeleteModal"
      :title="t('instanceManagement.deleteInstance')"
      size="sm"
    >
      <div class="text-center">
        <div class="w-12 h-12 mx-auto mb-4 rounded-full bg-destructive/10 flex items-center justify-center">
          <Trash2 class="h-6 w-6 text-destructive" />
        </div>
        <p>
          {{ t("instanceManagement.deleteConfirmMessage") }}
        </p>
        <p class="text-sm text-muted-foreground mt-2">
          <strong>{{ instanceToDelete?.title || instanceToDelete?.name }}</strong>
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
          @click="handleDeleteInstance"
        >
          {{ t("common.delete") }}
        </Button>
      </template>
    </AppModal>

    <!-- Create Instance Modal -->
    <AppModal
      v-model="showCreateModal"
      :title="t('instanceManagement.addInstance')"
      size="lg"
    >
      <form
        class="space-y-6"
        @submit.prevent="handleCreateInstance"
      >
        <!-- Basic Info Section -->
        <div class="space-y-4">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
            {{ t("instanceManagement.basicInfo") }}
          </h3>

          <div class="grid grid-cols-2 gap-4">
            <AppInput
              v-model="createForm.title"
              :label="t('instanceManagement.instanceTitle')"
              :placeholder="t('instanceManagement.instanceTitlePlaceholder')"
              required
              :error="createFormErrors.title"
            />
            <AppInput
              v-model="createForm.instanceId"
              :label="t('instanceManagement.instanceId')"
              :placeholder="t('instanceManagement.instanceIdPlaceholder')"
              required
              :error="createFormErrors.instanceId"
            />
          </div>

          <div class="grid grid-cols-2 gap-4">
            <div class="space-y-2">
              <Label>
                {{ t("instanceManagement.engine") }} <span class="text-destructive">*</span>
              </Label>
              <Select v-model="createForm.engine">
                <SelectTrigger>
                  <SelectValue :placeholder="t('instanceManagement.selectEngine')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem :value="String(Engine.MYSQL)">
                    MySQL
                  </SelectItem>
                  <SelectItem :value="String(Engine.POSTGRES)">
                    PostgreSQL
                  </SelectItem>
                  <SelectItem :value="String(Engine.MSSQL)">
                    SQL Server
                  </SelectItem>
                  <SelectItem :value="String(Engine.STARROCKS)">
                    StarRocks
                  </SelectItem>
                  <SelectItem :value="String(Engine.DORIS)">
                    Doris
                  </SelectItem>
                </SelectContent>
              </Select>
              <p
                v-if="createFormErrors.engine"
                class="text-sm text-destructive"
              >
                {{ createFormErrors.engine }}
              </p>
            </div>
            <EnvironmentSelect
              v-model="createForm.environment"
              :label="t('instanceManagement.environment')"
              :placeholder="t('instanceManagement.environmentPlaceholder')"
              required
              :error="createFormErrors.environment"
            />
          </div>

          <div class="flex items-center gap-2">
            <Checkbox
              id="activation"
              :checked="createForm.activation"
              @update:checked="createForm.activation = $event"
            />
            <Label
              for="activation"
              class="text-sm cursor-pointer"
            >
              {{ t("instanceManagement.activateInstance") }}
            </Label>
          </div>

          <!-- Sync Interval: toggle + minutes input -->
          <div class="space-y-2">
            <Label class="text-sm">{{ t("instanceManagement.syncInterval") }}</Label>
            <div class="flex items-center gap-4">
              <div class="flex items-center gap-2">
                <Checkbox
                  id="create-enable-sync"
                  :checked="createForm.enableSync"
                  @update:checked="onCreateSyncToggle"
                />
                <Label
                  for="create-enable-sync"
                  class="text-sm cursor-pointer"
                >
                  {{ t("instanceManagement.enableSync") }}
                </Label>
              </div>
              <div class="flex items-center gap-2">
                <AppInput
                  v-model="createForm.syncIntervalMinutes"
                  type="number"
                  :placeholder="t('instanceManagement.syncIntervalPlaceholder')"
                  :disabled="!createForm.enableSync"
                  class="w-32"
                />
                <span class="text-sm text-muted-foreground">{{ t("instanceManagement.syncIntervalMinutes") }}</span>
              </div>
            </div>
            <p class="text-xs text-muted-foreground">
              {{ t("instanceManagement.syncIntervalHint") }}
            </p>
          </div>
        </div>

        <!-- Admin Data Source Section (Required) -->
        <div class="space-y-4">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
            {{ t("instanceManagement.adminDataSource") }}
          </h3>

          <AppInput
            v-model="createForm.adminDataSource.id"
            :label="t('instanceManagement.dataSourceId')"
            :placeholder="t('instanceManagement.dataSourceIdPlaceholder')"
            required
            :error="createFormErrors.adminDataSource.id"
          />

          <div class="grid grid-cols-2 gap-4">
            <AppInput
              v-model="createForm.adminDataSource.host"
              :label="t('instanceManagement.host')"
              :placeholder="t('instanceManagement.hostPlaceholder')"
              required
              :error="createFormErrors.adminDataSource.host"
            />
            <AppInput
              v-model="createForm.adminDataSource.port"
              :label="t('instanceManagement.port')"
              :placeholder="t('instanceManagement.portPlaceholder')"
              required
              :error="createFormErrors.adminDataSource.port"
            />
          </div>

          <div class="grid grid-cols-2 gap-4">
            <AppInput
              v-model="createForm.adminDataSource.username"
              :label="t('instanceManagement.username')"
              :placeholder="t('instanceManagement.usernamePlaceholder')"
              required
              :error="createFormErrors.adminDataSource.username"
            />
            <AppInput
              v-model="createForm.adminDataSource.password"
              type="password"
              :label="t('instanceManagement.password')"
              :placeholder="t('instanceManagement.passwordPlaceholder')"
              required
              :error="createFormErrors.adminDataSource.password"
            />
          </div>

          <AppInput
            v-model="createForm.adminDataSource.database"
            :label="t('instanceManagement.database')"
            :placeholder="t('instanceManagement.databasePlaceholder')"
          />
        </div>

        <!-- Read-Only Data Sources Section (Optional) -->
        <div class="space-y-4">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
              {{ t("instanceManagement.readOnlyDataSources") }}
            </h3>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              @click="addReadOnlyDataSource"
            >
              <Plus class="h-4 w-4 mr-1" />
              {{ t("instanceManagement.addReadOnlyNode") }}
            </Button>
          </div>

          <div
            v-if="createForm.readOnlyDataSources.length === 0"
            class="text-sm text-muted-foreground italic"
          >
            {{ t("instanceManagement.noReadOnlyNodes") }}
          </div>

          <div
            v-for="(ds, index) in createForm.readOnlyDataSources"
            :key="index"
            class="border rounded-lg p-4 space-y-4 relative"
          >
            <div class="flex items-center justify-between">
              <span class="text-sm font-medium text-muted-foreground">
                {{ t("instanceManagement.readOnlyNode") }} #{{ index + 1 }}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                class="text-destructive hover:text-destructive"
                @click="removeReadOnlyDataSource(index)"
              >
                <Trash2 class="h-4 w-4" />
              </Button>
            </div>

            <AppInput
              v-model="ds.id"
              :label="t('instanceManagement.dataSourceId')"
              :placeholder="t('instanceManagement.dataSourceIdPlaceholder')"
              required
              :error="createFormErrors.readOnlyDataSources[index]?.id"
            />

            <div class="grid grid-cols-2 gap-4">
              <AppInput
                v-model="ds.host"
                :label="t('instanceManagement.host')"
                :placeholder="t('instanceManagement.hostPlaceholder')"
                required
                :error="createFormErrors.readOnlyDataSources[index]?.host"
              />
              <AppInput
                v-model="ds.port"
                :label="t('instanceManagement.port')"
                :placeholder="t('instanceManagement.portPlaceholder')"
                required
                :error="createFormErrors.readOnlyDataSources[index]?.port"
              />
            </div>

            <div class="grid grid-cols-2 gap-4">
              <AppInput
                v-model="ds.username"
                :label="t('instanceManagement.username')"
                :placeholder="t('instanceManagement.usernamePlaceholder')"
                required
                :error="createFormErrors.readOnlyDataSources[index]?.username"
              />
              <AppInput
                v-model="ds.password"
                type="password"
                :label="t('instanceManagement.password')"
                :placeholder="t('instanceManagement.passwordPlaceholder')"
                required
                :error="createFormErrors.readOnlyDataSources[index]?.password"
              />
            </div>

            <AppInput
              v-model="ds.database"
              :label="t('instanceManagement.database')"
              :placeholder="t('instanceManagement.databasePlaceholder')"
            />
          </div>
        </div>
      </form>
      <template #footer>
        <Button
          variant="outline"
          class="sm:mr-auto"
          :disabled="isCreating || isTestingConnection"
          @click="handleTestConnection"
        >
          <Loader2
            v-if="isTestingConnection"
            class="h-4 w-4 mr-2 animate-spin"
          />
          {{ isTestingConnection ? t("instanceManagement.testing") : t("instanceManagement.testConnection") }}
        </Button>
        <Button
          variant="outline"
          @click="showCreateModal = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          :disabled="isCreating || isTestingConnection"
          @click="handleCreateInstance"
        >
          {{ t("common.confirm") }}
        </Button>
      </template>
    </AppModal>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  ChevronDown,
  Database,
  Loader2,
  Plus,
  RotateCcw,
  Search,
  Trash2,
} from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import type { CreateInstanceInput } from "@/api/instance";
import {
  createInstance,
  deleteInstance,
  listInstances,
  undeleteInstance,
} from "@/api/instance";
import AppInput from "@/components/common/AppInput.vue";
import AppModal from "@/components/common/AppModal.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import EnvironmentSelect from "@/components/common/EnvironmentSelect.vue";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
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
import { useAuthStore } from "@/store/modules/auth";
import { useEnvironmentStore } from "@/store/modules/environment";
import { Engine, State } from "@/types/proto-es/v1/common_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";
import { DataSourceType } from "@/types/proto-es/v1/instance_service_pb";
import {
  engineBadgeClass,
  engineBgClass,
  engineIcon,
  engineLabel,
  engineTextClass,
} from "@/utils/engine";

const { t, locale } = useI18n();
const router = useRouter();
const authStore = useAuthStore();
const environmentStore = useEnvironmentStore();
const { handleError, showSuccess } = useErrorHandler();

// Instances and their data sources are administered by holders of the
// instance write permissions; members keep read-only access.
const canCreate = computed(() =>
  authStore.hasPermission("metaxisdata.instances.create")
);
const canDelete = computed(() =>
  authStore.hasPermission("metaxisdata.instances.delete")
);

// State
const isLoading = ref(false);
const isLoadingDeleted = ref(false);
const isCreating = ref(false);
const isTestingConnection = ref(false);
const isDeleting = ref(false);
const restoringInstance = ref<string | null>(null);
const error = ref<string | null>(null);
const searchQuery = ref("");
const showRecycleBin = ref(false);
const instances = ref<Instance[]>([]);
const deletedInstances = ref<Instance[]>([]);

// Modals
const showCreateModal = ref(false);
const showDeleteModal = ref(false);
const instanceToDelete = ref<Instance | null>(null);

// Create Form
// Data source form type
interface DataSourceForm {
  id: string;
  host: string;
  port: string;
  username: string;
  password: string;
  database: string;
}

interface DataSourceErrors {
  id: string;
  host: string;
  port: string;
  username: string;
  password: string;
}

function createEmptyDataSource(): DataSourceForm {
  return {
    id: "",
    host: "",
    port: "",
    username: "",
    password: "",
    database: "",
  };
}

function createEmptyDataSourceErrors(): DataSourceErrors {
  return {
    id: "",
    host: "",
    port: "",
    username: "",
    password: "",
  };
}

const createForm = ref({
  title: "",
  instanceId: "",
  engine: "",
  environment: "",
  activation: true,
  enableSync: false,
  syncIntervalMinutes: "15",
  adminDataSource: {
    id: "admin",
    host: "",
    port: "",
    username: "",
    password: "",
    database: "",
  } as DataSourceForm,
  readOnlyDataSources: [] as DataSourceForm[],
});

const createFormErrors = ref({
  title: "",
  instanceId: "",
  engine: "",
  environment: "",
  adminDataSource: createEmptyDataSourceErrors(),
  readOnlyDataSources: [] as DataSourceErrors[],
});

// Add a read-only data source
function addReadOnlyDataSource() {
  createForm.value.readOnlyDataSources.push(createEmptyDataSource());
  createFormErrors.value.readOnlyDataSources.push(
    createEmptyDataSourceErrors()
  );
}

// Remove a read-only data source
function removeReadOnlyDataSource(index: number) {
  createForm.value.readOnlyDataSources.splice(index, 1);
  createFormErrors.value.readOnlyDataSources.splice(index, 1);
}

// Computed
const activeInstances = computed(() => {
  return instances.value.filter((i) => i.state !== State.DELETED);
});

// Methods
function getInstanceId(name: string): string {
  // Format: instances/{id} -> return id
  return name.replace("instances/", "");
}

function navigateToInstanceDetail(instance: Instance) {
  const instanceId = getInstanceId(instance.name);
  router.push({ name: "InstanceDetail", params: { instanceId } });
}

function getEnvironmentLabel(environment: string): string {
  if (!environment) return "-";
  return environmentStore.titleOf(environment);
}

function getHostInfo(instance: Instance): string {
  const adminDataSource = instance.dataSources.find((ds) => ds.type === 1); // ADMIN type
  if (adminDataSource) {
    const port = adminDataSource.port ? `:${adminDataSource.port}` : "";
    return `${adminDataSource.host}${port}`;
  }
  return "-";
}

function formatLastSync(timestamp: Timestamp | undefined): string {
  if (!timestamp?.seconds) return "-";
  const d = new Date(Number(timestamp.seconds) * 1000);
  const formatter = new Intl.DateTimeFormat(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });

  return formatter.format(d);
}
async function fetchInstances() {
  isLoading.value = true;
  error.value = null;
  try {
    const response = await listInstances({
      pageSize: 100,
      showDeleted: false,
      filter: searchQuery.value
        ? `title.matches("${searchQuery.value}") || name.matches("${searchQuery.value}")`
        : "",
    });
    instances.value = response.instances;
  } catch (e) {
    error.value = t("instanceManagement.fetchError");
    console.error("Failed to fetch instances:", e);
  } finally {
    isLoading.value = false;
  }
}

async function fetchDeletedInstances() {
  isLoadingDeleted.value = true;
  try {
    const response = await listInstances({
      pageSize: 100,
      showDeleted: true,
    });
    deletedInstances.value = response.instances.filter(
      (i) => i.state === State.DELETED
    );
  } catch (e) {
    console.error("Failed to fetch deleted instances:", e);
  } finally {
    isLoadingDeleted.value = false;
  }
}

let searchTimeout: ReturnType<typeof setTimeout> | null = null;
function debouncedSearch() {
  if (searchTimeout) clearTimeout(searchTimeout);
  searchTimeout = setTimeout(() => {
    fetchInstances();
  }, 300);
}

function confirmDelete(instance: Instance) {
  instanceToDelete.value = instance;
  showDeleteModal.value = true;
}

async function handleDeleteInstance() {
  if (!instanceToDelete.value) return;

  isDeleting.value = true;
  try {
    await deleteInstance(instanceToDelete.value.name);
    showDeleteModal.value = false;
    instanceToDelete.value = null;
    showSuccess(t("instanceManagement.deleteSuccess"));
    await Promise.all([fetchInstances(), fetchDeletedInstances()]);
  } catch (e) {
    handleError(e, t("instanceManagement.deleteError"));
  } finally {
    isDeleting.value = false;
  }
}

async function restoreInstance(instance: Instance) {
  restoringInstance.value = instance.name;
  try {
    await undeleteInstance(instance.name);
    showSuccess(t("instanceManagement.restoreSuccess"));
    await Promise.all([fetchInstances(), fetchDeletedInstances()]);
  } catch (e) {
    handleError(e, t("instanceManagement.restoreError"));
  } finally {
    restoringInstance.value = null;
  }
}

function openCreateModal() {
  createForm.value = {
    title: "",
    instanceId: "",
    engine: "",
    environment: "",
    activation: true,
    enableSync: false,
    syncIntervalMinutes: "15",
    adminDataSource: {
      id: "admin",
      host: "",
      port: "",
      username: "",
      password: "",
      database: "",
    },
    readOnlyDataSources: [],
  };
  createFormErrors.value = {
    title: "",
    instanceId: "",
    engine: "",
    environment: "",
    adminDataSource: createEmptyDataSourceErrors(),
    readOnlyDataSources: [],
  };
  showCreateModal.value = true;
}

function onCreateSyncToggle(checked: boolean) {
  createForm.value.enableSync = checked;
  if (!checked) {
    createForm.value.syncIntervalMinutes = "0";
  } else if (
    !createForm.value.syncIntervalMinutes ||
    createForm.value.syncIntervalMinutes === "0"
  ) {
    createForm.value.syncIntervalMinutes = "15";
  }
}

function validateDataSource(
  ds: DataSourceForm,
  errors: DataSourceErrors
): boolean {
  let valid = true;

  if (!ds.id.trim()) {
    errors.id = t("instanceManagement.dataSourceIdRequired");
    valid = false;
  }

  if (!ds.host.trim()) {
    errors.host = t("instanceManagement.hostRequired");
    valid = false;
  }

  if (!ds.port.trim()) {
    errors.port = t("instanceManagement.portRequired");
    valid = false;
  }

  if (!ds.username.trim()) {
    errors.username = t("instanceManagement.usernameRequired");
    valid = false;
  }

  if (!ds.password.trim()) {
    errors.password = t("instanceManagement.passwordRequired");
    valid = false;
  }

  return valid;
}

function resetDataSourceErrors() {
  createFormErrors.value.adminDataSource = createEmptyDataSourceErrors();
  createFormErrors.value.readOnlyDataSources =
    createForm.value.readOnlyDataSources.map(() =>
      createEmptyDataSourceErrors()
    );
}

function validateInstanceId(): boolean {
  const instanceId = createForm.value.instanceId.trim();
  if (!instanceId) {
    createFormErrors.value.instanceId = t(
      "instanceManagement.instanceIdRequired"
    );
    return false;
  }
  // Must match ^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$.
  if (!/^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$/.test(instanceId)) {
    createFormErrors.value.instanceId = t(
      "instanceManagement.instanceIdInvalid"
    );
    return false;
  }
  return true;
}

function validateDataSources(): boolean {
  let valid = validateDataSource(
    createForm.value.adminDataSource,
    createFormErrors.value.adminDataSource
  );
  for (let i = 0; i < createForm.value.readOnlyDataSources.length; i++) {
    if (
      !validateDataSource(
        createForm.value.readOnlyDataSources[i],
        createFormErrors.value.readOnlyDataSources[i]
      )
    ) {
      valid = false;
    }
  }
  return valid;
}

function validateCreateForm(): boolean {
  // Reset basic field errors
  createFormErrors.value.title = "";
  createFormErrors.value.instanceId = "";
  createFormErrors.value.engine = "";
  createFormErrors.value.environment = "";
  resetDataSourceErrors();

  let valid = true;

  if (!createForm.value.title.trim()) {
    createFormErrors.value.title = t("instanceManagement.titleRequired");
    valid = false;
  }

  if (!validateInstanceId()) {
    valid = false;
  }

  if (!createForm.value.engine) {
    createFormErrors.value.engine = t("instanceManagement.engineRequired");
    valid = false;
  }

  if (!createForm.value.environment.trim()) {
    createFormErrors.value.environment = t(
      "instanceManagement.environmentRequired"
    );
    valid = false;
  }

  if (!validateDataSources()) {
    valid = false;
  }

  return valid;
}

/**
 * A test connection only dials the server with what the server validates before
 * connecting — a well-formed instance ID, an engine and complete data sources.
 * The title and environment describe the stored instance, so they stay optional.
 */
function validateTestConnectionForm(): boolean {
  createFormErrors.value.instanceId = "";
  createFormErrors.value.engine = "";
  resetDataSourceErrors();

  let valid = validateInstanceId();

  if (!createForm.value.engine) {
    createFormErrors.value.engine = t("instanceManagement.engineRequired");
    valid = false;
  }

  if (!validateDataSources()) {
    valid = false;
  }

  return valid;
}

function formDataSources(): CreateInstanceInput["dataSources"] {
  return [
    // Build data sources array: admin first, then read-only nodes
    {
      id: createForm.value.adminDataSource.id.trim(),
      type: DataSourceType.ADMIN,
      host: createForm.value.adminDataSource.host.trim(),
      port: createForm.value.adminDataSource.port.trim(),
      username: createForm.value.adminDataSource.username.trim(),
      password: createForm.value.adminDataSource.password,
      database: createForm.value.adminDataSource.database.trim(),
    },
    ...createForm.value.readOnlyDataSources.map((ds) => ({
      id: ds.id.trim(),
      type: DataSourceType.READ_ONLY,
      host: ds.host.trim(),
      port: ds.port.trim(),
      username: ds.username.trim(),
      password: ds.password,
      database: ds.database.trim(),
    })),
  ];
}

function createInstanceInput(validateOnly: boolean): CreateInstanceInput {
  // The picker already carries the full "environments/{id}" resource name.
  return {
    title: createForm.value.title.trim(),
    engine: Number(createForm.value.engine) as Engine,
    environment: createForm.value.environment.trim(),
    activation: createForm.value.activation,
    dataSources: formDataSources(),
    instanceId: createForm.value.instanceId.trim(),
    syncIntervalSeconds: createForm.value.enableSync
      ? (Number(createForm.value.syncIntervalMinutes) || 0) * 60
      : undefined,
    validateOnly,
  };
}

async function handleCreateInstance() {
  if (!validateCreateForm()) return;

  isCreating.value = true;
  try {
    await createInstance(createInstanceInput(false));

    showCreateModal.value = false;
    showSuccess(t("instanceManagement.createSuccess"));
    await fetchInstances();
  } catch (e) {
    handleError(e, t("instanceManagement.createError"));
  } finally {
    isCreating.value = false;
  }
}

/**
 * Test Connection runs the same server-side check as creation with
 * `validate_only`, so it pings every data source in the form and stores
 * nothing. The server reports why a connection failed, and that message is
 * surfaced to the user as-is.
 */
async function handleTestConnection() {
  if (!validateTestConnectionForm()) return;

  isTestingConnection.value = true;
  try {
    await createInstance(createInstanceInput(true));
    showSuccess(t("instanceManagement.testConnectionSuccess"));
  } catch (e) {
    handleError(e, t("instanceManagement.testConnectionError"));
  } finally {
    isTestingConnection.value = false;
  }
}

// Lifecycle
onMounted(() => {
  fetchInstances();
  fetchDeletedInstances();
  void environmentStore.ensureLoaded();
});
</script>
