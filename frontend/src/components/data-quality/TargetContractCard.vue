<template>
  <section class="rounded-md border px-3 py-2">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-sm">
        <ShieldCheck class="h-4 w-4 shrink-0 text-muted-foreground" />
        <span class="font-medium">{{ t("dataQuality.contract.title") }}</span>

        <Loader2
          v-if="loading"
          class="h-3.5 w-3.5 animate-spin text-muted-foreground"
        />
        <template v-else-if="error">
          <span class="text-destructive">
            {{ t("dataQuality.contract.loadFailed") }}
          </span>
          <Button
            variant="ghost"
            size="sm"
            class="h-7 px-2"
            @click="emit('retry')"
          >
            {{ t("common.retry") }}
          </Button>
        </template>
        <template v-else-if="contract">
          <template v-if="contract.publishedVersion === 0">
            <Badge variant="neutral">
              {{ t("dataQuality.contract.stateNeverPublished") }}
            </Badge>
            <span class="text-xs text-muted-foreground">
              {{ t("dataQuality.contract.neverPublishedHint") }}
            </span>
          </template>
          <template v-else>
            <Badge variant="success">
              {{
                t("dataQuality.contract.statePublished", {
                  version: contract.publishedVersion,
                })
              }}
            </Badge>
            <span
              class="text-xs text-muted-foreground"
              :title="publishedAtAbsolute"
            >
              {{
                t("dataQuality.contract.publishedAt", {
                  time: publishedAtRelative,
                })
              }}
            </span>
            <span
              v-if="publisherName"
              class="inline-flex items-center gap-1 text-xs text-muted-foreground"
              :title="contract.publishedBy"
            >
              <UserRound class="h-3.5 w-3.5" />
              {{ publisherName }}
            </span>
            <!-- The drift badge counts rule changes only: a settings edit can make
                 the contract unpublished with nothing to add, change or remove. -->
            <Badge
              v-if="contract.hasUnpublishedChanges"
              variant="warning"
            >
              {{
                t("dataQuality.contract.unpublishedChanges", {
                  count: changeCount,
                })
              }}
            </Badge>
          </template>
          <span
            v-if="hasTargetFilter"
            class="inline-flex items-center gap-1 text-xs text-muted-foreground"
          >
            <Filter class="h-3.5 w-3.5" />
            {{ t("dataQuality.contract.filterApplies") }}
          </span>
        </template>
      </div>

      <div
        v-if="contract && !error"
        class="flex shrink-0 flex-wrap items-center gap-1.5"
      >
        <Button
          v-if="canUpdate"
          variant="ghost"
          size="sm"
          @click="settingsOpen = true"
        >
          {{ t("dataQuality.contract.settings") }}
        </Button>
        <Button
          v-if="contract.publishedVersion > 0"
          variant="ghost"
          size="sm"
          @click="historyOpen = true"
        >
          {{ t("dataQuality.contract.history") }}
        </Button>
        <Button
          variant="outline"
          size="sm"
          :disabled="!canPublish || !contract.hasUnpublishedChanges"
          :title="publishHint"
          @click="publishOpen = true"
        >
          {{ t("dataQuality.contract.publish") }}
        </Button>
      </div>
    </div>

    <ContractSettingsDialog
      v-if="contract && !error"
      v-model:open="settingsOpen"
      :target-guid="targetGuid"
      :meta-type="metaType"
      :settings="contract.settings"
      @updated="emit('updated', $event)"
    />
    <ContractPublishDialog
      v-if="contract && !error"
      v-model:open="publishOpen"
      :target-guid="targetGuid"
      :meta-type="metaType"
      :contract="contract"
      @published="emit('published', $event)"
    />
    <ContractHistoryDrawer
      v-if="!error"
      v-model:open="historyOpen"
      :target-guid="targetGuid"
      :meta-type="metaType"
    />
  </section>
</template>

<script setup lang="ts">
import { Filter, Loader2, ShieldCheck, UserRound } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import ContractHistoryDrawer from "@/components/data-quality/ContractHistoryDrawer.vue";
import ContractPublishDialog from "@/components/data-quality/ContractPublishDialog.vue";
import ContractSettingsDialog from "@/components/data-quality/ContractSettingsDialog.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useUserDirectory } from "@/composables/useUserDirectory";
import type { QualityContract } from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * The target's contract as one compact line: whether anything is published, when
 * and by whom, and how far the live rules have drifted from that promise.
 *
 * The card owns its three entry points (publish, settings, history) but not the
 * contract itself: the page reads it next to the workspace and hands the updated
 * one back after a publish or a settings save, so the strip never re-reads.
 */

const props = withDefaults(
  defineProps<{
    targetGuid: string;
    metaType: MetaType;
    contract?: QualityContract;
    loading?: boolean;
    error?: string;
    canPublish?: boolean;
    canUpdate?: boolean;
  }>(),
  { loading: false, error: "", canPublish: false, canUpdate: false }
);

const emit = defineEmits<{
  /** The server returned the contract after a publish. */
  published: [contract: QualityContract];
  /** The server returned the contract after a settings save. */
  updated: [contract: QualityContract];
  /** The contract read failed; the page re-reads it. */
  retry: [];
}>();

const { t, locale } = useI18n();
const { emailOf, load: loadDirectory } = useUserDirectory();

const settingsOpen = ref(false);
const publishOpen = ref(false);
const historyOpen = ref(false);

onMounted(() => {
  void loadDirectory();
});

const publishedAtRelative = computed(() =>
  formatRelativeTime(props.contract?.publishedAt, locale.value)
);
const publishedAtAbsolute = computed(() =>
  formatDateTime(props.contract?.publishedAt, locale.value, { seconds: true })
);

const publisherName = computed(() =>
  props.contract?.publishedBy ? emailOf(props.contract.publishedBy) : ""
);

/** The rule changes the drift badge counts; settings drift adds no rows here. */
const changeCount = computed(() => {
  const summary = props.contract?.changeSummary;
  return (
    (summary?.added ?? 0) + (summary?.modified ?? 0) + (summary?.removed ?? 0)
  );
});

const hasTargetFilter = computed(
  () => (props.contract?.settings?.filter?.clauses.length ?? 0) > 0
);

/** Why Publish cannot be pressed; absent when it can. */
const publishHint = computed(() => {
  if (!props.canPublish) {
    return t("dataQuality.contract.noPermission");
  }
  if (!props.contract?.hasUnpublishedChanges) {
    return t("dataQuality.contract.publishNoChanges");
  }
  return undefined;
});
</script>
