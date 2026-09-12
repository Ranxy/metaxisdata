<template>
  <div class="space-y-4">
    <div>
      <h1 class="text-2xl font-bold tracking-tight">
        {{ t("generalSettings.title") }}
      </h1>
      <p class="text-muted-foreground mt-1">
        {{ t("generalSettings.description") }}
      </p>
    </div>

    <Card>
      <CardHeader>
        <CardTitle>{{ t("generalSettings.signupSection") }}</CardTitle>
        <CardDescription>{{
          t("generalSettings.signupSectionDescription")
        }}</CardDescription>
      </CardHeader>
      <CardContent>
        <div
          v-if="isLoading"
          class="p-8 flex justify-center"
        >
          <AppLoading />
        </div>
        <div
          v-else
          class="space-y-5"
        >
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

          <div class="pt-2 space-y-2">
            <Button
              :disabled="isSaving || !canUpdate"
              @click="handleSave"
            >
              {{ t("common.save") }}
            </Button>
            <p
              v-if="!canUpdate"
              class="text-sm text-muted-foreground"
            >
              {{ t("generalSettings.readOnlyHint") }}
            </p>
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
      <CardContent>
        <div
          v-if="isDebugLoading"
          class="p-8 flex justify-center"
        >
          <AppLoading />
        </div>
        <div
          v-else
          class="space-y-3"
        >
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
          <p
            v-if="!canUpdate"
            class="text-sm text-muted-foreground"
          >
            {{ t("generalSettings.readOnlyHint") }}
          </p>
        </div>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  getDebugConfig,
  getWorkspaceProfileSetting,
  updateDebugConfig,
  updateWorkspaceProfileSetting,
} from "@/api/setting";
import AppLoading from "@/components/common/AppLoading.vue";
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

const { t } = useI18n();
const authStore = useAuthStore();
const { handleError, showSuccess } = useErrorHandler();

// The setting is readable by every member (the login page reads it too), but
// only metaxisdata.settings.update may change it.
const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.settings.update")
);

const isLoading = ref(false);
const isSaving = ref(false);
const disallowSignup = ref(false);
const disallowPasswordSignin = ref(false);

// Runtime debug mode is applied immediately server-side; a failed call rolls
// the checkbox back so the UI never claims a state the server did not accept.
const isDebugLoading = ref(false);
const isDebugSaving = ref(false);
const debugEnabled = ref(false);

async function fetchSetting() {
  isLoading.value = true;
  try {
    const setting = await getWorkspaceProfileSetting();
    disallowSignup.value = setting.disallowSignup;
    disallowPasswordSignin.value = setting.disallowPasswordSignin;
  } catch (e) {
    handleError(e, t("generalSettings.loadError"));
  } finally {
    isLoading.value = false;
  }
}

async function fetchDebugConfig() {
  isDebugLoading.value = true;
  try {
    const config = await getDebugConfig();
    debugEnabled.value = config.enabled;
  } catch (e) {
    handleError(e, t("generalSettings.loadError"));
  } finally {
    isDebugLoading.value = false;
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
  isSaving.value = true;
  try {
    await updateWorkspaceProfileSetting(
      {
        disallowSignup: disallowSignup.value,
        disallowPasswordSignin: disallowPasswordSignin.value,
      },
      ["disallow_signup", "disallow_password_signin"]
    );
    showSuccess(t("generalSettings.saveSuccess"));
  } catch (e) {
    handleError(e, t("generalSettings.saveError"));
  } finally {
    isSaving.value = false;
  }
}

onMounted(() => {
  fetchSetting();
  fetchDebugConfig();
});
</script>
