<template>
  <div>
    <EmptyState
      v-if="rows.length === 0"
      :icon="ListOrdered"
      :title="t('metadataBrowser.noSequences')"
    />

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.sequenceName") }}</TableHead>
          <TableHead class="hidden md:table-cell">{{ t("metadataBrowser.dataType") }}</TableHead>
          <TableHead class="hidden md:table-cell">{{ t("metadataBrowser.start") }}</TableHead>
          <TableHead class="hidden md:table-cell">{{ t("metadataBrowser.increment") }}</TableHead>
          <TableHead class="hidden md:table-cell">{{ t("metadataBrowser.cycle") }}</TableHead>
          <TableHead v-if="hasComment" class="hidden lg:table-cell">{{ t("metadataBrowser.comment") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="row in rows"
          :key="row.value.name"
          class="cursor-pointer hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring focus-visible:outline-none"
          role="button"
          tabindex="0"
          @click="$emit('select', row.stored)"
          @keydown.enter.self.prevent="$emit('select', row.stored)"
          @keydown.space.self.prevent="$emit('select', row.stored)"
        >
          <TableCell>
            <div class="flex items-center gap-2">
              <ListOrdered class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.name }}</span>
            </div>
          </TableCell>
          <TableCell class="hidden text-muted-foreground md:table-cell">{{ row.value.dataType || "-" }}</TableCell>
          <TableCell class="hidden text-muted-foreground md:table-cell">{{ row.value.start || "-" }}</TableCell>
          <TableCell class="hidden text-muted-foreground md:table-cell">{{ row.value.increment || "-" }}</TableCell>
          <TableCell class="hidden md:table-cell">
            <Badge
              :variant="row.value.cycle ? 'success' : 'secondary'"
              class="whitespace-nowrap"
            >
              {{ row.value.cycle ? t("metadataBrowser.yes") : t("metadataBrowser.no") }}
            </Badge>
          </TableCell>
          <TableCell v-if="hasComment" class="hidden max-w-xs text-muted-foreground lg:table-cell">
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
import { ListOrdered } from "lucide-vue-next";
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
  SequenceMetadata,
  StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import ExpandableText from "./ExpandableText.vue";

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

// A comment that is empty on every row renders one column of dashes.
const hasComment = computed(() =>
  rows.value.some((row) => row.value.comment !== "")
);
</script>
