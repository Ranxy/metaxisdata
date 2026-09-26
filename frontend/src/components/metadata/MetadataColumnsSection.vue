<template>
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
          {{ filteredColumns.length }} / {{ columns.length }}
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
          :ref="(el) => rowRef?.(col.name, el as Element | ComponentPublicInstance | null)"
          :class="{ 'bg-accent/50': selectedColumnName === col.name }"
        >
          <TableCell class="font-medium">{{ col.name }}</TableCell>
          <TableCell class="text-muted-foreground">
            {{ col.type || "-" }}
          </TableCell>
          <TableCell>
            <Badge
              :variant="col.nullable ? 'secondary' : 'success'"
              class="whitespace-nowrap"
            >
              {{ col.nullable ? t("metadataBrowser.yes") : t("metadataBrowser.no") }}
            </Badge>
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ col.default || "-" }}
          </TableCell>
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

<script setup lang="ts">
import { type ComponentPublicInstance, computed, ref } from "vue";
import { useI18n } from "vue-i18n";
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
import type { ColumnMetadata } from "@/types/proto-es/v1/database_service_pb";
import ExpandableText from "./ExpandableText.vue";

/**
 * A metadata object's columns: the search box, the count and the table. The four
 * details that show a column list used to carry their own copy, and only the
 * table detail matched comments in the search — matching both here makes the same
 * query behave the same everywhere.
 */
const props = defineProps<{
  columns: ColumnMetadata[];
  /** Highlighted row, used by the table detail when a column is deep-linked. */
  selectedColumnName?: string | null;
  /** Receives each row element, which the table detail scrolls into view. */
  rowRef?: (
    column: string,
    element: Element | ComponentPublicInstance | null
  ) => void;
}>();

const { t } = useI18n();
const columnSearch = ref("");

const filteredColumns = computed((): ColumnMetadata[] => {
  const q = columnSearch.value.trim().toLowerCase();
  if (!q) return props.columns;
  return props.columns.filter(
    (column) =>
      column.name.toLowerCase().includes(q) ||
      (column.userComment || column.comment).toLowerCase().includes(q)
  );
});
</script>
