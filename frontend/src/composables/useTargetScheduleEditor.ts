import { type ComputedRef, computed, type Ref, ref } from "vue";
import { useI18n } from "vue-i18n";
import { updateQualityTargetSchedule } from "@/api/data-quality";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import type {
  QualityTargetSchedule,
  QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  durationToSchedule,
  type ScheduleUnit,
  scheduleToDuration,
  validateScheduleInterval,
} from "@/utils/dataQualityForm";

export interface TargetScheduleEditorOptions {
  summary: Ref<QualityTargetSummary>;
  targetGuid: ComputedRef<string>;
  effectiveMetaType: ComputedRef<MetaType>;
}

/**
 * The target's single schedule editor.
 *
 * Every enabled check on a target runs on one schedule, so the workspace holds
 * one dialog: open it seeded from the current interval, validate as the reader
 * types, and patch the summary with the server's answer. The open/validate/save
 * cycle was a set of page-local refs with no test; it lives here so the
 * seeding from a duration and the save contract are testable.
 */
export function useTargetScheduleEditor(options: TargetScheduleEditorOptions) {
  const { t } = useI18n();
  const { formatError } = useErrorMessage();

  const scheduleOpen = ref(false);
  const scheduleValue = ref("1");
  const scheduleUnit = ref<ScheduleUnit>("hours");
  const scheduleSaving = ref(false);
  const scheduleError = ref("");

  /** The target's schedule, as the aggregate read resolved it. */
  const targetSchedule = computed<QualityTargetSchedule | undefined>(
    () => options.summary.value.schedule
  );

  /** Live client-side validation for the schedule dialog. */
  const scheduleIssueKey = computed(() => {
    const [issue] = validateScheduleInterval({
      value: scheduleValue.value,
      unit: scheduleUnit.value,
    });
    return issue?.key ?? "";
  });

  function openScheduleEditor() {
    const parsed = durationToSchedule(targetSchedule.value?.scheduleInterval);
    const empty = parsed.value === "";
    scheduleValue.value = empty ? "1" : parsed.value;
    scheduleUnit.value = empty ? "hours" : parsed.unit;
    scheduleError.value = "";
    scheduleOpen.value = true;
  }

  function setScheduleUnit(value: unknown) {
    scheduleUnit.value = String(value) as ScheduleUnit;
  }

  function onScheduleOpenChange(open: boolean) {
    if (!open) {
      scheduleOpen.value = false;
    }
  }

  /** Save the target's schedule; the returned schedule patches the summary. */
  async function saveSchedule() {
    if (scheduleSaving.value || scheduleIssueKey.value !== "") {
      return;
    }
    scheduleSaving.value = true;
    scheduleError.value = "";
    try {
      const updated = await updateQualityTargetSchedule({
        targetGuid: options.targetGuid.value,
        metaType: options.effectiveMetaType.value,
        scheduleInterval: scheduleToDuration({
          value: scheduleValue.value,
          unit: scheduleUnit.value,
        }),
      });
      options.summary.value.schedule = updated;
      scheduleOpen.value = false;
      notify.success(t("dataQuality.workspace.scheduleUpdated"));
    } catch (cause: unknown) {
      scheduleError.value = formatError(
        cause,
        "dataQuality.workspace.scheduleUpdateFailed"
      );
    } finally {
      scheduleSaving.value = false;
    }
  }

  return {
    targetSchedule,
    scheduleOpen,
    scheduleValue,
    scheduleUnit,
    scheduleSaving,
    scheduleError,
    scheduleIssueKey,
    openScheduleEditor,
    setScheduleUnit,
    onScheduleOpenChange,
    saveSchedule,
  };
}
