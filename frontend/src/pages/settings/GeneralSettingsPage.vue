<template>
  <div class="space-y-4">
    <!-- One save for the whole form lives in the page header, like every other
         settings page (IAM). It used to be a sticky bottom bar that floated over
         the help text and the MCP toggle while scrolling, was offset from the
         form column, and contradicted the header-based save everywhere else. -->
    <PageHeader
      :title="t('generalSettings.title')"
      :description="t('generalSettings.description')"
    >
      <template #actions>
        <span
          v-if="!canUpdate"
          class="text-sm text-muted-foreground"
        >
          {{ t("generalSettings.readOnlyHint") }}
        </span>
        <span
          v-else-if="isDirty && !isLoading && !error"
          class="text-sm text-muted-foreground"
        >
          {{ t("generalSettings.unsavedHint") }}
        </span>
        <Button
          :disabled="isLoading || !!error || isSaving || !canUpdate || !!retentionError"
          @click="handleSave"
        >
          {{ t("common.save") }}
        </Button>
      </template>
    </PageHeader>

    <PageState
      :loading="isLoading"
      :error="error"
    >
      <div class="flex flex-col gap-4">
        <Card>
          <CardHeader>
            <CardTitle>{{ t("generalSettings.signupSection") }}</CardTitle>
            <CardDescription>{{
              t("generalSettings.signupSectionDescription")
            }}</CardDescription>
          </CardHeader>
          <CardContent class="space-y-5">
            <div class="flex items-start gap-3">
              <Checkbox
                id="disallow-signup"
                :checked="disallowSignup"
                :disabled="!canUpdate"
                @update:checked="disallowSignup = $event === true"
              />
              <div class="grid gap-1">
                <Label for="disallow-signup">{{
                  t("generalSettings.disallowSignup")
                }}</Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("generalSettings.disallowSignupHint") }}
                </p>
              </div>
            </div>

            <div class="flex items-start gap-3">
              <Checkbox
                id="disallow-password-signin"
                :checked="disallowPasswordSignin"
                :disabled="!canUpdate"
                @update:checked="disallowPasswordSignin = $event === true"
              />
              <div class="grid gap-1">
                <Label for="disallow-password-signin">{{
                  t("generalSettings.disallowPasswordSignin")
                }}</Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("generalSettings.disallowPasswordSigninHint") }}
                </p>
              </div>
            </div>

            <div class="flex items-start gap-3">
              <Checkbox
                id="enforce-identity-domain"
                :checked="enforceIdentityDomain"
                :disabled="!canUpdate"
                @update:checked="enforceIdentityDomain = $event === true"
              />
              <div class="grid gap-1">
                <Label for="enforce-identity-domain">{{
                  t("generalSettings.enforceIdentityDomain")
                }}</Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("generalSettings.enforceIdentityDomainHint") }}
                </p>
              </div>
            </div>

            <FormField
              v-model="domainsInput"
              :label="t('generalSettings.domains')"
              :placeholder="t('generalSettings.domainsPlaceholder')"
              :hint="t('generalSettings.domainsHint')"
              :disabled="!canUpdate"
            />

            <FormField
              v-model="ssoEmailIdentityIdpsInput"
              :label="t('generalSettings.ssoEmailIdentityIdps')"
              :placeholder="t('generalSettings.ssoEmailIdentityIdpsPlaceholder')"
              :hint="t('generalSettings.ssoEmailIdentityIdpsHint')"
              :disabled="!canUpdate"
            />
            <p
              v-if="parseIdpNames(ssoEmailIdentityIdpsInput).length > 0"
              class="text-sm text-destructive"
            >
              {{ t("generalSettings.ssoEmailIdentityIdpsWarning") }}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("generalSettings.workspaceSection") }}</CardTitle>
            <CardDescription>{{
              t("generalSettings.workspaceSectionDescription")
            }}</CardDescription>
          </CardHeader>
          <CardContent class="space-y-5">
            <FormField
              v-model="externalUrl"
              :label="t('generalSettings.externalUrl')"
              :placeholder="t('generalSettings.externalUrlPlaceholder')"
              :hint="t('generalSettings.externalUrlHint')"
              :disabled="!canUpdate"
            />
            <div class="flex items-start gap-3">
              <Checkbox
                id="mcp-enabled"
                :checked="mcpEnabled"
                :disabled="!canUpdate"
                @update:checked="mcpEnabled = $event === true"
              />
              <div class="grid gap-1">
                <Label for="mcp-enabled">{{
                  t("generalSettings.mcpEnabled")
                }}</Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("generalSettings.mcpEnabledHint") }}
                </p>
              </div>
            </div>
            <FormField
              v-model="retentionDaysInput"
              type="number"
              :label="t('generalSettings.retentionDays')"
              :hint="t('generalSettings.retentionDaysHint')"
              :error="retentionError"
              :disabled="!canUpdate"
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("generalSettings.explainProvidersSection") }}</CardTitle>
            <CardDescription>{{
              t("generalSettings.explainProvidersSectionDescription")
            }}</CardDescription>
          </CardHeader>
          <CardContent class="space-y-4">
            <p class="text-sm text-muted-foreground">
              {{ t("generalSettings.explainProvidersHint") }}
            </p>
            <p
              v-if="profilesError"
              class="text-sm text-destructive"
            >
              {{ profilesError }}
            </p>
            <p
              v-else-if="modelGroups.length === 0 && unavailableModels.length === 0"
              class="text-sm text-muted-foreground"
            >
              {{ t("generalSettings.explainProvidersEmpty") }}
            </p>
            <div
              v-for="group in modelGroups"
              :key="group.profile"
              class="space-y-2"
            >
              <p class="text-sm font-medium">{{ group.title }}</p>
              <div
                v-for="model in group.models"
                :key="model"
                class="flex items-center gap-3"
              >
                <Checkbox
                  :id="`allowed-model-${group.profile}-${model}`"
                  :checked="isModelChecked(group.profile, model)"
                  :disabled="!canUpdate"
                  @update:checked="
                    toggleAllowedModel(group.profile, model, $event === true)
                  "
                />
                <Label :for="`allowed-model-${group.profile}-${model}`">
                  {{ model }}
                </Label>
              </div>
            </div>
            <!-- A stored pair the registry no longer enables still restricts
            Explain SQL while the allowlist is non-empty, so it is rendered here
            and can be removed instead of becoming an invisible lock. -->
            <div v-if="unavailableModels.length > 0" class="space-y-2">
              <p class="text-sm font-medium">
                {{ t("generalSettings.explainProvidersUnavailable") }}
              </p>
              <div
                v-for="(ref, index) in unavailableModels"
                :key="modelRefKey(ref.providerProfile, ref.model)"
                class="flex items-center gap-3"
              >
                <Checkbox
                  :id="`unavailable-model-${index}`"
                  :checked="true"
                  :disabled="!canUpdate"
                  @update:checked="
                    toggleAllowedModel(
                      ref.providerProfile,
                      ref.model,
                      $event === true
                    )
                  "
                />
                <Label :for="`unavailable-model-${index}`">
                  {{ profileTitle(ref.providerProfile, llmProfiles) }} ·
                  {{ ref.model }}
                  <span class="text-muted-foreground">{{
                    t("generalSettings.explainProvidersUnavailableMarker")
                  }}</span>
                </Label>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("generalSettings.debugSection") }}</CardTitle>
            <CardDescription>{{
              t("generalSettings.debugSectionDescription")
            }}</CardDescription>
          </CardHeader>
          <CardContent class="space-y-3">
            <div class="flex items-start gap-3">
              <Checkbox
                id="debug-mode"
                :checked="debugEnabled"
                :disabled="!canUpdate || isDebugSaving"
                @update:checked="handleDebugToggle"
              />
              <div class="grid gap-1">
                <Label for="debug-mode">{{
                  t("generalSettings.debugMode")
                }}</Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("generalSettings.debugModeHint") }}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>

        <!-- Read-only, and outside the form snapshot: the build metadata is a
             property of the running server, not a setting anyone can save. -->
        <Card>
          <CardHeader>
            <CardTitle>{{ t("generalSettings.aboutSection") }}</CardTitle>
            <CardDescription>{{
              t("generalSettings.aboutSectionDescription")
            }}</CardDescription>
          </CardHeader>
          <CardContent class="space-y-1 text-sm">
            <p
              v-if="buildInfoState === 'loading'"
              class="text-muted-foreground"
            >
              {{ t("common.loading") }}
            </p>
            <template v-else-if="buildInfo">
              <div>
                {{ t("common.version") }}: {{ buildInfo.version }}
              </div>
              <div>
                {{ t("common.commit") }}: {{ buildInfo.git_commit }}
              </div>
              <div>
                {{ t("common.buildTime") }}: {{ buildInfo.build_time }}
              </div>
            </template>
            <!-- A server that does not answer at all (an older build, a proxy
                 that strips the path) says so once, instead of claiming to
                 still be loading forever. -->
            <p
              v-else
              class="text-muted-foreground"
            >
              —
            </p>
          </CardContent>
        </Card>
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { listAll } from "@/api/list";
import { listProfiles } from "@/api/llm";
import {
  getDebugConfig,
  getWorkspaceProfileSetting,
  updateDebugConfig,
  updateWorkspaceProfileSetting,
} from "@/api/setting";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { FormField } from "@/components/ui/form-field";
import { Label } from "@/components/ui/label";
import { useBuildInfo } from "@/composables/useBuildInfo";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { useAuthStore } from "@/store/modules/auth";
import {
  type LlmProviderModelRef,
  LlmProviderModelRefSchema,
  type LlmProviderProfile,
} from "@/types/proto-es/v1/llm_service_pb";
import {
  enabledModelRefs,
  modelRefKey,
  profileTitle,
} from "@/utils/llmProvider";

const { t } = useI18n();
const authStore = useAuthStore();
const { formatError, handleError, showSuccess } = useErrorHandler();
const { buildInfo, buildInfoState } = useBuildInfo();

// The setting is readable by every member (the login page reads it too), but
// only metaxisdata.settings.update may change it.
const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.settings.update")
);

const isLoading = ref(false);
const error = ref<string | null>(null);
const isSaving = ref(false);

const disallowSignup = ref(false);
const disallowPasswordSignin = ref(false);
const enforceIdentityDomain = ref(false);
const domainsInput = ref("");
// The identity providers whose email claim may stand in for a stable subject.
// Empty by default: an address is not an identity unless an administrator says so
// for a named provider.
const ssoEmailIdentityIdpsInput = ref("");

// Explain SQL may only use the (profile, model) pairs selected here; the data
// quality analyses and the quality agents choose their pair in their own
// settings. An empty list means "every enabled model is allowed".
const llmProfiles = ref<LlmProviderProfile[]>([]);
const allowedModels = ref<LlmProviderModelRef[]>([]);
// Listing profiles needs its own permission, so a failure here degrades to a
// message inside the card instead of taking the whole page down.
const profilesError = ref<string | null>(null);

const externalUrl = ref("");
const retentionDaysInput = ref("0");
// The MCP endpoint derives its issuer and resource identifiers from
// external_url, so the server refuses to turn this on without one.
const mcpEnabled = ref(false);

// Empty is treated as 0 (keep forever); anything not a whole non-negative
// number is rejected before the request is sent.
const retentionError = computed(() => {
  const days = Number(retentionDaysInput.value);
  if (
    retentionDaysInput.value.trim() === "" ||
    !Number.isInteger(days) ||
    days < 0
  ) {
    return t("generalSettings.retentionInvalid");
  }
  return "";
});

// Runtime debug mode is applied immediately server-side; a failed call rolls
// the checkbox back so the UI never claims a state the server did not accept.
const isDebugSaving = ref(false);
const debugEnabled = ref(false);

// Every editable field is saved in one request, so the footer needs to know
// whether anything actually changed since the last load or save.
const savedSnapshot = ref("");

function snapshot(): string {
  return JSON.stringify({
    disallowSignup: disallowSignup.value,
    disallowPasswordSignin: disallowPasswordSignin.value,
    enforceIdentityDomain: enforceIdentityDomain.value,
    domains: parseDomains(domainsInput.value),
    ssoEmailIdentityIdps: parseIdpNames(ssoEmailIdentityIdpsInput.value),
    externalUrl: externalUrl.value.trim(),
    retentionDays: Number(retentionDaysInput.value),
    mcpEnabled: mcpEnabled.value,
    // Sorted, stable pair keys: the stored order carries no meaning, and a stale
    // pair the page cannot render must not read as a change.
    allowedModels: allowedModels.value
      .map((ref) => modelRefKey(ref.providerProfile, ref.model))
      .sort(),
  });
}

const isDirty = computed(() => snapshot() !== savedSnapshot.value);

async function fetchSetting() {
  const setting = await getWorkspaceProfileSetting();
  disallowSignup.value = setting.disallowSignup;
  disallowPasswordSignin.value = setting.disallowPasswordSignin;
  enforceIdentityDomain.value = setting.enforceIdentityDomain;
  domainsInput.value = setting.domains.join(", ");
  ssoEmailIdentityIdpsInput.value = setting.ssoEmailIdentityIdps.join(", ");
  // Kept as returned: a pair whose profile or model is gone stays in the list and
  // travels back on save, so this page never silently drops an authorization.
  allowedModels.value = [...setting.allowedLlmProviderModels];
  externalUrl.value = setting.externalUrl;
  retentionDaysInput.value = String(setting.openlineageRetentionDays);
  mcpEnabled.value = setting.mcpEnabled;
}

/** One profile's enabled models, as the Explain SQL card renders them. */
interface ModelGroup {
  profile: string;
  title: string;
  models: string[];
}

// A group only exists when its profile shows at least one enabled model.
const modelGroups = computed<ModelGroup[]>(() => {
  const groups: ModelGroup[] = [];
  for (const profile of llmProfiles.value) {
    const models = profile.models
      .filter((model) => model.enabled)
      .map((model) => model.name);
    if (models.length > 0) {
      groups.push({
        profile: profile.name,
        title: profile.title || profile.name,
        models,
      });
    }
  }
  return groups;
});

// The pairs the registry currently enables, as comparison keys.
const enabledPairKeys = computed(
  () =>
    new Set(
      enabledModelRefs(llmProfiles.value).map((ref) =>
        modelRefKey(ref.providerProfile, ref.model)
      )
    )
);

// A stored pair the list no longer returns is kept in `allowedModels` and saved
// back, so it must remain visible and removable: while the allowlist is
// non-empty, unlisted models stay denied.
const unavailableModels = computed(() =>
  allowedModels.value.filter(
    (ref) =>
      !enabledPairKeys.value.has(modelRefKey(ref.providerProfile, ref.model))
  )
);

// Walk every page: a first-page-only read would silently hide models on a
// workspace with more profiles than one page holds.
async function fetchProfiles() {
  llmProfiles.value = await listAll(async (pageToken) => {
    const response = await listProfiles({ pageSize: 100, pageToken });
    return { items: response.profiles, nextPageToken: response.nextPageToken };
  });
}

async function fetchDebugConfig() {
  const config = await getDebugConfig();
  debugEnabled.value = config.enabled;
}

// Identity providers are named idps/{idp}, the form the server reports. A bare id
// is accepted and normalized, because the operator reads it out of the database.
function parseIdpNames(input: string): string[] {
  return input
    .split(/[,\n]/)
    .map((idp) => idp.trim().replace(/^idps\//, ""))
    .filter((idp) => idp !== "")
    .map((idp) => `idps/${idp}`);
}

function parseDomains(input: string): string[] {
  return input
    .split(/[,\n]/)
    .map((domain) => domain.trim().toLowerCase())
    .filter((domain) => domain !== "");
}

function isModelChecked(profile: string, model: string): boolean {
  const key = modelRefKey(profile, model);
  return allowedModels.value.some(
    (ref) => modelRefKey(ref.providerProfile, ref.model) === key
  );
}

function toggleAllowedModel(profile: string, model: string, checked: boolean) {
  const key = modelRefKey(profile, model);
  if (checked) {
    if (isModelChecked(profile, model)) {
      return;
    }
    allowedModels.value = [
      ...allowedModels.value,
      create(LlmProviderModelRefSchema, {
        providerProfile: profile,
        model,
      }),
    ];
    return;
  }
  allowedModels.value = allowedModels.value.filter(
    (ref) => modelRefKey(ref.providerProfile, ref.model) !== key
  );
}

async function load() {
  isLoading.value = true;
  error.value = null;
  try {
    await Promise.all([fetchSetting(), fetchDebugConfig()]);
    savedSnapshot.value = snapshot();
  } catch (e) {
    error.value = formatError(e, "generalSettings.loadError");
  } finally {
    isLoading.value = false;
  }

  try {
    await fetchProfiles();
  } catch (e) {
    profilesError.value = formatError(e, "generalSettings.loadError");
  }
}

async function handleDebugToggle(checked: boolean | "indeterminate") {
  const next = checked === true;
  const previous = debugEnabled.value;
  debugEnabled.value = next;
  isDebugSaving.value = true;
  try {
    await updateDebugConfig(next);
    showSuccess(t("generalSettings.saveSuccess"));
  } catch (e) {
    debugEnabled.value = previous;
    handleError(e, "generalSettings.saveError");
  } finally {
    isDebugSaving.value = false;
  }
}

async function handleSave() {
  // The form only holds the server's values once `load` succeeded; saving
  // defaults over a failed or unfinished load would overwrite the workspace
  // settings with empty ones.
  if (isLoading.value || error.value || retentionError.value) {
    return;
  }
  isSaving.value = true;
  try {
    await updateWorkspaceProfileSetting(
      {
        disallowSignup: disallowSignup.value,
        disallowPasswordSignin: disallowPasswordSignin.value,
        enforceIdentityDomain: enforceIdentityDomain.value,
        domains: parseDomains(domainsInput.value),
        ssoEmailIdentityIdps: parseIdpNames(ssoEmailIdentityIdpsInput.value),
        externalUrl: externalUrl.value.trim(),
        openlineageRetentionDays: Number(retentionDaysInput.value),
        allowedLlmProviderModels: allowedModels.value,
        mcpEnabled: mcpEnabled.value,
      },
      [
        "disallow_signup",
        "disallow_password_signin",
        "enforce_identity_domain",
        "domains",
        "sso_email_identity_idps",
        "external_url",
        "openlineage_retention_days",
        "allowed_llm_provider_models",
        "mcp_enabled",
      ]
    );
    savedSnapshot.value = snapshot();
    showSuccess(t("generalSettings.saveSuccess"));
  } catch (e) {
    handleError(e, "generalSettings.saveError");
  } finally {
    isSaving.value = false;
  }
}

onMounted(load);
</script>
