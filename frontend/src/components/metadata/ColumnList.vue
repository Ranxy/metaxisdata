<template>
  <div>
    <EmptyState
      v-if="columns.length === 0"
      :icon="Columns3"
      :title="t('metadataBrowser.noColumns')"
      :description="t('metadataBrowser.noColumnsHint')"
    />

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.columnName") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.columnType") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.nullable") }}</TableHead>
          <TableHead v-if="hasDefault" class="hidden md:table-cell">
            {{ t("metadataBrowser.defaultValue") }}
          </TableHead>
          <TableHead v-if="hasComment" class="hidden md:table-cell">
            {{ t("metadataBrowser.comment") }}
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="col in columns"
          :key="`${col.position}:${col.name}`"
        >
          <TableCell class="font-medium">{{ col.name }}</TableCell>
          <TableCell class="text-muted-foreground">
            {{ col.type || "-" }}
          </TableCell>
          <!-- Nullability is a property of the field, not a success state, so it
               reads as plain text instead of a green pill. -->
          <TableCell class="text-muted-foreground">
            {{ col.nullable ? t("metadataBrowser.yes") : t("metadataBrowser.no") }}
          </TableCell>
          <TableCell
            v-if="hasDefault"
            class="hidden text-muted-foreground md:table-cell"
          >
            {{ col.default || "-" }}
          </TableCell>
          <TableCell
            v-if="hasComment"
            class="hidden max-w-md text-muted-foreground md:table-cell"
          >
            <ExpandableText
              :text="col.userComment || col.comment"
              :item-name="col.name"
              :dialog-title="t('metadataBrowser.comment')"
            />
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { Columns3 } from "lucide-vue-next";
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
  ColumnMetadata,
  StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import ExpandableText from "./ExpandableText.vue";

const props = defineProps<{ items: StoredMetadata[] }>();

const { t } = useI18n();

const columns = computed(() => {
  const rows: ColumnMetadata[] = [];
  for (const stored of props.items) {
    if (stored.type.case === "columnMetadata") {
      rows.push(stored.type.value);
    }
  }
  return rows;
});

// A column that is empty on every row costs a column of dashes and reads as
// missing data, so the whole column collapses until one row fills it.
const hasDefault = computed(() =>
  columns.value.some((column) => column.default !== "")
);
const hasComment = computed(() =>
  columns.value.some((column) => (column.userComment || column.comment) !== "")
);
</script>
