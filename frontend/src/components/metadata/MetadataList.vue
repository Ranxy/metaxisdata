<template>
  <component
    v-if="listComponent"
    :is="listComponent"
    :items="items"
    v-bind="qualityProps"
    @select="(item: StoredMetadata) => emit('select', item, metaType)"
    @refresh="emit('refresh')"
  />
  <!-- A leaf object's children (columns, indexes) have their own lists; any
       other type the tab strip can name gets a neutral "no data" instead of
       another type's list, which used to say "No databases" over a columns
       tab. -->
  <EmptyState
    v-else
    :icon="SearchX"
    :title="t('metadataBrowser.empty')"
  />
</template>

<script setup lang="ts">
import { SearchX } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import EmptyState from "@/components/common/EmptyState.vue";
import type { QualitySignalState } from "@/composables/useQualitySignals";
import type { QualityTargetSummary } from "@/types/proto-es/v1/data_quality_service_pb";
import {
  MetaType,
  type StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import { isQualitySignalMetaType } from "@/utils/dataQualitySignal";
import ApplicationSQLList from "./ApplicationSQLList.vue";
import ColumnList from "./ColumnList.vue";
import DatabaseList from "./DatabaseList.vue";
import ExternalTableList from "./ExternalTableList.vue";
import FunctionList from "./FunctionList.vue";
import ManualSQLList from "./ManualSQLList.vue";
import MaterializedViewList from "./MaterializedViewList.vue";
import ProcedureList from "./ProcedureList.vue";
import SchemaList from "./SchemaList.vue";
import SequenceList from "./SequenceList.vue";
import TableList from "./TableList.vue";
import ViewList from "./ViewList.vue";

const props = defineProps<{
  metaType: MetaType;
  items: StoredMetadata[];
  /** Trust signals of the listed objects, keyed by GUID + meta type. */
  qualitySignals?: Map<string, QualityTargetSummary>;
  /** Whether the signal read has answered; see `QualitySignalState`. */
  qualityState?: QualitySignalState;
}>();

const emit = defineEmits<{
  select: [item: StoredMetadata, metaType: MetaType];
  refresh: [];
}>();

const { t } = useI18n();

/**
 * Only the four object types with quality columns take the signal props. The
 * other lists would otherwise receive undeclared props as DOM attributes.
 */
const qualityProps = computed(() =>
  isQualitySignalMetaType(props.metaType) && props.qualitySignals !== undefined
    ? {
        qualitySignals: props.qualitySignals,
        qualityState: props.qualityState ?? "ready",
      }
    : {}
);

const listComponent = computed(() => {
  switch (props.metaType) {
    case MetaType.DATABASE:
      return DatabaseList;
    case MetaType.SCHEMA:
      return SchemaList;
    case MetaType.TABLE:
      return TableList;
    case MetaType.EXTERNAL_TABLE:
      return ExternalTableList;
    case MetaType.VIEW:
      return ViewList;
    case MetaType.MATERIALIZED_VIEW:
      return MaterializedViewList;
    case MetaType.FUNCTION:
      return FunctionList;
    case MetaType.PROCEDURE:
      return ProcedureList;
    case MetaType.SEQUENCE:
      return SequenceList;
    case MetaType.MANUAL_SQL:
      return ManualSQLList;
    case MetaType.APPLICATION_SQL:
      return ApplicationSQLList;
    case MetaType.COLUMN:
      return ColumnList;
    default:
      return null;
  }
});
</script>
