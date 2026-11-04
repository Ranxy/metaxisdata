<template>
  <Card>
    <CardHeader class="space-y-1 border-b">
      <CardTitle class="flex items-center gap-2 text-base">
        <ListChecks class="h-4 w-4 shrink-0 text-muted-foreground" />
        {{ t("dataQuality.changeRequests.title") }}
      </CardTitle>
      <CardDescription>
        {{ t("dataQuality.changeRequests.description") }}
      </CardDescription>
    </CardHeader>
    <CardContent class="space-y-3 pt-4">
      <div
        v-if="loading"
        class="flex items-center gap-2 text-xs text-muted-foreground"
      >
        <Loader2 class="h-3.5 w-3.5 animate-spin" />
      </div>
      <div
        v-else-if="error"
        class="flex flex-wrap items-center gap-2 text-sm"
      >
        <span class="text-destructive">
          {{ t("dataQuality.changeRequests.loadFailed") }}
        </span>
        <Button
          variant="ghost"
          size="sm"
          @click="load()"
        >
          {{ t("common.retry") }}
        </Button>
      </div>
      <template v-else>
        <div class="flex flex-wrap items-center gap-2">
          <Badge :variant="pendingCount > 0 ? 'warning' : 'neutral'">
            {{
              pendingCapped
                ? t("dataQuality.changeRequests.pendingAtLeast", { count: pendingCount })
                : t("dataQuality.changeRequests.pending", { count: pendingCount })
            }}
          </Badge>
          <Button
            size="sm"
            variant="outline"
            :disabled="pendingCount === 0 && !hasAny"
            @click="dialogOpen = true"
          >
            {{ t("dataQuality.changeRequests.review") }}
          </Button>
        </div>
        <p
          v-if="!hasAny"
          class="text-xs text-muted-foreground"
        >
          {{ t("dataQuality.changeRequests.empty") }}
        </p>
      </template>
    </CardContent>

    <ChangeRequestsDialog
      v-model:open="dialogOpen"
      :target-guid="targetGuid"
      :can-decide="canDecide"
      :can-suggest="canSuggest"
      @decided="load()"
    />
  </Card>
</template>

<script setup lang="ts">
import { ListChecks, Loader2 } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listQualityChangeRequests } from "@/api/data-quality";
import ChangeRequestsDialog from "@/components/data-quality/ChangeRequestsDialog.vue";
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
  type QualityChangeRequest,
  QualityChangeRequestState,
} from "@/types/proto-es/v1/data_quality_service_pb";

/**
 * The target's change requests: how many batches are waiting for a decision and the
 * entry to the review surface. A request changes nothing until someone decides it.
 */

const props = withDefaults(
  defineProps<{
    targetGuid: string;
    canDecide?: boolean;
    canSuggest?: boolean;
  }>(),
  { canDecide: false, canSuggest: false }
);

const { t } = useI18n();

const pending = ref<QualityChangeRequest[]>([]);
const recent = ref<QualityChangeRequest[]>([]);
const loading = ref(false);
const error = ref("");
const dialogOpen = ref(false);
let generation = 0;

const pendingCount = computed(() => pending.value.length);
const capped = ref(false);
const pendingCapped = computed(() => capped.value);
const hasAny = computed(
  () => pending.value.length > 0 || recent.value.length > 0
);

/**
 * The card counts what waits for a decision from the decision states themselves
 * rather than from a single "newest 20" page: a queue must not silently undercount
 * once a target has history.
 */
async function load() {
  const guid = props.targetGuid;
  const ticket = ++generation;
  loading.value = true;
  error.value = "";
  try {
    const [proposed, approved, all] = await Promise.all([
      listQualityChangeRequests({
        targetGuid: guid,
        state: QualityChangeRequestState.PROPOSED,
        pageSize: 50,
      }),
      listQualityChangeRequests({
        targetGuid: guid,
        state: QualityChangeRequestState.APPROVED,
        pageSize: 50,
      }),
      listQualityChangeRequests({ targetGuid: guid, pageSize: 20 }),
    ]);
    if (ticket !== generation || guid !== props.targetGuid) {
      return;
    }
    pending.value = [...proposed.items, ...approved.items];
    capped.value =
      proposed.nextPageToken !== "" || approved.nextPageToken !== "";
    recent.value = all.items;
  } catch {
    if (ticket === generation && guid === props.targetGuid) {
      error.value = "failed";
      pending.value = [];
      recent.value = [];
    }
  } finally {
    if (ticket === generation) {
      loading.value = false;
    }
  }
}

watch(() => props.targetGuid, load);
onMounted(load);
</script>
