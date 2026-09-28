<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.overview')"
      :description="t('openlineage.overviewDescription')"
    />

    <PageState
      :loading="isLoading"
      :error="error"
    >
      <!-- Nothing ingested yet: the page becomes the setup guidance it used to
           be for every visit, instead of four permanent navigation cards that
           duplicated the sidebar. -->
      <Card v-if="runs.length === 0">
        <CardContent class="pt-6">
          <EmptyState
            :icon="RadioTower"
            :title="t('openlineage.noIngestedEvents')"
            :description="t('openlineage.noIngestedEventsHint')"
          >
            <Button
              variant="outline"
              size="sm"
              as-child
            >
              <RouterLink :to="{ name: 'OpenLineageSettings' }">
                {{ t("openlineage.ingestionSettings") }}
              </RouterLink>
            </Button>
          </EmptyState>
        </CardContent>
      </Card>

      <div
        v-else
        class="space-y-4"
      >
        <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <StatCard
            :label="t('openlineage.jobs')"
            :value="tasks.length"
            :hint="`${lineageReadyCount} ${t('openlineage.lineageReady')}`"
          />
          <StatCard
            :label="t('openlineage.datasets')"
            :value="datasets.length"
            :hint="`${internalCount} ${t('openlineage.internal')} · ${externalCount} ${t('openlineage.external')}`"
          />
          <StatCard
            :label="t('openlineage.recentRuns')"
            :value="runs.length"
            :hint="`${lineageRunCount} ${t('openlineage.lineageEvents')}`"
          />
          <StatCard
            :label="t('openlineage.lastEvent')"
            :value="lastEventLabel"
            :hint="lastEventHint"
          />
        </div>

        <Card>
          <CardHeader
            class="flex flex-row items-center justify-between space-y-0 border-b px-4 py-3"
          >
            <div class="space-y-1">
              <CardTitle class="text-base">
                {{ t("openlineage.recentRuns") }}
              </CardTitle>
              <CardDescription>
                {{ t("openlineage.overviewRunsDescription") }}
              </CardDescription>
            </div>
            <RouterLink
              :to="{ name: 'OpenLineageEvents' }"
              class="inline-flex shrink-0 items-center gap-1 text-sm text-primary hover:underline"
            >
              {{ t("openlineage.openEvents") }}
              <ArrowRight class="size-4" />
            </RouterLink>
          </CardHeader>
          <CardContent class="p-0">
            <!-- `whitespace-nowrap` keeps Chinese column labels from stacking one
                 character per line, and the caps keep a long integration or job
                 name from widening the table past its card. -->
            <Table class="[&_th]:whitespace-nowrap">
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t("openlineageSettings.latestEventTime") }}</TableHead>
                  <TableHead>{{ t("openlineage.eventType") }}</TableHead>
                  <TableHead>{{ t("openlineageSettings.jobName") }}</TableHead>
                  <TableHead>{{ t("openlineageSettings.integration") }}</TableHead>
                  <TableHead>{{ t("openlineage.hasLineage") }}</TableHead>
                  <TableHead>{{ t("openlineage.inputsOutputs") }}</TableHead>
                  <TableHead class="text-right">
                    {{ t("openlineageSettings.actions") }}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <!-- The whole row opens the run; the eye button stays the
                     labelled action so the row is a shortcut, not the only way
                     in. The job name belongs to the job, not to this run, so it
                     is not the link here. -->
                <TableRow
                  v-for="run in runs"
                  :key="run.guid"
                  class="cursor-pointer"
                  @click="handleRunRowClick(run.guid)"
                >
                  <TableCell class="whitespace-nowrap">
                    {{ formatTimestamp(run.eventTime) }}
                  </TableCell>
                  <TableCell>
                    <Badge :variant="statusVariant(run.eventType)">
                      {{ run.eventType || "-" }}
                    </Badge>
                  </TableCell>
                  <!-- The ellipsis lives on a block inside the cell:
                       `text-overflow` does not apply to a table cell, so
                       `truncate` on the `<td>` clipped values mid-character. -->
                  <TableCell>
                    <div
                      class="max-w-64 truncate"
                      :title="run.jobName"
                    >
                      {{ run.jobName }}
                    </div>
                  </TableCell>
                  <TableCell class="text-muted-foreground">
                    <div
                      class="max-w-32 truncate"
                      :title="run.integration || run.source"
                    >
                      {{ run.integration || run.source || "-" }}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge :variant="run.hasLineage ? 'success' : 'secondary'">
                      {{ run.hasLineage ? t("openlineage.yes") : t("openlineage.no") }}
                    </Badge>
                  </TableCell>
                  <TableCell
                    class="whitespace-nowrap"
                    :title="`${t('openlineage.inputs')} ${run.inputCount} · ${t('openlineage.outputs')} ${run.outputCount}`"
                  >
                    {{ run.inputCount }} / {{ run.outputCount }}
                  </TableCell>
                  <TableCell
                    class="text-right"
                    @click.stop
                  >
                    <Button
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :title="t('openlineageSettings.viewRun')"
                      :aria-label="t('openlineageSettings.viewRun')"
                      @click="openRun(run.guid)"
                    >
                      <Eye class="h-4 w-4 text-muted-foreground" />
                    </Button>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <Card>
          <CardHeader
            class="flex flex-row items-center justify-between space-y-0 border-b px-4 py-3"
          >
            <div class="space-y-1">
              <CardTitle class="text-base">
                {{ t("openlineage.overviewActiveJobs") }}
              </CardTitle>
              <CardDescription>
                {{ t("openlineage.overviewActiveJobsDescription") }}
              </CardDescription>
            </div>
            <RouterLink
              :to="{ name: 'OpenLineageTasks' }"
              class="inline-flex shrink-0 items-center gap-1 text-sm text-primary hover:underline"
            >
              {{ t("openlineage.openJobs") }}
              <ArrowRight class="size-4" />
            </RouterLink>
          </CardHeader>
          <CardContent class="p-0">
            <Table class="[&_th]:whitespace-nowrap">
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t("openlineageSettings.jobName") }}</TableHead>
                  <TableHead>{{ t("openlineageSettings.namespace") }}</TableHead>
                  <TableHead>{{ t("openlineageSettings.latestEventTime") }}</TableHead>
                  <TableHead>{{ t("openlineage.latestRunStatus") }}</TableHead>
                  <TableHead>{{ t("openlineageSettings.runCount") }}</TableHead>
                  <TableHead class="text-right">
                    {{ t("openlineageSettings.actions") }}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <!-- Here the row's detail is the job the name belongs to, so the
                     name is a real link and the row follows it. -->
                <TableRow
                  v-for="task in activeTasks"
                  :key="task.guid"
                  class="cursor-pointer"
                  @click="handleJobRowClick(task.guid)"
                >
                  <TableCell>
                    <RouterLink
                      class="block max-w-64 truncate font-medium text-primary hover:underline"
                      :title="task.jobName"
                      :to="{
                        name: 'OpenLineageTaskDetail',
                        params: { guid: task.guid },
                        query: { from: route.fullPath },
                      }"
                      @click.stop
                    >
                      {{ task.jobName }}
                    </RouterLink>
                  </TableCell>
                  <TableCell class="text-muted-foreground">
                    <div
                      class="max-w-64 truncate font-mono text-sm"
                      :title="task.jobNamespace"
                    >
                      {{ task.jobNamespace }}
                    </div>
                  </TableCell>
                  <TableCell class="whitespace-nowrap">
                    {{ formatTimestamp(task.latestEventTime) }}
                  </TableCell>
                  <TableCell>
                    <Badge :variant="statusVariant(task.latestEventType)">
                      {{ task.latestEventType || "-" }}
                    </Badge>
                  </TableCell>
                  <TableCell class="whitespace-nowrap">
                    {{ task.runCount }}
                    <span class="text-muted-foreground">
                      / {{ task.lineageRunCount }} {{ t("openlineage.lineageEvents") }}
                    </span>
                  </TableCell>
                  <TableCell
                    class="text-right"
                    @click.stop
                  >
                    <Button
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :title="t('openlineageSettings.viewDetail')"
                      :aria-label="t('openlineageSettings.viewDetail')"
                      @click="openJob(task.guid)"
                    >
                      <Eye class="h-4 w-4 text-muted-foreground" />
                    </Button>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>
    </PageState>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { ArrowRight, Eye, RadioTower } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute, useRouter } from "vue-router";
import {
  listOpenLineageDatasets,
  listOpenLineageRuns,
  listOpenLineageTasks,
} from "@/api/openlineage";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import StatCard from "@/components/common/StatCard.vue";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorMessage } from "@/composables/useErrorHandler";
import type {
  OpenLineageDatasetResource,
  OpenLineageRun,
  OpenLineageTask,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { formatDate, formatDateTime, formatTime } from "@/utils/datetime";

// Enough to describe the workspace's recent state without paging; every table
// links into the full directory for anything deeper.
const RUN_LIMIT = 10;
const JOB_LIMIT = 40;
const DATASET_LIMIT = 40;
const ACTIVE_JOB_LIMIT = 5;

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const router = useRouter();
const route = useRoute();

const isLoading = ref(false);
const error = ref<string | null>(null);
const runs = ref<OpenLineageRun[]>([]);
const tasks = ref<OpenLineageTask[]>([]);
const datasets = ref<OpenLineageDatasetResource[]>([]);

const lineageReadyCount = computed(
  () => tasks.value.filter((task) => task.lineageRunCount > 0).length
);

const internalCount = computed(
  () => datasets.value.filter((dataset) => dataset.internal).length
);

const externalCount = computed(
  () => datasets.value.length - internalCount.value
);

const lineageRunCount = computed(
  () => runs.value.filter((run) => run.hasLineage).length
);

// Runs arrive newest first, so the head is the most recent activity.
const lastRun = computed(() => runs.value[0] ?? null);

// The card's value is set at 3xl, where a full date-time breaks across two
// lines, so the date is the value and the time joins the source underneath.
const lastEventLabel = computed(() => {
  const run = lastRun.value;
  if (!run?.eventTime) {
    return t("openlineage.neverSeen");
  }
  return formatDate(run.eventTime, locale.value);
});

const lastEventHint = computed(() => {
  const run = lastRun.value;
  if (!run?.eventTime) {
    return "";
  }
  return [
    formatTime(run.eventTime, locale.value),
    run.integration || run.source || run.jobName,
  ]
    .filter(Boolean)
    .join(" · ");
});

const activeTasks = computed(() => tasks.value.slice(0, ACTIVE_JOB_LIMIT));

function formatTimestamp(value?: Timestamp): string {
  return formatDateTime(value, locale.value);
}

function statusVariant(
  eventType: string
): "success" | "destructive" | "secondary" | "outline" {
  const normalized = (eventType ?? "").toUpperCase();
  if (normalized === "COMPLETE") {
    return "success";
  }
  if (normalized === "FAIL" || normalized === "FAILED") {
    return "destructive";
  }
  if (normalized === "START" || normalized === "RUNNING") {
    return "outline";
  }
  return "secondary";
}

function openRun(guid: string) {
  router.push({
    name: "OpenLineageRunDetail",
    params: { guid },
    query: { from: route.fullPath },
  });
}

function openJob(guid: string) {
  router.push({
    name: "OpenLineageTaskDetail",
    params: { guid },
    query: { from: route.fullPath },
  });
}

/**
 * A click anywhere in the row opens its detail. A click that ends a text
 * selection is someone copying an identifier out of the table, not asking to
 * navigate, so it is left alone.
 */
function handleRowClick(open: () => void) {
  if (window.getSelection()?.toString()) {
    return;
  }
  open();
}

function handleRunRowClick(guid: string) {
  handleRowClick(() => openRun(guid));
}

function handleJobRowClick(guid: string) {
  handleRowClick(() => openJob(guid));
}

async function load() {
  isLoading.value = true;
  error.value = null;
  try {
    const [runResponse, taskResponse, datasetResponse] = await Promise.all([
      listOpenLineageRuns({ pageSize: RUN_LIMIT }),
      listOpenLineageTasks({ pageSize: JOB_LIMIT, lineageOnly: false }),
      listOpenLineageDatasets({ pageSize: DATASET_LIMIT }),
    ]);
    runs.value = runResponse.runs;
    tasks.value = taskResponse.tasks;
    datasets.value = datasetResponse.datasets;
  } catch (e) {
    error.value = formatError(e, "openlineage.overviewFetchError");
  } finally {
    isLoading.value = false;
  }
}

onMounted(load);
</script>
