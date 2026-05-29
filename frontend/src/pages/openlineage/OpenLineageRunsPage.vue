<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.jobs')"
      :description="t('openlineage.jobsDescription')"
    />

    <div class="grid gap-4 md:grid-cols-3">
      <Card>
        <CardContent class="p-5">
          <div class="text-sm text-muted-foreground">{{ t("openlineage.visibleJobs") }}</div>
          <div class="mt-2 text-2xl font-semibold">{{ filteredTasks.length }}</div>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="p-5">
          <div class="text-sm text-muted-foreground">{{ t("openlineage.lineageReadyJobs") }}</div>
          <div class="mt-2 text-2xl font-semibold">{{ lineageReadyCount }}</div>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="p-5">
          <div class="text-sm text-muted-foreground">{{ t("openlineage.activeNamespaces") }}</div>
          <div class="mt-2 text-2xl font-semibold">{{ namespaceCount }}</div>
        </CardContent>
      </Card>
    </div>

    <Card>
      <CardContent class="flex flex-col gap-4 pt-6 xl:flex-row xl:items-end">
        <div class="flex-1 space-y-2">
          <Label for="openlineage-job-search">{{ t("openlineage.search") }}</Label>
          <Input
            id="openlineage-job-search"
            v-model="searchTerm"
            :placeholder="t('openlineage.searchJobsPlaceholder')"
          />
        </div>

        <div class="space-y-2 xl:w-64">
          <Label>{{ t("openlineageSettings.namespace") }}</Label>
          <Select v-model="selectedNamespace">
            <SelectTrigger>
              <SelectValue :placeholder="t('openlineage.namespaceAll')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{{ t("openlineage.namespaceAll") }}</SelectItem>
              <SelectItem v-for="namespace in namespaces" :key="namespace" :value="namespace">
                {{ namespace }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="space-y-2 xl:w-56">
          <Label>{{ t("openlineageSettings.jobType") }}</Label>
          <Select v-model="selectedJobType">
            <SelectTrigger>
              <SelectValue :placeholder="t('openlineage.jobTypeAll')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{{ t("openlineage.jobTypeAll") }}</SelectItem>
              <SelectItem v-for="jobType in jobTypes" :key="jobType" :value="jobType">
                {{ jobType }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex items-center gap-2 xl:pb-2">
          <Checkbox
            id="jobs-lineage-only"
            :checked="lineageOnly"
            @update:checked="lineageOnly = $event === true"
          />
          <Label for="jobs-lineage-only" class="cursor-pointer text-sm">
            {{ t("openlineage.onlyLineage") }}
          </Label>
        </div>

        <Button variant="outline" @click="resetFilters">
          {{ t("openlineage.clearFilters") }}
        </Button>
      </CardContent>
    </Card>

    <Card>
      <CardContent class="pt-6">
        <div v-if="isLoading" class="p-8 flex justify-center">
          <AppLoading />
        </div>
        <div
          v-else-if="filteredTasks.length === 0"
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
              <TableHead>{{ t("openlineageSettings.integration") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.latestEventTime") }}</TableHead>
              <TableHead>{{ t("openlineage.coverage") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.runCount") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.lineageRunCount") }}</TableHead>
              <TableHead class="text-right">{{ t("openlineageSettings.actions") }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="task in filteredTasks" :key="task.guid">
              <TableCell class="font-mono text-sm">{{ task.jobNamespace }}</TableCell>
              <TableCell>{{ task.jobName }}</TableCell>
              <TableCell>{{ task.jobType }}</TableCell>
              <TableCell>{{ task.integration || "-" }}</TableCell>
              <TableCell>{{ formatTimestamp(task.latestEventTime) }}</TableCell>
              <TableCell>
                <Badge :variant="task.lineageRunCount > 0 ? 'success' : 'secondary'">
                  {{ task.lineageRunCount > 0 ? t("openlineage.lineageReady") : t("openlineage.lineageMissing") }}
                </Badge>
              </TableCell>
              <TableCell>{{ task.runCount }}</TableCell>
              <TableCell>{{ task.lineageRunCount }}</TableCell>
              <TableCell class="text-right">
                <div class="flex justify-end gap-2">
                  <Button variant="ghost" size="sm" @click="openGraph(task.guid)">
                    {{ t("openlineage.openGraph") }}
                  </Button>
                  <Button variant="ghost" size="sm" @click="openEvents(task)">
                    {{ t("openlineage.openEvents") }}
                  </Button>
                  <Button variant="ghost" size="sm" @click="openDetail(task.guid)">
                    {{ t("openlineageSettings.viewDetail") }}
                  </Button>
                </div>
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
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listOpenLineageTasks } from "@/api/openlineage";
import AppLoading from "@/components/common/AppLoading.vue";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();

const isLoading = ref(false);
const tasks = ref<OpenLineageTaskResource[]>([]);
const searchTerm = ref(
  typeof route.query.search === "string" ? route.query.search : ""
);
const selectedNamespace = ref(
  typeof route.query.namespace === "string" ? route.query.namespace : "all"
);
const selectedJobType = ref(
  typeof route.query.jobType === "string" ? route.query.jobType : "all"
);
const lineageOnly = ref(route.query.lineageOnly !== "false");

const namespaces = computed(() => {
  return Array.from(
    new Set(tasks.value.map((task) => task.jobNamespace).filter(Boolean))
  ).sort((left, right) => left.localeCompare(right));
});

const jobTypes = computed(() => {
  return Array.from(
    new Set(tasks.value.map((task) => task.jobType).filter(Boolean))
  ).sort((left, right) => left.localeCompare(right));
});

const filteredTasks = computed(() => {
  const query = searchTerm.value.trim().toLowerCase();

  return tasks.value.filter((task) => {
    if (
      selectedNamespace.value !== "all" &&
      task.jobNamespace !== selectedNamespace.value
    ) {
      return false;
    }

    if (
      selectedJobType.value !== "all" &&
      task.jobType !== selectedJobType.value
    ) {
      return false;
    }

    if (lineageOnly.value && task.lineageRunCount <= 0) {
      return false;
    }

    if (!query) {
      return true;
    }

    const haystack = [
      task.jobName,
      task.jobNamespace,
      task.jobType,
      task.integration,
      task.processingType,
      task.latestRunId,
    ]
      .join(" ")
      .toLowerCase();

    return haystack.includes(query);
  });
});

const lineageReadyCount = computed(() => {
  return filteredTasks.value.filter((task) => task.lineageRunCount > 0).length;
});

const namespaceCount = computed(() => {
  return new Set(
    filteredTasks.value.map((task) => task.jobNamespace).filter(Boolean)
  ).size;
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

function openDetail(guid: string) {
  router.push({
    name: "OpenLineageTaskDetail",
    params: { guid },
    query: { from: route.fullPath },
  });
}

function openGraph(guid: string) {
  router.push({
    name: "LineageGraph",
    params: { guid },
    query: {
      metaType: "100",
      from: route.fullPath,
    },
  });
}

function openEvents(task: OpenLineageTaskResource) {
  router.push({
    name: "OpenLineageEvents",
    query: {
      search: task.jobName,
      namespace: task.jobNamespace,
      from: route.fullPath,
    },
  });
}

function resetFilters() {
  searchTerm.value = "";
  selectedNamespace.value = "all";
  selectedJobType.value = "all";
  lineageOnly.value = true;
}

watch([searchTerm, selectedNamespace, selectedJobType, lineageOnly], () => {
  const nextQuery: Record<string, string> = {};

  if (searchTerm.value.trim()) {
    nextQuery.search = searchTerm.value.trim();
  }
  if (selectedNamespace.value !== "all") {
    nextQuery.namespace = selectedNamespace.value;
  }
  if (selectedJobType.value !== "all") {
    nextQuery.jobType = selectedJobType.value;
  }
  if (!lineageOnly.value) {
    nextQuery.lineageOnly = "false";
  }

  router.replace({ query: nextQuery });
});

async function fetchTasks() {
  isLoading.value = true;
  try {
    const response = await listOpenLineageTasks({
      pageSize: 200,
      jobType: "",
      lineageOnly: false,
    });
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