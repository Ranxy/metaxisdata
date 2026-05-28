<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ t("auditLogs.title") }}
        </h1>
        <p class="text-muted-foreground">
          {{ t("auditLogs.description") }}
        </p>
      </div>
      <div class="flex items-center gap-2">
        <Button :disabled="isLoading || isExporting" variant="outline" @click="exportCsv">
          <Download class="mr-2 h-4 w-4" :class="{ 'animate-pulse': isExporting }" />
          {{ isExporting ? t("auditLogs.exportingCsv") : t("auditLogs.exportCsv") }}
        </Button>
        <Button :disabled="isLoading || isExporting" @click="refreshLogs">
          <RefreshCcw class="mr-2 h-4 w-4" :class="{ 'animate-spin': isLoading }" />
          {{ t("auditLogs.refresh") }}
        </Button>
      </div>
    </div>

    <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_auto] xl:items-start">
      <div class="space-y-3">
        <div ref="searchBarRef" class="relative">
          <div
            class="flex min-h-[46px] w-full flex-wrap items-center gap-2 rounded-md border border-input bg-background px-3 py-2 text-sm transition-colors hover:border-ring focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2"
          >
            <button
              type="button"
              class="flex shrink-0 items-center gap-2 text-muted-foreground transition-colors hover:text-foreground"
              @click="toggleSearchPanel"
            >
              <Filter class="h-4 w-4" />
              <span class="font-medium">{{ t("auditLogs.filter") }}</span>
            </button>

            <Badge
              v-for="filter in activeFilters"
              :key="filter.id"
              variant="secondary"
              class="flex items-center gap-1 px-2 py-1"
            >
              <span class="text-xs font-medium">{{ filter.label }}:</span>
              <span class="text-xs">{{ filter.displayValue }}</span>
              <button
                type="button"
                class="ml-1 rounded-full transition-colors hover:bg-secondary-foreground/20"
                @click.stop="removeFilter(filter.id)"
              >
                <X class="h-3 w-3" />
              </button>
            </Badge>

            <Badge
              v-if="dateRangeChip"
              variant="secondary"
              class="flex items-center gap-1 px-2 py-1"
            >
              <span class="text-xs font-medium">{{ t("auditLogs.created") }}:</span>
              <span class="text-xs">{{ dateRangeChip }}</span>
              <button
                type="button"
                class="ml-1 rounded-full transition-colors hover:bg-secondary-foreground/20"
                @click.stop="clearDateRange"
              >
                <X class="h-3 w-3" />
              </button>
            </Badge>

            <input
              ref="searchInputRef"
              v-model="searchQuery"
              type="text"
              :placeholder="searchPlaceholder"
              class="min-w-[180px] flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
              @focus="openSearchPanel"
              @keydown.enter.prevent="handleSearchEnter"
              @keydown.esc.prevent="resetSearchDraft"
            >
          </div>

          <div
            v-if="showSearchPanel"
            class="absolute left-0 right-0 top-[calc(100%+8px)] z-20 overflow-hidden rounded-md border bg-popover shadow-md"
          >
            <div v-if="!selectedFilterType" class="max-h-80 overflow-auto py-2">
              <button
                v-for="filterType in filteredFilterTypes"
                :key="filterType.type"
                type="button"
                class="flex w-full items-start gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/60"
                @mousedown.prevent="selectFilterType(filterType.type)"
              >
                <div class="min-w-0 flex-1">
                  <div class="font-medium text-primary">{{ filterType.label }}</div>
                  <div class="text-sm text-muted-foreground">{{ filterType.description }}</div>
                </div>
              </button>
              <div v-if="filteredFilterTypes.length === 0" class="px-4 py-6 text-sm text-muted-foreground">
                {{ t("auditLogs.noFilterMatches") }}
              </div>
            </div>

            <div v-else-if="selectedFilterType === 'level'" class="py-2">
              <div class="border-b px-4 py-3 text-sm">
                <div class="font-medium">{{ selectedFilterMeta?.label }}</div>
                <div class="mt-1 text-muted-foreground">{{ selectedFilterMeta?.description }}</div>
              </div>
              <button
                v-for="option in severityOptions"
                :key="option.value"
                type="button"
                class="flex w-full items-center justify-between px-4 py-3 text-left transition-colors hover:bg-muted/60"
                @mousedown.prevent="applyLevelFilter(option.value)"
              >
                <span>{{ option.label }}</span>
                <span class="text-xs text-muted-foreground">{{ option.value }}</span>
              </button>
            </div>

            <div v-else class="space-y-3 p-4">
              <div>
                <div class="font-medium">{{ selectedFilterMeta?.label }}</div>
                <div class="mt-1 text-sm text-muted-foreground">{{ selectedFilterMeta?.description }}</div>
              </div>
              <div class="flex items-center justify-between gap-3 rounded-md border bg-muted/30 px-3 py-2 text-sm">
                <span class="text-muted-foreground">{{ t("auditLogs.pendingFilter") }}</span>
                <span class="font-medium">{{ searchQuery.trim() || t("auditLogs.waitingForValue") }}</span>
              </div>
              <div class="flex justify-end gap-2">
                <Button variant="ghost" size="sm" @click="resetSearchDraft">
                  {{ t("common.cancel") }}
                </Button>
                <Button size="sm" :disabled="!searchQuery.trim()" @click="applyTextFilter">
                  {{ t("auditLogs.applyFilter") }}
                </Button>
              </div>
            </div>
          </div>
        </div>

        <div v-if="hasActiveSearch" class="flex items-center justify-between text-xs text-muted-foreground">
          <span>
            {{ t("auditLogs.filterActive", { count: activeFilters.length + (dateRangeChip ? 1 : 0) }) }}
          </span>
          <Button variant="ghost" size="sm" class="h-auto p-0 text-xs hover:underline" @click="clearAllFilters">
            {{ t("auditLogs.clearFilters") }}
          </Button>
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-2 xl:justify-end">
        <Popover v-model:open="showDateRangePicker">
          <PopoverTrigger as-child>
            <Button
              variant="outline"
              class="min-h-[46px] min-w-[18rem] justify-between gap-3 px-3 text-left font-normal shadow-sm"
            >
              <span class="flex min-w-0 items-center gap-2">
                <CalendarRange class="h-4 w-4 shrink-0 text-muted-foreground" />
                <span
                  class="truncate"
                  :class="dateRangeChip ? 'text-foreground' : 'text-muted-foreground'"
                >
                  {{ dateRangeButtonLabel }}
                </span>
              </span>
            </Button>
          </PopoverTrigger>
          <PopoverContent align="end" class="w-auto p-0">
            <div class="space-y-4 p-4">
              <div class="space-y-1">
                <div class="text-sm font-medium">{{ t("auditLogs.created") }}</div>
                <div class="text-xs text-muted-foreground">
                  {{ draftDateRangeChip || t("auditLogs.selectDateRangeHint") }}
                </div>
              </div>

              <RangeCalendarRoot
                v-model="draftDateRange"
                :locale="locale"
                :number-of-months="2"
                fixed-weeks
                initial-focus
                class="rounded-md border p-3"
              >
                <template #default="{ grid, weekDays }">
                  <RangeCalendarHeader class="mb-4 flex items-center justify-between">
                    <RangeCalendarPrev class="inline-flex h-8 w-8 items-center justify-center rounded-md border border-input bg-background transition-colors hover:bg-accent hover:text-accent-foreground">
                      <ChevronLeft class="h-4 w-4" />
                    </RangeCalendarPrev>
                    <RangeCalendarHeading class="text-sm font-medium" />
                    <RangeCalendarNext class="inline-flex h-8 w-8 items-center justify-center rounded-md border border-input bg-background transition-colors hover:bg-accent hover:text-accent-foreground">
                      <ChevronRight class="h-4 w-4" />
                    </RangeCalendarNext>
                  </RangeCalendarHeader>

                  <div class="flex flex-col gap-4 sm:flex-row sm:gap-6">
                    <RangeCalendarGrid
                      v-for="month in grid"
                      :key="month.value.toString()"
                      class="select-none space-y-1"
                    >
                      <RangeCalendarGridHead>
                        <RangeCalendarGridRow class="mb-1 flex">
                          <RangeCalendarHeadCell
                            v-for="day in weekDays"
                            :key="day"
                            class="w-9 rounded-md text-xs font-normal text-muted-foreground"
                          >
                            {{ day }}
                          </RangeCalendarHeadCell>
                        </RangeCalendarGridRow>
                      </RangeCalendarGridHead>
                      <RangeCalendarGridBody class="space-y-1">
                        <RangeCalendarGridRow
                          v-for="(weekDates, weekIndex) in month.rows"
                          :key="`${month.value.toString()}-${weekIndex}`"
                          class="flex"
                        >
                          <RangeCalendarCell
                            v-for="weekDate in weekDates"
                            :key="weekDate.toString()"
                            :date="weekDate"
                            class="relative h-9 w-9 p-0 text-center text-sm focus-within:relative focus-within:z-20 [&:has([data-highlighted])]:bg-accent/50 [&:has([data-selection-end])]:rounded-r-md [&:has([data-selection-start])]:rounded-l-md"
                          >
                            <RangeCalendarCellTrigger
                              :day="weekDate"
                              :month="month.value"
                              class="flex h-9 w-9 items-center justify-center rounded-md border border-transparent bg-transparent p-0 text-sm font-normal outline-none transition-colors hover:bg-accent hover:text-accent-foreground focus:ring-2 focus:ring-ring focus:ring-offset-2 data-[disabled]:pointer-events-none data-[disabled]:opacity-30 data-[highlighted]:bg-accent/80 data-[outside-view]:text-muted-foreground/30 data-[selected]:bg-primary data-[selected]:text-primary-foreground data-[selection-end]:bg-primary data-[selection-end]:text-primary-foreground data-[selection-start]:bg-primary data-[selection-start]:text-primary-foreground data-[today]:border-border"
                            />
                          </RangeCalendarCell>
                        </RangeCalendarGridRow>
                      </RangeCalendarGridBody>
                    </RangeCalendarGrid>
                  </div>
                </template>
              </RangeCalendarRoot>

              <div class="flex items-center justify-between border-t pt-3">
                <Button variant="ghost" size="sm" @click="resetDraftDateRange">
                  {{ t("auditLogs.clearDateRange") }}
                </Button>
                <div class="flex items-center gap-2">
                  <Button variant="ghost" size="sm" @click="showDateRangePicker = false">
                    {{ t("common.cancel") }}
                  </Button>
                  <Button size="sm" @click="applyDraftDateRange">
                    {{ t("auditLogs.applyDateRange") }}
                  </Button>
                </div>
              </div>
            </div>
          </PopoverContent>
        </Popover>
      </div>
    </div>

    <Card>
      <CardHeader>
        <div class="flex items-center justify-between gap-4">
          <div>
            <CardTitle>{{ t("auditLogs.entries") }}</CardTitle>
            <CardDescription>
              {{ t("auditLogs.entriesDescription", { count: logs.length }) }}
            </CardDescription>
          </div>
          <div class="text-sm text-muted-foreground">
            {{ t("auditLogs.parentScope") }}: <span class="font-mono">workspaces/-</span>
          </div>
        </div>
      </CardHeader>
      <CardContent class="space-y-4">
        <div v-if="isLoading" class="p-8 flex justify-center">
          <AppLoading />
        </div>

        <div v-else-if="error" class="rounded-md border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
          {{ error }}
        </div>

        <div v-else-if="logs.length === 0" class="p-10 text-center text-muted-foreground">
          <ClipboardList class="mx-auto mb-4 h-12 w-12 text-muted-foreground/50" />
          <p class="text-base font-medium">{{ t("auditLogs.emptyTitle") }}</p>
          <p class="mt-1 text-sm">{{ t("auditLogs.emptyDescription") }}</p>
        </div>

        <Table v-else class="min-w-[88rem]">
          <TableHeader>
            <TableRow>
              <TableHead class="w-[11rem] whitespace-nowrap">{{ t("auditLogs.time") }}</TableHead>
              <TableHead class="w-[6rem] whitespace-nowrap">{{ t("auditLogs.severity") }}</TableHead>
              <TableHead class="min-w-[20rem] whitespace-nowrap">{{ t("auditLogs.method") }}</TableHead>
              <TableHead class="min-w-[15rem] whitespace-nowrap">{{ t("auditLogs.resource") }}</TableHead>
              <TableHead class="min-w-[13rem] whitespace-nowrap">{{ t("auditLogs.user") }}</TableHead>
              <TableHead class="w-[8rem] whitespace-nowrap">{{ t("auditLogs.status") }}</TableHead>
              <TableHead class="min-w-[16rem] whitespace-nowrap">{{ t("auditLogs.requestMeta") }}</TableHead>
              <TableHead class="w-[6rem] whitespace-nowrap text-right">{{ t("auditLogs.actions") }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="log in logs" :key="log.name">
              <TableCell class="whitespace-nowrap text-muted-foreground">
                {{ formatTimestamp(log.createTime) }}
              </TableCell>
              <TableCell class="whitespace-nowrap">
                <Badge :variant="getSeverityVariant(log.severity)" class="min-w-[3.5rem] justify-center whitespace-nowrap">
                  {{ getSeverityLabel(log.severity) }}
                </Badge>
              </TableCell>
              <TableCell class="max-w-[22rem]">
                <ExpandableText
                  :text="log.method"
                  :dialog-title="t('auditLogs.method')"
                  text-class="font-mono text-xs"
                />
              </TableCell>
              <TableCell class="max-w-[18rem]">
                <ExpandableText
                  :text="getAuditIdentityDisplay(log.resource)"
                  :dialog-title="t('auditLogs.resource')"
                  text-class="text-xs"
                />
              </TableCell>
              <TableCell class="max-w-[16rem]">
                <ExpandableText
                  :text="getAuditIdentityDisplay(log.user)"
                  :dialog-title="t('auditLogs.user')"
                  text-class="text-xs"
                />
              </TableCell>
              <TableCell class="whitespace-nowrap">
                <div class="space-y-1">
                  <div class="font-medium">{{ getStatusLabel(log) }}</div>
                  <div class="text-xs text-muted-foreground">
                    {{ t("auditLogs.latency", { value: String(log.latencyMs) }) }}
                  </div>
                </div>
              </TableCell>
              <TableCell class="max-w-[16rem] text-xs text-muted-foreground">
                <div>{{ log.requestMetadata?.ip || '-' }}</div>
                <ExpandableText
                  :text="log.requestMetadata?.userAgent || '-'"
                  :dialog-title="t('auditLogs.userAgent')"
                  text-class="block max-w-[14rem] truncate"
                />
              </TableCell>
              <TableCell class="text-right">
                <Button variant="ghost" size="sm" @click="openDetails(log)">
                  {{ t("common.details") }}
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>

        <div class="flex items-center justify-between border-t pt-4">
          <div class="text-sm text-muted-foreground">
            {{ t("auditLogs.pageStatus", { count: logs.length }) }}
          </div>
          <div class="flex items-center gap-2">
            <Button variant="outline" :disabled="previousPageTokens.length === 0 || isLoading" @click="goToPreviousPage">
              {{ t("common.previous") }}
            </Button>
            <Button variant="outline" :disabled="!nextPageToken || isLoading" @click="goToNextPage">
              {{ t("common.next") }}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>

    <AppModal v-model="showDetails" :title="t('auditLogs.detailTitle')" size="xl">
      <div v-if="selectedLog" class="space-y-6">
        <div class="grid gap-4 md:grid-cols-2">
          <div>
            <div class="text-sm text-muted-foreground">{{ t("auditLogs.time") }}</div>
            <div>{{ formatTimestamp(selectedLog.createTime) }}</div>
          </div>
          <div>
            <div class="text-sm text-muted-foreground">{{ t("auditLogs.severity") }}</div>
            <div><Badge :variant="getSeverityVariant(selectedLog.severity)">{{ getSeverityLabel(selectedLog.severity) }}</Badge></div>
          </div>
          <div>
            <div class="text-sm text-muted-foreground">{{ t("auditLogs.method") }}</div>
            <div class="font-mono text-xs break-all">{{ selectedLog.method }}</div>
          </div>
          <div>
            <div class="text-sm text-muted-foreground">{{ t("auditLogs.resource") }}</div>
            <div class="text-xs break-all">{{ getAuditIdentityDisplay(selectedLog.resource) }}</div>
          </div>
          <div>
            <div class="text-sm text-muted-foreground">{{ t("auditLogs.user") }}</div>
            <div class="text-xs break-all">{{ getAuditIdentityDisplay(selectedLog.user) }}</div>
          </div>
          <div>
            <div class="text-sm text-muted-foreground">{{ t("auditLogs.status") }}</div>
            <div>{{ getStatusLabel(selectedLog) }}</div>
          </div>
        </div>

        <div class="grid gap-4 lg:grid-cols-3">
          <Card>
            <CardHeader>
              <CardTitle class="text-base">{{ t("auditLogs.request") }}</CardTitle>
            </CardHeader>
            <CardContent>
              <pre class="max-h-96 overflow-auto rounded-md bg-muted p-4 text-xs leading-6">{{ formatJson(selectedLog.request) }}</pre>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle class="text-base">{{ t("auditLogs.response") }}</CardTitle>
            </CardHeader>
            <CardContent>
              <pre class="max-h-96 overflow-auto rounded-md bg-muted p-4 text-xs leading-6">{{ formatJson(selectedLog.response) }}</pre>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle class="text-base">{{ t("auditLogs.serviceData") }}</CardTitle>
            </CardHeader>
            <CardContent>
              <pre class="max-h-96 overflow-auto rounded-md bg-muted p-4 text-xs leading-6">{{ formatJson(selectedLog.serviceData) }}</pre>
            </CardContent>
          </Card>
        </div>
      </div>
      <template #footer>
        <Button variant="outline" @click="showDetails = false">
          {{ t("common.cancel") }}
        </Button>
      </template>
    </AppModal>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { getLocalTimeZone, today } from "@internationalized/date";
import { onClickOutside } from "@vueuse/core";
import {
  CalendarRange,
  ChevronLeft,
  ChevronRight,
  ClipboardList,
  Download,
  Filter,
  RefreshCcw,
  X,
} from "lucide-vue-next";
import {
  RangeCalendarCell,
  RangeCalendarCellTrigger,
  RangeCalendarGrid,
  RangeCalendarGridBody,
  RangeCalendarGridHead,
  RangeCalendarGridRow,
  RangeCalendarHeadCell,
  RangeCalendarHeader,
  RangeCalendarHeading,
  RangeCalendarNext,
  RangeCalendarPrev,
  RangeCalendarRoot,
} from "radix-vue";
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listAuditLogs } from "@/api/audit";
import { batchGetUsers } from "@/api/user";
import AppLoading from "@/components/common/AppLoading.vue";
import AppModal from "@/components/common/AppModal.vue";
import ExpandableText from "@/components/metadata/ExpandableText.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import {
  type AuditLog,
  AuditLogSeverity,
} from "@/types/proto-es/v1/audit_log_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

type AuditFilterType = "resource" | "actor" | "method" | "level";
type SeverityValue = "INFO" | "WARNING" | "ERROR";

interface AuditFilter {
  id: string;
  type: AuditFilterType;
  label: string;
  value: string;
  displayValue: string;
}

interface FilterTypeOption {
  type: AuditFilterType;
  label: string;
  description: string;
  placeholder: string;
}

type CalendarDateLike =
  | {
      toString(): string;
    }
  | undefined;

interface CalendarRangeValue {
  start: CalendarDateLike;
  end: CalendarDateLike;
}

const WORKSPACE_PARENT = "workspaces/-";

const { t, locale } = useI18n();
const { handleError } = useErrorHandler();

const logs = ref<AuditLog[]>([]);
const isLoading = ref(false);
const isExporting = ref(false);
const error = ref("");
const nextPageToken = ref("");
const currentPageToken = ref("");
const previousPageTokens = ref<string[]>([]);
const showDetails = ref(false);
const selectedLog = ref<AuditLog | null>(null);
const auditUserDisplayMap = ref<Record<string, string>>({});

const activeFilters = ref<AuditFilter[]>([]);
const searchQuery = ref("");
const selectedFilterType = ref<AuditFilterType | null>(null);
const showSearchPanel = ref(false);
const showDateRangePicker = ref(false);
const dateRange = ref<CalendarRangeValue>(createDefaultDateRange());
const draftDateRange = ref<any>(createDefaultDateRange());
const pendingAuditUsers = new Set<string>();

const searchBarRef = ref<HTMLElement | null>(null);
const searchInputRef = ref<HTMLInputElement | null>(null);

const filterTypes = computed<FilterTypeOption[]>(() => [
  {
    type: "resource",
    label: t("auditLogs.filterResource"),
    description: t("auditLogs.filterResourceDescription"),
    placeholder: t("auditLogs.resourcePlaceholder"),
  },
  {
    type: "actor",
    label: t("auditLogs.filterActor"),
    description: t("auditLogs.filterActorDescription"),
    placeholder: t("auditLogs.userPlaceholder"),
  },
  {
    type: "method",
    label: t("auditLogs.filterMethod"),
    description: t("auditLogs.filterMethodDescription"),
    placeholder: t("auditLogs.methodPlaceholder"),
  },
  {
    type: "level",
    label: t("auditLogs.filterLevel"),
    description: t("auditLogs.filterLevelDescription"),
    placeholder: t("auditLogs.severityAll"),
  },
]);

const severityOptions = computed(() => [
  { value: "INFO" as const, label: t("auditLogs.severityInfo") },
  { value: "WARNING" as const, label: t("auditLogs.severityWarning") },
  { value: "ERROR" as const, label: t("auditLogs.severityError") },
]);

const filteredFilterTypes = computed(() => {
  const query = searchQuery.value.trim().toLowerCase();
  if (!query) {
    return filterTypes.value;
  }
  return filterTypes.value.filter(
    (item) =>
      item.label.toLowerCase().includes(query) ||
      item.description.toLowerCase().includes(query) ||
      item.type.toLowerCase().includes(query)
  );
});

const selectedFilterMeta = computed(() =>
  filterTypes.value.find((item) => item.type === selectedFilterType.value)
);

const searchPlaceholder = computed(() => {
  if (selectedFilterMeta.value) {
    return selectedFilterMeta.value.placeholder;
  }
  return t("auditLogs.advancedSearchPlaceholder");
});

const hasActiveSearch = computed(
  () => activeFilters.value.length > 0 || Boolean(dateRangeChip.value)
);

const dateRangeButtonLabel = computed(
  () => dateRangeChip.value || t("auditLogs.selectDateRange")
);

const dateRangeChip = computed(() => {
  if (!dateRange.value.start && !dateRange.value.end) {
    return "";
  }
  const from = formatDateValue(dateRange.value.start);
  const to = formatDateValue(dateRange.value.end);
  if (from && to) {
    return `${from} - ${to}`;
  }
  if (from) {
    return `>= ${from}`;
  }
  if (to) {
    return `<= ${to}`;
  }
  return "";
});

const draftDateRangeChip = computed(() => {
  if (!draftDateRange.value.start && !draftDateRange.value.end) {
    return "";
  }
  const from = formatDateValue(draftDateRange.value.start);
  const to = formatDateValue(draftDateRange.value.end);
  if (from && to) {
    return `${from} - ${to}`;
  }
  if (from) {
    return `>= ${from}`;
  }
  if (to) {
    return `<= ${to}`;
  }
  return "";
});

const filterExpression = computed(() => {
  const clauses: string[] = [];
  for (const filter of activeFilters.value) {
    if (!filter.value) {
      continue;
    }
    if (filter.type === "resource") {
      clauses.push(`resource == ${JSON.stringify(filter.value)}`);
    } else if (filter.type === "actor") {
      clauses.push(`user == ${JSON.stringify(filter.value)}`);
    } else if (filter.type === "method") {
      clauses.push(`method == ${JSON.stringify(filter.value)}`);
    } else if (filter.type === "level") {
      clauses.push(`severity == ${JSON.stringify(filter.value)}`);
    }
  }
  const from = toRFC3339(dateRange.value.start, "start");
  if (from) {
    clauses.push(`create_time >= ${JSON.stringify(from)}`);
  }
  const to = toRFC3339(dateRange.value.end, "end");
  if (to) {
    clauses.push(`create_time <= ${JSON.stringify(to)}`);
  }
  return clauses.join(" && ");
});

watch(showDateRangePicker, (open) => {
  if (open) {
    draftDateRange.value = cloneDateRange(dateRange.value);
  }
});

onClickOutside(searchBarRef, () => {
  showSearchPanel.value = false;
  if (!selectedFilterType.value) {
    searchQuery.value = "";
  }
});

function generateUniqueId(): string {
  if (typeof crypto !== "undefined" && crypto.randomUUID) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

function toggleSearchPanel() {
  showSearchPanel.value = !showSearchPanel.value;
  if (showSearchPanel.value) {
    nextTick(() => searchInputRef.value?.focus());
  } else {
    resetSearchDraft();
  }
}

function openSearchPanel() {
  showSearchPanel.value = true;
}

function selectFilterType(type: AuditFilterType) {
  selectedFilterType.value = type;
  searchQuery.value = "";
  showSearchPanel.value = true;
  nextTick(() => searchInputRef.value?.focus());
}

function resetSearchDraft() {
  selectedFilterType.value = null;
  searchQuery.value = "";
  showSearchPanel.value = false;
}

function addOrReplaceFilter(
  type: AuditFilterType,
  value: string,
  displayValue = value
) {
  const filterMeta = filterTypes.value.find((item) => item.type === type);
  activeFilters.value = activeFilters.value.filter(
    (filter) => filter.type !== type
  );
  activeFilters.value.push({
    id: generateUniqueId(),
    type,
    label: filterMeta?.label || type,
    value,
    displayValue,
  });
}

function applyTextFilter() {
  if (!selectedFilterType.value || selectedFilterType.value === "level") {
    return;
  }
  const value = searchQuery.value.trim();
  if (!value) {
    return;
  }
  addOrReplaceFilter(selectedFilterType.value, value);
  previousPageTokens.value = [];
  resetSearchDraft();
  fetchLogs();
}

function applyLevelFilter(value: SeverityValue) {
  const label =
    severityOptions.value.find((item) => item.value === value)?.label || value;
  addOrReplaceFilter("level", value, label);
  previousPageTokens.value = [];
  resetSearchDraft();
  fetchLogs();
}

function parseInlineFilter() {
  const match = searchQuery.value.match(/^\s*([a-zA-Z_]+)\s*:\s*(.+)$/);
  if (!match) {
    return false;
  }
  const [, prefix, rawValue] = match;
  const value = rawValue.trim();
  if (!value) {
    return false;
  }

  const typeMap: Record<string, AuditFilterType> = {
    actor: "actor",
    user: "actor",
    resource: "resource",
    method: "method",
    level: "level",
    severity: "level",
  };
  const mapped = typeMap[prefix.toLowerCase()];
  if (!mapped) {
    return false;
  }

  if (mapped === "level") {
    const normalized = value.toUpperCase() as SeverityValue;
    if (!severityOptions.value.some((item) => item.value === normalized)) {
      return false;
    }
    applyLevelFilter(normalized);
    return true;
  }

  addOrReplaceFilter(mapped, value);
  previousPageTokens.value = [];
  resetSearchDraft();
  fetchLogs();
  return true;
}

function handleSearchEnter() {
  if (parseInlineFilter()) {
    return;
  }
  if (selectedFilterType.value) {
    if (selectedFilterType.value !== "level") {
      applyTextFilter();
    }
    return;
  }
  if (filteredFilterTypes.value.length === 1) {
    selectFilterType(filteredFilterTypes.value[0].type);
  }
}

function removeFilter(id: string) {
  activeFilters.value = activeFilters.value.filter(
    (filter) => filter.id !== id
  );
  previousPageTokens.value = [];
  fetchLogs();
}

function clearDateRange() {
  dateRange.value = emptyDateRange();
  draftDateRange.value = emptyDateRange();
  showDateRangePicker.value = false;
  previousPageTokens.value = [];
  fetchLogs();
}

function resetDraftDateRange() {
  draftDateRange.value = emptyDateRange();
}

function applyDraftDateRange() {
  const nextRange = cloneDateRange(draftDateRange.value);
  showDateRangePicker.value = false;
  if (isSameDateRange(dateRange.value, nextRange)) {
    return;
  }
  dateRange.value = nextRange;
  previousPageTokens.value = [];
  fetchLogs();
}

function clearAllFilters() {
  activeFilters.value = [];
  dateRange.value = emptyDateRange();
  draftDateRange.value = emptyDateRange();
  previousPageTokens.value = [];
  resetSearchDraft();
  fetchLogs();
}

function emptyDateRange(): CalendarRangeValue {
  return { start: undefined, end: undefined };
}

function createDefaultDateRange(): CalendarRangeValue {
  const end = today(getLocalTimeZone());
  const start = end.subtract({ months: 1 });
  return { start, end };
}

function cloneDateRange(value: CalendarRangeValue): CalendarRangeValue {
  return {
    start: value.start,
    end: value.end,
  };
}

function isSameDateRange(
  left: CalendarRangeValue,
  right: CalendarRangeValue
): boolean {
  return (
    getDateValueString(left.start) === getDateValueString(right.start) &&
    getDateValueString(left.end) === getDateValueString(right.end)
  );
}

function getDateValueString(value: CalendarDateLike): string {
  if (!value) {
    return "";
  }
  return value.toString();
}

function toRFC3339(value: CalendarDateLike, bound: "start" | "end"): string {
  const dateValue = getDateValueString(value);
  if (!dateValue) {
    return "";
  }
  const timeSuffix = bound === "start" ? "T00:00:00.000" : "T23:59:59.999";
  const date = new Date(`${dateValue}${timeSuffix}`);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return date.toISOString();
}

function formatDateValue(value: CalendarDateLike): string {
  const dateValue = getDateValueString(value);
  if (!dateValue) {
    return "";
  }
  const date = new Date(`${dateValue}T00:00:00.000`);
  if (Number.isNaN(date.getTime())) {
    return dateValue;
  }
  return new Intl.DateTimeFormat(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date);
}

function formatTimestamp(ts: Timestamp | undefined): string {
  if (!ts?.seconds) return "-";
  const milliseconds =
    Number(ts.seconds) * 1000 + Number(ts.nanos ?? 0) / 1_000_000;
  const date = new Date(milliseconds);
  return new Intl.DateTimeFormat(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(date);
}

function formatJson(value: unknown): string {
  if (
    !value ||
    (typeof value === "object" &&
      Object.keys(value as Record<string, unknown>).length === 0)
  ) {
    return "-";
  }
  return JSON.stringify(value, null, 2);
}

function getSeverityLabel(severity: AuditLogSeverity): string {
  switch (severity) {
    case AuditLogSeverity.INFO:
      return t("auditLogs.severityInfo");
    case AuditLogSeverity.WARNING:
      return t("auditLogs.severityWarning");
    case AuditLogSeverity.ERROR:
      return t("auditLogs.severityError");
    default:
      return t("auditLogs.severityUnknown");
  }
}

function getSeverityVariant(
  severity: AuditLogSeverity
): "default" | "secondary" | "destructive" | "outline" {
  switch (severity) {
    case AuditLogSeverity.INFO:
      return "secondary";
    case AuditLogSeverity.WARNING:
      return "outline";
    case AuditLogSeverity.ERROR:
      return "destructive";
    default:
      return "default";
  }
}

function getStatusLabel(log: AuditLog): string {
  const message = log.status?.message?.trim();
  const isSuccessMessage = !message || /^(ok|success)$/i.test(message);
  if (!log.status || (!log.status.code && !message)) {
    return t("auditLogs.statusUnknown");
  }
  if (!log.status.code) {
    return isSuccessMessage ? t("auditLogs.statusSuccess") : message;
  }
  return `${log.status.code} ${message || t("auditLogs.statusFailed")}`;
}

function formatAuditUserDisplay(user: User): string {
  if (user.title && user.email) {
    return `${user.title} (${user.email})`;
  }
  if (user.title) {
    return user.title;
  }
  if (user.email) {
    return user.email;
  }
  return user.name;
}

function getAuditIdentityDisplay(name: string): string {
  if (!name) {
    return "-";
  }
  return auditUserDisplayMap.value[name] || name;
}

function formatTimestampForExport(ts: Timestamp | undefined): string {
  if (!ts?.seconds) {
    return "";
  }
  const milliseconds =
    Number(ts.seconds) * 1000 + Number(ts.nanos ?? 0) / 1_000_000;
  const date = new Date(milliseconds);
  return Number.isNaN(date.getTime()) ? "" : date.toISOString();
}

function formatValueForCsv(value: unknown): string {
  if (value == null) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  return JSON.stringify(value);
}

function escapeCsvValue(value: unknown): string {
  const normalized = String(value ?? "").replace(/\r\n?/g, "\n");
  return `"${normalized.replace(/"/g, '""')}"`;
}

function buildAuditCsv(logEntries: AuditLog[]): string {
  const headers = [
    "createTime",
    "parent",
    "severity",
    "method",
    "resource",
    "resourceIdentifier",
    "user",
    "userIdentifier",
    "statusCode",
    "statusMessage",
    "latencyMs",
    "ip",
    "userAgent",
    "request",
    "response",
    "serviceData",
  ];
  const rows = logEntries.map((log) => [
    formatTimestampForExport(log.createTime),
    log.parent,
    getSeverityLabel(log.severity),
    log.method,
    getAuditIdentityDisplay(log.resource),
    log.resource,
    getAuditIdentityDisplay(log.user),
    log.user,
    log.status?.code ?? "",
    log.status?.message ?? "",
    log.latencyMs,
    log.requestMetadata?.ip ?? "",
    log.requestMetadata?.userAgent ?? "",
    formatValueForCsv(log.request),
    formatValueForCsv(log.response),
    formatValueForCsv(log.serviceData),
  ]);
  return [headers, ...rows]
    .map((row) => row.map((value) => escapeCsvValue(value)).join(","))
    .join("\n");
}

function getAuditCsvFilename(): string {
  const now = new Date();
  const timestamp = [
    now.getFullYear(),
    String(now.getMonth() + 1).padStart(2, "0"),
    String(now.getDate()).padStart(2, "0"),
    "-",
    String(now.getHours()).padStart(2, "0"),
    String(now.getMinutes()).padStart(2, "0"),
    String(now.getSeconds()).padStart(2, "0"),
  ].join("");
  return `audit-logs-${timestamp}.csv`;
}

function downloadCsv(content: string, fileName: string) {
  const blob = new Blob([content], { type: "text/csv;charset=utf-8;" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = fileName;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

async function hydrateAuditUserDisplay(logEntries: AuditLog[]) {
  const unresolvedNames = [
    ...new Set(
      logEntries
        .flatMap((log) => [log.user, log.resource])
        .filter(
          (name): name is string =>
            Boolean(name) &&
            name.startsWith("users/") &&
            !auditUserDisplayMap.value[name] &&
            !pendingAuditUsers.has(name)
        )
    ),
  ];
  if (unresolvedNames.length === 0) {
    return;
  }

  for (const name of unresolvedNames) {
    pendingAuditUsers.add(name);
  }

  try {
    const response = await batchGetUsers(unresolvedNames);
    const nextDisplayMap = { ...auditUserDisplayMap.value };
    for (const user of response.users) {
      nextDisplayMap[user.name] = formatAuditUserDisplay(user);
    }
    auditUserDisplayMap.value = nextDisplayMap;
  } catch {
    // Keep the original resource name as fallback when user lookup fails.
  } finally {
    for (const name of unresolvedNames) {
      pendingAuditUsers.delete(name);
    }
  }
}

async function fetchAuditLogsForExport(): Promise<AuditLog[]> {
  const exportedLogs: AuditLog[] = [];
  let pageToken = "";

  do {
    const response = await listAuditLogs({
      parent: WORKSPACE_PARENT,
      pageSize: 1000,
      pageToken,
      filter: filterExpression.value,
    });
    exportedLogs.push(...response.auditLogs);
    pageToken = response.nextPageToken;
  } while (pageToken);

  return exportedLogs;
}

async function exportCsv() {
  isExporting.value = true;
  try {
    const exportedLogs = await fetchAuditLogsForExport();
    await hydrateAuditUserDisplay(exportedLogs);
    downloadCsv(buildAuditCsv(exportedLogs), getAuditCsvFilename());
  } catch (err) {
    handleError(err, "auditLogs.exportError");
  } finally {
    isExporting.value = false;
  }
}

async function fetchLogs(pageToken = "") {
  isLoading.value = true;
  error.value = "";
  currentPageToken.value = pageToken;
  try {
    const response = await listAuditLogs({
      parent: WORKSPACE_PARENT,
      pageSize: 50,
      pageToken,
      filter: filterExpression.value,
    });
    logs.value = response.auditLogs;
    void hydrateAuditUserDisplay(response.auditLogs);
    nextPageToken.value = response.nextPageToken;
  } catch (err) {
    logs.value = [];
    nextPageToken.value = "";
    error.value = err instanceof Error ? err.message : String(err);
    handleError(err, "auditLogs.fetchError");
  } finally {
    isLoading.value = false;
  }
}

function refreshLogs() {
  previousPageTokens.value = [];
  fetchLogs();
}

function goToNextPage() {
  if (!nextPageToken.value) {
    return;
  }
  previousPageTokens.value.push(currentPageToken.value);
  fetchLogs(nextPageToken.value);
}

function goToPreviousPage() {
  if (previousPageTokens.value.length === 0) {
    return;
  }
  const token = previousPageTokens.value.pop() || "";
  fetchLogs(token);
}

function openDetails(log: AuditLog) {
  selectedLog.value = log;
  showDetails.value = true;
}

onMounted(() => {
  fetchLogs();
});
</script>