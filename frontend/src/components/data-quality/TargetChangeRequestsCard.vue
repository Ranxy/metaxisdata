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
              t("dataQuality.changeRequests.pending", {
                count: pendingCount,
              })
            }}
          </Badge>
          <span class="text-xs text-muted-foreground">
            {{ t("dataQuality.changeRequests.total", { count: requests.length }) }}
          </span>
          <Button
            size="sm"
            variant="outline"
            :disabled="requests.length === 0"
            @click="dialogOpen = true"
          >
            {{ t("dataQuality.changeRequests.review") }}
          </Button>
        </div>
        <p
          v-if="requests.length === 0"
          class="text-xs text-muted-foreground"
        >
          {{ t("dataQuality.changeRequests.empty") }}
        </p>
      </template>
    </CardContent>

    <ChangeRequestsDialog
      v-model:open="dialogOpen"
      :target-guid="targetGuid"
      :meta-type="metaType"
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
import { MetaType } from "@/types/proto-es/v1/database_service_pb";

/**
 * The target's change requests: how many batches are waiting for a decision and the
 * entry to the review surface. A request changes nothing until someone decides it.
 */

const props = withDefaults(
  defineProps<{
    targetGuid: string;
    metaType: MetaType;
    canDecide?: boolean;
    canSuggest?: boolean;
  }>(),
  { canDecide: false, canSuggest: false }
);

const { t } = useI18n();

const requests = ref<QualityChangeRequest[]>([]);
const loading = ref(false);
const error = ref("");
const dialogOpen = ref(false);

const pendingCount = computed(
  () =>
    requests.value.filter(
      (request) =>
        request.state === QualityChangeRequestState.PROPOSED ||
        request.state === QualityChangeRequestState.APPROVED
    ).length
);

async function load() {
  const guid = props.targetGuid;
  loading.value = true;
  error.value = "";
  try {
    const page = await listQualityChangeRequests({
      targetGuid: guid,
      pageSize: 20,
    });
    if (guid !== props.targetGuid) {
      return;
    }
    requests.value = page.items;
  } catch {
    if (guid === props.targetGuid) {
      error.value = "failed";
      requests.value = [];
    }
  } finally {
    loading.value = false;
  }
}

watch(() => props.targetGuid, load);
onMounted(load);
</script>
