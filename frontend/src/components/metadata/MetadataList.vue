<template>
  <component
    :is="listComponent"
    :items="items"
    :is-mysql="isMysql"
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
import FunctionList from "./FunctionList.vue";
import MaterializedViewList from "./MaterializedViewList.vue";
import SchemaList from "./SchemaList.vue";
import SequenceList from "./SequenceList.vue";
import TableList from "./TableList.vue";
import ViewList from "./ViewList.vue";

const props = defineProps<{
  metaType: MetaType;
  items: StoredMetadata[];
  currentGuid: string;
  isMysql: boolean;
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
    case MetaType.VIEW:
      return ViewList;
    case MetaType.MATERIALIZED_VIEW:
      return MaterializedViewList;
    case MetaType.FUNCTION:
      return FunctionList;
    case MetaType.SEQUENCE:
      return SequenceList;
    default:
      return DatabaseList;
  }
});
</script>
