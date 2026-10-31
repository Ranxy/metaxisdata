<template>
  <div class="space-y-4">
    <PageHeader :title="t('dataQuality.issueDetailTitle')">
      <template #breadcrumb>
        <RouterLink
          :to="{ name: 'DataQualityIssues' }"
          class="inline-flex items-center gap-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft class="h-3.5 w-3.5" />
          {{ t("dataQuality.issuesTitle") }}
        </RouterLink>
      </template>
      <template #title-extra>
        <Badge
          v-if="issue"
          :variant="qualityIssueStatusVariant(issue.status)"
        >
          {{ t(qualityIssueStatusKey(issue.status)) }}
        </Badge>
      </template>
      <template #actions>
        <Button
          v-if="targetWorkspaceTo"
          variant="outline"
          as-child
        >
          <RouterLink :to="targetWorkspaceTo">
            <Target class="mr-2 h-4 w-4" />
            {{ t("dataQuality.asset.openWorkspace") }}
          </RouterLink>
        </Button>
        <Button
          variant="outline"
          :disabled="isLoading || saving"
          @click="load"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': isLoading }"
          />
          {{ t("common.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <PageState
      :loading="isLoading"
      :error="error"
    >
      <template #error>
        <Button
          variant="outline"
          size="sm"
          @click="load"
        >
          {{ t("common.refresh") }}
        </Button>
      </template>

      <div
        v-if="issue"
        class="space-y-4"
      >
        <Alert
          v-if="saveError"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>{{ t("dataQuality.issueActionFailed") }}</AlertTitle>
          <AlertDescription class="break-words">
            {{ saveError }}
          </AlertDescription>
        </Alert>

        <Alert
          v-if="mutedUntil"
          variant="default"
        >
          <BellOff class="h-4 w-4" />
          <AlertTitle>{{ t("dataQuality.issueMuted") }}</AlertTitle>
          <AlertDescription>
            {{ t("dataQuality.issueMutedHint", { time: formatDateTime(issue.mutedUntil, locale) }) }}
          </AlertDescription>
        </Alert>

        <Card>
          <CardContent class="space-y-4 pt-6">
            <div class="flex flex-wrap items-center gap-2">
              <Badge :variant="qualitySeverityVariant(issue.severity)">
                {{ t(qualitySeverityKey(issue.severity)) }}
              </Badge>
              <RouterLink
                v-if="issue.rule && canViewRule"
                :to="ruleRoute(issue.rule)"
                class="font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              >
                {{ ruleTitle }}
              </RouterLink>
              <span
                v-else
                class="font-medium"
              >
                {{ ruleTitle }}
              </span>
            </div>
            <div class="font-mono text-xs break-all text-muted-foreground">
              <RouterLink
                v-if="targetRoute"
                :to="targetRoute"
                :title="t('dataQuality.viewMetadata')"
                class="inline-flex items-center gap-1 transition-colors hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              >
                {{ targetPath }}
                <ExternalLink class="h-3 w-3 shrink-0" />
              </RouterLink>
              <template v-else>{{ targetPath }}</template>
            </div>
            <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.firstSeen") }}
                </div>
                <div
                  class="text-sm"
                  :title="formatDateTime(issue.firstSeenTime, locale)"
                >
                  {{ formatRelativeTime(issue.firstSeenTime, locale) }}
                </div>
                <RouterLink
                  v-if="canViewExecution && issue.firstSeenExecution"
                  :to="executionRoute(issue.firstSeenExecution)"
                  class="mt-1 inline-flex text-xs text-primary hover:underline"
                >
                  {{ t("dataQuality.viewExecution") }}
                </RouterLink>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.lastSeen") }}
                </div>
                <div
                  class="text-sm"
                  :title="formatDateTime(issue.lastSeenTime, locale)"
                >
                  {{ formatRelativeTime(issue.lastSeenTime, locale) }}
                </div>
                <RouterLink
                  v-if="canViewExecution && issue.lastSeenExecution"
                  :to="executionRoute(issue.lastSeenExecution)"
                  class="mt-1 inline-flex text-xs text-primary hover:underline"
                >
                  {{ t("dataQuality.viewExecution") }}
                </RouterLink>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.occurrences") }}
                </div>
                <div class="text-sm tabular-nums">
                  {{ issue.occurrenceCount }}
                </div>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.acknowledged") }}
                </div>
                <div class="text-sm">
                  {{ issue.acknowledgedBy ? acknowledgedByLabel : "-" }}
                </div>
                <div
                  v-if="issue.acknowledgedTime"
                  class="text-xs text-muted-foreground"
                  :title="formatDateTime(issue.acknowledgedTime, locale)"
                >
                  {{ formatRelativeTime(issue.acknowledgedTime, locale) }}
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card v-if="issue.status === QualityIssueStatus.RESOLVED">
          <CardHeader>
            <CardTitle class="text-base">
              {{ t("dataQuality.issueResolution") }}
            </CardTitle>
          </CardHeader>
          <CardContent class="space-y-3">
            <div class="grid gap-4 sm:grid-cols-2">
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.resolutionReasonLabel") }}
                </div>
                <div class="text-sm">
                  {{ t(qualityResolutionReasonKey(issue.resolutionReason)) }}
                </div>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQuality.resolvedAt") }}
                </div>
                <div
                  class="text-sm"
                  :title="formatDateTime(issue.resolvedTime, locale)"
                >
                  {{ formatRelativeTime(issue.resolvedTime, locale) }}
                </div>
              </div>
            </div>
            <div v-if="issue.resolutionNote">
              <div class="text-xs text-muted-foreground">
                {{ t("dataQuality.resolutionNote") }}
              </div>
              <p class="text-sm break-words">
                {{ issue.resolutionNote }}
              </p>
            </div>
          </CardContent>
        </Card>

        <Card v-if="canUpdate && issue.status !== QualityIssueStatus.RESOLVED">
          <CardHeader>
            <CardTitle class="text-base">
              {{ t("dataQuality.issueActions") }}
            </CardTitle>
            <CardDescription>
              {{ t("dataQuality.issueActionsHint") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="flex flex-wrap gap-2">
            <Button
              v-if="issue.status === QualityIssueStatus.OPEN"
              variant="outline"
              :disabled="saving"
              @click="acknowledge"
            >
              <Check class="mr-2 h-4 w-4" />
              {{ t("dataQuality.acknowledge") }}
            </Button>
            <Button
              variant="outline"
              :disabled="saving"
              @click="resolveOpen = true"
            >
              <CheckCheck class="mr-2 h-4 w-4" />
              {{ t("dataQuality.resolve") }}
            </Button>
            <Button
              variant="outline"
              :disabled="saving"
              @click="muteOpen = true"
            >
              <BellOff class="mr-2 h-4 w-4" />
              {{ t("dataQuality.mute") }}
            </Button>
          </CardContent>
        </Card>
      </div>

      <p
        v-else
        class="text-sm text-muted-foreground"
      >
        {{ t("dataQuality.issueNotFound") }}
      </p>
    </PageState>

    <Dialog
      :open="resolveOpen"
      @update:open="(value: boolean) => (resolveOpen = value)"
    >
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("dataQuality.resolveTitle") }}</DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.resolveHint") }}
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-3">
          <div class="flex flex-col gap-1.5">
            <Label for="dq-issue-resolution-reason">
              {{ t("dataQuality.resolutionReasonLabel") }}
            </Label>
            <Select
              :model-value="String(resolveReason)"
              @update:model-value="setResolveReason"
            >
              <SelectTrigger id="dq-issue-resolution-reason">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="reason in RESOLVE_REASONS"
                  :key="reason"
                  :value="String(reason)"
                >
                  {{ t(qualityResolutionReasonKey(reason)) }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="space-y-1.5">
            <Label for="dq-issue-resolution-note">
              {{ t("dataQuality.resolutionNote") }}
            </Label>
            <textarea
              id="dq-issue-resolution-note"
              v-model="resolveNote"
              class="min-h-24 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              :placeholder="t('dataQuality.resolutionNotePlaceholder')"
            />
          </div>
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            @click="resolveOpen = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="saving"
            @click="resolve"
          >
            {{ t("dataQuality.resolve") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog
      :open="muteOpen"
      @update:open="(value: boolean) => (muteOpen = value)"
    >
      <DialogContent class="max-w-sm">
        <DialogHeader>
          <DialogTitle>{{ t("dataQuality.muteTitle") }}</DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.muteHint") }}
          </DialogDescription>
        </DialogHeader>
        <div class="flex flex-col gap-1.5">
          <Label for="dq-issue-mute-duration">
            {{ t("dataQuality.muteDuration") }}
          </Label>
          <Select v-model="muteHours">
            <SelectTrigger id="dq-issue-mute-duration">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="1">
                {{ t("dataQuality.muteOneHour") }}
              </SelectItem>
              <SelectItem value="24">
                {{ t("dataQuality.muteOneDay") }}
              </SelectItem>
              <SelectItem value="168">
                {{ t("dataQuality.muteOneWeek") }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            @click="muteOpen = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="saving"
            @click="mute"
          >
            {{ t("dataQuality.mute") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  AlertTriangle,
  ArrowLeft,
  BellOff,
  Check,
  CheckCheck,
  ExternalLink,
  RefreshCcw,
  Target,
} from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import {
  acknowledgeQualityIssue,
  getQualityIssue,
  getQualityRule,
  muteQualityIssue,
  qualityRuleID,
  resolveQualityIssue,
} from "@/api/data-quality";
import { getUser } from "@/api/user";
import PageState from "@/components/common/PageState.vue";
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useAsync } from "@/composables/useAsync";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityIssue,
  QualityIssueStatus,
  QualityResolutionReason,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  formatTargetPath,
  qualityIssueStatusKey,
  qualityIssueStatusVariant,
  qualityResolutionReasonKey,
  qualitySeverityKey,
  qualitySeverityVariant,
  qualityTargetMetadataRoute,
  targetScopeLabel,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";

/**
 * One issue's evidence and lifecycle. Acknowledge says "seen, still broken";
 * Resolve closes it with a reason; Mute silences its notifications for a while
 * while executions and results continue.
 */

const { t, locale } = useI18n();
const route = useRoute();
const { formatError } = useErrorHandler();
const authStore = useAuthStore();

// The issue opened against one rule of one target; the workspace is where the
// rest of that target's checks live.
const targetWorkspaceTo = computed(() => {
  const target = issue.value?.target;
  if (!target?.metaGuid) {
    return undefined;
  }
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.metaGuid) },
    query: {
      metaType: String(target.metaType),
      highlight: issue.value?.rule ?? "",
    },
  };
});
const ruleTitle = ref("");
const acknowledgedByLabel = ref("");
const error = ref("");
const saveError = ref("");
const saving = ref(false);

const resolveOpen = ref(false);
const resolveReason = ref(QualityResolutionReason.FIXED);
const resolveNote = ref("");
const muteOpen = ref(false);
const muteHours = ref("24");

const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.issues.update")
);
const canViewRule = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.get")
);
const canViewExecution = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.executions.get")
);

const mutedUntil = computed(() => {
  const seconds = issue.value?.mutedUntil?.seconds;
  return seconds !== undefined && Number(seconds) * 1000 > Date.now();
});

const targetPath = computed(() => {
  const target = issue.value?.target;
  const scope = targetScopeLabel(target?.path);
  const name = formatTargetPath(target?.path);
  if (name === "-") {
    return target?.metaGuid || "-";
  }
  return scope === "-" ? name : `${scope} · ${name}`;
});

/** The target's metadata page; absent when the snapshot carries no GUID. */
const targetRoute = computed(() =>
  qualityTargetMetadataRoute(issue.value?.target)
);

const RESOLVE_REASONS = [
  QualityResolutionReason.FIXED,
  QualityResolutionReason.EXPECTED,
  QualityResolutionReason.NO_ACTION_NEEDED,
  QualityResolutionReason.DUPLICATE,
];

/** The issue resource name the route carries, one segment per name segment. */
const issueName = computed(() => {
  const param = route.params.issueId;
  return Array.isArray(param) ? param.join("/") : String(param ?? "");
});

/**
 * One issue's evidence. The ticket-and-abort behaviour lives in `useAsync`: a
 * route change to another issue must not let the previous answer land.
 */
const {
  data: issue,
  loading: isLoading,
  refresh: refreshIssue,
} = useAsync(
  async () => {
    const loaded = await getQualityIssue(issueName.value);
    void loadAcknowledgedBy(loaded.acknowledgedBy);
    await loadRuleTitle(loaded.rule);
    return loaded;
  },
  {
    onError: (e: unknown) => {
      error.value = formatError(e, "dataQuality.fetchIssueError");
    },
  }
);

function setResolveReason(value: unknown) {
  resolveReason.value = Number(value) as QualityResolutionReason;
}

function principalLabel(name: string): string {
  return name.split("/").pop() ?? name;
}

/**
 * The acknowledged-by cell names the acknowledging user when the caller may read
 * users, and falls back to the trailing id otherwise. Best effort and silent:
 * the id is still meaningful, so a missing permission or a failed lookup is not
 * an error. The generation counter keeps a slower lookup for a previously shown
 * issue from overwriting the label of the issue now on screen.
 */
let acknowledgedByLookup = 0;

async function loadAcknowledgedBy(name: string) {
  const lookup = ++acknowledgedByLookup;
  acknowledgedByLabel.value = principalLabel(name);
  if (!name || !authStore.hasPermission("metaxisdata.users.get")) {
    return;
  }
  try {
    const user = await getUser(name);
    if (lookup === acknowledgedByLookup) {
      acknowledgedByLabel.value =
        user.title || user.email || principalLabel(name);
    }
  } catch {
    // The trailing id stands.
  }
}

/**
 * A rule is a check inside its target's contract workspace; the old per-rule
 * page is gone, so the link resolves to the workspace with the check selected.
 */
function ruleRoute(name: string) {
  const target = issue.value?.target;
  if (target?.metaGuid) {
    return {
      name: "DataQualityTarget",
      params: { targetGuid: guidToRouteParams(target.metaGuid) },
      query: { metaType: String(target.metaType), check: name },
    };
  }
  return {
    name: "DataQualityRuleRedirect",
    params: { ruleId: name.split("/") },
  };
}

function executionRoute(name: string) {
  return {
    name: "DataQualityExecutionDetail",
    params: { executionId: name.split("/") },
  };
}

async function load() {
  error.value = "";
  await refreshIssue();
}

/** Best effort: a caller without rules.get still sees the issue by rule id. */
async function loadRuleTitle(ruleName: string) {
  if (!ruleName) {
    ruleTitle.value = "";
    return;
  }
  try {
    const rule = await getQualityRule(ruleName);
    ruleTitle.value = rule.title || qualityRuleID(ruleName);
  } catch {
    ruleTitle.value = qualityRuleID(ruleName);
  }
}

/** Performs one lifecycle action; true when it landed, so the caller toasts. */
async function runAction(
  action: () => Promise<QualityIssue>
): Promise<boolean> {
  saving.value = true;
  saveError.value = "";
  try {
    await action();
    resolveOpen.value = false;
    muteOpen.value = false;
    await load();
    return true;
  } catch (e: unknown) {
    saveError.value = formatError(e, "dataQuality.issueActionFailed");
    return false;
  } finally {
    saving.value = false;
  }
}

async function acknowledge() {
  if (!issue.value) {
    return;
  }
  if (
    await runAction(() => acknowledgeQualityIssue(issue.value as QualityIssue))
  ) {
    notify.success(t("dataQuality.issueAcknowledged"));
  }
}

async function resolve() {
  if (!issue.value) {
    return;
  }
  if (
    await runAction(() =>
      resolveQualityIssue(
        issue.value as QualityIssue,
        resolveReason.value,
        resolveNote.value
      )
    )
  ) {
    notify.success(t("dataQuality.issueResolved"));
  }
}

async function mute() {
  if (!issue.value) {
    return;
  }
  const hours = Number(muteHours.value);
  const until = timestampFromDate(
    new Date(Date.now() + Math.max(1, hours) * 3_600_000)
  );
  if (
    await runAction(() => muteQualityIssue(issue.value as QualityIssue, until))
  ) {
    notify.success(t("dataQuality.issueMutedToast"));
  }
}
</script>
