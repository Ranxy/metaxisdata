<template>
  <div class="space-y-4">
    <PageHeader :title="t('auditLogs.title')">
      <template #actions>
        <Button :disabled="isLoading || isExporting" variant="outline" @click="exportCsv">
          <Download class="mr-2 h-4 w-4" :class="{ 'animate-pulse': isExporting }" />
          {{ isExporting ? t("auditLogs.exportingCsv") : t("auditLogs.exportCsv") }}
        </Button>
        <Button :disabled="isLoading || isExporting" @click="refresh">
          <RefreshCcw class="mr-2 h-4 w-4" :class="{ 'animate-spin': isLoading }" />
          {{ t("auditLogs.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <div class="space-y-3">
      <!-- The filter box is one line by design: chips live on their own row
           below, so a long filter list cannot push the input onto a second
           line and grow the chrome above the table. -->
      <div class="flex flex-col gap-3 lg:flex-row lg:items-center">
        <div ref="searchBarRef" class="relative min-w-0 flex-1">
          <div
            class="flex h-11 w-full items-center gap-2 rounded-md border border-input bg-background px-3 text-sm transition-colors hover:border-ring focus-within:border-ring"
          >
            <button
              type="button"
              class="flex shrink-0 items-center gap-2 text-muted-foreground transition-colors hover:text-foreground"
              @click="toggleSearchPanel"
            >
              <Filter class="h-4 w-4" />
              <span class="font-medium">{{ t("auditLogs.filter") }}</span>
            </button>

            <span class="h-5 w-px shrink-0 bg-border" aria-hidden="true" />

            <input
              id="audit-log-search"
              ref="searchInputRef"
              v-model="searchQuery"
              name="audit-log-search"
              type="text"
              :placeholder="searchPlaceholder"
              class="min-w-0 flex-1 bg-transparent outline-hidden placeholder:text-muted-foreground"
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

        <div class="shrink-0">
          <AuditLogsDateRangePicker
            v-model="dateRange"
            @apply="refreshLogs"
          />
        </div>
      </div>

      <div v-if="activeFilters.length > 0" class="flex flex-wrap items-center gap-2">
        <span class="text-xs text-muted-foreground">
          {{ t("auditLogs.filterActive", { count: activeFilters.length }) }}
        </span>
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
            :aria-label="t('common.removeFilter')"
            @click.stop="removeFilter(filter.id)"
          >
            <X class="h-3 w-3" />
          </button>
        </Badge>
        <Button variant="ghost" size="sm" class="h-auto p-0 text-xs hover:underline" @click="clearAllFilters">
          {{ t("auditLogs.clearFilters") }}
        </Button>
      </div>
    </div>

    <Card>
      <CardHeader>
        <div class="flex items-start justify-between gap-4">
          <div class="min-w-0">
            <CardTitle>{{ t("auditLogs.entries") }}</CardTitle>
            <CardDescription>
              {{ t("auditLogs.entriesDescription", { count: logs.length }) }}
            </CardDescription>
          </div>
          <div class="hidden shrink-0 whitespace-nowrap pt-1 text-xs text-muted-foreground lg:block">
            {{ t("auditLogs.parentScope") }}: <span class="font-mono">workspaces/-</span>
          </div>
        </div>
      </CardHeader>
      <CardContent class="space-y-4">
        <PageState
          :loading="isLoading"
          :error="error"
        >
          <EmptyState
            v-if="logs.length === 0"
            :icon="ClipboardList"
            :title="t('auditLogs.emptyTitle')"
            :description="t('auditLogs.emptyDescription')"
          />

          <!-- `table-fixed`, a pixel width per column, and one column left
               auto: the Event column then takes every pixel the others do not
               need — at 1440 the full RPC name fits — while the fixed columns
               keep exactly the width their content needs at any viewport.
               Percentages cannot do both; auto layout cannot do it at all,
               because a `nowrap` cell contributes its whole text to the
               column's minimum and one long user agent stretched the table past
               1800px. `min-w-[42rem]` is the point where the fixed columns stop
               leaving Event room to say anything; below it the wrapper scrolls
               instead of collapsing the columns into each other. -->
          <Table
            v-else
            class="table-fixed min-w-[42rem]"
          >
            <TableHeader>
              <TableRow>
                <TableHead class="w-27 whitespace-nowrap">{{ t("auditLogs.time") }}</TableHead>
                <TableHead class="whitespace-nowrap">{{ t("auditLogs.event") }}</TableHead>
                <TableHead class="w-25 whitespace-nowrap">{{ t("auditLogs.resource") }}</TableHead>
                <TableHead class="w-26 whitespace-nowrap">{{ t("auditLogs.status") }}</TableHead>
                <TableHead class="w-25 whitespace-nowrap">{{ t("auditLogs.source") }}</TableHead>
                <TableHead class="w-26 whitespace-nowrap text-right">{{ t("auditLogs.actions") }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="log in logs" :key="log.name">
                <TableCell>
                  <div class="text-xs text-muted-foreground">{{ formatLogDate(log.createTime) }}</div>
                  <div class="text-sm tabular-nums">{{ formatLogTime(log.createTime) }}</div>
                </TableCell>
                <TableCell>
                  <!-- The indicator slot keeps every method on the same x even
                       though only the notable severities carry a label: an
                       audit trail is mostly INFO, and a badge on every line
                       buries the warnings it exists to surface. -->
                  <div class="flex min-w-0 items-center gap-2">
                    <Badge
                      v-if="isSeverityNoteworthy(log.severity)"
                      :variant="getSeverityVariant(log.severity)"
                      class="shrink-0"
                    >
                      {{ getSeverityLabel(log.severity) }}
                    </Badge>
                    <span
                      v-else
                      class="flex w-4 shrink-0 items-center justify-center"
                      :title="getSeverityLabel(log.severity)"
                    >
                      <span class="h-1.5 w-1.5 rounded-full bg-muted-foreground/50" aria-hidden="true" />
                      <span class="sr-only">{{ getSeverityLabel(log.severity) }}</span>
                    </span>
                    <span class="truncate font-mono text-xs" :title="log.method">{{ log.method }}</span>
                  </div>
                  <div
                    v-if="log.user"
                    class="mt-1 flex min-w-0 items-center gap-1 text-xs text-muted-foreground"
                  >
                    <UserRound class="h-3 w-3 shrink-0" />
                    <span class="truncate" :title="getAuditIdentityDisplay(log.user)">
                      {{ getAuditIdentityDisplay(log.user) }}
                    </span>
                  </div>
                </TableCell>
                <TableCell>
                  <div class="truncate text-xs" :title="getAuditIdentityDisplay(log.resource)">
                    {{ getAuditIdentityDisplay(log.resource) }}
                  </div>
                </TableCell>
                <TableCell>
                  <Badge
                    :variant="getStatusVariant(log)"
                    :title="getStatusLabel(log)"
                    class="max-w-full"
                  >
                    <span class="truncate">{{ getStatusShortLabel(log) }}</span>
                  </Badge>
                  <div class="mt-1 text-xs tabular-nums text-muted-foreground">
                    {{ t("auditLogs.latency", { value: String(log.latencyMs) }) }}
                  </div>
                </TableCell>
                <TableCell>
                  <div class="truncate font-mono text-xs" :title="log.requestMetadata?.ip">
                    {{ log.requestMetadata?.ip || "-" }}
                  </div>
                  <div
                    class="truncate text-xs text-muted-foreground"
                    :title="log.requestMetadata?.userAgent"
                  >
                    {{ log.requestMetadata?.userAgent || "-" }}
                  </div>
                </TableCell>
                <TableCell class="text-right">
                  <Button variant="ghost" size="sm" @click="openDetails(log)">
                    {{ t("common.details") }}
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
          @previous="goToPreviousPage"
          @next="goToNextPage"
        />
      </CardContent>
    </Card>

    <Dialog v-model:open="showDetails">
      <DialogContent class="max-w-5xl">
        <DialogHeader>
          <DialogTitle>{{ t("auditLogs.detailTitle") }}</DialogTitle>
          <DialogDescription>{{ t("auditLogs.detailDescription") }}</DialogDescription>
        </DialogHeader>

        <div v-if="selectedLog" class="space-y-5">
          <dl class="grid gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-4">
            <div class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">{{ t("auditLogs.time") }}</dt>
              <dd class="text-sm">{{ formatTimestamp(selectedLog.createTime) }}</dd>
            </div>
            <div class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">{{ t("auditLogs.severity") }}</dt>
              <dd>
                <Badge :variant="getSeverityVariant(selectedLog.severity)">
                  {{ getSeverityLabel(selectedLog.severity) }}
                </Badge>
              </dd>
            </div>
            <div class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">{{ t("auditLogs.status") }}</dt>
              <dd>
                <Badge :variant="getStatusVariant(selectedLog)">
                  {{ getStatusLabel(selectedLog) }}
                </Badge>
              </dd>
            </div>
            <div class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">{{ t("auditLogs.latencyLabel") }}</dt>
              <dd class="text-sm tabular-nums">
                {{ t("auditLogs.latency", { value: String(selectedLog.latencyMs) }) }}
              </dd>
            </div>
            <div class="space-y-1 sm:col-span-2 lg:col-span-4">
              <dt class="text-xs font-medium text-muted-foreground">{{ t("auditLogs.method") }}</dt>
              <dd class="font-mono text-xs break-all">{{ selectedLog.method }}</dd>
            </div>
            <div class="space-y-1 sm:col-span-2">
              <dt class="text-xs font-medium text-muted-foreground">{{ t("auditLogs.user") }}</dt>
              <dd class="text-sm break-all">{{ getAuditIdentityDisplay(selectedLog.user) }}</dd>
            </div>
            <div class="space-y-1 sm:col-span-2">
              <dt class="text-xs font-medium text-muted-foreground">{{ t("auditLogs.resource") }}</dt>
              <dd class="text-sm break-all">{{ getAuditIdentityDisplay(selectedLog.resource) }}</dd>
            </div>
            <div class="space-y-1 sm:col-span-2 lg:col-span-4">
              <dt class="text-xs font-medium text-muted-foreground">{{ t("auditLogs.requestMeta") }}</dt>
              <dd class="grid gap-x-6 gap-y-3 rounded-md border bg-muted/30 p-3 sm:grid-cols-2">
                <div class="space-y-0.5">
                  <div class="text-xs text-muted-foreground">{{ t("auditLogs.ipAddress") }}</div>
                  <div class="font-mono text-sm">{{ selectedLog.requestMetadata?.ip || "-" }}</div>
                </div>
                <div class="min-w-0 space-y-0.5">
                  <div class="text-xs text-muted-foreground">{{ t("auditLogs.userAgent") }}</div>
                  <div class="text-xs break-all">{{ selectedLog.requestMetadata?.userAgent || "-" }}</div>
                </div>
              </dd>
            </div>
          </dl>

          <!-- Full-width sections, not a three-column grid: JSON wrapped
               mid-token at a third of the dialog and could not be read. -->
          <div
            v-for="section in payloadSections"
            :key="section.title"
            class="space-y-2"
          >
            <h3 class="text-sm font-medium">{{ section.title }}</h3>
            <p v-if="isEmptyJson(section.value)" class="text-sm text-muted-foreground">-</p>
            <pre v-else class="max-h-80 overflow-auto rounded-md bg-muted p-4 text-xs leading-5">{{ formatJson(section.value) }}</pre>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="showDetails = false">
            {{ t("common.close") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { onClickOutside } from "@vueuse/core";
import {
  ClipboardList,
  Download,
  Filter,
  RefreshCcw,
  UserRound,
  X,
} from "lucide-vue-next";
import type { DateRange } from "radix-vue";
import { computed, nextTick, onMounted, ref, shallowRef, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listAuditLogs } from "@/api/audit";
import { listAll } from "@/api/list";
import { batchGetUsers } from "@/api/user";
import AuditLogsDateRangePicker from "@/components/audit/AuditLogsDateRangePicker.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
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
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import {
  type AuditLog,
  AuditLogSeverity,
} from "@/types/proto-es/v1/audit_log_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { auditLogsCsvFilename, buildAuditCsv } from "@/utils/auditLogsCsv";
import { downloadCsv } from "@/utils/csv";
import { dateRangeBound, defaultDateRange } from "@/utils/dateRange";
import { formatDate, formatDateTime, formatTime } from "@/utils/datetime";

type AuditFilterType = "resource" | "actor" | "method" | "level";
type SeverityValue = "INFO" | "WARNING" | "ERROR";
type BadgeVariant = "secondary" | "warning" | "destructive" | "outline";
type StatusVariant = "success" | "secondary" | "destructive";

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

const WORKSPACE_PARENT = "workspaces/-";

const { t, locale } = useI18n();
const { handleError } = useErrorHandler();

const isExporting = ref(false);
const error = ref("");
const showDetails = ref(false);
const selectedLog = ref<AuditLog | null>(null);
const auditUserDisplayMap = ref<Record<string, string>>({});

const activeFilters = ref<AuditFilter[]>([]);
const searchQuery = ref("");
const selectedFilterType = ref<AuditFilterType | null>(null);
const showSearchPanel = ref(false);
// shallowRef, not ref: Vue's deep unwrap rewrites the date classes into
// structural look-alikes, which radix's own DateRange (and its calendar) no
// longer accepts.
const dateRange = shallowRef<DateRange>(defaultDateRange());
const pendingAuditUsers = new Set<string>();

const searchBarRef = ref<HTMLElement | null>(null);
const searchInputRef = ref<HTMLInputElement | null>(null);
const pageSize = ref(50);

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
  const from = dateRangeBound(dateRange.value.start, "start");
  if (from) {
    clauses.push(`create_time >= ${JSON.stringify(from)}`);
  }
  const to = dateRangeBound(dateRange.value.end, "end");
  if (to) {
    clauses.push(`create_time <= ${JSON.stringify(to)}`);
  }
  return clauses.join(" && ");
});

const payloadSections = computed(() => [
  { title: t("auditLogs.request"), value: selectedLog.value?.request },
  { title: t("auditLogs.response"), value: selectedLog.value?.response },
  { title: t("auditLogs.serviceData"), value: selectedLog.value?.serviceData },
]);

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
  resetSearchDraft();
  void refreshLogs();
}

function applyLevelFilter(value: SeverityValue) {
  const label =
    severityOptions.value.find((item) => item.value === value)?.label || value;
  addOrReplaceFilter("level", value, label);
  resetSearchDraft();
  void refreshLogs();
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
  resetSearchDraft();
  void refreshLogs();
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
  void refreshLogs();
}

function clearAllFilters() {
  activeFilters.value = [];
  resetSearchDraft();
  void refreshLogs();
}

function formatTimestamp(ts: Timestamp | undefined): string {
  return formatDateTime(ts, locale.value, { seconds: true });
}

/** The list splits the timestamp so the column fits a narrow window: the date
 *  above, the time of day (which is what an audit trail is scanned by) below. */
function formatLogDate(ts: Timestamp | undefined): string {
  return formatDate(ts, locale.value);
}

function formatLogTime(ts: Timestamp | undefined): string {
  return formatTime(ts, locale.value, { seconds: true });
}

function isEmptyJson(value: unknown): boolean {
  return (
    !value ||
    (typeof value === "object" &&
      Object.keys(value as Record<string, unknown>).length === 0)
  );
}

function formatJson(value: unknown): string {
  return isEmptyJson(value) ? "-" : JSON.stringify(value, null, 2);
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

function getSeverityVariant(severity: AuditLogSeverity): BadgeVariant {
  switch (severity) {
    case AuditLogSeverity.INFO:
      return "secondary";
    case AuditLogSeverity.WARNING:
      return "warning";
    case AuditLogSeverity.ERROR:
      return "destructive";
    default:
      return "outline";
  }
}

/** A call that carried no error code and no message beyond "ok" succeeded. */
function isSuccessStatus(log: AuditLog): boolean {
  if (!log.status || log.status.code) {
    return false;
  }
  const message = log.status.message?.trim();
  return !message || /^(ok|success)$/i.test(message);
}

/** The full outcome, including the server's explanation. */
function getStatusLabel(log: AuditLog): string {
  if (!log.status || (!log.status.code && !log.status.message?.trim())) {
    return t("auditLogs.statusUnknown");
  }
  if (isSuccessStatus(log)) {
    return t("auditLogs.statusSuccess");
  }
  if (!log.status.code) {
    return log.status.message?.trim() ?? "";
  }
  return `${log.status.code} ${log.status.message?.trim() || t("auditLogs.statusFailed")}`;
}

/** What the list column can actually show: an error message such as "16 the
 *  caller does not have permission" leaves room for nothing else, so the row
 *  carries the code and the tooltip and the dialog carry the wording. */
function getStatusShortLabel(log: AuditLog): string {
  if (!log.status || (!log.status.code && !log.status.message?.trim())) {
    return t("auditLogs.statusUnknown");
  }
  if (isSuccessStatus(log)) {
    return t("auditLogs.statusSuccess");
  }
  if (!log.status.code) {
    return log.status.message?.trim() ?? "";
  }
  return `${log.status.code} ${t("auditLogs.statusFailed")}`;
}

function getStatusVariant(log: AuditLog): StatusVariant {
  if (!log.status || (!log.status.code && !log.status.message?.trim())) {
    return "secondary";
  }
  return isSuccessStatus(log) ? "success" : "destructive";
}

/** INFO is the default outcome of an audited call; the rest deserve a label. */
function isSeverityNoteworthy(severity: AuditLogSeverity): boolean {
  return severity !== AuditLogSeverity.INFO;
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
  // The same walk the pager uses, with a bigger page: it also stops on a token
  // that does not advance, which the old do/while export did not.
  return await listAll(async (pageToken) => {
    const response = await listAuditLogs({
      parent: WORKSPACE_PARENT,
      pageSize: 1000,
      pageToken,
      filter: filterExpression.value,
    });
    return { items: response.auditLogs, nextPageToken: response.nextPageToken };
  });
}

async function exportCsv() {
  isExporting.value = true;
  try {
    const exportedLogs = await fetchAuditLogsForExport();
    await hydrateAuditUserDisplay(exportedLogs);
    downloadCsv(
      buildAuditCsv(exportedLogs, {
        severityLabel: getSeverityLabel,
        identity: getAuditIdentityDisplay,
      }),
      auditLogsCsvFilename()
    );
  } catch (err) {
    handleError(err, "auditLogs.exportError");
  } finally {
    isExporting.value = false;
  }
}

const {
  items: logs,
  isLoading,
  hasNext,
  hasPrevious,
  reset: refreshLogs,
  refresh,
  goNext: goToNextPage,
  goPrevious: goToPreviousPage,
} = usePagedFetch<AuditLog>({
  fetchPage: async (pageToken, signal) => {
    error.value = "";
    const response = await listAuditLogs({
      parent: WORKSPACE_PARENT,
      pageSize: pageSize.value,
      pageToken,
      filter: filterExpression.value,
      signal,
    });
    return { items: response.auditLogs, nextPageToken: response.nextPageToken };
  },
  onError: (err) => {
    error.value = handleError(err, "auditLogs.fetchError");
  },
});

// A bigger page restarts the walk: a cursor belongs to the query that produced it.
watch(pageSize, () => void refreshLogs());

// The actor display map is filled per page, not per row render.
watch(logs, (page) => void hydrateAuditUserDisplay(page));

function openDetails(log: AuditLog) {
  selectedLog.value = log;
  showDetails.value = true;
}

onMounted(() => {
  void refreshLogs();
});
</script>
