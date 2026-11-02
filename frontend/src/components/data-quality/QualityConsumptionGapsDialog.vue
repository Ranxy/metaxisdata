<template>
  <Dialog
    :open="open"
    @update:open="(value: boolean) => emit('update:open', value)"
  >
    <DialogContent class="max-h-[90vh] gap-0 overflow-hidden p-0 sm:max-w-2xl">
      <div class="flex max-h-[90vh] min-h-0 flex-col">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>{{ t("dataQuality.consumptionGaps.title") }}</DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.consumptionGaps.description") }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-3 overflow-auto px-6 py-5">
          <div
            v-if="loading"
            class="space-y-2"
            role="status"
            :aria-label="t('common.loading')"
          >
            <Skeleton class="h-10 w-full" />
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
            <p
              v-if="rows.length === 0"
              class="text-sm text-muted-foreground"
            >
              {{ t("dataQuality.consumptionGaps.empty") }}
            </p>
            <ul
              v-else
              class="divide-y"
            >
              <li
                v-for="row in rows"
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
            <p
              v-if="hasMore"
              class="text-xs text-muted-foreground"
            >
              {{
                t("dataQuality.consumptionGaps.limitHint", {
                  shown: rows.length,
                  total: response.gapCount,
                })
              }}
            </p>
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
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { getQualityConsumptionCoverage } from "@/api/data-quality";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useAuthStore } from "@/store/modules/auth";
import type { GetQualityConsumptionCoverageResponse } from "@/types/proto-es/v1/data_quality_service_pb";
import { consumptionGapRows } from "@/utils/dataQualityImpact";

/**
 * Every unmonitored consumed asset, up to the server's 100-row cap.
 *
 * The preview card shows the top five; this dialog is the full gap list. It
 * reads on open rather than with the card, so opening the page costs one page
 * size of gaps, not a hundred. A failure stays inside the dialog — the card
 * behind it keeps its own numbers.
 */

const props = defineProps<{ open: boolean }>();

const emit = defineEmits<{ "update:open": [open: boolean] }>();

const { t } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const canView = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.list")
);

const response = ref<GetQualityConsumptionCoverageResponse>();
const loading = ref(false);
const error = ref("");

/** Identifies the newest read; a stale answer must never write state. */
let generation = 0;
let controller: AbortController | undefined;

const rows = computed(() =>
  response.value ? consumptionGapRows(response.value.gaps) : []
);

/** Whether the server holds more gaps than this dialog could list. */
const hasMore = computed(
  () => (response.value?.gapCount ?? 0) > (response.value?.gaps.length ?? 0)
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
      pageSize: 100,
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
    error.value = formatError(e, "dataQuality.consumptionGaps.fetchError");
  } finally {
    if (current === generation) {
      loading.value = false;
    }
  }
}

// Opening reads; closing aborts an answer the reader no longer waits for. The
// immediate run covers a dialog mounted already open (a test or a deep link).
watch(
  () => props.open,
  (open) => {
    if (open) {
      void load();
      return;
    }
    generation += 1;
    controller?.abort();
    controller = undefined;
  },
  { immediate: true }
);

onBeforeUnmount(() => {
  generation += 1;
  controller?.abort();
});
</script>
