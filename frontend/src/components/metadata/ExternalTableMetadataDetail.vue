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
        {{ t("metadataBrowser.externalTableDetail") }}
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
      :meta-type="MetaType.EXTERNAL_TABLE"
      :title="t('metadataBrowser.relatedLineageAnalysis')"
    />

    <div class="space-y-2">
      <div class="flex items-center justify-between">
        <h2 class="text-sm font-medium">{{ t("metadataBrowser.columns") }}</h2>
        <div class="flex items-center gap-2">
          <Input
            v-model="columnSearch"
            class="h-9 w-64"
            :placeholder="t('metadataBrowser.searchColumnsPlaceholder')"
          />
          <Badge variant="outline">
            {{ filteredColumns.length }} / {{ table.columns.length }}
            {{ t("metadataBrowser.columnsCount") }}
          </Badge>
        </div>
      </div>

      <Table v-if="filteredColumns.length > 0">
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("metadataBrowser.columnName") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.columnType") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.nullable") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.defaultValue") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.comment") }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="col in filteredColumns"
            :key="`${col.position}:${col.name}`"
          >
            <TableCell class="font-medium">{{ col.name }}</TableCell>
            <TableCell class="text-muted-foreground">{{ col.type || "-" }}</TableCell>
            <TableCell>
              <Badge
                :variant="col.nullable ? 'secondary' : 'success'"
                class="whitespace-nowrap"
              >
                {{ col.nullable ? t("metadataBrowser.yes") : t("metadataBrowser.no") }}
              </Badge>
            </TableCell>
            <TableCell class="text-muted-foreground">{{ col.default || "-" }}</TableCell>
            <TableCell class="text-muted-foreground max-w-md">
              <ExpandableText
                :text="col.userComment || col.comment"
                :item-name="col.name"
                :dialog-title="t('metadataBrowser.comment')"
              />
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>

      <div
        v-else
        class="text-sm text-muted-foreground"
      >
        {{ columnSearch ? t("metadataBrowser.noMatchedColumns") : t("metadataBrowser.noColumns") }}
      </div>
    </div>
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
import MetadataHistorySection from "@/components/metadata/MetadataHistorySection.vue";
import TableLineageSection from "@/components/metadata/TableLineageSection.vue";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  type ColumnMetadata,
  type ExternalTableMetadata,
  MetaType,
} from "@/types/proto-es/v1/database_service_pb";
import ExpandableText from "./ExpandableText.vue";

const props = defineProps<{
  table: ExternalTableMetadata;
  guid?: string;
}>();

const { t } = useI18n();

const activeTab = ref<"details" | "history">("details");
const columnSearch = ref("");

const filteredColumns = computed((): ColumnMetadata[] => {
  const q = columnSearch.value.trim().toLowerCase();
  if (!q) return props.table.columns;
  return props.table.columns.filter((c) => c.name.toLowerCase().includes(q));
});

const summaryLine = computed(() => {
  return `${props.table.columns.length} ${t("metadataBrowser.columns")}`;
});
</script>
