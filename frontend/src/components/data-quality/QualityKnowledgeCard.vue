<template>
  <Card>
    <CardHeader class="border-b">
      <CardTitle class="text-base">
        {{ t("dataQuality.knowledge.title") }}
      </CardTitle>
      <CardDescription>{{ t("dataQuality.knowledge.hint") }}</CardDescription>
    </CardHeader>
    <CardContent class="space-y-4 pt-4">
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
        <AlertDescription class="space-y-2 break-words">
          <p>{{ error }}</p>
          <Button
            variant="outline"
            size="sm"
            @click="load"
          >
            {{ t("common.retry") }}
          </Button>
        </AlertDescription>
      </Alert>
      <template v-else>
        <section class="space-y-2">
          <h3 class="text-sm font-medium">{{ t("dataQuality.knowledge.investigations") }}</h3>
          <p
            v-if="investigations.length === 0"
            class="text-sm text-muted-foreground"
          >
            {{ t("dataQuality.knowledge.investigationsEmpty") }}
          </p>
          <ul
            v-else
            class="space-y-2"
          >
            <li
              v-for="investigation in investigations"
              :key="investigation.name"
              class="rounded-md border px-3 py-2 text-sm"
            >
              <div class="flex flex-wrap items-center gap-2">
                <Badge :variant="verdictVariant(investigation.verdict)">
                  {{ verdictLabel(investigation.verdict) }}
                </Badge>
                <span class="text-xs text-muted-foreground">
                  {{ triggerLabel(investigation.trigger) }}
                  <template v-if="investigation.triggerReference">
                    · {{ investigation.triggerReference }}
                  </template>
                </span>
                <span class="ml-auto text-xs text-muted-foreground">
                  {{ formatDateTime(investigation.createTime, locale) }}
                </span>
              </div>
              <p
                v-if="investigation.summary"
                class="mt-1 break-words"
              >
                {{ investigation.summary }}
              </p>
              <p
                v-if="investigation.failureReason"
                class="mt-1 text-xs text-destructive"
              >
                {{ investigation.failureReason }}
              </p>
              <details
                v-if="investigation.evidence.length > 0 || investigation.hypotheses.length > 0"
                class="mt-1"
              >
                <summary class="cursor-pointer text-xs text-muted-foreground">
                  {{ t("dataQuality.knowledge.details", {
                    evidence: investigation.evidence.length,
                    hypotheses: investigation.hypotheses.length,
                  }) }}
                </summary>
                <ul class="mt-1 space-y-1">
                  <li
                    v-for="(evidence, index) in investigation.evidence"
                    :key="`e-${index}`"
                    class="text-xs text-muted-foreground"
                  >
                    {{ evidence.kind }}: {{ evidence.reference }}
                  </li>
                  <li
                    v-for="(hypothesis, index) in investigation.hypotheses"
                    :key="`h-${index}`"
                    class="text-xs"
                  >
                    {{ hypothesis.status }} · {{ hypothesis.statement }}
                  </li>
                </ul>
              </details>
            </li>
          </ul>
        </section>

        <section class="space-y-2">
          <h3 class="text-sm font-medium">{{ t("dataQuality.knowledge.memories") }}</h3>
          <p
            v-if="memories.length === 0"
            class="text-sm text-muted-foreground"
          >
            {{ t("dataQuality.knowledge.memoriesEmpty") }}
          </p>
          <ul
            v-else
            class="space-y-2"
          >
            <li
              v-for="memory in memories"
              :key="memory.name"
              class="rounded-md border px-3 py-2 text-sm"
            >
              <div class="flex flex-wrap items-center gap-2">
                <Badge variant="neutral">
                  {{ kindLabel(memory.kind) }}
                </Badge>
                <Badge :variant="statusVariant(memory.status)">
                  {{ statusLabel(memory.status) }}
                </Badge>
                <span class="ml-auto text-xs text-muted-foreground">
                  {{ formatDateTime(memory.createTime, locale) }}
                </span>
              </div>
              <p class="mt-1 break-words">{{ memory.statement }}</p>
              <div
                v-if="
                  memory.status ===
                  QualityMemoryStatus.PROPOSED &&
                  canConfirm
                "
                class="mt-2 flex gap-2"
              >
                <Button
                  size="sm"
                  :disabled="busyName !== ''"
                  @click="decide(memory, 'confirm')"
                >
                  {{ t("dataQuality.knowledge.confirm") }}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  :disabled="busyName !== ''"
                  @click="decide(memory, 'reject')"
                >
                  {{ t("dataQuality.knowledge.reject") }}
                </Button>
              </div>
            </li>
          </ul>
        </section>
      </template>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { AlertTriangle } from "lucide-vue-next";
import { onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  confirmQualityMemory,
  listQualityInvestigations,
  listQualityMemories,
} from "@/api/data-quality";
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
import { notify } from "@/lib/notify";
import {
  ConfirmQualityMemoryRequest_Decision,
  type QualityInvestigation,
  QualityInvestigationTrigger,
  QualityInvestigationVerdict,
  type QualityMemory,
  QualityMemoryKind,
  QualityMemoryStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime } from "@/utils/datetime";

/**
 * What the system learned about one target: the agent's investigations (hypotheses,
 * evidence references, verdict) and the workspace's memory of it. A proposed memory is
 * only knowledge once a person confirms it; nothing here changes a check's verdict.
 */

const props = defineProps<{
  targetGuid: string;
  canConfirm: boolean;
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();

const investigations = ref<QualityInvestigation[]>([]);
const memories = ref<QualityMemory[]>([]);
const loading = ref(false);
const error = ref("");
const busyName = ref("");
let loadGeneration = 0;
let loadController: AbortController | undefined;

async function load() {
  // A target switch reuses this component: an older reply must never write the
  // previous target's rows under the new one.
  const generation = ++loadGeneration;
  loadController?.abort();
  const controller = new AbortController();
  loadController = controller;
  investigations.value = [];
  memories.value = [];
  error.value = "";
  if (props.targetGuid === "") {
    loading.value = false;
    return;
  }
  loading.value = true;
  try {
    const [nextInvestigations, nextMemories] = await Promise.all([
      listQualityInvestigations(props.targetGuid, controller.signal),
      listQualityMemories(props.targetGuid, controller.signal),
    ]);
    if (generation !== loadGeneration || controller.signal.aborted) {
      return;
    }
    investigations.value = nextInvestigations;
    memories.value = nextMemories;
  } catch (e: unknown) {
    if (generation !== loadGeneration || controller.signal.aborted) {
      return;
    }
    error.value = formatError(e, "dataQuality.knowledge.loadFailed");
  } finally {
    if (generation === loadGeneration) {
      loading.value = false;
    }
  }
}

async function decide(memory: QualityMemory, choice: "confirm" | "reject") {
  if (busyName.value !== "") {
    return;
  }
  busyName.value = memory.name;
  try {
    const updated = await confirmQualityMemory(
      memory.name,
      choice === "confirm"
        ? ConfirmQualityMemoryRequest_Decision.CONFIRM
        : ConfirmQualityMemoryRequest_Decision.REJECT
    );
    const index = memories.value.findIndex((item) => item.name === memory.name);
    if (index >= 0) {
      memories.value[index] = updated;
    }
    notify.success(
      choice === "confirm"
        ? t("dataQuality.knowledge.confirmed")
        : t("dataQuality.knowledge.rejected")
    );
  } catch (e: unknown) {
    notify.error(formatError(e, "dataQuality.knowledge.decideFailed"));
  } finally {
    busyName.value = "";
  }
}

function verdictLabel(verdict: QualityInvestigationVerdict): string {
  switch (verdict) {
    case QualityInvestigationVerdict.ROOT_CAUSE:
      return t("dataQuality.knowledge.verdictRootCause");
    case QualityInvestigationVerdict.CONTRIBUTING_FACTOR:
      return t("dataQuality.knowledge.verdictContributingFactor");
    case QualityInvestigationVerdict.INCONCLUSIVE:
      return t("dataQuality.knowledge.verdictInconclusive");
    case QualityInvestigationVerdict.UNRELATED:
      return t("dataQuality.knowledge.verdictUnrelated");
    default:
      return t("dataQuality.knowledge.verdictUnspecified");
  }
}

function verdictVariant(
  verdict: QualityInvestigationVerdict
): "success" | "warning" | "neutral" {
  switch (verdict) {
    case QualityInvestigationVerdict.ROOT_CAUSE:
      return "success";
    case QualityInvestigationVerdict.CONTRIBUTING_FACTOR:
      return "warning";
    default:
      return "neutral";
  }
}

function triggerLabel(trigger: QualityInvestigationTrigger): string {
  switch (trigger) {
    case QualityInvestigationTrigger.EXECUTION:
      return t("dataQuality.knowledge.triggerExecution");
    case QualityInvestigationTrigger.ISSUE:
      return t("dataQuality.knowledge.triggerIssue");
    case QualityInvestigationTrigger.SCHEMA_CHANGE:
      return t("dataQuality.knowledge.triggerSchemaChange");
    case QualityInvestigationTrigger.COVERAGE_GAP:
      return t("dataQuality.knowledge.triggerCoverageGap");
    case QualityInvestigationTrigger.CONTRACT_DRIFT:
      return t("dataQuality.knowledge.triggerContractDrift");
    default:
      return t("dataQuality.knowledge.triggerManual");
  }
}

function kindLabel(kind: QualityMemoryKind): string {
  switch (kind) {
    case QualityMemoryKind.ASSET_FACT:
      return t("dataQuality.knowledge.kindAssetFact");
    case QualityMemoryKind.DECISION:
      return t("dataQuality.knowledge.kindDecision");
    default:
      return t("dataQuality.knowledge.kindEvent");
  }
}

function statusLabel(status: QualityMemoryStatus): string {
  switch (status) {
    case QualityMemoryStatus.PROPOSED:
      return t("dataQuality.knowledge.statusProposed");
    case QualityMemoryStatus.CONFIRMED:
      return t("dataQuality.knowledge.statusConfirmed");
    case QualityMemoryStatus.REJECTED:
      return t("dataQuality.knowledge.statusRejected");
    case QualityMemoryStatus.SUPERSEDED:
      return t("dataQuality.knowledge.statusSuperseded");
    case QualityMemoryStatus.EXPIRED:
      return t("dataQuality.knowledge.statusExpired");
    default:
      return t("dataQuality.knowledge.statusUnspecified");
  }
}

function statusVariant(
  status: QualityMemoryStatus
): "success" | "warning" | "neutral" | "danger" {
  switch (status) {
    case QualityMemoryStatus.CONFIRMED:
      return "success";
    case QualityMemoryStatus.PROPOSED:
      return "warning";
    case QualityMemoryStatus.REJECTED:
      return "danger";
    default:
      return "neutral";
  }
}

watch(() => props.targetGuid, load, { immediate: true });
onBeforeUnmount(() => {
  loadGeneration++;
  loadController?.abort();
});
</script>
