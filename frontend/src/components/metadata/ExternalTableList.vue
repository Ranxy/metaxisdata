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
          <TableHead v-if="qualitySignals !== undefined" class="hidden md:table-cell">{{ t("dataQuality.signal.column") }}</TableHead>
          <TableHead class="hidden md:table-cell">{{ t("metadataBrowser.externalServer") }}</TableHead>
          <TableHead class="hidden md:table-cell">{{ t("metadataBrowser.externalDatabase") }}</TableHead>
          <TableHead class="hidden sm:table-cell">{{ t("metadataBrowser.columns") }}</TableHead>
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
              <Plug class="h-4 w-4 text-muted-foreground" />
              <span class="font-medium">{{ row.value.name }}</span>
            </div>
          </TableCell>
          <TableCell
            v-if="qualitySignals !== undefined"
            class="hidden md:table-cell"
          >
            <QualitySignalCell
              :signal="qualitySignalOf(row.stored.guid)"
              :state="qualityState"
            />
          </TableCell>
          <TableCell class="hidden text-muted-foreground md:table-cell">
            {{ row.value.externalServerName || "-" }}
          </TableCell>
          <TableCell class="hidden text-muted-foreground md:table-cell">
            {{ row.value.externalDatabaseName || "-" }}
          </TableCell>
          <TableCell class="hidden sm:table-cell">
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
import QualitySignalCell from "@/components/data-quality/QualitySignalCell.vue";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { QualitySignalState } from "@/composables/useQualitySignals";
import type { QualityTargetSummary } from "@/types/proto-es/v1/data_quality_service_pb";
import {
  type ExternalTableMetadata,
  MetaType,
  type StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import { qualitySignalKey } from "@/utils/dataQualitySignal";

const props = withDefaults(
  defineProps<{
    items: StoredMetadata[];
    qualitySignals?: Map<string, QualityTargetSummary>;
    qualityState?: QualitySignalState;
  }>(),
  { qualitySignals: undefined, qualityState: "idle" }
);

defineEmits<{ select: [item: StoredMetadata] }>();

const { t } = useI18n();

function qualitySignalOf(guid: string): QualityTargetSummary | undefined {
  return props.qualitySignals?.get(
    qualitySignalKey(guid, MetaType.EXTERNAL_TABLE)
  );
}

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
