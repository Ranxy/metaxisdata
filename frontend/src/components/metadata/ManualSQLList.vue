<template>
  <div>
    <div
      v-if="manualSQLRows.length === 0"
      class="p-8 text-center text-muted-foreground"
    >
      <FileCode2 class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
      <p>{{ t("metadataBrowser.noManualSqls") }}</p>
    </div>

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.manualSqlTitle") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.schemaName") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.comment") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in manualSQLRows"
          :key="row.value.manualSqlId"
          class="cursor-pointer hover:bg-muted/50"
          @click="$emit('select', row.stored)"
        >
          <TableCell>
            <div class="flex items-center gap-2">
              <FileCode2 class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.title || row.value.name }}</span>
            </div>
            <div class="text-xs text-muted-foreground mt-1">
              {{ row.value.name }}
            </div>
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ row.value.schemaName || t("metadataBrowser.defaultSchema") }}
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ row.value.comment || "-" }}
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { FileCode2 } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type {
  ManualSQLMetadata,
  StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";

const props = defineProps<{
  items: StoredMetadata[];
}>();

defineEmits<{
  select: [item: StoredMetadata];
}>();

const { t } = useI18n();

const manualSQLRows = computed(() => {
  const rows: Array<{ stored: StoredMetadata; value: ManualSQLMetadata }> = [];
  for (const stored of props.items) {
    if (stored.type.case === "manualSqlMetadata") {
      rows.push({ stored, value: stored.type.value });
    }
  }
  return rows;
});
</script>