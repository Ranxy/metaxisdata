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

          <div class="pt-2">
            <Button
              :disabled="isSaving"
              @click="handleSave"
            >
              {{ t("common.save") }}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  getWorkspaceProfileSetting,
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

const { t } = useI18n();
const { handleError, showSuccess } = useErrorHandler();

const isLoading = ref(false);
const isSaving = ref(false);
const disallowSignup = ref(false);
const disallowPasswordSignin = ref(false);

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
});
</script>
