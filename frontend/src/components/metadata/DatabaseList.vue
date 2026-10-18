<template>
  <div>
    <EmptyState
      v-if="databaseRows.length === 0"
      :icon="Database"
      :title="t('metadataBrowser.noDatabases')"
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
          <TableHead>{{ t("metadataBrowser.databaseName") }}</TableHead>
          <TableHead v-if="hasCharacterSet" class="hidden md:table-cell">{{ t("metadataBrowser.characterSet") }}</TableHead>
          <TableHead v-if="hasCollation" class="hidden md:table-cell">{{ t("metadataBrowser.collation") }}</TableHead>
          <TableHead v-if="hasOwner" class="hidden md:table-cell">{{ t("metadataBrowser.owner") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in databaseRows"
          :key="row.value.name"
          class="cursor-pointer hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring focus-visible:outline-none"
          role="button"
          tabindex="0"
          @click="$emit('select', row.stored)"
          @keydown.enter.prevent="$emit('select', row.stored)"
          @keydown.space.prevent="$emit('select', row.stored)"
        >
          <TableCell>
            <div class="flex items-center gap-2">
              <Database class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.name }}</span>
            </div>
          </TableCell>
          <TableCell v-if="hasCharacterSet" class="hidden text-muted-foreground md:table-cell">
            {{ row.value.characterSet || "-" }}
          </TableCell>
          <TableCell v-if="hasCollation" class="hidden text-muted-foreground md:table-cell">
            {{ row.value.collation || "-" }}
          </TableCell>
          <TableCell v-if="hasOwner" class="hidden text-muted-foreground md:table-cell">
            {{ row.value.owner || "-" }}
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { Database, RefreshCw } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import EmptyState from "@/components/common/EmptyState.vue";
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
  DatabaseSchemaMetadata,
  StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";

const props = defineProps<{
  items: StoredMetadata[];
}>();

defineEmits<{
  select: [item: StoredMetadata];
  refresh: [];
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

const hasCharacterSet = computed(() =>
  databaseRows.value.some((row) => row.value.characterSet !== "")
);
const hasCollation = computed(() =>
  databaseRows.value.some((row) => row.value.collation !== "")
);
const hasOwner = computed(() =>
  databaseRows.value.some((row) => row.value.owner !== "")
);
</script>
