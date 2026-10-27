<template>
  <Card>
    <CardHeader class="space-y-1.5 border-b">
      <CardTitle class="flex items-center gap-2 text-base">
        <Sparkles class="h-4 w-4 shrink-0 text-primary" />
        {{ t("dataQuality.copilot.title") }}
      </CardTitle>
      <CardDescription>
        {{ t("dataQuality.copilot.description") }}
      </CardDescription>
    </CardHeader>
    <CardContent class="space-y-4 pt-4">
      <p
        v-if="disabledReason"
        class="rounded-md border border-warning/30 bg-warning-subtle px-3 py-2 text-sm text-warning"
      >
        {{ disabledReason }}
      </p>

      <!-- The intent path needs words; this one does not. A label pass is a
           different output from a check draft, so it sits apart from the
           drafting form and hands its result to the page's suggestion queue. -->
      <div class="flex flex-wrap items-center justify-between gap-3 rounded-md border border-dashed px-3 py-2.5">
        <div class="min-w-0 space-y-0.5">
          <p class="text-xs font-medium">
            {{ t("dataQuality.copilot.quickTitle") }}
          </p>
          <p class="text-xs text-muted-foreground">
            {{ t("dataQuality.copilot.quickHint") }}
          </p>
        </div>
        <SuggestLabelsButton
          :target-guid="targetGuid"
          :meta-type="metaType"
          :columns="labelColumns"
          :can-profile="canProfile"
          size="sm"
          :disabled="!!disabledReason"
          @started="labelsError = ''"
          @suggested="onLabelsSuggested"
          @failed="labelsError = $event"
        />
      </div>
      <p
        v-if="labelsError"
        class="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive break-words"
      >
        {{ labelsError }}
      </p>

      <!-- An example fills the box; it does not submit, so the reader can edit
           the wording before asking. -->
      <div class="space-y-1.5">
        <Label>{{ t("dataQuality.copilot.examplesTitle") }}</Label>
        <div class="flex flex-wrap gap-1.5">
          <button
            v-for="example in examples"
            :key="example"
            type="button"
            class="rounded-full border border-input px-2.5 py-1 text-xs transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:pointer-events-none disabled:opacity-50"
            :disabled="generating"
            @click="intent = example"
          >
            {{ example }}
          </button>
        </div>
      </div>

      <div class="space-y-1.5">
        <div class="flex items-center justify-between gap-2">
          <Label for="dq-copilot-intent">
            {{ t("dataQuality.copilot.title") }}
          </Label>
          <span class="text-xs text-muted-foreground tabular-nums">
            {{ intent.length }} / {{ MAX_INTENT_LENGTH }}
          </span>
        </div>
        <textarea
          id="dq-copilot-intent"
          ref="inputRef"
          v-model="intent"
          rows="4"
          :maxlength="MAX_INTENT_LENGTH"
          class="min-h-24 w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/25 focus-visible:outline-none"
          :placeholder="t('dataQuality.copilot.placeholder')"
          :disabled="generating"
          :aria-invalid="overLimit"
          @keydown.enter.exact.prevent="submit"
        />
      </div>

      <div class="flex items-center justify-between gap-2">
        <span class="text-xs text-muted-foreground">
          {{ t("dataQuality.copilot.newDrafts") }}
        </span>
        <Button
          :disabled="!canSubmit"
          @click="submit"
        >
          <component
            :is="generating ? Loader2 : Sparkles"
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': generating }"
          />
          {{
            generating
              ? t("dataQuality.copilot.generating")
              : t("dataQuality.copilot.generate")
          }}
        </Button>
      </div>

      <!-- Failures stay in the panel beside the intent, never as a toast: the
           button and the text the reader typed remain where they were. -->
      <div
        v-if="errorKind"
        class="space-y-1.5 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive"
      >
        <p class="font-medium">
          {{ errorTitle }}
        </p>
        <p
          v-if="errorHint"
          class="break-words"
        >
          {{ errorHint }}
        </p>
        <p
          v-if="errorDetail"
          class="text-xs break-words"
        >
          {{ errorDetail }}
        </p>
        <RouterLink
          v-if="errorKind === 'needLlm'"
          :to="{ name: 'DataQualitySettings' }"
          class="inline-flex text-primary hover:underline"
        >
          {{ t("dataQuality.copilot.openSettings") }}
        </RouterLink>
      </div>

      <p
        v-else-if="generated"
        class="rounded-md border bg-muted/40 px-3 py-2 text-sm text-muted-foreground break-words"
      >
        {{ resultText }}
      </p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { Code } from "@connectrpc/connect";
import { Loader2, Sparkles } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { suggestQualityRule } from "@/api/data-quality";
import SuggestLabelsButton from "@/components/data-quality/SuggestLabelsButton.vue";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useSuggestionSummary } from "@/composables/useSuggestionSummary";
import {
  type QualitySuggestion,
  type QualitySuggestionQueueSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { connectErrorCode } from "@/utils/dataQualityLlm";

/**
 * The target workspace's AI copilot: one intent in, rule drafts out.
 *
 * The pass never decides anything: it queues drafts, which the page renders
 * inline beside the rules they would become, and a failed call keeps the intent
 * so the reader can retry without retyping. The synchronous call is the same one
 * the rule editor's workbench uses; the page owns where the returned drafts go.
 */

const props = withDefaults(
  defineProps<{
    targetGuid: string;
    metaType: MetaType;
    canSuggest: boolean;
    /** The columns a one-click label pass may propose labels for. */
    labelColumns?: string[];
    /** Whether the reader may profile columns before labeling them. */
    canProfile?: boolean;
    /** Why drafting is unavailable right now; unset when it is available. */
    disabledReason?: string;
  }>(),
  { labelColumns: () => [], canProfile: false }
);

const emit = defineEmits<{
  drafted: [
    suggestions: QualitySuggestion[],
    summary: QualitySuggestionQueueSummary | undefined,
    newNames: string[],
  ];
  /** The label pass is done; the page re-reads the pending queue. */
  labelsSuggested: [];
}>();

type CopilotErrorKind = "" | "needLlm" | "quota" | "permission" | "failed";

/** The server's drafting intent limit, mirrored by the field's maxlength. */
const MAX_INTENT_LENGTH = 2000;

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const { summaryText } = useSuggestionSummary();

const intent = ref("");
const generating = ref(false);
const generated = ref(false);
/** The one-click label pass reports its own failure beside its button. */
const labelsError = ref("");
const errorKind = ref<CopilotErrorKind>("");
const errorDetail = ref("");
const lastCount = ref(0);
const lastSummary = ref<QualitySuggestionQueueSummary>();
const inputRef = ref<HTMLTextAreaElement>();

/**
 * The example prompts. They are translated at setup, not looked up from a key in
 * the template: the i18n checker can only follow a literal `t(...)` key.
 */
const examples = computed(() => [
  t("dataQuality.copilot.exampleNulls"),
  t("dataQuality.copilot.exampleDuplicates"),
  t("dataQuality.copilot.exampleFreshness"),
  t("dataQuality.copilot.exampleRange"),
]);

const overLimit = computed(() => intent.value.length > MAX_INTENT_LENGTH);

const canSubmit = computed(
  () =>
    props.canSuggest &&
    !props.disabledReason &&
    !generating.value &&
    !overLimit.value &&
    intent.value.trim() !== ""
);

/** The sentence the pass's queue summary turns into; no toast competes with it. */
const resultText = computed(() =>
  lastCount.value === 0
    ? t("dataQuality.copilot.noDrafts")
    : summaryText(lastSummary.value, "rule", lastCount.value)
);

const errorTitle = computed(() => {
  switch (errorKind.value) {
    case "needLlm":
      return t("dataQuality.copilot.needLlmTitle");
    case "quota":
      return t("dataQuality.copilot.quotaExceeded");
    case "permission":
      return t("dataQuality.copilot.noPermission");
    default:
      return t("dataQuality.copilot.failed");
  }
});

const errorHint = computed(() =>
  errorKind.value === "needLlm" ? t("dataQuality.copilot.needLlmHint") : ""
);

/** Focus the intent from the page header's AI action. */
function focus() {
  inputRef.value?.focus();
}

function classify(error: unknown): CopilotErrorKind {
  switch (connectErrorCode(error)) {
    case Code.FailedPrecondition:
      return "needLlm";
    case Code.ResourceExhausted:
      return "quota";
    case Code.PermissionDenied:
      return "permission";
    default:
      return "failed";
  }
}

async function submit() {
  if (!canSubmit.value) {
    return;
  }
  generating.value = true;
  errorKind.value = "";
  errorDetail.value = "";
  try {
    const response = await suggestQualityRule({
      tableGuid: props.targetGuid,
      metaType: props.metaType,
      intent: intent.value.trim(),
      language: locale.value,
    });
    lastCount.value = response.qualitySuggestions.length;
    lastSummary.value = response.summary;
    generated.value = true;
    emit(
      "drafted",
      response.qualitySuggestions,
      response.summary,
      response.qualitySuggestions.map((suggestion) => suggestion.name)
    );
  } catch (error: unknown) {
    generated.value = false;
    errorKind.value = classify(error);
    const detail = formatError(error, "dataQuality.copilot.failed");
    // A plain failure's fallback sentence is already the title; showing it twice
    // reads like two different problems.
    errorDetail.value =
      detail === t("dataQuality.copilot.failed") ? "" : detail;
  } finally {
    generating.value = false;
  }
}

/** The label pass queued proposals; the page owns reading the queue back. */
function onLabelsSuggested() {
  emit("labelsSuggested");
}

defineExpose({ focus });
</script>
