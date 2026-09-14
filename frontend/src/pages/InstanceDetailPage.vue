<template>
  <div class="space-y-4">
    <!-- Page Header -->
    <div class="flex items-center gap-4">
      <Button
        variant="ghost"
        size="icon"
        @click="goBack"
      >
        <ArrowLeft class="h-5 w-5" />
      </Button>
      <div class="flex-1">
        <h1 class="text-2xl font-bold tracking-tight">
          {{ instance?.title || instanceId }}
        </h1>
      </div>
      <Button
        v-if="instance"
        @click="openEditModal"
      >
        <Pencil class="h-4 w-4 mr-2" />
        {{ t("instanceDetail.editInstance") }}
      </Button>
    </div>

    <!-- Instance Info Card -->
    <Card v-if="isLoadingInstance">
      <CardHeader>
        <CardTitle>{{ t("instanceDetail.instanceInfo") }}</CardTitle>
      </CardHeader>
      <CardContent>
        <div class="p-8 flex justify-center">
          <AppLoading />
        </div>
      </CardContent>
    </Card>

    <Card v-else-if="instance">
      <CardHeader>
        <CardTitle>{{ t("instanceDetail.instanceInfo") }}</CardTitle>
      </CardHeader>
      <CardContent>
        <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
          <div>
            <p class="text-sm text-muted-foreground">
              {{ t("instanceManagement.engine") }}
            </p>
            <p class="font-medium">
              {{ getEngineLabel(instance.engine) }}
            </p>
          </div>
          <div>
            <p class="text-sm text-muted-foreground">
              {{ t("instanceManagement.host") }}
            </p>
            <p class="font-medium">
              {{ getHostInfo(instance) }}
            </p>
          </div>
          <div>
            <p class="text-sm text-muted-foreground">
              {{ t("instanceManagement.environment") }}
            </p>
            <p class="font-medium">
              {{ getEnvironmentLabel(instance.environment) }}
            </p>
          </div>
          <div>
            <p class="text-sm text-muted-foreground">
              {{ t("instanceManagement.status") }}
            </p>
            <Badge :variant="instance.activation ? 'success' : 'secondary'">
              {{ instance.activation ? t("instanceManagement.active") : t("instanceManagement.inactive") }}
            </Badge>
          </div>
          <div>
            <p class="text-sm text-muted-foreground">
              {{ t("instanceDetail.syncInterval") }}
            </p>
            <p class="font-medium">
              {{ instance.syncInterval ? t("instanceDetail.syncIntervalDisplay", { value: Number(instance.syncInterval.seconds) / 60 }) : t("instanceDetail.noSyncInterval") }}
            </p>
          </div>
        </div>
      </CardContent>
    </Card>

    <!-- Databases Section -->
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <CardTitle>{{ t("instanceDetail.databases") }}</CardTitle>
          <div class="flex items-center gap-2">
            <div class="text-sm text-muted-foreground">
              {{ t("instanceDetail.totalDatabases", { count: databases.length }) }}
            </div>
            <Button
              class="w-32 justify-center"
              variant="outline"
              size="sm"
              :disabled="isSyncingAllDatabases"
              @click="handleSyncAllDatabases"
            >
              <Loader2
                v-if="isSyncingAllDatabases"
                class="h-4 w-4 mr-2 animate-spin"
              />
              <span>
                {{ isSyncingAllDatabases ? t("instanceDetail.syncing") : t("instanceDetail.syncAllDatabases") }}
              </span>
            </Button>
          </div>
        </div>
      </CardHeader>

      <!-- Loading State -->
      <div
        v-if="isLoadingDatabases"
        class="p-8 flex justify-center"
      >
        <AppLoading />
      </div>

      <!-- Error State -->
      <div
        v-else-if="databaseError"
        class="p-8 text-center text-destructive"
      >
        {{ databaseError }}
      </div>

      <!-- Empty State -->
      <div
        v-else-if="databases.length === 0"
        class="p-8 text-center text-muted-foreground"
      >
        <Database class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
        <p>{{ t("instanceDetail.noDatabases") }}</p>
      </div>

      <!-- Databases List -->
      <Table v-else>
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("instanceDetail.databaseName") }}</TableHead>
            <TableHead>{{ t("instanceDetail.environment") }}</TableHead>
            <TableHead>{{ t("instanceDetail.schemaVersion") }}</TableHead>
            <TableHead>{{ t("instanceDetail.lastSync") }}</TableHead>
            <TableHead>{{ t("instanceDetail.status") }}</TableHead>
            <TableHead class="w-36 text-right">{{ t("instanceDetail.actions") }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="database in databases"
            :key="database.name"
          >
            <TableCell>
              <div class="flex items-center">
                <Database class="h-4 w-4 mr-2 text-muted-foreground" />
                <div>
                  <div class="font-medium">
                    {{ getDatabaseName(database.name) }}
                  </div>
                  <div class="text-xs text-muted-foreground">
                    {{ database.name }}
                  </div>
                </div>
              </div>
            </TableCell>
            <TableCell class="text-muted-foreground">
              {{ getEnvironmentLabel(database.effectiveEnvironment) }}
            </TableCell>
            <TableCell class="text-muted-foreground">
              {{ database.schemaVersion || "-" }}
            </TableCell>
            <TableCell class="text-muted-foreground">
              {{ formatLastSync(database.successfulSyncTime) }}
            </TableCell>
            <TableCell>
              <Badge
                :variant="database.state === State.ACTIVE ? 'success' : 'secondary'"
              >
                {{ getStateLabel(database.state) }}
              </Badge>
            </TableCell>
            <TableCell class="w-36 text-right">
              <Button
                class="w-28 justify-center"
                variant="outline"
                size="sm"
                :disabled="isSyncingAllDatabases || isDatabaseSyncing(database.name)"
                @click="handleSyncSingleDatabase(database.name)"
              >
                <Loader2
                  v-if="isDatabaseSyncing(database.name)"
                  class="h-4 w-4 mr-2 animate-spin"
                />
                <span>
                  {{ isDatabaseSyncing(database.name) ? t("instanceDetail.syncing") : t("instanceDetail.syncDatabase") }}
                </span>
              </Button>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </Card>

    <!-- Edit Instance Modal -->
    <AppModal
      v-model="showEditModal"
      :title="t('instanceDetail.editInstance')"
      size="lg"
    >
      <form
        class="space-y-6"
        @submit.prevent="handleUpdateInstance"
      >
        <!-- Basic Info Section -->
        <div class="space-y-4">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
            {{ t("instanceManagement.basicInfo") }}
          </h3>

          <div class="grid grid-cols-2 gap-4">
            <div class="space-y-2">
              <Label class="text-sm text-muted-foreground">
                {{ t("instanceManagement.instanceId") }}
              </Label>
              <p class="font-medium text-sm py-2">
                {{ getInstanceIdFromName(instance?.name ?? "") }}
              </p>
            </div>
            <AppInput
              v-model="editForm.title"
              :label="t('instanceManagement.instanceTitle')"
              :placeholder="t('instanceManagement.instanceTitlePlaceholder')"
              required
              :error="editFormErrors.title"
            />
          </div>

          <EnvironmentSelect
            v-model="editForm.environment"
            :label="t('instanceManagement.environment')"
            :placeholder="t('instanceManagement.environmentPlaceholder')"
            required
            :error="editFormErrors.environment"
          />

          <div class="flex items-center gap-2">
            <Checkbox
              id="edit-activation"
              :checked="editForm.activation"
              @update:checked="editForm.activation = $event"
            />
            <Label
              for="edit-activation"
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
                  id="edit-enable-sync"
                  :checked="editForm.enableSync"
                  @update:checked="onEditSyncToggle"
                />
                <Label
                  for="edit-enable-sync"
                  class="text-sm cursor-pointer"
                >
                  {{ t("instanceManagement.enableSync") }}
                </Label>
              </div>
              <div class="flex items-center gap-2">
                <AppInput
                  v-model="editForm.syncIntervalMinutes"
                  type="number"
                  :placeholder="t('instanceManagement.syncIntervalPlaceholder')"
                  :disabled="!editForm.enableSync"
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

        <!-- Data Sources Section -->
        <div class="space-y-4">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
              {{ t("instanceDetail.currentDataSources") }}
            </h3>
          </div>

          <!-- Show current data source info read-only -->
          <div
            v-if="!editForm.editDataSources"
            class="space-y-2"
          >
            <div
              v-for="ds in instance?.dataSources ?? []"
              :key="ds.name"
              class="border rounded-lg p-3 text-sm space-y-1"
            >
              <div class="flex items-center gap-2">
                <Badge variant="secondary">
                  {{ ds.type === 1 ? "ADMIN" : "READ_ONLY" }}
                </Badge>
                <span class="font-medium">{{ dataSourceId(ds.name) }}</span>
              </div>
              <div class="text-muted-foreground">
                {{ ds.host }}:{{ ds.port }} · {{ ds.username }} {{ ds.database ? `· ${ds.database}` : "" }}
              </div>
            </div>
          </div>

          <!-- Toggle to enable data source editing -->
          <div class="flex items-center gap-2">
            <Checkbox
              id="edit-ds-toggle"
              :checked="editForm.editDataSources"
              @update:checked="onEditDataSourcesToggle"
            />
            <Label
              for="edit-ds-toggle"
              class="text-sm cursor-pointer"
            >
              {{ t("instanceDetail.updateDataSources") }}
            </Label>
          </div>
          <p
            v-if="!editForm.editDataSources"
            class="text-xs text-muted-foreground"
          >
            {{ t("instanceDetail.updateDataSourcesHint") }}
          </p>

          <!-- Editable data source fields (only shown when toggle is on) -->
          <template v-if="editForm.editDataSources">
            <!-- Admin Data Source -->
            <div class="space-y-4 border rounded-lg p-4">
              <h4 class="text-sm font-semibold text-muted-foreground">
                {{ t("instanceManagement.adminDataSource") }}
              </h4>

              <div class="grid grid-cols-2 gap-4">
                <AppInput
                  v-model="editForm.adminDataSource.host"
                  :label="t('instanceManagement.host')"
                  :placeholder="t('instanceManagement.hostPlaceholder')"
                  required
                  :error="editFormErrors.adminHost"
                />
                <AppInput
                  v-model="editForm.adminDataSource.port"
                  :label="t('instanceManagement.port')"
                  :placeholder="t('instanceManagement.portPlaceholder')"
                  required
                  :error="editFormErrors.adminPort"
                />
              </div>

              <div class="grid grid-cols-2 gap-4">
                <AppInput
                  v-model="editForm.adminDataSource.username"
                  :label="t('instanceManagement.username')"
                  :placeholder="t('instanceManagement.usernamePlaceholder')"
                  required
                  :error="editFormErrors.adminUsername"
                />
                <AppInput
                  v-model="editForm.adminDataSource.password"
                  type="password"
                  :label="t('instanceManagement.password')"
                  :placeholder="t('instanceManagement.passwordPlaceholder')"
                />
              </div>

              <AppInput
                v-model="editForm.adminDataSource.database"
                :label="t('instanceManagement.database')"
                :placeholder="t('instanceManagement.databasePlaceholder')"
              />
            </div>

            <!-- Read-Only Data Sources -->
            <div class="flex items-center justify-between">
              <h4 class="text-sm font-semibold text-muted-foreground">
                {{ t("instanceManagement.readOnlyDataSources") }}
              </h4>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                @click="addEditReadOnlyDataSource"
              >
                <Plus class="h-4 w-4 mr-1" />
                {{ t("instanceManagement.addReadOnlyNode") }}
              </Button>
            </div>

            <div
              v-if="editForm.readOnlyDataSources.length === 0"
              class="text-sm text-muted-foreground italic"
            >
              {{ t("instanceManagement.noReadOnlyNodes") }}
            </div>

            <div
              v-for="(ds, index) in editForm.readOnlyDataSources"
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
                  @click="removeEditReadOnlyDataSource(index)"
                >
                  <Trash2 class="h-4 w-4" />
                </Button>
              </div>

              <div class="grid grid-cols-2 gap-4">
                <AppInput
                  v-model="ds.host"
                  :label="t('instanceManagement.host')"
                  :placeholder="t('instanceManagement.hostPlaceholder')"
                  required
                />
                <AppInput
                  v-model="ds.port"
                  :label="t('instanceManagement.port')"
                  :placeholder="t('instanceManagement.portPlaceholder')"
                  required
                />
              </div>

              <div class="grid grid-cols-2 gap-4">
                <AppInput
                  v-model="ds.username"
                  :label="t('instanceManagement.username')"
                  :placeholder="t('instanceManagement.usernamePlaceholder')"
                  required
                />
                <AppInput
                  v-model="ds.password"
                  type="password"
                  :label="t('instanceManagement.password')"
                  :placeholder="t('instanceManagement.passwordPlaceholder')"
                />
              </div>

              <AppInput
                v-model="ds.database"
                :label="t('instanceManagement.database')"
                :placeholder="t('instanceManagement.databasePlaceholder')"
              />
            </div>
          </template>
        </div>
      </form>
      <template #footer>
        <Button
          variant="outline"
          @click="showEditModal = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          :disabled="isUpdating"
          @click="handleUpdateInstance"
        >
          {{ t("common.confirm") }}
        </Button>
      </template>
    </AppModal>
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import {
  ArrowLeft,
  Database,
  Loader2,
  Pencil,
  Plus,
  Trash2,
} from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listDatabases, syncDatabase } from "@/api/database";
import {
  createDataSource,
  dataSourceId,
  deleteDataSource,
  listInstances,
  syncInstance,
  updateDataSource,
  updateInstance,
} from "@/api/instance";
import AppInput from "@/components/common/AppInput.vue";
import AppLoading from "@/components/common/AppLoading.vue";
import AppModal from "@/components/common/AppModal.vue";
import EnvironmentSelect from "@/components/common/EnvironmentSelect.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { useEnvironmentStore } from "@/store/modules/environment";
import { useToastStore } from "@/store/modules/toast";
import { Engine, State } from "@/types/proto-es/v1/common_pb";
import type { Database as DatabaseType } from "@/types/proto-es/v1/database_service_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";
import {
  DataSourceType,
  InstanceSchema,
} from "@/types/proto-es/v1/instance_service_pb";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError, showSuccess } = useErrorHandler();
const toastStore = useToastStore();
const environmentStore = useEnvironmentStore();

// State
const instanceId = computed(() => route.params.instanceId as string);
const instance = ref<Instance | null>(null);
const databases = ref<DatabaseType[]>([]);
const isLoadingInstance = ref(false);
const isLoadingDatabases = ref(false);
const databaseError = ref<string | null>(null);
const isSyncingAllDatabases = ref(false);
const syncingDatabases = ref<Record<string, boolean>>({});

// Edit state
const showEditModal = ref(false);
const isUpdating = ref(false);

interface EditDataSourceForm {
  /** The resource name; empty for a data source that does not exist yet. */
  name: string;
  host: string;
  port: string;
  username: string;
  password: string;
  database: string;
}

const editForm = ref({
  title: "",
  environment: "",
  activation: true,
  enableSync: false,
  syncIntervalMinutes: "15",
  editDataSources: false,
  adminDataSource: {
    name: "",
    host: "",
    port: "",
    username: "",
    password: "",
    database: "",
  } as EditDataSourceForm,
  readOnlyDataSources: [] as EditDataSourceForm[],
});

const editFormErrors = ref({
  title: "",
  environment: "",
  adminHost: "",
  adminPort: "",
  adminUsername: "",
});

function getInstanceIdFromName(name: string): string {
  return name.replace("instances/", "");
}

function openEditModal() {
  if (!instance.value) return;
  const inst = instance.value;
  const adminDs = inst.dataSources.find(
    (ds) => ds.type === DataSourceType.ADMIN
  );
  const readOnlyDs = inst.dataSources.filter(
    (ds) => ds.type === DataSourceType.READ_ONLY
  );

  const syncSeconds = inst.syncInterval ? Number(inst.syncInterval.seconds) : 0;

  editForm.value = {
    title: inst.title,
    environment: inst.environment,
    activation: inst.activation,
    enableSync: syncSeconds > 0,
    syncIntervalMinutes: syncSeconds > 0 ? String(syncSeconds / 60) : "15",
    editDataSources: false,
    adminDataSource: {
      name: adminDs?.name ?? "",
      host: adminDs?.host ?? "",
      port: adminDs?.port ?? "",
      username: adminDs?.username ?? "",
      password: "",
      database: adminDs?.database ?? "",
    },
    readOnlyDataSources: readOnlyDs.map((ds) => ({
      name: ds.name,
      host: ds.host,
      port: ds.port,
      username: ds.username,
      password: "",
      database: ds.database,
    })),
  };
  editFormErrors.value = {
    title: "",
    environment: "",
    adminHost: "",
    adminPort: "",
    adminUsername: "",
  };
  showEditModal.value = true;
}

function addEditReadOnlyDataSource() {
  editForm.value.readOnlyDataSources.push({
    name: "",
    host: "",
    port: "",
    username: "",
    password: "",
    database: "",
  });
}

function removeEditReadOnlyDataSource(index: number) {
  editForm.value.readOnlyDataSources.splice(index, 1);
}

function onEditSyncToggle(checked: boolean) {
  editForm.value.enableSync = checked;
  if (!checked) {
    editForm.value.syncIntervalMinutes = "0";
  } else if (
    !editForm.value.syncIntervalMinutes ||
    editForm.value.syncIntervalMinutes === "0"
  ) {
    editForm.value.syncIntervalMinutes = "15";
  }
}

function onEditDataSourcesToggle(checked: boolean) {
  editForm.value.editDataSources = checked;
}

function validateEditForm(): boolean {
  let valid = true;
  editFormErrors.value = {
    title: "",
    environment: "",
    adminHost: "",
    adminPort: "",
    adminUsername: "",
  };

  if (!editForm.value.title.trim()) {
    editFormErrors.value.title = t("instanceManagement.titleRequired");
    valid = false;
  }
  if (!editForm.value.environment.trim()) {
    editFormErrors.value.environment = t(
      "instanceManagement.environmentRequired"
    );
    valid = false;
  }
  if (editForm.value.editDataSources) {
    if (!editForm.value.adminDataSource.host.trim()) {
      editFormErrors.value.adminHost = t("instanceManagement.hostRequired");
      valid = false;
    }
    if (!editForm.value.adminDataSource.port.trim()) {
      editFormErrors.value.adminPort = t("instanceManagement.portRequired");
      valid = false;
    }
    if (!editForm.value.adminDataSource.username.trim()) {
      editFormErrors.value.adminUsername = t(
        "instanceManagement.usernameRequired"
      );
      valid = false;
    }
  }
  return valid;
}

async function handleUpdateInstance() {
  if (!instance.value || !validateEditForm()) return;

  isUpdating.value = true;
  try {
    // The picker already carries the full "environments/{id}" resource name.
    const environment = editForm.value.environment.trim();

    const updateMask = ["title", "environment", "activation", "sync_interval"];

    const instanceData: Record<string, unknown> = {
      name: instance.value.name,
      title: editForm.value.title.trim(),
      environment,
      activation: editForm.value.activation,
    };

    // Sync interval: convert minutes to seconds
    const syncMinutes = editForm.value.enableSync
      ? Number(editForm.value.syncIntervalMinutes) || 0
      : 0;
    if (syncMinutes > 0) {
      instanceData.syncInterval = create(DurationSchema, {
        seconds: BigInt(syncMinutes * 60),
      });
    }

    const updatedInstance = create(InstanceSchema, instanceData);

    await updateInstance({
      instance: updatedInstance,
      updateMask,
    });

    // Data sources are their own resources: create the new ones, update the
    // changed ones and delete the omitted ones through the child methods.
    if (editForm.value.editDataSources) {
      await applyDataSourceChanges(instance.value.name);
    }

    showEditModal.value = false;
    showSuccess(t("instanceDetail.updateSuccess"));
    await fetchInstance();
  } catch (e) {
    handleError(e, t("instanceDetail.updateError"));
  } finally {
    isUpdating.value = false;
  }
}

/**
 * applyDataSourceChanges turns the edited form into Create/Update/Delete calls.
 * A form entry without a name is new; a stored data source that is no longer in
 * the form is deleted. Only the fields that actually changed are sent, so an
 * untouched password is never cleared.
 */
async function applyDataSourceChanges(instanceName: string) {
  if (!instance.value) return;

  const stored = new Map(instance.value.dataSources.map((ds) => [ds.name, ds]));
  const entries: {
    form: EditDataSourceForm;
    type: DataSourceType;
  }[] = [
    { form: editForm.value.adminDataSource, type: DataSourceType.ADMIN },
    ...editForm.value.readOnlyDataSources.map((form) => ({
      form,
      type: DataSourceType.READ_ONLY,
    })),
  ];

  for (const { form, type } of entries) {
    const patch = {
      username: form.username.trim(),
      host: form.host.trim(),
      port: form.port.trim(),
      database: form.database.trim(),
    };

    if (!form.name) {
      // The server only creates read-only data sources here; the admin
      // connection is part of the instance and cannot be re-created.
      if (type === DataSourceType.READ_ONLY) {
        await createDataSource(instanceName, {
          type,
          ...patch,
          password: form.password,
        });
      }
      continue;
    }

    const before = stored.get(form.name);
    stored.delete(form.name);
    if (!before) continue;

    const updateMask: string[] = [];
    if (before.host !== patch.host) updateMask.push("host");
    if (before.port !== patch.port) updateMask.push("port");
    if (before.username !== patch.username) updateMask.push("username");
    if ((before.database ?? "") !== patch.database) updateMask.push("database");
    // Reads never return the password: a non-empty one replaces it, an empty
    // one means "unchanged".
    if (form.password) updateMask.push("password");
    if (updateMask.length === 0) continue;

    await updateDataSource(
      form.name,
      { ...patch, password: form.password },
      updateMask
    );
  }

  for (const name of stored.keys()) {
    await deleteDataSource(name);
  }
}

function isDatabaseSyncing(name: string): boolean {
  return !!syncingDatabases.value[name];
}

async function handleSyncAllDatabases() {
  if (!instance.value) return;

  isSyncingAllDatabases.value = true;
  try {
    await syncInstance(instance.value.name, true);
    toastStore.success(t("instanceDetail.syncAllSuccess"));
    await Promise.all([fetchInstance(), fetchDatabases()]);
  } catch (e) {
    handleError(e, t("instanceDetail.syncAllError"));
  } finally {
    isSyncingAllDatabases.value = false;
  }
}

async function handleSyncSingleDatabase(name: string) {
  syncingDatabases.value = {
    ...syncingDatabases.value,
    [name]: true,
  };

  try {
    await syncDatabase(name);
    toastStore.success(t("instanceDetail.syncSingleSuccess"));
    await fetchDatabases();
  } catch (e) {
    handleError(e, t("instanceDetail.syncSingleError"));
  } finally {
    const nextSyncingDatabases = { ...syncingDatabases.value };
    delete nextSyncingDatabases[name];
    syncingDatabases.value = nextSyncingDatabases;
  }
}

// Methods
function goBack() {
  router.push({ name: "InstanceManagement" });
}

function getDatabaseName(name: string): string {
  // Format: instances/{instance}/databases/{database} -> return database
  const parts = name.split("/");
  return parts[parts.length - 1] || name;
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

function getEngineLabel(engine: Engine): string {
  const engineLabels: Record<number, string> = {
    [Engine.ENGINE_UNSPECIFIED]: "Unknown",
    [Engine.MYSQL]: "MySQL",
    [Engine.POSTGRES]: "PostgreSQL",
    [Engine.TIDB]: "TiDB",
    [Engine.MARIADB]: "MariaDB",
    [Engine.OCEANBASE]: "OceanBase",
    [Engine.STARROCKS]: "StarRocks",
    [Engine.DORIS]: "Doris",
    [Engine.MSSQL]: "SQL Server",
  };
  return engineLabels[engine] || "Unknown";
}

function getStateLabel(state: State): string {
  const stateLabels: Record<number, string> = {
    [State.STATE_UNSPECIFIED]: t("instanceDetail.stateUnspecified"),
    [State.ACTIVE]: t("instanceDetail.stateActive"),
    [State.DELETED]: t("instanceDetail.stateDeleted"),
  };
  return stateLabels[state] || "Unknown";
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

async function fetchInstance() {
  isLoadingInstance.value = true;
  try {
    const response = await listInstances({
      pageSize: 100,
      showDeleted: false,
      filter: `resource_id == "${instanceId.value}"`,
    });
    if (response.instances.length > 0) {
      instance.value = response.instances[0];
    } else {
      handleError(
        new Error("Instance not found"),
        t("instanceDetail.fetchInstanceError")
      );
      router.push({ name: "InstanceManagement" });
    }
  } catch (e) {
    handleError(e, t("instanceDetail.fetchInstanceError"));
    router.push({ name: "InstanceManagement" });
  } finally {
    isLoadingInstance.value = false;
  }
}

async function fetchDatabases() {
  isLoadingDatabases.value = true;
  databaseError.value = null;
  try {
    const response = await listDatabases({
      parent: `instances/${instanceId.value}`,
      pageSize: 100,
      showDeleted: false,
    });
    databases.value = response.databases;
  } catch (e) {
    databaseError.value = t("instanceDetail.fetchDatabasesError");
    console.error("Failed to fetch databases:", e);
  } finally {
    isLoadingDatabases.value = false;
  }
}

// Lifecycle
onMounted(async () => {
  await fetchInstance();
  await fetchDatabases();
  void environmentStore.ensureLoaded();
});
</script>
