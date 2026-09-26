<template>
  <component
    :is="listComponent"
    :items="items"
    @select="(item: StoredMetadata) => emit('select', item, metaType)"
  />
</template>

<script setup lang="ts">
import { computed } from "vue";
import {
  MetaType,
  type StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";
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
}>();

const emit = defineEmits<{
  select: [item: StoredMetadata, metaType: MetaType];
}>();

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
    default:
      return DatabaseList;
  }
});
</script>
