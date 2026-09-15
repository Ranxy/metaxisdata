<template>
  <div class="mx-auto max-w-xl space-y-4">
    <div>
      <h1 class="text-2xl font-bold tracking-tight">
        {{ t("deviceLogin.title") }}
      </h1>
      <p class="text-muted-foreground mt-1">
        {{ t("deviceLogin.description") }}
      </p>
    </div>

    <Alert
      v-if="errorMessage"
      variant="destructive"
    >
      <AlertCircle class="h-4 w-4" />
      <AlertDescription>{{ errorMessage }}</AlertDescription>
    </Alert>

    <Card>
      <CardContent class="space-y-4 pt-6">
        <!-- Step 1: the user types the code shown in the terminal. -->
        <form
          v-if="step === 'input'"
          class="space-y-4"
          @submit.prevent="loadDeviceLogin"
        >
          <AppInput
            v-model="userCodeInput"
            :label="t('deviceLogin.codeLabel')"
            :placeholder="t('deviceLogin.codePlaceholder')"
            autocomplete="off"
            required
          />
          <p
            v-if="prefilled"
            class="text-sm text-muted-foreground"
          >
            {{ t("deviceLogin.prefilledHint") }}
          </p>
          <AppButton
            type="submit"
            :loading="isLoading"
          >
            {{ t("deviceLogin.continue") }}
          </AppButton>
        </form>

        <!-- Step 2: show exactly what is being approved. -->
        <template v-else-if="step === 'confirm' && deviceLogin">
          <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
            <dt class="text-muted-foreground">
              {{ t("deviceLogin.client") }}
            </dt>
            <dd>{{ clientDescription }}</dd>
            <dt class="text-muted-foreground">
              {{ t("deviceLogin.requestIp") }}
            </dt>
            <dd>{{ deviceLogin.requestIp || t("deviceLogin.unknown") }}</dd>
            <dt class="text-muted-foreground">
              {{ t("deviceLogin.requestedAt") }}
            </dt>
            <dd>{{ formatTime(deviceLogin.createTime) }}</dd>
            <dt class="text-muted-foreground">
              {{ t("deviceLogin.expiresAt") }}
            </dt>
            <dd>{{ formatTime(deviceLogin.expireTime) }}</dd>
          </dl>

          <div class="rounded-md border p-4 text-center">
            <p class="text-sm text-muted-foreground">
              {{ t("deviceLogin.codeCompareHint") }}
            </p>
            <!-- Deliberately large: the point of the page is to compare this
                 with the code shown in the terminal that started the request. -->
            <p class="mt-2 font-mono text-3xl font-bold tracking-widest">
              {{ deviceLogin.userCode }}
            </p>
          </div>

          <div class="flex gap-2">
            <AppButton
              :loading="isLoading"
              @click="decide(true)"
            >
              {{ t("deviceLogin.approve") }}
            </AppButton>
            <AppButton
              variant="secondary"
              :disabled="isLoading"
              @click="decide(false)"
            >
              {{ t("deviceLogin.deny") }}
            </AppButton>
          </div>
        </template>

        <!-- Step 3: the request is settled. -->
        <div
          v-else
          class="space-y-4"
        >
          <p v-if="outcome === 'approved'">
            {{ t("deviceLogin.outcome.approved") }}
          </p>
          <p v-else-if="outcome === 'denied'">
            {{ t("deviceLogin.outcome.denied") }}
          </p>
          <p v-else>
            {{ t("deviceLogin.outcome.expired") }}
          </p>
          <AppButton
            variant="secondary"
            @click="startOver"
          >
            {{ t("deviceLogin.startOver") }}
          </AppButton>
        </div>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { AlertCircle } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { approveDeviceLogin, getDeviceLogin } from "@/api/device-login";
import AppButton from "@/components/common/AppButton.vue";
import AppInput from "@/components/common/AppInput.vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardContent } from "@/components/ui/card";
import {
  type DeviceLogin,
  DeviceLoginState,
} from "@/types/proto-es/v1/auth_service_pb";
import { extractErrorMessage } from "@/utils/error";

type Step = "input" | "confirm" | "done";
type Outcome = "approved" | "denied" | "expired";

const { t, locale } = useI18n();
const route = useRoute();

const step = ref<Step>("input");
const outcome = ref<Outcome>("approved");
const deviceLogin = ref<DeviceLogin>();
const userCodeInput = ref("");
const isLoading = ref(false);
const errorMessage = ref("");
const prefilled = ref(false);

const clientDescription = computed(() => {
  const login = deviceLogin.value;
  if (!login) return "";
  const version = login.clientVersion ? ` ${login.clientVersion}` : "";
  return `${login.clientName || t("deviceLogin.unknownClient")}${version}`;
});

function formatTime(value?: Timestamp): string {
  if (!value) return t("deviceLogin.unknown");
  const milliseconds =
    Number(value.seconds ?? 0) * 1000 +
    Math.floor((value.nanos ?? 0) / 1_000_000);
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) {
    return t("deviceLogin.unknown");
  }
  return new Date(milliseconds).toLocaleString(locale.value);
}

async function loadDeviceLogin() {
  errorMessage.value = "";
  isLoading.value = true;
  try {
    const login = await getDeviceLogin(userCodeInput.value);
    deviceLogin.value = login;
    userCodeInput.value = login.userCode;

    if (login.state === DeviceLoginState.PENDING) {
      step.value = "confirm";
      return;
    }
    // The request was already settled (or expired) before this page saw it.
    outcome.value = outcomeFor(login.state);
    step.value = "done";
  } catch (error) {
    errorMessage.value = extractErrorMessage(error);
  } finally {
    isLoading.value = false;
  }
}

async function decide(approve: boolean) {
  errorMessage.value = "";
  isLoading.value = true;
  try {
    await approveDeviceLogin(userCodeInput.value, approve);
    outcome.value = approve ? "approved" : "denied";
    step.value = "done";
  } catch (error) {
    errorMessage.value = extractErrorMessage(error);
  } finally {
    isLoading.value = false;
  }
}

function outcomeFor(state: DeviceLoginState): Outcome {
  switch (state) {
    case DeviceLoginState.APPROVED:
      return "approved";
    case DeviceLoginState.DENIED:
      return "denied";
    case DeviceLoginState.EXPIRED:
      return "expired";
    default:
      return "denied";
  }
}

function startOver() {
  step.value = "input";
  deviceLogin.value = undefined;
  userCodeInput.value = "";
  errorMessage.value = "";
  prefilled.value = false;
}

onMounted(() => {
  // A code in the URL only prefills the field; the user still has to read the
  // page and confirm, because such a link can come from anyone.
  const fromQuery = route.query.user_code;
  if (typeof fromQuery === "string" && fromQuery) {
    userCodeInput.value = fromQuery;
    prefilled.value = true;
  }
});
</script>
