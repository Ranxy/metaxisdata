<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ t("iam.roles.pageTitle") }}
        </h1>
        <p class="text-muted-foreground mt-1">
          {{ t("iam.roles.pageDescription") }}
        </p>
      </div>
      <Button
        v-if="canCreate"
        @click="openCreate"
      >
        <Plus class="h-4 w-4 mr-2" />
        {{ t("iam.roles.create") }}
      </Button>
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
        class="p-8 text-center text-destructive"
      >
        {{ error }}
      </div>
      <div
        v-else-if="roles.length === 0"
        class="p-8 text-center text-muted-foreground"
      >
        {{ t("iam.roles.noRoles") }}
      </div>
      <Table v-else>
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("iam.roles.titleLabel") }}</TableHead>
            <TableHead>{{ t("iam.roles.nameLabel") }}</TableHead>
            <TableHead>{{ t("iam.roles.typeLabel") }}</TableHead>
            <TableHead>{{ t("iam.roles.permissionsLabel") }}</TableHead>
            <TableHead class="text-right">
              {{ t("common.edit") }}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="role in roles"
            :key="role.name"
          >
            <TableCell>
              <div class="font-medium">{{ role.title || role.name }}</div>
              <div
                v-if="role.description"
                class="text-sm text-muted-foreground"
              >
                {{ role.description }}
              </div>
            </TableCell>
            <TableCell class="font-mono text-sm">{{ role.name }}</TableCell>
            <TableCell>
              <Badge :variant="role.predefined ? 'secondary' : 'default'">
                {{
                  role.predefined
                    ? t("iam.roles.predefined")
                    : t("iam.roles.custom")
                }}
              </Badge>
            </TableCell>
            <TableCell>{{ role.permissions.length }}</TableCell>
            <TableCell class="text-right space-x-2">
              <Button
                v-if="canUpdate"
                variant="ghost"
                size="sm"
                :disabled="role.predefined"
                :title="
                  role.predefined ? t('iam.roles.predefinedReadonly') : ''
                "
                @click="openEdit(role)"
              >
                <Pencil class="h-4 w-4" />
              </Button>
              <Button
                v-if="canDelete"
                variant="ghost"
                size="sm"
                :disabled="role.predefined"
                :title="
                  role.predefined ? t('iam.roles.predefinedReadonly') : ''
                "
                @click="openDelete(role)"
              >
                <Trash2 class="h-4 w-4 text-destructive" />
              </Button>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </Card>

    <AppModal
      v-model="showEditor"
      :title="editing ? t('iam.roles.edit') : t('iam.roles.create')"
      size="xl"
    >
      <div class="space-y-4">
        <div class="grid gap-1">
          <Label for="role-name">{{ t("iam.roles.nameLabel") }}</Label>
          <AppInput
            id="role-name"
            v-model="form.name"
            :disabled="editing"
            :placeholder="t('iam.roles.namePlaceholder')"
          />
          <p class="text-xs text-muted-foreground">
            {{ t("iam.roles.nameHint") }}
          </p>
        </div>
        <div class="grid gap-1">
          <Label for="role-title">{{ t("iam.roles.titleLabel") }}</Label>
          <AppInput
            id="role-title"
            v-model="form.title"
          />
        </div>
        <div class="grid gap-1">
          <Label for="role-description">
            {{ t("iam.roles.descriptionLabel") }}
          </Label>
          <AppInput
            id="role-description"
            v-model="form.description"
          />
        </div>

        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <Label>{{ t("iam.roles.permissionsLabel") }}</Label>
            <div class="space-x-2">
              <Button
                variant="ghost"
                size="sm"
                @click="selectAllPermissions"
              >
                {{ t("iam.roles.selectAll") }}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                @click="clearAllPermissions"
              >
                {{ t("iam.roles.clearAll") }}
              </Button>
            </div>
          </div>
          <div
            v-for="group in PERMISSION_GROUPS"
            :key="group.resource"
            class="rounded-md border p-3"
          >
            <div class="font-mono text-sm font-medium mb-2">
              {{ group.resource }}
            </div>
            <div class="grid grid-cols-2 gap-2 md:grid-cols-3">
              <div
                v-for="permission in group.permissions"
                :key="permission"
                class="flex items-center gap-2"
              >
                <Checkbox
                  :id="`perm-${permission}`"
                  :checked="form.permissions.includes(permission)"
                  @update:checked="togglePermission(permission, $event === true)"
                />
                <Label
                  :for="`perm-${permission}`"
                  class="font-mono text-xs cursor-pointer"
                >
                  {{ permissionSuffix(permission) }}
                </Label>
              </div>
            </div>
          </div>
        </div>
      </div>
      <template #footer>
        <Button
          variant="outline"
          @click="showEditor = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          :disabled="isSaving || !isFormValid"
          @click="handleSave"
        >
          {{ t("common.save") }}
        </Button>
      </template>
    </AppModal>

    <AppModal
      v-model="showDeleteConfirm"
      size="sm"
      :title="t('iam.roles.deleteTitle')"
    >
      <p class="text-sm">
        {{ t("iam.roles.deleteConfirm", { name: deletingRole?.name ?? "" }) }}
      </p>
      <template #footer>
        <Button
          variant="outline"
          @click="showDeleteConfirm = false"
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
import { Pencil, Plus, Trash2 } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { createRole, deleteRole, listRoles, updateRole } from "@/api/role";
import AppInput from "@/components/common/AppInput.vue";
import AppLoading from "@/components/common/AppLoading.vue";
import AppModal from "@/components/common/AppModal.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
import { PERMISSION_GROUPS, permissionSuffix } from "@/lib/permissions";
import { useAuthStore } from "@/store/modules/auth";
import type { Role } from "@/types/proto-es/v1/role_service_pb";

const { t } = useI18n();
const authStore = useAuthStore();
const { handleError, showSuccess } = useErrorHandler();

const canCreate = computed(() =>
  authStore.hasPermission("metaxisdata.roles.create")
);
const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.roles.update")
);
const canDelete = computed(() =>
  authStore.hasPermission("metaxisdata.roles.delete")
);

const roles = ref<Role[]>([]);
const isLoading = ref(false);
const isSaving = ref(false);
const isDeleting = ref(false);
const error = ref<string | null>(null);
const showEditor = ref(false);
const showDeleteConfirm = ref(false);
const editing = ref(false);
const deletingRole = ref<Role | null>(null);

const form = ref({
  name: "roles/",
  title: "",
  description: "",
  permissions: [] as string[],
});

const isFormValid = computed(
  () => form.value.name.startsWith("roles/") && form.value.name.length > 6
);

async function loadRoles() {
  isLoading.value = true;
  error.value = null;
  try {
    const response = await listRoles();
    roles.value = response.roles;
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    isLoading.value = false;
  }
}

function openCreate() {
  editing.value = false;
  form.value = {
    name: "roles/",
    title: "",
    description: "",
    permissions: [],
  };
  showEditor.value = true;
}

function openEdit(role: Role) {
  editing.value = true;
  form.value = {
    name: role.name,
    title: role.title,
    description: role.description,
    permissions: [...role.permissions],
  };
  showEditor.value = true;
}

function openDelete(role: Role) {
  deletingRole.value = role;
  showDeleteConfirm.value = true;
}

function togglePermission(permission: string, checked: boolean) {
  if (checked) {
    form.value.permissions = [...form.value.permissions, permission];
  } else {
    form.value.permissions = form.value.permissions.filter(
      (p) => p !== permission
    );
  }
}

function selectAllPermissions() {
  form.value.permissions = PERMISSION_GROUPS.flatMap((g) => g.permissions);
}

function clearAllPermissions() {
  form.value.permissions = [];
}

async function handleSave() {
  isSaving.value = true;
  try {
    if (editing.value) {
      await updateRole(
        {
          name: form.value.name,
          title: form.value.title,
          description: form.value.description,
          permissions: form.value.permissions,
        },
        ["title", "description", "permissions"]
      );
      showSuccess(t("iam.roles.updated"));
    } else {
      await createRole({
        name: form.value.name,
        title: form.value.title,
        description: form.value.description,
        permissions: form.value.permissions,
      });
      showSuccess(t("iam.roles.created"));
    }
    showEditor.value = false;
    await loadRoles();
  } catch (err) {
    handleError(err, "error.unknown");
  } finally {
    isSaving.value = false;
  }
}

async function handleDelete() {
  if (!deletingRole.value) {
    return;
  }
  isDeleting.value = true;
  try {
    await deleteRole(deletingRole.value.name);
    showSuccess(t("iam.roles.deleted"));
    showDeleteConfirm.value = false;
    await loadRoles();
  } catch (err) {
    handleError(err, "error.unknown");
  } finally {
    isDeleting.value = false;
  }
}

onMounted(loadRoles);
</script>
