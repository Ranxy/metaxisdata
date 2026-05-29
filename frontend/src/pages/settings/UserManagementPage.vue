<template>
  <div class="space-y-4">
    <!-- Page Header -->
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ t("userManagement.title") }}
        </h1>
      </div>
      <Button @click="openCreateModal">
        <Plus class="h-4 w-4 mr-2" />
        {{ t("userManagement.addUser") }}
      </Button>
    </div>

    <!-- Search Bar -->
    <div class="flex items-center gap-4">
      <div class="flex-1">
        <AppInput
          v-model="searchQuery"
          :placeholder="t('userManagement.searchPlaceholder')"
          @update:model-value="debouncedSearch"
        >
          <template #suffix>
            <Search class="absolute right-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          </template>
        </AppInput>
      </div>
    </div>

    <!-- Users Table -->
    <Card>
      <!-- Loading State -->
      <div
        v-if="isLoading"
        class="p-8 flex justify-center"
      >
        <AppLoading />
      </div>

      <!-- Error State -->
      <div
        v-else-if="error"
        class="p-8 text-center text-destructive"
      >
        {{ error }}
      </div>

      <!-- Empty State -->
      <div
        v-else-if="activeUsers.length === 0"
        class="p-8 text-center text-muted-foreground"
      >
        <Users class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
        <p>{{ t("userManagement.noUsers") }}</p>
      </div>

      <!-- Users List -->
      <Table v-else>
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("userManagement.user") }}</TableHead>
            <TableHead>{{ t("userManagement.email") }}</TableHead>
            <TableHead>{{ t("userManagement.userType") }}</TableHead>
            <TableHead>
              {{ t("userManagement.phone") }}
            </TableHead>
            <TableHead>{{ t("userManagement.lastLogin") }}</TableHead>
            <TableHead class="text-right">
              {{ t("userManagement.actions") }}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="user in activeUsers"
            :key="user.name"
          >
            <TableCell>
              <div class="flex items-center">
                <Avatar class="h-10 w-10">
                  <AvatarFallback class="bg-primary/10 text-primary text-sm">
                    {{ getInitials(user.title || user.email) }}
                  </AvatarFallback>
                </Avatar>
                <div class="ml-4">
                  <div class="font-medium">
                    {{ user.title || "-" }}
                  </div>
                  <div class="text-sm text-muted-foreground">
                    {{ getUserId(user.name) }}
                  </div>
                </div>
              </div>
            </TableCell>
            <TableCell class="text-muted-foreground">
              {{ user.email }}
            </TableCell>
            <TableCell>
              <Badge :variant="user.userType === UserType.SERVICE_ACCOUNT ? 'secondary' : 'default'">
                {{ getUserTypeLabel(user.userType) }}
              </Badge>
            </TableCell>
            <TableCell class="text-muted-foreground">
              {{ user.phone || "-" }}
            </TableCell>
            <TableCell class="text-muted-foreground">
              {{ formatLastLogin(user.profile?.lastLoginTime) }}
            </TableCell>
            <TableCell class="text-right">
              <div class="flex items-center justify-end gap-1">
                <Button
                  v-if="canEditUser(user)"
                  variant="ghost"
                  size="icon"
                  :title="t('common.edit')"
                  @click="openEditModal(user)"
                >
                  <Pencil class="h-4 w-4 text-muted-foreground" />
                </Button>
                <span
                  v-else
                  class="p-2 text-muted-foreground/30 cursor-not-allowed"
                  :title="t('userManagement.cannotEditNonUser')"
                >
                  <Pencil class="h-4 w-4" />
                </span>
                <Button
                  v-if="canDeleteUser(user)"
                  variant="ghost"
                  size="icon"
                  :title="t('common.delete')"
                  @click="confirmDelete(user)"
                >
                  <Trash2 class="h-4 w-4 text-muted-foreground hover:text-destructive" />
                </Button>
                <span
                  v-else
                  class="p-2 text-muted-foreground/30 cursor-not-allowed"
                  :title="getCannotDeleteReason(user)"
                >
                  <Trash2 class="h-4 w-4" />
                </span>
              </div>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </Card>

    <!-- Deleted Users (Recycle Bin) -->
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
                {{ t("userManagement.recycleBin") }}
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
          <!-- Loading State -->
          <div
            v-if="isLoadingDeleted"
            class="p-6 flex justify-center"
          >
            <AppLoading />
          </div>

          <!-- Empty State -->
          <div
            v-else-if="deletedUsers.length === 0"
            class="p-8 text-center text-muted-foreground"
          >
            <RotateCcw class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
            <p>{{ t("userManagement.noDeletedUsers") }}</p>
          </div>

          <!-- Deleted Users List -->
          <Table v-else>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("userManagement.user") }}</TableHead>
                <TableHead>{{ t("userManagement.email") }}</TableHead>
                <TableHead>{{ t("userManagement.userType") }}</TableHead>
                <TableHead class="text-right">
                  {{ t("userManagement.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="user in deletedUsers"
                :key="user.name"
                class="opacity-60"
              >
                <TableCell>
                  <div class="flex items-center">
                    <Avatar class="h-10 w-10">
                      <AvatarFallback class="bg-muted text-muted-foreground text-sm">
                        {{ getInitials(user.title || user.email) }}
                      </AvatarFallback>
                    </Avatar>
                    <div class="ml-4">
                      <div class="font-medium text-muted-foreground">
                        {{ user.title || "-" }}
                      </div>
                      <div class="text-sm text-muted-foreground/70">
                        {{ getUserId(user.name) }}
                      </div>
                    </div>
                  </div>
                </TableCell>
                <TableCell class="text-muted-foreground">
                  {{ user.email }}
                </TableCell>
                <TableCell>
                  <Badge variant="secondary">
                    {{ getUserTypeLabel(user.userType) }}
                  </Badge>
                </TableCell>
                <TableCell class="text-right">
                  <Button
                    variant="secondary"
                    size="sm"
                    :disabled="restoringUser === user.name"
                    @click="restoreUser(user)"
                  >
                    <RotateCcw class="h-4 w-4 mr-1" />
                    {{ t("userManagement.restore") }}
                  </Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </CollapsibleContent>
      </Card>
    </Collapsible>

    <!-- Create User Modal -->
    <AppModal
      v-model="showCreateModal"
      :title="t('userManagement.addUser')"
      size="md"
    >
      <form @submit.prevent="handleCreateUser">
        <div class="space-y-4">
          <AppInput
            v-model="createForm.email"
            type="email"
            :label="t('userManagement.email')"
            :placeholder="t('userManagement.emailPlaceholder')"
            required
            :error="createFormErrors.email"
          />
          <AppInput
            v-model="createForm.title"
            :label="t('userManagement.userName')"
            :placeholder="t('userManagement.userNamePlaceholder')"
            required
            :error="createFormErrors.title"
          />
          <AppInput
            v-model="createForm.password"
            type="password"
            :label="t('userManagement.password')"
            :placeholder="t('userManagement.passwordPlaceholder')"
            required
            :error="createFormErrors.password"
          />
          <AppInput
            v-model="createForm.confirmPassword"
            type="password"
            :label="t('userManagement.confirmPassword')"
            :placeholder="t('userManagement.confirmPasswordPlaceholder')"
            required
            :error="createFormErrors.confirmPassword"
          />
        </div>
      </form>
      <template #footer>
        <Button
          variant="outline"
          @click="showCreateModal = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          :disabled="isCreating"
          @click="handleCreateUser"
        >
          {{ t("common.confirm") }}
        </Button>
      </template>
    </AppModal>

    <!-- Edit User Modal -->
    <AppModal
      v-model="showEditModal"
      :title="t('userManagement.editUser')"
      size="md"
    >
      <form @submit.prevent="handleUpdateUser">
        <div class="space-y-4">
          <AppInput
            v-model="editForm.email"
            type="email"
            :label="t('userManagement.email')"
            :placeholder="t('userManagement.emailPlaceholder')"
            required
            :error="editFormErrors.email"
          />
          <AppInput
            v-model="editForm.title"
            :label="t('userManagement.userName')"
            :placeholder="t('userManagement.userNamePlaceholder')"
          />
          <AppInput
            v-model="editForm.phone"
            :label="t('userManagement.phone')"
            :placeholder="t('userManagement.phonePlaceholder')"
            :error="editFormErrors.phone"
          />
          <div class="pt-4 border-t">
            <p class="text-sm text-muted-foreground mb-3">
              {{ t("userManagement.changePasswordHint") }}
            </p>
            <AppInput
              v-model="editForm.password"
              type="password"
              :label="t('userManagement.newPassword')"
              :placeholder="t('userManagement.newPasswordPlaceholder')"
              :error="editFormErrors.password"
            />
          </div>
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
          @click="handleUpdateUser"
        >
          {{ t("common.save") }}
        </Button>
      </template>
    </AppModal>

    <!-- Delete Confirmation Modal -->
    <AppModal
      v-model="showDeleteModal"
      :title="t('userManagement.deleteUser')"
      size="sm"
    >
      <div class="text-center">
        <div class="w-12 h-12 mx-auto mb-4 rounded-full bg-destructive/10 flex items-center justify-center">
          <Trash2 class="h-6 w-6 text-destructive" />
        </div>
        <p>
          {{ t("userManagement.deleteConfirmMessage") }}
        </p>
        <p class="text-sm text-muted-foreground mt-2">
          <strong>{{ userToDelete?.email }}</strong>
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
          @click="handleDeleteUser"
        >
          {{ t("common.delete") }}
        </Button>
      </template>
    </AppModal>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  ChevronDown,
  Pencil,
  Plus,
  RotateCcw,
  Search,
  Trash2,
  Users,
} from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  createUser,
  deleteUser,
  listUsers,
  undeleteUser,
  updateUser,
} from "@/api/user";
import AppInput from "@/components/common/AppInput.vue";
import AppLoading from "@/components/common/AppLoading.vue";
import AppModal from "@/components/common/AppModal.vue";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
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
import { State } from "@/types/proto-es/v1/common_pb";
import { type User, UserType } from "@/types/proto-es/v1/user_service_pb";

const { t, locale } = useI18n();
const authStore = useAuthStore();
const { handleError, showSuccess } = useErrorHandler();

// State
const isLoading = ref(false);
const isLoadingDeleted = ref(false);
const isCreating = ref(false);
const isUpdating = ref(false);
const isDeleting = ref(false);
const restoringUser = ref<string | null>(null);
const error = ref<string | null>(null);
const searchQuery = ref("");
const showRecycleBin = ref(false);
const users = ref<User[]>([]);
const deletedUsers = ref<User[]>([]);

// Modals
const showCreateModal = ref(false);
const showEditModal = ref(false);
const showDeleteModal = ref(false);
const userToDelete = ref<User | null>(null);
const userToEdit = ref<User | null>(null);

// Forms
const createForm = ref({
  email: "",
  title: "",
  password: "",
  confirmPassword: "",
});

const createFormErrors = ref({
  email: "",
  title: "",
  password: "",
  confirmPassword: "",
});

const editForm = ref({
  email: "",
  title: "",
  phone: "",
  password: "",
});

const editFormErrors = ref({
  email: "",
  password: "",
  phone: "",
  title: "",
});

// Computed
const activeUsers = computed(() => {
  return users.value.filter((u) => u.state !== State.DELETED);
});

// Methods
function getInitials(name: string): string {
  if (!name) return "?";
  const parts = name.split(/[@\s]+/);
  if (parts.length >= 2) {
    return (parts[0][0] + parts[1][0]).toUpperCase();
  }
  return name.substring(0, 2).toUpperCase();
}

function getUserId(name: string): string {
  // Format: users/{id} -> return id
  return name.replace("users/", "");
}

// Permission checks
function canEditUser(user: User): boolean {
  // Only normal users (UserType.USER) can be edited
  return user.userType === UserType.USER;
}

function canDeleteUser(user: User): boolean {
  // Cannot delete SYSTEM_BOT
  if (user.userType === UserType.SYSTEM_BOT) {
    return false;
  }
  // Cannot delete self
  if (authStore.user?.name === user.name) {
    return false;
  }
  return true;
}

function getCannotDeleteReason(user: User): string {
  if (user.userType === UserType.SYSTEM_BOT) {
    return t("userManagement.cannotDeleteSystemBot");
  }
  if (authStore.user?.name === user.name) {
    return t("userManagement.cannotDeleteSelf");
  }
  return "";
}

function getUserTypeLabel(userType: UserType): string {
  switch (userType) {
    case UserType.USER:
      return t("userManagement.typeUser");
    case UserType.SERVICE_ACCOUNT:
      return t("userManagement.typeServiceAccount");
    case UserType.SYSTEM_BOT:
      return t("userManagement.typeSystemBot");
    default:
      return t("userManagement.typeUnknown");
  }
}

function formatLastLogin(timestamp: Timestamp | undefined): string {
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

async function fetchUsers() {
  isLoading.value = true;
  error.value = null;
  try {
    const response = await listUsers({
      pageSize: 100,
      showDeleted: false,
      filter: searchQuery.value
        ? `email.matches("${searchQuery.value}") || name.matches("${searchQuery.value}")`
        : "",
    });
    users.value = response.users;
  } catch (e) {
    error.value = t("userManagement.fetchError");
    console.error("Failed to fetch users:", e);
  } finally {
    isLoading.value = false;
  }
}

async function fetchDeletedUsers() {
  isLoadingDeleted.value = true;
  try {
    const response = await listUsers({
      pageSize: 100,
      showDeleted: true,
      filter: 'state == "DELETED"',
    });
    deletedUsers.value = response.users.filter(
      (u) => u.state === State.DELETED
    );
  } catch (e) {
    console.error("Failed to fetch deleted users:", e);
  } finally {
    isLoadingDeleted.value = false;
  }
}

let searchTimeout: ReturnType<typeof setTimeout> | null = null;
function debouncedSearch() {
  if (searchTimeout) clearTimeout(searchTimeout);
  searchTimeout = setTimeout(() => {
    fetchUsers();
  }, 300);
}

function openCreateModal() {
  createForm.value = {
    email: "",
    title: "",
    password: "",
    confirmPassword: "",
  };
  createFormErrors.value = {
    email: "",
    title: "",
    password: "",
    confirmPassword: "",
  };
  showCreateModal.value = true;
}

function openEditModal(user: User) {
  userToEdit.value = user;
  editForm.value = {
    email: user.email,
    title: user.title,
    phone: user.phone,
    password: "",
  };
  editFormErrors.value = { email: "", password: "", phone: "", title: "" };
  showEditModal.value = true;
}

function confirmDelete(user: User) {
  userToDelete.value = user;
  showDeleteModal.value = true;
}

function validateCreateForm(): boolean {
  let valid = true;
  createFormErrors.value = {
    email: "",
    title: "",
    password: "",
    confirmPassword: "",
  };

  if (!createForm.value.email) {
    createFormErrors.value.email = t("userManagement.emailRequired");
    valid = false;
  } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(createForm.value.email)) {
    createFormErrors.value.email = t("userManagement.emailInvalid");
    valid = false;
  }

  if (!createForm.value.title) {
    createFormErrors.value.title = t("userManagement.userNameRequired");
    valid = false;
  }

  if (!createForm.value.password) {
    createFormErrors.value.password = t("userManagement.passwordRequired");
    valid = false;
  } else if (createForm.value.password.length < 8) {
    createFormErrors.value.password = t("userManagement.passwordTooShort");
    valid = false;
  }

  if (createForm.value.password !== createForm.value.confirmPassword) {
    createFormErrors.value.confirmPassword = t(
      "userManagement.passwordMismatch"
    );
    valid = false;
  }

  return valid;
}

function validateEditForm(): boolean {
  let valid = true;
  editFormErrors.value = { email: "", password: "", phone: "", title: "" };

  if (!editForm.value.email) {
    editFormErrors.value.email = t("userManagement.emailRequired");
    valid = false;
  } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(editForm.value.email)) {
    editFormErrors.value.email = t("userManagement.emailInvalid");
    valid = false;
  }

  if (editForm.value.password && editForm.value.password.length < 8) {
    editFormErrors.value.password = t("userManagement.passwordTooShort");
    valid = false;
  }

  return valid;
}

async function handleCreateUser() {
  if (!validateCreateForm()) return;

  isCreating.value = true;
  try {
    await createUser(
      createForm.value.email,
      createForm.value.password,
      createForm.value.title
    );
    showCreateModal.value = false;
    showSuccess(t("userManagement.createSuccess"));
    await fetchUsers();
  } catch (e) {
    handleError(e, t("userManagement.createError"));
  } finally {
    isCreating.value = false;
  }
}

async function handleUpdateUser() {
  if (!validateEditForm() || !userToEdit.value) return;

  isUpdating.value = true;
  try {
    const updateFields: string[] = ["email", "title", "phone"];
    const userData: Partial<User> & { name: string } = {
      name: userToEdit.value.name,
      email: editForm.value.email,
      title: editForm.value.title,
      phone: editForm.value.phone,
    };

    if (editForm.value.password) {
      updateFields.push("password");
      userData.password = editForm.value.password;
    }

    await updateUser(userData, updateFields);
    showEditModal.value = false;
    showSuccess(t("userManagement.updateSuccess"));
    await fetchUsers();
  } catch (e) {
    handleError(e, t("userManagement.updateError"));
  } finally {
    isUpdating.value = false;
  }
}

async function handleDeleteUser() {
  if (!userToDelete.value) return;

  isDeleting.value = true;
  try {
    await deleteUser(userToDelete.value.name);
    showDeleteModal.value = false;
    userToDelete.value = null;
    showSuccess(t("userManagement.deleteSuccess"));
    await Promise.all([fetchUsers(), fetchDeletedUsers()]);
  } catch (e) {
    handleError(e, t("userManagement.deleteError"));
  } finally {
    isDeleting.value = false;
  }
}

async function restoreUser(user: User) {
  restoringUser.value = user.name;
  try {
    await undeleteUser(user.name);
    showSuccess(t("userManagement.restoreSuccess"));
    await Promise.all([fetchUsers(), fetchDeletedUsers()]);
  } catch (e) {
    handleError(e, t("userManagement.restoreError"));
  } finally {
    restoringUser.value = null;
  }
}

// Lifecycle
onMounted(() => {
  fetchUsers();
});

watch(showRecycleBin, (isOpen) => {
  if (isOpen && deletedUsers.value.length === 0) {
    fetchDeletedUsers();
  }
});
</script>

<style scoped>
.slide-enter-active,
.slide-leave-active {
  transition: all 0.3s ease;
  max-height: 1000px;
  overflow: hidden;
}

.slide-enter-from,
.slide-leave-to {
  max-height: 0;
  opacity: 0;
}
</style>
