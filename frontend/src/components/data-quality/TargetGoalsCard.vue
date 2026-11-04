<template>
  <Card>
    <CardHeader class="border-b">
      <CardTitle class="text-base">
        {{ t("dataQuality.goals.title") }}
      </CardTitle>
      <CardDescription>{{ t("dataQuality.goals.hint") }}</CardDescription>
    </CardHeader>
    <CardContent class="space-y-3 pt-4">
      <p
        v-if="loading"
        class="text-sm text-muted-foreground"
      >
        {{ t("common.loading") }}
      </p>
      <Alert
        v-else-if="error"
        variant="destructive"
      >
        <AlertTriangle class="h-4 w-4" />
        <AlertDescription>{{ error }}</AlertDescription>
      </Alert>
      <p
        v-else-if="goals.length === 0"
        class="text-sm text-muted-foreground"
      >
        {{ t("dataQuality.goals.empty") }}
      </p>
      <ul
        v-else
        class="space-y-2"
      >
        <li
          v-for="goal in goals"
          :key="goal.name"
          class="rounded-md border px-3 py-2 text-sm"
        >
          <div class="flex flex-wrap items-center gap-2">
            <Badge :variant="phaseVariant(goal.phase)">
              {{ phaseLabel(goal.phase) }}
            </Badge>
            <span class="text-xs text-muted-foreground">
              {{ t("dataQuality.goals.round", { round: goal.roundsStarted, max: goal.maxRounds }) }}
            </span>
            <span class="ml-auto text-xs text-muted-foreground">
              {{ formatDateTime(goal.createTime, locale) }}
            </span>
          </div>
          <p
            v-if="goal.objective"
            class="mt-1 break-words"
          >
            {{ goal.objective }}
          </p>
          <p
            v-if="goal.blockedCode"
            class="mt-1 break-words text-xs text-muted-foreground"
          >
            {{ goal.blockedCode }}<template v-if="goal.blockedReason"> · {{ goal.blockedReason }}</template>
          </p>
          <p
            v-if="goal.nextWakeTime"
            class="mt-1 text-xs text-muted-foreground"
          >
            {{ t("dataQuality.goals.nextWake", { time: formatDateTime(goal.nextWakeTime, locale) }) }}
          </p>
          <div
            v-if="canManage"
            class="mt-2 flex flex-wrap gap-2"
          >
            <Button
              v-if="goal.phase === QualityAgentGoalPhase.ACTIVE"
              size="sm"
              variant="outline"
              :disabled="busy"
              @click="pause(goal)"
            >
              {{ t("dataQuality.goals.pause") }}
            </Button>
            <Button
              v-else-if="goal.phase === QualityAgentGoalPhase.PAUSED || goal.phase === QualityAgentGoalPhase.BLOCKED"
              size="sm"
              variant="outline"
              :disabled="busy"
              @click="resume(goal)"
            >
              {{ t("dataQuality.goals.resume") }}
            </Button>
            <Button
              v-if="goal.phase !== QualityAgentGoalPhase.COMPLETE"
              size="sm"
              variant="outline"
              :disabled="busy"
              @click="complete(goal)"
            >
              {{ t("dataQuality.goals.complete") }}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              class="text-destructive"
              :disabled="busy"
              @click="clearTarget = goal"
            >
              {{ t("dataQuality.goals.clear") }}
            </Button>
          </div>
        </li>
      </ul>

      <Dialog
        :open="clearTarget !== undefined"
        @update:open="(open) => { if (!open) clearTarget = undefined; }"
      >
        <DialogContent class="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{{ t("dataQuality.goals.clearTitle") }}</DialogTitle>
            <DialogDescription>{{ t("dataQuality.goals.clearDescription") }}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              @click="clearTarget = undefined"
            >
              {{ t("common.cancel") }}
            </Button>
            <Button
              variant="destructive"
              :disabled="busy"
              @click="clear"
            >
              {{ t("dataQuality.goals.clearConfirm") }}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { AlertTriangle } from "lucide-vue-next";
import { onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  clearQualityAgentGoal,
  completeQualityAgentGoal,
  listQualityAgentGoals,
  pauseQualityAgentGoal,
  resumeQualityAgentGoal,
} from "@/api/data-quality";
import { Alert, AlertDescription } from "@/components/ui/alert";
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
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import {
  type QualityAgentGoal,
  QualityAgentGoalPhase,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime } from "@/utils/datetime";

/**
 * The runner's goals for this target: what it is doing, how far it got and why it
 * stopped. Pause/resume/complete/clear are the human controls over the boundary; the
 * goal itself never publishes anything.
 */

const props = defineProps<{
  targetGuid: string;
  canManage: boolean;
}>();

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();

const goals = ref<QualityAgentGoal[]>([]);
const loading = ref(false);
const error = ref("");
const busy = ref(false);
const clearTarget = ref<QualityAgentGoal | undefined>(undefined);

async function load() {
  if (!props.targetGuid) {
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    goals.value = await listQualityAgentGoals(props.targetGuid);
  } catch (e: unknown) {
    error.value = formatError(e, "dataQuality.goals.loadFailed");
  } finally {
    loading.value = false;
  }
}

function phaseLabel(phase: QualityAgentGoalPhase): string {
  switch (phase) {
    case QualityAgentGoalPhase.ACTIVE:
      return t("dataQuality.goals.phaseActive");
    case QualityAgentGoalPhase.PAUSED:
      return t("dataQuality.goals.phasePaused");
    case QualityAgentGoalPhase.BLOCKED:
      return t("dataQuality.goals.phaseBlocked");
    case QualityAgentGoalPhase.COMPLETE:
      return t("dataQuality.goals.phaseComplete");
    default:
      return t("dataQuality.goals.phaseUnspecified");
  }
}

function phaseVariant(
  phase: QualityAgentGoalPhase
): "success" | "warning" | "neutral" | "danger" {
  switch (phase) {
    case QualityAgentGoalPhase.ACTIVE:
      return "success";
    case QualityAgentGoalPhase.BLOCKED:
      return "warning";
    case QualityAgentGoalPhase.PAUSED:
      return "neutral";
    case QualityAgentGoalPhase.COMPLETE:
      return "neutral";
    default:
      return "neutral";
  }
}

async function pause(goal: QualityAgentGoal) {
  await apply(goal, () => pauseQualityAgentGoal(goal.name, goal.revision));
}

async function resume(goal: QualityAgentGoal) {
  await apply(goal, () => resumeQualityAgentGoal(goal.name, goal.revision));
}

async function complete(goal: QualityAgentGoal) {
  await apply(goal, () => completeQualityAgentGoal(goal.name, goal.revision));
}

async function clear() {
  const target = clearTarget.value;
  if (!target || busy.value) {
    return;
  }
  busy.value = true;
  try {
    await clearQualityAgentGoal(target.name, target.revision);
    clearTarget.value = undefined;
    notify.success(t("dataQuality.goals.cleared"));
    await load();
  } catch (e: unknown) {
    notify.error(formatError(e, "dataQuality.goals.actionFailed"));
  } finally {
    busy.value = false;
  }
}

async function apply(
  goal: QualityAgentGoal,
  action: () => Promise<QualityAgentGoal>
) {
  if (busy.value) {
    return;
  }
  busy.value = true;
  try {
    const updated = await action();
    const index = goals.value.findIndex((item) => item.name === goal.name);
    if (index >= 0) {
      goals.value[index] = updated;
    }
    notify.success(t("dataQuality.goals.updated"));
  } catch (e: unknown) {
    notify.error(formatError(e, "dataQuality.goals.actionFailed"));
    // A stale revision means the runner moved: reload so the controls match.
    await load();
  } finally {
    busy.value = false;
  }
}

onMounted(load);
watch(
  () => props.targetGuid,
  () => void load()
);
</script>
