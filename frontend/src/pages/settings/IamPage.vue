<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ t("iam.policy.pageTitle") }}
        </h1>
        <p class="text-muted-foreground mt-1">
          {{ t("iam.policy.pageDescription") }}
        </p>
      </div>
      <div class="space-x-2">
        <Button
          variant="outline"
          :disabled="isLoading || isSaving"
          @click="loadPolicy"
        >
          {{ t("iam.policy.reload") }}
        </Button>
        <Button
          v-if="canSet"
          :disabled="isSaving || !dirty"
          @click="handleSave"
        >
          {{ t("common.save") }}
        </Button>
      </div>
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
        v-else-if="bindings.length === 0"
        class="p-8 text-center text-muted-foreground"
      >
        {{ t("iam.policy.noBindings") }}
      </div>
      <Table v-else>
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("iam.policy.role") }}</TableHead>
            <TableHead>{{ t("iam.policy.members") }}</TableHead>
            <TableHead
              v-if="canSet"
              class="text-right"
            >
              {{ t("common.edit") }}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="(binding, index) in bindings"
            :key="binding.role"
          >
            <TableCell>
              <div class="font-medium">
                {{ roleTitle(binding.role) }}
              </div>
              <div class="font-mono text-xs text-muted-foreground">
                {{ binding.role }}
              </div>
            </TableCell>
            <TableCell>
              <div class="flex flex-wrap gap-2">
                <Badge
                  v-for="member in binding.members"
                  :key="member"
                  variant="secondary"
                  class="gap-1"
                >
                  <span>{{ memberLabel(member) }}</span>
                  <button
                    v-if="canSet"
                    type="button"
                    class="text-muted-foreground hover:text-destructive"
                    :title="t('iam.policy.removeMember')"
                    @click="removeMember(index, member)"
                  >
                    ×
                  </button>
                </Badge>
                <span
                  v-if="binding.members.length === 0"
                  class="text-sm text-muted-foreground"
                >
                  {{ t("iam.policy.noMembers") }}
                </span>
              </div>
            </TableCell>
            <TableCell
              v-if="canSet"
              class="text-right space-x-2"
            >
              <Button
                variant="ghost"
                size="sm"
                @click="openAddMember(index)"
              >
                <Plus class="h-4 w-4" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                @click="removeBinding(index)"
              >
                <Trash2 class="h-4 w-4 text-destructive" />
              </Button>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
      <div
        v-if="canSet && !isLoading && !error"
        class="p-4 border-t"
      >
        <Button
          variant="outline"
          @click="openAddBinding"
        >
          <Plus class="h-4 w-4 mr-2" />
          {{ t("iam.policy.addBinding") }}
        </Button>
      </div>
    </Card>

    <AppModal
      v-model="showAddMember"
      :title="t('iam.policy.addMember')"
      size="md"
    >
      <div class="space-y-4">
        <div class="grid gap-1">
          <Label>{{ t("iam.policy.memberType") }}</Label>
          <Select v-model="memberType">
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="user">
                {{ t("iam.policy.memberTypeUser") }}
              </SelectItem>
              <SelectItem value="group">
                {{ t("iam.policy.memberTypeGroup") }}
              </SelectItem>
              <SelectItem value="allUsers">
                {{ t("iam.policy.memberTypeAllUsers") }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div
          v-if="memberType === 'user'"
          class="grid gap-1"
        >
          <Label>{{ t("iam.policy.user") }}</Label>
          <Select v-model="selectedUser">
            <SelectTrigger>
              <SelectValue :placeholder="t('iam.policy.selectUser')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="user in users"
                :key="user.name"
                :value="user.name"
              >
                {{ user.email }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div
          v-if="memberType === 'group'"
          class="grid gap-1"
        >
          <Label>{{ t("iam.policy.group") }}</Label>
          <Select v-model="selectedGroup">
            <SelectTrigger>
              <SelectValue :placeholder="t('iam.policy.selectGroup')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="group in groups"
                :key="group.name"
                :value="group.name"
              >
                {{ group.title || group.name }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
      <template #footer>
        <Button
          variant="outline"
          @click="showAddMember = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          :disabled="!selectedMember"
          @click="handleAddMember"
        >
          {{ t("common.create") }}
        </Button>
      </template>
    </AppModal>

    <AppModal
      v-model="showAddBinding"
      :title="t('iam.policy.addBinding')"
      size="md"
    >
      <div class="grid gap-1">
        <Label>{{ t("iam.policy.role") }}</Label>
        <Select v-model="selectedRole">
          <SelectTrigger>
            <SelectValue :placeholder="t('iam.policy.selectRole')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="role in grantableRoles"
              :key="role.name"
              :value="role.name"
            >
              {{ role.title || role.name }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <template #footer>
        <Button
          variant="outline"
          @click="showAddBinding = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          :disabled="!selectedRole"
          @click="handleAddBinding"
        >
          {{ t("common.create") }}
        </Button>
      </template>
    </AppModal>
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { Plus, Trash2 } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { listGroups } from "@/api/group";
import { getWorkspaceIamPolicy, setWorkspaceIamPolicy } from "@/api/iam";
import { listRoles } from "@/api/role";
import { listUsers } from "@/api/user";
import AppLoading from "@/components/common/AppLoading.vue";
import AppModal from "@/components/common/AppModal.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
import type { Group } from "@/types/proto-es/v1/group_service_pb";
import {
  BindingSchema,
  IamPolicySchema,
} from "@/types/proto-es/v1/iam_service_pb";
import type { Role } from "@/types/proto-es/v1/role_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

type BindingForm = { role: string; members: string[] };
type MemberType = "user" | "group" | "allUsers";

const { t } = useI18n();
const authStore = useAuthStore();
const { handleError, showSuccess } = useErrorHandler();

const canSet = computed(() =>
  authStore.hasPermission("metaxisdata.iam.setPolicy")
);

const bindings = ref<BindingForm[]>([]);
const etag = ref("");
const roles = ref<Role[]>([]);
const users = ref<User[]>([]);
const groups = ref<Group[]>([]);
const isLoading = ref(false);
const isSaving = ref(false);
const error = ref<string | null>(null);
const dirty = ref(false);

const showAddMember = ref(false);
const showAddBinding = ref(false);
const targetBinding = ref(0);
const memberType = ref<MemberType>("user");
const selectedUser = ref("");
const selectedGroup = ref("");
const selectedRole = ref("");

const userLabels = computed(() => {
  const map = new Map<string, string>();
  for (const user of users.value) {
    map.set(user.name, user.email);
  }
  return map;
});

const groupLabels = computed(() => {
  const map = new Map<string, string>();
  for (const group of groups.value) {
    map.set(group.name, group.title || group.name);
  }
  return map;
});

const roleTitles = computed(() => {
  const map = new Map<string, string>();
  for (const role of roles.value) {
    map.set(role.name, role.title || role.name);
  }
  return map;
});

// workspaceMember is granted to everyone implicitly, so binding it explicitly
// adds nothing; keep it off the picker.
const grantableRoles = computed(() =>
  roles.value.filter((role) => role.name !== "roles/workspaceMember")
);

const selectedMember = computed(() => {
  switch (memberType.value) {
    case "user":
      return selectedUser.value;
    case "group":
      return selectedGroup.value;
    case "allUsers":
      return "allUsers";
    default:
      return "";
  }
});

function roleTitle(role: string): string {
  return roleTitles.value.get(role) ?? role;
}

function memberLabel(member: string): string {
  if (member === "allUsers") {
    return t("iam.policy.memberTypeAllUsers");
  }
  return (
    userLabels.value.get(member) ?? groupLabels.value.get(member) ?? member
  );
}

async function loadPolicy() {
  isLoading.value = true;
  error.value = null;
  try {
    const [policyResponse, roleResponse, userResponse, groupResponse] =
      await Promise.all([
        getWorkspaceIamPolicy(),
        listRoles(),
        listUsers(),
        listGroups(),
      ]);
    etag.value = policyResponse.etag;
    bindings.value =
      policyResponse.policy?.bindings.map((binding) => ({
        role: binding.role,
        members: [...binding.members],
      })) ?? [];
    roles.value = roleResponse.roles;
    users.value = userResponse.users;
    groups.value = groupResponse.groups;
    dirty.value = false;
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    isLoading.value = false;
  }
}

function openAddMember(index: number) {
  targetBinding.value = index;
  memberType.value = "user";
  selectedUser.value = "";
  selectedGroup.value = "";
  showAddMember.value = true;
}

function handleAddMember() {
  const member = selectedMember.value;
  if (!member) {
    return;
  }
  const binding = bindings.value[targetBinding.value];
  if (binding && !binding.members.includes(member)) {
    binding.members = [...binding.members, member];
    dirty.value = true;
  }
  showAddMember.value = false;
}

function removeMember(index: number, member: string) {
  const binding = bindings.value[index];
  if (!binding) {
    return;
  }
  binding.members = binding.members.filter((m) => m !== member);
  dirty.value = true;
}

function removeBinding(index: number) {
  bindings.value = bindings.value.filter((_, i) => i !== index);
  dirty.value = true;
}

function openAddBinding() {
  selectedRole.value = "";
  showAddBinding.value = true;
}

function handleAddBinding() {
  if (!selectedRole.value) {
    return;
  }
  if (!bindings.value.some((b) => b.role === selectedRole.value)) {
    bindings.value = [
      ...bindings.value,
      { role: selectedRole.value, members: [] },
    ];
    dirty.value = true;
  }
  showAddBinding.value = false;
}

async function handleSave() {
  isSaving.value = true;
  try {
    const policy = create(IamPolicySchema, {
      bindings: bindings.value.map((binding) =>
        create(BindingSchema, {
          role: binding.role,
          members: binding.members,
        })
      ),
    });
    const response = await setWorkspaceIamPolicy(policy, etag.value);
    etag.value = response.etag;
    bindings.value =
      response.policy?.bindings.map((binding) => ({
        role: binding.role,
        members: [...binding.members],
      })) ?? [];
    dirty.value = false;
    showSuccess(t("iam.policy.saved"));
  } catch (err) {
    // A rejected write (etag conflict, unknown role, last-admin guard) leaves
    // the local draft in place; the operator can reload after reading the
    // message. handleError already reports the server's message.
    handleError(err, t("iam.policy.saveFailed"));
  } finally {
    isSaving.value = false;
  }
}

onMounted(loadPolicy);
</script>
