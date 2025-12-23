<template>
  <div class="w-full">
    <!-- Desktop: horizontal tab buttons -->
    <div class="hidden md:flex gap-2 flex-wrap">
      <Button
        v-for="g in groups"
        :key="g.metaType"
        :variant="g.metaType === activeResolved ? 'secondary' : 'ghost'"
        size="sm"
        class="h-9"
        @click="$emit('select', g.metaType)"
      >
        <span>{{ getMetaTypeLabel(g.metaType) }}</span>
        <span class="ml-2">
          <Badge variant="secondary">{{ g.list.length }}</Badge>
        </span>
      </Button>
    </div>

    <!-- Mobile: dropdown select -->
    <div class="md:hidden">
      <Select
        :model-value="String(activeResolved)"
        @update:model-value="(v) => $emit('select', Number(v) as MetaType)"
      >
        <SelectTrigger>
          <SelectValue :placeholder="t('metadataBrowser.selectType')" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="g in groups"
            :key="g.metaType"
            :value="String(g.metaType)"
          >
            {{ getMetaTypeLabel(g.metaType) }} ({{ g.list.length }})
          </SelectItem>
        </SelectContent>
      </Select>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  type MetadataResponse_MetadataList,
  MetaType,
} from "@/types/proto-es/v1/database_service_pb";

const { t } = useI18n();

const props = defineProps<{
  groups: MetadataResponse_MetadataList[];
  active: MetaType | null;
}>();

defineEmits<{
  select: [metaType: MetaType];
}>();

const activeResolved = computed(() => {
  if (props.active != null) return props.active;
  return props.groups[0]?.metaType ?? MetaType.UNSPECIFIED;
});

function getMetaTypeLabel(type: MetaType): string {
  const labels: Partial<Record<MetaType, string>> = {
    [MetaType.INSTANCE]: t("metadataBrowser.instances"),
    [MetaType.DATABASE]: t("metadataBrowser.databases"),
    [MetaType.SCHEMA]: t("metadataBrowser.schemas"),
    [MetaType.TABLE]: t("metadataBrowser.tables"),
    [MetaType.VIEW]: t("metadataBrowser.views"),
  };
  return labels[type] || t("metadataBrowser.other");
}
</script>
