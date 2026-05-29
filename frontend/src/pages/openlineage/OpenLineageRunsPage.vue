<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between gap-4">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ t("openlineageSettings.tasks") }}
        </h1>
      </div>
      <Button variant="outline" @click="router.push({ name: 'OpenLineageSettings' })">
        {{ t("lineageGraph.backToMetadata") }}
      </Button>
    </div>

    <Card>
      <CardContent class="pt-6">
        <div v-if="isLoading" class="p-8 flex justify-center">
          <AppLoading />
        </div>
        <div
          v-else-if="tasks.length === 0"
          class="p-8 text-center text-muted-foreground"
        >
          <ScrollText class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
          <p>{{ t("openlineageSettings.noTasks") }}</p>
        </div>
        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("openlineageSettings.namespace") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.jobName") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.jobType") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.latestEventTime") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.latestRunId") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.runCount") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.lineageRunCount") }}</TableHead>
              <TableHead class="text-right">{{ t("openlineageSettings.actions") }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="task in tasks" :key="task.guid">
              <TableCell class="font-mono text-sm">{{ task.jobNamespace }}</TableCell>
              <TableCell>{{ task.jobName }}</TableCell>
              <TableCell>{{ task.jobType }}</TableCell>
              <TableCell>{{ formatTimestamp(task.latestEventTime) }}</TableCell>
              <TableCell class="font-mono text-sm">{{ task.latestRunId }}</TableCell>
              <TableCell>{{ task.runCount }}</TableCell>
              <TableCell>{{ task.lineageRunCount }}</TableCell>
              <TableCell class="text-right">
                <Button variant="ghost" size="sm" @click="openDetail(task.guid)">
                  {{ t("openlineageSettings.viewDetail") }}
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { ScrollText } from "lucide-vue-next";
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { listOpenLineageTasks } from "@/api/openlineage";
import AppLoading from "@/components/common/AppLoading.vue";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import type { OpenLineageTaskResource } from "@/types/proto-es/v1/openlineage_service_pb";

const { t, locale } = useI18n();
const router = useRouter();
const { handleError } = useErrorHandler();

const isLoading = ref(false);
const tasks = ref<OpenLineageTaskResource[]>([]);

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

function openDetail(guid: string) {
  router.push({ name: "OpenLineageTaskDetail", params: { guid } });
}

async function fetchTasks() {
  isLoading.value = true;
  try {
    const response = await listOpenLineageTasks({ pageSize: 100 });
    tasks.value = response.tasks;
  } catch (error) {
    handleError(error);
  } finally {
    isLoading.value = false;
  }
}

onMounted(() => {
  fetchTasks();
});
</script>