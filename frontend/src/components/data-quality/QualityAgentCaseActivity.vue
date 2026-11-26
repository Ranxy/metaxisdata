<template>
  <div class="space-y-2">
    <p
      v-if="loading && actions.length === 0"
      class="text-sm text-muted-foreground"
    >
      {{ t("common.loading") }}
    </p>
    <p
      v-else-if="error"
      class="text-sm text-destructive"
    >
      {{ error }}
    </p>
    <p
      v-else-if="actions.length === 0"
      class="text-sm text-muted-foreground"
    >
      {{ emptyText }}
    </p>
    <ul
      v-else
      class="space-y-2"
    >
      <li
        v-for="action in actions"
        :key="action.name"
        class="rounded-md border px-3 py-2 text-sm"
      >
        <div class="flex flex-wrap items-center gap-2">
          <Badge :variant="actionVariant(action.action)">
            {{ t(actionKindKey(action.action)) }}
          </Badge>
          <span
            v-if="action.check"
            class="font-mono text-xs"
          >
            {{ checkName(action.check) }}
          </span>
          <span
            v-if="action.field"
            class="font-mono text-xs text-muted-foreground"
          >
            {{ action.field }}
          </span>
          <span
            v-if="revisionText(action)"
            class="text-xs text-muted-foreground"
          >
            {{ revisionText(action) }}
          </span>
          <span
            v-if="action.class"
            class="text-xs uppercase text-muted-foreground"
          >
            {{ action.class }}
          </span>
          <span
            class="ml-auto text-xs text-muted-foreground"
            :title="formatDateTime(action.createTime, locale)"
          >
            {{ formatRelativeTime(action.createTime, locale) }}
          </span>
        </div>
        <div
          v-if="action.reason || action.execution"
          class="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground"
        >
          <span
            v-if="action.reason"
            class="wrap-anywhere"
          >
            {{ action.reason }}
          </span>
          <RouterLink
            v-if="action.execution"
            :to="executionRoute(action.execution)"
            class="hover:underline"
          >
            {{ t("qualityAgent.activity.openExecution") }}
          </RouterLink>
        </div>
      </li>
    </ul>
    <Button
      v-if="nextPageToken"
      variant="outline"
      size="sm"
      :disabled="loading"
      @click="loadMore"
    >
      {{ t("qualityAgent.activity.loadMore") }}
    </Button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { listQualityAgentCaseActions } from "@/api/data-quality";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useErrorMessage } from "@/composables/useErrorHandler";
import {
  type QualityAgentCaseAction,
  QualityAgentCaseActionKind,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

const props = defineProps<{
  agentName: string;
  /** Narrow the history to one work item; omitted reads the agent's whole activity. */
  caseName?: string;
  /** The sentence an empty list shows, so a case view and the activity view can differ. */
  empty?: string;
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const actions = ref<QualityAgentCaseAction[]>([]);
const nextPageToken = ref("");
const loading = ref(false);
const error = ref("");
const emptyText = computed(
  () => props.empty ?? t("qualityAgent.activity.empty")
);

/** actionKindKey names one kind of write for a person. */
function actionKindKey(kind: QualityAgentCaseActionKind): string {
  switch (kind) {
    case QualityAgentCaseActionKind.REVISE:
      return "qualityAgent.activity.kind.revise";
    case QualityAgentCaseActionKind.CONTROL:
      return "qualityAgent.activity.kind.control";
    case QualityAgentCaseActionKind.RUN:
      return "qualityAgent.activity.kind.run";
    case QualityAgentCaseActionKind.PROPOSE:
      return "qualityAgent.activity.kind.propose";
    default:
      return "qualityAgent.activity.kind.unknown";
  }
}

/** actionVariant keeps a proposal visually apart from a write that changed a check. */
function actionVariant(
  kind: QualityAgentCaseActionKind
): "default" | "secondary" | "outline" {
  switch (kind) {
    case QualityAgentCaseActionKind.REVISE:
      return "default";
    case QualityAgentCaseActionKind.CONTROL:
      return "secondary";
    default:
      return "outline";
  }
}

/** checkName is the rule's resource id, which is what the ledger row stores. */
function checkName(check: string): string {
  const index = check.lastIndexOf("/");
  return index >= 0 ? check.slice(index + 1) : check;
}

/** revisionText shows the definition movement, and says nothing when there was none. */
function revisionText(action: QualityAgentCaseAction): string {
  if (action.toRevision === 0) {
    return "";
  }
  if (action.fromRevision === action.toRevision) {
    return t("qualityAgent.activity.revision", {
      revision: action.toRevision,
    });
  }
  return t("qualityAgent.activity.revisionChange", {
    from: action.fromRevision,
    to: action.toRevision,
  });
}

/** executionRoute opens one execution's detail page from its resource name. */
function executionRoute(name: string) {
  return {
    name: "DataQualityExecutionDetail",
    params: { executionId: name.split("/") },
  };
}

async function load(reset: boolean) {
  if (loading.value) {
    return;
  }
  loading.value = true;
  error.value = "";
  if (reset) {
    actions.value = [];
    nextPageToken.value = "";
  }
  try {
    const page = await listQualityAgentCaseActions(props.agentName, {
      caseName: props.caseName,
      pageToken: nextPageToken.value,
    });
    actions.value = reset ? page.actions : [...actions.value, ...page.actions];
    nextPageToken.value = page.nextPageToken;
  } catch (err) {
    error.value = formatError(err);
  } finally {
    loading.value = false;
  }
}

function loadMore() {
  void load(false);
}

watch(
  () => [props.agentName, props.caseName],
  () => void load(true),
  { immediate: true }
);
</script>
