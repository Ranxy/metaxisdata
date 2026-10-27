<template>
  <div
    class="rounded-md border border-primary/30 bg-primary/5 p-3"
    :class="
      highlight
        ? 'animate-in fade-in ring-2 ring-primary/60'
        : 'ring-1 ring-primary/40'
    "
  >
    <div class="flex flex-wrap items-start justify-between gap-2">
      <div class="min-w-0 flex-1 space-y-1">
        <div class="flex flex-wrap items-center gap-x-1.5 gap-y-1">
          <Sparkles class="h-3.5 w-3.5 shrink-0 text-primary" />
          <Badge variant="info">
            {{ t("dataQuality.workspace.draftBadge") }}
          </Badge>
          <span class="min-w-0 truncate text-sm font-medium">
            {{ title }}
          </span>
          <Badge
            v-if="typeKey"
            variant="secondary"
          >
            {{ t(typeKey) }}
          </Badge>
          <Badge
            v-if="severity !== undefined"
            :variant="qualitySeverityVariant(severity)"
          >
            {{ t(qualitySeverityKey(severity)) }}
          </Badge>
        </div>
        <p
          v-if="rationale"
          class="line-clamp-2 text-xs text-muted-foreground"
        >
          {{ rationale }}
        </p>
        <div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <span
            v-if="column"
            class="font-mono"
          >
            {{ t("dataQuality.workspace.draftColumn", { column }) }}
          </span>
          <span>{{ t("dataQuality.workspace.draftConfidence", { value: confidence }) }}</span>
        </div>
      </div>

      <div class="flex shrink-0 flex-wrap items-center gap-1.5">
        <Button
          variant="ghost"
          size="sm"
          :disabled="!canSuggest || busy"
          @click="ignore"
        >
          {{ t("dataQuality.workspace.draftIgnore") }}
        </Button>
        <Button
          v-if="canEdit"
          variant="outline"
          size="sm"
          @click="emit('edit')"
        >
          {{ t("dataQuality.workspace.draftEdit") }}
        </Button>
        <Button
          v-else
          variant="outline"
          size="sm"
          disabled
          :title="
            canPrefill
              ? t('dataQuality.suggestions.noCreatePermission')
              : t('dataQuality.workspace.draftCannotPrefill')
          "
        >
          {{ t("dataQuality.workspace.draftEdit") }}
        </Button>
        <Button
          size="sm"
          :disabled="!canCreate || !canBuild || busy"
          :title="t('dataQuality.workspace.draftAcceptHint')"
          @click="accept"
        >
          {{ t("dataQuality.workspace.draftAccept") }}
        </Button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Sparkles } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { createQualityRule, updateQualitySuggestion } from "@/api/data-quality";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import {
  type QualityRule,
  QualitySeverity,
  type QualitySuggestion,
  QualitySuggestionStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  qualityRuleTypeKey,
  qualitySeverityKey,
  qualitySeverityVariant,
} from "@/utils/dataQuality";
import {
  buildRuleFromSuggestion,
  formatConfidence,
  ruleDraftOf,
  suggestionRationale,
  suggestionToEditorPrefill,
} from "@/utils/dataQualitySuggestion";
import { draftColumn } from "@/utils/dataQualityTarget";

/**
 * One AI-drafted rule, rendered inline in the section it would join.
 *
 * The draft is a proposal, not a rule: creating it accepts the suggestion
 * server-side, ignoring it only records the decision, and adjusting it hands the
 * suggestion to the workspace composer so its typed config is reviewed in the
 * same place every other check is authored. The card keeps its own busy state so
 * one slow decision cannot fire twice or freeze its siblings.
 */

const props = withDefaults(
  defineProps<{
    suggestion: QualitySuggestion;
    canCreate: boolean;
    canSuggest: boolean;
    /** True for a draft the latest copilot pass just returned. */
    highlight?: boolean;
  }>(),
  { highlight: false }
);

const emit = defineEmits<{
  accepted: [rule: QualityRule];
  rejected: [name: string];
  edit: [];
  /** A failed decision leaves the card's data stale; the page re-reads the queue. */
  refresh: [];
}>();

const { t } = useI18n();
const { formatError } = useErrorMessage();

const busy = ref(false);

const draft = computed(() => ruleDraftOf(props.suggestion));
const prefill = computed(() => suggestionToEditorPrefill(props.suggestion));

const title = computed(
  () =>
    draft.value?.title ||
    props.suggestion.name.split("/").pop() ||
    props.suggestion.name
);
const typeKey = computed(() =>
  prefill.value ? qualityRuleTypeKey(prefill.value.ruleType) : ""
);
const severity = computed<QualitySeverity | undefined>(() => {
  const value = draft.value?.severity;
  return value === undefined || value === QualitySeverity.UNSPECIFIED
    ? undefined
    : value;
});
const confidence = computed(() =>
  formatConfidence(draft.value?.confidence ?? 0)
);
const rationale = computed(() => suggestionRationale(props.suggestion));
const column = computed(() => draftColumn(props.suggestion));

/** The create payload, or undefined when this build cannot compile the draft. */
const canBuild = computed(
  () => buildRuleFromSuggestion({ suggestion: props.suggestion }) !== undefined
);
const canPrefill = computed(() => prefill.value !== undefined);
const canEdit = computed(() => props.canCreate && canPrefill.value);

async function accept() {
  if (busy.value) {
    return;
  }
  const rule = buildRuleFromSuggestion({ suggestion: props.suggestion });
  if (!rule) {
    return;
  }
  busy.value = true;
  try {
    // One call creates the rule and accepts the suggestion: the server applies
    // the decision as a side effect, so no second round trip can half-commit.
    const created = await createQualityRule(rule, props.suggestion.name);
    notify.success(t("dataQuality.workspace.draftCreated"));
    emit("accepted", created);
  } catch (error: unknown) {
    notify.error(formatError(error, "dataQuality.workspace.draftCreateFailed"));
    emit("refresh");
  } finally {
    busy.value = false;
  }
}

async function ignore() {
  if (busy.value) {
    return;
  }
  busy.value = true;
  try {
    await updateQualitySuggestion({
      name: props.suggestion.name,
      decision: QualitySuggestionStatus.REJECTED,
    });
    notify.success(t("dataQuality.workspace.draftIgnored"));
    emit("rejected", props.suggestion.name);
  } catch (error: unknown) {
    notify.error(
      formatError(error, "dataQuality.workspace.draftRejectedFailed")
    );
    emit("refresh");
  } finally {
    busy.value = false;
  }
}
</script>
