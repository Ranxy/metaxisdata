<template>
  <div>
    <div
      v-if="rows.length === 0"
      class="p-8 text-center text-muted-foreground"
    >
      <ListOrdered class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
      <p>{{ t("metadataBrowser.noSequences") }}</p>
    </div>

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.sequenceName") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.dataType") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.start") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.increment") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.cycle") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.comment") }}</TableHead>
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
              <ListOrdered class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.name }}</span>
            </div>
          </TableCell>
          <TableCell class="text-muted-foreground">{{ row.value.dataType || "-" }}</TableCell>
          <TableCell class="text-muted-foreground">{{ row.value.start || "-" }}</TableCell>
          <TableCell class="text-muted-foreground">{{ row.value.increment || "-" }}</TableCell>
          <TableCell>
            <Badge
              :variant="row.value.cycle ? 'success' : 'secondary'"
              class="whitespace-nowrap"
            >
              {{ row.value.cycle ? t("metadataBrowser.yes") : t("metadataBrowser.no") }}
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
import { ListOrdered } from "lucide-vue-next";
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
  SequenceMetadata,
  StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";

const props = defineProps<{ items: StoredMetadata[] }>();

defineEmits<{ select: [item: StoredMetadata] }>();

const { t } = useI18n();

const rows = computed(() => {
  const list: Array<{ stored: StoredMetadata; value: SequenceMetadata }> = [];
  for (const stored of props.items) {
    if (stored.type.case === "sequenceMetadata") {
      list.push({ stored, value: stored.type.value });
    }
  }
  return list;
});
</script>
