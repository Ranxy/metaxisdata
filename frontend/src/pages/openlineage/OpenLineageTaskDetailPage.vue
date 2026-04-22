<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between gap-4">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ task?.jobName || t("openlineageSettings.tasks") }}
        </h1>
        <p class="text-muted-foreground font-mono text-sm">
          {{ task?.guid || currentGuid }}
        </p>
      </div>
      <div class="flex items-center gap-2">
        <Button v-if="task?.airflowDagUrl" variant="secondary" asChild>
          <a :href="task.airflowDagUrl" target="_blank" rel="noreferrer noopener">
            {{ t("openlineageSettings.openInAirflow") }}
          </a>
        </Button>
        <Button variant="outline" @click="router.push({ name: 'OpenLineageTasks' })">
          {{ t("openlineageSettings.backToTasks") }}
        </Button>
      </div>
    </div>

    <div v-if="isLoading" class="p-8 flex justify-center">
      <AppLoading />
    </div>

    <div v-else-if="task" class="space-y-6">
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
          <CardTitle>{{ t("openlineageSettings.runHistory") }}</CardTitle>
        </CardHeader>
        <CardContent>
          <div v-if="isRunsLoading" class="p-6 flex justify-center">
            <AppLoading />
          </div>
          <Table v-else-if="runs.length > 0">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("openlineageSettings.eventTime") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.runId") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.inputCount") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.outputCount") }}</TableHead>
                <TableHead class="text-right">{{ t("openlineageSettings.actions") }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="run in runs" :key="run.guid">
                <TableCell>{{ formatTimestamp(run.eventTime) }}</TableCell>
                <TableCell class="font-mono text-sm">{{ run.runId }}</TableCell>
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
import { useErrorHandler } from "@/composables/useErrorHandler";
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

const currentGuid = computed(() => {
  const guidParam = route.params.guid;
  return Array.isArray(guidParam) ? guidParam[0] : (guidParam ?? "");
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
  router.push({ name: "OpenLineageRunDetail", params: { guid } });
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