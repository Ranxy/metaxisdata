<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent class="max-h-[90vh] gap-0 overflow-hidden p-0 sm:max-w-2xl">
      <div class="flex max-h-[90vh] min-h-0 flex-col">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>
            {{ t("dataQuality.contract.settings") }}
          </DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.contract.settingsDescription") }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-6 overflow-auto px-6 py-5">
          <Alert
            v-if="error"
            variant="destructive"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>{{ t("dataQuality.contract.settingsFailed") }}</AlertTitle>
            <AlertDescription class="break-words">
              {{ error }}
            </AlertDescription>
          </Alert>

          <!-- The filter is the target-level layer's only rule-affecting setting:
               it is ANDed into every rule on the target at compile time. -->
          <section class="space-y-3">
            <div class="space-y-1">
              <Label>{{ t("dataQuality.contract.settingsFilter") }}</Label>
              <p class="text-sm text-muted-foreground">
                {{ t("dataQuality.contract.settingsFilterHint") }}
              </p>
            </div>
            <RuleFilterEditor
              v-model="clauses"
              :columns="columns"
            />
          </section>

          <section class="grid gap-4 border-t pt-4 sm:grid-cols-2">
            <div class="flex flex-col gap-1.5">
              <Label for="dq-contract-schedule">
                {{ t("dataQuality.contract.settingsSchedule") }}
              </Label>
              <div class="flex gap-2">
                <FormField
                  id="dq-contract-schedule"
                  v-model="schedule.value"
                  type="number"
                  class="min-w-0 flex-1"
                />
                <Select
                  :model-value="schedule.unit"
                  @update:model-value="setScheduleUnit"
                >
                  <SelectTrigger
                    id="dq-contract-schedule-unit"
                    class="w-32"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="minutes">
                      {{ t("dataQuality.editor.unitMinutes") }}
                    </SelectItem>
                    <SelectItem value="hours">
                      {{ t("dataQuality.editor.unitHours") }}
                    </SelectItem>
                    <SelectItem value="days">
                      {{ t("dataQuality.editor.unitDays") }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <p class="text-sm text-muted-foreground">
                {{ t("dataQuality.contract.settingsScheduleHint") }}
              </p>
            </div>

            <div class="flex flex-col gap-1.5">
              <Label for="dq-contract-severity">
                {{ t("dataQuality.contract.settingsSeverity") }}
              </Label>
              <Select
                :model-value="String(severity)"
                @update:model-value="setSeverity"
              >
                <SelectTrigger id="dq-contract-severity">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="option in severityOptions"
                    :key="option.value"
                    :value="String(option.value)"
                  >
                    {{ t(option.key) }}
                  </SelectItem>
                </SelectContent>
              </Select>
              <p class="text-sm text-muted-foreground">
                {{ t("dataQuality.contract.settingsSeverityHint") }}
              </p>
            </div>
          </section>
        </div>

        <DialogFooter class="shrink-0 border-t px-6 py-4">
          <Button
            variant="outline"
            :disabled="saving"
            @click="close"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="saving"
            @click="save"
          >
            {{ t("dataQuality.contract.saveSettings") }}
          </Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { AlertTriangle } from "lucide-vue-next";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { updateQualityContract } from "@/api/data-quality";
import { listAllMetadata } from "@/api/database";
import RuleFilterEditor from "@/components/data-quality/RuleFilterEditor.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import FormField from "@/components/ui/form-field/FormField.vue";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import {
  type QualityContract,
  type QualityContractSettings,
  QualityContractSettingsSchema,
  type QualityFilter,
  QualitySeverity,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { qualitySeverityKey } from "@/utils/dataQuality";
import {
  buildFilter,
  durationToSchedule,
  type FilterClauseForm,
  type ScheduleUnit,
  scheduleToDuration,
  type TargetColumnDetail,
} from "@/utils/dataQualityForm";

/**
 * The target-level defaults: one filter ANDed into every rule, and the schedule
 * and severity a newly authored rule starts with.
 *
 * The server replaces the whole settings message, so this dialog always sends a
 * complete one — a cleared field is a cleared default, not an untouched one.
 * Existing rules keep their own schedule and severity; only the filter changes
 * what they judge.
 */

const props = defineProps<{
  open: boolean;
  targetGuid: string;
  metaType: MetaType;
  settings?: QualityContractSettings;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  updated: [contract: QualityContract];
}>();

const { t } = useI18n();
const { formatError } = useErrorMessage();

const clauses = ref<FilterClauseForm[]>([]);
const schedule = ref<{ value: string; unit: ScheduleUnit }>({
  value: "",
  unit: "hours",
});
const severity = ref<QualitySeverity>(QualitySeverity.UNSPECIFIED);
const columns = ref<TargetColumnDetail[]>([]);
const error = ref("");
const saving = ref(false);

const severityOptions = [
  QualitySeverity.UNSPECIFIED,
  QualitySeverity.WARNING,
  QualitySeverity.ERROR,
  QualitySeverity.CRITICAL,
].map((value) => ({ value, key: qualitySeverityKey(value) }));

/** The stored filter as the editor's clause rows. */
function clausesFromFilter(
  filter: QualityFilter | undefined
): FilterClauseForm[] {
  return (filter?.clauses ?? []).map((clause) => ({
    column: clause.column,
    op: clause.op,
    values: clause.values.join(", "),
  }));
}

function reset() {
  const settings = props.settings;
  clauses.value = clausesFromFilter(settings?.filter);
  schedule.value = durationToSchedule(
    create(DurationSchema, {
      seconds: settings?.defaultScheduleIntervalSeconds ?? 0n,
      nanos: 0,
    })
  );
  severity.value = settings?.defaultSeverity ?? QualitySeverity.UNSPECIFIED;
  columns.value = [];
  error.value = "";
  saving.value = false;
}

function setScheduleUnit(value: unknown) {
  schedule.value.unit = String(value) as ScheduleUnit;
}

function setSeverity(value: unknown) {
  severity.value = Number(value) as QualitySeverity;
}

/**
 * Column suggestions for the filter's column picker; a failure is not fatal
 * because the picker also accepts a typed name.
 */
async function loadColumns() {
  const guid = props.targetGuid;
  try {
    const items = await listAllMetadata({
      parentGuid: guid,
      metaType: MetaType.COLUMN,
    });
    if (!props.open || guid !== props.targetGuid) {
      return;
    }
    columns.value = items.map((item) => {
      const value = item.type.value as
        | { name?: string; type?: string; nullable?: boolean }
        | undefined;
      return {
        name: value?.name ?? item.guid,
        type: value?.type ?? "",
        nullable: value?.nullable ?? true,
      };
    });
  } catch {
    if (props.open && guid === props.targetGuid) {
      columns.value = [];
    }
  }
}

watch(
  () => props.open,
  (isOpen) => {
    reset();
    if (isOpen) {
      void loadColumns();
    }
  },
  { immediate: true }
);

function close() {
  emit("update:open", false);
}

function onOpenChange(value: boolean) {
  if (value) {
    return;
  }
  close();
}

async function save() {
  if (saving.value) {
    return;
  }
  saving.value = true;
  error.value = "";
  try {
    const settings = create(QualityContractSettingsSchema, {
      filter: buildFilter(clauses.value),
      defaultScheduleIntervalSeconds: scheduleToDuration(schedule.value)
        .seconds,
      defaultSeverity: severity.value,
    });
    const updated = await updateQualityContract({
      targetGuid: props.targetGuid,
      metaType: props.metaType,
      settings,
    });
    notify.success(t("dataQuality.contract.settingsSaved"));
    emit("updated", updated);
    emit("update:open", false);
  } catch (e: unknown) {
    error.value = formatError(e, "dataQuality.contract.settingsFailed");
  } finally {
    saving.value = false;
  }
}
</script>
