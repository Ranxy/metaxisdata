<template>
  <Dialog
    :open="open"
    @update:open="(value: boolean) => emit('update:open', value)"
  >
    <DialogContent
      class="left-auto right-0 top-0 h-dvh max-h-dvh w-full max-w-2xl translate-x-0 translate-y-0 gap-0 overflow-hidden rounded-none border-l p-0 sm:max-w-2xl sm:rounded-none"
    >
      <div class="flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>{{ t("dataQuality.contract.historyTitle") }}</DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.contract.description") }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-3 overflow-auto px-6 py-5">
          <Alert
            v-if="listError"
            variant="destructive"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>{{ t("dataQuality.contract.loadFailed") }}</AlertTitle>
            <AlertDescription class="flex items-center justify-between gap-3">
              <span class="break-words">{{ listError }}</span>
              <Button
                variant="outline"
                size="sm"
                @click="refresh"
              >
                {{ t("common.retry") }}
              </Button>
            </AlertDescription>
          </Alert>

          <div
            v-if="isLoading && revisions.length === 0"
            class="flex justify-center p-10"
          >
            <Loader2 class="h-5 w-5 animate-spin text-muted-foreground" />
          </div>

          <EmptyState
            v-else-if="revisions.length === 0 && !listError"
            :icon="Inbox"
            :title="t('dataQuality.contract.historyEmpty')"
          />

          <div
            v-for="revision in revisions"
            :key="revision.version"
            class="rounded-md border"
          >
            <div class="flex flex-wrap items-start justify-between gap-2 p-3">
              <div class="min-w-0 space-y-1">
                <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <Badge variant="secondary">
                    {{
                      t("dataQuality.contract.statePublished", {
                        version: revision.version,
                      })
                    }}
                  </Badge>
                  <span class="text-sm font-medium">
                    {{
                      t("dataQuality.contract.checkCount", {
                        count: revision.checkCount,
                      })
                    }}
                  </span>
                  <span
                    class="text-xs text-muted-foreground"
                    :title="formatDateTime(revision.publishedAt, locale)"
                  >
                    {{ formatRelativeTime(revision.publishedAt, locale) }}
                  </span>
                  <span
                    v-if="revision.publishedBy"
                    class="inline-flex items-center gap-1 text-xs text-muted-foreground"
                    :title="revision.publishedBy"
                  >
                    <UserRound class="h-3.5 w-3.5" />
                    {{ emailOf(revision.publishedBy) }}
                  </span>
                </div>
                <p
                  v-if="revision.note"
                  class="text-sm break-words"
                >
                  {{ revision.note }}
                </p>
                <div class="flex flex-wrap items-center gap-1.5">
                  <Badge
                    v-if="(revision.changeSummary?.added ?? 0) > 0"
                    variant="success"
                  >
                    {{
                      t("dataQuality.contract.changeAdded", {
                        count: revision.changeSummary?.added ?? 0,
                      })
                    }}
                  </Badge>
                  <Badge
                    v-if="(revision.changeSummary?.modified ?? 0) > 0"
                    variant="warning"
                  >
                    {{
                      t("dataQuality.contract.changeModified", {
                        count: revision.changeSummary?.modified ?? 0,
                      })
                    }}
                  </Badge>
                  <Badge
                    v-if="(revision.changeSummary?.removed ?? 0) > 0"
                    variant="neutral"
                  >
                    {{
                      t("dataQuality.contract.changeRemoved", {
                        count: revision.changeSummary?.removed ?? 0,
                      })
                    }}
                  </Badge>
                </div>
              </div>

              <Button
                variant="ghost"
                size="sm"
                class="shrink-0"
                :aria-expanded="expandedVersion === revision.version"
                @click="toggleDiff(revision)"
              >
                {{
                  t("dataQuality.contract.diffTitle", {
                    version: revision.version,
                  })
                }}
              </Button>
            </div>

            <div
              v-if="expandedVersion === revision.version"
              class="space-y-3 border-t p-3"
            >
              <div
                v-if="diffLoading === revision.version"
                class="flex items-center gap-2 text-sm text-muted-foreground"
              >
                <Loader2 class="h-4 w-4 animate-spin" />
                {{ t("common.loading") }}
              </div>

              <p
                v-else-if="diffError"
                class="text-sm break-words text-destructive"
              >
                {{ diffError }}
              </p>

              <div
                v-else-if="diff"
                class="space-y-3"
              >
                <p
                  v-if="diffEmpty"
                  class="text-sm text-muted-foreground"
                >
                  {{ t("dataQuality.contract.diffEmpty") }}
                </p>
                <template v-else>
                  <section
                    v-for="group in diffGroups"
                    :key="group.label"
                    class="space-y-1"
                  >
                    <div class="text-xs font-medium text-muted-foreground">
                      {{ group.label }}
                    </div>
                    <ul class="space-y-1">
                      <li
                        v-for="check in group.checks"
                        :key="check.rule"
                        class="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm"
                      >
                        <span class="min-w-0 truncate">
                          {{ check.title || check.rule }}
                        </span>
                        <Badge variant="outline">
                          {{ t(qualityRuleTypeKey(check.ruleType)) }}
                        </Badge>
                        <span
                          v-if="check.column"
                          class="font-mono text-xs text-muted-foreground"
                        >
                          {{ check.column }}
                        </span>
                      </li>
                    </ul>
                  </section>
                </template>
              </div>
            </div>
          </div>
        </div>

        <TablePager
          v-if="revisions.length > 0"
          class="shrink-0 px-6 pb-2"
          :page-size="pageSize"
          :has-previous="hasPrevious"
          :has-next="hasNext"
          :disabled="isLoading"
          @update:page-size="onPageSizeChange"
          @previous="goPrevious"
          @next="goNext"
        />
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { AlertTriangle, Inbox, Loader2, UserRound } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  getQualityContractRevision,
  listQualityContractRevisions,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import TablePager from "@/components/common/TablePager.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { useUserDirectory } from "@/composables/useUserDirectory";
import {
  type QualityContractCheck,
  type QualityContractRevision,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { qualityRuleTypeKey } from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/**
 * Every published version of the target's contract, newest first, with one
 * version's diff against its predecessor expanded in place.
 *
 * A revision stores only metadata; its checks are a separate read. Comparing the
 * two snapshots is therefore one request per version shown, which is why a diff
 * loads on demand rather than with the list.
 */

const PAGE_SIZE = 20;

/** The difference between one published version and the one before it. */
interface ContractDiff {
  added: QualityContractCheck[];
  modified: QualityContractCheck[];
  removed: QualityContractCheck[];
}

const props = defineProps<{
  open: boolean;
  targetGuid: string;
  metaType: MetaType;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const { emailOf, load: loadDirectory } = useUserDirectory();

const pageSize = ref(PAGE_SIZE);
const listError = ref("");
const expandedVersion = ref(0);
const diffLoading = ref(0);
const diffError = ref("");
const diff = ref<ContractDiff>();

const {
  items: revisions,
  isLoading,
  hasNext,
  hasPrevious,
  reset,
  refresh,
  goNext,
  goPrevious,
} = usePagedFetch<QualityContractRevision>({
  fetchPage: async (pageToken, signal) => {
    listError.value = "";
    return await listQualityContractRevisions({
      targetGuid: props.targetGuid,
      metaType: props.metaType,
      pageSize: pageSize.value,
      pageToken,
      signal,
    });
  },
  onError: (e: unknown) => {
    listError.value = formatError(e, "dataQuality.contract.loadFailed");
  },
});

const diffGroups = computed(() => {
  const value = diff.value;
  if (!value) {
    return [];
  }
  return [
    {
      label: t("dataQuality.contract.diffAdded"),
      checks: value.added,
    },
    {
      label: t("dataQuality.contract.diffModified"),
      checks: value.modified,
    },
    {
      label: t("dataQuality.contract.diffRemoved"),
      checks: value.removed,
    },
  ].filter((group) => group.checks.length > 0);
});

const diffEmpty = computed(
  () =>
    diff.value !== undefined &&
    diff.value.added.length === 0 &&
    diff.value.modified.length === 0 &&
    diff.value.removed.length === 0
);

/**
 * What changed from the version before: a check is the same rule, so added and
 * removed are set differences while modified means the recorded revision, title,
 * severity or watched column moved.
 */
function diffChecks(
  previous: readonly QualityContractCheck[],
  current: readonly QualityContractCheck[]
): ContractDiff {
  const before = new Map(previous.map((check) => [check.rule, check]));
  const after = new Map(current.map((check) => [check.rule, check]));
  return {
    added: current.filter((check) => !before.has(check.rule)),
    modified: current.filter((check) => {
      const old = before.get(check.rule);
      return (
        old !== undefined &&
        (old.ruleRevision !== check.ruleRevision ||
          old.title !== check.title ||
          old.severity !== check.severity ||
          old.column !== check.column)
      );
    }),
    removed: previous.filter((check) => !after.has(check.rule)),
  };
}

async function toggleDiff(revision: QualityContractRevision) {
  if (expandedVersion.value === revision.version) {
    expandedVersion.value = 0;
    return;
  }
  expandedVersion.value = revision.version;
  diff.value = undefined;
  diffError.value = "";
  diffLoading.value = revision.version;
  try {
    const current = await getQualityContractRevision({
      targetGuid: props.targetGuid,
      metaType: props.metaType,
      version: revision.version,
    });
    // Version 1 has no predecessor: every check in it is new.
    let previous: QualityContractCheck[] = [];
    if (revision.version > 1) {
      const before = await getQualityContractRevision({
        targetGuid: props.targetGuid,
        metaType: props.metaType,
        version: revision.version - 1,
      });
      previous = before.checks;
    }
    if (expandedVersion.value !== revision.version) {
      return;
    }
    diff.value = diffChecks(previous, current.checks);
  } catch (e: unknown) {
    if (expandedVersion.value !== revision.version) {
      return;
    }
    diffError.value = formatError(e, "dataQuality.contract.loadFailed");
  } finally {
    if (diffLoading.value === revision.version) {
      diffLoading.value = 0;
    }
  }
}

function onPageSizeChange(value: number) {
  pageSize.value = value;
  expandedVersion.value = 0;
  void reset();
}

watch(
  () => props.open,
  (isOpen) => {
    if (!isOpen) {
      return;
    }
    expandedVersion.value = 0;
    diff.value = undefined;
    diffError.value = "";
    listError.value = "";
    void loadDirectory();
    void reset();
  },
  { immediate: true }
);
</script>
