<template>
  <div class="p-4 space-y-4">
    <div class="space-y-1">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <div class="text-lg font-semibold wrap-break-word">
          {{ table.name }}
        </div>
        <Badge
          v-if="table.externalServerName"
          variant="outline"
          class="gap-1"
        >
          <Plug class="h-3 w-3" />
          {{ table.externalServerName }}
        </Badge>
        <Badge
          v-if="table.externalDatabaseName"
          variant="secondary"
        >
          {{ table.externalDatabaseName }}
        </Badge>
      </div>
      <div class="text-sm text-muted-foreground">
        {{ summaryLine }}
      </div>
    </div>

    <MetadataTabGroup
      v-if="guid"
      v-model="activeTab"
      :tabs="tabs"
    />

    <template v-if="!guid || activeTab === 'details'">
    <TableLineageSection
      v-if="guid"
      :guid="guid"
      :meta-type="MetaType.EXTERNAL_TABLE"
      :title="t('metadataBrowser.relatedLineageAnalysis')"
    />

    <MetadataColumnsSection :columns="table.columns" />
    </template>

    <MetadataHistorySection
      v-else
      :guid="guid"
      :meta-type="MetaType.EXTERNAL_TABLE"
    />
  </div>
</template>

<script setup lang="ts">
import { Plug } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import MetadataColumnsSection from "@/components/metadata/MetadataColumnsSection.vue";
import MetadataHistorySection from "@/components/metadata/MetadataHistorySection.vue";
import MetadataTabGroup from "@/components/metadata/MetadataTabGroup.vue";
import TableLineageSection from "@/components/metadata/TableLineageSection.vue";
import { Badge } from "@/components/ui/badge";
import {
  type ExternalTableMetadata,
  MetaType,
} from "@/types/proto-es/v1/database_service_pb";

const props = defineProps<{
  table: ExternalTableMetadata;
  guid?: string;
}>();

const { t } = useI18n();

const activeTab = ref<"details" | "history">("details");

const tabs = computed(() => [
  {
    value: "details" as const,
    label: t("metadataBrowser.externalTableDetail"),
  },
  { value: "history" as const, label: t("metadataBrowser.historyTitle") },
]);

const summaryLine = computed(() => {
  return `${props.table.columns.length} ${t("metadataBrowser.columns")}`;
});
</script>
