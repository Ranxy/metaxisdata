<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent
      class="max-h-[85vh] gap-0 overflow-hidden p-0"
      :class="editorMode ? 'sm:max-w-2xl' : 'sm:max-w-3xl'"
    >
      <div class="flex max-h-[85vh] min-h-0 flex-col">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>{{ t("dataQuality.llm.draft.title") }}</DialogTitle>
          <DialogDescription>
            {{
              editorMode
                ? t("dataQuality.llm.draft.description")
                : t("dataQuality.suggestions.workbench.description")
            }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-5 overflow-auto px-6 py-5">
          <Alert
            v-if="!availability.available"
            variant="warning"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>{{ t("dataQuality.llm.draft.title") }}</AlertTitle>
            <AlertDescription class="space-y-1">
              <p>{{ t(availability.reasonKey ?? "dataQuality.llm.disabledHint") }}</p>
              <RouterLink
                v-if="!llmEnabled && canSuggest"
                :to="{ name: 'DataQualitySettings' }"
                class="text-primary hover:underline"
              >
                {{ t("dataQuality.llm.enableInSettings") }}
              </RouterLink>
            </AlertDescription>
          </Alert>

          <!-- Page mode: queue several targets in one run. -->
          <section
            v-if="!editorMode"
            class="space-y-2"
          >
            <div class="flex flex-wrap items-center justify-between gap-2">
              <div>
                <Label>{{ t("dataQuality.suggestions.workbench.targets") }}</Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("dataQuality.suggestions.workbench.targetsHint") }}
                </p>
              </div>
              <Button
                variant="outline"
                size="sm"
                :disabled="generating"
                @click="pickerOpen = true"
              >
                <Plus class="mr-2 h-4 w-4" />
                {{ t("dataQuality.suggestions.workbench.addTarget") }}
              </Button>
            </div>
            <div
              v-if="targets.length === 0"
              class="rounded-md border border-dashed p-4 text-sm text-muted-foreground"
            >
              {{ t("dataQuality.suggestions.workbench.noTargets") }}
            </div>
            <ul
              v-else
              class="space-y-2"
            >
              <li
                v-for="target in targets"
                :key="target.guid"
                class="rounded-md border p-3"
              >
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <div class="truncate text-sm font-medium">
                      {{ targetLabel(target) }}
                    </div>
                    <div class="text-xs text-muted-foreground">
                      {{ metaTypeSingularLabel(target.metaType, t) }}
                    </div>
                    <p
                      v-if="progress[target.guid]?.status === 'failed'"
                      class="mt-1 text-xs break-words text-destructive"
                      :title="progress[target.guid]?.message"
                    >
                      {{ progressMessage(target.guid) }}
                    </p>
                  </div>
                  <div class="flex shrink-0 items-center gap-2">
                    <Badge
                      v-if="progress[target.guid]"
                      :variant="progressVariant(target.guid)"
                    >
                      {{ progressLabel(target.guid) }}
                    </Badge>
                    <Button
                      v-if="!generating"
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :title="t('common.delete')"
                      :aria-label="t('common.delete')"
                      @click="removeTarget(target.guid)"
                    >
                      <X class="h-4 w-4 text-muted-foreground" />
                    </Button>
                  </div>
                </div>
              </li>
            </ul>
          </section>

          <!-- Columns are only meaningful when the intent can name them: one
               fixed target in the editor, one picked target in page mode. -->
          <section
            v-if="columnsTargetGuid"
            class="space-y-2"
          >
            <div class="flex flex-wrap items-center justify-between gap-2">
              <Label>{{ t("dataQuality.llm.draft.columns") }}</Label>
              <div class="flex items-center gap-3 text-xs">
                <span class="text-muted-foreground">
                  {{ t("dataQuality.llm.draft.columnsSelected", { selected: selectedColumns.length, total: columnOptions.length }) }}
                </span>
                <button
                  type="button"
                  class="text-primary hover:underline"
                  :disabled="generating"
                  @click="selectAllColumns"
                >
                  {{ t("common.selectAll") }}
                </button>
                <button
                  type="button"
                  class="text-primary hover:underline"
                  :disabled="generating"
                  @click="selectedColumns = []"
                >
                  {{ t("common.clear") }}
                </button>
              </div>
            </div>
            <div class="max-h-40 overflow-auto rounded-md border p-2">
              <div
                v-if="columnsLoading"
                class="flex items-center gap-2 px-2 py-1 text-sm text-muted-foreground"
              >
                <Loader2 class="h-4 w-4 animate-spin" />
                {{ t("common.loading") }}
              </div>
              <div
                v-else-if="columnOptions.length === 0"
                class="px-2 py-1 text-sm text-muted-foreground"
              >
                {{ t("dataQuality.llm.draft.noColumns") }}
              </div>
              <template v-else>
                <label
                  v-for="column in columnOptions"
                  :key="column"
                  class="flex cursor-pointer items-center gap-2 rounded px-2 py-1 text-sm hover:bg-muted/50"
                >
                  <Checkbox
                    :checked="selectedColumns.includes(column)"
                    @update:checked="(checked: boolean | 'indeterminate') => toggleColumn(column, checked === true)"
                  />
                  <span class="font-mono">{{ column }}</span>
                </label>
              </template>
            </div>
            <p class="text-sm text-muted-foreground">
              {{ t("dataQuality.llm.draft.columnsHint") }}
            </p>
          </section>

          <div class="space-y-1.5">
            <div class="flex items-center justify-between gap-2">
              <Label for="dq-workbench-intent">
                {{ t("dataQuality.llm.draft.intent") }}
              </Label>
              <span class="text-xs text-muted-foreground">{{ intent.length }} / 2000</span>
            </div>
            <textarea
              id="dq-workbench-intent"
              v-model="intent"
              maxlength="2000"
              class="min-h-24 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              :placeholder="t('dataQuality.llm.draft.intentPlaceholder')"
            />
            <p class="text-sm text-muted-foreground">
              {{ t("dataQuality.llm.draft.intentHint") }}
            </p>
          </div>

          <Alert
            v-if="error"
            variant="destructive"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>{{ errorKey ? t(errorKey) : generateFailedLabel() }}</AlertTitle>
            <AlertDescription class="break-words">
              {{ error }}
            </AlertDescription>
          </Alert>

          <div
            v-if="generating"
            class="flex items-center gap-2 text-sm text-muted-foreground"
          >
            <Loader2 class="h-4 w-4 animate-spin" />
            {{ t("dataQuality.llm.draft.generating") }}
          </div>

          <section
            v-else-if="results.length > 0"
            class="space-y-3"
          >
            <div class="flex flex-wrap items-center justify-between gap-2">
              <div class="text-sm font-medium">
                {{
                  editorMode
                    ? t("dataQuality.llm.draft.resultsTitle", { count: results.length })
                    : t("dataQuality.suggestions.workbench.resultsSummary", {
                        count: results.length,
                      })
                }}
              </div>
              <RouterLink
                v-if="editorMode"
                :to="{ name: 'DataQualitySuggestions' }"
                class="text-sm text-primary hover:underline"
              >
                {{ t("dataQuality.suggestions.openCenter") }}
              </RouterLink>
            </div>
            <p
              v-if="!editorMode"
              class="text-sm text-muted-foreground"
            >
              {{ t("dataQuality.suggestions.workbench.resultsHint") }}
            </p>
            <div
              v-for="suggestion in results"
              :key="suggestion.name"
              class="space-y-2 rounded-md border p-3"
            >
              <div class="flex flex-wrap items-center gap-2">
                <Badge
                  v-if="draftSummary(suggestion)?.typeKey"
                  variant="secondary"
                >
                  {{ t(draftSummary(suggestion)?.typeKey ?? "") }}
                </Badge>
                <span class="text-sm font-medium">
                  {{ draftSummary(suggestion)?.title }}
                </span>
                <span class="text-xs text-muted-foreground">
                  {{ t("dataQuality.suggestions.confidence") }}
                  {{ draftSummary(suggestion)?.confidence }}
                </span>
              </div>
              <p
                v-if="draftSummary(suggestion)?.rationale"
                class="text-sm break-words text-muted-foreground"
              >
                {{ draftSummary(suggestion)?.rationale }}
              </p>
              <div
                v-if="editorMode"
                class="flex justify-end gap-2"
              >
                <Button
                  variant="outline"
                  size="sm"
                  :disabled="busyName !== ''"
                  @click="rejectResult(suggestion)"
                >
                  {{ t("dataQuality.suggestions.reject") }}
                </Button>
                <Button
                  size="sm"
                  :disabled="busyName !== ''"
                  @click="acceptResult(suggestion)"
                >
                  {{ t("dataQuality.llm.draft.openInEditor") }}
                </Button>
              </div>
            </div>
          </section>

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
            :disabled="generating"
            @click="close"
          >
            {{ t("common.close") }}
          </Button>
          <Button
            v-if="!editorMode && results.length > 0 && !generating"
            @click="finishQueued"
          >
            {{ t("dataQuality.suggestions.workbench.goReview") }}
          </Button>
          <Button
            :disabled="!canGenerate"
            @click="generate"
          >
            <Sparkles class="mr-2 h-4 w-4" />
            {{
              generating
                ? t("dataQuality.llm.draft.generating")
                : t("dataQuality.llm.draft.generate")
            }}
          </Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>

  <TargetPicker
    v-if="!editorMode"
    v-model:open="pickerOpen"
    multiple
    @select="addTarget"
    @select-many="addTargets"
  />
</template>

<script setup lang="ts">
import { AlertTriangle, Loader2, Plus, Sparkles, X } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import {
  batchUpdateQualitySuggestions,
  suggestQualityRule,
} from "@/api/data-quality";
import { listMetadata } from "@/api/database";
import EmptyState from "@/components/common/EmptyState.vue";
import TargetPicker from "@/components/data-quality/TargetPicker.vue";
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
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import { useInstanceStore } from "@/store/modules/instance";
import {
  type QualitySuggestion,
  QualitySuggestionStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { TargetSelection } from "@/utils/dataQualityForm";
import {
  draftSuggestionSummary,
  explainErrorMessageKey,
  llmFeatureAvailability,
} from "@/utils/dataQualityLlm";
import { metaTypeSingularLabel } from "@/utils/metaType";

/**
 * The AI drafting workbench: one dialog with two entry points.
 *
 * Page mode (the suggestion center) queues one or more targets and runs them
 * sequentially; a target's failure is reported per row and does not stop the
 * rest. Editor mode takes a fixed target and keeps the previous behavior — a
 * result is filled into the open form or rejected, and nothing is created here.
 * Every result is only a suggestion; the review queue owns creating rules.
 */

interface FixedTarget {
  tableGuid: string;
  metaType: MetaType;
  columns: string[];
}

const props = withDefaults(
  defineProps<{
    open: boolean;
    /** Editor mode: the target is fixed and its columns are already loaded. */
    fixedTarget?: FixedTarget;
    /** The workspace LLM switch, read by the page that opens this dialog. */
    llmEnabled?: boolean;
  }>(),
  { fixedTarget: undefined, llmEnabled: false }
);

const emit = defineEmits<{
  "update:open": [value: boolean];
  accepted: [suggestion: QualitySuggestion];
  queued: [suggestions: QualitySuggestion[]];
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();
const instanceStore = useInstanceStore();

const editorMode = computed(() => props.fixedTarget !== undefined);
const canSuggest = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.suggest")
);
const availability = computed(() =>
  llmFeatureAvailability(props.llmEnabled, canSuggest.value)
);

const targets = ref<TargetSelection[]>([]);
const pickerOpen = ref(false);
const intent = ref("");
const columnOptions = ref<string[]>(props.fixedTarget?.columns ?? []);
const selectedColumns = ref<string[]>([...(props.fixedTarget?.columns ?? [])]);
const columnsLoading = ref(false);
const generating = ref(false);
const generated = ref(false);
const results = ref<QualitySuggestion[]>([]);
const busyName = ref("");
const error = ref("");
const errorKey = ref("");

type TargetStatus = "waiting" | "running" | "done" | "failed";
interface TargetProgress {
  status: TargetStatus;
  count: number;
  message: string;
  messageKey: string;
}

const progress = ref<Record<string, TargetProgress>>({});

/** The one target whose column checklist is shown; undefined when ambiguous. */
const columnsTargetGuid = computed(() => {
  if (editorMode.value) {
    return props.fixedTarget?.tableGuid ?? "";
  }
  return targets.value.length === 1 ? (targets.value[0]?.guid ?? "") : "";
});

const canGenerate = computed(
  () =>
    availability.value.available &&
    !generating.value &&
    intent.value.trim() !== "" &&
    (editorMode.value
      ? props.fixedTarget !== undefined
      : targets.value.length > 0)
);

function reset() {
  targets.value = [];
  pickerOpen.value = false;
  intent.value = "";
  columnOptions.value = [...(props.fixedTarget?.columns ?? [])];
  selectedColumns.value = [...(props.fixedTarget?.columns ?? [])];
  columnsLoading.value = false;
  generating.value = false;
  generated.value = false;
  results.value = [];
  busyName.value = "";
  error.value = "";
  errorKey.value = "";
  progress.value = {};
}

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      reset();
    }
  },
  { immediate: true }
);

// The editor can finish loading the target's columns after the dialog opened;
// keep the checklist in step rather than showing an empty one.
watch(
  () => props.fixedTarget?.columns,
  (columns) => {
    if (!editorMode.value || generating.value) {
      return;
    }
    columnOptions.value = [...(columns ?? [])];
    selectedColumns.value = [...(columns ?? [])];
  }
);

/** The dialog stays open while requests are on the wire; closing would orphan them. */
function onOpenChange(value: boolean) {
  if (value) {
    return;
  }
  if (generating.value) {
    return;
  }
  close();
}

function close() {
  emit("update:open", false);
}

function addTarget(selection: TargetSelection) {
  addTargets([selection]);
}

/** Add a whole picker selection at once, skipping targets already queued. */
function addTargets(selections: TargetSelection[]) {
  const known = new Set(targets.value.map((target) => target.guid));
  const added: TargetSelection[] = [];
  for (const selection of selections) {
    if (known.has(selection.guid)) {
      continue;
    }
    known.add(selection.guid);
    added.push(selection);
  }
  if (added.length === 0) {
    return;
  }
  targets.value = [...targets.value, ...added];
  if (targets.value.length === 1) {
    void loadColumns(targets.value[0]?.guid ?? "");
  } else {
    // Several targets cannot share one column checklist: the intent scopes the
    // draft and each target gets every column.
    columnOptions.value = [];
    selectedColumns.value = [];
  }
}

function removeTarget(guid: string) {
  targets.value = targets.value.filter((target) => target.guid !== guid);
  if (targets.value.length === 1) {
    void loadColumns(targets.value[0]?.guid ?? "");
  } else if (targets.value.length === 0) {
    columnOptions.value = [];
    selectedColumns.value = [];
  }
}

function targetLabel(target: TargetSelection): string {
  const instance = target.path.instance
    ? instanceStore.titleOf(`instances/${target.path.instance}`)
    : "";
  const parts = [
    instance,
    target.path.database,
    target.path.schema,
    target.path.object,
  ].filter(Boolean);
  return parts.length > 0 ? parts.join(" · ") : target.guid;
}

/** Columns the model should consider for one target; several targets send none. */
function columnsFor(targetGuid: string): string[] {
  if (targets.value.length > 1 && !editorMode.value) {
    return [];
  }
  if (targets.value.length === 1 || editorMode.value) {
    return targetGuid === columnsTargetGuid.value
      ? [...selectedColumns.value]
      : [];
  }
  return [];
}

async function loadColumns(guid: string) {
  if (!guid) {
    return;
  }
  columnsLoading.value = true;
  try {
    const response = await listMetadata({ parentGuid: guid, pageSize: 200 });
    const group = response.typesStoredMetadata.find(
      (item) => item.metaType === MetaType.COLUMN
    );
    columnOptions.value =
      group?.list.map((item) => {
        const value = item.type.value as { name?: string } | undefined;
        return value?.name ?? item.guid;
      }) ?? [];
    selectedColumns.value = [...columnOptions.value];
  } catch {
    columnOptions.value = [];
    selectedColumns.value = [];
  } finally {
    columnsLoading.value = false;
  }
}

function selectAllColumns() {
  selectedColumns.value = [...columnOptions.value];
}

function toggleColumn(column: string, checked: boolean) {
  if (checked && !selectedColumns.value.includes(column)) {
    selectedColumns.value = [...selectedColumns.value, column];
  } else if (!checked) {
    selectedColumns.value = selectedColumns.value.filter(
      (item) => item !== column
    );
  }
}

function progressVariant(
  guid: string
): "secondary" | "outline" | "success" | "danger" {
  switch (progress.value[guid]?.status) {
    case "running":
      return "outline";
    case "done":
      return "success";
    case "failed":
      return "danger";
    default:
      return "secondary";
  }
}

/** The failed target's friendly reason, in the language of the panel. */
function progressMessage(guid: string): string {
  const key = progress.value[guid]?.messageKey;
  return key ? t(key) : generateFailedLabel();
}

/** A static reference so the unused-key check can see the fallback copy. */
function generateFailedLabel(): string {
  return t("dataQuality.suggestions.workbench.generateFailed");
}

function progressLabel(guid: string): string {
  const state = progress.value[guid];
  switch (state?.status) {
    case "running":
      return t("dataQuality.suggestions.workbench.running");
    case "done":
      return t("dataQuality.suggestions.workbench.doneCount", {
        count: state.count,
      });
    case "failed":
      return t("dataQuality.suggestions.workbench.failed");
    default:
      return t("dataQuality.suggestions.workbench.waiting");
  }
}

function draftSummary(suggestion: QualitySuggestion) {
  return draftSuggestionSummary(suggestion);
}

/** The targets the run visits, in the order they were queued. */
function generationTargets(): Array<{
  tableGuid: string;
  metaType: MetaType;
  columns: string[];
}> {
  if (editorMode.value) {
    return props.fixedTarget ? [{ ...props.fixedTarget }] : [];
  }
  return targets.value.map((target) => ({
    tableGuid: target.guid,
    metaType: target.metaType,
    columns: columnsFor(target.guid),
  }));
}

async function generate() {
  if (!canGenerate.value) {
    return;
  }
  generating.value = true;
  error.value = "";
  errorKey.value = "";
  results.value = [];
  generated.value = false;
  const queue = generationTargets();
  progress.value = Object.fromEntries(
    queue.map((target) => [
      target.tableGuid,
      { status: "waiting", count: 0, message: "", messageKey: "" },
    ])
  );
  const collected: QualitySuggestion[] = [];
  for (const target of queue) {
    progress.value = {
      ...progress.value,
      [target.tableGuid]: {
        status: "running",
        count: 0,
        message: "",
        messageKey: "",
      },
    };
    try {
      const suggestions = await suggestQualityRule({
        tableGuid: target.tableGuid,
        metaType: target.metaType,
        columns: target.columns,
        intent: intent.value.trim(),
        language: locale.value,
      });
      collected.push(...suggestions);
      progress.value = {
        ...progress.value,
        [target.tableGuid]: {
          status: "done",
          count: suggestions.length,
          message: "",
          messageKey: "",
        },
      };
    } catch (e: unknown) {
      // One failed target must not cancel the rest of the run.
      progress.value = {
        ...progress.value,
        [target.tableGuid]: {
          status: "failed",
          count: 0,
          message: formatError(
            e,
            "dataQuality.suggestions.workbench.generateFailed"
          ),
          messageKey: explainErrorMessageKey(e),
        },
      };
    }
  }
  results.value = collected;
  generated.value = true;
  generating.value = false;
  if (collected.length === 0) {
    const failed = Object.values(progress.value).some(
      (state) => state.status === "failed"
    );
    if (!failed) {
      notify.info(t("dataQuality.llm.draft.emptyTitle"));
    }
  }
}

/** Page mode's review hand-off: the page refreshes and selects what was queued. */
function finishQueued() {
  emit("queued", [...results.value]);
  close();
}

/** Editor mode: prefill the form; the editor accepts the suggestion on save. */
function acceptResult(suggestion: QualitySuggestion) {
  emit("accepted", suggestion);
  close();
}

/** Editor mode: a draft the person does not want is rejected, not created. */
async function rejectResult(suggestion: QualitySuggestion) {
  if (busyName.value !== "") {
    return;
  }
  busyName.value = suggestion.name;
  error.value = "";
  errorKey.value = "";
  try {
    await batchUpdateQualitySuggestions([
      {
        name: suggestion.name,
        decision: QualitySuggestionStatus.REJECTED,
      },
    ]);
    results.value = results.value.filter(
      (item) => item.name !== suggestion.name
    );
    notify.success(t("dataQuality.suggestions.rejected"));
  } catch (e: unknown) {
    errorKey.value = explainErrorMessageKey(e);
    error.value = formatError(e, "dataQuality.suggestions.decideError");
  } finally {
    busyName.value = "";
  }
}
</script>
