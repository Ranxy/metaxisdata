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
        <span>{{ metaTypeLabel(g.metaType, t) }}</span>
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
            {{ metaTypeLabel(g.metaType, t) }}
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
  type MetadataResponse_Metadata,
  MetaType,
} from "@/types/proto-es/v1/database_service_pb";
import { metaTypeLabel } from "@/utils/metaType";

const { t } = useI18n();

const props = defineProps<{
  groups: MetadataResponse_Metadata[];
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
</script>
