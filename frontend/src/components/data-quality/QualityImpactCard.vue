<template>
  <Card v-if="canView && hasTarget">
    <CardHeader class="border-b">
      <div class="min-w-0 space-y-1">
        <CardTitle class="text-base">
          {{ t("dataQuality.impact.title") }}
        </CardTitle>
        <CardDescription>
          {{ t("dataQuality.impact.description") }}
        </CardDescription>
      </div>
    </CardHeader>
    <CardContent class="space-y-5 pt-4">
      <div
        v-if="loading"
        class="space-y-3"
        role="status"
        :aria-label="t('common.loading')"
      >
        <Skeleton class="h-4 w-40" />
        <Skeleton class="h-14 w-full" />
        <Skeleton class="h-14 w-full" />
      </div>

      <div
        v-else-if="error"
        class="flex flex-col items-start gap-2"
      >
        <p class="text-sm text-muted-foreground">
          {{ error }}
        </p>
        <Button
          variant="outline"
          size="sm"
          @click="load"
        >
          {{ t("common.retry") }}
        </Button>
      </div>

      <template v-else-if="impact">
        <!-- The two directions are separate sections because they answer
             different questions: what this failure may have come from, and
             what it may break. Both are bounded lineage walks, so an empty
             list says "nothing is recorded", never "nothing exists". -->
        <section class="space-y-1">
          <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
            <h3 class="text-sm font-medium">
              {{ t("dataQuality.impact.upstreamTitle") }}
            </h3>
            <p class="text-xs text-muted-foreground">
              {{ t(windowText.key, windowText.params) }}
            </p>
          </div>
          <p
            v-if="rootCauses.length === 0"
            class="py-2 text-sm text-muted-foreground"
          >
            {{ t("dataQuality.impact.upstreamEmpty") }}
          </p>
          <ul
            v-else
            class="divide-y"
          >
            <li
              v-for="row in rootCauses"
              :key="row.key"
              class="flex min-h-14 items-center justify-between gap-3 py-2"
            >
              <div class="min-w-0">
                <RouterLink
                  v-if="row.metadataRoute"
                  :to="row.metadataRoute"
                  class="block truncate text-sm font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
                  :title="row.path"
                >
                  {{ row.path }}
                </RouterLink>
                <span
                  v-else
                  class="block truncate text-sm font-medium"
                  :title="row.path"
                >
                  {{ row.path }}
                </span>
                <div class="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                  <span>{{ t(row.distance.key, row.distance.params) }}</span>
                  <Badge
                    v-if="row.failingRuleCount > 0"
                    variant="destructive"
                  >
                    {{
                      t("dataQuality.impact.failingChecks", {
                        count: row.failingRuleCount,
                      })
                    }}
                  </Badge>
                  <Badge
                    v-if="row.failingExecutionCount > 0"
                    variant="outline"
                  >
                    {{
                      t("dataQuality.impact.failingRuns", {
                        count: row.failingExecutionCount,
                      })
                    }}
                  </Badge>
                  <Badge
                    v-if="row.structureChangedTime"
                    variant="warning"
                    :title="formatDateTime(row.structureChangedTime, locale)"
                  >
                    {{
                      t("dataQuality.impact.structureChanged", {
                        time: formatRelativeTime(row.structureChangedTime, locale),
                      })
                    }}
                  </Badge>
                </div>
              </div>
              <RouterLink
                v-if="row.executionRoute"
                :to="row.executionRoute"
                class="shrink-0 text-xs text-primary hover:underline"
              >
                {{ t("dataQuality.impact.viewFailingRun") }}
              </RouterLink>
            </li>
          </ul>
        </section>

        <section class="space-y-1">
          <h3 class="text-sm font-medium">
            {{ t("dataQuality.impact.downstreamTitle") }}
          </h3>
          <p
            v-if="downstream.length === 0"
            class="py-2 text-sm text-muted-foreground"
          >
            {{ t("dataQuality.impact.downstreamEmpty") }}
          </p>
          <ul
            v-else
            class="divide-y"
          >
            <li
              v-for="row in downstream"
              :key="row.key"
              class="flex min-h-14 items-center justify-between gap-3 py-2"
            >
              <div class="min-w-0">
                <RouterLink
                  v-if="row.metadataRoute"
                  :to="row.metadataRoute"
                  class="block truncate text-sm font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
                  :title="row.path"
                >
                  {{ row.path }}
                </RouterLink>
                <span
                  v-else
                  class="block truncate text-sm font-medium"
                  :title="row.path"
                >
                  {{ row.path }}
                </span>
                <div class="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                  <span>{{ t(row.distance.key, row.distance.params) }}</span>
                  <Badge :variant="monitoringBadge(row).variant">
                    {{ monitoringBadge(row).label }}
                  </Badge>
                  <Badge
                    v-if="showIssueCount(row)"
                    variant="danger"
                  >
                    {{
                      t("dataQuality.signal.issues", {
                        count: row.summary?.openIssueCount ?? 0,
                      })
                    }}
                  </Badge>
                  <span
                    v-if="showLastRun(row)"
                    :title="formatDateTime(row.summary?.lastRunTime, locale)"
                  >
                    {{
                      t("dataQuality.signal.lastRun", {
                        time: formatRelativeTime(row.summary?.lastRunTime, locale),
                      })
                    }}
                  </span>
                </div>
              </div>
            </li>
          </ul>
        </section>
      </template>
    </CardContent>
    <CardFooter
      v-if="!loading && !error && footnotes.length > 0"
      class="flex flex-col items-start gap-1 border-t pt-3 text-xs text-muted-foreground"
    >
      <p
        v-for="note in footnotes"
        :key="note.key"
      >
        {{ t(note.key, note.params) }}
      </p>
    </CardFooter>
  </Card>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { getQualityTargetImpact } from "@/api/data-quality";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useAuthStore } from "@/store/modules/auth";
import {
  type GetQualityTargetImpactResponse,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  type QualityBadgeVariant,
  qualityStatusKey,
  qualityStatusVariant,
} from "@/utils/dataQuality";
import {
  IMPACT_DEPTH,
  IMPACT_WINDOW_HOURS,
  type ImpactDownstreamRow,
  type ImpactText,
  impactDownstreamRows,
  impactFootnotes,
  impactRootCauseRows,
  impactWindowText,
} from "@/utils/dataQualityImpact";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * One target's lineage impact: the assets its data reaches downstream, and the
 * upstream targets that may explain a failure.
 *
 * The lists come from recorded lineage, so they are evidence, not a census:
 * the description says so, an empty list says nothing is recorded, and the
 * footnotes say when the server's bounded walk was cut short. A downstream
 * asset with no check is "unmonitored", never green; one whose checks are all
 * paused is not being verified either.
 *
 * A caller without the rule-catalog permission renders nothing and sends no
 * request. A target that cannot be addressed (no GUID or no metadata type) has
 * nothing to read, so the card stays out of the layout rather than guessing.
 */

const props = withDefaults(
  defineProps<{
    targetGuid: string;
    metaType: MetaType;
    /** The instant the root-cause window ends; unset means "now". */
    asOf?: Timestamp;
  }>(),
  { asOf: undefined }
);

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const canView = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.list")
);

const hasTarget = computed(
  () =>
    props.targetGuid.trim() !== "" && props.metaType !== MetaType.UNSPECIFIED
);

const impact = ref<GetQualityTargetImpactResponse>();
const loading = ref(false);
const error = ref("");

/** Identifies the newest load; a superseded answer must never write state. */
let generation = 0;
let controller: AbortController | undefined;

const downstream = computed<ImpactDownstreamRow[]>(() =>
  impact.value ? impactDownstreamRows(impact.value) : []
);

const rootCauses = computed(() =>
  impact.value ? impactRootCauseRows(impact.value) : []
);

const footnotes = computed<ImpactText[]>(() =>
  impact.value ? impactFootnotes(impact.value) : []
);

const windowText = computed<ImpactText>(() =>
  impact.value
    ? impactWindowText(impact.value)
    : {
        key: "dataQuality.impact.window",
        params: { hours: IMPACT_WINDOW_HOURS },
      }
);

/**
 * The three-state monitoring badge. A monitored target with no judged result
 * yet is "never run", not "unspecified": the enum has no verdict to show.
 */
function monitoringBadge(row: ImpactDownstreamRow): {
  variant: QualityBadgeVariant;
  label: string;
} {
  if (row.monitoring === "unmonitored") {
    return { variant: "neutral", label: t("dataQuality.signal.notMonitored") };
  }
  if (row.monitoring === "allPaused") {
    return { variant: "neutral", label: t("dataQuality.signal.allPaused") };
  }
  const status = row.summary?.worstStatus ?? QualityStatus.UNSPECIFIED;
  if (status === QualityStatus.UNSPECIFIED) {
    return { variant: "neutral", label: t("dataQuality.targets.neverRun") };
  }
  return {
    variant: qualityStatusVariant(status),
    label: t(qualityStatusKey(status)),
  };
}

/** Only a monitored asset's open issues are evidence about a current result. */
function showIssueCount(row: ImpactDownstreamRow): boolean {
  return (
    row.monitoring === "monitored" && (row.summary?.openIssueCount ?? 0) > 0
  );
}

/** Only a monitored asset has a meaningful last run to show. */
function showLastRun(row: ImpactDownstreamRow): boolean {
  return (
    row.monitoring === "monitored" && row.summary?.lastRunTime !== undefined
  );
}

async function load() {
  if (!canView.value || !hasTarget.value) {
    generation += 1;
    controller?.abort();
    controller = undefined;
    impact.value = undefined;
    loading.value = false;
    error.value = "";
    return;
  }

  const current = ++generation;
  controller?.abort();
  controller = new AbortController();
  const { signal } = controller;
  loading.value = true;
  error.value = "";

  try {
    const response = await getQualityTargetImpact({
      targetGuid: props.targetGuid.trim(),
      metaType: props.metaType,
      depth: IMPACT_DEPTH,
      asOf: props.asOf,
      windowHours: IMPACT_WINDOW_HOURS,
      signal,
    });
    if (current !== generation || signal.aborted) {
      return;
    }
    impact.value = response;
  } catch (e: unknown) {
    if (current !== generation || signal.aborted) {
      return;
    }
    // Keep the previous answer off the screen: it describes another instant.
    impact.value = undefined;
    error.value = formatError(e, "dataQuality.impact.fetchError");
  } finally {
    if (current === generation) {
      loading.value = false;
    }
  }
}

// A route change to another target (or another issue/execution on the same
// component) must re-read, and the permission can only change by signing in
// again; watching it keeps a stale denial from outliving the session.
watch(
  [() => props.targetGuid, () => props.metaType, () => props.asOf, canView],
  () => void load(),
  { immediate: true }
);

onBeforeUnmount(() => {
  generation += 1;
  controller?.abort();
});
</script>
