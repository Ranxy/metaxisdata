<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.events')"
      :description="t('openlineage.eventsDescription')"
    />

    <div class="grid gap-3 md:grid-cols-3">
      <Card>
        <CardContent class="flex items-center justify-between px-3 py-2">
          <span class="text-sm text-muted-foreground">{{ t("openlineage.visibleEvents") }}</span>
          <span class="text-xl font-semibold">{{ filteredRuns.length }}</span>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="flex items-center justify-between px-3 py-2">
          <span class="text-sm text-muted-foreground">{{ t("openlineage.lineageEvents") }}</span>
          <span class="text-xl font-semibold">{{ lineageEventCount }}</span>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="flex items-center justify-between px-3 py-2">
          <span class="text-sm text-muted-foreground">{{ t("openlineage.activeNamespaces") }}</span>
          <span class="text-xl font-semibold">{{ namespaceCount }}</span>
        </CardContent>
      </Card>
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
        <div v-if="isLoading" class="flex justify-center p-8">
          <AppLoading />
        </div>
        <div v-else-if="filteredRuns.length === 0" class="p-8 text-center text-muted-foreground">
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
              <TableHead>{{ t("openlineageSettings.producer") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.sourceLabel") }}</TableHead>
              <TableHead>{{ t("openlineage.hasLineage") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.inputCount") }}</TableHead>
              <TableHead>{{ t("openlineageSettings.outputCount") }}</TableHead>
              <TableHead class="sticky right-0 z-10 bg-background text-right">{{ t("openlineageSettings.actions") }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="run in filteredRuns" :key="run.guid">
              <TableCell>{{ formatTimestamp(run.eventTime) }}</TableCell>
              <TableCell>
                <Badge variant="outline">{{ run.eventType || "-" }}</Badge>
              </TableCell>
              <TableCell>{{ run.jobName }}</TableCell>
              <TableCell class="font-mono text-sm">{{ run.jobNamespace }}</TableCell>
              <TableCell class="font-mono text-sm"><ExpandableText :text="run.runId" /></TableCell>
              <TableCell class="max-w-48">
                <ExpandableText :text="run.producer" />
              </TableCell>
              <TableCell>{{ run.source || "-" }}</TableCell>
              <TableCell>
                <Badge :variant="run.hasLineage ? 'success' : 'secondary'">
                  {{ run.hasLineage ? t("openlineage.yes") : t("openlineage.no") }}
                </Badge>
              </TableCell>
              <TableCell>{{ run.inputCount }}</TableCell>
              <TableCell>{{ run.outputCount }}</TableCell>
              <TableCell class="sticky right-0 bg-background text-right">
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
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listOpenLineageRuns } from "@/api/openlineage";
import type { ActiveFilter } from "@/components/common/AdvancedSearchBar.vue";
import AdvancedSearchBar from "@/components/common/AdvancedSearchBar.vue";
import AppLoading from "@/components/common/AppLoading.vue";
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
import type { OpenLineageRunResource } from "@/types/proto-es/v1/openlineage_service_pb";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();

const isLoading = ref(false);
const runs = ref<OpenLineageRunResource[]>([]);
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

const lineageEventCount = computed(
  () => filteredRuns.value.filter((run) => run.hasLineage).length
);

const namespaceCount = computed(() => {
  return new Set(
    filteredRuns.value.map((run) => run.jobNamespace).filter(Boolean)
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