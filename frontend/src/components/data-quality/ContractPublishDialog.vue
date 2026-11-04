<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>{{ t("dataQuality.contract.publishTitle") }}</DialogTitle>
        <DialogDescription>
          {{ t("dataQuality.contract.publishDescription") }}
        </DialogDescription>
      </DialogHeader>

      <div class="space-y-4">
        <Alert
          v-if="error"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>{{ t("dataQuality.contract.publishFailed") }}</AlertTitle>
          <AlertDescription class="break-words">
            {{ error }}
          </AlertDescription>
        </Alert>

        <!-- What the next version would record, counted from the live rules the
             contract was read with. Nothing to record means nothing to publish. -->
        <div class="flex flex-wrap items-center gap-2">
          <Badge variant="secondary">
            {{
              t("dataQuality.contract.changeAdded", { count: summary.added })
            }}
          </Badge>
          <Badge variant="secondary">
            {{
              t("dataQuality.contract.changeModified", {
                count: summary.modified,
              })
            }}
          </Badge>
          <Badge variant="secondary">
            {{
              t("dataQuality.contract.changeRemoved", { count: summary.removed })
            }}
          </Badge>
        </div>

        <!-- The version also records a plan declaration and the intent it was
             published with; show those sections next to the check counts. -->
        <div class="space-y-2">
          <div class="flex flex-wrap items-center gap-2">
            <Badge
              v-if="intentChanged"
              variant="secondary"
            >
              {{ t("dataQuality.contract.publishIntentChanged") }}
            </Badge>
            <Badge
              v-for="(change, index) in visiblePlanChanges"
              :key="`${change.kind}:${index}`"
              variant="secondary"
            >
              {{ planChangeLabel(change) }}
            </Badge>
            <span
              v-if="hiddenPlanChangeCount > 0"
              class="text-xs text-muted-foreground"
            >
              {{
                t("dataQuality.contract.publishPlanMore", {
                  count: hiddenPlanChangeCount,
                })
              }}
            </span>
          </div>
          <p
            v-if="planNotRecorded"
            class="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground"
          >
            {{ t("dataQuality.contract.publishPlanNotRecorded") }}
          </p>
          <p
            v-else-if="planChanges.length > 0"
            class="text-xs text-muted-foreground"
          >
            {{ t("dataQuality.contract.publishPlanHint") }}
          </p>
        </div>

        <p
          v-if="showNoChanges"
          class="rounded-md border border-dashed p-3 text-sm text-muted-foreground"
        >
          {{ t("dataQuality.contract.publishNoChanges") }}
        </p>

        <div class="space-y-1.5">
          <Label for="dq-contract-note">
            {{ t("dataQuality.contract.publishNote") }}
          </Label>
          <textarea
            id="dq-contract-note"
            v-model="note"
            maxlength="500"
            class="min-h-20 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
            :placeholder="t('dataQuality.contract.publishNotePlaceholder')"
          />
        </div>
      </div>

      <DialogFooter>
        <Button
          variant="outline"
          :disabled="publishing"
          @click="close"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          :disabled="!hasChanges || publishing"
          @click="publish"
        >
          {{
            t("dataQuality.contract.publishConfirm", { version: nextVersion })
          }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { AlertTriangle } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { publishQualityContract } from "@/api/data-quality";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import {
  type QualityContract,
  QualityCoverageProperty,
  type QualityPlanChange,
  QualityPlanChange_Kind,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";

/**
 * Publish the live rules as the target's next contract version.
 *
 * A publish records what the rules already are; it does not change them. The
 * server may answer that the live state already matches the newest version, in
 * which case no version is created and the dialog says so instead of pretending
 * a new promise was made.
 */

const props = defineProps<{
  open: boolean;
  targetGuid: string;
  metaType: MetaType;
  /** The contract as read with the workspace; the dialog shows its drift counts. */
  contract?: QualityContract;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  published: [contract: QualityContract];
}>();

const { t } = useI18n();
const { formatError } = useErrorMessage();

const note = ref("");
const error = ref("");
const publishing = ref(false);
const serverNoChanges = ref(false);

const hasChanges = computed(
  () => props.contract?.hasUnpublishedChanges ?? false
);

const showNoChanges = computed(
  () => !hasChanges.value || serverNoChanges.value
);

const nextVersion = computed(() => (props.contract?.publishedVersion ?? 0) + 1);

const summary = computed(() => ({
  added: props.contract?.changeSummary?.added ?? 0,
  modified: props.contract?.changeSummary?.modified ?? 0,
  removed: props.contract?.changeSummary?.removed ?? 0,
}));

const intentChanged = computed(
  () => props.contract?.diff?.intentChanged ?? false
);

const planChanges = computed<QualityPlanChange[]>(
  () => props.contract?.diff?.planChanges ?? []
);

const planNotRecorded = computed(
  () => props.contract?.diff?.planNotRecorded ?? false
);

/** The section shows the first few changes; the rest is a count. */
const maxPlanChanges = 6;
const visiblePlanChanges = computed(() =>
  planChanges.value.slice(0, maxPlanChanges)
);
const hiddenPlanChangeCount = computed(
  () => planChanges.value.length - visiblePlanChanges.value.length
);

function propertyLabel(property: QualityCoverageProperty): string {
  switch (property) {
    case QualityCoverageProperty.VOLUME:
      return t("dataQuality.workspace.planPropertyVolume");
    case QualityCoverageProperty.FRESHNESS:
      return t("dataQuality.workspace.planPropertyFreshness");
    case QualityCoverageProperty.NULL_RATE:
      return t("dataQuality.workspace.planPropertyNullRate");
    case QualityCoverageProperty.UNIQUENESS:
      return t("dataQuality.workspace.planPropertyUniqueness");
    case QualityCoverageProperty.ACCEPTED_VALUES:
      return t("dataQuality.workspace.planPropertyAcceptedValues");
    case QualityCoverageProperty.FORMAT:
      return t("dataQuality.workspace.planPropertyFormat");
    case QualityCoverageProperty.RANGE:
      return t("dataQuality.workspace.planPropertyRange");
    case QualityCoverageProperty.SCHEMA_STABILITY:
      return t("dataQuality.workspace.planPropertySchemaStability");
    default:
      return t("dataQuality.workspace.planPropertyUnspecified");
  }
}

function planChangeLabel(change: QualityPlanChange): string {
  const scope = change.coverage?.column
    ? `${propertyLabel(change.coverage.property)} · ${change.coverage.column}`
    : propertyLabel(
        change.coverage?.property ?? QualityCoverageProperty.UNSPECIFIED
      );
  switch (change.kind) {
    case QualityPlanChange_Kind.ADDED:
      return t("dataQuality.contract.publishPlanAdded", { scope });
    case QualityPlanChange_Kind.REMOVED:
      return t("dataQuality.contract.publishPlanRemoved", { scope });
    case QualityPlanChange_Kind.MODIFIED:
      return t("dataQuality.contract.publishPlanModified", { scope });
    default:
      return scope;
  }
}

watch(
  () => props.open,
  (isOpen) => {
    if (!isOpen) {
      return;
    }
    note.value = "";
    error.value = "";
    publishing.value = false;
    serverNoChanges.value = false;
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

async function publish() {
  if (!hasChanges.value || publishing.value) {
    return;
  }
  publishing.value = true;
  error.value = "";
  try {
    const response = await publishQualityContract({
      targetGuid: props.targetGuid,
      metaType: props.metaType,
      note: note.value.trim(),
    });
    if (response.noChanges) {
      serverNoChanges.value = true;
      return;
    }
    notify.success(t("dataQuality.contract.published"));
    if (response.contract) {
      emit("published", response.contract);
    }
    emit("update:open", false);
  } catch (e: unknown) {
    error.value = formatError(e, "dataQuality.contract.publishFailed");
  } finally {
    publishing.value = false;
  }
}
</script>
