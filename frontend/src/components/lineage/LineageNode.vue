<template>
  <div
    class="relative w-[232px] overflow-hidden rounded-lg border bg-card text-card-foreground shadow-sm"
    :class="data.isRoot ? 'ring-2 ring-ring' : 'hover:border-foreground/30'"
    @click="$emit('select-node', data.guid)"
  >
    <!-- The scope's accent: the one mark that stays on screen when two objects
         in different instances share a database, schema and name. -->
    <div
      class="absolute inset-y-0 left-0 w-1"
      :style="{ backgroundColor: data.scopeColor }"
    />

    <div
      class="flex items-center gap-1.5 border-b py-1.5 pl-3 pr-2"
      :class="data.isRoot ? 'bg-accent/60' : 'bg-muted/40'"
    >
      <span
        class="size-2 shrink-0 rounded-full"
        :style="{ backgroundColor: data.scopeColor }"
      />
      <span
        class="truncate text-[11px] font-medium text-muted-foreground"
        :title="data.scopeLabel"
      >
        {{ data.scopeLabel || t("lineageGraph.unknownScope") }}
      </span>
      <Badge
        variant="outline"
        class="ml-auto shrink-0 px-1.5 py-0 text-[10px] font-normal"
      >
        {{ data.typeLabel }}
      </Badge>
      <component
        :is="nodeIcon"
        class="size-3.5 shrink-0"
        :class="isExternal ? 'text-amber-600 dark:text-amber-400' : 'text-muted-foreground'"
      />
    </div>

    <div class="space-y-0.5 py-2 pl-3 pr-2" :title="data.fullLabel">
      <div class="truncate text-sm font-medium">
        {{ data.label }}
      </div>
      <div
        v-if="data.qualifier"
        class="truncate font-mono text-[11px] text-muted-foreground"
      >
        {{ data.qualifier }}
      </div>
      <div class="flex flex-wrap items-center gap-1.5 pt-1">
        <Badge v-if="data.upstreamCount > 0" variant="warning" class="text-[10px]">
          ↑ {{ data.upstreamCount }}
        </Badge>
        <Badge
          v-if="data.downstreamCount > 0"
          variant="outline"
          class="text-[10px]"
        >
          ↓ {{ data.downstreamCount }}
        </Badge>
        <Badge v-if="data.isRoot" variant="secondary" class="text-[10px]">
          {{ t("lineageGraph.rootNode") }}
        </Badge>
      </div>
    </div>

    <div class="flex flex-wrap items-center gap-x-3 gap-y-1 border-t py-1.5 pl-3 pr-2">
      <button
        v-if="!data.upstreamExpanded"
        class="cursor-pointer text-[11px] text-primary hover:underline"
        @click.stop="$emit('expand', data.guid, 'upstream')"
      >
        {{ t("lineageGraph.expandUpstream") }}
      </button>
      <button
        v-if="!data.downstreamExpanded"
        class="cursor-pointer text-[11px] text-primary hover:underline"
        @click.stop="$emit('expand', data.guid, 'downstream')"
      >
        {{ t("lineageGraph.expandDownstream") }}
      </button>
      <button
        v-if="data.columns.length > 0"
        class="cursor-pointer text-[11px] text-primary hover:underline"
        @click.stop="handleToggleFields"
      >
        {{ showFields ? t("lineageGraph.hideFields") : t("lineageGraph.showFields") }}
      </button>
    </div>

    <div v-if="showFields && data.columns.length > 0" class="max-h-[200px] overflow-y-auto border-t">
      <button
        v-for="col in data.columns"
        :key="col"
        class="flex w-full cursor-pointer items-center gap-1.5 py-1 pl-3 pr-2 text-left text-xs transition-colors hover:bg-muted/50"
        :class="{
          'bg-primary/10 font-medium text-primary': data.selectedColumn === col,
          'bg-accent/50 font-medium': data.selectedColumn !== col && data.highlightedColumns.has(col),
        }"
        @click.stop="$emit('select-column', data.guid, col)"
      >
        <Columns3 class="size-3 shrink-0 text-muted-foreground" />
        <span class="truncate">{{ col }}</span>
      </button>
    </div>

    <Handle
      type="target"
      :position="Position.Left"
      class="!h-2 !w-2 !border-0"
      :style="{ backgroundColor: data.scopeColor }"
    />
    <Handle
      type="source"
      :position="Position.Right"
      class="!h-2 !w-2 !border-0"
      :style="{ backgroundColor: data.scopeColor }"
    />
  </div>
</template>

<script setup lang="ts">
import { Handle, Position } from "@vue-flow/core";
import { CloudIcon, Columns3, TableIcon, ViewIcon } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";

/** What the node's icon shows; a dataset outside every instance is `external`. */
export type LineageNodeKind = "table" | "view" | "external";

export interface LineageNodeData {
  guid: string;
  /** The object's own name, which is all the title states. */
  label: string;
  /** The scope the object lives in: an instance title, or a dataset namespace. */
  scopeLabel: string;
  /** The scope's accent, shared by every node of the same instance. */
  scopeColor: string;
  /** The path between the scope and the object, e.g. `e2e.e2e_dwd`; may be empty. */
  qualifier: string;
  /** `scope · qualifier.name`, the tooltip that tells same-named objects apart. */
  fullLabel: string;
  /** The object's translated type, e.g. `Table`, `View`, `External dataset`. */
  typeLabel: string;
  kind: LineageNodeKind;
  isRoot: boolean;
  /** Whether that direction's neighbours are already part of the graph. */
  upstreamExpanded: boolean;
  downstreamExpanded: boolean;
  upstreamCount: number;
  downstreamCount: number;
  columns: string[];
  selectedColumn: string | null;
  highlightedColumns: Set<string>;
}

const props = defineProps<{
  data: LineageNodeData;
}>();

const emit = defineEmits<{
  expand: [guid: string, direction: "upstream" | "downstream"];
  "select-node": [guid: string];
  "select-column": [guid: string, column: string];
  "toggle-fields": [guid: string, visible: boolean];
}>();

const { t } = useI18n();

const showFields = ref(false);

function handleToggleFields() {
  showFields.value = !showFields.value;
  emit("toggle-fields", props.data.guid, showFields.value);
}

const isExternal = computed(() => props.data.kind === "external");

const nodeIcon = computed(() => {
  if (props.data.kind === "external") {
    return CloudIcon;
  }
  return props.data.kind === "view" ? ViewIcon : TableIcon;
});
</script>
