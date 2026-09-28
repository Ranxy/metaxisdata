<template>
  <div class="mx-auto max-w-xl space-y-4">
    <div>
      <h1 class="text-2xl font-bold tracking-tight">
        {{ t("oauthConsent.title") }}
      </h1>
      <p class="text-muted-foreground mt-1">
        {{ t("oauthConsent.description") }}
      </p>
    </div>

    <Card>
      <CardContent class="space-y-4 pt-6">
        <div
          v-if="state === 'loading'"
          class="flex justify-center p-8"
        >
          <AppLoading />
        </div>

        <p
          v-else-if="state === 'invalid'"
          class="text-sm"
        >
          {{ t("oauthConsent.invalidLink") }}
        </p>

        <!-- The decision step: exactly what the approval would hand over. -->
        <template v-else-if="state === 'confirm' && request">
          <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
            <dt class="text-muted-foreground">
              {{ t("oauthConsent.client") }}
            </dt>
            <dd>{{ request.clientName || t("oauthConsent.unknownClient") }}</dd>
            <dt class="text-muted-foreground">
              {{ t("oauthConsent.requestIp") }}
            </dt>
            <dd>{{ request.requestIp || t("oauthConsent.unknown") }}</dd>
            <dt class="text-muted-foreground">
              {{ t("oauthConsent.requestedAt") }}
            </dt>
            <dd>{{ formatTime(request.createTime) }}</dd>
            <dt class="text-muted-foreground">
              {{ t("oauthConsent.expiresAt") }}
            </dt>
            <dd>{{ formatTime(request.expireTime) }}</dd>
            <dt class="text-muted-foreground">
              {{ t("oauthConsent.resource") }}
            </dt>
            <dd class="break-all">
              {{ request.resource }}
            </dd>
          </dl>

          <!-- The host is deliberately large: a redirect to an address the user
               did not expect is the signal this page exists to surface. -->
          <div class="rounded-md border p-4 text-center">
            <p class="text-sm text-muted-foreground">
              {{ t("oauthConsent.redirectHint") }}
            </p>
            <p class="mt-2 break-all font-mono text-2xl font-bold">
              {{ redirectHost }}
            </p>
            <p class="mt-1 break-all font-mono text-xs text-muted-foreground">
              {{ request.redirectUri }}
            </p>
          </div>

          <div>
            <p class="text-sm font-medium">
              {{ t("oauthConsent.scopesTitle") }}
            </p>
            <ul class="mt-1 list-disc space-y-1 pl-5 text-sm">
              <li
                v-for="scope in request.scopes"
                :key="scope"
              >
                {{ scopeDescription(scope) }}
              </li>
            </ul>
          </div>

          <div class="flex gap-2">
            <Button
              :disabled="isDeciding"
              @click="decide(true)"
            >
              <Loader2
                v-if="isDeciding"
                class="mr-2 h-4 w-4 animate-spin"
              />
              {{ t("oauthConsent.approve") }}
            </Button>
            <Button
              variant="secondary"
              :disabled="isDeciding"
              @click="decide(false)"
            >
              {{ t("oauthConsent.deny") }}
            </Button>
          </div>
        </template>

        <!-- The request is settled, or can no longer be acted on. Each terminal
             state says what happened; none of them shows a code, because the
             page never sees one. -->
        <div
          v-else
          class="space-y-4"
        >
          <p v-if="state === 'approved'">
            {{ t("oauthConsent.outcome.approved") }}
          </p>
          <p v-else-if="state === 'denied'">
            {{ t("oauthConsent.outcome.denied") }}
          </p>
          <p v-else-if="state === 'expired'">
            {{ t("oauthConsent.outcome.expired") }}
          </p>
          <p v-else-if="state === 'notFound'">
            {{ t("oauthConsent.outcome.notFound") }}
          </p>
          <p v-else-if="state === 'permissionDenied'">
            {{ t("oauthConsent.outcome.permissionDenied") }}
          </p>
          <p v-else>
            {{ errorMessage }}
          </p>
        </div>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Code } from "@connectrpc/connect";
import { Loader2 } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  approveOAuthAuthorizationRequest,
  getOAuthAuthorizationRequest,
} from "@/api/oauth";
import AppLoading from "@/components/common/AppLoading.vue";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useErrorMessage } from "@/composables/useErrorHandler";
import type { OAuthAuthorizationRequest } from "@/types/proto-es/v1/oauth_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { errorCode } from "@/utils/error";

type PageState =
  | "loading"
  | "invalid"
  | "confirm"
  | "approved"
  | "denied"
  | "notFound"
  | "expired"
  | "permissionDenied"
  | "error";

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const route = useRoute();

// The page only ever loads a request and records a decision; nothing is
// approved before the user presses the button.
const state = ref<PageState>("loading");
const request = ref<OAuthAuthorizationRequest>();
const isDeciding = ref(false);
const errorMessage = ref("");

const requestId = computed(() => {
  const value = route.query.request_id;
  return typeof value === "string" ? value : "";
});

// A person recognises an address, not a URL: the host is what tells them the
// code is about to be handed to somewhere they did not expect.
const redirectHost = computed(() => {
  const uri = request.value?.redirectUri ?? "";
  try {
    return new URL(uri).host || uri;
  } catch {
    // The server accepts only registered absolute redirect URIs, so this should
    // not be reachable; showing the value verbatim beats an empty line where the
    // host belongs.
    return uri;
  }
});

function formatTime(value?: Timestamp): string {
  return formatDateTime(value, locale.value, {
    seconds: true,
    fallback: t("oauthConsent.unknown"),
  });
}

// A scope is a protocol identifier; the person approving it is owed a sentence.
// An unknown scope is shown as it arrived, which is still truer than a made-up
// description.
function scopeDescription(scope: string): string {
  switch (scope) {
    case "metaxisdata.mcp.read":
      return t("oauthConsent.scopeMcpRead");
    default:
      return scope;
  }
}

function stateForError(error: unknown): PageState {
  switch (errorCode(error)) {
    case Code.InvalidArgument:
      return "invalid";
    case Code.NotFound:
      // Unknown, already consumed and never issued are one answer here: there is
      // nothing left to decide.
      return "notFound";
    case Code.FailedPrecondition:
      // The server reports "expired" and "already answered" with the same code,
      // and the user can act on neither, so they share one state.
      return "expired";
    case Code.PermissionDenied:
      return "permissionDenied";
    default:
      return "error";
  }
}

async function load() {
  state.value = "loading";
  try {
    request.value = await getOAuthAuthorizationRequest(requestId.value);
    state.value = "confirm";
  } catch (error) {
    errorMessage.value = formatError(error);
    state.value = stateForError(error);
  }
}

async function decide(approve: boolean) {
  errorMessage.value = "";
  isDeciding.value = true;
  try {
    await approveOAuthAuthorizationRequest(requestId.value, approve);
    if (!approve) {
      state.value = "denied";
      return;
    }
    // The code is minted server-side and redirected straight to the client, so
    // the page hands the browser to the completion endpoint instead of building
    // a callback itself. A relative URL is correct: the SPA and the server share
    // an origin in production, and the dev proxy forwards this path.
    state.value = "approved";
    window.location.assign(
      "/oauth/authorize/complete?request_id=" +
        encodeURIComponent(requestId.value)
    );
  } catch (error) {
    errorMessage.value = formatError(error);
    state.value = stateForError(error);
  } finally {
    isDeciding.value = false;
  }
}

onMounted(async () => {
  // Without an id this page was not reached through the server's redirect, so
  // there is nothing to look up; asking anyway would only turn a bad link into a
  // generic error.
  if (!requestId.value) {
    state.value = "invalid";
    return;
  }
  await load();
});
</script>
