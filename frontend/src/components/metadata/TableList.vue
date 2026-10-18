<template>
  <div>
    <EmptyState
      v-if="tableRows.length === 0"
      :icon="Table2"
      :title="t('metadataBrowser.noTables')"
    >
        <Button
          variant="outline"
          size="sm"
          @click="$emit('refresh')"
        >
          <RefreshCw class="mr-1 h-3.5 w-3.5" />
          {{ t("common.refresh") }}
        </Button>
      </EmptyState>

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.tableName") }}</TableHead>
          <TableHead v-if="hasEngine" class="hidden md:table-cell">{{ t("metadataBrowser.engine") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.rowCount") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.dataSize") }}</TableHead>
          <TableHead class="hidden sm:table-cell">{{ t("metadataBrowser.columns") }}</TableHead>
          <TableHead class="hidden sm:table-cell">{{ t("metadataBrowser.indexes") }}</TableHead>
          <TableHead v-if="hasComment" class="hidden md:table-cell">{{ t("metadataBrowser.comment") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in tableRows"
          :key="row.value.name"
          class="cursor-pointer hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring focus-visible:outline-none"
          role="button"
          tabindex="0"
          @click="$emit('select', row.stored)"
          @keydown.enter.self.prevent="$emit('select', row.stored)"
          @keydown.space.self.prevent="$emit('select', row.stored)"
        >
          <TableCell>
            <div class="flex items-center gap-2">
              <Table2 class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.name }}</span>
            </div>
          </TableCell>
          <TableCell v-if="hasEngine" class="hidden text-muted-foreground md:table-cell">
            {{ row.value.engine || "-" }}
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ formatNumber(row.value.rowCount) }}
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ formatBytes(row.value.dataSize) }}
          </TableCell>
          <TableCell class="hidden sm:table-cell">
            <Badge variant="outline">
              {{ row.value.columns.length }} {{ t("metadataBrowser.columnsCount") }}
            </Badge>
          </TableCell>
          <TableCell class="hidden sm:table-cell">
            <Badge variant="outline">
              {{ row.value.indexes.length }} {{ t("metadataBrowser.indexesCount") }}
            </Badge>
          </TableCell>
          <TableCell v-if="hasComment" class="hidden max-w-xs text-muted-foreground md:table-cell">
            <ExpandableText
              :text="row.value.comment"
              :item-name="row.value.name"
              :dialog-title="t('metadataBrowser.comment')"
            />
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { RefreshCw, Table2 } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import EmptyState from "@/components/common/EmptyState.vue";
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
import type {
  StoredMetadata,
  TableMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import { formatBytes, formatNumber } from "@/utils/format";
import ExpandableText from "./ExpandableText.vue";

const props = defineProps<{
  items: StoredMetadata[];
}>();

defineEmits<{
  select: [item: StoredMetadata];
  refresh: [];
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

// An all-empty column costs a column of dashes and reads as missing data.
const hasEngine = computed(() =>
  tableRows.value.some((row) => row.value.engine !== "")
);
const hasComment = computed(() =>
  tableRows.value.some((row) => row.value.comment !== "")
);
</script>
