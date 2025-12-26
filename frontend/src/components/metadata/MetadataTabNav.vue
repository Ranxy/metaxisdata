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
        @click="handleSelect(g.metaType)"
      >
        <span>{{ getMetaTypeLabel(g.metaType) }}</span>
      </Button>
    </div>

    <!-- Mobile: dropdown select -->
    <div class="md:hidden">
      <Select
        :model-value="String(activeResolved)"
        @update:model-value="handleSelect(Number($event) as MetaType)"
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
            {{ getMetaTypeLabel(g.metaType) }}
          </SelectItem>
        </SelectContent>
      </Select>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
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

const emit = defineEmits<{
  select: [metaType: MetaType];
}>();

const activeResolved = computed(() => {
  if (props.active != null) return props.active;
  return props.groups[0]?.metaType ?? MetaType.UNSPECIFIED;
});

function handleSelect(metaType: MetaType) {
  // Clicking the current (last) level is a no-op.
  if (metaType === activeResolved.value) return;
  emit("select", metaType);
}

function getMetaTypeLabel(type: MetaType): string {
  const labels: Partial<Record<MetaType, string>> = {
    [MetaType.INSTANCE]: t("metadataBrowser.instances"),
    [MetaType.DATABASE]: t("metadataBrowser.databases"),
    [MetaType.SCHEMA]: t("metadataBrowser.schemas"),
    [MetaType.TABLE]: t("metadataBrowser.tables"),
    [MetaType.VIEW]: t("metadataBrowser.views"),
    [MetaType.MATERIALIZED_VIEW]: t("metadataBrowser.materializedViews"),
    [MetaType.FUNCTION]: t("metadataBrowser.functions"),
    [MetaType.PROCEDURE]: t("metadataBrowser.procedures"),
    [MetaType.SEQUENCE]: t("metadataBrowser.sequences"),
  };
  return labels[type] || t("metadataBrowser.other");
}
</script>
