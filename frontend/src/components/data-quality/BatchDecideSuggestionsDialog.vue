<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>{{ title }}</DialogTitle>
        <DialogDescription>{{ description }}</DialogDescription>
      </DialogHeader>

      <div class="space-y-4">
        <Alert
          v-if="error"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>{{ t("dataQuality.suggestions.batchDecide.failed") }}</AlertTitle>
          <AlertDescription class="break-words">
            {{ error }}
          </AlertDescription>
        </Alert>

        <Alert
          v-if="summary"
          :variant="summary.failed.length > 0 ? 'warning' : 'success'"
        >
          <AlertTriangle
            v-if="summary.failed.length > 0"
            class="h-4 w-4"
          />
          <AlertTitle>
            {{
              summary.failed.length > 0
                ? t("dataQuality.suggestions.batchDecide.partial", {
                    succeeded: summary.succeeded,
                    failed: summary.failed.length,
                  })
                : t("dataQuality.suggestions.batchDecide.success", {
                    count: summary.succeeded,
                  })
            }}
          </AlertTitle>
        </Alert>

        <div
          v-if="summary && summary.failed.length > 0"
          class="space-y-2"
        >
          <div class="text-sm font-medium">
            {{ t("dataQuality.suggestions.batchDecide.failures") }}
          </div>
          <ul class="space-y-1 text-sm">
            <li
              v-for="failure in summary.failed"
              :key="failure.index"
              class="break-words"
            >
              <span class="font-medium">{{ suggestionLabel(failure.index) }}</span>
              <span class="text-muted-foreground">— {{ failure.message }}</span>
            </li>
          </ul>
        </div>

        <div
          v-if="!summary"
          class="space-y-1.5"
        >
          <Label for="dq-batch-decide-note">
            {{ t("dataQuality.suggestions.note") }}
          </Label>
          <textarea
            id="dq-batch-decide-note"
            v-model="note"
            class="min-h-20 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
            :placeholder="t('dataQuality.suggestions.notePlaceholder')"
          />
        </div>
      </div>

      <DialogFooter>
        <Button
          variant="outline"
          :disabled="updating"
          @click="close"
        >
          {{ t(summary ? "common.close" : "common.cancel") }}
        </Button>
        <Button
          v-if="!summary"
          :disabled="!canSubmit"
          :variant="decision === QualitySuggestionStatus.REJECTED ? 'destructive' : 'default'"
          @click="submit"
        >
          {{
            updating
              ? t("dataQuality.suggestions.batchDecide.submitting")
              : decision === QualitySuggestionStatus.ACCEPTED
                ? t("dataQuality.suggestions.accept")
                : t("dataQuality.suggestions.reject")
          }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { AlertTriangle } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { batchUpdateQualitySuggestions } from "@/api/data-quality";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
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
import { useErrorMessage } from "@/composables/useErrorHandler";
import {
  type BatchUpdateQualitySuggestionResult,
  type QualitySuggestion,
  QualitySuggestionKind,
  QualitySuggestionStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  labelSuggestionOf,
  MAX_SUGGESTION_BATCH,
  ruleDraftOf,
  semanticTypeKey,
  summarizeBatchResults,
} from "@/utils/dataQualitySuggestion";

/**
 * Accept or reject several suggestions in one call. Every item is decided
 * independently, so an already-decided row is reported on its own instead of
 * failing the batch. Accepting a label writes the semantic label immediately;
 * accepting a draft here is the reviewer's decision, not rule creation.
 */

const props = withDefaults(
  defineProps<{
    open: boolean;
    suggestions?: QualitySuggestion[];
    decision?: QualitySuggestionStatus;
  }>(),
  {
    suggestions: () => [],
    decision: QualitySuggestionStatus.REJECTED,
  }
);

const emit = defineEmits<{
  "update:open": [value: boolean];
  /** The suggestions a decision was actually written for. */
  done: [suggestionNames: string[]];
}>();

const { t } = useI18n();
const { formatError } = useErrorMessage();

const note = ref("");
const updating = ref(false);
const error = ref("");
const decidedNames = ref<string[]>([]);
const results = ref<BatchUpdateQualitySuggestionResult[]>([]);

const accepted = computed(
  () => props.decision === QualitySuggestionStatus.ACCEPTED
);
const labelCount = computed(
  () =>
    props.suggestions.filter(
      (suggestion) => suggestion.kind === QualitySuggestionKind.LABEL
    ).length
);
const draftCount = computed(
  () =>
    props.suggestions.filter(
      (suggestion) => suggestion.kind === QualitySuggestionKind.RULE_DRAFT
    ).length
);

const title = computed(() =>
  accepted.value
    ? t("dataQuality.suggestions.batchDecide.acceptTitle", {
        count: props.suggestions.length,
      })
    : t("dataQuality.suggestions.batchDecide.rejectTitle", {
        count: props.suggestions.length,
      })
);

const description = computed(() => {
  const notes: string[] = [];
  if (accepted.value) {
    if (labelCount.value > 0) {
      notes.push(t("dataQuality.suggestions.batchDecide.acceptLabelsHint"));
    }
    if (draftCount.value > 0) {
      notes.push(t("dataQuality.suggestions.batchDecide.acceptDraftsHint"));
    }
  } else {
    notes.push(t("dataQuality.suggestions.batchDecide.rejectHint"));
  }
  return notes.join(" ");
});

const summary = computed(() =>
  results.value.length > 0 ? summarizeBatchResults(results.value) : undefined
);

const canSubmit = computed(
  () => !updating.value && props.suggestions.length > 0
);

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      reset();
    }
  },
  { immediate: true }
);

function reset() {
  note.value = "";
  updating.value = false;
  error.value = "";
  decidedNames.value = [];
  results.value = [];
}

function suggestionLabel(index: number): string {
  const echoed = results.value[index]?.name;
  const suggestion = echoed
    ? props.suggestions.find((item) => item.name === echoed)
    : props.suggestions[index];
  if (!suggestion) {
    return "-";
  }
  const draft = ruleDraftOf(suggestion);
  if (draft?.title) {
    return draft.title;
  }
  const label = labelSuggestionOf(suggestion);
  if (label) {
    return `${label.column} · ${t(semanticTypeKey(label.label))}`;
  }
  return suggestion.name.split("/").pop() ?? suggestion.name;
}

function submit() {
  if (!canSubmit.value) {
    return;
  }
  const batch = props.suggestions.slice(0, MAX_SUGGESTION_BATCH);
  updating.value = true;
  error.value = "";
  results.value = [];
  batchUpdateQualitySuggestions(
    batch.map((suggestion) => ({
      name: suggestion.name,
      decision: props.decision,
      note: note.value.trim(),
    }))
  )
    .then((created) => {
      results.value = created;
      // The server echoes each request's name; fall back to request order only
      // if a result is missing it.
      decidedNames.value = created.flatMap((result, index) => {
        if (result.error) {
          return [];
        }
        const name = result.name || batch[index]?.name;
        return name ? [name] : [];
      });
      // The page can refresh behind the dialog; the per-item failures stay here.
      if (decidedNames.value.length > 0) {
        emit("done", [...decidedNames.value]);
      }
    })
    .catch((e: unknown) => {
      error.value = formatError(
        e,
        "dataQuality.suggestions.batchDecide.failed"
      );
    })
    .finally(() => {
      updating.value = false;
    });
}

function onOpenChange(value: boolean) {
  if (value) {
    return;
  }
  if (updating.value) {
    return;
  }
  close();
}

function close() {
  emit("update:open", false);
}
</script>
