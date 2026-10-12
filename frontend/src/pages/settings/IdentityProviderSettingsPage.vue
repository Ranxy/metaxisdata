<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('identityProviderSettings.title')"
      :description="t('identityProviderSettings.description')"
    >
      <template #actions>
        <Button
          v-if="canCreate"
          @click="startCreate"
        >
          <Plus class="mr-2 h-4 w-4" />
          {{ t("identityProviderSettings.add") }}
        </Button>
      </template>
    </PageHeader>

    <p
      v-if="!canList"
      class="text-sm text-muted-foreground"
    >
      {{ t("identityProviderSettings.noPermission") }}
    </p>

    <Card>
      <CardContent class="p-0">
        <PageState :loading="isLoading">
          <EmptyState
            v-if="identityProviders.length === 0"
            :icon="ShieldCheck"
            :title="t('identityProviderSettings.empty')"
            :description="t('identityProviderSettings.emptyDescription')"
          />
          <Table v-else>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("identityProviderSettings.columnTitle") }}</TableHead>
                <TableHead>{{ t("identityProviderSettings.columnId") }}</TableHead>
                <TableHead>{{ t("identityProviderSettings.columnDomain") }}</TableHead>
                <TableHead>{{ t("identityProviderSettings.columnSubject") }}</TableHead>
                <TableHead class="text-right">
                  {{ t("common.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="identityProvider in identityProviders"
                :key="identityProvider.name"
              >
                <TableCell class="font-medium">
                  {{ identityProvider.title }}
                </TableCell>
                <TableCell class="font-mono text-xs text-muted-foreground">
                  {{ identityProviderId(identityProvider.name) }}
                </TableCell>
                <TableCell class="text-muted-foreground">
                  {{ identityProvider.domain || t("identityProviderSettings.none") }}
                </TableCell>
                <TableCell class="font-mono text-xs text-muted-foreground">
                  {{ identityProvider.config?.config?.value?.fieldMapping?.subject || t("identityProviderSettings.none") }}
                </TableCell>
                <TableCell class="text-right">
                  <Button
                    v-if="canUpdate"
                    variant="ghost"
                    size="icon"
                    :title="t('identityProviderSettings.testConnection')"
                    :disabled="isStartingSSO"
                    @click="testConnection(identityProvider)"
                  >
                    <PlugZap class="h-4 w-4 text-muted-foreground" />
                  </Button>
                  <Button
                    v-if="canUpdate"
                    variant="ghost"
                    size="icon"
                    :title="t('common.edit')"
                    @click="startEdit(identityProvider)"
                  >
                    <Pencil class="h-4 w-4 text-muted-foreground" />
                  </Button>
                  <Button
                    v-if="canDelete"
                    variant="ghost"
                    size="icon"
                    :title="t('common.delete')"
                    @click="deleteTarget = identityProvider"
                  >
                    <Trash2 class="h-4 w-4 text-muted-foreground hover:text-destructive" />
                  </Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </PageState>
      </CardContent>
    </Card>

    <template v-if="canGet">
      <div class="space-y-2">
        <h2 class="text-lg font-semibold">
          {{ t("identityProviderSettings.bindingsTitle") }}
        </h2>
        <p class="text-sm text-muted-foreground">
          {{ t("identityProviderSettings.bindingsDescription") }}
        </p>
      </div>

      <Card>
        <CardContent class="p-0">
          <PageState :loading="isLoadingBindings">
            <EmptyState
              v-if="bindings.length === 0"
              :icon="Link2Off"
              :title="t('identityProviderSettings.bindingsEmpty')"
            />
            <Table v-else>
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t("identityProviderSettings.bindingUser") }}</TableHead>
                  <TableHead>{{ t("identityProviderSettings.bindingIdp") }}</TableHead>
                  <TableHead>{{ t("identityProviderSettings.bindingSubject") }}</TableHead>
                  <TableHead class="text-right">
                    {{ t("common.actions") }}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow
                  v-for="binding in bindings"
                  :key="`${binding.user}/${binding.idp}/${binding.subject}`"
                >
                  <TableCell>
                    <div class="font-medium">
                      {{ binding.userTitle || binding.userEmail }}
                    </div>
                    <div class="text-xs text-muted-foreground">
                      {{ binding.userEmail }}
                    </div>
                  </TableCell>
                  <TableCell class="font-mono text-xs">
                    {{ identityProviderId(binding.idp) }}
                  </TableCell>
                  <TableCell class="min-w-0 break-all font-mono text-xs text-muted-foreground">
                    {{ binding.subject }}
                  </TableCell>
                  <TableCell class="text-right">
                    <Button
                      v-if="canUpdate"
                      variant="ghost"
                      size="icon"
                      :title="t('identityProviderSettings.unbind')"
                      @click="unbindTarget = binding"
                    >
                      <Unlink class="h-4 w-4 text-muted-foreground hover:text-destructive" />
                    </Button>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </PageState>
        </CardContent>
      </Card>
    </template>

    <!-- Create / edit -->
    <Dialog v-model:open="showForm">
      <DialogContent class="max-h-[85vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {{ editing ? t("identityProviderSettings.editTitle") : t("identityProviderSettings.createTitle") }}
          </DialogTitle>
        </DialogHeader>

        <div class="space-y-4">
          <FormField
            v-model="form.title"
            :label="t('identityProviderSettings.fieldTitle')"
            :placeholder="t('identityProviderSettings.fieldTitlePlaceholder')"
            required
          />

          <FormField
            v-if="!editing"
            v-model="form.id"
            :label="t('identityProviderSettings.fieldId')"
            :placeholder="t('identityProviderSettings.fieldIdPlaceholder')"
            :hint="t('identityProviderSettings.fieldIdHint')"
            required
          />

          <FormField
            v-model="form.domain"
            :label="t('identityProviderSettings.fieldDomain')"
            :placeholder="t('identityProviderSettings.fieldDomainPlaceholder')"
            :hint="t('identityProviderSettings.fieldDomainHint')"
          />

          <div class="grid gap-4 sm:grid-cols-2">
            <FormField
              v-model="form.clientId"
              :label="t('identityProviderSettings.fieldClientId')"
              :placeholder="t('identityProviderSettings.fieldClientIdPlaceholder')"
              required
            />
            <FormField
              v-model="form.clientSecret"
              type="password"
              :label="t('identityProviderSettings.fieldClientSecret')"
              :placeholder="
                editing
                  ? t('identityProviderSettings.fieldClientSecretKept')
                  : t('identityProviderSettings.fieldClientSecretPlaceholder')
              "
              required
            />
          </div>

          <FormField
            v-model="form.authUrl"
            :label="t('identityProviderSettings.fieldAuthUrl')"
            :placeholder="t('identityProviderSettings.fieldAuthUrlPlaceholder')"
            required
          />
          <FormField
            v-model="form.tokenUrl"
            :label="t('identityProviderSettings.fieldTokenUrl')"
            :placeholder="t('identityProviderSettings.fieldTokenUrlPlaceholder')"
            required
          />
          <FormField
            v-model="form.userInfoUrl"
            :label="t('identityProviderSettings.fieldUserInfoUrl')"
            :placeholder="t('identityProviderSettings.fieldUserInfoUrlPlaceholder')"
            required
          />
          <FormField
            v-model="form.scopes"
            :label="t('identityProviderSettings.fieldScopes')"
            :placeholder="t('identityProviderSettings.fieldScopesPlaceholder')"
            :hint="t('identityProviderSettings.fieldScopesHint')"
          />

          <div class="space-y-2">
            <Label>{{ t("identityProviderSettings.fieldMappingTitle") }}</Label>
            <p class="text-xs text-muted-foreground">
              {{ t("identityProviderSettings.fieldMappingDescription") }}
            </p>
          </div>

          <div class="grid gap-4 sm:grid-cols-2">
            <FormField
              v-model="form.identifier"
              :label="t('identityProviderSettings.fieldIdentifier')"
              :placeholder="t('identityProviderSettings.fieldIdentifierPlaceholder')"
              :hint="t('identityProviderSettings.fieldIdentifierHint')"
              required
            />
            <FormField
              v-model="form.displayName"
              :label="t('identityProviderSettings.fieldDisplayName')"
              :placeholder="t('identityProviderSettings.fieldDisplayNamePlaceholder')"
            />
            <FormField
              v-model="form.subject"
              :label="t('identityProviderSettings.fieldSubject')"
              :placeholder="t('identityProviderSettings.fieldSubjectPlaceholder')"
              :hint="t('identityProviderSettings.fieldSubjectHint')"
            />
            <FormField
              v-model="form.groups"
              :label="t('identityProviderSettings.fieldGroups')"
              :placeholder="t('identityProviderSettings.fieldGroupsPlaceholder')"
              :hint="t('identityProviderSettings.fieldGroupsHint')"
            />
          </div>

          <div class="space-y-2">
            <Label>{{ t("identityProviderSettings.fieldAuthStyle") }}</Label>
            <Select v-model="form.authStyle">
              <SelectTrigger class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="IN_PARAMS">
                  {{ t("identityProviderSettings.fieldAuthStyleInParams") }}
                </SelectItem>
                <SelectItem value="IN_HEADER">
                  {{ t("identityProviderSettings.fieldAuthStyleInHeader") }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="flex items-start gap-3 rounded-md border p-3">
            <Checkbox
              id="skip-tls-verify"
              :checked="form.skipTlsVerify"
              @update:checked="form.skipTlsVerify = $event === true"
            />
            <div class="grid gap-1">
              <Label for="skip-tls-verify">
                {{ t("identityProviderSettings.fieldSkipTlsVerify") }}
              </Label>
              <p
                v-if="form.skipTlsVerify"
                class="text-xs text-destructive"
              >
                {{ t("identityProviderSettings.fieldSkipTlsVerifyWarning") }}
              </p>
              <p
                v-else
                class="text-xs text-muted-foreground"
              >
                {{ t("identityProviderSettings.fieldSkipTlsVerifyHint") }}
              </p>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            @click="showForm = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="isSaving"
            @click="save"
          >
            <Loader2
              v-if="isSaving"
              class="mr-2 h-4 w-4 animate-spin"
            />
            {{ t("common.apply") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <ConfirmDeleteDialog
      v-model="isDeleteOpen"
      :title="t('identityProviderSettings.deleteTitle')"
      :message="t('identityProviderSettings.deleteMessage')"
      :item-name="deleteTarget?.title ?? ''"
      :loading="isDeleting"
      @confirm="confirmDelete"
    />

    <ConfirmDeleteDialog
      v-model="isUnbindOpen"
      :title="t('identityProviderSettings.unbindTitle')"
      :message="t('identityProviderSettings.unbindMessage')"
      :item-name="unbindTarget ? `${unbindTarget.userEmail} — ${unbindTarget.subject}` : ''"
      :loading="isUnbinding"
      @confirm="confirmUnbind"
    />
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import {
  Link2Off,
  Loader2,
  Pencil,
  PlugZap,
  Plus,
  ShieldCheck,
  Trash2,
  Unlink,
} from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  createIdentityProvider,
  deleteIdentityProvider,
  deleteIdentityProviderBinding,
  identityProviderId,
  listIdentityProviderBindings,
  listIdentityProviders,
  updateIdentityProvider,
} from "@/api/identity-provider";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
import { useSSOLogin } from "@/composables/useSSOLogin";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type IdentityProvider,
  type IdentityProviderBinding,
  IdentityProviderSchema,
  IdentityProviderType,
  OAuth2AuthStyle,
} from "@/types/proto-es/v1/identity_provider_service_pb";
import { slugify } from "@/utils/slug";

const { t } = useI18n();
const authStore = useAuthStore();
const { formatError } = useErrorHandler();
const { isStarting: isStartingSSO, start: startSSOLogin } = useSSOLogin();

const canList = computed(() =>
  authStore.hasPermission("metaxisdata.identityProviders.list")
);
const canGet = computed(() =>
  authStore.hasPermission("metaxisdata.identityProviders.get")
);
const canCreate = computed(() =>
  authStore.hasPermission("metaxisdata.identityProviders.create")
);
const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.identityProviders.update")
);
const canDelete = computed(() =>
  authStore.hasPermission("metaxisdata.identityProviders.delete")
);

const isLoading = ref(true);
const isLoadingBindings = ref(false);
const identityProviders = ref<IdentityProvider[]>([]);
const bindings = ref<IdentityProviderBinding[]>([]);

// The client id and the secret are the only required non-URL fields, and the
// server re-validates all of it; this list exists to point at the field that is
// wrong rather than at a generic failure.
const ID_PATTERN = /^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$/;

interface ProviderForm {
  id: string;
  title: string;
  domain: string;
  authUrl: string;
  tokenUrl: string;
  userInfoUrl: string;
  clientId: string;
  clientSecret: string;
  scopes: string;
  identifier: string;
  displayName: string;
  subject: string;
  groups: string;
  skipTlsVerify: boolean;
  authStyle: "IN_PARAMS" | "IN_HEADER";
}

function emptyForm(): ProviderForm {
  return {
    id: "",
    title: "",
    domain: "",
    authUrl: "",
    tokenUrl: "",
    userInfoUrl: "",
    clientId: "",
    clientSecret: "",
    scopes: "openid email profile",
    identifier: "email",
    displayName: "name",
    subject: "sub",
    groups: "",
    skipTlsVerify: false,
    authStyle: "IN_PARAMS",
  };
}

const showForm = ref(false);
const editing = ref<IdentityProvider | null>(null);
const form = ref<ProviderForm>(emptyForm());
const isSaving = ref(false);
// Set once the operator types an id, so editing the title stops rewriting it.
// A title with no ASCII letters slugifies to nothing, and the id is immutable
// after create: it has to be a deliberate choice, not an accident of the title.
let idTouched = false;

const deleteTarget = ref<IdentityProvider | null>(null);
const isDeleting = ref(false);
const unbindTarget = ref<IdentityProviderBinding | null>(null);
const isUnbinding = ref(false);

const isDeleteOpen = computed({
  get: () => deleteTarget.value !== null,
  set: (open: boolean) => {
    if (!open) {
      deleteTarget.value = null;
    }
  },
});

const isUnbindOpen = computed({
  get: () => unbindTarget.value !== null,
  set: (open: boolean) => {
    if (!open) {
      unbindTarget.value = null;
    }
  },
});

watch(
  () => form.value.title,
  (title) => {
    if (!editing.value && !idTouched) {
      form.value.id = slugify(title);
    }
  }
);

function startCreate() {
  editing.value = null;
  form.value = emptyForm();
  idTouched = false;
  showForm.value = true;
}

function startEdit(identityProvider: IdentityProvider) {
  const config = identityProvider.config?.config?.value;
  editing.value = identityProvider;
  idTouched = true;
  form.value = {
    id: identityProviderId(identityProvider.name),
    title: identityProvider.title,
    domain: identityProvider.domain,
    authUrl: config?.authUrl ?? "",
    tokenUrl: config?.tokenUrl ?? "",
    userInfoUrl: config?.userInfoUrl ?? "",
    clientId: config?.clientId ?? "",
    // The read path never returns the stored secret, so an empty field means
    // "keep it". The hint on the field says so.
    clientSecret: "",
    scopes: (config?.scopes ?? []).join(" "),
    identifier: config?.fieldMapping?.identifier ?? "",
    displayName: config?.fieldMapping?.displayName ?? "",
    subject: config?.fieldMapping?.subject ?? "",
    groups: config?.fieldMapping?.groups ?? "",
    skipTlsVerify: config?.skipTlsVerify ?? false,
    authStyle:
      config?.authStyle === OAuth2AuthStyle.IN_HEADER
        ? "IN_HEADER"
        : "IN_PARAMS",
  };
  showForm.value = true;
}

function validate(): string | null {
  const value = form.value;
  if (!value.title.trim()) {
    return t("identityProviderSettings.invalidTitle");
  }
  if (!editing.value && !ID_PATTERN.test(value.id)) {
    return t("identityProviderSettings.invalidId");
  }
  if (!value.clientId.trim()) {
    return t("identityProviderSettings.invalidClientId");
  }
  if (!value.clientSecret && !editing.value) {
    return t("identityProviderSettings.invalidClientSecret");
  }
  // Each URL is named on its own so the message points at the field that is
  // empty; a table would need a dynamic key the i18n checker cannot follow.
  if (!value.authUrl.trim()) {
    return t("identityProviderSettings.invalidAuthUrl");
  }
  if (!value.tokenUrl.trim()) {
    return t("identityProviderSettings.invalidTokenUrl");
  }
  if (!value.userInfoUrl.trim()) {
    return t("identityProviderSettings.invalidUserInfoUrl");
  }
  if (!value.identifier.trim()) {
    return t("identityProviderSettings.invalidIdentifier");
  }
  return null;
}

function buildConfig() {
  return {
    config: {
      case: "oauth2Config" as const,
      value: {
        authUrl: form.value.authUrl.trim(),
        tokenUrl: form.value.tokenUrl.trim(),
        userInfoUrl: form.value.userInfoUrl.trim(),
        clientId: form.value.clientId.trim(),
        clientSecret: form.value.clientSecret,
        scopes: form.value.scopes
          .split(/\s+/)
          .map((scope) => scope.trim())
          .filter(Boolean),
        fieldMapping: {
          identifier: form.value.identifier.trim(),
          displayName: form.value.displayName.trim(),
          subject: form.value.subject.trim(),
          groups: form.value.groups.trim(),
        },
        skipTlsVerify: form.value.skipTlsVerify,
        authStyle:
          form.value.authStyle === "IN_HEADER"
            ? OAuth2AuthStyle.IN_HEADER
            : OAuth2AuthStyle.IN_PARAMS,
      },
    },
  };
}

async function save() {
  const invalid = validate();
  if (invalid) {
    notify.error(invalid);
    return;
  }
  isSaving.value = true;
  try {
    const identityProvider = create(IdentityProviderSchema, {
      name: editing.value?.name ?? "",
      title: form.value.title.trim(),
      domain: form.value.domain.trim().toLowerCase(),
      type: IdentityProviderType.OAUTH2,
      config: buildConfig(),
    });
    if (editing.value) {
      await updateIdentityProvider(identityProvider, [
        "title",
        "domain",
        "config",
      ]);
      notify.success(t("identityProviderSettings.saved"));
    } else {
      await createIdentityProvider(form.value.id, identityProvider);
      notify.success(t("identityProviderSettings.created"));
    }
    showForm.value = false;
    await load();
  } catch (error) {
    notify.error(
      editing.value
        ? `${t("identityProviderSettings.saveFailed")}: ${formatError(error)}`
        : `${t("identityProviderSettings.createFailed")}: ${formatError(error)}`
    );
  } finally {
    isSaving.value = false;
  }
}

async function confirmDelete() {
  const target = deleteTarget.value;
  if (!target) {
    return;
  }
  isDeleting.value = true;
  try {
    await deleteIdentityProvider(target.name);
    notify.success(t("identityProviderSettings.deleted"));
    deleteTarget.value = null;
    await load();
  } catch (error) {
    notify.error(
      `${t("identityProviderSettings.deleteFailed")}: ${formatError(error)}`
    );
  } finally {
    isDeleting.value = false;
  }
}

async function confirmUnbind() {
  const target = unbindTarget.value;
  if (!target) {
    return;
  }
  isUnbinding.value = true;
  try {
    await deleteIdentityProviderBinding(
      target.user,
      target.idp,
      target.subject
    );
    notify.success(t("identityProviderSettings.unbound"));
    unbindTarget.value = null;
    await loadBindings();
  } catch (error) {
    notify.error(
      `${t("identityProviderSettings.unbindFailed")}: ${formatError(error)}`
    );
  } finally {
    isUnbinding.value = false;
  }
}

/**
 * Sends the administrator's browser through the provider and back to the
 * callback with the flow marked as a test, which is the only way to prove the
 * client id and secret actually work: OAuth2 has no credential-only probe.
 */
async function testConnection(identityProvider: IdentityProvider) {
  try {
    const started = await startSSOLogin(identityProvider, { mode: "test" });
    if (!started) {
      notify.error(t("identityProviderSettings.testUnavailable"));
    }
  } catch (error) {
    notify.error(formatError(error));
  }
}

async function loadBindings() {
  isLoadingBindings.value = true;
  try {
    bindings.value = await listIdentityProviderBindings({});
  } catch (error) {
    notify.error(
      `${t("identityProviderSettings.bindingsLoadFailed")}: ${formatError(error)}`
    );
  } finally {
    isLoadingBindings.value = false;
  }
}

async function load() {
  isLoading.value = true;
  try {
    identityProviders.value = await listIdentityProviders();
  } catch (error) {
    notify.error(
      `${t("identityProviderSettings.loadFailed")}: ${formatError(error)}`
    );
  } finally {
    isLoading.value = false;
  }
  if (canGet.value) {
    await loadBindings();
  }
}

onMounted(load);
</script>
