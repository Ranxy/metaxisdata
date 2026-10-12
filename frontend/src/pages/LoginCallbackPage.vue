<template>
  <div class="min-h-screen flex items-center justify-center py-12 px-4 sm:px-6 lg:px-8">
    <div class="w-full max-w-md">
      <div class="text-center mb-8">
        <div class="flex justify-center mb-4">
          <div class="w-16 h-16 bg-primary rounded-2xl flex items-center justify-center">
            <Database class="w-10 h-10 text-primary-foreground" />
          </div>
        </div>
        <h1 class="text-3xl font-bold">
          MetaxisData
        </h1>
      </div>

      <Card class="p-2">
        <CardContent class="pt-6">
          <!-- An authorization code is being exchanged. -->
          <template v-if="!settled">
            <div class="flex items-center justify-center gap-2 py-6 text-muted-foreground">
              <Loader2 class="h-4 w-4 animate-spin" />
              <span>{{ t("ssoCallback.signingIn") }}</span>
            </div>
          </template>

          <!-- A provider test: the administrator stays signed in. -->
          <template v-else-if="isTest && identity">
            <div class="space-y-2 mb-4">
              <CardTitle>{{ t("ssoCallback.testSucceeded") }}</CardTitle>
              <CardDescription>{{ t("ssoCallback.testDescription") }}</CardDescription>
            </div>
            <dl class="space-y-2 text-sm">
              <div
                v-for="field in identityFields"
                :key="field.label"
                class="flex gap-3"
              >
                <dt class="w-28 shrink-0 text-muted-foreground">
                  {{ field.label }}
                </dt>
                <dd class="min-w-0 break-all">
                  {{ field.value }}
                </dd>
              </div>
            </dl>
            <div class="mt-6">
              <Button
                class="w-full"
                @click="backToSettings"
              >
                {{ t("ssoCallback.backToSettings") }}
              </Button>
            </div>
          </template>

          <!-- Anything that did not produce a session. -->
          <template v-else>
            <div class="space-y-2 mb-4">
              <CardTitle>{{ t("ssoCallback.failed") }}</CardTitle>
              <CardDescription>{{ t("ssoCallback.failedDescription") }}</CardDescription>
            </div>

            <Alert
              variant="destructive"
              class="mb-4"
            >
              <AlertCircle class="h-4 w-4" />
              <AlertDescription>{{ errorMessage }}</AlertDescription>
            </Alert>

            <Button
              class="w-full"
              @click="backToSettings"
            >
              {{ isTest ? t("ssoCallback.backToSettings") : t("ssoCallback.backToLogin") }}
            </Button>
          </template>
        </CardContent>
      </Card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { AlertCircle, Database, Loader2 } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { testIdentityProvider } from "@/api/identity-provider";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardTitle,
} from "@/components/ui/card";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import { consumeSSOState } from "@/lib/oauth";
import { resolveSignInTarget } from "@/lib/signInRedirect";
import { useAuthStore } from "@/store/modules/auth";
import type { IdentityProviderUserInfo } from "@/types/proto-es/v1/identity_provider_service_pb";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const authStore = useAuthStore();
const { formatError } = useErrorHandler();

const settled = ref(false);
const isTest = ref(false);
const identity = ref<IdentityProviderUserInfo | undefined>();
const errorMessage = ref("");

const identityFields = computed(() => {
  const userInfo = identity.value;
  if (!userInfo) {
    return [];
  }
  return [
    {
      label: t("ssoCallback.fieldIdentifier"),
      value: userInfo.identifier,
    },
    {
      label: t("ssoCallback.fieldSubject"),
      value: userInfo.subject,
    },
    {
      label: t("ssoCallback.fieldDisplayName"),
      value: userInfo.displayName,
    },
    {
      label: t("ssoCallback.fieldGroups"),
      value:
        userInfo.groups.length > 0
          ? userInfo.groups.join(", ")
          : t("ssoCallback.noGroups"),
    },
  ];
});

function backToSettings() {
  void router.replace(
    isTest.value ? { name: "IdentityProviderSettings" } : { name: "Login" }
  );
}

async function exchange(code: string, state: string) {
  const record = consumeSSOState(state);
  if (!record) {
    // The state is unknown, already used, or past its TTL. Saying so is the
    // point: without it the user cannot tell an expired tab from a broken
    // provider.
    errorMessage.value = t("ssoCallback.invalidState");
    settled.value = true;
    return;
  }
  isTest.value = record.mode === "test";

  const context = {
    idpName: record.idpName,
    code,
    state,
    codeVerifier: record.codeVerifier,
  };

  if (isTest.value) {
    try {
      identity.value = await testIdentityProvider(
        context.idpName,
        context.code,
        context.codeVerifier
      );
    } catch (error) {
      errorMessage.value = formatError(error);
    }
    settled.value = true;
    return;
  }

  try {
    const response = await authStore.loginWithSSO(context);
    if (response.accountAdopted) {
      // Signing in took the account over from whoever held it: the password it
      // had no longer works. Say so before the page changes, or the next failed
      // password attempt looks like a stolen account.
      notify.warning(t("login.passwordInvalidated"));
    }
    const target = resolveSignInTarget(
      record.redirect,
      // The router's catch-all answers every unknown path, so only a real match
      // counts as an in-app route.
      (path) => router.resolve(path).name !== "NotFound"
    );
    if (target.kind === "server") {
      // A server route has to be loaded from the server. The one that matters is
      // the MCP client's /oauth/authorize, whose round trip through the sign-in
      // page is how a signed-out user resumes an authorization.
      window.location.assign(target.path);
      return;
    }
    await router.replace(target.kind === "spa" ? target.path : "/");
  } catch (error) {
    errorMessage.value = formatError(error);
    settled.value = true;
  }
}

onMounted(async () => {
  // The flow runs once per mount. The code and state are read from the query
  // because that is where the provider puts them.
  const code = route.query.code;
  const state = route.query.state;
  if (
    typeof code !== "string" ||
    !code ||
    typeof state !== "string" ||
    !state
  ) {
    errorMessage.value = t("ssoCallback.invalidState");
    settled.value = true;
    return;
  }
  await exchange(code, state);
});
</script>
