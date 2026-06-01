<template>
  <div class="space-y-4">
    <OpenLineageSectionHeader
      :title="t('openlineage.events')"
      :description="t('openlineage.eventsDescription')"
    />

    <div class="grid gap-4 md:grid-cols-3">
      <Card>
        <CardContent class="p-5">
          <div class="text-sm text-muted-foreground">{{ t("openlineage.visibleEvents") }}</div>
          <div class="mt-2 text-2xl font-semibold">{{ filteredRuns.length }}</div>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="p-5">
          <div class="text-sm text-muted-foreground">{{ t("openlineage.lineageEvents") }}</div>
          <div class="mt-2 text-2xl font-semibold">{{ lineageEventCount }}</div>
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
          <Label for="openlineage-event-search">{{ t("openlineage.search") }}</Label>
          <Input
            id="openlineage-event-search"
            v-model="searchTerm"
            :placeholder="t('openlineage.searchEventsPlaceholder')"
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
          <Label>{{ t("openlineage.eventType") }}</Label>
          <Select v-model="selectedEventType">
            <SelectTrigger>
              <SelectValue :placeholder="t('openlineage.eventTypeAll')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{{ t("openlineage.eventTypeAll") }}</SelectItem>
              <SelectItem v-for="eventType in eventTypes" :key="eventType" :value="eventType">
                {{ eventType }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex items-center gap-2 xl:pb-2">
          <Checkbox
            id="events-lineage-only"
            :checked="lineageOnly"
            @update:checked="lineageOnly = $event === true"
          />
          <Label for="events-lineage-only" class="cursor-pointer text-sm">
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
              <TableHead class="text-right">{{ t("openlineageSettings.actions") }}</TableHead>
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
              <TableCell class="font-mono text-sm">{{ run.runId }}</TableCell>
              <TableCell class="max-w-48 truncate" :title="run.producer">{{ run.producer || "-" }}</TableCell>
              <TableCell>{{ run.source || "-" }}</TableCell>
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
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listOpenLineageRuns } from "@/api/openlineage";
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
import type { OpenLineageRunResource } from "@/types/proto-es/v1/openlineage_service_pb";

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();

const isLoading = ref(false);
const runs = ref<OpenLineageRunResource[]>([]);
const searchTerm = ref(
  typeof route.query.search === "string" ? route.query.search : ""
);
const selectedNamespace = ref(
  typeof route.query.namespace === "string" ? route.query.namespace : "all"
);
const selectedEventType = ref(
  typeof route.query.eventType === "string" ? route.query.eventType : "all"
);
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

const filteredRuns = computed(() => {
  const query = searchTerm.value.trim().toLowerCase();

  return runs.value.filter((run) => {
    if (
      selectedNamespace.value !== "all" &&
      run.jobNamespace !== selectedNamespace.value
    ) {
      return false;
    }

    if (
      selectedEventType.value !== "all" &&
      run.eventType !== selectedEventType.value
    ) {
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
  searchTerm.value = "";
  selectedNamespace.value = "all";
  selectedEventType.value = "all";
  lineageOnly.value = true;
}

watch([searchTerm, selectedNamespace, selectedEventType, lineageOnly], () => {
  const nextQuery: Record<string, string> = {};

  if (route.query.from && typeof route.query.from === "string") {
    nextQuery.from = route.query.from;
  }
  if (searchTerm.value.trim()) {
    nextQuery.search = searchTerm.value.trim();
  }
  if (selectedNamespace.value !== "all") {
    nextQuery.namespace = selectedNamespace.value;
  }
  if (selectedEventType.value !== "all") {
    nextQuery.eventType = selectedEventType.value;
  }
  if (!lineageOnly.value) {
    nextQuery.lineageOnly = "false";
  }

  router.replace({ query: nextQuery });
  fetchRuns();
});

async function fetchRuns() {
  isLoading.value = true;
  try {
    const response = await listOpenLineageRuns({
      pageSize: 200,
      eventType:
        selectedEventType.value !== "all" ? selectedEventType.value : "",
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