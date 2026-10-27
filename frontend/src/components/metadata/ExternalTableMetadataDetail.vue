<template>
  <div class="p-4 space-y-4">
    <div class="space-y-1">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <h1 class="text-2xl font-semibold tracking-tight wrap-break-word">
          {{ table.name }}
        </h1>
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

    <MetadataQualitySection
      v-if="activeTab === 'quality' && guid"
      :table-guid="guid"
      :meta-type="MetaType.EXTERNAL_TABLE"
      :columns="table.columns"
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
      v-else-if="activeTab === 'history' && guid"
      :guid="guid"
      :meta-type="MetaType.EXTERNAL_TABLE"
    />
  </div>
</template>

<script setup lang="ts">
import { Plug } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import MetadataQualitySection from "@/components/data-quality/MetadataQualitySection.vue";
import MetadataColumnsSection from "@/components/metadata/MetadataColumnsSection.vue";
import MetadataHistorySection from "@/components/metadata/MetadataHistorySection.vue";
import MetadataTabGroup from "@/components/metadata/MetadataTabGroup.vue";
import TableLineageSection from "@/components/metadata/TableLineageSection.vue";
import { Badge } from "@/components/ui/badge";
import { useAuthStore } from "@/store/modules/auth";
import {
  type ExternalTableMetadata,
  MetaType,
} from "@/types/proto-es/v1/database_service_pb";

const props = defineProps<{
  table: ExternalTableMetadata;
  guid?: string;
}>();

const { t } = useI18n();
const authStore = useAuthStore();

type DetailTab = "details" | "quality" | "history";

const activeTab = ref<DetailTab>("details");

/** The quality tab exists only for a caller who can read the rule catalog. */
function hasQualityPermission(): boolean {
  return authStore.hasPermission("metaxisdata.dataQuality.rules.list");
}

const tabs = computed(() => {
  const list: Array<{ value: DetailTab; label: string }> = [
    {
      value: "details",
      label: t("metadataBrowser.externalTableDetail"),
    },
  ];
  if (props.guid && hasQualityPermission()) {
    list.push({ value: "quality", label: t("dataQuality.asset.tab") });
  }
  list.push({ value: "history", label: t("metadataBrowser.historyTitle") });
  return list;
});

const summaryLine = computed(() => {
  return `${props.table.columns.length} ${t("metadataBrowser.columnsCount")}`;
});
</script>
