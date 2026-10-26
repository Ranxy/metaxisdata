<template>
  <Card class="overflow-hidden">
    <!-- One run is one dispatch batch. The header is the batch, the counts row
         is this target's slice of it: a scheduled batch can cover several
         targets, so the batch-wide totals would misreport this table. -->
    <div class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b bg-muted/30 px-3 py-2">
      <span
        class="text-sm font-medium whitespace-nowrap"
        :title="formatDateTime(startTime, locale)"
      >
        {{ formatRelativeTime(startTime, locale) }}
      </span>
      <Badge variant="outline">
        {{ t(qualityExecutionTriggerKey(run.trigger)) }}
      </Badge>
      <span
        v-if="run.targets.length > 1"
        class="text-xs text-muted-foreground"
      >
        {{ t("dataQuality.executions.runTargets", { count: run.targets.length }) }}
      </span>
      <span
        v-if="run.initiator"
        class="text-xs text-muted-foreground"
        :title="run.initiator"
      >
        {{ t("dataQuality.executions.initiator") }} {{ emailOf(run.initiator) }}
      </span>
      <Badge :variant="qualityExecutionStateVariant(run.state)">
        {{ t(qualityExecutionStateKey(run.state)) }}
      </Badge>
      <Badge
        v-if="outcome !== QualityStatus.UNSPECIFIED"
        :variant="qualityStatusVariant(outcome)"
      >
        {{ t(qualityStatusKey(outcome)) }}
      </Badge>
      <span class="text-xs tabular-nums text-muted-foreground">
        {{ durationLabel }}
      </span>
      <span
        v-if="revisionLabel"
        class="text-xs tabular-nums text-muted-foreground"
      >
        {{ revisionLabel }}
      </span>

      <div class="ml-auto flex items-center gap-1">
        <Button
          variant="ghost"
          size="sm"
          class="h-7"
          as-child
        >
          <RouterLink :to="allExecutionsRoute">
            {{ t("dataQuality.executions.runOpenInList") }}
          </RouterLink>
        </Button>
        <Button
          variant="ghost"
          size="icon"
          class="h-7 w-7"
          :aria-expanded="expanded"
          :aria-label="
            expanded
              ? t('dataQuality.executions.runCollapse')
              : t('dataQuality.executions.runExpand')
          "
          :title="
            expanded
              ? t('dataQuality.executions.runCollapse')
              : t('dataQuality.executions.runExpand')
          "
          @click="toggle"
        >
          <ChevronDown
            v-if="expanded"
            class="h-4 w-4"
          />
          <ChevronRight
            v-else
            class="h-4 w-4"
          />
        </Button>
      </div>
    </div>

    <div class="flex flex-wrap items-center gap-x-2 gap-y-1.5 px-3 py-2">
      <Badge
        v-for="part in verdictCounts"
        :key="part.status"
        :variant="qualityStatusVariant(part.status)"
        class="tabular-nums"
      >
        {{ part.count }} {{ t(qualityStatusKey(part.status)) }}
      </Badge>
      <Badge
        v-if="errorCount > 0"
        variant="danger"
        class="tabular-nums"
      >
        {{ t("dataQuality.executions.runErrors", { count: errorCount }) }}
      </Badge>
      <span class="text-xs tabular-nums text-muted-foreground">
        {{ t("dataQuality.executions.runChecks", { count: totalChecks }) }}
      </span>
      <span
        v-if="unfinishedChecks > 0"
        class="text-xs tabular-nums text-warning"
      >
        {{ t("dataQuality.executions.runIncomplete", { count: unfinishedChecks }) }}
      </span>
      <span
        v-if="failedChecks > 0"
        class="text-xs tabular-nums text-destructive"
      >
        {{ t("dataQuality.executions.runFailed", { count: failedChecks }) }}
      </span>
      <span
        v-if="stateHintKey"
        class="text-xs text-muted-foreground"
      >
        {{ t(stateHintKey) }}
      </span>
    </div>

    <!-- The batch's checks for this target, loaded on first expand: the history
         list stays one request deep until a reader asks for the detail. -->
    <div
      v-if="expanded"
      class="border-t"
    >
      <PageState
        :loading="checksLoading"
        :error="checksError"
        :title="t('dataQuality.executions.runExecutionsFailed')"
      >
        <template #error>
          <Button
            variant="outline"
            size="sm"
            @click="loadChecks"
          >
            {{ t("common.refresh") }}
          </Button>
        </template>

        <p
          v-if="checks.length === 0"
          class="px-3 py-2 text-xs text-muted-foreground"
        >
          {{ t("dataQuality.executions.runChecks", { count: 0 }) }}
        </p>

        <Table
          v-else
          class="[&_th]:whitespace-nowrap"
        >
          <TableHeader>
            <TableRow>
              <TableHead>{{ t("dataQuality.executions.checksColumn") }}</TableHead>
              <TableHead>{{ t("dataQuality.executions.checkResult") }}</TableHead>
              <TableHead class="hidden md:table-cell">
                {{ t("dataQuality.executions.checkObserved") }}
              </TableHead>
              <TableHead class="hidden text-right md:table-cell">
                {{ t("dataQuality.executions.checkFailedRows") }}
              </TableHead>
              <TableHead class="hidden md:table-cell">
                {{ t("dataQuality.executions.checkDuration") }}
              </TableHead>
              <TableHead class="hidden whitespace-nowrap md:table-cell">
                {{ t("dataQuality.executions.checkStarted") }}
              </TableHead>
              <TableHead class="w-12 text-right">
                <span class="sr-only">{{ t("common.actions") }}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="execution in checks"
              :key="execution.name"
            >
              <TableCell>
                <RouterLink
                  :to="checkRoute(execution)"
                  class="block max-w-[16rem] truncate text-left font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none lg:max-w-[22rem]"
                  :title="ruleLabel(execution)"
                  @click.stop
                >
                  {{ ruleLabel(execution) }}
                </RouterLink>
                <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                  <span class="tabular-nums">{{ observedLabel(execution) }}</span>
                  <span
                    v-if="execution.outcome !== QualityStatus.UNSPECIFIED"
                    class="tabular-nums"
                  >
                    {{ formatRowCount(execution.failedRows, locale) }} /
                    {{ formatRowCount(execution.totalRows, locale) }}
                  </span>
                  <span class="tabular-nums">{{ executionDurationLabel(execution) }}</span>
                  <span :title="formatDateTime(execution.startTime, locale)">
                    {{ formatRelativeTime(execution.startTime, locale) }}
                  </span>
                </div>
              </TableCell>
              <TableCell>
                <Badge
                  v-if="execution.outcome !== QualityStatus.UNSPECIFIED"
                  :variant="qualityStatusVariant(execution.outcome)"
                >
                  {{ t(qualityStatusKey(execution.outcome)) }}
                </Badge>
                <span
                  v-else
                  class="text-sm text-muted-foreground"
                >
                  -
                </span>
              </TableCell>
              <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                {{ observedLabel(execution) }}
              </TableCell>
              <TableCell class="hidden text-right whitespace-nowrap tabular-nums md:table-cell">
                {{
                  execution.outcome === QualityStatus.UNSPECIFIED
                    ? "-"
                    : formatRowCount(execution.failedRows, locale)
                }}
              </TableCell>
              <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                {{ executionDurationLabel(execution) }}
              </TableCell>
              <TableCell
                class="hidden whitespace-nowrap text-muted-foreground md:table-cell"
                :title="formatDateTime(execution.startTime, locale)"
              >
                {{ formatRelativeTime(execution.startTime, locale) }}
              </TableCell>
              <TableCell class="text-right">
                <Button
                  variant="ghost"
                  size="icon"
                  class="h-8 w-8"
                  as-child
                >
                  <RouterLink
                    :to="executionRoute(execution)"
                    :title="t('dataQuality.viewExecution')"
                    :aria-label="t('dataQuality.viewExecution')"
                  >
                    <Eye class="h-4 w-4 text-muted-foreground" />
                  </RouterLink>
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </PageState>
    </div>
  </Card>
</template>

<script setup lang="ts">
import { ChevronDown, ChevronRight, Eye } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { listQualityExecutions, qualityRuleID } from "@/api/data-quality";
import PageState from "@/components/common/PageState.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useUserDirectory } from "@/composables/useUserDirectory";
import {
  type QualityExecution,
  QualityExecutionState,
  type QualityRun,
  QualityStatus,
  type QualityStatusCounts,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  executionDurationSeconds,
  formatObservedValue,
  formatRowCount,
  formatSeconds,
  qualityExecutionStateKey,
  qualityExecutionStateVariant,
  qualityExecutionTriggerKey,
  qualityStatusKey,
  qualityStatusVariant,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";

/**
 * One scan batch as a target sees it: the batch header, this target's slice of
 * the counts, and the checks themselves on demand.
 *
 * The counts always come from `run.targets[target]`, never from the batch-wide
 * tally: a scheduled batch can cover several targets, and this card answers
 * "what did this batch do to this table?".
 */

const props = withDefaults(
  defineProps<{
    run: QualityRun;
    /** The target whose slice and checks this card shows. */
    targetGuid: string;
    /** Rule resource name -> title, for the check rows. */
    ruleTitles?: Record<string, string>;
    /** Whether the checks table is expanded. */
    modelValue?: boolean;
  }>(),
  { ruleTitles: () => ({}), modelValue: false }
);

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const { emailOf, load: loadUserDirectory } = useUserDirectory();

const VERDICT_ORDER: QualityStatus[] = [
  QualityStatus.PASS,
  QualityStatus.WARN,
  QualityStatus.FAIL,
  QualityStatus.ERROR,
  QualityStatus.SKIPPED,
  QualityStatus.NOT_EVALUATED,
];

/** This target's slice of the batch; the batch may have covered many targets. */
const slice = computed(() =>
  props.run.targets.find(
    (candidate) => candidate.target?.metaGuid === props.targetGuid
  )
);

const counts = computed<QualityStatusCounts | undefined>(
  () => slice.value?.counts
);
const totalChecks = computed(() => counts.value?.total ?? 0);
const outcome = computed(
  () => slice.value?.outcome ?? QualityStatus.UNSPECIFIED
);

const startTime = computed(() => slice.value?.startTime ?? props.run.startTime);

function verdictCount(status: QualityStatus): number {
  const current = counts.value;
  if (!current) {
    return 0;
  }
  switch (status) {
    case QualityStatus.PASS:
      return current.pass;
    case QualityStatus.WARN:
      return current.warn;
    case QualityStatus.FAIL:
      return current.fail;
    case QualityStatus.ERROR:
      return current.error;
    case QualityStatus.SKIPPED:
      return current.skipped;
    case QualityStatus.NOT_EVALUATED:
      return current.notEvaluated;
    default:
      return 0;
  }
}

/**
 * The verdicts this slice recorded, in status order. ERROR has its own badge
 * (the "runErrors" wording), so it is left out here to avoid counting it twice.
 */
const verdictCounts = computed(() =>
  VERDICT_ORDER.filter((status) => status !== QualityStatus.ERROR)
    .map((status) => ({ status, count: verdictCount(status) }))
    .filter((verdict) => verdict.count > 0)
);

const errorCount = computed(() => verdictCount(QualityStatus.ERROR));
const failedChecks = computed(
  () => verdictCount(QualityStatus.FAIL) + errorCount.value
);

/** Planned checks that never reached a verdict; a running or abandoned batch. */
const unfinishedChecks = computed(() => {
  const finished = VERDICT_ORDER.reduce(
    (sum, status) => sum + verdictCount(status),
    0
  );
  return Math.max(0, totalChecks.value - finished);
});

const durationLabel = computed(() => {
  const seconds = executionDurationSeconds({
    startTime: startTime.value,
    finishTime: slice.value?.finishTime ?? props.run.finishTime,
  });
  return seconds === undefined ? "-" : formatSeconds(seconds);
});

/** The rule revisions this slice judged; a batch is only comparable at one range. */
const revisionLabel = computed(() => {
  const current = slice.value;
  if (!current || current.ruleRevisionMax === 0) {
    return "";
  }
  if (current.ruleRevisionMin === current.ruleRevisionMax) {
    return t("dataQuality.executions.revisionSingle", {
      revision: current.ruleRevisionMax,
    });
  }
  return t("dataQuality.executions.revisionRange", {
    min: current.ruleRevisionMin,
    max: current.ruleRevisionMax,
  });
});

/** A batch can be queued, running or abandoned; only the last two need words. */
const stateHintKey = computed(() => {
  switch (props.run.state) {
    case QualityExecutionState.PENDING:
    case QualityExecutionState.RUNNING:
      return "dataQuality.executions.runRunningHint";
    case QualityExecutionState.ABANDONED:
      return "dataQuality.executions.runAbandonedHint";
    default:
      return "";
  }
});

const expanded = computed(() => props.modelValue);

function toggle() {
  emit("update:modelValue", !expanded.value);
}

const allExecutionsRoute = computed(() => ({
  name: "DataQualityExecutions",
  query: { view: "executions", run: props.run.name },
}));

function ruleLabel(execution: QualityExecution): string {
  return props.ruleTitles[execution.rule] || qualityRuleID(execution.rule);
}

/** The check's workspace row: its target, with this check selected. */
function checkRoute(execution: QualityExecution) {
  const query: Record<string, string> = {};
  const metaType = slice.value?.target?.metaType ?? MetaType.UNSPECIFIED;
  if (metaType !== MetaType.UNSPECIFIED) {
    query.metaType = String(metaType);
  }
  query.check = execution.rule;
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(props.targetGuid) },
    query,
  };
}

function executionRoute(execution: QualityExecution) {
  return {
    name: "DataQualityExecutionDetail",
    params: { executionId: execution.name.split("/") },
  };
}

function observedLabel(execution: QualityExecution): string {
  if (execution.outcome === QualityStatus.UNSPECIFIED) {
    return "-";
  }
  return formatObservedValue(
    execution.observedValue,
    execution.observedKind,
    locale.value
  );
}

function executionDurationLabel(execution: QualityExecution): string {
  const seconds = executionDurationSeconds(execution);
  return seconds === undefined ? "-" : formatSeconds(seconds);
}

// ---- checks, loaded on first expand ----

const checks = ref<QualityExecution[]>([]);
const checksLoading = ref(false);
const checksError = ref("");
let checksLoaded = false;

async function loadChecks() {
  checksLoading.value = true;
  checksError.value = "";
  try {
    const page = await listQualityExecutions({
      run: props.run.name,
      pageSize: 200,
    });
    checks.value = page.items;
    checksLoaded = true;
  } catch (error: unknown) {
    checksError.value = formatError(
      error,
      "dataQuality.executions.runExecutionsFailed"
    );
  } finally {
    checksLoading.value = false;
  }
}

watch(
  expanded,
  (open) => {
    if (open && !checksLoaded) {
      void loadChecks();
    }
  },
  { immediate: true }
);

onMounted(() => {
  // Only a manual batch names an initiator; names resolve through the shared
  // directory when the caller may read it.
  if (props.run.initiator) {
    void loadUserDirectory();
  }
});
</script>
