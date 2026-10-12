<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('applicationManagement.title')"
      :description="t('applicationManagement.pageDescription')"
    >
      <template #actions>
        <Button
          v-if="canCreate"
          @click="openCreate"
        >
          <Plus class="mr-2 h-4 w-4" />
          {{ t("applicationManagement.create") }}
        </Button>
      </template>
    </PageHeader>

    <Card>
      <PageState
        :loading="isLoading"
        :error="error"
      >
        <EmptyState
          v-if="applications.length === 0"
          :icon="AppWindow"
          :title="t('applicationManagement.noApplications')"
          :description="t('applicationManagement.noApplicationsHint')"
        />

        <div v-else>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("applicationManagement.application") }}</TableHead>
                <TableHead>{{ t("applicationManagement.description") }}</TableHead>
                <TableHead>{{ t("applicationManagement.state") }}</TableHead>
                <TableHead>{{ t("applicationManagement.createdAt") }}</TableHead>
                <TableHead class="w-64 text-right">
                  {{ t("applicationManagement.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="application in applications"
                :key="application.name"
              >
                <TableCell>
                  <div class="font-medium">{{ application.displayName }}</div>
                  <div class="text-xs text-muted-foreground">
                    {{ application.applicationId }}
                  </div>
                </TableCell>
                <TableCell class="text-sm text-muted-foreground">
                  {{ application.description || "-" }}
                </TableCell>
                <TableCell>
                  <Badge :variant="application.disabled ? 'secondary' : 'default'">
                    {{
                      application.disabled
                        ? t("applicationManagement.stopped")
                        : t("applicationManagement.active")
                    }}
                  </Badge>
                </TableCell>
                <TableCell class="text-sm">
                  {{ formatDateTime(application.createdAt, locale, { seconds: false }) }}
                </TableCell>
                <TableCell class="text-right">
                  <div class="flex justify-end gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      @click="openKeys(application)"
                    >
                      <KeyRound class="mr-2 h-4 w-4" />
                      {{ t("applicationManagement.keys") }}
                    </Button>
                    <Button
                      v-if="canUpdate"
                      variant="outline"
                      size="sm"
                      @click="toggleDisabled(application)"
                    >
                      {{
                        application.disabled
                          ? t("applicationManagement.enable")
                          : t("applicationManagement.stop")
                      }}
                    </Button>
                    <Button
                      v-if="canDelete"
                      variant="outline"
                      size="sm"
                      @click="confirmDelete(application)"
                    >
                      <Trash2 class="h-4 w-4" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>

          <TablePager
            v-model:page-size="pageSize"
            :has-previous="hasPrevious"
            :has-next="hasNext"
            :disabled="isLoading"
            @previous="goToPreviousPage"
            @next="goToNextPage"
          />
        </div>
      </PageState>
    </Card>

    <!-- Creating an application issues its first key, and the plaintext key is shown once. -->
    <Dialog
      :open="createOpen"
      @update:open="(open: boolean) => (createOpen = open)"
    >
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("applicationManagement.create") }}</DialogTitle>
          <DialogDescription>
            {{ t("applicationManagement.createHint") }}
          </DialogDescription>
        </DialogHeader>

        <div
          v-if="!createdKey"
          class="space-y-3"
        >
          <div class="space-y-1">
            <Label for="application-name">
              {{ t("applicationManagement.displayName") }}
            </Label>
            <Input
              id="application-name"
              v-model="createForm.displayName"
              :placeholder="t('applicationManagement.displayNamePlaceholder')"
            />
          </div>
          <div class="space-y-1">
            <Label for="application-description">
              {{ t("applicationManagement.description") }}
            </Label>
            <Input
              id="application-description"
              v-model="createForm.description"
            />
          </div>
        </div>

        <div
          v-else
          class="space-y-2"
        >
          <p class="text-sm text-muted-foreground">
            {{ t("applicationManagement.keyOnce") }}
          </p>
          <pre
            class="overflow-x-auto rounded-md border bg-muted p-3 text-xs"
            data-testid="created-key"
          >{{ createdKey }}</pre>
        </div>

        <DialogFooter>
          <template v-if="!createdKey">
            <Button
              variant="outline"
              @click="createOpen = false"
            >
              {{ t("common.cancel") }}
            </Button>
            <Button
              :disabled="saving || !createForm.displayName.trim()"
              @click="handleCreate"
            >
              {{ t("applicationManagement.create") }}
            </Button>
          </template>
          <Button
            v-else
            @click="dismissCreatedKey"
          >
            {{ t("common.close") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Keys: the masked list, issuing another one, and revoking. -->
    <Dialog
      :open="keysOpen"
      @update:open="(open: boolean) => (keysOpen = open)"
    >
      <DialogContent class="max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {{ t("applicationManagement.keysOf", { name: keysFor?.displayName ?? "" }) }}
          </DialogTitle>
          <DialogDescription>
            {{ t("applicationManagement.keysHint") }}
          </DialogDescription>
        </DialogHeader>

        <div
          v-if="issuedKey"
          class="space-y-2"
        >
          <p class="text-sm text-muted-foreground">
            {{ t("applicationManagement.keyOnce") }}
          </p>
          <pre
            class="overflow-x-auto rounded-md border bg-muted p-3 text-xs"
            data-testid="issued-key"
          >{{ issuedKey }}</pre>
        </div>

        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("applicationManagement.key") }}</TableHead>
              <TableHead>{{ t("applicationManagement.description") }}</TableHead>
              <TableHead>{{ t("applicationManagement.lastUsedAt") }}</TableHead>
              <TableHead>{{ t("applicationManagement.counters") }}</TableHead>
              <TableHead class="w-24 text-right" />
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="key in keys"
              :key="key.name"
            >
              <!-- A masked key is 60 characters of monospace; allowed to size the
                   column it pushes the revoke button out of the dialog. It wraps
                   instead, which keeps every character of the mask visible. -->
              <TableCell class="max-w-[18rem] break-all font-mono text-xs">
                {{ key.maskedKey }}
              </TableCell>
              <TableCell class="text-sm">{{ key.description || "-" }}</TableCell>
              <TableCell class="text-sm">
                {{ key.lastUsedAt ? formatDateTime(key.lastUsedAt, locale) : "-" }}
              </TableCell>
              <TableCell class="whitespace-nowrap text-xs text-muted-foreground">
                {{ t("applicationManagement.counterValue", {
                  accepted: key.statementAcceptedCount,
                  rejected: key.statementRejectedCount,
                }) }}
              </TableCell>
              <TableCell class="text-right">
                <Button
                  v-if="canManageKeys"
                  variant="outline"
                  size="sm"
                  @click="handleRevokeKey(key.name)"
                >
                  {{ t("applicationManagement.revoke") }}
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>

        <DialogFooter>
          <Button
            v-if="!issuedKey && canManageKeys"
            variant="outline"
            :disabled="saving"
            @click="handleIssueKey"
          >
            {{ t("applicationManagement.issueKey") }}
          </Button>
          <Button @click="keysOpen = false">
            {{ t("common.close") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <ConfirmDeleteDialog
      v-model="deleteOpen"
      :title="t('applicationManagement.deleteTitle')"
      :message="t('applicationManagement.deleteConfirm')"
      :item-name="deleting?.displayName"
      :loading="saving"
      @confirm="handleDelete"
    />
  </div>
</template>

<script setup lang="ts">
import { AppWindow, KeyRound, Plus, Trash2 } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  createApplication,
  createApplicationAPIKey,
  deleteApplication,
  listApplicationAPIKeys,
  listApplications,
  revokeApplicationAPIKey,
  updateApplication,
} from "@/api/application";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import Badge from "@/components/ui/badge/Badge.vue";
import Button from "@/components/ui/button/Button.vue";
import Card from "@/components/ui/card/Card.vue";
import Dialog from "@/components/ui/dialog/Dialog.vue";
import DialogContent from "@/components/ui/dialog/DialogContent.vue";
import DialogDescription from "@/components/ui/dialog/DialogDescription.vue";
import DialogFooter from "@/components/ui/dialog/DialogFooter.vue";
import DialogHeader from "@/components/ui/dialog/DialogHeader.vue";
import DialogTitle from "@/components/ui/dialog/DialogTitle.vue";
import Input from "@/components/ui/input/Input.vue";
import Label from "@/components/ui/label/Label.vue";
import Table from "@/components/ui/table/Table.vue";
import TableBody from "@/components/ui/table/TableBody.vue";
import TableCell from "@/components/ui/table/TableCell.vue";
import TableHead from "@/components/ui/table/TableHead.vue";
import TableHeader from "@/components/ui/table/TableHeader.vue";
import TableRow from "@/components/ui/table/TableRow.vue";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import type {
  Application,
  ApplicationAPIKey,
} from "@/types/proto-es/v1/application_service_pb";
import { formatDateTime } from "@/utils/datetime";

const { t, locale } = useI18n();
const { handleError } = useErrorHandler();
const authStore = useAuthStore();

const canCreate = computed(() =>
  authStore.hasPermission("metaxisdata.applications.create")
);
const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.applications.update")
);
const canDelete = computed(() =>
  authStore.hasPermission("metaxisdata.applications.delete")
);
const canManageKeys = computed(() =>
  authStore.hasPermission("metaxisdata.applications.apiKeys.create")
);

const error = ref("");
const saving = ref(false);
const pageSize = ref(50);
const showDisabled = ref(true);

const createOpen = ref(false);
const createForm = ref({ displayName: "", description: "" });
const createdKey = ref("");

const keysOpen = ref(false);
const keysFor = ref<Application>();
const keys = ref<ApplicationAPIKey[]>([]);
const issuedKey = ref("");

const deleteOpen = ref(false);
const deleting = ref<Application>();

const {
  items: applications,
  isLoading,
  hasNext,
  hasPrevious,
  reset: resetPage,
  refresh: refreshPage,
  goNext: goToNextPage,
  goPrevious: goToPreviousPage,
} = usePagedFetch<Application>({
  fetchPage: async (pageToken, signal) => {
    const response = await listApplications({
      pageSize: pageSize.value,
      pageToken,
      showDisabled: showDisabled.value,
      signal,
    });
    return {
      items: response.applications,
      nextPageToken: response.nextPageToken,
    };
  },
  onError: (e) => {
    error.value = handleError(e, "applicationManagement.fetchError");
  },
});

// A bigger page restarts the walk: a cursor belongs to the query that produced it.
watch(pageSize, () => void resetPage());
watch(showDisabled, () => void resetPage());

function openCreate() {
  createForm.value = { displayName: "", description: "" };
  createdKey.value = "";
  createOpen.value = true;
}

function dismissCreatedKey() {
  createdKey.value = "";
  createOpen.value = false;
}

async function handleCreate() {
  saving.value = true;
  try {
    const response = await createApplication({
      displayName: createForm.value.displayName.trim(),
      description: createForm.value.description.trim(),
    });
    // The key is only ever returned here, so it is held on screen until the operator
    // dismisses it rather than shown as a toast that fades.
    createdKey.value = response.apiKey;
    await refreshPage();
  } catch (e: unknown) {
    handleError(e, "applicationManagement.createError");
  } finally {
    saving.value = false;
  }
}

async function openKeys(application: Application) {
  keysFor.value = application;
  issuedKey.value = "";
  keysOpen.value = true;
  try {
    const response = await listApplicationAPIKeys({ parent: application.name });
    keys.value = response.keys;
  } catch (e: unknown) {
    handleError(e, "applicationManagement.keysFetchError");
  }
}

async function handleIssueKey() {
  if (!keysFor.value) return;
  saving.value = true;
  try {
    const response = await createApplicationAPIKey({
      parent: keysFor.value.name,
      description: keysFor.value.displayName,
    });
    issuedKey.value = response.apiKey;
  } catch (e: unknown) {
    handleError(e, "applicationManagement.keyCreateError");
  } finally {
    saving.value = false;
  }
}

async function handleRevokeKey(name: string) {
  try {
    await revokeApplicationAPIKey(name);
    if (keysFor.value) {
      const response = await listApplicationAPIKeys({
        parent: keysFor.value.name,
      });
      keys.value = response.keys;
    }
  } catch (e: unknown) {
    handleError(e, "applicationManagement.keyRevokeError");
  }
}

async function toggleDisabled(application: Application) {
  try {
    await updateApplication({
      name: application.name,
      disabled: !application.disabled,
    });
    await refreshPage();
  } catch (e: unknown) {
    handleError(e, "applicationManagement.updateError");
  }
}

function confirmDelete(application: Application) {
  deleting.value = application;
  deleteOpen.value = true;
}

async function handleDelete() {
  if (!deleting.value) return;
  saving.value = true;
  try {
    await deleteApplication(deleting.value.name);
    notify.success(t("applicationManagement.deleted"));
    deleteOpen.value = false;
    await refreshPage();
  } catch (e: unknown) {
    handleError(e, "applicationManagement.deleteError");
  } finally {
    saving.value = false;
  }
}

onMounted(async () => {
  await resetPage();
});
</script>
