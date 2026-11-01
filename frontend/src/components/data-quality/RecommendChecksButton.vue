<template>
  <div class="flex">
    <Button
      variant="outline"
      :size="size"
      class="rounded-r-none"
      :disabled="busy || disabled"
      :aria-busy="busy"
      :title="t('dataQuality.recommend.labelHint')"
      @click="recommend(false)"
    >
      <Lightbulb :class="iconClass" />
      {{ actionLabel }}
    </Button>
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <Button
          variant="outline"
          :size="size"
          class="rounded-l-none border-l-0 px-2"
          :disabled="busy || disabled"
          :title="t('dataQuality.recommend.regenerateHint')"
          :aria-label="t('dataQuality.recommend.regenerate')"
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
          @select="recommend(true)"
        >
          <div class="space-y-1">
            <div class="flex items-center gap-2">
              <RefreshCw class="h-4 w-4" />
              {{ t("dataQuality.recommend.regenerate") }}
            </div>
            <p class="text-xs whitespace-normal text-muted-foreground">
              {{ t("dataQuality.recommend.regenerateHint") }}
            </p>
          </div>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  </div>
</template>

<script setup lang="ts">
import { ChevronDown, Lightbulb, RefreshCw } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  listColumnProfiles,
  profileColumns,
  recommendQualityChecks,
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
import type { RecommendQualityChecksResponse } from "@/types/proto-es/v1/data_quality_service_pb";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";

/**
 * The deterministic recommendation pass, as a primary action plus a split
 * "regenerate".
 *
 * It belongs to the monitoring/coverage context, not beside the model-assisted
 * label pass: this pass reads stored profiles only and works with the model off.
 * The surfaces that start it (the target workspace and the asset page's quality
 * tab) share this control, so the request chain and the queue summary cannot
 * drift between them. The pass only queues proposals; the parent decides how to
 * read the queue back and where a failure is shown.
 *
 * The server refuses to derive a check from an absent or stale profile, so the
 * control checks the stored profiles first and profiles only the missing or
 * stale columns before asking for recommendations.
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
  suggested: [response: RecommendQualityChecksResponse];
  /** The pass failed; the message is already localized. */
  failed: [message: string];
}>();

/**
 * How old a stored profile may be and still evidence a recommendation. Mirrors
 * the server's freshness bound; the server re-checks it, so a mismatch only
 * costs an avoidable round trip, never a wrong verdict.
 */
const PROFILE_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;

type RecommendPhase = "" | "profiling";

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const { notifySummary } = useSuggestionSummary();

const busy = ref(false);
const phase = ref<RecommendPhase>("");

const iconClass = computed(() =>
  props.size === "sm" ? "mr-1 h-4 w-4" : "mr-2 h-4 w-4"
);

const actionLabel = computed(() =>
  phase.value === "profiling"
    ? t("dataQuality.recommend.profiling")
    : t("dataQuality.recommend.label")
);

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

/** Measure the columns the recommendation pass would otherwise refuse to read. */
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
    emit("failed", t("dataQuality.recommend.profileRequired"));
    return false;
  }
  await profileColumns({
    tableGuid: props.targetGuid,
    metaType: props.metaType,
    columns: stale,
  });
  return true;
}

async function recommend(forceRegenerate: boolean) {
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
      emit("failed", formatError(e, "dataQuality.recommend.profileFailed"));
      return;
    }
    const response = await recommendQualityChecks({
      tableGuid: props.targetGuid,
      metaType: props.metaType,
      columns: props.columns,
      language: locale.value,
      forceRegenerate,
    });
    notifySummary(response.summary, "rule", response.qualitySuggestions.length);
    emit("suggested", response);
  } catch (e: unknown) {
    emit("failed", formatError(e, "dataQuality.recommend.failed"));
  } finally {
    busy.value = false;
    phase.value = "";
  }
}
</script>
