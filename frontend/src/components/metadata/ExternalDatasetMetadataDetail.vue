<template>
  <div class="p-4 space-y-6">
    <div class="space-y-1">
      <div class="flex flex-wrap items-center gap-3">
        <div class="text-lg font-semibold wrap-break-word">
          {{ displayName }}
        </div>
        <Badge variant="secondary">
          {{ datasetType || t("metadataBrowser.unknown") }}
        </Badge>
      </div>
      <div class="text-sm text-muted-foreground break-all">
        {{ guid }}
      </div>
    </div>

    <div class="space-y-2">
      <div class="text-sm font-medium">{{ t("metadataBrowser.externalDatasetInfo") }}</div>
      <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
        <div class="rounded-md border px-3 py-2 sm:col-span-2 xl:col-span-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.namespace") }}</div>
          <div class="text-sm font-medium break-all">{{ namespace || "-" }}</div>
        </div>
        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.datasetType") }}</div>
          <div class="text-sm font-medium">{{ datasetType || "-" }}</div>
        </div>
        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.relatedObject") }}</div>
          <div class="text-sm font-medium break-all">{{ name || "-" }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";

const props = defineProps<{
  guid: string;
  name: string;
  namespace: string;
  datasetType: string;
}>();

const { t } = useI18n();

const displayName = computed(() => {
  if (props.name) {
    return props.name;
  }
  if (props.namespace) {
    return props.namespace;
  }
  return props.guid;
});
</script>