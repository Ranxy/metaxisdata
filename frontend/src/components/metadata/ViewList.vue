<template>
  <div>
    <div
      v-if="viewRows.length === 0"
      class="p-8 text-center text-muted-foreground"
    >
      <Eye class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
      <p>{{ t("metadataBrowser.noViews") }}</p>
    </div>

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.viewName") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.columns") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.definition") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.comment") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in viewRows"
          :key="row.value.name"
          class="cursor-pointer hover:bg-muted/50"
          @click="$emit('select', row.stored)"
        >
          <TableCell>
            <div class="flex items-center gap-2">
              <Eye class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.name }}</span>
            </div>
          </TableCell>
          <TableCell>
            <Badge variant="outline">
              {{ row.value.columns.length }} {{ t("metadataBrowser.columnsCount") }}
            </Badge>
          </TableCell>
          <TableCell class="max-w-md">
            <code class="text-xs bg-muted px-2 py-1 rounded truncate block">
              {{ truncateDefinition(row.value.definition) }}
            </code>
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
import { Eye } from "lucide-vue-next";
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
  ViewMetadata,
} from "@/types/proto-es/v1/database_service_pb";

const props = defineProps<{
  items: StoredMetadata[];
}>();

defineEmits<{
  select: [item: StoredMetadata];
}>();

const { t } = useI18n();

const viewRows = computed(() => {
  const rows: Array<{ stored: StoredMetadata; value: ViewMetadata }> = [];
  for (const stored of props.items) {
    if (stored.type.case === "viewMetadata") {
      rows.push({ stored, value: stored.type.value });
    }
  }
  return rows;
});

function truncateDefinition(def: string): string {
  if (!def) return "-";
  const maxLength = 100;
  return def.length > maxLength ? `${def.slice(0, maxLength)}...` : def;
}
</script>
