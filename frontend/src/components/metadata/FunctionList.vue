<template>
  <div>
    <div
      v-if="rows.length === 0"
      class="p-8 text-center text-muted-foreground"
    >
      <FunctionSquare class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
      <p>{{ t("metadataBrowser.noFunctions") }}</p>
    </div>

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.functionName") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.signature") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.definition") }}</TableHead>
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
              <FunctionSquare class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.name }}</span>
            </div>
          </TableCell>
          <TableCell class="text-muted-foreground max-w-md truncate">
            {{ row.value.signature || "-" }}
          </TableCell>
          <TableCell class="max-w-md">
            <code class="text-xs bg-muted px-2 py-1 rounded truncate block">
              {{ truncateDefinition(row.value.definition) }}
            </code>
          </TableCell>
          <TableCell class="text-muted-foreground max-w-xs">
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
import { FunctionSquare } from "lucide-vue-next";
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
  FunctionMetadata,
  StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import ExpandableText from "./ExpandableText.vue";

const props = defineProps<{ items: StoredMetadata[] }>();

defineEmits<{ select: [item: StoredMetadata] }>();

const { t } = useI18n();

const rows = computed(() => {
  const list: Array<{ stored: StoredMetadata; value: FunctionMetadata }> = [];

  for (const stored of props.items) {
    if (stored.type.case === "functionMetadata") {
      list.push({ stored, value: stored.type.value });
    }
  }

  return list;
});

function truncateDefinition(def: string): string {
  if (!def) return "-";
  const maxLength = 100;
  return def.length > maxLength ? `${def.slice(0, maxLength)}...` : def;
}
</script>
