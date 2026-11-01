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
  isValidCronExpression,
  type ScheduleUnit,
  scheduleToDuration,
  validateScheduleInterval,
} from "@/utils/dataQualityForm";
import { isValidTimeZone } from "@/utils/dataQualitySettings";

export interface TargetScheduleEditorOptions {
  summary: Ref<QualityTargetSummary>;
  targetGuid: ComputedRef<string>;
  effectiveMetaType: ComputedRef<MetaType>;
}

/** The two exclusive forms a target's schedule can take. */
export type TargetScheduleMode = "interval" | "cron";

const browserTimezone = (() => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
})();

// A short curated list; the browser zone and the stored zone are added to it.
const SCHEDULE_TIMEZONES = [
  "UTC",
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Asia/Singapore",
  "Asia/Kolkata",
  "Europe/London",
  "Europe/Berlin",
  "Europe/Paris",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "Australia/Sydney",
];

/**
 * The target's single schedule editor.
 *
 * Every enabled check on a target runs on one schedule, so the workspace holds
 * one dialog: open it seeded from the stored form (an interval, or a cron
 * expression in an IANA time zone), validate as the reader types, and patch the
 * summary with the server's answer. The open/validate/save cycle lives here so
 * the seeding from a stored schedule and the save contract are testable.
 */
export function useTargetScheduleEditor(options: TargetScheduleEditorOptions) {
  const { t } = useI18n();
  const { formatError } = useErrorMessage();

  const scheduleOpen = ref(false);
  const scheduleMode = ref<TargetScheduleMode>("interval");
  const scheduleValue = ref("1");
  const scheduleUnit = ref<ScheduleUnit>("hours");
  const cronExpression = ref("");
  const scheduleTimezone = ref(browserTimezone);
  const scheduleSaving = ref(false);
  const scheduleError = ref("");

  /** The target's schedule, as the aggregate read resolved it. */
  const targetSchedule = computed<QualityTargetSchedule | undefined>(
    () => options.summary.value.schedule
  );

  /** The zone options: the curated list plus the browser and stored zones. */
  const timezoneOptions = computed(() => {
    const zones = [...SCHEDULE_TIMEZONES];
    for (const zone of [browserTimezone, scheduleTimezone.value]) {
      if (zone !== "" && !zones.includes(zone)) {
        zones.unshift(zone);
      }
    }
    return zones;
  });

  /** Live client-side validation for the schedule dialog, as a catalog key. */
  const scheduleIssueKey = computed(() => {
    if (scheduleMode.value === "cron") {
      if (!isValidCronExpression(cronExpression.value)) {
        return "dataQuality.editor.validation.cronInvalid";
      }
      if (!isValidTimeZone(scheduleTimezone.value)) {
        return "dataQuality.editor.validation.timezoneInvalid";
      }
      return "";
    }
    const [issue] = validateScheduleInterval({
      value: scheduleValue.value,
      unit: scheduleUnit.value,
    });
    return issue?.key ?? "";
  });

  function openScheduleEditor() {
    const schedule = targetSchedule.value;
    scheduleError.value = "";
    // Both forms are seeded: a cron target reports the expression's nominal gap
    // as its interval, so switching the mode starts from a usable cadence.
    const parsed = durationToSchedule(schedule?.scheduleInterval);
    const empty = parsed.value === "";
    scheduleValue.value = empty ? "1" : parsed.value;
    scheduleUnit.value = empty ? "hours" : parsed.unit;
    cronExpression.value = schedule?.cron ?? "";
    scheduleTimezone.value = schedule?.timezone || browserTimezone;
    scheduleMode.value = schedule?.cron ? "cron" : "interval";
    scheduleOpen.value = true;
  }

  function setScheduleMode(mode: TargetScheduleMode) {
    scheduleMode.value = mode === "cron" ? "cron" : "interval";
    scheduleError.value = "";
  }

  function setScheduleUnit(value: unknown) {
    scheduleUnit.value = String(value) as ScheduleUnit;
  }

  function setScheduleTimezone(value: unknown) {
    scheduleTimezone.value = String(value);
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
      const updated =
        scheduleMode.value === "cron"
          ? await updateQualityTargetSchedule({
              targetGuid: options.targetGuid.value,
              metaType: options.effectiveMetaType.value,
              cron: cronExpression.value.trim(),
              timezone: scheduleTimezone.value,
            })
          : await updateQualityTargetSchedule({
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
    scheduleMode,
    scheduleValue,
    scheduleUnit,
    cronExpression,
    scheduleTimezone,
    timezoneOptions,
    scheduleSaving,
    scheduleError,
    scheduleIssueKey,
    openScheduleEditor,
    setScheduleMode,
    setScheduleUnit,
    setScheduleTimezone,
    onScheduleOpenChange,
    saveSchedule,
  };
}
