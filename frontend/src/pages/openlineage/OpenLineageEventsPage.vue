<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.events')"
      :description="t('openlineage.eventsDescription')"
    />

    <Card>
      <CardContent class="pt-6">
        <div v-if="isLoading" class="flex justify-center p-8">
          <AppLoading />
        </div>
        <div v-else-if="runs.length === 0" class="p-8 text-center text-muted-foreground">
          <Files class="mx-auto mb-4 h-12 w-12 text-muted-foreground/50" />
          <p>{{ t("openlineageSettings.noRuns") }}</p>
        </div>
        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("openlineageSettings.eventTime") }}</TableHead>
              <TableHead>{{ t("openlineage.eventType") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.jobName") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.namespace") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.runId") }}</TableHead>
              <TableHead>{{ t("openlineage.hasLineage") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.inputCount") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.outputCount") }}</TableHead>
              <TableHead class="text-right">{{ t("openlineageSettings.actions") }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="run in runs" :key="run.guid">
              <TableCell>{{ formatTimestamp(run.eventTime) }}</TableCell>
              <TableCell>
                <Badge variant="outline">{{ run.eventType || "-" }}</Badge>
              </TableCell>
              <TableCell>{{ run.jobName }}</TableCell>
              <TableCell class="font-mono text-sm">{{ run.jobNamespace }}</TableCell>
              <TableCell class="font-mono text-sm">{{ run.runId }}</TableCell>
              <TableCell>
                <Badge :variant="run.hasLineage ? 'success' : 'secondary'">
                  {{ run.hasLineage ? t("openlineage.yes") : t("openlineage.no") }}
                </Badge>
              </TableCell>
              <TableCell>{{ run.inputCount }}</TableCell>
              <TableCell>{{ run.outputCount }}</TableCell>
              <TableCell class="text-right">
                <Button variant="ghost" size="sm" @click="openDetail(run.guid)">
                  {{ t("openlineageSettings.viewRun") }}
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
import { Files } from "lucide-vue-next";
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listOpenLineageRuns } from "@/api/openlineage";
import AppLoading from "@/components/common/AppLoading.vue";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Badge } from "@/components/ui/badge";
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
import type { OpenLineageRunResource } from "@/types/proto-es/v1/openlineage_service_pb";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();

const isLoading = ref(false);
const runs = ref<OpenLineageRunResource[]>([]);

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
  router.push({
    name: "OpenLineageRunDetail",
    params: { guid },
    query: { from: route.fullPath },
  });
}

async function fetchRuns() {
  isLoading.value = true;
  try {
    const response = await listOpenLineageRuns({ pageSize: 100 });
    runs.value = response.runs;
  } catch (error) {
    handleError(error);
  } finally {
    isLoading.value = false;
  }
}

onMounted(() => {
  fetchRuns();
});
</script>