<template>
  <Card>
    <CardHeader class="space-y-1">
      <CardTitle>{{ t("dataQualitySettings.agentsSection") }}</CardTitle>
      <CardDescription>
        {{ t("dataQualitySettings.agentsSectionDescription") }}
      </CardDescription>
    </CardHeader>
    <CardContent class="space-y-3">
      <p
        v-if="loading"
        class="text-sm text-muted-foreground"
      >
        {{ t("common.loading") }}
      </p>
      <Alert
        v-else-if="error"
        variant="destructive"
      >
        <AlertTriangle class="h-4 w-4" />
        <AlertDescription class="break-words">
          {{ error }}
        </AlertDescription>
      </Alert>
      <template v-else>
        <p
          v-if="agents.length === 0"
          class="text-sm text-muted-foreground"
        >
          {{ t("dataQualitySettings.agentsEmpty") }}
        </p>
        <ul
          v-else
          class="divide-y rounded-md border"
        >
          <li
            v-for="agent in agents"
            :key="agent.name"
            class="flex flex-wrap items-center justify-between gap-2 px-3 py-2"
          >
            <span class="flex min-w-0 flex-wrap items-center gap-2 text-sm">
              <span class="font-medium">{{ agent.displayName }}</span>
              <Badge :variant="agent.enabled ? 'success' : 'neutral'">
                {{
                  agent.enabled
                    ? t("dataQualitySettings.agentsEnabled")
                    : t("dataQualitySettings.agentsDisabled")
                }}
              </Badge>
              <span
                v-if="agent.description"
                class="truncate text-xs text-muted-foreground"
              >
                {{ agent.description }}
              </span>
            </span>
            <span
              v-if="canUpdate && agent.enabled"
              class="flex shrink-0 items-center gap-1"
            >
              <Button
                variant="ghost"
                size="sm"
                @click="rotate(agent)"
              >
                {{ t("dataQualitySettings.agentsRotate") }}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                class="text-destructive"
                @click="disableTarget = agent"
              >
                {{ t("dataQualitySettings.agentsDisable") }}
              </Button>
            </span>
          </li>
        </ul>
        <Button
          v-if="canUpdate"
          size="sm"
          @click="createOpen = true"
        >
          {{ t("dataQualitySettings.agentsCreate") }}
        </Button>

        <!-- The token is shown once: it cannot be retrieved again, only rotated. -->
        <Alert
          v-if="issuedToken"
          class="space-y-2"
        >
          <AlertTitle>{{ t("dataQualitySettings.agentsTokenTitle") }}</AlertTitle>
          <AlertDescription class="space-y-2">
            <p>{{ t("dataQualitySettings.agentsTokenHint", { name: issuedName }) }}</p>
            <div class="flex flex-wrap items-center gap-2">
              <code class="max-w-full break-all rounded bg-muted px-2 py-1 text-xs">{{ issuedToken }}</code>
              <Button
                size="sm"
                variant="outline"
                @click="copyToken"
              >
                {{ t("dataQualitySettings.agentsCopyToken") }}
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      </template>
    </CardContent>

    <Dialog
      :open="createOpen"
      @update:open="createOpen = $event"
    >
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t("dataQualitySettings.agentsCreateTitle") }}</DialogTitle>
          <DialogDescription>
            {{ t("dataQualitySettings.agentsCreateDescription") }}
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-3">
          <div class="space-y-1">
            <Label for="dq-agent-name">{{ t("dataQualitySettings.agentsName") }}</Label>
            <Input
              id="dq-agent-name"
              v-model="displayName"
              maxlength="200"
            />
          </div>
          <div class="space-y-1">
            <Label for="dq-agent-description">{{ t("dataQualitySettings.agentsDescription") }}</Label>
            <Input
              id="dq-agent-description"
              v-model="description"
              maxlength="2000"
            />
          </div>
          <Alert
            v-if="createError"
            variant="destructive"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertDescription class="break-words">{{ createError }}</AlertDescription>
          </Alert>
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            @click="createOpen = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="busy || displayName.trim() === ''"
            @click="create"
          >
            {{ t("dataQualitySettings.agentsCreate") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog
      :open="disableTarget !== undefined"
      @update:open="(open) => { if (!open) disableTarget = undefined; }"
    >
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t("dataQualitySettings.agentsDisableTitle") }}</DialogTitle>
          <DialogDescription>
            {{ t("dataQualitySettings.agentsDisableDescription", { name: disableTarget?.displayName ?? "" }) }}
          </DialogDescription>
        </DialogHeader>
        <Alert
          v-if="disableError"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertDescription class="break-words">{{ disableError }}</AlertDescription>
        </Alert>
        <DialogFooter>
          <Button
            variant="outline"
            @click="disableTarget = undefined"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            variant="destructive"
            :disabled="busy"
            @click="confirmDisable"
          >
            {{ t("dataQualitySettings.agentsDisable") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </Card>
</template>

<script setup lang="ts">
import { AlertTriangle } from "lucide-vue-next";
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  createQualityAgent,
  disableQualityAgent,
  listQualityAgents,
  rotateQualityAgentToken,
} from "@/api/data-quality";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import type { QualityAgent } from "@/types/proto-es/v1/data_quality_service_pb";

/**
 * The workspace's agent principals. An administrator creates one and gets its bearer
 * token once; rotating replaces it, disabling ends every token. The token is never
 * shown again, and the list never carries one.
 */

defineProps<{ canUpdate: boolean }>();

const { t } = useI18n();
const { formatError } = useErrorMessage();

const agents = ref<QualityAgent[]>([]);
const loading = ref(false);
const error = ref("");
const busy = ref(false);
const createOpen = ref(false);
const displayName = ref("");
const description = ref("");
const createError = ref("");
const disableTarget = ref<QualityAgent | undefined>(undefined);
const disableError = ref("");
const issuedToken = ref("");
const issuedName = ref("");

async function load() {
  loading.value = true;
  error.value = "";
  try {
    agents.value = await listQualityAgents();
  } catch (e: unknown) {
    error.value = formatError(e, "dataQualitySettings.agentsLoadFailed");
  } finally {
    loading.value = false;
  }
}

function showToken(name: string, token: string) {
  issuedName.value = name;
  issuedToken.value = token;
}

async function copyToken() {
  try {
    await navigator.clipboard.writeText(issuedToken.value);
    notify.success(t("dataQualitySettings.agentsTokenCopied"));
  } catch {
    notify.warning(t("dataQualitySettings.agentsTokenCopyFailed"));
  }
}

async function create() {
  if (busy.value || displayName.value.trim() === "") {
    return;
  }
  busy.value = true;
  createError.value = "";
  try {
    const response = await createQualityAgent({
      displayName: displayName.value.trim(),
      description: description.value.trim(),
    });
    if (response.agent) {
      showToken(response.agent.displayName, response.token);
    }
    createOpen.value = false;
    displayName.value = "";
    description.value = "";
    notify.success(t("dataQualitySettings.agentsCreated"));
    await load();
  } catch (e: unknown) {
    createError.value = formatError(
      e,
      "dataQualitySettings.agentsCreateFailed"
    );
  } finally {
    busy.value = false;
  }
}

async function rotate(agent: QualityAgent) {
  busy.value = true;
  try {
    const response = await rotateQualityAgentToken(agent.name);
    if (response.agent) {
      showToken(response.agent.displayName, response.token);
    }
    notify.success(t("dataQualitySettings.agentsRotated"));
    await load();
  } catch (e: unknown) {
    error.value = formatError(e, "dataQualitySettings.agentsRotateFailed");
  } finally {
    busy.value = false;
  }
}

async function confirmDisable() {
  const target = disableTarget.value;
  if (!target) {
    return;
  }
  busy.value = true;
  disableError.value = "";
  try {
    await disableQualityAgent(target.name);
    disableTarget.value = undefined;
    notify.success(t("dataQualitySettings.agentsDisabledNotice"));
    await load();
  } catch (e: unknown) {
    disableError.value = formatError(
      e,
      "dataQualitySettings.agentsDisableFailed"
    );
  } finally {
    busy.value = false;
  }
}

onMounted(load);
</script>
