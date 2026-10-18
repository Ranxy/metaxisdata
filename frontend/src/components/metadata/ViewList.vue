<template>
  <div>
    <EmptyState
      v-if="viewRows.length === 0"
      :icon="Eye"
      :title="t('metadataBrowser.noViews')"
    />

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.viewName") }}</TableHead>
          <TableHead class="hidden sm:table-cell">{{ t("metadataBrowser.columns") }}</TableHead>
          <TableHead class="hidden md:table-cell">{{ t("metadataBrowser.definition") }}</TableHead>
          <TableHead v-if="hasComment" class="hidden md:table-cell">{{ t("metadataBrowser.comment") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in viewRows"
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
              <Eye class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.name }}</span>
            </div>
          </TableCell>
          <TableCell class="hidden sm:table-cell">
            <Badge variant="outline">
              {{ row.value.columns.length }} {{ t("metadataBrowser.columnsCount") }}
            </Badge>
          </TableCell>
          <TableCell class="hidden max-w-md md:table-cell">
            <code class="text-xs bg-muted px-2 py-1 rounded truncate block">
              {{ truncateDefinition(row.value.definition) }}
            </code>
          </TableCell>
          <TableCell v-if="hasComment" class="hidden max-w-xs text-muted-foreground md:table-cell">
            <ExpandableText
              :text="row.value.comment"
              :item-name="row.value.name"
              :dialog-title="t('metadataBrowser.comment')"
            />
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
  StoredMetadata,
  ViewMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import ExpandableText from "./ExpandableText.vue";

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

// A comment that is empty on every row renders one column of dashes.
const hasComment = computed(() =>
  viewRows.value.some((row) => row.value.comment !== "")
);

function truncateDefinition(def: string): string {
  if (!def) return "-";
  const maxLength = 100;
  return def.length > maxLength ? `${def.slice(0, maxLength)}...` : def;
}
</script>
