<template>
  <div>
    <EmptyState
      v-if="manualSQLRows.length === 0"
      :icon="FileCode2"
      :title="t('metadataBrowser.noManualSqls')"
    />

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.manualSqlTitle") }}</TableHead>
          <TableHead class="hidden md:table-cell">{{ t("metadataBrowser.schemaName") }}</TableHead>
          <TableHead v-if="hasComment" class="hidden md:table-cell">{{ t("metadataBrowser.comment") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in manualSQLRows"
          :key="row.value.manualSqlId"
          class="cursor-pointer hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring focus-visible:outline-none"
          role="button"
          tabindex="0"
          @click="$emit('select', row.stored)"
          @keydown.enter.self.prevent="$emit('select', row.stored)"
          @keydown.space.self.prevent="$emit('select', row.stored)"
        >
          <TableCell>
            <div class="flex items-center gap-2">
              <FileCode2 class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.title || row.value.name }}</span>
            </div>
            <div
              v-if="showsIdentifier(row.value)"
              class="text-xs text-muted-foreground mt-1"
            >
              {{ row.value.name }}
            </div>
          </TableCell>
          <TableCell class="hidden text-muted-foreground md:table-cell">
            {{ row.value.schemaName || t("metadataBrowser.defaultSchema") }}
          </TableCell>
          <TableCell v-if="hasComment" class="hidden text-muted-foreground md:table-cell">
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
import EmptyState from "@/components/common/EmptyState.vue";
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

// A comment that is empty on every row renders one column of dashes.
const hasComment = computed(() =>
  manualSQLRows.value.some((row) => row.value.comment !== "")
);

/**
 * The identifier under the title is only worth a line when the title above it
 * says something else: the fallback title is the name itself, which used to
 * render the same string twice.
 */
function showsIdentifier(value: ManualSQLMetadata): boolean {
  return value.title !== "" && value.title !== value.name;
}
</script>