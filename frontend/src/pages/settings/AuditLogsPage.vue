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

    <div class="flex items-start gap-3">
      <AdvancedSearchBar
        class="min-w-0 flex-1"
        :filter-categories="filterCategories"
        :search-placeholder="t('auditLogs.searchPlaceholder')"
        @update:filters="handleFiltersUpdate"
      />

      <div class="shrink-0">
        <AuditLogsDateRangePicker
          v-model="dateRange"
          @apply="refreshLogs"
        />
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
            {{ t("auditLogs.parentScope") }}: {{ t("auditLogs.scopeWorkspace") }}
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
                    <!-- The ledger stores the RPC path; an administrator reads
                         an event name. The raw procedure stays in the tooltip
                         and in the detail dialog. -->
                    <span class="truncate text-sm" :title="log.method">
                      {{ auditEventName(log.method) }}
                    </span>
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
            <div class="space-y-1 sm:col-span-2">
              <dt class="text-xs font-medium text-muted-foreground">{{ t("auditLogs.event") }}</dt>
              <dd class="text-sm">{{ auditEventName(selectedLog.method) }}</dd>
            </div>
            <div class="space-y-1 sm:col-span-2">
              <dt class="text-xs font-medium text-muted-foreground">{{ t("auditLogs.rawMethod") }}</dt>
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
import {
  ClipboardList,
  Download,
  RefreshCcw,
  UserRound,
} from "lucide-vue-next";
import type { DateRange } from "radix-vue";
import { computed, onMounted, ref, shallowRef, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listAuditLogs } from "@/api/audit";
import { listAll } from "@/api/list";
import { batchGetUsers, listAllUsers } from "@/api/user";
import AuditLogsDateRangePicker from "@/components/audit/AuditLogsDateRangePicker.vue";
import AdvancedSearchBar, {
  type ActiveFilter,
  type FilterCategory,
  type FilterOption,
} from "@/components/common/AdvancedSearchBar.vue";
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
import { useDebounced } from "@/composables/useTimers";
import {
  type AuditLog,
  AuditLogSeverity,
} from "@/types/proto-es/v1/audit_log_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { auditLogsCsvFilename, buildAuditCsv } from "@/utils/auditLogsCsv";
import { downloadCsv } from "@/utils/csv";
import { dateRangeBound, defaultDateRange } from "@/utils/dateRange";
import { formatDate, formatDateTime, formatTime } from "@/utils/datetime";

type SeverityValue = "INFO" | "WARNING" | "ERROR";
type BadgeVariant = "secondary" | "warning" | "destructive" | "outline";
type StatusVariant = "success" | "secondary" | "destructive";

const WORKSPACE_PARENT = "workspaces/-";

/**
 * The audited procedures this build knows, by `Service/Method`, mapped to the
 * `auditLogs.eventNames.*` key that names the event in the reader's language.
 * Audit rows store the raw ConnectRPC path, so anything missing here falls back
 * to the shortened procedure (`AuthService.Login`) rather than disappearing.
 */
const AUDIT_EVENT_KEYS: Record<string, string> = {
  "ApplicationService/CreateApplication": "createApplication",
  "ApplicationService/CreateApplicationAPIKey": "createApplicationApiKey",
  "ApplicationService/DeleteApplication": "deleteApplication",
  "ApplicationService/DeleteApplicationSQL": "deleteApplicationSql",
  "ApplicationService/RevokeApplicationAPIKey": "revokeApplicationApiKey",
  "ApplicationService/UpdateApplication": "updateApplication",
  "AuditLogService/ListAuditLogs": "listAuditLogs",
  "AuthService/ApproveDeviceLogin": "approveDeviceLogin",
  "AuthService/CreateDeviceLogin": "createDeviceLogin",
  "AuthService/Login": "login",
  "AuthService/Logout": "logout",
  "DatabaseService/CreateManualSQL": "createManualSql",
  "DatabaseService/DeleteManualSQL": "deleteManualSql",
  "DatabaseService/SyncDatabase": "syncSchema",
  "DatabaseService/UpdateManualSQL": "updateManualSql",
  "EnvironmentService/CreateEnvironment": "createEnvironment",
  "EnvironmentService/DeleteEnvironment": "deleteEnvironment",
  "EnvironmentService/UpdateEnvironment": "updateEnvironment",
  "GroupService/CreateGroup": "createGroup",
  "GroupService/DeleteGroup": "deleteGroup",
  "GroupService/UpdateGroup": "updateGroup",
  "IamService/SetWorkspaceIamPolicy": "setIamPolicy",
  "IdentityProviderService/CreateIdentityProvider": "createIdentityProvider",
  "IdentityProviderService/DeleteIdentityProvider": "deleteIdentityProvider",
  "IdentityProviderService/DeleteIdentityProviderBinding":
    "deleteIdentityProviderBinding",
  "IdentityProviderService/UpdateIdentityProvider": "updateIdentityProvider",
  "InstanceService/BatchSyncInstances": "syncSchema",
  "InstanceService/BatchUpdateInstances": "batchUpdateInstances",
  "InstanceService/CreateDataSource": "createDataSource",
  "InstanceService/CreateInstance": "createInstance",
  "InstanceService/DeleteDataSource": "deleteDataSource",
  "InstanceService/DeleteInstance": "deleteInstance",
  "InstanceService/SyncInstance": "syncSchema",
  "InstanceService/UndeleteInstance": "undeleteInstance",
  "InstanceService/UpdateDataSource": "updateDataSource",
  "InstanceService/UpdateInstance": "updateInstance",
  "OAuthService/ApproveOAuthAuthorizationRequest": "approveOAuthAuthorization",
  "OpenLineageService/CreateAPIKey": "createOpenLineageApiKey",
  "OpenLineageService/CreateNamespaceMapping": "createNamespaceMapping",
  "OpenLineageService/DeleteNamespaceMapping": "deleteNamespaceMapping",
  "OpenLineageService/RevokeAPIKey": "revokeOpenLineageApiKey",
  "OpenLineageService/UpdateNamespaceMapping": "updateNamespaceMapping",
  "RoleService/CreateRole": "createRole",
  "RoleService/DeleteRole": "deleteRole",
  "RoleService/UpdateRole": "updateRole",
  "SettingService/UpdateDebugConfig": "updateDebugConfig",
  "SettingService/UpdateWorkspaceProfileSetting": "updateSettings",
  "UserService/CreateUser": "createUser",
  "UserService/DeleteUser": "deleteUser",
  "UserService/UndeleteUser": "undeleteUser",
  "UserService/UpdateUser": "updateUser",
};

/**
 * Ledger rows whose method is an HTTP route or an MCP tool call rather than a
 * ConnectRPC procedure: the OAuth endpoints, OpenLineage ingestion, and the MCP
 * surface call the audit logger directly, so they never reach the RPC table.
 */
const AUDIT_RAW_PATHS: Record<string, string> = {
  "/api/v1/lineage": "ingestOpenLineageEvent",
  "/oauth/authorize": "authorizeOAuthRequest",
  "/oauth/authorize/complete": "completeOAuthAuthorization",
  "/oauth/register": "registerOAuthClient",
  "/oauth/token": "issueOAuthToken",
};

const MCP_TOOL_CALL_PREFIX = "mcp/tools/call:";

const { t, locale, te } = useI18n();
const { handleError } = useErrorHandler();

const isExporting = ref(false);
const error = ref("");
const showDetails = ref(false);
const selectedLog = ref<AuditLog | null>(null);
const auditUserDisplayMap = ref<Record<string, string>>({});

// Filter state, owned by AdvancedSearchBar and read back from its pills. The
// search box is the method filter: it matches the event name as a substring, so
// method is not a category of its own.
const searchText = ref("");
const resourceFilter = ref("");
const actorFilter = ref("");
const severityFilter = ref<SeverityValue | "">("");
// shallowRef, not ref: Vue's deep unwrap rewrites the date classes into
// structural look-alikes, which radix's own DateRange (and its calendar) no
// longer accepts.
const dateRange = shallowRef<DateRange>(defaultDateRange());
const pendingAuditUsers = new Set<string>();

const pageSize = ref(50);

const severityOptions = computed<FilterOption[]>(() => [
  { value: "INFO", label: t("auditLogs.severityInfo") },
  { value: "WARNING", label: t("auditLogs.severityWarning") },
  { value: "ERROR", label: t("auditLogs.severityError") },
]);

// The workspace's users as actor options. The stored `user` is a resource name
// such as `users/101`, so a picker is the only way to name an actor that
// actually matches; a typed email or display name matches nothing.
let actorOptionsPromise: Promise<FilterOption[]> | null = null;
function loadActorOptions(): Promise<FilterOption[]> {
  actorOptionsPromise ??= listAllUsers()
    .then((users) =>
      users
        .filter((user) => !!user.name)
        .map((user) => ({
          value: user.name,
          label: formatAuditUserDisplay(user),
        }))
    )
    .catch((err: unknown) => {
      actorOptionsPromise = null;
      handleError(err, "auditLogs.fetchActorsError");
      return [];
    });
  return actorOptionsPromise;
}

/** The filter dimensions this page offers. The bar owns their UI; the page only
 *  maps the emitted pills back onto the CEL filter the API understands. */
const filterCategories = computed<FilterCategory[]>(() => [
  {
    type: "resource",
    label: t("auditLogs.filterResource"),
    icon: "📦",
    kind: "input",
    placeholder: t("auditLogs.resourcePlaceholder"),
  },
  {
    type: "actor",
    label: t("auditLogs.filterActor"),
    icon: "👤",
    options: loadActorOptions,
  },
  {
    type: "level",
    label: t("auditLogs.filterLevel"),
    icon: "🚦",
    options: severityOptions.value,
  },
]);

function handleFiltersUpdate(filters: ActiveFilter[]) {
  const valueOf = (type: string) =>
    filters.find((filter) => filter.type === type)?.value ?? "";
  // The search text is emitted as a `name` filter; it drives the method search.
  searchText.value = valueOf("name");
  resourceFilter.value = valueOf("resource");
  actorFilter.value = valueOf("actor");
  severityFilter.value = valueOf("level") as SeverityValue | "";
}

/**
 * The search box prints event names, so it has to search them too. The ledger
 * filter only matches the stored method, so a localized name is translated back
 * to every procedure it can name; a raw method fragment still matches as
 * before, which keeps `AuthService` / `Login` queries working.
 */
function methodSearchClause(term: string): string {
  const needle = term.toLowerCase();
  const localizedProcedures = [
    ...Object.entries(AUDIT_EVENT_KEYS),
    ...Object.entries(AUDIT_RAW_PATHS),
  ]
    .filter(([, key]) => {
      const i18nKey = `auditLogs.eventNames.${key}`;
      return te(i18nKey) && t(i18nKey).toLowerCase().includes(needle);
    })
    .map(([procedure]) => procedure);
  const clauses = localizedProcedures.map(
    (procedure) => `method.matches(${JSON.stringify(procedure)})`
  );
  // A raw fragment still matches: `Login` finds AuthService/Login even though
  // its localized name reads "Sign in".
  clauses.push(`method.matches(${JSON.stringify(term)})`);
  return clauses.length === 1 ? clauses[0] : `(${clauses.join(" || ")})`;
}

const filterExpression = computed(() => {
  const clauses: string[] = [];
  const resource = resourceFilter.value.trim();
  if (resource) {
    clauses.push(`resource.matches(${JSON.stringify(resource)})`);
  }
  if (actorFilter.value) {
    clauses.push(`user == ${JSON.stringify(actorFilter.value)}`);
  }
  const search = searchText.value.trim();
  if (search) {
    clauses.push(methodSearchClause(search));
  }
  if (severityFilter.value) {
    clauses.push(`severity == ${JSON.stringify(severityFilter.value)}`);
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

function formatTimestamp(ts: Timestamp | undefined): string {
  return formatDateTime(ts, locale.value, { seconds: true });
}

/** `/metaxisdata.v1.AuthService/Login` → `{ service: "AuthService", method:
 *  "Login" }`. The package prefix is noise for a reader and the last segment is
 *  the operation. */
function splitProcedure(procedure: string): {
  service: string;
  method: string;
} {
  const segments = procedure.replace(/^\//, "").split("/");
  const method = segments.pop() ?? "";
  const servicePath = segments.pop() ?? "";
  const service = servicePath.split(".").pop() ?? servicePath;
  return { service, method };
}

/** What the Event column prints: a localized event name when this build knows
 *  the procedure, otherwise `Service.Method` — never the raw `/metaxisdata.v1.`
 *  path, which stays in the tooltip and the detail dialog. */
function auditEventName(procedure: string): string {
  if (!procedure) {
    return t("auditLogs.unknownOperation");
  }
  if (procedure.startsWith(MCP_TOOL_CALL_PREFIX)) {
    return t("auditLogs.eventNames.callMcpTool", {
      tool: procedure.slice(MCP_TOOL_CALL_PREFIX.length),
    });
  }
  const { service, method } = splitProcedure(procedure);
  const key =
    AUDIT_RAW_PATHS[procedure] ?? AUDIT_EVENT_KEYS[`${service}/${method}`];
  if (key) {
    const i18nKey = `auditLogs.eventNames.${key}`;
    if (te(i18nKey)) {
      return t(i18nKey);
    }
  }
  return service ? `${service}.${method}` : method || procedure;
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

// The bar emits on every keystroke, so a filter change restarts the walk on a
// short debounce rather than firing one request per character.
const refreshOnFilter = useDebounced(() => void refreshLogs(), 250);
watch(
  [searchText, resourceFilter, actorFilter, severityFilter],
  () => void refreshOnFilter()
);

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
