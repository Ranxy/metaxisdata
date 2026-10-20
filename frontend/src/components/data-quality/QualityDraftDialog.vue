<template>
  <Dialog
    :open="open"
    @update:open="(value: boolean) => emit('update:open', value)"
  >
    <DialogContent class="max-h-[85vh] max-w-2xl gap-0 overflow-hidden p-0">
      <div class="flex max-h-[85vh] min-h-0 flex-col">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>{{ t("dataQuality.llm.draft.title") }}</DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.llm.draft.description") }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-5 overflow-auto px-6 py-5">
          <div class="space-y-1.5">
            <div class="flex items-center justify-between gap-2">
              <Label for="dq-ai-intent">
                {{ t("dataQuality.llm.draft.intent") }}
              </Label>
              <span class="text-xs text-muted-foreground">{{ intent.length }} / 2000</span>
            </div>
            <textarea
              id="dq-ai-intent"
              v-model="intent"
              maxlength="2000"
              class="min-h-24 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              :placeholder="t('dataQuality.llm.draft.intentPlaceholder')"
            />
            <p class="text-sm text-muted-foreground">
              {{ t("dataQuality.llm.draft.intentHint") }}
            </p>
          </div>

          <div class="space-y-2">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <Label>{{ t("dataQuality.llm.draft.columns") }}</Label>
              <div class="flex items-center gap-3 text-xs">
                <span class="text-muted-foreground">
                  {{ t("dataQuality.llm.draft.columnsSelected", { selected: selected.length, total: columns.length }) }}
                </span>
                <button
                  type="button"
                  class="text-primary hover:underline"
                  @click="selectAll"
                >
                  {{ t("common.selectAll") }}
                </button>
                <button
                  type="button"
                  class="text-primary hover:underline"
                  @click="clearSelection"
                >
                  {{ t("common.clear") }}
                </button>
              </div>
            </div>
            <div
              class="max-h-40 overflow-auto rounded-md border p-2"
            >
              <div
                v-if="columns.length === 0"
                class="px-2 py-1 text-sm text-muted-foreground"
              >
                {{ t("dataQuality.llm.draft.noColumns") }}
              </div>
              <label
                v-for="column in columns"
                :key="column"
                class="flex cursor-pointer items-center gap-2 rounded px-2 py-1 text-sm hover:bg-muted/50"
              >
                <Checkbox
                  :checked="selected.includes(column)"
                  @update:checked="(checked: boolean | 'indeterminate') => toggleColumn(column, checked === true)"
                />
                <span class="font-mono">{{ column }}</span>
              </label>
            </div>
            <p class="text-sm text-muted-foreground">
              {{ t("dataQuality.llm.draft.columnsHint") }}
            </p>
          </div>

          <Alert
            v-if="error"
            variant="destructive"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>{{ t(errorKey) }}</AlertTitle>
            <AlertDescription class="break-words">
              {{ error }}
            </AlertDescription>
          </Alert>

          <div
            v-if="submitting"
            class="flex items-center gap-2 text-sm text-muted-foreground"
          >
            <Loader2 class="h-4 w-4 animate-spin" />
            {{ t("dataQuality.llm.draft.generating") }}
          </div>

          <div
            v-else-if="results.length > 0"
            class="space-y-3"
          >
            <div class="text-sm font-medium">
              {{ t("dataQuality.llm.draft.resultsTitle", { count: results.length }) }}
            </div>
            <div
              v-for="suggestion in results"
              :key="suggestion.name"
              class="space-y-2 rounded-md border p-3"
            >
              <div class="flex flex-wrap items-center gap-2">
                <Badge
                  v-if="summary(suggestion)?.typeKey"
                  variant="secondary"
                >
                  {{ t(summary(suggestion)?.typeKey ?? "") }}
                </Badge>
                <span class="text-sm font-medium">
                  {{ summary(suggestion)?.title }}
                </span>
                <span class="text-xs text-muted-foreground">
                  {{ t("dataQuality.suggestions.confidence") }}
                  {{ summary(suggestion)?.confidence }}
                </span>
              </div>
              <p
                v-if="summary(suggestion)?.rationale"
                class="text-sm break-words text-muted-foreground"
              >
                {{ summary(suggestion)?.rationale }}
              </p>
              <div class="flex justify-end gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  :disabled="busyName !== ''"
                  @click="reject(suggestion)"
                >
                  {{ t("dataQuality.suggestions.reject") }}
                </Button>
                <Button
                  size="sm"
                  :disabled="busyName !== ''"
                  @click="accept(suggestion)"
                >
                  {{ t("dataQuality.llm.draft.openInEditor") }}
                </Button>
              </div>
            </div>
          </div>

          <EmptyState
            v-else-if="generated"
            compact
            :icon="Sparkles"
            :title="t('dataQuality.llm.draft.emptyTitle')"
            :description="t('dataQuality.llm.draft.emptyDescription')"
          />
        </div>

        <DialogFooter class="shrink-0 border-t px-6 py-4">
          <Button
            variant="outline"
            @click="emit('update:open', false)"
          >
            {{ t("common.close") }}
          </Button>
          <Button
            :disabled="submitting || !canSubmit"
            @click="generate"
          >
            <Sparkles class="mr-2 h-4 w-4" />
            {{ submitting ? t("dataQuality.llm.draft.generating") : t("dataQuality.llm.draft.generate") }}
          </Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { AlertTriangle, Loader2, Sparkles } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  suggestQualityRule,
  updateQualitySuggestion,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { notify } from "@/lib/notify";
import {
  type QualitySuggestion,
  QualitySuggestionStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  draftSuggestionSummary,
  explainErrorMessageKey,
} from "@/utils/dataQualityLlm";

/**
 * Ask the model for rule drafts for the current target. The results are only
 * suggestions: opening one accepts it and prefills the editor; nothing is
 * created until the person saves the rule.
 */

const props = defineProps<{
  open: boolean;
  tableGuid: string;
  metaType: MetaType;
  columns: string[];
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  accepted: [suggestion: QualitySuggestion];
}>();

const { t, locale } = useI18n();

const intent = ref("");
const selected = ref<string[]>([]);
const submitting = ref(false);
const generated = ref(false);
const results = ref<QualitySuggestion[]>([]);
const error = ref("");
const errorKey = ref("");
const busyName = ref("");

const canSubmit = computed(() => intent.value.trim() !== "");

watch(
  () => props.open,
  (isOpen) => {
    if (!isOpen) {
      return;
    }
    intent.value = "";
    selected.value = [...props.columns];
    results.value = [];
    generated.value = false;
    error.value = "";
    errorKey.value = "";
    busyName.value = "";
  },
  { immediate: true }
);

function selectAll() {
  selected.value = [...props.columns];
}

function clearSelection() {
  selected.value = [];
}

function toggleColumn(column: string, checked: boolean) {
  if (checked && !selected.value.includes(column)) {
    selected.value = [...selected.value, column];
  } else if (!checked) {
    selected.value = selected.value.filter((item) => item !== column);
  }
}

function summary(suggestion: QualitySuggestion) {
  return draftSuggestionSummary(suggestion);
}

function fail(e: unknown) {
  errorKey.value = explainErrorMessageKey(e);
  error.value = e instanceof Error ? e.message : String(e);
}

async function generate() {
  if (submitting.value || !canSubmit.value) {
    return;
  }
  submitting.value = true;
  error.value = "";
  results.value = [];
  generated.value = false;
  try {
    results.value = await suggestQualityRule({
      tableGuid: props.tableGuid,
      metaType: props.metaType,
      columns: selected.value,
      intent: intent.value.trim(),
      language: locale.value,
    });
    generated.value = true;
    if (results.value.length === 0) {
      notify.info(t("dataQuality.llm.draft.emptyTitle"));
    }
  } catch (e: unknown) {
    fail(e);
  } finally {
    submitting.value = false;
  }
}

/** Opening a draft only prefills the editor. The editor records the acceptance
 * after the rule is created, so a cancelled draft stays pending for review. */
function accept(suggestion: QualitySuggestion) {
  emit("accepted", suggestion);
  emit("update:open", false);
}

async function reject(suggestion: QualitySuggestion) {
  if (busyName.value !== "") {
    return;
  }
  busyName.value = suggestion.name;
  error.value = "";
  try {
    await updateQualitySuggestion({
      name: suggestion.name,
      decision: QualitySuggestionStatus.REJECTED,
      note: "",
    });
    results.value = results.value.filter(
      (item) => item.name !== suggestion.name
    );
    notify.success(t("dataQuality.suggestions.rejected"));
  } catch (e: unknown) {
    fail(e);
  } finally {
    busyName.value = "";
  }
}
</script>
