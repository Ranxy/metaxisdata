<template>
  <div class="p-4 space-y-6">
    <div class="space-y-1">
      <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <div class="text-lg font-semibold wrap-break-word">
          {{ seq.name }}
        </div>
        <div
          v-if="seq.comment"
          class="text-sm text-muted-foreground wrap-break-word"
        >
          {{ seq.comment }}
        </div>
      </div>
    </div>

    <div class="space-y-2">
      <div class="text-sm font-medium">{{ t("metadataBrowser.sequenceInfo") }}</div>
      <div class="flex flex-wrap gap-2">
        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.dataType") }}</div>
          <div class="text-sm font-medium">{{ seq.dataType || "-" }}</div>
        </div>

        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.start") }}</div>
          <div class="text-sm font-medium">{{ seq.start || "-" }}</div>
        </div>

        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.minValue") }}</div>
          <div class="text-sm font-medium">{{ seq.minValue || "-" }}</div>
        </div>

        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.maxValue") }}</div>
          <div class="text-sm font-medium">{{ seq.maxValue || "-" }}</div>
        </div>

        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.increment") }}</div>
          <div class="text-sm font-medium">{{ seq.increment || "-" }}</div>
        </div>

        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.cycle") }}</div>
          <div class="text-sm font-medium">
            <Badge :variant="seq.cycle ? 'success' : 'secondary'">
              {{ seq.cycle ? t("metadataBrowser.yes") : t("metadataBrowser.no") }}
            </Badge>
          </div>
        </div>

        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.cacheSize") }}</div>
          <div class="text-sm font-medium">{{ seq.cacheSize || "-" }}</div>
        </div>

        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.lastValue") }}</div>
          <div class="text-sm font-medium">{{ seq.lastValue || "-" }}</div>
        </div>

        <div
          v-if="seq.ownerTable || seq.ownerColumn"
          class="rounded-md border px-3 py-2"
        >
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.owner") }}</div>
          <div class="text-sm font-medium">
            {{ seq.ownerTable || "-" }}{{ seq.ownerColumn ? "." + seq.ownerColumn : "" }}
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import type { SequenceMetadata } from "@/types/proto-es/v1/database_service_pb";

defineProps<{ seq: SequenceMetadata }>();

const { t } = useI18n();
</script>
