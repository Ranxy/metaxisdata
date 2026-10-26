<template>
  <Dialog
    :open="open"
    @update:open="(value: boolean) => emit('update:open', value)"
  >
    <DialogContent
      class="left-auto right-0 top-0 h-dvh max-h-dvh w-full max-w-2xl translate-x-0 translate-y-0 gap-0 overflow-hidden rounded-none border-l p-0 sm:max-w-2xl sm:rounded-none"
    >
      <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <div class="flex flex-wrap items-center gap-2">
            <Badge
              v-if="suggestion"
              variant="outline"
            >
              {{ t(suggestionKindKey(suggestion.kind)) }}
            </Badge>
            <Badge
              v-if="suggestion"
              :variant="suggestionStatusVariant(suggestion.status)"
            >
              {{ t(suggestionStatusKey(suggestion.status)) }}
            </Badge>
          </div>
          <DialogTitle class="truncate text-xl">
            {{ t("dataQuality.suggestions.detailTitle") }}
          </DialogTitle>
          <DialogDescription class="truncate">
            {{ displayTarget }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-5 overflow-auto px-6 py-5">
          <template v-if="suggestion">
            <p
              v-if="error"
              class="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm break-words text-destructive"
            >
              {{ error }}
            </p>

            <div class="grid gap-3 sm:grid-cols-2">
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.suggestions.source") }}
                </div>
                <div class="text-sm">
                  {{ t(suggestionSourceKey(suggestion.source)) }}
                </div>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.suggestions.provider") }}
                </div>
                <div class="text-sm">
                  {{ [suggestion.provider, suggestion.model].filter(Boolean).join(" · ") || "-" }}
                </div>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.suggestions.createdAt") }}
                </div>
                <div
                  class="text-sm"
                  :title="formatDateTime(suggestion.createTime, locale)"
                >
                  {{ formatRelativeTime(suggestion.createTime, locale) }}
                </div>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.suggestions.promptVersion") }}
                </div>
                <div class="text-sm">{{ suggestion.promptVersion || "-" }}</div>
              </div>
            </div>

            <!-- A label suggestion: the label, its confidence and the evidence. -->
            <section
              v-if="label"
              class="space-y-3"
            >
              <div class="flex flex-wrap items-center gap-2">
                <Badge variant="secondary">
                  {{ t(semanticTypeKey(label.label)) }}
                </Badge>
                <span class="text-sm">
                  {{ t("dataQuality.suggestions.column") }}:
                  <span class="font-mono">{{ label.column }}</span>
                </span>
                <span class="text-sm text-muted-foreground">
                  {{ t("dataQuality.suggestions.confidence") }}
                  {{ formatConfidence(label.confidence) }}
                </span>
              </div>
              <div
                v-if="evidenceLines.length > 0"
                class="space-y-1"
              >
                <div class="text-xs font-medium text-muted-foreground">
                  {{ t("dataQuality.suggestions.evidence") }}
                </div>
                <ul class="list-disc space-y-1 pl-5 text-sm">
                  <li
                    v-for="line in evidenceLines"
                    :key="line"
                  >
                    {{ line }}
                  </li>
                </ul>
              </div>
              <div v-if="label.suggestedRuleTypes.length > 0">
                <div class="text-xs font-medium text-muted-foreground">
                  {{ t("dataQuality.suggestions.suggestedRuleTypes") }}
                </div>
                <div class="mt-1 flex flex-wrap gap-1">
                  <Badge
                    v-for="ruleType in label.suggestedRuleTypes"
                    :key="ruleType"
                    variant="outline"
                  >
                    {{ t(qualityRuleTypeKey(ruleType)) }}
                  </Badge>
                </div>
              </div>
            </section>

            <!-- A rule draft: the type, rationale and the parameters it would set. -->
            <section
              v-else-if="draft"
              class="space-y-3"
            >
              <div class="flex flex-wrap items-center gap-2">
                <Badge
                  v-if="prefill"
                  variant="secondary"
                >
                  {{ t(qualityRuleTypeKey(prefill.ruleType)) }}
                </Badge>
                <Badge :variant="qualitySeverityVariant(draft.severity)">
                  {{ t(qualitySeverityKey(draft.severity)) }}
                </Badge>
                <span class="text-sm text-muted-foreground">
                  {{ t("dataQuality.suggestions.confidence") }}
                  {{ formatConfidence(draft.confidence) }}
                </span>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.editor.title") }}
                </div>
                <div class="text-sm font-medium">{{ draft.title || "-" }}</div>
              </div>
              <div v-if="draft.rationale">
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.suggestions.rationale") }}
                </div>
                <p class="text-sm break-words">
                  {{ draft.rationale }}
                </p>
              </div>
              <div v-if="configEntries.length > 0">
                <div class="text-xs font-medium text-muted-foreground">
                  {{ t("dataQuality.suggestions.parameters") }}
                </div>
                <dl class="mt-1 grid gap-x-4 gap-y-1 text-sm sm:grid-cols-2">
                  <template
                    v-for="entry in configEntries"
                    :key="entry.field"
                  >
                    <dt class="text-muted-foreground">
                      {{ t(configEntryLabelKey(entry.field)) }}
                    </dt>
                    <dd class="font-mono break-all">
                      {{ entry.value }}
                    </dd>
                  </template>
                </dl>
              </div>
            </section>

            <section
              v-if="suggestion.status === QualitySuggestionStatus.PENDING && canDecide"
              class="space-y-2 border-t pt-4"
            >
              <div
                v-if="!draft"
                class="space-y-1.5"
              >
                <Label for="dq-suggestion-note">
                  {{ t("dataQuality.suggestions.note") }}
                </Label>
                <textarea
                  id="dq-suggestion-note"
                  v-model="note"
                  class="min-h-20 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                  :placeholder="t('dataQuality.suggestions.notePlaceholder')"
                />
              </div>
              <p
                v-if="draft && !canCreateRule"
                class="text-xs text-muted-foreground"
              >
                {{ t("dataQuality.suggestions.noCreatePermission") }}
              </p>
              <div class="flex flex-wrap justify-end gap-2">
                <Button
                  variant="outline"
                  :disabled="deciding"
                  @click="emit('reject', note)"
                >
                  {{ t("dataQuality.suggestions.reject") }}
                </Button>
                <template v-if="draft">
                  <Button
                    variant="outline"
                    :disabled="deciding || !canCreateRule"
                    @click="emit('openTarget')"
                  >
                    {{ t("dataQuality.targets.openWorkspace") }}
                  </Button>
                  <Button
                    :disabled="deciding || !canCreateRule"
                    @click="emit('createRule')"
                  >
                    {{ t("dataQuality.suggestions.createRule") }}
                  </Button>
                </template>
                <Button
                  v-else
                  :disabled="deciding"
                  @click="emit('accept', note)"
                >
                  {{ t("dataQuality.suggestions.accept") }}
                </Button>
              </div>
            </section>

            <section
              v-else-if="suggestion.status === QualitySuggestionStatus.SUPERSEDED"
              class="space-y-2 border-t pt-4"
            >
              <div class="flex flex-wrap items-center gap-2 text-sm">
                <span class="text-muted-foreground">
                  {{ t("dataQuality.suggestions.supersededByServer") }}
                </span>
                <span class="text-muted-foreground">
                  {{ t("dataQuality.suggestions.closedAt") }}
                </span>
                <span :title="formatDateTime(suggestion.decideTime, locale)">
                  {{ formatRelativeTime(suggestion.decideTime, locale) }}
                </span>
              </div>
              <p
                v-if="suggestion.decisionNote"
                class="text-sm break-words text-muted-foreground"
              >
                {{ suggestion.decisionNote }}
              </p>
            </section>
            <section
              v-else-if="suggestion.status !== QualitySuggestionStatus.PENDING"
              class="space-y-2 border-t pt-4"
            >
              <div class="flex flex-wrap items-center gap-2 text-sm">
                <span class="text-muted-foreground">
                  {{ t("dataQuality.suggestions.decidedBy") }}
                </span>
                <span>{{ principalLabel(suggestion.decidedBy) }}</span>
                <span class="text-muted-foreground">
                  {{ t("dataQuality.suggestions.decidedAt") }}
                </span>
                <span :title="formatDateTime(suggestion.decideTime, locale)">
                  {{ formatRelativeTime(suggestion.decideTime, locale) }}
                </span>
              </div>
              <p
                v-if="suggestion.decisionNote"
                class="text-sm break-words text-muted-foreground"
              >
                {{ suggestion.decisionNote }}
              </p>
            </section>
          </template>
        </div>

        <DialogFooter class="shrink-0 border-t px-6 py-4">
          <Button
            variant="outline"
            @click="emit('update:open', false)"
          >
            {{ t("common.close") }}
          </Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  type QualitySuggestion,
  QualitySuggestionStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  qualityRuleTypeKey,
  qualitySeverityKey,
  qualitySeverityVariant,
} from "@/utils/dataQuality";
import {
  configEntryLabelKey,
  formatConfidence,
  labelEvidenceLines,
  labelSuggestionOf,
  ruleConfigEntries,
  ruleDraftOf,
  semanticTypeKey,
  suggestionKindKey,
  suggestionSourceKey,
  suggestionStatusKey,
  suggestionStatusVariant,
  suggestionTargetLabel,
  suggestionToEditorPrefill,
} from "@/utils/dataQualitySuggestion";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * One suggestion, reviewed in place: the evidence for a label or the parameters
 * of a rule draft, and the accept/reject decision. It is a side sheet so the
 * queue behind it keeps its scroll position and filters.
 */

const props = defineProps<{
  open: boolean;
  suggestion?: QualitySuggestion;
  canDecide: boolean;
  canCreateRule: boolean;
  deciding: boolean;
  error: string;
  /** The page's display label for the target, with the instance title. */
  targetLabel?: string;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  accept: [note: string];
  reject: [note: string];
  /** Open the suggestion's target workspace, where its draft is listed inline. */
  openTarget: [];
  createRule: [];
}>();

const { t, locale } = useI18n();

const note = ref("");

const label = computed(() =>
  props.suggestion ? labelSuggestionOf(props.suggestion) : undefined
);
const draft = computed(() =>
  props.suggestion ? ruleDraftOf(props.suggestion) : undefined
);
const prefill = computed(() =>
  props.suggestion ? suggestionToEditorPrefill(props.suggestion) : undefined
);
const configEntries = computed(() =>
  prefill.value ? ruleConfigEntries(prefill.value.form.config) : []
);
const evidenceLines = computed(() =>
  label.value ? labelEvidenceLines(label.value.evidence) : []
);
const displayTarget = computed(() => {
  if (props.targetLabel) {
    return props.targetLabel;
  }
  const guid = props.suggestion?.target?.metaGuid;
  return guid ? suggestionTargetLabel(guid) : "";
});

function principalLabel(name: string): string {
  return name ? (name.split("/").pop() ?? name) : "-";
}

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      note.value = "";
    }
  }
);
</script>
