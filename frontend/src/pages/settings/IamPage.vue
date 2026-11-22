<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('iam.policy.pageTitle')"
      :description="t('iam.policy.pageDescription')"
    >
      <template #actions>
        <Button
          variant="outline"
          :disabled="isLoading || isSaving"
          @click="reloadAll"
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
      </template>
    </PageHeader>

    <Card>
      <PageState
        :loading="isLoading"
        :error="error"
      >
        <EmptyState
          v-if="bindings.length === 0"
          :icon="KeyRound"
          :title="t('iam.policy.noBindings')"
        />
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
              :key="`${binding.role}#${index}`"
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
                      v-if="
                        canSet &&
                        !(
                          member === ALL_USERS &&
                          isAllUsersBaselineBinding(binding)
                        )
                      "
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
                <!-- The allUsers binding is server-managed: it must survive
                     every save, so it is shown read-only. -->
                <span
                  v-if="isAllUsersBaselineBinding(binding)"
                  class="align-middle text-xs text-muted-foreground"
                  :title="t('iam.policy.systemBindingHint')"
                >
                  {{ t("iam.policy.systemBinding") }}
                </span>
                <template v-else>
                  <Button
                    variant="ghost"
                    size="sm"
                    :aria-label="t('iam.policy.addMember')"
                    @click="openAddMember(index)"
                  >
                    <Plus class="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    :aria-label="t('iam.policy.removeMember')"
                    @click="removeBinding(index)"
                  >
                    <Trash2 class="h-4 w-4 text-destructive" />
                  </Button>
                </template>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </PageState>
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

    <Card>
      <div class="flex items-center justify-between gap-3 border-b p-4">
        <div>
          <h3 class="font-medium">
            {{ t("iam.grants.title") }}
          </h3>
          <p class="mt-1 text-sm text-muted-foreground">
            {{ t("iam.grants.description") }}
          </p>
        </div>
        <Button
          v-if="canSet"
          variant="outline"
          @click="openAddGrant"
        >
          <Plus class="h-4 w-4 mr-2" />
          {{ t("iam.grants.add") }}
        </Button>
      </div>
      <PageState
        :loading="grantsLoading"
        :error="grantsError"
      >
        <EmptyState
          v-if="grants.length === 0"
          :icon="KeyRound"
          :title="t('iam.grants.empty')"
        />
        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("iam.grants.scope") }}</TableHead>
              <TableHead>{{ t("iam.policy.role") }}</TableHead>
              <TableHead>{{ t("iam.policy.members") }}</TableHead>
              <TableHead
                v-if="canSet"
                class="text-right"
              >
                {{ t("common.actions") }}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="grant in grants"
              :key="grant.name"
            >
              <TableCell>
                <div
                  class="font-mono text-xs"
                  :title="grant.resource"
                >
                  {{ grantResourceLabel(grant.resource) }}
                </div>
                <div
                  v-if="grant.createdBy"
                  class="text-xs text-muted-foreground"
                >
                  {{
                    t("iam.grants.createdBy", {
                      member: memberLabel(grant.createdBy),
                    })
                  }}
                </div>
                <div
                  v-if="grant.condition"
                  class="font-mono text-xs text-muted-foreground"
                  :title="grant.condition.expression"
                >
                  {{ t("iam.grants.conditionLabel") }}:
                  {{ grant.condition.expression }}
                </div>
              </TableCell>
              <TableCell>
                <div class="font-medium">
                  {{ roleTitle(grant.role) }}
                </div>
                <div class="font-mono text-xs text-muted-foreground">
                  {{ grant.role }}
                </div>
              </TableCell>
              <TableCell>
                <Badge variant="secondary">
                  {{ memberLabel(grant.member) }}
                </Badge>
              </TableCell>
              <TableCell
                v-if="canSet"
                class="text-right"
              >
                <Button
                  variant="ghost"
                  size="sm"
                  :aria-label="t('iam.grants.delete')"
                  @click="deleteTarget = grant"
                >
                  <Trash2 class="h-4 w-4 text-destructive" />
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </PageState>
    </Card>

    <Dialog v-model:open="showAddMember">
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("iam.policy.addMember") }}</DialogTitle>
        </DialogHeader>

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
                  v-for="user in assignableUsers"
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
        <DialogFooter>
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
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="showAddBinding">
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("iam.policy.addBinding") }}</DialogTitle>
        </DialogHeader>

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
        <DialogFooter>
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
        </DialogFooter>
      </DialogContent>
    </Dialog>
    <Dialog v-model:open="showAddGrant">
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("iam.grants.add") }}</DialogTitle>
        </DialogHeader>

        <div class="space-y-4">
          <div class="grid gap-1">
            <Label>{{ t("iam.grants.scopeType") }}</Label>
            <Select v-model="grantScopeType">
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="instance">
                  {{ t("iam.grants.scopeTypeInstance") }}
                </SelectItem>
                <SelectItem value="database">
                  {{ t("iam.grants.scopeTypeDatabase") }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="grid gap-1">
            <Label>{{ t("iam.grants.instance") }}</Label>
            <Select
              :model-value="grantInstance"
              @update:model-value="setGrantInstance"
            >
              <SelectTrigger>
                <SelectValue :placeholder="t('iam.grants.selectInstance')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="instance in instances"
                  :key="instance.name"
                  :value="instance.name"
                >
                  {{ instance.title || instance.name }}
                </SelectItem>
              </SelectContent>
            </Select>
            <p
              v-if="instancesError || instances.length === 0"
              class="text-xs text-muted-foreground"
            >
              {{ t("iam.grants.instancesUnavailable") }}
            </p>
          </div>

          <div
            v-if="grantScopeType === 'database'"
            class="grid gap-1"
          >
            <Label>{{ t("iam.grants.database") }}</Label>
            <Select v-model="grantDatabase">
              <SelectTrigger>
                <SelectValue :placeholder="t('iam.grants.selectDatabase')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="database in databases"
                  :key="database.name"
                  :value="database.name"
                >
                  {{ databaseLabel(database.name) }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="grid gap-1">
            <Label>{{ t("iam.policy.role") }}</Label>
            <Select v-model="grantRole">
              <SelectTrigger>
                <SelectValue :placeholder="t('iam.policy.selectRole')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="role in grantScopedRoles"
                  :key="role.name"
                  :value="role.name"
                >
                  {{ role.title || role.name }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

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
                  v-for="user in assignableUsers"
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

          <div class="grid gap-1">
            <Label>{{ t("iam.grants.condition") }}</Label>
            <Input
              v-model="grantCondition"
              :placeholder="t('iam.grants.conditionPlaceholder')"
            />
            <p class="text-xs text-muted-foreground">
              {{ t("iam.grants.conditionHint") }}
            </p>
          </div>

          <p
            v-if="grantError"
            class="text-sm text-destructive"
          >
            {{ grantError }}
          </p>
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            @click="showAddGrant = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="
              grantSaving || !grantResource || !grantRole || !selectedMember
            "
            @click="handleCreateGrant"
          >
            {{ t("common.create") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog
      :open="deleteTarget !== null"
      @update:open="closeDeleteDialog"
    >
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("iam.grants.deleteTitle") }}</DialogTitle>
        </DialogHeader>
        <p class="text-sm text-muted-foreground">
          {{
            t("iam.grants.deleteConfirm", {
              resource: deleteTarget ? grantResourceLabel(deleteTarget.resource) : "",
            })
          }}
        </p>
        <DialogFooter>
          <Button
            variant="outline"
            @click="deleteTarget = null"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            variant="destructive"
            :disabled="grantSaving"
            @click="handleDeleteGrant"
          >
            {{ t("common.delete") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { KeyRound, Plus, Trash2 } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { listDatabases } from "@/api/database";
import { listGroups } from "@/api/group";
import {
  createIamGrant,
  deleteIamGrant,
  getWorkspaceIamPolicy,
  listIamGrants,
  setWorkspaceIamPolicy,
} from "@/api/iam";
import { listAllInstances } from "@/api/instance";
import { listRoles } from "@/api/role";
import { batchGetUsers, listAllUsers } from "@/api/user";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
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
import { State } from "@/types/proto-es/v1/common_pb";
import type { Database } from "@/types/proto-es/v1/database_service_pb";
import type { Group } from "@/types/proto-es/v1/group_service_pb";
import {
  BindingSchema,
  type IamGrant,
  type IamPolicy,
  IamPolicySchema,
} from "@/types/proto-es/v1/iam_service_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";
import type { Role } from "@/types/proto-es/v1/role_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import {
  ALL_USERS,
  isAllUsersBaselineBinding,
  withAllUsersBaselineBinding,
} from "@/utils/iamPolicy";

type BindingForm = { role: string; members: string[] };
type MemberType = "user" | "group";

const { t } = useI18n();
const authStore = useAuthStore();
const { formatError, handleError, showSuccess } = useErrorHandler();

const canSet = computed(() =>
  authStore.hasPermission("metaxisdata.iam.setPolicy")
);

const bindings = ref<BindingForm[]>([]);
const etag = ref("");
const roles = ref<Role[]>([]);
const users = ref<User[]>([]);

// A resolved member from an existing policy binding or grant may be a deleted
// account: the label still needs its row, but it must not be offered again as a
// new assignment.
const assignableUsers = computed(() =>
  users.value.filter((user) => user.state !== State.DELETED)
);
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

const grants = ref<IamGrant[]>([]);
const grantsLoading = ref(false);
const grantsError = ref<string | null>(null);
const instances = ref<Instance[]>([]);
const databases = ref<Database[]>([]);
const showAddGrant = ref(false);
const grantSaving = ref(false);
const grantError = ref<string | null>(null);
const deleteTarget = ref<IamGrant | null>(null);
const grantScopeType = ref<"instance" | "database">("database");
const grantInstance = ref("");
const grantDatabase = ref("");
const grantRole = ref("");
const grantCondition = ref("");
const instancesError = ref("");

const grantResource = computed(() =>
  grantScopeType.value === "instance"
    ? grantInstance.value
    : grantDatabase.value
);

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
// adds nothing; keep it off the picker. allUsers is not offered either: the
// server only accepts it on that baseline binding, which is server-managed.
const grantableRoles = computed(() =>
  roles.value.filter((role) => role.name !== "roles/workspaceMember")
);

// Workspace-wide roles are not meaningful at a resource scope; a scoped grant
// binds qualityOwner or a custom role.
const SCOPED_GRANT_EXCLUDED_ROLES = new Set([
  "roles/workspaceAdmin",
  "roles/workspaceMember",
  "roles/qualityAgent",
]);
const grantScopedRoles = computed(() =>
  grantableRoles.value.filter(
    (role) => !SCOPED_GRANT_EXCLUDED_ROLES.has(role.name)
  )
);

const selectedMember = computed(() => {
  switch (memberType.value) {
    case "user":
      return selectedUser.value;
    case "group":
      return selectedGroup.value;
    default:
      return "";
  }
});

function roleTitle(role: string): string {
  return roleTitles.value.get(role) ?? role;
}

function memberLabel(member: string): string {
  if (member === ALL_USERS) {
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
    const [policyResponse, roleResponse, allUsers, groupResponse] =
      await Promise.all([
        getWorkspaceIamPolicy(),
        listRoles(),
        listAllUsers(),
        listGroups(),
      ]);
    etag.value = policyResponse.etag;
    bindings.value = toBindings(policyResponse.policy);
    roles.value = roleResponse.roles;
    users.value = allUsers;
    groups.value = groupResponse.groups;
    await resolveUserMembers();
    dirty.value = false;
  } catch (err) {
    error.value = formatError(err);
  } finally {
    isLoading.value = false;
  }
}

// toBindings copies a policy into the local draft. A full replace must carry
// the server-managed allUsers binding, so an editable draft gets it back when
// the stored policy lost it: the save then repairs the policy instead of being
// rejected.
function toBindings(policy: IamPolicy | undefined): BindingForm[] {
  const stored =
    policy?.bindings.map((binding) => ({
      role: binding.role,
      members: [...binding.members],
    })) ?? [];
  return canSet.value ? withAllUsersBaselineBinding(stored) : stored;
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
    bindings.value = toBindings(response.policy);
    dirty.value = false;
    showSuccess(t("iam.policy.saved"));
  } catch (err) {
    // A rejected write (etag conflict, unknown role, last-admin guard) leaves
    // the local draft in place; the operator can reload after reading the
    // message. handleError already reports the server's message.
    handleError(err, "iam.policy.saveFailed");
  } finally {
    isSaving.value = false;
  }
}

// Internal principals (quality agents) and deleted users are hidden from the
// directory, but a policy binding or grant may still name them. Resolve every
// users/{id} reference so the page shows an identity, not a raw resource name.
async function resolveUserMembers() {
  const known = new Set(users.value.map((user) => user.name));
  const unresolved = new Set<string>();
  for (const binding of bindings.value) {
    for (const member of binding.members) {
      if (member.startsWith("users/") && !known.has(member)) {
        unresolved.add(member);
      }
    }
  }
  for (const grant of grants.value) {
    for (const name of [grant.member, grant.createdBy]) {
      if (name && name.startsWith("users/") && !known.has(name)) {
        unresolved.add(name);
      }
    }
  }
  if (unresolved.size === 0) {
    return;
  }
  try {
    const response = await batchGetUsers([...unresolved]);
    const knownNames = new Set(users.value.map((user) => user.name));
    for (const user of response.users) {
      if (knownNames.has(user.name)) {
        continue;
      }
      users.value.push(user);
      knownNames.add(user.name);
    }
  } catch {
    // Keep the raw resource name as the label; the policy and grants are unaffected.
  }
}

async function loadGrants() {
  grantsLoading.value = true;
  grantsError.value = null;
  try {
    grants.value = await listIamGrants();
    await resolveUserMembers();
  } catch (err) {
    grantsError.value = formatError(err);
  } finally {
    grantsLoading.value = false;
  }
}

async function loadInstances() {
  instancesError.value = "";
  try {
    instances.value = await listAllInstances();
  } catch (err) {
    // The picker stays empty; the hint explains why the Create button is disabled.
    instances.value = [];
    instancesError.value = formatError(err);
  }
}

async function reloadAll() {
  void loadInstances();
  await Promise.all([loadPolicy(), loadGrants()]);
}

async function loadDatabases(instance: string) {
  databases.value = [];
  grantDatabase.value = "";
  if (!instance) {
    return;
  }
  try {
    let pageToken = "";
    do {
      const response = await listDatabases({
        parent: instance,
        pageSize: 100,
        pageToken,
      });
      databases.value = [...databases.value, ...response.databases];
      pageToken = response.nextPageToken;
    } while (pageToken);
  } catch (err) {
    grantError.value = formatError(err);
  }
}

function grantResourceLabel(resource: string): string {
  const database = /^instances\/([^/]+)\/databases\/(.*)$/.exec(resource);
  if (database) {
    return `${database[1]} / ${database[2]}`;
  }
  const instance = /^instances\/([^/]+)$/.exec(resource);
  if (instance) {
    return instance[1] ?? resource;
  }
  return resource;
}

function databaseLabel(name: string): string {
  const parts = name.split("/");
  return parts[parts.length - 1] ?? name;
}

function openAddGrant() {
  grantError.value = null;
  grantScopeType.value = "database";
  grantInstance.value = instances.value[0]?.name ?? "";
  grantDatabase.value = "";
  grantRole.value = "";
  grantCondition.value = "";
  memberType.value = "user";
  selectedUser.value = "";
  selectedGroup.value = "";
  if (grantInstance.value) {
    void loadDatabases(grantInstance.value);
  }
  showAddGrant.value = true;
}

function setGrantInstance(value: unknown) {
  grantInstance.value = String(value);
  void loadDatabases(grantInstance.value);
}

async function handleCreateGrant() {
  if (!grantResource.value || !grantRole.value || !selectedMember.value) {
    return;
  }
  grantSaving.value = true;
  grantError.value = null;
  try {
    await createIamGrant({
      resource: grantResource.value,
      role: grantRole.value,
      member: selectedMember.value,
      condition: grantCondition.value.trim() || undefined,
    });
    showAddGrant.value = false;
    showSuccess(t("iam.grants.created"));
    await loadGrants();
  } catch (err) {
    grantError.value = formatError(err);
  } finally {
    grantSaving.value = false;
  }
}

async function handleDeleteGrant() {
  const target = deleteTarget.value;
  if (!target) {
    return;
  }
  grantSaving.value = true;
  grantError.value = null;
  try {
    await deleteIamGrant(target.name);
    deleteTarget.value = null;
    showSuccess(t("iam.grants.deleted"));
    await loadGrants();
  } catch (err) {
    deleteTarget.value = null;
    handleError(err, "iam.grants.deleteFailed");
  } finally {
    grantSaving.value = false;
  }
}

function closeDeleteDialog(open: boolean) {
  if (!open) {
    deleteTarget.value = null;
  }
}

onMounted(() => {
  void loadPolicy();
  void loadGrants();
  void loadInstances();
});
</script>
