<template>
  <Dialog :open="modelValue" @update:open="handleOpenChange">
    <DialogContent
      class="left-auto right-0 top-0 h-screen max-h-screen w-full max-w-2xl translate-x-0 translate-y-0 gap-0 overflow-hidden rounded-none border-l p-0 sm:max-w-2xl sm:rounded-none"
    >
      <div class="flex h-full flex-col">
        <DialogHeader class="border-b px-6 py-5">
          <DialogTitle class="text-xl">{{ dataset?.name || t("openlineageSettings.viewDetail") }}</DialogTitle>
          <DialogDescription>
            {{ dataset?.namespace || t("openlineage.datasetDetailDescription") }}
          </DialogDescription>
        </DialogHeader>

        <div class="flex-1 overflow-y-auto px-6 py-5">
          <div v-if="isLoading" class="flex min-h-56 items-center justify-center">
            <AppLoading />
          </div>

          <div v-else-if="errorMessage" class="rounded-md border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
            {{ errorMessage }}
          </div>

          <div v-else-if="detail" class="space-y-6">
            <div class="flex flex-wrap items-center gap-2">
              <Badge :variant="detail.dataset?.internal ? 'default' : 'outline'">
                {{ detail.dataset?.internal ? t("openlineage.internal") : t("openlineage.external") }}
              </Badge>
              <Badge :variant="detail.dataset?.supportsColumnLineage ? 'success' : 'secondary'">
                {{ detail.dataset?.supportsColumnLineage ? t("openlineage.supportsColumnLineage") : t("openlineage.lineageMissing") }}
              </Badge>
              <Badge v-for="integration in detail.dataset?.integrations ?? []" :key="integration" variant="outline">
                {{ integration }}
              </Badge>
            </div>

            <div class="flex flex-wrap gap-2">
              <Button variant="outline" size="sm" @click="openGraph">
                {{ t("openlineage.openGraph") }}
              </Button>
              <Button variant="outline" size="sm" :disabled="!detail.dataset?.internal" @click="openMetadata">
                {{ t("openlineage.openMetadata") }}
              </Button>
            </div>

            <section class="space-y-3">
              <div>
                <h3 class="text-sm font-semibold">{{ t("openlineage.datasetSummary") }}</h3>
                <p class="text-sm text-muted-foreground">{{ t("openlineage.datasetDetailDescription") }}</p>
              </div>

              <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("openlineage.datasetType") }}</div>
                  <div class="mt-1 text-sm font-medium">{{ detail.dataset?.datasetType || "-" }}</div>
                </div>
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("openlineage.resolvedTarget") }}</div>
                  <div class="mt-1 break-all font-mono text-sm">{{ detail.dataset?.resolvedTarget || "-" }}</div>
                </div>
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("openlineage.lastSeen") }}</div>
                  <div class="mt-1 text-sm font-medium">{{ formatTimestamp(detail.dataset?.lastSeen) }}</div>
                </div>
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("openlineage.sourceJobsCount") }}</div>
                  <div class="mt-1 text-sm font-medium">{{ detail.dataset?.sourceJobCount ?? 0 }}</div>
                </div>
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("openlineage.targetJobsCount") }}</div>
                  <div class="mt-1 text-sm font-medium">{{ detail.dataset?.targetJobCount ?? 0 }}</div>
                </div>
                <div class="rounded-md border p-3">
                  <div class="text-xs text-muted-foreground">{{ t("openlineageSettings.sourceLabel") }}</div>
                  <div class="mt-1 text-sm font-medium">{{ formatList(detail.dataset?.sources ?? []) }}</div>
                </div>
              </div>
            </section>

            <section class="space-y-3">
              <div>
                <h3 class="text-sm font-semibold">{{ t("openlineage.datasetSchema") }}</h3>
                <p class="text-sm text-muted-foreground">{{ t("openlineage.datasetSchemaDescription") }}</p>
              </div>

              <div v-if="detail.schemaFields.length === 0" class="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
                {{ t("openlineage.noSchemaFields") }}
              </div>

              <Table v-else>
                <TableHeader>
                  <TableRow>
                    <TableHead>{{ t("openlineage.fieldName") }}</TableHead>
                    <TableHead>{{ t("openlineage.fieldType") }}</TableHead>
                    <TableHead>{{ t("openlineage.fieldDescription") }}</TableHead>
                    <TableHead>{{ t("openlineage.supportsColumnLineage") }}</TableHead>
                    <TableHead class="text-right">{{ t("openlineageSettings.actions") }}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-for="field in detail.schemaFields" :key="field.name">
                    <TableCell class="font-medium">{{ field.name }}</TableCell>
                    <TableCell class="font-mono text-xs">{{ field.type || "-" }}</TableCell>
                    <TableCell class="text-muted-foreground">{{ field.description || "-" }}</TableCell>
                    <TableCell>
                      <Badge :variant="field.columnLineageReady ? 'success' : 'secondary'">
                        {{ field.columnLineageReady ? t("openlineage.yes") : t("openlineage.no") }}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="sm"
                        :disabled="!field.columnLineageReady"
                        @click="openColumnLineage(field.name)"
                      >
                        {{ t("openlineage.openColumnLineage") }}
                      </Button>
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </section>

            <section class="space-y-3">
              <div>
                <h3 class="text-sm font-semibold">{{ t("openlineage.relatedJobs") }}</h3>
                <p class="text-sm text-muted-foreground">{{ t("openlineage.relatedJobsDescription") }}</p>
              </div>

              <div v-if="detail.relatedJobs.length === 0" class="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
                {{ t("openlineage.noRelatedJobs") }}
              </div>

              <div v-else class="space-y-3">
                <div v-for="job in detail.relatedJobs" :key="job.taskGuid || `${job.jobNamespace}:${job.jobName}`" class="rounded-md border p-4">
                  <div class="flex flex-wrap items-start justify-between gap-3">
                    <div class="space-y-1">
                      <div class="font-medium">{{ job.jobName }}</div>
                      <div class="text-sm text-muted-foreground">{{ job.jobNamespace }}</div>
                    </div>
                    <Button variant="ghost" size="sm" @click="openJobDetail(job.taskGuid)">
                      {{ t("openlineage.openJobDetail") }}
                    </Button>
                  </div>
                  <div class="mt-3 flex flex-wrap items-center gap-2">
                    <Badge v-if="job.readsDataset" variant="warning">{{ t("openlineage.readsDataset") }}</Badge>
                    <Badge v-if="job.writesDataset" variant="success">{{ t("openlineage.writesDataset") }}</Badge>
                    <Badge variant="outline">{{ job.integration || "-" }}</Badge>
                    <Badge variant="outline">{{ t("openlineage.seenInRuns", { count: job.runCount }) }}</Badge>
                  </div>
                  <div class="mt-3 text-xs text-muted-foreground">{{ t("openlineage.lastSeen") }}: {{ formatTimestamp(job.lastSeen) }}</div>
                </div>
              </div>
            </section>

            <section class="space-y-3">
              <div>
                <h3 class="text-sm font-semibold">{{ t("openlineage.recentRuns") }}</h3>
                <p class="text-sm text-muted-foreground">{{ t("openlineage.recentRunsDescription") }}</p>
              </div>

              <div v-if="detail.recentRuns.length === 0" class="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
                {{ t("openlineage.noRecentRuns") }}
              </div>

              <Table v-else>
                <TableHeader>
                  <TableRow>
                    <TableHead>{{ t("openlineageSettings.eventTime") }}</TableHead>
                    <TableHead>{{ t("openlineageSettings.jobName") }}</TableHead>
                    <TableHead>{{ t("openlineageSettings.runId") }}</TableHead>
                    <TableHead>{{ t("openlineage.eventType") }}</TableHead>
                    <TableHead>{{ t("openlineage.scope") }}</TableHead>
                    <TableHead class="text-right">{{ t("openlineageSettings.actions") }}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-for="run in detail.recentRuns" :key="run.guid">
                    <TableCell>{{ formatTimestamp(run.eventTime) }}</TableCell>
                    <TableCell>
                      <div class="font-medium">{{ run.jobName }}</div>
                      <div class="text-xs text-muted-foreground">{{ run.jobNamespace }}</div>
                    </TableCell>
                    <TableCell class="font-mono text-xs">{{ run.runId || "-" }}</TableCell>
                    <TableCell>{{ run.eventType || "-" }}</TableCell>
                    <TableCell>
                      <div class="flex flex-wrap gap-2">
                        <Badge v-if="run.readsDataset" variant="warning">{{ t("openlineage.readsDataset") }}</Badge>
                        <Badge v-if="run.writesDataset" variant="success">{{ t("openlineage.writesDataset") }}</Badge>
                      </div>
                    </TableCell>
                    <TableCell class="text-right">
                      <Button variant="ghost" size="sm" @click="openRunDetail(run.guid)">
                        {{ t("openlineageSettings.viewRun") }}
                      </Button>
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </section>
          </div>
        </div>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { getOpenLineageDataset } from "@/api/openlineage";
import AppLoading from "@/components/common/AppLoading.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { toGuidPath } from "@/lib/openlineage";
import type {
  OpenLineageDatasetDetailResource,
  OpenLineageDatasetResource,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { extractErrorMessage } from "@/utils/error";

const props = defineProps<{
  modelValue: boolean;
  dataset: OpenLineageDatasetResource | null;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
}>();

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();

const isLoading = ref(false);
const errorMessage = ref("");
const detail = ref<OpenLineageDatasetDetailResource | null>(null);

const dataset = computed(() => props.dataset);

watch(
  () => ({
    isOpen: props.modelValue,
    guid: props.dataset?.guid ?? "",
    namespace: props.dataset?.namespace ?? "",
    name: props.dataset?.name ?? "",
  }),
  async ({ isOpen, guid, namespace, name }) => {
    if (!isOpen || !guid || !namespace || !name) {
      if (!isOpen) {
        detail.value = null;
        errorMessage.value = "";
      }
      return;
    }

    isLoading.value = true;
    errorMessage.value = "";
    try {
      detail.value = await getOpenLineageDataset({ guid, namespace, name });
    } catch (error) {
      detail.value = null;
      errorMessage.value = extractErrorMessage(error);
    } finally {
      isLoading.value = false;
    }
  },
  { immediate: true }
);

function handleOpenChange(open: boolean) {
  emit("update:modelValue", open);
}

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

function formatList(items: string[]): string {
  return items.length > 0 ? items.join(", ") : "-";
}

function openGraph() {
  if (!detail.value?.dataset) {
    return;
  }

  router.push({
    name: "LineageGraph",
    params: { guid: detail.value.dataset.guid },
    query: {
      metaType: String(detail.value.dataset.resolvedMetaType),
      from: route.fullPath,
    },
  });
}

function openMetadata() {
  if (!detail.value?.dataset?.internal) {
    return;
  }

  router.push({
    name: "MetadataDetail",
    params: { guid: toGuidPath(detail.value.dataset.guid) },
    query: {
      metaType: String(detail.value.dataset.resolvedMetaType),
      from: route.fullPath,
    },
  });
}

function openJobDetail(taskGuid: string) {
  if (!taskGuid) {
    return;
  }

  router.push({
    name: "OpenLineageTaskDetail",
    params: { guid: taskGuid },
    query: { from: route.fullPath },
  });
}

function openRunDetail(runGuid: string) {
  if (!runGuid) {
    return;
  }

  router.push({
    name: "OpenLineageRunDetail",
    params: { guid: runGuid },
    query: { from: route.fullPath },
  });
}

function openColumnLineage(columnName: string) {
  if (!detail.value?.dataset?.guid) {
    return;
  }

  router.push({
    name: "OpenLineageColumnLineage",
    params: { guid: toGuidPath(detail.value.dataset.guid) },
    query: {
      metaType: String(detail.value.dataset.resolvedMetaType),
      column: columnName,
      from: route.fullPath,
    },
  });
}
</script>