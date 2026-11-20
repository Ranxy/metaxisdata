<template>
  <Collapsible
    :open="open"
    class="rounded-md border"
    @update:open="onToggle"
  >
    <CollapsibleTrigger
      class="flex w-full items-start justify-between gap-3 px-3 py-2.5 text-left hover:bg-accent/40"
    >
      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-center gap-2">
          <span class="truncate text-sm font-medium">
            {{ targetTitle }}
          </span>
          <Badge
            v-if="attention.kind === QualityAgentAttentionKind.PROPOSAL_PENDING"
            variant="warning"
          >
            {{ t("qualityAgent.attentionKindProposal") }}
          </Badge>
          <Badge
            v-else-if="attention.kind === QualityAgentAttentionKind.BLOCKED"
            variant="danger"
          >
            {{ t("qualityAgent.attentionKindBlocked") }}
          </Badge>
          <Badge
            v-else-if="attention.kind === QualityAgentAttentionKind.REPORT_ANOMALY"
            variant="info"
          >
            {{ t("qualityAgent.attentionKindReport") }}
          </Badge>
          <Badge
            v-if="!attention.target"
            variant="neutral"
          >
            {{
              isAgentLevel
                ? t("qualityAgent.attentionAgentLevel")
                : t("qualityAgent.attentionTargetUnknown")
            }}
          </Badge>
          <span
            v-if="attention.createTime"
            class="text-[11px] text-muted-foreground"
            :title="formatDateTime(attention.createTime, locale)"
          >
            {{ formatRelativeTime(attention.createTime, locale) }}
          </span>
        </div>
        <p
          v-if="targetScope"
          class="mt-0.5 truncate text-xs text-muted-foreground"
        >
          {{ targetScope }}
        </p>
        <p
          v-if="!open"
          class="mt-1 line-clamp-2 whitespace-pre-wrap break-words text-xs text-muted-foreground"
        >
          {{ attention.question }}
        </p>
      </div>
      <ChevronDown
        class="mt-1 h-4 w-4 shrink-0 text-muted-foreground transition-transform"
        :class="{ 'rotate-180': open }"
      />
    </CollapsibleTrigger>

    <CollapsibleContent class="space-y-3 border-t px-3 py-3">
      <p class="whitespace-pre-wrap break-words text-sm text-muted-foreground">
        {{ attention.question }}
      </p>
      <div v-if="attention.kind === QualityAgentAttentionKind.PROPOSAL_PENDING">
        <p class="text-xs font-medium text-muted-foreground">
          {{ t("qualityAgent.attentionDraftsTitle") }}
        </p>
        <ul
          v-if="attention.pendingDraftTitles.length > 0"
          class="mt-1 list-disc space-y-1 pl-5 text-xs text-muted-foreground"
        >
          <li
            v-for="title in attention.pendingDraftTitles"
            :key="title"
            class="break-words"
          >
            {{ title }}
          </li>
        </ul>
        <p
          v-else
          class="mt-1 text-xs text-muted-foreground"
        >
          {{ t("qualityAgent.attentionDraftsEmpty") }}
        </p>
        <p
          v-if="attention.pendingDraftTitles.length >= ATTENTION_DRAFT_PREVIEW"
          class="mt-1 text-[11px] text-muted-foreground"
        >
          {{ t("qualityAgent.attentionDraftsMore") }}
        </p>
        <div
          v-if="canAdoptPending"
          class="mt-2 space-y-1"
        >
          <Button
            size="sm"
            :disabled="adoptDisabled"
            @click="requestAdopt"
          >
            <Loader2
              v-if="adoptDisabled"
              class="mr-2 h-4 w-4 animate-spin"
            />
            {{ t("qualityAgent.attentionAdoptPending") }}
          </Button>
          <p class="text-[11px] text-muted-foreground">
            {{ t("qualityAgent.attentionAdoptHint") }}
          </p>
        </div>
      </div>

      <div class="flex flex-wrap gap-2">
        <Button
          v-if="targetRoute"
          variant="outline"
          size="sm"
          as-child
        >
          <RouterLink :to="targetRoute">
            {{ t("qualityAgent.attentionOpenTarget") }}
          </RouterLink>
        </Button>
        <Button
          v-if="sessionRoute"
          variant="outline"
          size="sm"
          as-child
        >
          <RouterLink :to="sessionRoute">
            {{ t("qualityAgent.attentionOpenSession") }}
          </RouterLink>
        </Button>
      </div>

      <template v-if="canReplyToTarget">
        <div class="space-y-1">
          <p class="text-xs font-medium text-muted-foreground">
            {{ t("qualityAgent.attentionReplyLabel") }}
          </p>
          <textarea
            v-model="replyBody"
            rows="3"
            class="w-full resize-none rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
            :placeholder="t('qualityAgent.attentionReplyPlaceholder')"
            :disabled="busy"
          />
        </div>
        <p
          v-if="error"
          class="text-xs text-destructive"
        >
          {{ error }}
        </p>
        <div class="flex flex-wrap justify-end gap-2">
          <Button
            variant="outline"
            size="sm"
            :disabled="busy"
            @click="dismiss"
          >
            {{ t("qualityAgent.attentionDismiss") }}
          </Button>
          <Button
            size="sm"
            :disabled="busy || replyBody.trim() === ''"
            @click="sendReply"
          >
            <Loader2
              v-if="busy"
              class="mr-2 h-4 w-4 animate-spin"
            />
            {{ t("qualityAgent.attentionReplySend") }}
          </Button>
        </div>
      </template>
      <template v-else-if="!canReply">
        <p class="text-xs text-muted-foreground">
          {{ t("qualityAgent.attentionReadOnly") }}
        </p>
      </template>
      <div
        v-else
        class="flex flex-wrap items-center justify-between gap-2"
      >
        <p class="text-xs text-muted-foreground">
          {{ t("qualityAgent.attentionNoSession") }}
        </p>
        <Button
          variant="outline"
          size="sm"
          :disabled="busy"
          @click="dismiss"
        >
          {{ t("qualityAgent.attentionDismiss") }}
        </Button>
      </div>
    </CollapsibleContent>
  </Collapsible>
</template>

<script setup lang="ts">
import { ChevronDown, Loader2 } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import {
  dismissQualityAgentAttention,
  replyQualityAgentAttention,
} from "@/api/data-quality";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import {
  type QualityAgentAttention,
  QualityAgentAttentionKind,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { targetScopeLabel } from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";
import { qualityAgentSessionRoute } from "@/utils/qualityAgent";

const ATTENTION_DRAFT_PREVIEW = 5;

const props = defineProps<{
  attention: QualityAgentAttention;
  agentName: string;
  canReply: boolean;
  /** The viewer may run an adoption pass; the parent owns the RPC. */
  canAdopt?: boolean;
  /** The parent is already running an adoption pass. */
  adoptBusy?: boolean;
}>();

const emit = defineEmits<{
  resolved: [name: string];
  adopt: [];
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const open = ref(false);
const replyBody = ref("");
const busy = ref(false);
const error = ref("");

const isAgentLevel = computed(
  () => props.attention.kind === QualityAgentAttentionKind.REPORT_ANOMALY
);

const targetTitle = computed(
  () =>
    props.attention.target?.path?.object ||
    props.attention.target?.metaGuid ||
    (props.attention.target
      ? props.attention.title
      : t("qualityAgent.attentionTargetUnknown"))
);

const targetScope = computed(() => {
  const target = props.attention.target;
  if (!target) {
    return "";
  }
  return targetScopeLabel(target.path);
});

const targetRoute = computed(() => {
  const target = props.attention.target;
  if (!target?.metaGuid) {
    return undefined;
  }
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.metaGuid) },
    query: { metaType: String(target.metaType ?? MetaType.UNSPECIFIED) },
  };
});

const sessionRoute = computed(() =>
  props.attention.session
    ? qualityAgentSessionRoute(props.agentName, props.attention.session)
    : undefined
);

const canReplyToTarget = computed(
  () => props.canReply && props.attention.session !== ""
);

const canAdoptPending = computed(
  () =>
    props.canAdopt === true &&
    props.attention.kind === QualityAgentAttentionKind.PROPOSAL_PENDING
);

const adoptDisabled = computed(() => busy.value || props.adoptBusy === true);

function onToggle(next: boolean) {
  open.value = next;
}

/** The parent runs the adoption pass and reloads this card's list. */
function requestAdopt() {
  if (adoptDisabled.value) {
    return;
  }
  emit("adopt");
}

async function sendReply() {
  const body = replyBody.value.trim();
  if (body === "" || busy.value) {
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    await replyQualityAgentAttention(props.attention.name, body, {
      requestId: `attention-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`,
    });
    notify.success(t("qualityAgent.attentionSent"));
    emit("resolved", props.attention.name);
  } catch (err) {
    error.value = formatError(err);
  } finally {
    busy.value = false;
  }
}

async function dismiss() {
  if (busy.value) {
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    await dismissQualityAgentAttention(props.attention.name);
    notify.success(t("qualityAgent.attentionDismissed"));
    emit("resolved", props.attention.name);
  } catch (err) {
    error.value = formatError(err);
  } finally {
    busy.value = false;
  }
}
</script>
