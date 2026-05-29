<template>
  <div class="space-y-6">
    <OpenLineageSectionHeader
      :title="run?.jobName || t('openlineage.events')"
      :description="t('openlineage.eventsDescription')"
    >
      <template #actions>
        <Button v-if="run?.airflowRunLogUrl" variant="secondary" asChild>
          <a :href="run.airflowRunLogUrl" target="_blank" rel="noreferrer noopener">
            {{ t("openlineageSettings.openRunLogInAirflow") }}
          </a>
        </Button>
        <Button variant="outline" @click="goBack">
          {{ t("openlineage.backToEvents") }}
        </Button>
      </template>
    </OpenLineageSectionHeader>

    <p class="-mt-2 break-all font-mono text-sm text-muted-foreground">
      {{ run?.guid || currentGuid }}
    </p>

    <div v-if="isLoading" class="p-8 flex justify-center">
      <AppLoading />
    </div>

    <div v-else-if="run" class="space-y-6">
      <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <Card>
          <CardHeader>
            <CardTitle>{{ t("openlineage.nextActions") }}</CardTitle>
            <CardDescription>{{ t("openlineage.runNextActionsDescription") }}</CardDescription>
          </CardHeader>
          <CardContent class="flex flex-col gap-3">
            <Button variant="outline" :disabled="!run.taskGuid" @click="openJobDetail">
              {{ t("openlineage.openJobDetail") }}
            </Button>
            <Button variant="outline" :disabled="!run.taskGuid" @click="openGraph">
              {{ t("openlineage.openGraph") }}
            </Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("openlineage.relatedDatasets") }}</CardTitle>
            <CardDescription>{{ t("openlineage.relatedDatasetsDescription") }}</CardDescription>
          </CardHeader>
          <CardContent class="space-y-4">
            <div>
              <div class="mb-2 text-sm font-medium text-muted-foreground">
                {{ t("openlineage.inputs") }}
              </div>
              <div v-if="inputDatasets.length > 0" class="space-y-2">
                <div
                  v-for="dataset in inputDatasets"
                  :key="`input-${dataset.namespace}-${dataset.name}`"
                  class="rounded-md border px-3 py-2"
                >
                  <div class="font-medium">{{ dataset.name }}</div>
                  <div class="font-mono text-xs text-muted-foreground">{{ dataset.namespace }}</div>
                </div>
              </div>
              <p v-else class="text-sm text-muted-foreground">
                {{ t("openlineage.noInputs") }}
              </p>
            </div>

            <div>
              <div class="mb-2 text-sm font-medium text-muted-foreground">
                {{ t("openlineage.outputs") }}
              </div>
              <div v-if="outputDatasets.length > 0" class="space-y-2">
                <div
                  v-for="dataset in outputDatasets"
                  :key="`output-${dataset.namespace}-${dataset.name}`"
                  class="rounded-md border px-3 py-2"
                >
                  <div class="font-medium">{{ dataset.name }}</div>
                  <div class="font-mono text-xs text-muted-foreground">{{ dataset.namespace }}</div>
                </div>
              </div>
              <p v-else class="text-sm text-muted-foreground">
                {{ t("openlineage.noOutputs") }}
              </p>
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{{ t("openlineageSettings.runSummary") }}</CardTitle>
          <CardDescription>{{ t("openlineageSettings.runSummaryDescription") }}</CardDescription>
        </CardHeader>
        <CardContent>
          <div class="grid gap-4 md:grid-cols-2">
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.runId") }}</div>
              <div class="font-mono text-sm break-all">{{ run.runId }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.eventTime") }}</div>
              <div>{{ formatTimestamp(run.eventTime) }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.namespace") }}</div>
              <div class="font-mono text-sm break-all">{{ run.jobNamespace }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.jobName") }}</div>
              <div>{{ run.jobName }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.jobType") }}</div>
              <div>{{ run.jobType || "-" }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.producer") }}</div>
              <div class="break-all">{{ run.producer || "-" }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.integration") }}</div>
              <div>{{ run.integration || "-" }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.processingType") }}</div>
              <div>{{ run.processingType || "-" }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.sourceLabel") }}</div>
              <div>{{ run.source || "-" }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.inputCount") }}</div>
              <div>{{ run.inputCount }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.outputCount") }}</div>
              <div>{{ run.outputCount }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.parentJob") }}</div>
              <div>{{ formatJobRef(run.parentJobNamespace, run.parentJobName) }}</div>
            </div>
            <div>
              <div class="text-sm text-muted-foreground">{{ t("openlineageSettings.rootJob") }}</div>
              <div>{{ formatJobRef(run.rootJobNamespace, run.rootJobName) }}</div>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{{ t("openlineageSettings.rawPayload") }}</CardTitle>
        </CardHeader>
        <CardContent>
          <pre class="overflow-x-auto rounded-md bg-muted p-4 text-xs leading-6">{{ formattedPayload }}</pre>
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
import { getOpenLineageRun } from "@/api/openlineage";
import AppLoading from "@/components/common/AppLoading.vue";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { extractOpenLineageDatasets } from "@/lib/openlineage";
import type { OpenLineageRunResource } from "@/types/proto-es/v1/openlineage_service_pb";

const route = useRoute();
const router = useRouter();
const { t, locale } = useI18n();
const { handleError } = useErrorHandler();

const isLoading = ref(false);
const run = ref<OpenLineageRunResource | null>(null);

const currentGuid = computed(() => {
  const guidParam = route.params.guid;
  return Array.isArray(guidParam) ? guidParam[0] : (guidParam ?? "");
});

const formattedPayload = computed(() => {
  if (!run.value?.rawPayload) return "";
  try {
    return JSON.stringify(JSON.parse(run.value.rawPayload), null, 2);
  } catch {
    return run.value.rawPayload;
  }
});

const relatedDatasets = computed(() => {
  return extractOpenLineageDatasets(run.value?.rawPayload ?? "");
});

const inputDatasets = computed(() => {
  return relatedDatasets.value.inputs;
});

const outputDatasets = computed(() => {
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

function goBack() {
  const from = route.query.from;
  if (typeof from === "string" && from.length > 0) {
    router.push(from);
    return;
  }

  router.push({ name: "OpenLineageEvents" });
}

function openJobDetail() {
  if (!run.value?.taskGuid) {
    return;
  }

  router.push({
    name: "OpenLineageTaskDetail",
    params: { guid: run.value.taskGuid },
    query: { from: route.fullPath },
  });
}

function openGraph() {
  if (!run.value?.taskGuid) {
    return;
  }

  router.push({
    name: "LineageGraph",
    params: { guid: run.value.taskGuid },
    query: {
      metaType: "100",
      from: route.fullPath,
    },
  });
}

async function fetchRun() {
  if (!currentGuid.value) return;
  isLoading.value = true;
  try {
    run.value = await getOpenLineageRun(currentGuid.value);
  } catch (error) {
    handleError(error);
  } finally {
    isLoading.value = false;
  }
}

onMounted(() => {
  fetchRun();
});
</script>