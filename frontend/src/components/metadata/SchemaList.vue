<template>
  <div>
    <div
      v-if="schemaRows.length === 0"
      class="p-8 text-center text-muted-foreground"
    >
      <Folder class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
      <p>{{ t("metadataBrowser.noSchemas") }}</p>
    </div>

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.schemaName") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in schemaRows"
          :key="row.key"
          class="cursor-pointer hover:bg-muted/50"
          @click="$emit('select', row.stored)"
        >
          <TableCell>
            <div class="flex items-center gap-2">
              <Folder class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.displayName }}</span>
            </div>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { Folder } from "lucide-vue-next";
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
  SchemaMetadata,
  StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";

const props = defineProps<{
  items: StoredMetadata[];
  isMysql: boolean;
}>();

defineEmits<{
  select: [item: StoredMetadata];
}>();

const { t } = useI18n();

const schemaRows = computed(() => {
  const rows: Array<{
    stored: StoredMetadata;
    value: SchemaMetadata;
    key: string;
    displayName: string;
  }> = [];

  for (const stored of props.items) {
    if (stored.type.case === "schemaMetadata") {
      const value = stored.type.value;
      const displayName = value.name || t("metadataBrowser.defaultSchema");
      rows.push({
        stored,
        value,
        key: `schema:${value.name}`,
        displayName,
      });
    }
  }

  return rows;
});
</script>
