<template>
  <Card v-if="canView">
    <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-3 border-b">
      <div class="min-w-0 space-y-1">
        <CardTitle class="text-base">
          {{ t("dataQuality.overview.consumptionTitle") }}
        </CardTitle>
        <CardDescription>
          {{ t("dataQuality.overview.consumptionDescription") }}
        </CardDescription>
      </div>
      <Button
        v-if="canViewAll"
        variant="ghost"
        size="sm"
        class="shrink-0"
        @click="gapsOpen = true"
      >
        {{
          t("dataQuality.overview.consumptionViewAll", { count: gapCount })
        }}
      </Button>
    </CardHeader>
    <CardContent class="pt-4">
      <div
        v-if="loading"
        class="space-y-2"
        role="status"
        :aria-label="t('common.loading')"
      >
        <Skeleton class="h-8 w-32" />
        <Skeleton class="h-10 w-full" />
        <Skeleton class="h-10 w-full" />
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

      <template v-else-if="response">
        <div
          v-if="isEmpty"
          class="space-y-2"
        >
          <p class="text-2xl font-semibold tabular-nums">
            -
          </p>
          <EmptyState
            compact
            :icon="Waypoints"
            :title="t('dataQuality.overview.consumptionEmptyTitle')"
            :description="t('dataQuality.overview.consumptionEmptyDescription')"
          />
        </div>
        <template v-else>
          <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <span class="text-2xl font-semibold tabular-nums">
              {{
                t("dataQuality.overview.consumptionValue", {
                  monitored: response.monitoredAssetCount,
                  consumed: response.consumedAssetCount,
                })
              }}
            </span>
            <span class="text-sm text-muted-foreground tabular-nums">
              {{ percentText }}
            </span>
            <span
              v-if="gapCount > 0"
              class="text-xs text-muted-foreground"
            >
              {{
                t("dataQuality.overview.consumptionGapHint", {
                  count: gapCount,
                })
              }}
            </span>
          </div>
          <ul
            v-if="previewRows.length > 0"
            class="mt-3 divide-y"
          >
            <li
              v-for="row in previewRows"
              :key="row.key"
              class="flex min-h-12 items-center justify-between gap-3 py-2"
            >
              <RouterLink
                v-if="row.workspaceRoute"
                :to="row.workspaceRoute"
                class="block min-w-0 truncate text-sm font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
                :title="row.path"
              >
                {{ row.path }}
              </RouterLink>
              <span
                v-else
                class="block min-w-0 truncate text-sm font-medium"
                :title="row.path"
              >
                {{ row.path }}
              </span>
              <span class="shrink-0 text-xs text-muted-foreground tabular-nums">
                {{
                  t("dataQuality.overview.consumptionDownstream", {
                    count: row.downstreamAssetCount,
                  })
                }}
              </span>
            </li>
          </ul>
        </template>
      </template>
    </CardContent>

    <QualityConsumptionGapsDialog v-model:open="gapsOpen" />
  </Card>
</template>

<script setup lang="ts">
import { Waypoints } from "lucide-vue-next";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { getQualityConsumptionCoverage } from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useAuthStore } from "@/store/modules/auth";
import type { GetQualityConsumptionCoverageResponse } from "@/types/proto-es/v1/data_quality_service_pb";
import {
  type ConsumptionGapRow,
  consumptionGapRows,
} from "@/utils/dataQualityImpact";
import {
  coveragePercent,
  formatPercentValue,
} from "@/utils/dataQualityOverview";
import QualityConsumptionGapsDialog from "./QualityConsumptionGapsDialog.vue";

/**
 * The consumption side of coverage: of the assets whose lineage shows another
 * object reading them, how many have an enabled check?
 *
 * It is deliberately a different ratio from the overview's coverage card,
 * whose denominator is every monitorable object: being read by someone is what
 * makes an asset's quality someone else's problem, so this card counts only
 * recorded consumers. An empty lineage is not 0% coverage — it is no measured
 * population at all, which is why the value is "-" and the card says why.
 *
 * The read is independent of the overview's: it starts on mount, does not
 * block the page, and a failure stays inside this card. The page's refresh
 * button bumps `refreshKey` to re-read it with everything else.
 */

const props = withDefaults(
  defineProps<{
    /** Bumped by the page's refresh action to re-read the card. */
    refreshKey?: number;
  }>(),
  { refreshKey: 0 }
);

const { t } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

/** How many gaps the card previews; the dialog lists the rest. */
const PREVIEW_SIZE = 5;

const canView = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.list")
);

const response = ref<GetQualityConsumptionCoverageResponse>();
const loading = ref(false);
const error = ref("");
const gapsOpen = ref(false);

/** Identifies the newest read; a superseded answer must never write state. */
let generation = 0;
let controller: AbortController | undefined;

const isEmpty = computed(() => (response.value?.consumedAssetCount ?? 0) <= 0);

const gapCount = computed(() => response.value?.gapCount ?? 0);

const previewRows = computed<ConsumptionGapRow[]>(() =>
  response.value
    ? consumptionGapRows(response.value.gaps).slice(0, PREVIEW_SIZE)
    : []
);

const percentText = computed(() =>
  response.value
    ? formatPercentValue(
        coveragePercent(
          response.value.monitoredAssetCount,
          response.value.consumedAssetCount
        )
      )
    : ""
);

/** "View all" is only meaningful while the server reports unmonitored assets. */
const canViewAll = computed(
  () => response.value !== undefined && gapCount.value > 0
);

async function load() {
  if (!canView.value) {
    generation += 1;
    controller?.abort();
    controller = undefined;
    response.value = undefined;
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
    const loaded = await getQualityConsumptionCoverage({
      pageSize: PREVIEW_SIZE,
      signal,
    });
    if (current !== generation || signal.aborted) {
      return;
    }
    response.value = loaded;
  } catch (e: unknown) {
    if (current !== generation || signal.aborted) {
      return;
    }
    response.value = undefined;
    error.value = formatError(e, "dataQuality.overview.consumptionFetchError");
  } finally {
    if (current === generation) {
      loading.value = false;
    }
  }
}

// One read on mount (and whenever the page refreshes); a caller without the
// rule-catalog permission renders nothing and reads nothing.
watch([canView, () => props.refreshKey], () => void load(), {
  immediate: true,
});

onBeforeUnmount(() => {
  generation += 1;
  controller?.abort();
});
</script>
