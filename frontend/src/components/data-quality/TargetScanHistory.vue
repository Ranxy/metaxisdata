<template>
  <div class="space-y-3 py-3 pr-4 pl-6">
    <h3 class="text-sm font-medium">
      {{ t("dataQuality.executions.historyTitle") }}
    </h3>

    <PageState
      :loading="isLoading && runs.length === 0"
      :error="error"
      :title="t('dataQuality.executions.historyLoadFailed')"
    >
      <template #error>
        <Button
          variant="outline"
          size="sm"
          @click="load"
        >
          {{ t("common.refresh") }}
        </Button>
      </template>

      <EmptyState
        v-if="runs.length === 0"
        :icon="History"
        compact
        :title="t('dataQuality.executions.historyEmpty')"
        :description="t('dataQuality.executions.historyEmptyHint')"
      />

      <div
        v-else
        class="space-y-2"
      >
        <QualityRunCard
          v-for="run in runs"
          :key="run.name"
          :run="run"
          :target-guid="targetGuid"
          :rule-titles="ruleTitles"
          :target-rule-count="targetRuleCount"
          :model-value="expandedRun === run.name"
          @update:model-value="(open) => toggleRun(run.name, open)"
        />
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import { History } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listQualityRuns } from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import QualityRunCard from "@/components/data-quality/QualityRunCard.vue";
import { Button } from "@/components/ui/button";
import { useErrorHandler } from "@/composables/useErrorHandler";
import type { QualityRun } from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";

/**
 * A target's scan history, newest batch first, lazily loaded under its directory
 * row. One card per run; each card can open its checks without leaving the page.
 */

const props = withDefaults(
  defineProps<{
    targetGuid: string;
    /** The target's object type; runs are filtered by target identity. */
    metaType?: MetaType;
    /** Rule resource name -> title, passed through to the run cards. */
    ruleTitles?: Record<string, string>;
    /** The target's live rule count, so a due-only batch can say 3/5 checks. */
    targetRuleCount?: number;
    /** The run whose checks are open, carried by `?run=`. */
    expandRun?: string;
  }>(),
  {
    ruleTitles: () => ({}),
    targetRuleCount: 0,
    expandRun: "",
    metaType: MetaType.UNSPECIFIED,
  }
);

const emit = defineEmits<{
  "update:expandRun": [name: string];
}>();

const { t } = useI18n();
const { handleError } = useErrorHandler();

const runs = ref<QualityRun[]>([]);
const isLoading = ref(true);
const error = ref("");

const expandedRun = computed(() => props.expandRun);

function toggleRun(name: string, open: boolean) {
  emit("update:expandRun", open ? name : "");
}

async function load() {
  isLoading.value = true;
  error.value = "";
  try {
    const page = await listQualityRuns({
      targetGuid: props.targetGuid,
      metaType: props.metaType,
      pageSize: 20,
    });
    runs.value = page.items;
  } catch (failure: unknown) {
    error.value = handleError(
      failure,
      "dataQuality.executions.historyLoadFailed"
    );
    runs.value = [];
  } finally {
    isLoading.value = false;
  }
}

onMounted(load);

// The row stays mounted while the URL's target changes (back/forward, a pasted
// link); a new target needs its own history.
watch(
  () => props.targetGuid,
  () => void load()
);
</script>
