<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.events')"
      :description="t('openlineage.eventsDescription')"
    />

    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <!-- Counted by the server across the whole registry: the table shows one
           page at a time, so anything derived from the rows on screen would be a
           summary of one page. -->
      <StatCard
        :label="t('openlineage.eventsTotal')"
        :value="totalEventCount"
      />
      <StatCard
        :label="t('openlineage.jobsCovered')"
        :value="totalJobCount"
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
            v-if="runs.length === 0"
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
              <!-- The whole row opens the run. The run id stays the copyable
                   value it is (its popover shows all of it) and the eye button
                   stays the labelled action, so the row is a convenience rather
                   than the only way in. -->
              <TableRow
                v-for="run in runs"
                :key="run.guid"
                class="cursor-pointer"
                @click="handleRowClick(run.guid)"
              >
                <TableCell class="whitespace-nowrap">{{ formatTimestamp(run.eventTime) }}</TableCell>
                <TableCell>
                  <Badge :variant="openLineageStatusVariant(run.eventType)">
                    {{ run.eventType || "-" }}
                  </Badge>
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
                <TableCell
                  class="sticky right-0 bg-background text-right"
                  @click.stop
                >
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

        <TablePager
          v-model:page-size="pageSize"
          :has-previous="hasPrevious"
          :has-next="hasNext"
          :disabled="isLoading"
          @previous="goPrevious"
          @next="goNext"
        />
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
import {
  listOpenLineageFilterOptions,
  listOpenLineageRuns,
} from "@/api/openlineage";
import type {
  ActiveFilter,
  FilterCategory,
} from "@/components/common/AdvancedSearchBar.vue";
import AdvancedSearchBar from "@/components/common/AdvancedSearchBar.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import StatCard from "@/components/common/StatCard.vue";
import TablePager from "@/components/common/TablePager.vue";
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
import { usePagedFetch } from "@/composables/usePagedFetch";
import type {
  ListOpenLineageFilterOptionsResponse,
  OpenLineageFilterOption,
  OpenLineageRun,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { formatDateTime } from "@/utils/datetime";
import { openLineageStatusVariant } from "@/utils/openlineageStatus";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();

const pageSize = ref(50);
const activeFilters = ref<ActiveFilter[]>([]);
const lineageOnly = ref(route.query.lineageOnly !== "false");
const facets = ref<ListOpenLineageFilterOptionsResponse | null>(null);

function filterValue(type: string): string {
  return (
    activeFilters.value.find((filter) => filter.type === type)?.value ?? ""
  );
}

// The filter menus read their values from the server: the table takes one page
// at a time, so a menu built from the rows on screen would offer only the
// namespaces that happen to be on that page. The summary cards read the same
// answer, so it is fetched once and kept.
async function loadFacets(): Promise<ListOpenLineageFilterOptionsResponse | null> {
  if (!facets.value) {
    try {
      facets.value = await listOpenLineageFilterOptions();
    } catch (error) {
      // The menus and the summary cards are an enhancement; a failure must not
      // take the table down with it. Nothing is cached, so the next menu open
      // retries.
      handleError(error);
    }
  }
  return facets.value;
}

function asFilterOptions(values: OpenLineageFilterOption[]) {
  return values.map((value) => ({ value: value.value, label: value.value }));
}

const filterCategories = computed<FilterCategory[]>(() => [
  {
    type: "namespace",
    label: t("openlineageSettings.namespace"),
    icon: "📦",
    options: async () =>
      asFilterOptions((await loadFacets())?.jobNamespaces ?? []),
  },
  {
    type: "eventType",
    label: t("openlineage.eventType"),
    icon: "🏷️",
    options: async () =>
      asFilterOptions((await loadFacets())?.eventTypes ?? []),
  },
]);

const {
  items: runs,
  isLoading,
  hasNext,
  hasPrevious,
  reset,
  goNext,
  goPrevious,
} = usePagedFetch<OpenLineageRun>({
  fetchPage: async (pageToken, signal) => {
    const response = await listOpenLineageRuns({
      pageSize: pageSize.value,
      pageToken,
      search: filterValue("name"),
      jobNamespace: filterValue("namespace"),
      eventType: filterValue("eventType"),
      hasLineage: lineageOnly.value,
      signal,
    });
    return { items: response.runs, nextPageToken: response.nextPageToken };
  },
  onError: handleError,
});

const totalEventCount = computed(() => Number(facets.value?.totalRuns ?? 0));
const totalJobCount = computed(() => Number(facets.value?.totalJobs ?? 0));
const namespaceCount = computed(() =>
  Number(facets.value?.totalJobNamespaces ?? 0)
);

function handleFiltersUpdate(filters: ActiveFilter[]) {
  activeFilters.value = filters;
  const nextQuery: Record<string, string> = {};

  if (route.query.from && typeof route.query.from === "string") {
    nextQuery.from = route.query.from;
  }
  const nameFilter = filters.find((filter) => filter.type === "name");
  if (nameFilter?.value) {
    nextQuery.search = nameFilter.value;
  }
  const namespaceFilter = filters.find((filter) => filter.type === "namespace");
  if (namespaceFilter?.value) {
    nextQuery.namespace = namespaceFilter.value;
  }
  const eventTypeFilter = filters.find((filter) => filter.type === "eventType");
  if (eventTypeFilter?.value) {
    nextQuery.eventType = eventTypeFilter.value;
  }

  router.replace({ query: nextQuery });
  void reset();
}

function resetFilters() {
  activeFilters.value = [];
  // Turning the toggle back on reloads through the watcher below; when it is
  // already on, nothing else would, so the reload is asked for here.
  const reloadHere = lineageOnly.value;
  lineageOnly.value = true;
  router.replace({ query: {} });
  if (reloadHere) {
    void reset();
  }
}

// A filter, the lineage toggle and the page size each restart the walk at its
// first page: a cursor belongs to the query that produced it.
watch([lineageOnly, pageSize], () => {
  void reset();
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

/**
 * A click anywhere in the row opens the run. A click that ends a text selection
 * is someone copying an identifier out of the table, not asking to navigate, so
 * it is left alone.
 */
function handleRowClick(guid: string) {
  if (window.getSelection()?.toString()) {
    return;
  }
  openDetail(guid);
}

onMounted(async () => {
  void reset();
  void loadFacets();
});
</script>