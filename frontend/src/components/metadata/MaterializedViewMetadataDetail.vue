<template>
  <div class="p-4 space-y-4">
    <div class="space-y-1">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <h1 class="text-2xl font-semibold tracking-tight wrap-break-word">
          {{ view.name }}
        </h1>
        <div
          v-if="view.comment"
          class="text-sm text-muted-foreground wrap-break-word max-w-xl"
        >
          <ExpandableText
            :text="view.comment"
            :item-name="view.name"
            :dialog-title="t('metadataBrowser.comment')"
          />
        </div>
        <SchemaDefinitionDialog
          v-if="guid"
          :guid="guid"
          :meta-type="MetaType.MATERIALIZED_VIEW"
          :object-name="view.name"
        />
        <Button
          v-if="guid"
          variant="outline"
          size="sm"
          @click="$router.push({ name: 'ExplainSQLWithGuid', params: { guid: guidToRouteParams(guid ?? '') }, query: { metaType: MetaType.MATERIALIZED_VIEW } })"
        >
          <Sparkles class="h-3.5 w-3.5 mr-1" />
          {{ t("explainSQL.explain") }}
        </Button>
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
      :meta-type="MetaType.MATERIALIZED_VIEW"
      :columns="view.columns"
    />

    <template v-if="!guid || activeTab === 'details'">
    <TableLineageSection
      v-if="guid"
      :guid="guid"
      :meta-type="MetaType.MATERIALIZED_VIEW"
      :title="t('metadataBrowser.relatedLineageAnalysis')"
    />

    <MetadataColumnsSection :columns="view.columns" />
    </template>

    <MetadataHistorySection
      v-else-if="activeTab === 'history' && guid"
      :guid="guid"
      :meta-type="MetaType.MATERIALIZED_VIEW"
    />
  </div>
</template>

<script setup lang="ts">
import { Sparkles } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import MetadataQualitySection from "@/components/data-quality/MetadataQualitySection.vue";
import MetadataColumnsSection from "@/components/metadata/MetadataColumnsSection.vue";
import MetadataHistorySection from "@/components/metadata/MetadataHistorySection.vue";
import MetadataTabGroup from "@/components/metadata/MetadataTabGroup.vue";
import TableLineageSection from "@/components/metadata/TableLineageSection.vue";
import { Button } from "@/components/ui/button";
import { useAuthStore } from "@/store/modules/auth";
import {
  type MaterializedViewMetadata,
  MetaType,
} from "@/types/proto-es/v1/database_service_pb";
import { guidToRouteParams } from "@/utils/guid";
import ExpandableText from "./ExpandableText.vue";
import SchemaDefinitionDialog from "./SchemaDefinitionDialog.vue";

const props = defineProps<{
  view: MaterializedViewMetadata;
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
      label: t("metadataBrowser.materializedViewDetail"),
    },
  ];
  if (props.guid && hasQualityPermission()) {
    list.push({ value: "quality", label: t("dataQuality.asset.tab") });
  }
  list.push({ value: "history", label: t("metadataBrowser.historyTitle") });
  return list;
});

const summaryLine = computed(() => {
  const parts: string[] = [];
  parts.push(`${props.view.columns.length} ${t("metadataBrowser.columns")}`);
  parts.push(`${props.view.indexes.length} ${t("metadataBrowser.indexes")}`);
  return parts.join(" · ");
});
</script>
