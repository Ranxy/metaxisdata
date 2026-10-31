<template>
  <Card class="rounded-2xl border-border/70 shadow-e3">
    <CardContent class="p-7 sm:p-8">
      <!-- Forced Password Reset -->
      <template v-if="requireResetPassword">
        <div class="mb-6 space-y-1.5">
          <h2 class="text-2xl font-semibold tracking-tight">
            {{ t("resetPassword.title") }}
          </h2>
          <p class="text-sm text-muted-foreground">
            {{ t("resetPassword.description") }}
          </p>
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
            :type="showResetPassword ? 'text' : 'password'"
            :label="t('resetPassword.currentPassword')"
            :placeholder="t('resetPassword.currentPasswordPlaceholder')"
            autocomplete="current-password"
            required
            class="pl-9 pr-10"
          >
            <template #prefix>
              <KeyRound
                class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
              />
            </template>
            <template #suffix>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                tabindex="-1"
                class="absolute right-1 top-1/2 h-8 w-8 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                :aria-label="
                  showResetPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                :title="
                  showResetPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                @click="showResetPassword = !showResetPassword"
              >
                <EyeOff v-if="showResetPassword" class="h-4 w-4" />
                <Eye v-else class="h-4 w-4" />
              </Button>
            </template>
          </FormField>

          <FormField
            v-model="resetForm.newPassword"
            :type="showResetPassword ? 'text' : 'password'"
            :label="t('resetPassword.newPassword')"
            :placeholder="t('resetPassword.newPasswordPlaceholder')"
            autocomplete="new-password"
            required
            class="pl-9 pr-10"
          >
            <template #prefix>
              <Lock
                class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
              />
            </template>
            <template #suffix>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                tabindex="-1"
                class="absolute right-1 top-1/2 h-8 w-8 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                :aria-label="
                  showResetPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                :title="
                  showResetPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                @click="showResetPassword = !showResetPassword"
              >
                <EyeOff v-if="showResetPassword" class="h-4 w-4" />
                <Eye v-else class="h-4 w-4" />
              </Button>
            </template>
          </FormField>

          <FormField
            v-model="resetForm.confirmPassword"
            :type="showResetPassword ? 'text' : 'password'"
            :label="t('resetPassword.confirmPassword')"
            :placeholder="t('resetPassword.confirmPasswordPlaceholder')"
            autocomplete="new-password"
            required
            class="pl-9 pr-10"
          >
            <template #prefix>
              <Lock
                class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
              />
            </template>
            <template #suffix>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                tabindex="-1"
                class="absolute right-1 top-1/2 h-8 w-8 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                :aria-label="
                  showResetPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                :title="
                  showResetPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                @click="showResetPassword = !showResetPassword"
              >
                <EyeOff v-if="showResetPassword" class="h-4 w-4" />
                <Eye v-else class="h-4 w-4" />
              </Button>
            </template>
          </FormField>

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
        <div class="mb-6 space-y-1.5">
          <h2 class="text-2xl font-semibold tracking-tight">
            {{ t("login.welcome") }}
          </h2>
          <p class="text-sm text-muted-foreground">
            {{ t("login.description") }}
          </p>
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
            autocomplete="username"
            required
            class="pl-9"
          >
            <template #prefix>
              <Mail
                class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
              />
            </template>
          </FormField>

          <!-- Password Input -->
          <FormField
            v-model="loginForm.password"
            :type="showLoginPassword ? 'text' : 'password'"
            :label="t('login.password')"
            :placeholder="t('login.passwordPlaceholder')"
            autocomplete="current-password"
            required
            class="pl-9 pr-10"
          >
            <template #prefix>
              <Lock
                class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
              />
            </template>
            <template #suffix>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                tabindex="-1"
                class="absolute right-1 top-1/2 h-8 w-8 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                :aria-label="
                  showLoginPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                :title="
                  showLoginPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                @click="showLoginPassword = !showLoginPassword"
              >
                <EyeOff v-if="showLoginPassword" class="h-4 w-4" />
                <Eye v-else class="h-4 w-4" />
              </Button>
            </template>
          </FormField>

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
        <div class="mb-6 space-y-1.5">
          <h2 class="text-2xl font-semibold tracking-tight">
            {{ t("register.welcome") }}
          </h2>
          <p class="text-sm text-muted-foreground">
            {{ t("register.description") }}
          </p>
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
            autocomplete="email"
            required
            class="pl-9"
          >
            <template #prefix>
              <Mail
                class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
              />
            </template>
          </FormField>

          <!-- Title (Username) Input -->
          <FormField
            v-model="registerForm.title"
            type="text"
            :label="t('register.title')"
            :placeholder="t('register.titlePlaceholder')"
            autocomplete="nickname"
            class="pl-9"
          >
            <template #prefix>
              <User
                class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
              />
            </template>
          </FormField>

          <!-- Password Input -->
          <FormField
            v-model="registerForm.password"
            :type="showRegisterPassword ? 'text' : 'password'"
            :label="t('login.password')"
            :placeholder="t('login.passwordPlaceholder')"
            autocomplete="new-password"
            required
            class="pl-9 pr-10"
          >
            <template #prefix>
              <Lock
                class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
              />
            </template>
            <template #suffix>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                tabindex="-1"
                class="absolute right-1 top-1/2 h-8 w-8 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                :aria-label="
                  showRegisterPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                :title="
                  showRegisterPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                @click="showRegisterPassword = !showRegisterPassword"
              >
                <EyeOff v-if="showRegisterPassword" class="h-4 w-4" />
                <Eye v-else class="h-4 w-4" />
              </Button>
            </template>
          </FormField>

          <!-- Confirm Password Input -->
          <FormField
            v-model="registerForm.confirmPassword"
            :type="showRegisterPassword ? 'text' : 'password'"
            :label="t('register.confirmPassword')"
            :placeholder="t('register.confirmPasswordPlaceholder')"
            autocomplete="new-password"
            required
            class="pl-9 pr-10"
          >
            <template #prefix>
              <Lock
                class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
              />
            </template>
            <template #suffix>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                tabindex="-1"
                class="absolute right-1 top-1/2 h-8 w-8 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                :aria-label="
                  showRegisterPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                :title="
                  showRegisterPassword ? t('login.hidePassword') : t('login.showPassword')
                "
                @click="showRegisterPassword = !showRegisterPassword"
              >
                <EyeOff v-if="showRegisterPassword" class="h-4 w-4" />
                <Eye v-else class="h-4 w-4" />
              </Button>
            </template>
          </FormField>

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
</template>

<script setup lang="ts">
import {
  AlertCircle,
  CheckCircle2,
  Eye,
  EyeOff,
  KeyRound,
  Loader2,
  Lock,
  Mail,
  User,
} from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listIdentityProviders } from "@/api/identity-provider";
import { getWorkspaceProfileSetting } from "@/api/setting";
import * as userApi from "@/api/user";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { FormField } from "@/components/ui/form-field";
import { Separator } from "@/components/ui/separator";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { useSSOLogin } from "@/composables/useSSOLogin";
import { useOneShot } from "@/composables/useTimers";
import { notify } from "@/lib/notify";
import { isKnownSpaRouteName, resolveSignInTarget } from "@/lib/signInRedirect";
import { useAuthStore } from "@/store/modules/auth";
import {
  type IdentityProvider,
  IdentityProviderType,
} from "@/types/proto-es/v1/identity_provider_service_pb";

const { t } = useI18n();
const router = useRouter();
const route = useRoute();
const authStore = useAuthStore();
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

// The redirect target is a query parameter, so a repeated `?redirect=` arrives
// as an array; only a single string names a path.
function redirectTarget(): string | undefined {
  return typeof route.query.redirect === "string"
    ? route.query.redirect
    : undefined;
}

async function handleSSOLogin(provider: IdentityProvider) {
  errorMessage.value = "";
  try {
    const started = await startSSOLogin(provider, {
      redirect: redirectTarget(),
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
// These reveal toggles are out of the tab order (`tabindex="-1"` in the
// template): Tab from a password field must reach the next field, not an
// adornment inside the current one. Pointer and assistive-technology users can
// still click or activate them.
const showLoginPassword = ref(false);
const showRegisterPassword = ref(false);
const showResetPassword = ref(false);

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
    const target = resolveSignInTarget(redirectTarget(), (path) =>
      isKnownSpaRouteName(router.resolve(path).name)
    );
    if (target.kind === "server") {
      // `replace` keeps the login form out of the history: with the session
      // established, Back would only be bounced forward again by the guard.
      window.location.replace(target.path);
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

    const redirect = redirectTarget();
    router.push(redirect ?? { name: "Home" });
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

/** Switching to the login form waits for the success message to be read. */
const switchToLoginLater = useOneShot(
  () => switchToLogin(),
  REGISTRATION_SUCCESS_DELAY
);

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

    // Switch to login after a short delay; the timer is owned by the scope so a
    // navigated-away page cannot flip a form it no longer shows.
    switchToLoginLater.start();
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
