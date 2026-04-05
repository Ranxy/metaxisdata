<template>
  <div
    class="rounded-lg border bg-card text-card-foreground shadow-sm min-w-[180px]"
    :class="[
      data.isRoot ? 'ring-2 ring-primary' : '',
      isExternal ? 'border-amber-300 dark:border-amber-700' : '',
      showFields ? 'max-w-[300px]' : 'max-w-[260px]',
    ]"
  >
    <div
      class="flex items-center gap-2 px-3 py-2 border-b"
      :class="[
        data.isRoot ? 'bg-primary/10' : isExternal ? 'bg-amber-50 dark:bg-amber-950/30' : 'bg-muted/50',
      ]"
    >
      <component
        :is="nodeIcon"
        class="size-4 shrink-0"
        :class="data.isRoot ? 'text-primary' : isExternal ? 'text-amber-600 dark:text-amber-400' : 'text-muted-foreground'"
      />
      <span class="text-sm font-medium truncate" :title="data.label">
        {{ data.label }}
      </span>
      <Badge v-if="isExternal" variant="outline" class="text-[10px] px-1 py-0 ml-auto shrink-0 border-amber-400 text-amber-600">
        External
      </Badge>
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

    <div class="border-t px-3 py-1.5 flex items-center gap-2 flex-wrap">
      <button
        v-if="!data.expanded"
        class="text-xs text-primary hover:underline cursor-pointer"
        @click.stop="$emit('expand', data.guid)"
      >
        {{ t("lineageGraph.expandNode") }}
      </button>
      <button
        v-if="data.columns.length > 0"
        class="text-xs text-primary hover:underline cursor-pointer"
        @click.stop="handleToggleFields"
      >
        {{ showFields ? t("lineageGraph.hideFields") : t("lineageGraph.showFields") }}
      </button>
    </div>

    <div v-if="showFields && data.columns.length > 0" class="border-t max-h-[200px] overflow-y-auto">
      <button
        v-for="col in data.columns"
        :key="col"
        class="flex items-center gap-1.5 w-full px-3 py-1 text-xs text-left hover:bg-muted/50 cursor-pointer transition-colors"
        :class="{
          'bg-primary/10 text-primary font-medium': data.selectedColumn === col,
          'bg-accent/50 font-medium': data.selectedColumn !== col && data.highlightedColumns.has(col),
        }"
        @click.stop="$emit('select-column', data.guid, col)"
      >
        <Columns3 class="size-3 shrink-0 text-muted-foreground" />
        <span class="truncate">{{ col }}</span>
      </button>
    </div>

    <Handle type="target" :position="Position.Left" class="!bg-primary !w-2 !h-2" />
    <Handle type="source" :position="Position.Right" class="!bg-primary !w-2 !h-2" />
  </div>
</template>

<script setup lang="ts">
import { Handle, Position } from "@vue-flow/core";
import { CloudIcon, Columns3, TableIcon, ViewIcon } from "lucide-vue-next";
import { computed, ref } from "vue";
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
  columns: string[];
  selectedColumn: string | null;
  highlightedColumns: Set<string>;
}

const props = defineProps<{
  data: LineageNodeData;
}>();

const emit = defineEmits<{
  expand: [guid: string];
  "select-column": [guid: string, column: string];
  "toggle-fields": [guid: string, visible: boolean];
}>();

const { t } = useI18n();

const showFields = ref(false);

function handleToggleFields() {
  showFields.value = !showFields.value;
  emit("toggle-fields", props.data.guid, showFields.value);
}

const isExternal = computed(() => props.data.metaType === "external");

const nodeIcon = computed(() => {
  if (isExternal.value) {
    return CloudIcon;
  }
  if (
    props.data.metaType === "view" ||
    props.data.metaType === "materialized_view"
  ) {
    return ViewIcon;
  }
  return TableIcon;
});
</script>
