<template>
  <div class="flex">
    <Button
      variant="outline"
      :size="size"
      class="rounded-r-none"
      :disabled="busy || disabled"
      :aria-busy="busy"
      :title="t('dataQuality.suggest.labelsHint')"
      @click="suggest(false)"
    >
      <Sparkles :class="iconClass" />
      {{ actionLabel }}
    </Button>
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <Button
          variant="outline"
          :size="size"
          class="rounded-l-none border-l-0 px-2"
          :disabled="busy || disabled"
          :title="t('dataQuality.suggest.regenerateHint')"
          :aria-label="t('dataQuality.suggest.regenerate')"
        >
          <ChevronDown class="h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        class="max-w-72"
      >
        <DropdownMenuItem
          :disabled="busy || disabled"
          @select="suggest(true)"
        >
          <div class="space-y-1">
            <div class="flex items-center gap-2">
              <RefreshCw class="h-4 w-4" />
              {{ t("dataQuality.suggest.regenerate") }}
            </div>
            <p class="text-xs whitespace-normal text-muted-foreground">
              {{ t("dataQuality.suggest.regenerateHint") }}
            </p>
          </div>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  </div>
</template>

<script setup lang="ts">
import { ChevronDown, RefreshCw, Sparkles } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  listColumnProfiles,
  profileColumns,
  suggestColumnLabels,
} from "@/api/data-quality";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useSuggestionSummary } from "@/composables/useSuggestionSummary";
import type { SuggestColumnLabelsResponse } from "@/types/proto-es/v1/data_quality_service_pb";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";

/**
 * The one-click label pass, as a primary action plus a split "regenerate".
 *
 * Both surfaces that can start a pass (the metadata tab and the target
 * workspace) share this control, so the wording, the request chain and the
 * queue summary cannot drift between them. The pass only queues proposals; the
 * parent decides how to read the queue back and where a failure is shown.
 *
 * A label pass refuses to label blind: the server requires a profile of every
 * analyzed column younger than its own freshness bound. The metadata page made
 * that a separate "Analyze columns" step, but the target workspace has no such
 * button, so the control checks the stored profiles first and profiles only the
 * missing or stale columns before asking the model.
 */

const props = withDefaults(
  defineProps<{
    targetGuid: string;
    metaType: MetaType;
    /** Columns to consider; empty lets the server read all of the target's. */
    columns?: string[];
    /** Whether the reader may run the deterministic column analysis. */
    canProfile?: boolean;
    size?: "sm" | "default";
    disabled?: boolean;
  }>(),
  { columns: () => [], canProfile: false, size: "default", disabled: false }
);

const emit = defineEmits<{
  /** A pass is starting; the parent can clear a stale error. */
  started: [];
  /** The pass finished; the queue may have new or refreshed proposals. */
  suggested: [response: SuggestColumnLabelsResponse];
  /** The pass failed; the message is already localized. */
  failed: [message: string];
}>();

/**
 * How old a stored profile may be and still evidence a label call. Mirrors the
 * server's `qualityLabelProfileMaxAgeDays`; the server re-checks it, so a
 * mismatch only costs an avoidable round trip, never a wrong verdict.
 */
const PROFILE_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;

type SuggestPhase = "" | "profiling" | "suggesting";

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const { notifySummary } = useSuggestionSummary();

const busy = ref(false);
const phase = ref<SuggestPhase>("");

const iconClass = computed(() =>
  props.size === "sm" ? "mr-1 h-4 w-4" : "mr-2 h-4 w-4"
);

const actionLabel = computed(() => {
  switch (phase.value) {
    case "profiling":
      return t("dataQuality.suggest.profiling");
    case "suggesting":
      return t("dataQuality.suggest.suggesting");
    default:
      return t("dataQuality.suggest.labels");
  }
});

/** A profile's measurement time in milliseconds, or 0 when it carries none. */
function profiledAtMillis(profile: {
  profiledAt?: { seconds?: bigint | number | null; nanos?: number | null };
}): number {
  const seconds = Number(profile.profiledAt?.seconds ?? 0);
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return 0;
  }
  return seconds * 1000 + (profile.profiledAt?.nanos ?? 0) / 1_000_000;
}

/** The requested columns whose stored profile is missing, empty or stale. */
async function staleColumns(columns: string[]): Promise<string[]> {
  const profiles = await listColumnProfiles({
    tableGuid: props.targetGuid,
    metaType: props.metaType,
    columns,
  });
  const byColumn = new Map(
    profiles.map((profile) => [profile.column, profile])
  );
  const now = Date.now();
  return columns.filter((column) => {
    const profile = byColumn.get(column);
    if (!profile?.profile) {
      return true;
    }
    return now - profiledAtMillis(profile) > PROFILE_MAX_AGE_MS;
  });
}

/** Analyze the columns the label pass would otherwise refuse to read. */
async function ensureProfiles(): Promise<boolean> {
  if (props.columns.length === 0) {
    // Without a column list there is nothing to check against; the server
    // decides the analysis set and applies its own freshness rule.
    return true;
  }
  phase.value = "profiling";
  const stale = await staleColumns(props.columns);
  if (stale.length === 0) {
    return true;
  }
  if (!props.canProfile) {
    emit("failed", t("dataQuality.suggest.profileRequired"));
    return false;
  }
  await profileColumns({
    tableGuid: props.targetGuid,
    metaType: props.metaType,
    columns: stale,
  });
  return true;
}

async function suggest(forceRegenerate: boolean) {
  if (busy.value || props.disabled) {
    return;
  }
  busy.value = true;
  emit("started");
  try {
    try {
      if (!(await ensureProfiles())) {
        return;
      }
    } catch (e: unknown) {
      emit("failed", formatError(e, "dataQuality.suggest.profileFailed"));
      return;
    }
    phase.value = "suggesting";
    const response = await suggestColumnLabels({
      tableGuid: props.targetGuid,
      metaType: props.metaType,
      columns: props.columns,
      language: locale.value,
      forceRegenerate,
    });
    notifySummary(
      response.summary,
      "label",
      response.qualitySuggestions.length
    );
    emit("suggested", response);
  } catch (e: unknown) {
    emit("failed", formatError(e, "dataQuality.suggest.failed"));
  } finally {
    busy.value = false;
    phase.value = "";
  }
}
</script>
