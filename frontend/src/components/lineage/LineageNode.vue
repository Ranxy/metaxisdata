<template>
  <div
    class="rounded-lg border bg-card text-card-foreground shadow-sm min-w-[180px] max-w-[260px]"
    :class="{ 'ring-2 ring-primary': data.isRoot }"
  >
    <div
      class="flex items-center gap-2 px-3 py-2 border-b"
      :class="data.isRoot ? 'bg-primary/10' : 'bg-muted/50'"
    >
      <component
        :is="nodeIcon"
        class="size-4 shrink-0"
        :class="data.isRoot ? 'text-primary' : 'text-muted-foreground'"
      />
      <span class="text-sm font-medium truncate" :title="data.label">
        {{ data.label }}
      </span>
    </div>

    <div class="px-3 py-2 space-y-1">
      <div class="text-xs text-muted-foreground truncate" :title="data.guid">
        {{ data.shortPath }}
      </div>
      <div class="flex items-center gap-2">
        <Badge v-if="data.upstreamCount > 0" variant="warning" class="text-xs">
          ↑ {{ data.upstreamCount }}
        </Badge>
        <Badge v-if="data.downstreamCount > 0" variant="outline" class="text-xs">
          ↓ {{ data.downstreamCount }}
        </Badge>
      </div>
    </div>

    <div v-if="!data.expanded" class="border-t px-3 py-1.5">
      <button
        class="text-xs text-primary hover:underline cursor-pointer"
        @click.stop="$emit('expand', data.guid)"
      >
        {{ t("lineageGraph.expandNode") }}
      </button>
    </div>

    <Handle type="target" :position="Position.Left" class="!bg-primary !w-2 !h-2" />
    <Handle type="source" :position="Position.Right" class="!bg-primary !w-2 !h-2" />
  </div>
</template>

<script setup lang="ts">
import { Handle, Position } from "@vue-flow/core";
import { TableIcon, ViewIcon } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";

export interface LineageNodeData {
  guid: string;
  label: string;
  shortPath: string;
  isRoot: boolean;
  expanded: boolean;
  upstreamCount: number;
  downstreamCount: number;
  metaType: string;
}

const props = defineProps<{
  data: LineageNodeData;
}>();

defineEmits<{
  expand: [guid: string];
}>();

const { t } = useI18n();

const nodeIcon = computed(() => {
  if (
    props.data.metaType === "view" ||
    props.data.metaType === "materialized_view"
  ) {
    return ViewIcon;
  }
  return TableIcon;
});
</script>
