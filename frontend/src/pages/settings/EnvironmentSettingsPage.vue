<template>
  <div class="space-y-4">
    <div class="flex items-start justify-between gap-4">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ t("environmentSettings.title") }}
        </h1>
        <p class="text-muted-foreground mt-1">
          {{ t("environmentSettings.description") }}
        </p>
      </div>
      <AppButton
        v-if="canUpdate"
        :disabled="isLoading"
        @click="startCreate"
      >
        <Plus class="mr-2 h-4 w-4" />
        {{ t("environmentSettings.add") }}
      </AppButton>
    </div>

    <p
      v-if="!canUpdate"
      class="text-sm text-muted-foreground"
    >
      {{ t("environmentSettings.readOnlyHint") }}
    </p>

    <Card>
      <CardContent class="p-0">
        <div
          v-if="isLoading"
          class="flex justify-center p-8"
        >
          <AppLoading />
        </div>
        <div
          v-else-if="environments.length === 0"
          class="p-8 text-center text-sm text-muted-foreground"
        >
          {{ t("environmentSettings.empty") }}
        </div>
        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("environmentSettings.nameColumn") }}</TableHead>
              <TableHead>{{ t("environmentSettings.idColumn") }}</TableHead>
              <TableHead>{{ t("environmentSettings.usageColumn") }}</TableHead>
              <TableHead class="text-right">
                {{ t("environmentSettings.actionsColumn") }}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="environment in environments"
              :key="environment.name"
            >
              <TableCell>
                <span class="flex items-center gap-2">
                  <span
                    class="h-2.5 w-2.5 shrink-0 rounded-full"
                    :style="{ backgroundColor: environmentColorHex(environment) }"
                  />
                  {{ environmentTitle(environment) }}
                </span>
              </TableCell>
              <TableCell class="font-mono text-xs text-muted-foreground">
                {{ environmentId(environment.name) }}
              </TableCell>
              <TableCell>
                <Badge variant="secondary">
                  {{
                    t("environmentSettings.instanceCount", {
                      count: environment.instanceCount,
                    })
                  }}
                </Badge>
              </TableCell>
              <TableCell class="text-right">
                <Button
                  v-if="canUpdate"
                  variant="ghost"
                  size="icon"
                  :title="t('common.edit')"
                  @click="startEdit(environment)"
                >
                  <Pencil class="h-4 w-4 text-muted-foreground" />
                </Button>
                <Button
                  v-if="canUpdate"
                  variant="ghost"
                  size="icon"
                  :title="
                    environment.instanceCount > 0
                      ? t('environmentSettings.inUseHint')
                      : t('common.delete')
                  "
                  :disabled="environment.instanceCount > 0"
                  @click="startDelete(environment)"
                >
                  <Trash2 class="h-4 w-4 text-muted-foreground hover:text-destructive" />
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </CardContent>
    </Card>

    <AppModal
      v-model="showEditModal"
      :title="
        editing
          ? t('environmentSettings.editTitle')
          : t('environmentSettings.createTitle')
      "
      size="sm"
    >
      <div class="space-y-4">
        <AppInput
          v-model="formTitle"
          :label="t('environmentSettings.nameLabel')"
          required
          :error="formError"
          @keyup.enter="save"
        />
        <p
          v-if="editing"
          class="text-sm text-muted-foreground"
        >
          {{ t("environmentSettings.idImmutableHint") }}
        </p>
        <div class="space-y-2">
          <Label>{{ t("environmentSettings.colorLabel") }}</Label>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="key in ENVIRONMENT_COLOR_KEYS"
              :key="key"
              type="button"
              class="h-7 w-7 rounded-full border-2 transition-transform hover:scale-110"
              :class="
                formColor === key ? 'border-foreground' : 'border-transparent'
              "
              :style="{ backgroundColor: environmentColorHexByKey(key) }"
              :title="key"
              @click="formColor = key"
            />
          </div>
        </div>
      </div>
      <template #footer>
        <Button
          variant="outline"
          @click="showEditModal = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <AppButton
          :loading="isSaving"
          @click="save"
        >
          {{ t("common.save") }}
        </AppButton>
      </template>
    </AppModal>

    <AppModal
      v-model="showDeleteModal"
      :title="t('environmentSettings.deleteTitle')"
      size="sm"
    >
      <p>
        {{
          t("environmentSettings.deleteConfirm", {
            name: deleting ? environmentTitle(deleting) : "",
          })
        }}
      </p>
      <template #footer>
        <Button
          variant="outline"
          @click="showDeleteModal = false"
        >
          {{ t("common.cancel") }}
        </Button>
        <AppButton
          variant="danger"
          :loading="isDeleting"
          @click="confirmDelete"
        >
          {{ t("common.delete") }}
        </AppButton>
      </template>
    </AppModal>
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { Pencil, Plus, Trash2 } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { environmentId } from "@/api/environment";
import AppButton from "@/components/common/AppButton.vue";
import AppInput from "@/components/common/AppInput.vue";
import AppLoading from "@/components/common/AppLoading.vue";
import AppModal from "@/components/common/AppModal.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
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
import { useEnvironmentStore } from "@/store/modules/environment";
import type { Environment } from "@/types/proto-es/v1/environment_service_pb";
import { EnvironmentSchema } from "@/types/proto-es/v1/environment_service_pb";
import {
  ENVIRONMENT_COLOR_KEYS,
  type EnvironmentColorKey,
  environmentColorHex,
  environmentColorHexByKey,
  environmentColorKey,
} from "@/utils/environment";

const { t } = useI18n();
const authStore = useAuthStore();
const environmentStore = useEnvironmentStore();
const { handleError, showSuccess } = useErrorHandler();

const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.settings.update")
);

const isLoading = ref(false);
const environments = computed(() => environmentStore.environments);

const showEditModal = ref(false);
const editing = ref<Environment | null>(null);
const formTitle = ref("");
const formColor = ref<EnvironmentColorKey>("slate");
const formError = ref("");
const isSaving = ref(false);

const showDeleteModal = ref(false);
const deleting = ref<Environment | null>(null);
const isDeleting = ref(false);

onMounted(() => {
  void fetchEnvironments();
});

async function fetchEnvironments() {
  isLoading.value = true;
  try {
    await environmentStore.fetch(true);
  } catch (error) {
    handleError(error, "error.unknown");
  } finally {
    isLoading.value = false;
  }
}

function environmentTitle(environment: Environment): string {
  return environment.title || environmentId(environment.name);
}

function startCreate() {
  editing.value = null;
  formTitle.value = "";
  formColor.value = ENVIRONMENT_COLOR_KEYS[0];
  formError.value = "";
  showEditModal.value = true;
}

function startEdit(environment: Environment) {
  editing.value = environment;
  formTitle.value = environmentTitle(environment);
  formColor.value = environmentColorKey(environment);
  formError.value = "";
  showEditModal.value = true;
}

async function save() {
  const title = formTitle.value.trim();
  if (!title) {
    formError.value = t("environmentSettings.nameRequired");
    return;
  }
  isSaving.value = true;
  formError.value = "";
  try {
    if (editing.value) {
      const target = create(EnvironmentSchema, {
        name: editing.value.name,
        title,
        color: formColor.value,
        tags: editing.value.tags,
      });
      await environmentStore.update(target, ["title", "color"]);
      showSuccess("environmentSettings.updated");
    } else {
      await environmentStore.create(title, formColor.value);
      showSuccess("environmentSettings.created");
    }
    showEditModal.value = false;
  } catch (error) {
    formError.value =
      error instanceof Error ? error.message : t("error.unknown");
  } finally {
    isSaving.value = false;
  }
}

function startDelete(environment: Environment) {
  deleting.value = environment;
  showDeleteModal.value = true;
}

async function confirmDelete() {
  if (!deleting.value) return;
  isDeleting.value = true;
  try {
    await environmentStore.remove(deleting.value.name);
    showSuccess("environmentSettings.deleted");
    showDeleteModal.value = false;
  } catch (error) {
    handleError(error, "error.unknown");
  } finally {
    isDeleting.value = false;
  }
}
</script>
