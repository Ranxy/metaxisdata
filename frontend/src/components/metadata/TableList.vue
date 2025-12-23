<template>
  <div>
    <div
      v-if="tableRows.length === 0"
      class="p-8 text-center text-muted-foreground"
    >
      <Table2 class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
      <p>{{ t("metadataBrowser.noTables") }}</p>
    </div>

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.tableName") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.engine") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.rowCount") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.dataSize") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.columns") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.indexes") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.comment") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in tableRows"
          :key="row.value.name"
          class="cursor-pointer hover:bg-muted/50"
          @click="$emit('select', row.stored)"
        >
          <TableCell>
            <div class="flex items-center gap-2">
              <Table2 class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.name }}</span>
            </div>
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ row.value.engine || "-" }}
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ formatNumber(row.value.rowCount) }}
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ formatBytes(row.value.dataSize) }}
          </TableCell>
          <TableCell>
            <Badge variant="outline">
              {{ row.value.columns.length }} {{ t("metadataBrowser.columnsCount") }}
            </Badge>
          </TableCell>
          <TableCell>
            <Badge variant="outline">
              {{ row.value.indexes.length }} {{ t("metadataBrowser.indexesCount") }}
            </Badge>
          </TableCell>
          <TableCell class="text-muted-foreground max-w-xs truncate">
            {{ row.value.comment || "-" }}
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { Table2 } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type {
  StoredMetadata,
  TableMetadata,
} from "@/types/proto-es/v1/database_service_pb";

const props = defineProps<{
  items: StoredMetadata[];
}>();

defineEmits<{
  select: [item: StoredMetadata];
}>();

const { t } = useI18n();

const tableRows = computed(() => {
  const rows: Array<{ stored: StoredMetadata; value: TableMetadata }> = [];
  for (const stored of props.items) {
    if (stored.type.case === "tableMetadata") {
      rows.push({ stored, value: stored.type.value });
    }
  }
  return rows;
});

function formatNumber(value: bigint): string {
  return new Intl.NumberFormat().format(Number(value));
}

function formatBytes(bytes: bigint): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = Number(bytes);
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex++;
  }
  return `${size.toFixed(1)} ${units[unitIndex]}`;
}
</script>
