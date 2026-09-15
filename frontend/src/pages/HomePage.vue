<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('home.title')"
      :description="greeting"
    >
      <template #actions>
        <span
          v-if="lastUpdated"
          class="hidden text-xs text-muted-foreground sm:inline"
        >
          {{ t("home.refreshedAt", { time: formatClock(lastUpdated) }) }}
        </span>
        <Button
          variant="outline"
          size="sm"
          :disabled="isLoading"
          @click="load"
        >
          <RefreshCw :class="['mr-1 h-4 w-4', isLoading ? 'animate-spin' : '']" />
          {{ t("home.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <!-- A section that failed to load must not blank the rest of the page. -->
    <Alert
      v-if="failedSectionLabels.length > 0"
      variant="destructive"
    >
      <AlertTriangle class="h-4 w-4" />
      <AlertTitle>{{ t("home.loadErrorTitle") }}</AlertTitle>
      <AlertDescription>
        <p>{{ t("home.loadErrorDescription") }}</p>
        <div class="mt-2 flex flex-wrap gap-2">
          <Badge
            v-for="label in failedSectionLabels"
            :key="label"
            variant="outline"
          >
            {{ label }}
          </Badge>
        </div>
      </AlertDescription>
    </Alert>

    <div
      v-if="initialLoading"
      class="flex justify-center p-16"
    >
      <AppLoading :text="t('home.loading')" />
    </div>

    <template v-else>
      <!-- First-run guidance: only while the core metadata is still missing. -->
      <Card
        v-if="showSetup"
        class="border-primary/30 bg-primary/5"
      >
        <CardHeader>
          <CardTitle class="flex items-center gap-2">
            <Rocket class="h-5 w-5 text-primary" />
            {{ t("home.getStartedTitle") }}
          </CardTitle>
          <CardDescription>{{ t("home.getStartedDescription") }}</CardDescription>
        </CardHeader>
        <CardContent class="grid gap-4 md:grid-cols-2">
          <div
            v-for="step in setupSteps"
            :key="step.key"
            class="flex items-start gap-3 rounded-lg border bg-background p-4"
          >
            <CheckCircle2
              v-if="step.done"
              class="mt-0.5 h-5 w-5 flex-shrink-0 text-green-600"
            />
            <Circle
              v-else
              class="mt-0.5 h-5 w-5 flex-shrink-0 text-muted-foreground"
            />
            <div class="min-w-0 flex-1">
              <p class="font-medium">
                {{ step.title }}
              </p>
              <p class="mt-0.5 text-sm text-muted-foreground">
                {{ step.description }}
              </p>
            </div>
            <Button
              v-if="!step.done"
              variant="outline"
              size="sm"
              asChild
            >
              <RouterLink :to="step.to">
                {{ step.cta }}
              </RouterLink>
            </Button>
          </div>
        </CardContent>
      </Card>

      <!-- Actionable problems in the estate. -->
      <Card
        v-if="attentionItems.length > 0"
        class="border-yellow-200"
      >
        <CardHeader>
          <CardTitle class="flex items-center gap-2 text-base">
            <AlertTriangle class="h-5 w-5 text-yellow-600" />
            {{ t("home.attentionTitle") }}
          </CardTitle>
          <CardDescription>{{ t("home.attentionDescription") }}</CardDescription>
        </CardHeader>
        <CardContent class="space-y-2">
          <div
            v-for="item in attentionItems"
            :key="item.key"
            class="flex flex-wrap items-center justify-between gap-3 rounded-md border bg-background px-3 py-2"
          >
            <span class="text-sm">{{ item.text }}</span>
            <Button
              variant="ghost"
              size="sm"
              asChild
            >
              <RouterLink :to="item.to">
                {{ t("home.review") }}
                <ArrowRight class="ml-1 h-4 w-4" />
              </RouterLink>
            </Button>
          </div>
        </CardContent>
      </Card>

      <!-- Estate numbers. Each card jumps to the list it summarizes. -->
      <div
        v-if="stats.length > 0"
        class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4"
      >
        <RouterLink
          v-for="stat in stats"
          :key="stat.key"
          :to="stat.to"
          class="group"
        >
          <Card class="h-full transition-shadow group-hover:shadow-md">
            <CardContent class="p-5">
              <div class="flex items-center justify-between">
                <span class="text-sm font-medium text-muted-foreground">
                  {{ stat.label }}
                </span>
                <component
                  :is="stat.icon"
                  class="h-4 w-4 text-muted-foreground"
                />
              </div>
              <p class="mt-2 text-3xl font-bold">
                {{ stat.value }}
              </p>
              <p class="mt-1 text-xs text-muted-foreground">
                {{ stat.hint }}
              </p>
            </CardContent>
          </Card>
        </RouterLink>
      </div>

      <div class="grid gap-6 xl:grid-cols-3">
        <!-- Data sources at a glance. -->
        <Card class="xl:col-span-2">
          <CardHeader class="flex flex-row items-center justify-between space-y-0">
            <div class="space-y-1.5">
              <CardTitle>{{ t("home.dataSourcesTitle") }}</CardTitle>
              <CardDescription>
                {{ t("home.dataSourcesDescription") }}
              </CardDescription>
            </div>
            <Button
              variant="ghost"
              size="sm"
              asChild
            >
              <RouterLink :to="{ name: 'InstanceManagement' }">
                {{ t("home.viewAll") }}
                <ArrowRight class="ml-1 h-4 w-4" />
              </RouterLink>
            </Button>
          </CardHeader>
          <CardContent>
            <div
              v-if="instances.length === 0"
              class="p-8 text-center text-muted-foreground"
            >
              <Database class="mx-auto mb-4 h-10 w-10 text-muted-foreground/50" />
              <p class="text-sm">
                {{ t("instanceManagement.noInstances") }}
              </p>
            </div>
            <template v-else>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{{ t("home.columnDataSource") }}</TableHead>
                    <TableHead>{{ t("instanceManagement.engine") }}</TableHead>
                    <TableHead>
                      {{ t("instanceManagement.environment") }}
                    </TableHead>
                    <TableHead class="text-right">
                      {{ t("home.columnDatabases") }}
                    </TableHead>
                    <TableHead>{{ t("instanceManagement.lastSync") }}</TableHead>
                    <TableHead>{{ t("instanceManagement.status") }}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow
                    v-for="instance in visibleInstances"
                    :key="instance.name"
                    class="cursor-pointer"
                    @click="openInstance(instance)"
                  >
                    <TableCell>
                      <div class="font-medium">
                        {{ instance.title || instanceId(instance.name) }}
                      </div>
                      <div
                        v-if="instance.title"
                        class="text-xs text-muted-foreground"
                      >
                        {{ instanceId(instance.name) }}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant="secondary">
                        {{ engineLabel(instance.engine) }}
                      </Badge>
                    </TableCell>
                    <TableCell class="text-muted-foreground">
                      {{ environmentLabel(instance.environment) }}
                    </TableCell>
                    <TableCell class="text-right">
                      {{ databasesPerInstance[instanceId(instance.name)] ?? 0 }}
                    </TableCell>
                    <TableCell class="whitespace-nowrap text-muted-foreground">
                      {{ formatTimestamp(instance.lastSyncTime) }}
                    </TableCell>
                    <TableCell>
                      <Badge :variant="instance.activation ? 'success' : 'secondary'">
                        {{
                          instance.activation
                            ? t("instanceManagement.active")
                            : t("instanceManagement.inactive")
                        }}
                      </Badge>
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
              <p
                v-if="instances.length > visibleInstances.length"
                class="mt-3 text-xs text-muted-foreground"
              >
                {{
                  t("home.moreInstances", {
                    count: instances.length - visibleInstances.length,
                  })
                }}
              </p>
            </template>
          </CardContent>
        </Card>

        <!-- Permission-aware shortcuts into the rest of the product. -->
        <Card>
          <CardHeader>
            <CardTitle>{{ t("home.quickActionsTitle") }}</CardTitle>
            <CardDescription>
              {{ t("home.quickActionsDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-1">
            <RouterLink
              v-for="action in quickActions"
              :key="action.key"
              :to="action.to"
              class="flex items-center gap-3 rounded-md px-3 py-2 transition-colors hover:bg-accent"
            >
              <component
                :is="action.icon"
                class="h-4 w-4 flex-shrink-0 text-muted-foreground"
              />
              <div class="min-w-0 flex-1">
                <p class="truncate text-sm font-medium">
                  {{ action.label }}
                </p>
                <p class="truncate text-xs text-muted-foreground">
                  {{ action.description }}
                </p>
              </div>
              <ArrowRight class="h-4 w-4 flex-shrink-0 text-muted-foreground" />
            </RouterLink>
          </CardContent>
        </Card>
      </div>

      <!-- Raw OpenLineage evidence, newest first. -->
      <Card v-if="canViewOpenLineage">
        <CardHeader class="flex flex-row items-center justify-between space-y-0">
          <div class="space-y-1.5">
            <CardTitle>{{ t("home.recentEventsTitle") }}</CardTitle>
            <CardDescription>
              {{ t("home.recentEventsDescription") }}
            </CardDescription>
          </div>
          <Button
            variant="ghost"
            size="sm"
            asChild
          >
            <RouterLink :to="{ name: 'OpenLineageEvents' }">
              {{ t("home.viewAll") }}
              <ArrowRight class="ml-1 h-4 w-4" />
            </RouterLink>
          </Button>
        </CardHeader>
        <CardContent>
          <div
            v-if="recentRuns.length === 0"
            class="p-8 text-center text-muted-foreground"
          >
            <Activity class="mx-auto mb-4 h-10 w-10 text-muted-foreground/50" />
            <p class="text-sm">
              {{ t("openlineageSettings.noRuns") }}
            </p>
            <Button
              variant="outline"
              size="sm"
              class="mt-4"
              asChild
            >
              <RouterLink :to="{ name: 'OpenLineageSettings' }">
                {{ t("openlineage.ingestionSettings") }}
              </RouterLink>
            </Button>
          </div>
          <Table v-else>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("openlineageSettings.eventTime") }}</TableHead>
                <TableHead>{{ t("openlineage.eventType") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.jobName") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.namespace") }}</TableHead>
                <TableHead>{{ t("openlineageSettings.integration") }}</TableHead>
                <TableHead>{{ t("openlineage.hasLineage") }}</TableHead>
                <TableHead class="text-right">
                  {{ t("instanceManagement.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="run in recentRuns"
                :key="run.guid"
              >
                <TableCell
                  class="whitespace-nowrap text-muted-foreground"
                  :title="formatTimestamp(run.eventTime)"
                >
                  {{ formatRelativeTime(run.eventTime) }}
                </TableCell>
                <TableCell>
                  <Badge :variant="eventVariant(run.eventType)">
                    {{ run.eventType || "-" }}
                  </Badge>
                </TableCell>
                <TableCell>{{ run.jobName || "-" }}</TableCell>
                <TableCell class="font-mono text-xs">
                  {{ run.jobNamespace || "-" }}
                </TableCell>
                <TableCell>{{ run.integration || "-" }}</TableCell>
                <TableCell>
                  <Badge :variant="run.hasLineage ? 'success' : 'secondary'">
                    {{
                      run.hasLineage ? t("openlineage.yes") : t("openlineage.no")
                    }}
                  </Badge>
                </TableCell>
                <TableCell class="text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    @click="openRun(run.guid)"
                  >
                    {{ t("openlineageSettings.viewRun") }}
                  </Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  Bot,
  Boxes,
  CheckCircle2,
  Circle,
  ClipboardList,
  Database,
  FileCode2,
  Network,
  RefreshCw,
  Rocket,
  Sparkles,
  Table2,
  Users,
} from "lucide-vue-next";
import { computed, onMounted } from "vue";
import { useI18n } from "vue-i18n";
import {
  type RouteLocationRaw,
  RouterLink,
  useRoute,
  useRouter,
} from "vue-router";
import AppLoading from "@/components/common/AppLoading.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useDashboard } from "@/composables/dashboard";
import { useAuthStore } from "@/store/modules/auth";
import { useEnvironmentStore } from "@/store/modules/environment";
import { Engine } from "@/types/proto-es/v1/common_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";

const { t, locale } = useI18n();
const router = useRouter();
const route = useRoute();
const authStore = useAuthStore();
const environmentStore = useEnvironmentStore();

const {
  instances,
  recentRuns,
  estate,
  openLineage,
  databasesPerInstance,
  isLoading,
  failedSections,
  lastUpdated,
  instancesTruncated,
  databasesTruncated,
  jobsTruncated,
  datasetsTruncated,
  canViewInstances,
  canViewDatabases,
  canViewOpenLineage,
  load,
} = useDashboard();

/** How many data sources the table shows before pointing at the full list. */
const TABLE_LIMIT = 8;

const userName = computed(() => authStore.userName);
const greeting = computed(() =>
  userName.value
    ? t("home.greeting", { name: userName.value })
    : t("home.welcome")
);

const initialLoading = computed(
  () => isLoading.value && lastUpdated.value === null
);

const visibleInstances = computed(() => instances.value.slice(0, TABLE_LIMIT));

const failedSectionLabels = computed(() =>
  failedSections.value.map((section) => {
    switch (section) {
      case "instances":
        return t("home.loadErrorInstances");
      case "databases":
        return t("home.loadErrorDatabases");
      default:
        return t("home.loadErrorOpenLineage");
    }
  })
);

const showSetup = computed(
  () =>
    canViewInstances.value &&
    canViewDatabases.value &&
    (estate.value.instanceCount === 0 || estate.value.databaseCount === 0)
);

const setupSteps = computed(() => [
  {
    key: "connect",
    done: estate.value.instanceCount > 0,
    title: t("home.stepConnectTitle"),
    description: t("home.stepConnectDescription"),
    cta: t("home.stepConnectCta"),
    to: { name: "InstanceManagement" },
  },
  {
    key: "sync",
    done: estate.value.databaseCount > 0,
    title: t("home.stepSyncTitle"),
    description: t("home.stepSyncDescription"),
    cta: t("home.stepSyncCta"),
    to: { name: "DatabaseManagement" },
  },
]);

const attentionItems = computed(() => {
  const summary = estate.value;
  const items: {
    key: string;
    text: string;
    to: RouteLocationRaw;
  }[] = [];

  if (canViewInstances.value && summary.inactiveInstanceCount > 0) {
    items.push({
      key: "inactiveInstances",
      text: t("home.inactiveInstances", {
        count: summary.inactiveInstanceCount,
      }),
      to: { name: "InstanceManagement" },
    });
  }
  if (canViewInstances.value && summary.neverSyncedInstanceCount > 0) {
    items.push({
      key: "neverSyncedInstances",
      text: t("home.neverSyncedInstances", {
        count: summary.neverSyncedInstanceCount,
      }),
      to: { name: "InstanceManagement" },
    });
  }
  if (canViewDatabases.value && summary.driftedDatabaseCount > 0) {
    items.push({
      key: "driftedDatabases",
      text: t("home.driftedDatabases", {
        count: summary.driftedDatabaseCount,
      }),
      to: { name: "DatabaseManagement" },
    });
  }
  if (canViewDatabases.value && summary.neverSyncedDatabaseCount > 0) {
    items.push({
      key: "neverSyncedDatabases",
      text: t("home.neverSyncedDatabases", {
        count: summary.neverSyncedDatabaseCount,
      }),
      to: { name: "DatabaseManagement" },
    });
  }

  return items;
});

// A full page means more rows exist, so the card shows a floor, not a total.
function countLabel(count: number, truncated: boolean): string {
  return truncated ? `${count}+` : `${count}`;
}

const stats = computed(() => {
  const summary = estate.value;
  const items: {
    key: string;
    label: string;
    value: string;
    hint: string;
    icon: typeof Database;
    to: RouteLocationRaw;
  }[] = [];

  if (canViewInstances.value) {
    items.push({
      key: "instances",
      label: t("home.statDataSources"),
      value: countLabel(summary.instanceCount, instancesTruncated.value),
      hint: t("home.statDataSourcesHint", {
        count: summary.activeInstanceCount,
      }),
      icon: Database,
      to: { name: "InstanceManagement" },
    });
  }
  if (canViewDatabases.value) {
    items.push({
      key: "databases",
      label: t("home.statDatabases"),
      value: countLabel(summary.databaseCount, databasesTruncated.value),
      hint: t("home.statDatabasesHint", {
        count: summary.driftedDatabaseCount,
      }),
      icon: Boxes,
      to: { name: "DatabaseManagement" },
    });
  }
  if (canViewOpenLineage.value) {
    items.push(
      {
        key: "jobs",
        label: t("home.statJobs"),
        value: countLabel(openLineage.value.jobCount, jobsTruncated.value),
        hint: t("home.statJobsHint", {
          count: openLineage.value.lineageReadyJobCount,
        }),
        icon: Network,
        to: { name: "OpenLineageTasks" },
      },
      {
        key: "datasets",
        label: t("home.statDatasets"),
        value: countLabel(
          openLineage.value.datasetCount,
          datasetsTruncated.value
        ),
        hint: t("home.statDatasetsHint", {
          count: openLineage.value.internalDatasetCount,
        }),
        icon: Table2,
        to: { name: "OpenLineageDatasets" },
      }
    );
  }

  return items;
});

const quickActions = computed(() => {
  const items: {
    key: string;
    label: string;
    description: string;
    icon: typeof Database;
    to: RouteLocationRaw;
  }[] = [];

  if (authStore.hasPermission("metaxisdata.databases.read")) {
    items.push({
      key: "metadata",
      label: t("menu.metadata"),
      description: t("home.actionMetadataDescription"),
      icon: Table2,
      to: { name: "MetadataBrowser" },
    });
  }
  if (authStore.hasPermission("metaxisdata.explainSql.explain")) {
    items.push({
      key: "explain",
      label: t("menu.explainSQL"),
      description: t("home.actionExplainSQLDescription"),
      icon: Sparkles,
      to: { name: "ExplainSQL" },
    });
  }
  if (authStore.hasPermission("metaxisdata.manualSqls.list")) {
    items.push({
      key: "manualSql",
      label: t("menu.manualSql"),
      description: t("home.actionManualSQLDescription"),
      icon: FileCode2,
      to: { name: "ManualSQLManagement" },
    });
  }
  if (
    authStore.hasPermission("metaxisdata.openlineage.namespaceMappings.list")
  ) {
    items.push({
      key: "openlineage",
      label: t("openlineage.ingestionSettings"),
      description: t("home.actionOpenLineageDescription"),
      icon: Network,
      to: { name: "OpenLineageSettings" },
    });
  }
  if (authStore.hasPermission("metaxisdata.llm.profiles.list")) {
    items.push({
      key: "llm",
      label: t("llmProvider.sidebar"),
      description: t("home.actionLLMDescription"),
      icon: Bot,
      to: { name: "LLMProviderManagement" },
    });
  }
  if (authStore.hasPermission("metaxisdata.users.list")) {
    items.push({
      key: "users",
      label: t("menu.users"),
      description: t("home.actionUsersDescription"),
      icon: Users,
      to: { name: "UserManagement" },
    });
  }
  if (authStore.hasPermission("metaxisdata.auditLogs.search")) {
    items.push({
      key: "auditLogs",
      label: t("menu.auditLogs"),
      description: t("home.actionAuditLogsDescription"),
      icon: ClipboardList,
      to: { name: "AuditLogs" },
    });
  }

  return items;
});

function instanceId(name: string): string {
  return name.replace("instances/", "");
}

function engineLabel(engine: Engine): string {
  const labels: Record<number, string> = {
    [Engine.ENGINE_UNSPECIFIED]: "Unknown",
    [Engine.MYSQL]: "MySQL",
    [Engine.POSTGRES]: "PostgreSQL",
    [Engine.TIDB]: "TiDB",
    [Engine.MARIADB]: "MariaDB",
    [Engine.OCEANBASE]: "OceanBase",
    [Engine.STARROCKS]: "StarRocks",
    [Engine.DORIS]: "Doris",
    [Engine.MSSQL]: "SQL Server",
  };
  return labels[engine] || "Unknown";
}

function environmentLabel(environment: string): string {
  if (!environment) {
    return "-";
  }
  return environmentStore.titleOf(environment);
}

function formatTimestamp(timestamp: Timestamp | undefined): string {
  if (!timestamp?.seconds) {
    return "-";
  }
  return new Intl.DateTimeFormat(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(Number(timestamp.seconds) * 1000));
}

function formatRelativeTime(timestamp: Timestamp | undefined): string {
  if (!timestamp?.seconds) {
    return "-";
  }
  const diffMs = Number(timestamp.seconds) * 1000 - Date.now();
  const formatter = new Intl.RelativeTimeFormat(locale.value, {
    numeric: "auto",
  });
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ["year", 365 * 24 * 60 * 60 * 1000],
    ["month", 30 * 24 * 60 * 60 * 1000],
    ["day", 24 * 60 * 60 * 1000],
    ["hour", 60 * 60 * 1000],
    ["minute", 60 * 1000],
    ["second", 1000],
  ];
  for (const [unit, unitMs] of units) {
    if (Math.abs(diffMs) >= unitMs || unit === "second") {
      return formatter.format(Math.round(diffMs / unitMs), unit);
    }
  }
  return formatter.format(0, "second");
}

function formatClock(date: Date | null): string {
  if (!date) {
    return "";
  }
  return new Intl.DateTimeFormat(locale.value, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function eventVariant(
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

function openInstance(instance: Instance) {
  router.push({
    name: "InstanceDetail",
    params: { instanceId: instanceId(instance.name) },
  });
}

function openRun(guid: string) {
  router.push({
    name: "OpenLineageRunDetail",
    params: { guid },
    query: { from: route.fullPath },
  });
}

onMounted(async () => {
  if (canViewInstances.value) {
    // Environment titles resolve from the shared cache; a failure there only
    // degrades the label to the raw environment ID.
    try {
      await environmentStore.ensureLoaded();
    } catch (error) {
      console.error("Failed to load environments for the dashboard:", error);
    }
  }
  await load();
});
</script>
