<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('qualityAgents.listSection')"
      :description="t('qualityAgents.listSectionHint')"
    >
      <template #actions>
        <Button
          variant="outline"
          :disabled="loading"
          @click="load"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': loading }"
          />
          {{ t("common.refresh") }}
        </Button>
        <Button
          v-if="canManage"
          as-child
        >
          <RouterLink :to="{ name: 'QualityAgentsSettings' }">
            <Settings class="mr-2 h-4 w-4" />
            {{ t("qualityAgents.title") }}
          </RouterLink>
        </Button>
      </template>
    </PageHeader>

    <Alert
      v-if="error"
      variant="destructive"
    >
      <AlertTriangle class="h-4 w-4" />
      <AlertDescription>{{ error }}</AlertDescription>
    </Alert>

    <PageState :loading="loading">
      <div
        v-if="agents.length === 0"
        class="rounded-lg border border-dashed p-8 text-center"
      >
        <p class="font-medium">{{ t("qualityAgents.emptyTitle") }}</p>
        <p class="mt-1 text-sm text-muted-foreground">{{ t("qualityAgents.emptyDescription") }}</p>
      </div>
      <div
        v-else
        class="grid gap-3 md:grid-cols-2 xl:grid-cols-3"
      >
        <Card
          v-for="agent in agents"
          :key="agent.name"
        >
          <CardHeader class="border-b">
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0">
                <CardTitle class="truncate text-base">{{ agent.displayName }}</CardTitle>
                <CardDescription class="truncate">{{ agent.description }}</CardDescription>
              </div>
              <Badge :variant="stateVariant(agent.state)">
                {{ stateLabel(agent.state) }}
              </Badge>
            </div>
          </CardHeader>
          <CardContent class="space-y-2 pt-4 text-sm">
            <div class="flex flex-wrap gap-1">
              <Badge
                v-for="capability in agent.capabilities"
                :key="capability"
                variant="secondary"
              >
                {{ capabilityLabel(capability) }}
              </Badge>
            </div>
            <p class="text-xs text-muted-foreground">
              {{ t("qualityAgents.columnDatabases") }}: {{ agent.databases.length }} ·
              {{ t("qualityAgents.columnOwner") }}: {{ ownerLabel(agent.owner) }}
            </p>
            <Button
              size="sm"
              variant="outline"
              as-child
            >
              <RouterLink :to="agentRoute(agent.name)">
                {{ t("qualityAgents.open") }}
              </RouterLink>
            </Button>
          </CardContent>
        </Card>
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import { AlertTriangle, RefreshCcw, Settings } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { listQualityAgents } from "@/api/data-quality";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityAgent,
  QualityAgentState,
} from "@/types/proto-es/v1/data_quality_service_pb";

const { t } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();
const loading = ref(true);
const error = ref("");
const agents = ref<QualityAgent[]>([]);

const canManage = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.agents.manage")
);

function stateLabel(state: QualityAgentState): string {
  switch (state) {
    case QualityAgentState.ACTIVE:
      return t("qualityAgents.stateActive");
    case QualityAgentState.PAUSED:
      return t("qualityAgents.statePaused");
    case QualityAgentState.DISABLED:
      return t("qualityAgents.stateDisabled");
    case QualityAgentState.DRAFT:
      return t("qualityAgents.stateDraft");
    default:
      return t("qualityAgents.stateUnspecified");
  }
}

function stateVariant(state: QualityAgentState) {
  switch (state) {
    case QualityAgentState.ACTIVE:
      return "success" as const;
    case QualityAgentState.PAUSED:
      return "warning" as const;
    case QualityAgentState.DISABLED:
      return "neutral" as const;
    default:
      return "outline" as const;
  }
}

function capabilityLabel(id: string): string {
  switch (id) {
    case "suggest":
      return t("qualityAgents.capabilitySuggest");
    case "adopt":
      return t("qualityAgents.capabilityAdopt");
    case "patrol":
      return t("qualityAgents.capabilityPatrol");
    default:
      return id;
  }
}

function agentRoute(name: string) {
  const parts = name.split("/");
  return {
    name: "QualityAgent",
    params: { workspace: parts[1], agentId: parts[3] },
  };
}

function ownerLabel(owner: string): string {
  return owner === "" ? "-" : owner;
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    agents.value = await listQualityAgents();
  } catch (err) {
    error.value = formatError(err);
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>
