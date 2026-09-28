<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.jobs')"
      :description="t('openlineage.jobsDescription')"
    />

    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <StatCard
        :label="t('openlineage.visibleJobs')"
        :value="filteredTasks.length"
      />
      <!-- Not "lineage-ready jobs": the lineage toggle is on by default, so that
           count is this one's twin until someone unchecks it. Total runs says
           something the row count cannot. -->
      <StatCard
        :label="t('openlineage.runsTotal')"
        :value="totalRunCount"
      />
      <StatCard
        :label="t('openlineage.activeNamespaces')"
        :value="namespaceCount"
      />
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <AdvancedSearchBar
        class="min-w-0 flex-1"
        :filter-categories="filterCategories"
        :search-placeholder="t('openlineage.searchJobsPlaceholder')"
        @update:filters="handleFiltersUpdate"
      />
      <div class="flex items-center gap-2">
        <Checkbox
          id="jobs-lineage-only"
          :checked="lineageOnly"
          @update:checked="lineageOnly = $event === true"
        />
        <Label for="jobs-lineage-only" class="cursor-pointer text-sm whitespace-nowrap">
          {{ t("openlineage.onlyLineage") }}
        </Label>
      </div>
      <Button variant="outline" size="sm" @click="resetFilters">
        {{ t("openlineage.clearFilters") }}
      </Button>
    </div>

    <Card>
      <CardContent class="pt-6">
        <PageState :loading="isLoading">
          <EmptyState
            v-if="filteredTasks.length === 0"
            :icon="ScrollText"
            :title="t('openlineageSettings.noTasks')"
          />
          <!-- Ten columns of long values overflowed the card by 315px. The
               lineage count now shares the run-count cell, the coverage badge
               is gone (it only restated that count), the row actions collapse
               into a menu, and job type is left to the filter menu — it is a
               low-cardinality constant in practice and cost a whole column.
               `whitespace-nowrap` keeps Chinese labels from stacking one
               character per line. -->
          <Table
            v-else
            class="[&_th]:whitespace-nowrap"
          >
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("openlineageSettings.namespace") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.jobName") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.integration") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.latestEventTime") }}</TableHead>
                <TableHead>{{ t("openlineage.latestRunStatus") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.runCount") }}</TableHead>
                <TableHead class="sticky right-0 bg-background text-right">
                  {{ t("openlineageSettings.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="task in filteredTasks" :key="task.guid">
                <TableCell class="font-mono text-sm">{{ task.jobNamespace }}</TableCell>
                <TableCell>
                  <div
                    class="max-w-64 truncate"
                    :title="task.jobName"
                  >
                    {{ task.jobName }}
                  </div>
                </TableCell>
                <TableCell>{{ task.integration || "-" }}</TableCell>
                <TableCell class="whitespace-nowrap">{{ formatTimestamp(task.latestEventTime) }}</TableCell>
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
                <TableCell class="sticky right-0 bg-background text-right">
                  <DropdownMenu>
                    <DropdownMenuTrigger as-child>
                      <Button
                        variant="ghost"
                        size="icon"
                        class="h-8 w-8"
                        :title="t('openlineage.moreActions')"
                        :aria-label="t('openlineage.moreActions')"
                      >
                        <MoreHorizontal class="h-4 w-4 text-muted-foreground" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem @select="openGraph(task.guid)">
                        <Network class="mr-2 h-4 w-4" />
                        {{ t("openlineage.openGraph") }}
                      </DropdownMenuItem>
                      <DropdownMenuItem @select="openEvents(task)">
                        <Files class="mr-2 h-4 w-4" />
                        {{ t("openlineage.openEvents") }}
                      </DropdownMenuItem>
                      <DropdownMenuItem @select="openDetail(task.guid)">
                        <ScrollText class="mr-2 h-4 w-4" />
                        {{ t("openlineageSettings.viewDetail") }}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </PageState>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Files, MoreHorizontal, Network, ScrollText } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listOpenLineageTasks } from "@/api/openlineage";
import type { ActiveFilter } from "@/components/common/AdvancedSearchBar.vue";
import AdvancedSearchBar from "@/components/common/AdvancedSearchBar.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import StatCard from "@/components/common/StatCard.vue";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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
import type { OpenLineageTask } from "@/types/proto-es/v1/openlineage_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();

const isLoading = ref(false);
const tasks = ref<OpenLineageTask[]>([]);
const activeFilters = ref<ActiveFilter[]>([]);
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

const filterCategories = computed(() => {
  return [
    {
      type: "namespace",
      label: t("openlineageSettings.namespace"),
      icon: "📦",
      options: namespaces.value.map((ns) => ({ value: ns, label: ns })),
    },
    {
      type: "jobType",
      label: t("openlineageSettings.jobType"),
      icon: "⚙️",
      options: jobTypes.value.map((jt) => ({ value: jt, label: jt })),
    },
  ].filter((cat) => cat.options.length > 0);
});

function handleFiltersUpdate(filters: ActiveFilter[]) {
  activeFilters.value = filters;
  const nextQuery: Record<string, string> = {};

  const nameFilter = filters.find((f) => f.type === "name");
  if (nameFilter?.value) {
    nextQuery.search = nameFilter.value;
  }
  const nsFilter = filters.find((f) => f.type === "namespace");
  if (nsFilter?.value) {
    nextQuery.namespace = nsFilter.value;
  }
  const jtFilter = filters.find((f) => f.type === "jobType");
  if (jtFilter?.value) {
    nextQuery.jobType = jtFilter.value;
  }

  router.replace({ query: nextQuery });
}

const filteredTasks = computed(() => {
  const nameFilter =
    activeFilters.value.find((f) => f.type === "name")?.value ?? "";
  const nsFilter =
    activeFilters.value.find((f) => f.type === "namespace")?.value ?? "";
  const jtFilter =
    activeFilters.value.find((f) => f.type === "jobType")?.value ?? "";

  const query = nameFilter.toLowerCase();

  return tasks.value.filter((task) => {
    if (nsFilter && task.jobNamespace !== nsFilter) {
      return false;
    }
    if (jtFilter && task.jobType !== jtFilter) {
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

const totalRunCount = computed(() => {
  return filteredTasks.value.reduce((sum, task) => sum + task.runCount, 0);
});

const namespaceCount = computed(() => {
  return new Set(
    filteredTasks.value.map((task) => task.jobNamespace).filter(Boolean)
  ).size;
});

function formatTimestamp(ts: Timestamp | undefined): string {
  return formatDateTime(ts, locale.value);
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
    params: { guid: guidToRouteParams(guid) },
    query: {
      metaType: "100",
      from: route.fullPath,
    },
  });
}

function openEvents(task: OpenLineageTask) {
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
  activeFilters.value = [];
  lineageOnly.value = true;
  router.replace({ query: {} });
}

watch([lineageOnly], () => {
  const nextQuery = { ...route.query };
  if (!lineageOnly.value) {
    nextQuery.lineageOnly = "false";
  } else {
    delete nextQuery.lineageOnly;
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