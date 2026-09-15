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
        <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <Card>
            <CardContent class="space-y-1 px-3 py-2">
              <div class="text-sm text-muted-foreground">
                {{ t("openlineage.jobs") }}
              </div>
              <div class="text-2xl font-semibold">
                {{ tasks.length }}
              </div>
              <div class="text-xs text-muted-foreground">
                {{ lineageReadyCount }} {{ t("openlineage.lineageReady") }}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent class="space-y-1 px-3 py-2">
              <div class="text-sm text-muted-foreground">
                {{ t("openlineage.datasets") }}
              </div>
              <div class="text-2xl font-semibold">
                {{ datasets.length }}
              </div>
              <div class="text-xs text-muted-foreground">
                {{ internalCount }} {{ t("openlineage.internal") }} ·
                {{ externalCount }} {{ t("openlineage.external") }}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent class="space-y-1 px-3 py-2">
              <div class="text-sm text-muted-foreground">
                {{ t("openlineage.recentRuns") }}
              </div>
              <div class="text-2xl font-semibold">
                {{ runs.length }}
              </div>
              <div class="text-xs text-muted-foreground">
                {{ lineageRunCount }} {{ t("openlineage.lineageEvents") }}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent class="space-y-1 px-3 py-2">
              <div class="text-sm text-muted-foreground">
                {{ t("openlineage.lastEvent") }}
              </div>
              <div class="truncate text-2xl font-semibold">
                {{ lastEventLabel }}
              </div>
              <div class="truncate text-xs text-muted-foreground">
                {{ lastEventHint }}
              </div>
            </CardContent>
          </Card>
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
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t("openlineageSettings.latestEventTime") }}</TableHead>
                  <TableHead>{{ t("openlineage.eventType") }}</TableHead>
                  <TableHead>{{ t("openlineageSettings.jobName") }}</TableHead>
                  <TableHead>{{ t("openlineageSettings.integration") }}</TableHead>
                  <TableHead>{{ t("openlineage.hasLineage") }}</TableHead>
                  <TableHead>{{ t("openlineage.inputs") }}</TableHead>
                  <TableHead>{{ t("openlineage.outputs") }}</TableHead>
                  <TableHead class="text-right">
                    {{ t("openlineageSettings.actions") }}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow
                  v-for="run in runs"
                  :key="run.guid"
                >
                  <TableCell class="whitespace-nowrap">
                    {{ formatTimestamp(run.eventTime) }}
                  </TableCell>
                  <TableCell>
                    <Badge :variant="statusVariant(run.eventType)">
                      {{ run.eventType || "-" }}
                    </Badge>
                  </TableCell>
                  <TableCell class="max-w-64 truncate">
                    {{ run.jobName }}
                  </TableCell>
                  <TableCell class="text-muted-foreground">
                    {{ run.integration || run.source || "-" }}
                  </TableCell>
                  <TableCell>
                    <Badge :variant="run.hasLineage ? 'success' : 'secondary'">
                      {{ run.hasLineage ? t("openlineage.yes") : t("openlineage.no") }}
                    </Badge>
                  </TableCell>
                  <TableCell>{{ run.inputCount }}</TableCell>
                  <TableCell>{{ run.outputCount }}</TableCell>
                  <TableCell class="text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      @click="openRun(run.guid)"
                    >
                      {{ t("openlineageSettings.viewRun") }}
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
            <Table>
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
                <TableRow
                  v-for="task in activeTasks"
                  :key="task.guid"
                >
                  <TableCell class="max-w-64 truncate">
                    {{ task.jobName }}
                  </TableCell>
                  <TableCell class="max-w-64 truncate font-mono text-sm text-muted-foreground">
                    {{ task.jobNamespace }}
                  </TableCell>
                  <TableCell class="whitespace-nowrap">
                    {{ formatTimestamp(task.latestEventTime) }}
                  </TableCell>
                  <TableCell>
                    <Badge :variant="statusVariant(task.latestEventType)">
                      {{ task.latestEventType || "-" }}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {{ task.runCount }}
                    <span class="text-muted-foreground">
                      / {{ task.lineageRunCount }} {{ t("openlineage.lineageEvents") }}
                    </span>
                  </TableCell>
                  <TableCell class="text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      @click="openJob(task.guid)"
                    >
                      {{ t("openlineageSettings.viewDetail") }}
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
import { ArrowRight, RadioTower } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRouter } from "vue-router";
import {
  listOpenLineageDatasets,
  listOpenLineageRuns,
  listOpenLineageTasks,
} from "@/api/openlineage";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
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
import type {
  OpenLineageDatasetResource,
  OpenLineageRun,
  OpenLineageTask,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { extractErrorMessage } from "@/utils/error";

// Enough to describe the workspace's recent state without paging; every table
// links into the full directory for anything deeper.
const RUN_LIMIT = 10;
const JOB_LIMIT = 40;
const DATASET_LIMIT = 40;
const ACTIVE_JOB_LIMIT = 5;

const { t } = useI18n();
const router = useRouter();

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

const lastEventLabel = computed(() => {
  const run = lastRun.value;
  if (!run?.eventTime) {
    return t("openlineage.neverSeen");
  }
  return formatTimestamp(run.eventTime);
});

const lastEventHint = computed(() => {
  const run = lastRun.value;
  if (!run) {
    return "";
  }
  return run.integration || run.source || run.jobName;
});

const activeTasks = computed(() => tasks.value.slice(0, ACTIVE_JOB_LIMIT));

function formatTimestamp(value?: Timestamp): string {
  if (!value) {
    return "-";
  }
  return new Intl.DateTimeFormat(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(Number(value.seconds) * 1000));
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
    query: { from: router.currentRoute.value.fullPath },
  });
}

function openJob(guid: string) {
  router.push({
    name: "OpenLineageTaskDetail",
    params: { guid },
    query: { from: router.currentRoute.value.fullPath },
  });
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
    error.value = extractErrorMessage(e) || t("openlineage.overviewFetchError");
  } finally {
    isLoading.value = false;
  }
}

onMounted(load);
</script>
