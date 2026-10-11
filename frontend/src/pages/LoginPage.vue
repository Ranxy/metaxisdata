<template>
  <div class="min-h-screen flex items-center justify-center py-12 px-4 sm:px-6 lg:px-8">
    <div class="w-full max-w-md">
      <!-- Logo Section -->
      <div class="text-center mb-8">
        <div class="flex justify-center mb-4">
          <div class="w-16 h-16 bg-primary rounded-2xl flex items-center justify-center">
            <Database class="w-10 h-10 text-primary-foreground" />
          </div>
        </div>
        <h1 class="text-3xl font-bold">
          MetaxisData
        </h1>
        <p class="text-muted-foreground mt-2">
          {{ t("login.subtitle") }}
        </p>
      </div>

      <!-- Login/Register Card -->
      <Card class="p-2">
        <CardContent class="pt-6">
          <!-- Forced Password Reset -->
          <template v-if="requireResetPassword">
            <div class="space-y-2 mb-6">
              <CardTitle>{{ t("resetPassword.title") }}</CardTitle>
              <CardDescription>{{ t("resetPassword.description") }}</CardDescription>
            </div>

            <!-- Error Alert -->
            <Alert
              v-if="errorMessage"
              variant="destructive"
              class="mb-4"
            >
              <AlertCircle class="h-4 w-4" />
              <AlertDescription>{{ errorMessage }}</AlertDescription>
            </Alert>

            <form
              class="space-y-4"
              @submit.prevent="handleResetPassword"
            >
              <FormField
                v-model="resetForm.currentPassword"
                type="password"
                :label="t('resetPassword.currentPassword')"
                :placeholder="t('resetPassword.currentPasswordPlaceholder')"
                required
              />

              <FormField
                v-model="resetForm.newPassword"
                type="password"
                :label="t('resetPassword.newPassword')"
                :placeholder="t('resetPassword.newPasswordPlaceholder')"
                required
              />

              <FormField
                v-model="resetForm.confirmPassword"
                type="password"
                :label="t('resetPassword.confirmPassword')"
                :placeholder="t('resetPassword.confirmPasswordPlaceholder')"
                required
              />

              <Button
                type="submit"
                :disabled="isResetting"
                class="w-full"
                size="lg"
              >
                <Loader2
                  v-if="isResetting"
                  class="mr-2 h-4 w-4 animate-spin"
                />
                {{ isResetting ? t("resetPassword.submitting") : t("resetPassword.submit") }}
              </Button>
            </form>

            <div class="mt-6 text-center text-sm">
              <Button
                variant="link"
                class="px-1"
                @click="handleResetSignOut"
              >
                {{ t("resetPassword.signOut") }}
              </Button>
            </div>
          </template>

          <!-- Login Mode -->
          <template v-else-if="!isRegisterMode">
            <div class="space-y-2 mb-6">
              <CardTitle>{{ t("login.welcome") }}</CardTitle>
              <CardDescription>{{ t("login.description") }}</CardDescription>
            </div>

            <!-- Error Alert -->
            <Alert
              v-if="errorMessage"
              variant="destructive"
              class="mb-4"
            >
              <AlertCircle class="h-4 w-4" />
              <AlertDescription>{{ errorMessage }}</AlertDescription>
            </Alert>

            <form
              class="space-y-4"
              @submit.prevent="handleLogin"
            >
              <!-- Email Input -->
              <FormField
                v-model="loginForm.email"
                type="text"
                :label="t('login.email')"
                :placeholder="t('login.emailPlaceholder')"
                required
              />

              <!-- Password Input -->
              <FormField
                v-model="loginForm.password"
                type="password"
                :label="t('login.password')"
                :placeholder="t('login.passwordPlaceholder')"
                required
              />

              <!-- Login Button -->
              <Button
                type="submit"
                :disabled="isLoading"
                class="w-full"
                size="lg"
              >
                <Loader2
                  v-if="isLoading"
                  class="mr-2 h-4 w-4 animate-spin"
                />
                {{ isLoading ? t("login.loggingIn") : t("login.submit") }}
              </Button>
            </form>

            <!-- SSO Login -->
            <template v-if="oauthProviders.length > 0">
              <div class="relative my-6">
                <div class="absolute inset-0 flex items-center">
                  <Separator class="w-full" />
                </div>
                <div class="relative flex justify-center text-xs uppercase">
                  <span class="bg-card px-2 text-muted-foreground">{{ t("login.or") }}</span>
                </div>
              </div>

              <div class="space-y-2">
                <Button
                  v-for="provider in oauthProviders"
                  :key="provider.name"
                  variant="outline"
                  class="w-full"
                  :disabled="isStartingSSO"
                  @click="handleSSOLogin(provider)"
                >
                  <Loader2
                    v-if="isStartingSSO"
                    class="mr-2 h-4 w-4 animate-spin"
                  />
                  <Lock
                    v-else
                    class="mr-2 h-4 w-4"
                  />
                  {{ t("login.ssoWith", { provider: provider.title }) }}
                </Button>
              </div>
            </template>

            <!-- Switch to Register -->
            <div
              v-if="allowSignup"
              class="mt-6 text-center text-sm"
            >
              <span class="text-muted-foreground">{{ t("login.noAccount") }}</span>
              <Button
                variant="link"
                class="px-1"
                @click="switchToRegister"
              >
                {{ t("login.signUp") }}
              </Button>
            </div>
          </template>

          <!-- Register Mode -->
          <template v-else>
            <div class="space-y-2 mb-6">
              <CardTitle>{{ t("register.welcome") }}</CardTitle>
              <CardDescription>{{ t("register.description") }}</CardDescription>
            </div>

            <!-- Error Alert -->
            <Alert
              v-if="errorMessage"
              variant="destructive"
              class="mb-4"
            >
              <AlertCircle class="h-4 w-4" />
              <AlertDescription>{{ errorMessage }}</AlertDescription>
            </Alert>

            <!-- Success Alert -->
            <Alert
              v-if="successMessage"
              variant="success"
              class="mb-4"
            >
              <CheckCircle2 class="h-4 w-4" />
              <AlertDescription>{{ successMessage }}</AlertDescription>
            </Alert>

            <form
              class="space-y-4"
              @submit.prevent="handleRegister"
            >
              <!-- Email Input -->
              <FormField
                v-model="registerForm.email"
                type="email"
                :label="t('login.email')"
                :placeholder="t('login.emailPlaceholder')"
                required
              />

              <!-- Title (Username) Input -->
              <FormField
                v-model="registerForm.title"
                type="text"
                :label="t('register.title')"
                :placeholder="t('register.titlePlaceholder')"
              />

              <!-- Password Input -->
              <FormField
                v-model="registerForm.password"
                type="password"
                :label="t('login.password')"
                :placeholder="t('login.passwordPlaceholder')"
                required
              />

              <!-- Confirm Password Input -->
              <FormField
                v-model="registerForm.confirmPassword"
                type="password"
                :label="t('register.confirmPassword')"
                :placeholder="t('register.confirmPasswordPlaceholder')"
                required
              />

              <!-- Register Button -->
              <Button
                type="submit"
                :disabled="isRegistering"
                class="w-full"
                size="lg"
              >
                <Loader2
                  v-if="isRegistering"
                  class="mr-2 h-4 w-4 animate-spin"
                />
                {{ isRegistering ? t("register.registering") : t("register.submit") }}
              </Button>
            </form>

            <!-- Switch to Login -->
            <div class="mt-6 text-center text-sm">
              <span class="text-muted-foreground">{{ t("register.hasAccount") }}</span>
              <Button
                variant="link"
                class="px-1"
                @click="switchToLogin"
              >
                {{ t("register.signIn") }}
              </Button>
            </div>
          </template>
        </CardContent>
      </Card>

      <!-- Language Switcher at bottom -->
      <div class="mt-6 text-center">
        <Button
          v-for="item in locales"
          :key="item.value"
          variant="ghost"
          size="sm"
          :class="currentLocale === item.value ? 'text-primary font-medium' : 'text-muted-foreground'"
          @click="appStore.setLocale(item.value)"
        >
          {{ item.label }}
        </Button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import {
  AlertCircle,
  CheckCircle2,
  Database,
  Loader2,
  Lock,
} from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listIdentityProviders } from "@/api/identity-provider";
import { getWorkspaceProfileSetting } from "@/api/setting";
import * as userApi from "@/api/user";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardTitle,
} from "@/components/ui/card";
import { FormField } from "@/components/ui/form-field";
import { Separator } from "@/components/ui/separator";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { useSSOLogin } from "@/composables/useSSOLogin";
import { notify } from "@/lib/notify";
import { resolveSignInTarget } from "@/lib/signInRedirect";
import type { AppLocale } from "@/locales";
import { useAppStore } from "@/store/modules/app";
import { useAuthStore } from "@/store/modules/auth";
import {
  type IdentityProvider,
  IdentityProviderType,
} from "@/types/proto-es/v1/identity_provider_service_pb";

const { t } = useI18n();
const router = useRouter();
const route = useRoute();
const authStore = useAuthStore();
const appStore = useAppStore();
const { formatError } = useErrorHandler();

// Mode toggle
const isRegisterMode = ref(false);
// Signup can be disabled workspace-wide; hide the register entry then.
const allowSignup = ref(true);
// The configured identity providers, listed anonymously so the buttons can be
// rendered before there is a session. A failed load leaves the list empty and the
// password form untouched: SSO being unavailable must not block signing in.
const identityProviders = ref<IdentityProvider[]>([]);
const { isStarting: isStartingSSO, start: startSSOLogin } = useSSOLogin();

const oauthProviders = computed(() =>
  identityProviders.value.filter(
    (provider) => provider.type === IdentityProviderType.OAUTH2
  )
);

// A direct entry point, `?sso=idps/acme` (a bare id is accepted too). It is what
// a workspace hands out when password sign-in is turned off, and it is matched
// against the full resource name rather than by suffix: a suffix match would let
// `?sso=acme` start the flow for `idps/other-acme`.
function findSSOProvider(target: string): IdentityProvider | undefined {
  if (!target) {
    return undefined;
  }
  const wanted = target.startsWith("idps/") ? target : `idps/${target}`;
  return identityProviders.value.find(
    (provider) =>
      provider.name === wanted && provider.type === IdentityProviderType.OAUTH2
  );
}

async function handleSSOLogin(provider: IdentityProvider) {
  errorMessage.value = "";
  try {
    const started = await startSSOLogin(provider, {
      redirect: route.query.redirect as string | undefined,
    });
    if (!started) {
      errorMessage.value = t("login.ssoUnavailable");
    }
  } catch (error) {
    errorMessage.value = formatError(error);
  }
}

// Login form
const loginForm = ref({
  email: "",
  password: "",
});

// Register form
const registerForm = ref({
  email: "",
  title: "",
  password: "",
  confirmPassword: "",
});

const errorMessage = ref("");
const successMessage = ref("");
const isLoading = computed(() => authStore.isLoading);
const isRegistering = ref(false);
const isResetting = ref(false);
const requireResetPassword = computed(() => authStore.requireResetPassword);

// Reset form
const resetForm = ref({
  currentPassword: "",
  newPassword: "",
  confirmPassword: "",
});

const locales: Array<{ value: AppLocale; label: string }> = [
  { value: "zh-CN", label: "简体中文" },
  { value: "en-US", label: "English" },
];

const currentLocale = computed(() => appStore.locale);

function switchToRegister() {
  isRegisterMode.value = true;
  errorMessage.value = "";
  successMessage.value = "";
}

function switchToLogin() {
  isRegisterMode.value = false;
  errorMessage.value = "";
  successMessage.value = "";
}

async function handleLogin() {
  if (!loginForm.value.email || !loginForm.value.password) {
    errorMessage.value = t("login.loginFailed");
    return;
  }

  errorMessage.value = "";

  try {
    const response = await authStore.login(
      loginForm.value.email,
      loginForm.value.password
    );

    if (response.accountAdopted) {
      // This sign-in took over the account from whoever held it: the password it
      // had no longer works, and the only way in from now on is this identity
      // provider. Tell them before they are redirected, or they will conclude
      // their account was stolen the next time the password fails.
      notify.warning(t("login.passwordInvalidated"));
    }

    if (response.requireResetPassword) {
      // The server restricted the issued token to this password change, so stay
      // on this page until it is done instead of heading into the app.
      resetForm.value.currentPassword = loginForm.value.password;
      return;
    }

    // Redirect to the original destination or home. The server sends a signed-out
    // user here with the path it was asked for, and that path may not be an SPA
    // route at all: the MCP client's /oauth/authorize is the one that matters, and
    // the router would answer NotFound instead of resuming the authorization.
    // `matched.length` cannot tell the two apart — the catch-all route matches
    // everything — so the decision is made on whether a real route has that name.
    const target = resolveSignInTarget(
      route.query.redirect as string | undefined,
      (path) => router.resolve(path).name !== "NotFound"
    );
    if (target.kind === "server") {
      window.location.assign(target.path);
      return;
    }
    router.push(target.kind === "spa" ? target.path : { name: "Home" });
  } catch (error) {
    errorMessage.value = formatError(error, "login.loginFailed");
    console.error("Login error:", error);
  }
}

async function handleResetPassword() {
  const { currentPassword, newPassword, confirmPassword } = resetForm.value;
  if (!currentPassword || !newPassword || !confirmPassword) {
    errorMessage.value = t("resetPassword.missingFields");
    return;
  }
  if (newPassword !== confirmPassword) {
    errorMessage.value = t("resetPassword.mismatch");
    return;
  }
  if (newPassword === currentPassword) {
    errorMessage.value = t("resetPassword.sameAsCurrent");
    return;
  }

  errorMessage.value = "";
  isResetting.value = true;
  try {
    await authStore.changePassword(currentPassword, newPassword);
    // The password change invalidates every token minted before it, including
    // the restricted one, so sign in again to get a full-access token.
    await authStore.login(loginForm.value.email, newPassword);
    if (authStore.requireResetPassword) {
      errorMessage.value = t("resetPassword.failed");
      return;
    }

    const redirect = route.query.redirect as string;
    router.push(redirect || { name: "Home" });
  } catch (error) {
    errorMessage.value = formatError(error, "resetPassword.failed");
    console.error("Password reset error:", error);
  } finally {
    isResetting.value = false;
  }
}

async function handleResetSignOut() {
  await authStore.logout();
  resetForm.value = {
    currentPassword: "",
    newPassword: "",
    confirmPassword: "",
  };
  errorMessage.value = "";
}

const REGISTRATION_SUCCESS_DELAY = 1500;

async function handleRegister() {
  if (
    !registerForm.value.email ||
    !registerForm.value.password ||
    !registerForm.value.confirmPassword
  ) {
    errorMessage.value = t("register.missingFields");
    return;
  }

  if (registerForm.value.password !== registerForm.value.confirmPassword) {
    errorMessage.value = t("register.passwordMismatch");
    return;
  }

  errorMessage.value = "";
  successMessage.value = "";
  isRegistering.value = true;

  try {
    await userApi.createUser(
      registerForm.value.email,
      registerForm.value.password,
      registerForm.value.title
    );

    successMessage.value = t("register.registerSuccess");

    // Clear form
    registerForm.value = {
      email: "",
      title: "",
      password: "",
      confirmPassword: "",
    };

    // Switch to login after a short delay
    setTimeout(() => {
      switchToLogin();
    }, REGISTRATION_SUCCESS_DELAY);
  } catch (error) {
    errorMessage.value = formatError(error, "register.registerFailed");
    console.error("Register error:", error);
  } finally {
    isRegistering.value = false;
  }
}

onMounted(async () => {
  if (route.query.expired === "1") {
    errorMessage.value = t("error.sessionExpired");
  }
  try {
    const setting = await getWorkspaceProfileSetting();
    allowSignup.value = !setting.disallowSignup;
    if (!allowSignup.value) {
      isRegisterMode.value = false;
    }
  } catch {
    // Keep signup visible if the setting cannot be loaded; the server still
    // enforces the policy.
  }
  try {
    identityProviders.value = await listIdentityProviders();
  } catch {
    // The password form still works; a workspace with no reachable provider list
    // simply shows no SSO buttons.
  }

  // `?sso=` starts the flow without a click. It runs after the list is known, and
  // an unknown target is reported rather than silently ignored: a link that does
  // nothing is worse than one that says why.
  const target = route.query.sso;
  if (typeof target === "string" && target) {
    // Drop the parameter before leaving. It is a one-shot, and leaving it in the
    // URL means a browser Back from the provider starts the flow again, with no
    // way for the user to reach the form.
    const remaining: Record<string, string> = {};
    for (const [key, value] of Object.entries(route.query)) {
      if (key !== "sso" && typeof value === "string") {
        remaining[key] = value;
      }
    }
    void router.replace({ query: remaining });

    const provider = findSSOProvider(target);
    if (provider) {
      await handleSSOLogin(provider);
    } else {
      errorMessage.value = t("login.ssoUnavailable");
    }
  }
});
</script>
