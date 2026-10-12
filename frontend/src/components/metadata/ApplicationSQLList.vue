<template>
  <div>
    <EmptyState
      v-if="rows.length === 0"
      :icon="TerminalSquare"
      :title="t('metadataBrowser.noApplicationSqls')"
      :description="t('metadataBrowser.noApplicationSqlsHint')"
    />

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("applicationSql.statement") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.schemaName") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in rows"
          :key="row.value.applicationSqlId"
          class="cursor-pointer hover:bg-muted/50"
          @click="$emit('select', row.stored)"
        >
          <TableCell>
            <div class="flex items-center gap-2">
              <TerminalSquare class="h-4 w-4 shrink-0 text-muted-foreground" />
              <span class="truncate font-mono text-xs">{{ row.value.name }}</span>
            </div>
            <!-- The stored text is what the object is; the label above is its first
                 line, so the rest is worth showing when it is not the same thing. -->
            <div
              v-if="restOfStatement(row.value) !== ''"
              class="mt-1 truncate text-xs text-muted-foreground"
            >
              {{ restOfStatement(row.value) }}
            </div>
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ row.value.schemaName || t("metadataBrowser.defaultSchema") }}
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { TerminalSquare } from "lucide-vue-next";
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
  ApplicationSQLMetadata,
  StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";

const props = defineProps<{
  items: StoredMetadata[];
}>();

defineEmits<{
  select: [item: StoredMetadata];
}>();

const { t } = useI18n();

const rows = computed(() => {
  const found: Array<{
    stored: StoredMetadata;
    value: ApplicationSQLMetadata;
  }> = [];
  for (const stored of props.items) {
    if (stored.type.case === "applicationSqlMetadata") {
      found.push({ stored, value: stored.type.value });
    }
  }
  return found;
});

/** The statement with the label's first line taken off the front. */
function restOfStatement(value: ApplicationSQLMetadata): string {
  const firstLineEnd = value.sqlText.indexOf("\n");
  if (firstLineEnd < 0) {
    return value.sqlText.length > value.name.length
      ? value.sqlText.slice(value.name.length).trim()
      : "";
  }
  return value.sqlText.slice(firstLineEnd + 1).trim();
}
</script>
