<template>
  <div
    v-if="canUpdate"
    class="flex flex-wrap items-center gap-1"
  >
    <Button
      v-if="canPause"
      variant="ghost"
      size="sm"
      :disabled="busy"
      @click.stop="pause"
    >
      <Pause class="mr-1.5 h-3.5 w-3.5" />
      {{ t("dataQuality.agent.pause") }}
    </Button>
    <Button
      v-if="canResume"
      variant="ghost"
      size="sm"
      :disabled="busy"
      @click.stop="resume"
    >
      <Play class="mr-1.5 h-3.5 w-3.5" />
      {{ t("dataQuality.agent.resume") }}
    </Button>
    <Button
      v-if="!archived"
      variant="ghost"
      size="sm"
      class="text-destructive hover:text-destructive"
      :disabled="busy"
      @click.stop="archiveOpen = true"
    >
      <Archive class="mr-1.5 h-3.5 w-3.5" />
      {{ t("dataQuality.agent.archive") }}
    </Button>

    <ConfirmDeleteDialog
      v-model="archiveOpen"
      :title="t('dataQuality.agent.archiveTitle')"
      :message="t('dataQuality.agent.archiveDescription')"
      :item-name="itemName"
      :confirm-label="t('dataQuality.agent.archive')"
      :loading="busy"
      @confirm="archive"
    />
  </div>
</template>

<script setup lang="ts">
import { Archive, Pause, Play } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  archiveQualityAgentSession,
  pauseQualityAgentSession,
  resumeQualityAgentSession,
} from "@/api/data-quality";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import { Button } from "@/components/ui/button";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import {
  type QualityAgentSession,
  QualityAgentSessionState,
} from "@/types/proto-es/v1/data_quality_service_pb";

/**
 * The human controls over one session: pause it, resume it, or archive it.
 *
 * Every mutation carries the revision the caller saw, so the server rejects a
 * change made against a session the runner has moved on from; the page then
 * reloads instead of retrying with a stale revision. Archive is destructive
 * enough to confirm, because an archived session cannot be unarchived.
 */

const props = defineProps<{
  session: QualityAgentSession;
  canUpdate: boolean;
}>();

const emit = defineEmits<{
  updated: [session: QualityAgentSession];
  archived: [];
  /** The row moved under us; the page re-reads it instead of retrying. */
  reload: [];
}>();

const { t } = useI18n();
const { formatError } = useErrorMessage();

const busy = ref(false);
const archiveOpen = ref(false);

const archived = computed(
  () => props.session.state === QualityAgentSessionState.ARCHIVED
);
const canPause = computed(
  () =>
    !archived.value && props.session.state !== QualityAgentSessionState.PAUSED
);
const canResume = computed(
  () =>
    !archived.value &&
    (props.session.state === QualityAgentSessionState.PAUSED ||
      props.session.state === QualityAgentSessionState.BLOCKED)
);

const itemName = computed(
  () =>
    props.session.target?.path?.object ||
    props.session.target?.metaGuid ||
    props.session.name
);

async function pause() {
  const updated = await apply(() =>
    pauseQualityAgentSession(props.session.name, props.session.revision)
  );
  if (updated) {
    notify.success(t("dataQuality.agent.paused"));
  }
}

async function resume() {
  const updated = await apply(() =>
    resumeQualityAgentSession(props.session.name, props.session.revision)
  );
  if (updated) {
    notify.success(t("dataQuality.agent.resumed"));
  }
}

/** Run one mutation; `undefined` means it failed and the page must re-read. */
async function apply(
  action: () => Promise<QualityAgentSession>
): Promise<QualityAgentSession | undefined> {
  if (busy.value) {
    return undefined;
  }
  busy.value = true;
  try {
    const updated = await action();
    emit("updated", updated);
    return updated;
  } catch (e: unknown) {
    notify.error(formatError(e, "dataQuality.agent.actionFailed"));
    // A stale revision means the runner changed the session first, so the page
    // re-reads the row rather than retrying with the revision it already lost.
    emit("reload");
    return undefined;
  } finally {
    busy.value = false;
  }
}

async function archive() {
  if (busy.value) {
    return;
  }
  busy.value = true;
  try {
    await archiveQualityAgentSession(
      props.session.name,
      props.session.revision
    );
    archiveOpen.value = false;
    notify.success(t("dataQuality.agent.archived"));
    emit("archived");
  } catch (e: unknown) {
    archiveOpen.value = false;
    notify.error(formatError(e, "dataQuality.agent.archiveFailed"));
    emit("reload");
  } finally {
    busy.value = false;
  }
}
</script>
