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
import type { QualityContract } from "@/types/proto-es/v1/data_quality_service_pb";
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
