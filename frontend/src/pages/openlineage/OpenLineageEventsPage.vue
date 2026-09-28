<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.events')"
      :description="t('openlineage.eventsDescription')"
    />

    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <StatCard
        :label="t('openlineage.visibleEvents')"
        :value="filteredRuns.length"
      />
      <!-- Not "lineage events": the lineage toggle is on by default, so that
           count is this one's twin until someone unchecks it. -->
      <StatCard
        :label="t('openlineage.jobsCovered')"
        :value="jobCount"
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
        :search-placeholder="t('openlineage.searchEventsPlaceholder')"
        @update:filters="handleFiltersUpdate"
      />
      <div class="flex items-center gap-2">
        <Checkbox
          id="events-lineage-only"
          :checked="lineageOnly"
          @update:checked="lineageOnly = $event === true"
        />
        <Label for="events-lineage-only" class="cursor-pointer text-sm whitespace-nowrap">
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
            v-if="filteredRuns.length === 0"
            :icon="Files"
            :title="t('openlineageSettings.noRuns')"
          />
          <!-- Eleven columns overflowed the card by 521px, and the sticky action
               column was the only way to reach a row's action. The job's
               namespace is now its second line, the producer URI stays in the
               run detail (it is a constant per integration, and long), the two
               dataset counts share a cell, the run id is capped — its popover
               still shows the whole value — and the action is an icon. The
               lineage badge is gone: the lineage toggle is on by default, so it
               restated the filter rather than the row.
               `whitespace-nowrap` keeps Chinese labels from stacking one
               character per line. -->
          <Table
            v-else
            class="[&_th]:whitespace-nowrap"
          >
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("openlineageSettings.eventTime") }}</TableHead>
                <TableHead>{{ t("openlineage.eventType") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.jobName") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.runId") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.sourceLabel") }}</TableHead>
                <TableHead>{{ t("openlineage.inputsOutputs") }}</TableHead>
                <TableHead class="sticky right-0 bg-background text-right">
                  {{ t("openlineageSettings.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="run in filteredRuns" :key="run.guid">
                <TableCell class="whitespace-nowrap">{{ formatTimestamp(run.eventTime) }}</TableCell>
                <TableCell>
                  <Badge variant="outline">{{ run.eventType || "-" }}</Badge>
                </TableCell>
                <TableCell>
                  <div
                    class="max-w-64 truncate"
                    :title="run.jobName"
                  >
                    {{ run.jobName }}
                  </div>
                  <div
                    class="max-w-64 truncate font-mono text-xs text-muted-foreground"
                    :title="run.jobNamespace"
                  >
                    {{ run.jobNamespace }}
                  </div>
                </TableCell>
                <TableCell class="font-mono text-sm">
                  <div class="max-w-32">
                    <ExpandableText
                      :text="run.runId"
                      :dialog-title="t('openlineageSettings.runId')"
                    />
                  </div>
                </TableCell>
                <TableCell>
                  <div
                    class="max-w-32 truncate"
                    :title="run.source"
                  >
                    {{ run.source || "-" }}
                  </div>
                </TableCell>
                <TableCell
                  class="whitespace-nowrap"
                  :title="`${t('openlineage.inputs')} ${run.inputCount} · ${t('openlineage.outputs')} ${run.outputCount}`"
                >
                  {{ run.inputCount }} / {{ run.outputCount }}
                </TableCell>
                <TableCell class="sticky right-0 bg-background text-right">
                  <Button
                    variant="ghost"
                    size="icon"
                    class="h-8 w-8"
                    :title="t('openlineageSettings.viewRun')"
                    :aria-label="t('openlineageSettings.viewRun')"
                    @click="openDetail(run.guid)"
                  >
                    <Eye class="h-4 w-4 text-muted-foreground" />
                  </Button>
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
import { Eye, Files } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listOpenLineageRuns } from "@/api/openlineage";
import type { ActiveFilter } from "@/components/common/AdvancedSearchBar.vue";
import AdvancedSearchBar from "@/components/common/AdvancedSearchBar.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import StatCard from "@/components/common/StatCard.vue";
import ExpandableText from "@/components/metadata/ExpandableText.vue";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
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
import type { OpenLineageRun } from "@/types/proto-es/v1/openlineage_service_pb";
import { formatDateTime } from "@/utils/datetime";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();

const isLoading = ref(false);
const runs = ref<OpenLineageRun[]>([]);
const activeFilters = ref<ActiveFilter[]>([]);
const lineageOnly = ref(route.query.lineageOnly !== "false");

const namespaces = computed(() => {
  return Array.from(
    new Set(runs.value.map((run) => run.jobNamespace).filter(Boolean))
  ).sort((left, right) => left.localeCompare(right));
});

const eventTypes = computed(() => {
  return Array.from(
    new Set(runs.value.map((run) => run.eventType).filter(Boolean))
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
      type: "eventType",
      label: t("openlineage.eventType"),
      icon: "🏷️",
      options: eventTypes.value.map((et) => ({ value: et, label: et })),
    },
  ].filter((cat) => cat.options.length > 0);
});

function handleFiltersUpdate(filters: ActiveFilter[]) {
  activeFilters.value = filters;
  const nextQuery: Record<string, string> = {};

  if (route.query.from && typeof route.query.from === "string") {
    nextQuery.from = route.query.from;
  }
  const nameFilter = filters.find((f) => f.type === "name");
  if (nameFilter?.value) {
    nextQuery.search = nameFilter.value;
  }
  const nsFilter = filters.find((f) => f.type === "namespace");
  if (nsFilter?.value) {
    nextQuery.namespace = nsFilter.value;
  }
  const etFilter = filters.find((f) => f.type === "eventType");
  if (etFilter?.value) {
    nextQuery.eventType = etFilter.value;
  }

  router.replace({ query: nextQuery });
  fetchRuns();
}

const filteredRuns = computed(() => {
  const nameFilter =
    activeFilters.value.find((f) => f.type === "name")?.value ?? "";
  const nsFilter =
    activeFilters.value.find((f) => f.type === "namespace")?.value ?? "";
  const etFilter =
    activeFilters.value.find((f) => f.type === "eventType")?.value ?? "";

  const query = nameFilter.toLowerCase();

  return runs.value.filter((run) => {
    if (nsFilter && run.jobNamespace !== nsFilter) {
      return false;
    }
    if (etFilter && run.eventType !== etFilter) {
      return false;
    }
    if (lineageOnly.value && !run.hasLineage) {
      return false;
    }
    if (!query) {
      return true;
    }
    const haystack = [
      run.jobName,
      run.jobNamespace,
      run.runId,
      run.eventType,
      run.producer,
      run.source,
    ]
      .join(" ")
      .toLowerCase();
    return haystack.includes(query);
  });
});

const jobCount = computed(() => {
  return new Set(filteredRuns.value.map((run) => run.jobName).filter(Boolean))
    .size;
});

const namespaceCount = computed(() => {
  return new Set(
    filteredRuns.value.map((run) => run.jobNamespace).filter(Boolean)
  ).size;
});

function formatTimestamp(ts: Timestamp | undefined): string {
  return formatDateTime(ts, locale.value);
}

function openDetail(guid: string) {
  router.push({
    name: "OpenLineageRunDetail",
    params: { guid },
    query: { from: route.fullPath },
  });
}

function resetFilters() {
  activeFilters.value = [];
  lineageOnly.value = true;
  router.replace({ query: {} });
  fetchRuns();
}

watch([lineageOnly], () => {
  const nextQuery = { ...route.query };
  if (!lineageOnly.value) {
    nextQuery.lineageOnly = "false";
  } else {
    delete nextQuery.lineageOnly;
  }
  router.replace({ query: nextQuery });
  fetchRuns();
});

async function fetchRuns() {
  isLoading.value = true;
  try {
    const etFilter =
      activeFilters.value.find((f) => f.type === "eventType")?.value ?? "";
    const response = await listOpenLineageRuns({
      pageSize: 200,
      eventType: etFilter,
      hasLineage: lineageOnly.value || undefined,
    });
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