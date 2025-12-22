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
          <!-- Login Mode -->
          <template v-if="!isRegisterMode">
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
              <AppInput
                v-model="loginForm.email"
                type="text"
                :label="t('login.email')"
                :placeholder="t('login.emailPlaceholder')"
                required
              />

              <!-- Password Input -->
              <AppInput
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

            <!-- Divider -->
            <div class="relative my-6">
              <div class="absolute inset-0 flex items-center">
                <Separator class="w-full" />
              </div>
              <div class="relative flex justify-center text-xs uppercase">
                <span class="bg-card px-2 text-muted-foreground">{{ t("login.or") }}</span>
              </div>
            </div>

            <!-- SSO Login (Disabled) -->
            <Button
              variant="outline"
              class="w-full"
              disabled
            >
              <Lock class="mr-2 h-4 w-4" />
              {{ t("login.sso") }}
            </Button>

            <!-- Switch to Register -->
            <div class="mt-6 text-center text-sm">
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
              <AppInput
                v-model="registerForm.email"
                type="email"
                :label="t('login.email')"
                :placeholder="t('login.emailPlaceholder')"
                required
              />

              <!-- Title (Username) Input -->
              <AppInput
                v-model="registerForm.title"
                type="text"
                :label="t('register.title')"
                :placeholder="t('register.titlePlaceholder')"
              />

              <!-- Password Input -->
              <AppInput
                v-model="registerForm.password"
                type="password"
                :label="t('login.password')"
                :placeholder="t('login.passwordPlaceholder')"
                required
              />

              <!-- Confirm Password Input -->
              <AppInput
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
          @click="changeLocale(item.value)"
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
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import * as userApi from "@/api/user";
import AppInput from "@/components/common/AppInput.vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardTitle,
} from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { useAppStore } from "@/store/modules/app";
import { useAuthStore } from "@/store/modules/auth";
import { extractErrorMessage } from "@/utils/error";

const { t, locale } = useI18n();
const router = useRouter();
const route = useRoute();
const authStore = useAuthStore();
const appStore = useAppStore();

// Mode toggle
const isRegisterMode = ref(false);

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

const locales = [
  { value: "zh-CN", label: "简体中文" },
  { value: "en-US", label: "English" },
];

const currentLocale = computed(() => appStore.locale);

function changeLocale(newLocale: string) {
  appStore.setLocale(newLocale);
  locale.value = newLocale as "zh-CN" | "en-US";
}

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
    await authStore.login(loginForm.value.email, loginForm.value.password);

    // Redirect to the original destination or home
    const redirect = route.query.redirect as string;
    router.push(redirect || { name: "Home" });
  } catch (error) {
    // Show the actual error message from the server
    const message = extractErrorMessage(error);
    errorMessage.value = message || t("login.loginFailed");
    console.error("Login error:", error);
  }
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
    // Show the actual error message from the server
    const message = extractErrorMessage(error);
    errorMessage.value = message || t("register.registerFailed");
    console.error("Register error:", error);
  } finally {
    isRegistering.value = false;
  }
}
</script>
