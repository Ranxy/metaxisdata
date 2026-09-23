<template>
  <div>
    <EmptyState
      v-if="rows.length === 0"
      :icon="Plug"
      :title="t('metadataBrowser.noExternalTables')"
    />

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.externalTableName") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.externalServer") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.externalDatabase") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.columns") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in rows"
          :key="row.value.name"
          class="cursor-pointer hover:bg-muted/50"
          @click="$emit('select', row.stored)"
        >
          <TableCell>
            <div class="flex items-center gap-2">
              <Plug class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.name }}</span>
            </div>
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ row.value.externalServerName || "-" }}
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ row.value.externalDatabaseName || "-" }}
          </TableCell>
          <TableCell>
            <Badge variant="outline">
              {{ row.value.columns.length }} {{ t("metadataBrowser.columnsCount") }}
            </Badge>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { Plug } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import EmptyState from "@/components/common/EmptyState.vue";
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
  ExternalTableMetadata,
  StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";

const props = defineProps<{ items: StoredMetadata[] }>();

defineEmits<{ select: [item: StoredMetadata] }>();

const { t } = useI18n();

const rows = computed(() => {
  const list: Array<{
    stored: StoredMetadata;
    value: ExternalTableMetadata;
  }> = [];

  for (const stored of props.items) {
    if (stored.type.case === "externalTableMetadata") {
      list.push({ stored, value: stored.type.value });
    }
  }

  return list;
});
</script>
