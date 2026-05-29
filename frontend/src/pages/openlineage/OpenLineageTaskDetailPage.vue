<template>
  <div class="space-y-6">
    <OpenLineageSectionHeader
      :title="task?.jobName || t('openlineage.jobs')"
      :description="t('openlineage.jobsDescription')"
    >
      <template #actions>
        <Button v-if="task?.airflowDagUrl" variant="secondary" asChild>
          <a :href="task.airflowDagUrl" target="_blank" rel="noreferrer noopener">
            {{ t("openlineageSettings.openInAirflow") }}
          </a>
        </Button>
        <Button variant="outline" @click="goBackToJobs">
          {{ t("openlineage.backToJobs") }}
        </Button>
      </template>
    </OpenLineageSectionHeader>

    <p class="-mt-2 break-all font-mono text-sm text-muted-foreground">
      {{ task?.guid || currentGuid }}
    </p>

    <div v-if="isLoading" class="p-8 flex justify-center">
      <AppLoading />
    </div>

    <div v-else-if="task" class="space-y-6">
      <div class="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <Card>
          <CardHeader>
            <CardTitle>{{ t("openlineage.recentEvents") }}</CardTitle>
            <CardDescription>{{ t("openlineage.recentEventsDescription") }}</CardDescription>
          </CardHeader>
          <CardContent class="grid gap-4 sm:grid-cols-3">
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.runCount") }}</div>
              <div class="mt-2 text-2xl font-semibold">{{ task.runCount }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.lineageRunCount") }}</div>
              <div class="mt-2 text-2xl font-semibold">{{ task.lineageRunCount }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineage.lineageCoverage") }}</div>
              <div class="mt-2">
                <Badge :variant="task.lineageRunCount > 0 ? 'success' : 'secondary'">
                  {{ task.lineageRunCount > 0 ? t("openlineage.lineageReady") : t("openlineage.lineageMissing") }}
                </Badge>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("openlineage.nextActions") }}</CardTitle>
            <CardDescription>{{ t("openlineage.nextActionsDescription") }}</CardDescription>
          </CardHeader>
          <CardContent class="flex flex-col gap-3">
            <Button variant="outline" @click="openGraph">
              {{ t("openlineage.openGraph") }}
            </Button>
            <Button variant="outline" @click="openEvents">
              {{ t("openlineage.openEvents") }}
            </Button>
            <Button variant="outline" :disabled="!latestLineageRun" @click="openLatestLineageRun">
              {{ t("openlineage.openLatestLineageRun") }}
            </Button>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{{ t("openlineageSettings.taskSummary") }}</CardTitle>
          <CardDescription>{{ t("openlineageSettings.tasksDescription") }}</CardDescription>
        </CardHeader>
        <CardContent>
          <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.namespace") }}</div>
              <div class="font-mono text-sm break-all">{{ task.jobNamespace }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.jobName") }}</div>
              <div>{{ task.jobName }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.jobType") }}</div>
              <div>{{ task.jobType || "-" }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.integration") }}</div>
              <div>{{ task.integration || "-" }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.processingType") }}</div>
              <div>{{ task.processingType || "-" }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.latestEventTime") }}</div>
              <div>{{ formatTimestamp(task.latestEventTime) }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.latestRunId") }}</div>
              <div class="font-mono text-sm break-all">{{ task.latestRunId || "-" }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.runCount") }}</div>
              <div>{{ task.runCount }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.lineageRunCount") }}</div>
              <div>{{ task.lineageRunCount }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.parentJob") }}</div>
              <div>{{ formatJobRef(task.parentJobNamespace, task.parentJobName) }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.rootJob") }}</div>
              <div>{{ formatJobRef(task.rootJobNamespace, task.rootJobName) }}</div>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{{ t("openlineage.relatedDatasets") }}</CardTitle>
          <CardDescription>{{ t("openlineage.taskRelatedDatasetsDescription") }}</CardDescription>
        </CardHeader>
        <CardContent class="grid gap-6 lg:grid-cols-2">
          <div>
            <div class="mb-3 flex items-center justify-between gap-3">
              <div class="text-sm font-medium text-muted-foreground">
                {{ t("openlineage.inputs") }}
              </div>
              <Badge variant="secondary">{{ relatedInputDatasets.length }}</Badge>
            </div>
            <div v-if="relatedInputDatasets.length > 0" class="max-h-80 space-y-2 overflow-y-auto pr-1">
              <div
                v-for="dataset in relatedInputDatasets"
                :key="`input-${dataset.namespace}-${dataset.name}`"
                class="rounded-md border px-3 py-2"
              >
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <div class="truncate font-medium">{{ dataset.name }}</div>
                    <div class="break-all font-mono text-xs text-muted-foreground">
                      {{ dataset.namespace }}
                    </div>
                  </div>
                  <Badge variant="outline">
                    {{ t("openlineage.seenInRuns", { count: dataset.runCount }) }}
                  </Badge>
                </div>
              </div>
            </div>
            <div v-else class="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
              {{ t("openlineage.noInputs") }}
            </div>
          </div>

          <div>
            <div class="mb-3 flex items-center justify-between gap-3">
              <div class="text-sm font-medium text-muted-foreground">
                {{ t("openlineage.outputs") }}
              </div>
              <Badge variant="secondary">{{ relatedOutputDatasets.length }}</Badge>
            </div>
            <div v-if="relatedOutputDatasets.length > 0" class="max-h-80 space-y-2 overflow-y-auto pr-1">
              <div
                v-for="dataset in relatedOutputDatasets"
                :key="`output-${dataset.namespace}-${dataset.name}`"
                class="rounded-md border px-3 py-2"
              >
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <div class="truncate font-medium">{{ dataset.name }}</div>
                    <div class="break-all font-mono text-xs text-muted-foreground">
                      {{ dataset.namespace }}
                    </div>
                  </div>
                  <Badge variant="outline">
                    {{ t("openlineage.seenInRuns", { count: dataset.runCount }) }}
                  </Badge>
                </div>
              </div>
            </div>
            <div v-else class="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
              {{ t("openlineage.noOutputs") }}
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{{ t("openlineageSettings.runHistory") }}</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="mb-4 flex items-center gap-2">
            <Checkbox
              id="job-detail-lineage-only"
              :checked="lineageOnlyRuns"
              @update:checked="lineageOnlyRuns = $event === true"
            />
            <Label for="job-detail-lineage-only" class="cursor-pointer text-sm">
              {{ t("openlineage.onlyLineage") }}
            </Label>
          </div>
          <div v-if="isRunsLoading" class="p-6 flex justify-center">
            <AppLoading />
          </div>
          <Table v-else-if="displayRuns.length > 0">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("openlineageSettings.eventTime") }}</TableHead>
                <TableHead>{{ t("openlineage.eventType") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.runId") }}</TableHead>
                <TableHead>{{ t("openlineage.hasLineage") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.inputCount") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.outputCount") }}</TableHead>
                <TableHead class="text-right">{{ t("openlineageSettings.actions") }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="run in displayRuns" :key="run.guid">
                <TableCell>{{ formatTimestamp(run.eventTime) }}</TableCell>
                <TableCell>
                  <Badge variant="outline">{{ run.eventType || "-" }}</Badge>
                </TableCell>
                <TableCell class="font-mono text-sm">{{ run.runId }}</TableCell>
                <TableCell>
                  <Badge :variant="run.hasLineage ? 'success' : 'secondary'">
                    {{ run.hasLineage ? t("openlineage.yes") : t("openlineage.no") }}
                  </Badge>
                </TableCell>
                <TableCell>{{ run.inputCount }}</TableCell>
                <TableCell>{{ run.outputCount }}</TableCell>
                <TableCell class="text-right">
                  <Button variant="ghost" size="sm" @click="openRun(run.guid)">
                    {{ t("openlineageSettings.viewRun") }}
                  </Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
          <div v-else class="p-6 text-sm text-muted-foreground text-center">
            {{ t("openlineageSettings.noRuns") }}
          </div>
        </CardContent>
      </Card>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { getOpenLineageTask, listOpenLineageRuns } from "@/api/openlineage";
import AppLoading from "@/components/common/AppLoading.vue";
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
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { aggregateOpenLineageDatasets } from "@/lib/openlineage";
import type {
  OpenLineageRunResource,
  OpenLineageTaskResource,
} from "@/types/proto-es/v1/openlineage_service_pb";

const route = useRoute();
const router = useRouter();
const { t, locale } = useI18n();
const { handleError } = useErrorHandler();

const isLoading = ref(false);
const isRunsLoading = ref(false);
const task = ref<OpenLineageTaskResource | null>(null);
const runs = ref<OpenLineageRunResource[]>([]);
const lineageOnlyRuns = ref(false);

const currentGuid = computed(() => {
  const guidParam = route.params.guid;
  return Array.isArray(guidParam) ? guidParam[0] : (guidParam ?? "");
});

const displayRuns = computed(() => {
  if (!lineageOnlyRuns.value) {
    return runs.value;
  }

  return runs.value.filter((run) => run.hasLineage);
});

const latestLineageRun = computed(() => {
  return runs.value.find((run) => run.hasLineage) ?? null;
});

const relatedDatasets = computed(() => {
  return aggregateOpenLineageDatasets(runs.value);
});

const relatedInputDatasets = computed(() => {
  return relatedDatasets.value.inputs;
});

const relatedOutputDatasets = computed(() => {
  return relatedDatasets.value.outputs;
});

function formatTimestamp(ts: Timestamp | undefined): string {
  if (!ts?.seconds) return "-";
  const date = new Date(Number(ts.seconds) * 1000);
  return new Intl.DateTimeFormat(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function formatJobRef(namespace: string, name: string): string {
  if (!namespace && !name) return "-";
  if (!namespace) return name;
  if (!name) return namespace;
  return `${namespace} / ${name}`;
}

function openRun(guid: string) {
  router.push({
    name: "OpenLineageRunDetail",
    params: { guid },
    query: { from: route.fullPath },
  });
}

function goBackToJobs() {
  const from = route.query.from;
  if (typeof from === "string" && from.length > 0) {
    router.push(from);
    return;
  }

  router.push({ name: "OpenLineageTasks" });
}

function openGraph() {
  if (!task.value?.guid) {
    return;
  }

  router.push({
    name: "LineageGraph",
    params: { guid: task.value.guid },
    query: {
      metaType: "100",
      from: route.fullPath,
    },
  });
}

function openEvents() {
  if (!task.value) {
    return;
  }

  router.push({
    name: "OpenLineageEvents",
    query: {
      search: task.value.jobName,
      namespace: task.value.jobNamespace,
      from: route.fullPath,
    },
  });
}

function openLatestLineageRun() {
  if (!latestLineageRun.value?.guid) {
    return;
  }

  openRun(latestLineageRun.value.guid);
}

async function fetchRuns() {
  if (!task.value?.guid) return;
  isRunsLoading.value = true;
  try {
    const response = await listOpenLineageRuns({
      pageSize: 100,
      taskGuid: task.value.guid,
      jobType: task.value.jobType,
    });
    runs.value = response.runs;
  } catch (error) {
    handleError(error);
  } finally {
    isRunsLoading.value = false;
  }
}

async function fetchTask() {
  if (!currentGuid.value) return;
  isLoading.value = true;
  try {
    task.value = await getOpenLineageTask(currentGuid.value);
    await fetchRuns();
  } catch (error) {
    handleError(error);
  } finally {
    isLoading.value = false;
  }
}

onMounted(() => {
  fetchTask();
});
</script>