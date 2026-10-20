<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.runsTitle')"
      :description="t('dataQuality.runsDescription')"
    >
      <template #actions>
        <Button
          variant="outline"
          :disabled="isLoading"
          @click="refreshPage"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': isLoading }"
          />
          {{ t("common.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <Card>
      <PageState
        :loading="isLoading"
        :error="error"
      >
        <EmptyState
          v-if="runs.length === 0"
          :icon="Activity"
          :title="t('dataQuality.noRuns')"
          :description="t('dataQuality.noRunsHint')"
        >
          <Button
            variant="outline"
            size="sm"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualityRules' }">
              {{ t("dataQuality.viewRules") }}
            </RouterLink>
          </Button>
        </EmptyState>

        <div v-else>
          <Table class="[&_th]:whitespace-nowrap">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("dataQuality.run") }}</TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.trigger") }}
                </TableHead>
                <TableHead class="hidden sm:table-cell">
                  {{ t("dataQuality.instance") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.ruleTarget") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.startedAt") }}
                </TableHead>
                <TableHead class="hidden whitespace-nowrap md:table-cell">
                  {{ t("dataQuality.duration") }}
                </TableHead>
                <TableHead>{{ t("dataQuality.results") }}</TableHead>
                <TableHead class="w-12 text-right">
                  <span class="sr-only">{{ t("common.actions") }}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="run in runs"
                :key="run.name"
                class="cursor-pointer"
                @click="openRun(run)"
              >
                <TableCell>
                  <div class="flex flex-wrap items-center gap-2">
                    <Badge :variant="qualityRunStatusVariant(run.status)">
                      {{ t(qualityRunStatusKey(run.status)) }}
                    </Badge>
                    <Badge
                      v-if="run.outcome !== QualityStatus.UNSPECIFIED"
                      :variant="qualityStatusVariant(run.outcome)"
                      :title="t('dataQuality.outcome')"
                    >
                      {{ t(qualityStatusKey(run.outcome)) }}
                    </Badge>
                  </div>
                  <div class="mt-1 font-mono text-xs text-muted-foreground">
                    {{ qualityRunID(run.name) }}
                  </div>
                  <!-- Below md the trigger, target and duration columns collapse in here. -->
                  <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                    <span>{{ t(qualityRunTriggerKey(run.trigger)) }}</span>
                    <template v-if="runTargets(run).length > 0">
                      <span aria-hidden="true">·</span>
                      <span
                        class="min-w-0 truncate"
                        :title="targetsTitle(run)"
                      >
                        {{ runTargets(run)[0] }}
                      </span>
                    </template>
                    <span aria-hidden="true">·</span>
                    <span>{{ runDurationLabel(run) }}</span>
                    <span aria-hidden="true">·</span>
                    <span :title="formatDateTime(run.startTime, locale)">
                      {{ formatRelativeTime(run.startTime, locale) }}
                    </span>
                  </div>
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  {{ t(qualityRunTriggerKey(run.trigger)) }}
                </TableCell>
                <TableCell class="hidden font-mono text-xs sm:table-cell">
                  {{ instanceLabel(run) }}
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  <div
                    v-if="runTargets(run).length > 0"
                    class="flex flex-wrap items-center gap-1"
                  >
                    <span
                      class="max-w-[14rem] truncate text-xs"
                      :title="targetsTitle(run)"
                    >
                      {{ runTargets(run)[0] }}
                    </span>
                    <Badge
                      v-if="runTargets(run).length > 1"
                      variant="outline"
                      class="tabular-nums"
                      :title="targetsTitle(run)"
                    >
                      +{{ runTargets(run).length - 1 }}
                    </Badge>
                  </div>
                  <span
                    v-else
                    class="text-sm text-muted-foreground"
                  >
                    -
                  </span>
                </TableCell>
                <TableCell
                  class="hidden whitespace-nowrap text-muted-foreground md:table-cell"
                  :title="formatDateTime(run.startTime, locale)"
                >
                  {{ formatRelativeTime(run.startTime, locale) }}
                </TableCell>
                <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                  {{ runDurationLabel(run) }}
                </TableCell>
                <TableCell>
                  <div
                    class="flex flex-wrap items-center gap-1"
                    :title="t('dataQuality.results')"
                  >
                    <Badge
                      v-for="count in countsOf(run)"
                      :key="count.status"
                      :variant="qualityStatusVariant(count.status)"
                      :title="t(qualityStatusKey(count.status))"
                      class="tabular-nums"
                    >
                      {{ count.value }}
                    </Badge>
                    <span
                      v-if="countsOf(run).length === 0"
                      class="text-sm text-muted-foreground"
                    >
                      -
                    </span>
                  </div>
                </TableCell>
                <TableCell
                  class="text-right"
                  @click.stop
                >
                  <Button
                    variant="ghost"
                    size="icon"
                    class="h-8 w-8"
                    :title="t('dataQuality.viewRun')"
                    :aria-label="t('dataQuality.viewRun')"
                    @click="openRun(run)"
                  >
                    <Eye class="h-4 w-4 text-muted-foreground" />
                  </Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>

          <!-- The pager footer keeps the card's content inset. -->
          <div class="px-6 pb-6">
            <TablePager
              v-model:page-size="pageSize"
              :has-previous="hasPrevious"
              :has-next="hasNext"
              :disabled="isLoading"
              @previous="goPreviousPage"
              @next="goNextPage"
            />
          </div>
        </div>
      </PageState>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { Activity, Eye, RefreshCcw } from "lucide-vue-next";
import { onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRouter } from "vue-router";
import { listQualityRuns, qualityRunID } from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import {
  type QualityRun,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  formatSeconds,
  formatTargetPath,
  qualityRunStatusKey,
  qualityRunStatusVariant,
  qualityRunTriggerKey,
  qualityStatusKey,
  qualityStatusVariant,
  runDurationSeconds,
  targetScopeLabel,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

const { t, locale } = useI18n();
const router = useRouter();
const { handleError } = useErrorHandler();

const pageSize = ref(50);
const error = ref("");

const {
  items: runs,
  isLoading,
  hasNext,
  hasPrevious,
  reset: resetPage,
  refresh: refreshPage,
  goNext: goNextPage,
  goPrevious: goPreviousPage,
} = usePagedFetch<QualityRun>({
  fetchPage: async (pageToken, signal) => {
    error.value = "";
    return await listQualityRuns({
      pageSize: pageSize.value,
      pageToken,
      signal,
    });
  },
  onError: (e) => {
    error.value = handleError(e, "dataQuality.fetchRunsError");
  },
});

watch(pageSize, () => void resetPage());

/** The id part of `instances/{instance}`, or "-". */
function instanceLabel(run: QualityRun): string {
  return run.instance ? (run.instance.split("/").pop() ?? "-") : "-";
}

/**
 * The targets of the rules this run executed, as display paths. A run belongs to
 * one instance, which the instance column already names, so the cell shows the
 * target below it; a target without a recorded path falls back to its GUID.
 */
function runTargets(run: QualityRun): string[] {
  return (run.targets ?? []).flatMap((target) => {
    const path = formatTargetPath(target.path);
    if (path !== "-") {
      return [path];
    }
    return target.metaGuid ? [target.metaGuid] : [];
  });
}

/** Every target's full path, one per line, for a cell's tooltip. */
function targetsTitle(run: QualityRun): string {
  return (run.targets ?? [])
    .map((target) => {
      const path = formatTargetPath(target.path);
      if (path === "-") {
        return target.metaGuid || "-";
      }
      const scope = targetScopeLabel(target.path);
      return scope === "-" ? path : `${scope} · ${path}`;
    })
    .join("\n");
}

function runDurationLabel(run: QualityRun): string {
  const seconds = runDurationSeconds(run);
  return seconds === undefined ? "-" : formatSeconds(seconds);
}

/**
 * The run's tallies, as one badge per state that actually occurred. A zero is
 * noise in a table cell, and `total` is implicit in the badges; "no results" is
 * the empty case, not a row of six zeroes.
 */
function countsOf(run: QualityRun): Array<{
  status: QualityStatus;
  value: number;
}> {
  const counts = run.counts;
  if (!counts) {
    return [];
  }
  return [
    { status: QualityStatus.PASS, value: counts.pass },
    { status: QualityStatus.WARN, value: counts.warn },
    { status: QualityStatus.FAIL, value: counts.fail },
    { status: QualityStatus.ERROR, value: counts.error },
    { status: QualityStatus.SKIPPED, value: counts.skipped },
    { status: QualityStatus.NOT_EVALUATED, value: counts.notEvaluated },
  ].filter((count) => count.value > 0);
}

/** A click that ends a text selection is a copy, not a navigation. */
function openRun(run: QualityRun) {
  if (window.getSelection()?.toString()) {
    return;
  }
  void router.push({
    name: "DataQualityRunDetail",
    params: { runId: run.name.split("/") },
  });
}

onMounted(() => {
  void resetPage();
});
</script>
