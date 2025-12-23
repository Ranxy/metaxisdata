<template>
  <div>
    <div
      v-if="databaseRows.length === 0"
      class="p-8 text-center text-muted-foreground"
    >
      <Database class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
      <p>{{ t("metadataBrowser.noDatabases") }}</p>
    </div>

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.databaseName") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.characterSet") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.collation") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.owner") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in databaseRows"
          :key="row.value.name"
          class="cursor-pointer hover:bg-muted/50"
          @click="$emit('select', row.stored)"
        >
          <TableCell>
            <div class="flex items-center gap-2">
              <Database class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.name }}</span>
            </div>
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ row.value.characterSet || "-" }}
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ row.value.collation || "-" }}
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ row.value.owner || "-" }}
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { Database } from "lucide-vue-next";
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
  DatabaseSchemaMetadata,
  StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";

const props = defineProps<{
  items: StoredMetadata[];
}>();

defineEmits<{
  select: [item: StoredMetadata];
}>();

const { t } = useI18n();

const databaseRows = computed(() => {
  const rows: Array<{ stored: StoredMetadata; value: DatabaseSchemaMetadata }> =
    [];
  for (const stored of props.items) {
    if (stored.type.case === "databaseSchemaMetadata") {
      rows.push({ stored, value: stored.type.value });
    }
  }
  return rows;
});
</script>
