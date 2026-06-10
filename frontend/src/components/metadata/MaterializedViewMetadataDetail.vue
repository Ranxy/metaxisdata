<template>
  <div class="p-4 space-y-6">
    <div class="space-y-1">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <div class="text-lg font-semibold wrap-break-word">
          {{ view.name }}
        </div>
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
          @click="$router.push({ path: `/explain-sql/${guid}`, query: { metaType: MetaType.MATERIALIZED_VIEW } })"
        >
          <Sparkles class="h-3.5 w-3.5 mr-1" />
          {{ t("explainSQL.explain") }}
        </Button>
      </div>
      <div class="text-sm text-muted-foreground">
        {{ summaryLine }}
      </div>
    </div>

    <div
      v-if="guid"
      class="inline-flex rounded-lg border bg-muted/30 p-1"
    >
      <button
        type="button"
        class="rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
        :class="activeTab === 'details' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'"
        @click="activeTab = 'details'"
      >
        {{ t("metadataBrowser.materializedViewDetail") }}
      </button>
      <button
        type="button"
        class="rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
        :class="activeTab === 'history' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'"
        @click="activeTab = 'history'"
      >
        {{ t("metadataBrowser.historyTitle") }}
      </button>
    </div>

    <template v-if="!guid || activeTab === 'details'">
    <TableLineageSection
      v-if="guid"
      :guid="guid"
      :meta-type="MetaType.MATERIALIZED_VIEW"
      :title="t('metadataBrowser.relatedLineageAnalysis')"
    />

    <div class="space-y-2">
      <div class="flex items-center justify-between">
        <div class="text-sm font-medium">{{ t("metadataBrowser.indexes") }}</div>
        <Badge variant="outline">
          {{ view.indexes.length }} {{ t("metadataBrowser.indexesCount") }}
        </Badge>
      </div>

      <Table v-if="view.indexes.length > 0">
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("metadataBrowser.indexName") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.indexType") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.expressions") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.unique") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.primary") }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="idx in view.indexes"
            :key="idx.name"
          >
            <TableCell class="font-medium">{{ idx.name }}</TableCell>
            <TableCell class="text-muted-foreground">{{ idx.type || "-" }}</TableCell>
            <TableCell class="text-muted-foreground max-w-md truncate">
              {{ idx.expressions.join(", ") || "-" }}
            </TableCell>
            <TableCell>
              <Badge :variant="idx.unique ? 'success' : 'secondary'">
                {{ idx.unique ? t("metadataBrowser.yes") : t("metadataBrowser.no") }}
              </Badge>
            </TableCell>
            <TableCell>
              <Badge :variant="idx.primary ? 'success' : 'secondary'">
                {{ idx.primary ? t("metadataBrowser.yes") : t("metadataBrowser.no") }}
              </Badge>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>

      <div
        v-else
        class="text-sm text-muted-foreground"
      >
        {{ t("metadataBrowser.noIndexes") }}
      </div>
    </div>

    <div
      v-if="view.triggers.length > 0"
      class="space-y-2"
    >
      <div class="flex items-center justify-between">
        <div class="text-sm font-medium">{{ t("metadataBrowser.triggers") }}</div>
        <Badge variant="outline">
          {{ view.triggers.length }} {{ t("metadataBrowser.triggersCount") }}
        </Badge>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("metadataBrowser.triggerName") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.event") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.timing") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.comment") }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="tr in view.triggers"
            :key="tr.name"
          >
            <TableCell class="font-medium">{{ tr.name }}</TableCell>
            <TableCell class="text-muted-foreground">{{ tr.event || "-" }}</TableCell>
            <TableCell class="text-muted-foreground">{{ tr.timing || "-" }}</TableCell>
            <TableCell class="text-muted-foreground max-w-md">
              <ExpandableText
                :text="tr.comment"
                :item-name="tr.name"
                :dialog-title="t('metadataBrowser.comment')"
              />
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
    </template>

    <MetadataHistorySection
      v-else
      :guid="guid"
      :meta-type="MetaType.MATERIALIZED_VIEW"
    />
  </div>
</template>

<script setup lang="ts">
import { Sparkles } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import MetadataHistorySection from "@/components/metadata/MetadataHistorySection.vue";
import TableLineageSection from "@/components/metadata/TableLineageSection.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  type MaterializedViewMetadata,
  MetaType,
} from "@/types/proto-es/v1/database_service_pb";
import ExpandableText from "./ExpandableText.vue";
import SchemaDefinitionDialog from "./SchemaDefinitionDialog.vue";

const props = defineProps<{
  view: MaterializedViewMetadata;
  guid?: string;
}>();

const { t } = useI18n();

const activeTab = ref<"details" | "history">("details");
const summaryLine = computed(() => {
  const parts: string[] = [];
  parts.push(`${props.view.indexes.length} ${t("metadataBrowser.indexes")}`);
  if (props.view.triggers.length > 0) {
    parts.push(
      `${props.view.triggers.length} ${t("metadataBrowser.triggers")}`
    );
  }
  return parts.join(" · ");
});
</script>
