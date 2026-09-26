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

    <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_auto] xl:items-start">
      <div class="space-y-3">
        <div ref="searchBarRef" class="relative">
          <div
            class="flex min-h-11 w-full flex-wrap items-center gap-2 rounded-md border border-input bg-background px-3 py-2 text-sm transition-colors hover:border-ring"
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
                :aria-label="t('common.removeFilter')"
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
                :aria-label="t('auditLogs.clearDateRange')"
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
        <AuditLogsDateRangePicker
          v-model="dateRange"
          @apply="refreshLogs"
        />
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
        </PageState>

        <div class="flex items-center justify-between border-t pt-4">
          <div class="text-sm text-muted-foreground">
            {{ t("auditLogs.pageStatus", { count: logs.length }) }}
          </div>
          <div class="flex items-center gap-2">
            <Button variant="outline" :disabled="!previousPageTokens || isLoading" @click="goToPreviousPage">
              {{ t("common.previous") }}
            </Button>
            <Button variant="outline" :disabled="!nextPageToken || isLoading" @click="goToNextPage">
              {{ t("common.next") }}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>

    <Dialog v-model:open="showDetails">
      <DialogContent class="max-w-4xl">
        <DialogHeader>
          <DialogTitle>{{ t("auditLogs.detailTitle") }}</DialogTitle>
        </DialogHeader>

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

        <DialogFooter>
          <Button variant="outline" @click="showDetails = false">
            {{ t("common.cancel") }}
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
import PageHeader from "@/components/layout/PageHeader.vue";
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
  Dialog,
  DialogContent,
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
import {
  dateRangeBound,
  defaultDateRange,
  emptyDateRange,
  formatDateRangeChip,
} from "@/utils/dateRange";
import { formatDateTime } from "@/utils/datetime";

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

const dateRangeChip = computed(() =>
  formatDateRangeChip(dateRange.value, locale.value)
);

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

function clearDateRange() {
  dateRange.value = emptyDateRange();
  void refreshLogs();
}

function clearAllFilters() {
  activeFilters.value = [];
  dateRange.value = emptyDateRange();
  resetSearchDraft();
  void refreshLogs();
}

function formatTimestamp(ts: Timestamp | undefined): string {
  return formatDateTime(ts, locale.value, { seconds: true });
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
  hasNext: nextPageToken,
  hasPrevious: previousPageTokens,
  reset: refreshLogs,
  refresh,
  goNext: goToNextPage,
  goPrevious: goToPreviousPage,
} = usePagedFetch<AuditLog>({
  fetchPage: async (pageToken, signal) => {
    error.value = "";
    const response = await listAuditLogs({
      parent: WORKSPACE_PARENT,
      pageSize: 50,
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