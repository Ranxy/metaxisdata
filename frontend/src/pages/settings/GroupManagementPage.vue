<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('iam.groups.pageTitle')"
      :description="t('iam.groups.pageDescription')"
    >
      <template #actions>
        <Button
          v-if="canCreate"
          @click="openCreate"
        >
          <Plus class="h-4 w-4 mr-2" />
          {{ t("iam.groups.create") }}
        </Button>
      </template>
    </PageHeader>

    <Card>
      <PageState
        :loading="isLoading"
        :error="error"
      >
        <EmptyState
          v-if="groups.length === 0"
          :title="t('iam.groups.noGroups')"
        />
        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("iam.groups.titleLabel") }}</TableHead>
              <TableHead>{{ t("iam.groups.nameLabel") }}</TableHead>
              <TableHead>{{ t("iam.groups.membersLabel") }}</TableHead>
              <TableHead class="text-right">
                {{ t("common.edit") }}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="group in groups"
              :key="group.name"
            >
              <TableCell>
                <div class="font-medium">{{ group.title || group.name }}</div>
                <div
                  v-if="group.description"
                  class="text-sm text-muted-foreground"
                >
                  {{ group.description }}
                </div>
              </TableCell>
              <TableCell class="font-mono text-sm">{{ group.name }}</TableCell>
              <TableCell>
                <div class="flex flex-wrap gap-2">
                  <Badge
                    v-for="member in group.members"
                    :key="member.member"
                    variant="secondary"
                  >
                    {{ userLabel(member.member) }}
                  </Badge>
                  <span
                    v-if="group.members.length === 0"
                    class="text-sm text-muted-foreground"
                  >
                    {{ t("iam.groups.noMembers") }}
                  </span>
                </div>
              </TableCell>
              <TableCell class="text-right space-x-2">
                <Button
                  v-if="canUpdate"
                  variant="ghost"
                  size="sm"
                  @click="openEdit(group)"
                >
                  <Pencil class="h-4 w-4" />
                </Button>
                <Button
                  v-if="canDelete"
                  variant="ghost"
                  size="sm"
                  @click="openDelete(group)"
                >
                  <Trash2 class="h-4 w-4 text-destructive" />
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </PageState>
    </Card>

    <AppModal
      v-model="showEditor"
      :title="editing ? t('iam.groups.edit') : t('iam.groups.create')"
      size="lg"
    >
      <div class="space-y-4">
        <div class="grid gap-1">
          <Label for="group-name">{{ t("iam.groups.nameLabel") }}</Label>
          <AppInput
            id="group-name"
            v-model="form.name"
            :disabled="editing"
            :placeholder="t('iam.groups.namePlaceholder')"
          />
          <p class="text-xs text-muted-foreground">
            {{ t("iam.groups.nameHint") }}
          </p>
        </div>
        <div class="grid gap-1">
          <Label for="group-title">{{ t("iam.groups.titleLabel") }}</Label>
          <AppInput
            id="group-title"
            v-model="form.title"
          />
        </div>
        <div class="grid gap-1">
          <Label for="group-description">
            {{ t("iam.groups.descriptionLabel") }}
          </Label>
          <AppInput
            id="group-description"
            v-model="form.description"
          />
        </div>
        <div class="space-y-2">
          <Label>{{ t("iam.groups.membersLabel") }}</Label>
          <div
            v-if="users.length === 0"
            class="text-sm text-muted-foreground"
          >
            {{ t("iam.groups.noUsers") }}
          </div>
          <div
            v-else
            class="max-h-64 overflow-y-auto rounded-md border p-3 grid gap-2 md:grid-cols-2"
          >
            <div
              v-for="user in users"
              :key="user.name"
              class="flex items-center gap-2"
            >
              <Checkbox
                :id="`member-${user.name}`"
                :checked="form.members.includes(user.name)"
                @update:checked="toggleMember(user.name, $event === true)"
              />
              <Label
                :for="`member-${user.name}`"
                class="cursor-pointer text-sm"
              >
                {{ user.email }}
              </Label>
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
      :title="t('iam.groups.deleteTitle')"
    >
      <p class="text-sm">
        {{ t("iam.groups.deleteConfirm", { name: deletingGroup?.name ?? "" }) }}
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
import { create } from "@bufbuild/protobuf";
import { Pencil, Plus, Trash2 } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { createGroup, deleteGroup, listGroups, updateGroup } from "@/api/group";
import { listUsers } from "@/api/user";
import AppInput from "@/components/common/AppInput.vue";
import AppModal from "@/components/common/AppModal.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
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
import { useAuthStore } from "@/store/modules/auth";
import type { Group } from "@/types/proto-es/v1/group_service_pb";
import {
  GroupMember_Role,
  GroupMemberSchema,
} from "@/types/proto-es/v1/group_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

const { t } = useI18n();
const authStore = useAuthStore();
const { handleError, showSuccess } = useErrorHandler();

const canCreate = computed(() =>
  authStore.hasPermission("metaxisdata.groups.create")
);
const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.groups.update")
);
const canDelete = computed(() =>
  authStore.hasPermission("metaxisdata.groups.delete")
);

const groups = ref<Group[]>([]);
const users = ref<User[]>([]);
const isLoading = ref(false);
const isSaving = ref(false);
const isDeleting = ref(false);
const error = ref<string | null>(null);
const showEditor = ref(false);
const showDeleteConfirm = ref(false);
const editing = ref(false);
const deletingGroup = ref<Group | null>(null);

const form = ref({
  name: "",
  title: "",
  description: "",
  members: [] as string[],
});

const isFormValid = computed(
  () => form.value.name.startsWith("groups/") && form.value.name.length > 7
);

const userLabels = computed(() => {
  const map = new Map<string, string>();
  for (const user of users.value) {
    map.set(user.name, user.email);
  }
  return map;
});

function userLabel(member: string): string {
  return userLabels.value.get(member) ?? member;
}

async function loadGroups() {
  isLoading.value = true;
  error.value = null;
  try {
    const [groupResponse, userResponse] = await Promise.all([
      listGroups(),
      listUsers({ pageSize: 1000 }),
    ]);
    groups.value = groupResponse.groups;
    users.value = userResponse.users;
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    isLoading.value = false;
  }
}

function openCreate() {
  editing.value = false;
  form.value = { name: "", title: "", description: "", members: [] };
  showEditor.value = true;
}

function openEdit(group: Group) {
  editing.value = true;
  form.value = {
    name: group.name,
    title: group.title,
    description: group.description,
    members: group.members.map((member) => member.member),
  };
  showEditor.value = true;
}

function openDelete(group: Group) {
  deletingGroup.value = group;
  showDeleteConfirm.value = true;
}

function toggleMember(member: string, checked: boolean) {
  if (checked) {
    form.value.members = [...form.value.members, member];
  } else {
    form.value.members = form.value.members.filter((m) => m !== member);
  }
}

function buildPayload() {
  return {
    name: form.value.name,
    title: form.value.title,
    description: form.value.description,
    members: form.value.members.map((member) =>
      create(GroupMemberSchema, { member, role: GroupMember_Role.MEMBER })
    ),
  };
}

async function handleSave() {
  isSaving.value = true;
  try {
    if (editing.value) {
      await updateGroup(buildPayload(), ["title", "description", "members"]);
      showSuccess(t("iam.groups.updated"));
    } else {
      await createGroup(buildPayload());
      showSuccess(t("iam.groups.created"));
    }
    showEditor.value = false;
    await loadGroups();
  } catch (err) {
    handleError(err, "error.unknown");
  } finally {
    isSaving.value = false;
  }
}

async function handleDelete() {
  if (!deletingGroup.value) {
    return;
  }
  isDeleting.value = true;
  try {
    await deleteGroup(deletingGroup.value.name);
    showSuccess(t("iam.groups.deleted"));
    showDeleteConfirm.value = false;
    await loadGroups();
  } catch (err) {
    handleError(err, "error.unknown");
  } finally {
    isDeleting.value = false;
  }
}

onMounted(loadGroups);
</script>
