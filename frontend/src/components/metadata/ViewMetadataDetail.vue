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
          :meta-type="MetaType.VIEW"
          :object-name="view.name"
        />
      </div>
      <div class="text-sm text-muted-foreground">
        {{ summaryLine }}
      </div>
    </div>

    <div class="space-y-2">
      <div class="text-sm font-medium">{{ t("metadataBrowser.definition") }}</div>
      <DefinitionMonacoViewer :content="view.definition" />
    </div>

    <ContextLineageSection
      v-if="guid"
      :guid="guid"
      :meta-type="MetaType.VIEW"
    />

    <div class="space-y-2">
      <div class="flex items-center justify-between">
        <div class="text-sm font-medium">{{ t("metadataBrowser.columns") }}</div>
        <div class="flex items-center gap-2">
          <Input
            v-model="columnSearch"
            class="h-9 w-64"
            :placeholder="t('metadataBrowser.searchColumnsPlaceholder')"
          />
          <Badge variant="outline">
            {{ filteredColumns.length }} / {{ view.columns.length }}
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
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import ContextLineageSection from "@/components/metadata/ContextLineageSection.vue";
import DefinitionMonacoViewer from "@/components/metadata/DefinitionMonacoViewer.vue";
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
  MetaType,
  type ViewMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import ExpandableText from "./ExpandableText.vue";
import SchemaDefinitionDialog from "./SchemaDefinitionDialog.vue";

const props = defineProps<{
  view: ViewMetadata;
  guid?: string;
}>();

const { t } = useI18n();

const columnSearch = ref("");

const filteredColumns = computed((): ColumnMetadata[] => {
  const q = columnSearch.value.trim().toLowerCase();
  if (!q) return props.view.columns;
  return props.view.columns.filter((c) => c.name.toLowerCase().includes(q));
});

const summaryLine = computed(() => {
  const parts: string[] = [];
  parts.push(`${props.view.columns.length} ${t("metadataBrowser.columns")}`);
  return parts.join(" · ");
});
</script>
