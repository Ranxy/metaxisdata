<template>
  <div class="p-4 space-y-4">
    <div class="space-y-1">
      <div class="flex flex-wrap items-center gap-3">
        <h1 class="text-2xl font-semibold tracking-tight wrap-break-word">
          {{ displayName }}
        </h1>
        <Badge variant="secondary">
          {{ datasetType || t("metadataBrowser.unknown") }}
        </Badge>
      </div>
      <!-- The GUID is the external dataset's internal name; the qualified label
           reads as namespace · name. -->
      <div class="break-all text-sm text-muted-foreground">
        {{ assetLabel }}
      </div>
    </div>

    <div class="space-y-2">
      <h2 class="text-sm font-medium">{{ t("metadataBrowser.externalDatasetInfo") }}</h2>
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
import { lineageAssetView } from "@/utils/lineageAsset";

const props = defineProps<{
  guid: string;
  name: string;
  namespace: string;
  datasetType: string;
}>();

const { t } = useI18n();

const assetLabel = computed(
  () => lineageAssetView(props.guid, new Map()).fullLabel
);

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