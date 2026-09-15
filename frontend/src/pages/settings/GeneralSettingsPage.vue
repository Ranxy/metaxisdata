<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('generalSettings.title')"
      :description="t('generalSettings.description')"
    />

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

            <AppInput
              v-model="domainsInput"
              :label="t('generalSettings.domains')"
              :placeholder="t('generalSettings.domainsPlaceholder')"
              :hint="t('generalSettings.domainsHint')"
              :disabled="!canUpdate"
            />
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
            <AppInput
              v-model="externalUrl"
              :label="t('generalSettings.externalUrl')"
              :placeholder="t('generalSettings.externalUrlPlaceholder')"
              :hint="t('generalSettings.externalUrlHint')"
              :disabled="!canUpdate"
            />
            <AppInput
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
          <CardContent class="space-y-3">
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
              v-else-if="llmProfiles.length === 0"
              class="text-sm text-muted-foreground"
            >
              {{ t("generalSettings.explainProvidersEmpty") }}
            </p>
            <div
              v-for="profile in llmProfiles"
              :key="profile.name"
              class="flex items-center gap-3"
            >
              <Checkbox
                :id="`allowed-provider-${profile.name}`"
                :checked="allowedProfiles.includes(profile.name)"
                :disabled="!canUpdate"
                @update:checked="
                  toggleAllowedProfile(profile.name, $event === true)
                "
              />
              <Label :for="`allowed-provider-${profile.name}`">
                {{ profile.title || profile.name }}
              </Label>
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

        <!-- One save for the whole form. Debug mode is deliberately excluded:
             it applies immediately through its own RPC. -->
        <div
          class="sticky bottom-0 z-10 flex flex-wrap items-center gap-3 border-t bg-background/95 py-3 backdrop-blur"
        >
          <Button
            :disabled="isSaving || !canUpdate || !!retentionError"
            @click="handleSave"
          >
            {{ t("common.save") }}
          </Button>
          <span
            v-if="!canUpdate"
            class="text-sm text-muted-foreground"
          >
            {{ t("generalSettings.readOnlyHint") }}
          </span>
          <span
            v-else-if="isDirty"
            class="text-sm text-muted-foreground"
          >
            {{ t("generalSettings.unsavedHint") }}
          </span>
        </div>
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { listProfiles } from "@/api/llm";
import {
  getDebugConfig,
  getWorkspaceProfileSetting,
  updateDebugConfig,
  updateWorkspaceProfileSetting,
} from "@/api/setting";
import AppInput from "@/components/common/AppInput.vue";
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
import { Label } from "@/components/ui/label";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { useAuthStore } from "@/store/modules/auth";
import { extractErrorMessage } from "@/utils/error";

const { t } = useI18n();
const authStore = useAuthStore();
const { handleError, showSuccess } = useErrorHandler();

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

// ExplainSQL may only use the provider profiles selected here. An empty list
// means "every enabled profile is allowed".
const llmProfiles = ref<{ name: string; title: string }[]>([]);
const allowedProfiles = ref<string[]>([]);
// Listing profiles needs its own permission, so a failure here degrades to a
// message inside the card instead of taking the whole page down.
const profilesError = ref<string | null>(null);

const externalUrl = ref("");
const retentionDaysInput = ref("0");

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
    externalUrl: externalUrl.value.trim(),
    retentionDays: Number(retentionDaysInput.value),
    allowedProfiles: [...allowedProfiles.value].sort(),
  });
}

const isDirty = computed(() => snapshot() !== savedSnapshot.value);

async function fetchSetting() {
  const setting = await getWorkspaceProfileSetting();
  disallowSignup.value = setting.disallowSignup;
  disallowPasswordSignin.value = setting.disallowPasswordSignin;
  enforceIdentityDomain.value = setting.enforceIdentityDomain;
  domainsInput.value = setting.domains.join(", ");
  allowedProfiles.value = [...setting.allowedLlmProviderProfiles];
  externalUrl.value = setting.externalUrl;
  retentionDaysInput.value = String(setting.openlineageRetentionDays);
}

// Only profiles with at least one enabled model can be used by ExplainSQL, so
// the picker lists exactly those.
async function fetchProfiles() {
  const resp = await listProfiles({ pageSize: 100 });
  llmProfiles.value = resp.profiles
    .filter((profile) => profile.models.some((model) => model.enabled))
    .map((profile) => ({
      name: profile.name,
      title: profile.title || profile.name,
    }));
}

async function fetchDebugConfig() {
  const config = await getDebugConfig();
  debugEnabled.value = config.enabled;
}

function parseDomains(input: string): string[] {
  return input
    .split(/[,\n]/)
    .map((domain) => domain.trim().toLowerCase())
    .filter((domain) => domain !== "");
}

function toggleAllowedProfile(name: string, checked: boolean) {
  if (checked) {
    if (!allowedProfiles.value.includes(name)) {
      allowedProfiles.value = [...allowedProfiles.value, name];
    }
    return;
  }
  allowedProfiles.value = allowedProfiles.value.filter((p) => p !== name);
}

async function load() {
  isLoading.value = true;
  error.value = null;
  try {
    await Promise.all([fetchSetting(), fetchDebugConfig()]);
    savedSnapshot.value = snapshot();
  } catch (e) {
    error.value = extractErrorMessage(e) || t("generalSettings.loadError");
  } finally {
    isLoading.value = false;
  }

  try {
    await fetchProfiles();
  } catch (e) {
    profilesError.value =
      extractErrorMessage(e) || t("generalSettings.loadError");
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
    handleError(e, t("generalSettings.saveError"));
  } finally {
    isDebugSaving.value = false;
  }
}

async function handleSave() {
  if (retentionError.value) {
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
        externalUrl: externalUrl.value.trim(),
        openlineageRetentionDays: Number(retentionDaysInput.value),
        allowedLlmProviderProfiles: allowedProfiles.value,
      },
      [
        "disallow_signup",
        "disallow_password_signin",
        "enforce_identity_domain",
        "domains",
        "external_url",
        "openlineage_retention_days",
        "allowed_llm_provider_profiles",
      ]
    );
    savedSnapshot.value = snapshot();
    showSuccess(t("generalSettings.saveSuccess"));
  } catch (e) {
    handleError(e, t("generalSettings.saveError"));
  } finally {
    isSaving.value = false;
  }
}

onMounted(load);
</script>
