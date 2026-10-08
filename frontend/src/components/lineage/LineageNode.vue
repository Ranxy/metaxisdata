<template>
  <!-- The instance's accent is the card's left border rather than an absolutely
       positioned strip: the node must not clip its overflow, because Vue Flow's
       connection handles sit astride the border box and an `overflow-hidden`
       card cuts them — and their drag targets — in half. -->
  <ContextMenu>
    <ContextMenuTrigger as-child>
      <div
        class="w-[232px] rounded-lg border border-l-4 bg-card text-card-foreground shadow-sm"
        :class="cardClass"
        :style="{ borderLeftColor: data.scopeColor }"
        @click="$emit('select-node', data.guid)"
      >
        <div
          class="flex items-center gap-1.5 border-b py-1.5 pl-2.5 pr-2"
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

        <div class="space-y-0.5 py-2 pl-2.5 pr-2" :title="data.fullLabel">
          <div class="truncate text-sm font-medium">
            {{ data.label }}
          </div>
          <!-- The fields of this node that the selected column's trail runs through,
               named here rather than in an expanded field list: a node that has to be
               opened first would hide the very flow the trail is drawn to show. -->
          <div
            v-if="data.qualifier || trailColumns.length > 0"
            class="truncate font-mono text-[11px] text-muted-foreground"
            :title="pathTitle"
          >
            <span v-if="data.qualifier">{{ data.qualifier }}</span>
            <template v-for="(column, index) in trailColumns" :key="column">
              <span
                v-if="data.qualifier || index > 0"
                class="text-muted-foreground/50"
              >
                ·
              </span>
              <span class="font-medium text-amber-600 dark:text-amber-400">
                {{ column }}
              </span>
            </template>
          </div>
          <div class="flex flex-wrap items-center gap-1.5 pt-1">
            <Badge
              v-if="data.upstreamCount > 0"
              variant="warning"
              class="text-[10px]"
            >
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

        <div
          class="flex flex-wrap items-center gap-x-3 gap-y-1 border-t py-1.5 pl-2.5 pr-2"
        >
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
            {{ data.fieldsVisible ? t("lineageGraph.hideFields") : t("lineageGraph.showFields") }}
          </button>
        </div>

        <div
          v-if="data.fieldsVisible && data.columns.length > 0"
          ref="fieldList"
          class="max-h-[200px] overflow-y-auto border-t"
        >
          <!-- Each field carries its own menu: a right-click on a field asks for
               that field, and the node's own Expand items would answer with all of
               it. The two are nested, and the inner trigger's `preventDefault`
               keeps the outer menu shut. -->
          <ContextMenu v-for="col in data.columns" :key="col">
            <ContextMenuTrigger as-child>
              <button
                :data-column="col"
                class="flex w-full cursor-pointer items-center gap-1.5 py-1 pl-2.5 pr-2 text-left text-xs transition-colors hover:bg-muted/50"
                :class="{
                  'bg-primary/10 font-medium text-primary': data.selectedColumn === col,
                  'bg-accent/50 font-medium': data.selectedColumn !== col && data.highlightedColumns.has(col),
                }"
                @click.stop="$emit('select-column', data.guid, col)"
              >
                <Columns3 class="size-3 shrink-0 text-muted-foreground" />
                <span class="truncate">{{ col }}</span>
              </button>
            </ContextMenuTrigger>

            <ContextMenuContent class="w-52">
              <ContextMenuLabel class="truncate" :title="`${data.fullLabel} · ${col}`">
                {{ col }}
              </ContextMenuLabel>
              <ContextMenuSeparator />
              <ContextMenuItem
                :disabled="data.kind === 'external'"
                @select="$emit('view-schema', data.guid)"
              >
                <Code class="size-3.5 text-muted-foreground" />
                {{ t("metadataBrowser.viewSchema") }}
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem
                v-if="canExpandColumn(col, 'upstream')"
                @select="$emit('expand-column', data.guid, col, 'upstream')"
              >
                <ArrowUp class="size-3.5 text-muted-foreground" />
                {{ t("lineageGraph.expandColumnUpstream") }}
              </ContextMenuItem>
              <ContextMenuItem
                v-if="canExpandColumn(col, 'downstream')"
                @select="$emit('expand-column', data.guid, col, 'downstream')"
              >
                <ArrowDown class="size-3.5 text-muted-foreground" />
                {{ t("lineageGraph.expandColumnDownstream") }}
              </ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
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
    </ContextMenuTrigger>

    <!-- The same actions the footer offers, where a reader looks for them. The
         footer hides what it cannot do, and so does this: an item that would do
         nothing is worse than an item that is not there. -->
    <ContextMenuContent class="w-52">
      <ContextMenuLabel class="truncate" :title="data.fullLabel">
        {{ data.label }}
      </ContextMenuLabel>
      <ContextMenuSeparator />
      <ContextMenuItem
        :disabled="data.kind === 'external'"
        @select="$emit('view-schema', data.guid)"
      >
        <Code class="size-3.5 text-muted-foreground" />
        {{ t("metadataBrowser.viewSchema") }}
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem
        v-if="!data.upstreamExpanded"
        @select="$emit('expand', data.guid, 'upstream')"
      >
        <ArrowUp class="size-3.5 text-muted-foreground" />
        {{ t("lineageGraph.expandUpstream") }}
      </ContextMenuItem>
      <ContextMenuItem
        v-if="!data.downstreamExpanded"
        @select="$emit('expand', data.guid, 'downstream')"
      >
        <ArrowDown class="size-3.5 text-muted-foreground" />
        {{ t("lineageGraph.expandDownstream") }}
      </ContextMenuItem>
      <ContextMenuItem v-if="data.columns.length > 0" @select="handleToggleFields">
        <Columns3 class="size-3.5 text-muted-foreground" />
        {{ data.fieldsVisible ? t("lineageGraph.hideFields") : t("lineageGraph.showFields") }}
      </ContextMenuItem>
    </ContextMenuContent>
  </ContextMenu>
</template>

<script setup lang="ts">
import { Handle, Position } from "@vue-flow/core";
import {
  ArrowDown,
  ArrowUp,
  CloudIcon,
  Code,
  Columns3,
  TableIcon,
  ViewIcon,
} from "lucide-vue-next";
import { computed, onMounted, onUpdated, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";

/** What the node's icon shows; a dataset outside every instance is `external`. */
export type LineageNodeKind = "table" | "view" | "external";

export interface LineageNodeData {
  guid: string;
  /** Whether the selected column's trail runs through this node. */
  onTrail: boolean;
  /** Whether a trail is being shown and this node is not on it. */
  dimmed: boolean;
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
  /** Whether this node's field list is on screen. The page owns it, so an
   * expansion can open the list of every node it reached. */
  fieldsVisible: boolean;
  /** The columns of this node that the selected column's trail runs through. */
  highlightedColumns: Set<string>;
  /** The fields whose own lineage has been expanded that way, so the node's
   * direction draws only what those fields reach. */
  columnExpandedUpstream: Set<string>;
  columnExpandedDownstream: Set<string>;
}

const props = defineProps<{
  data: LineageNodeData;
}>();

const emit = defineEmits<{
  expand: [guid: string, direction: "upstream" | "downstream"];
  "expand-column": [
    guid: string,
    column: string,
    direction: "upstream" | "downstream",
  ];
  "select-node": [guid: string];
  "select-column": [guid: string, column: string];
  "toggle-fields": [guid: string, visible: boolean];
  "view-schema": [guid: string];
}>();

const { t } = useI18n();

/** Whether the field list is on screen is the page's to say: it opens the lists of
 * the nodes an expansion reached, so a revealed node shows the field that carries
 * the lineage without a click. */
const fieldList = ref<HTMLElement | null>(null);

function handleToggleFields() {
  emit("toggle-fields", props.data.guid, !props.data.fieldsVisible);
}

/**
 * Whether one field can still be expanded that way. A direction the node itself
 * has already expanded draws all of its relations — there is nothing left for a
 * field to add — and a field that was expanded this way is drawn already.
 */
function canExpandColumn(
  column: string,
  direction: "upstream" | "downstream"
): boolean {
  const expanded =
    direction === "upstream"
      ? props.data.columnExpandedUpstream
      : props.data.columnExpandedDownstream;
  const wholeNode =
    direction === "upstream"
      ? props.data.upstreamExpanded
      : props.data.downstreamExpanded;
  return !wholeNode && !expanded.has(column);
}

const isExternal = computed(() => props.data.kind === "external");

/**
 * A node on the trail is ringed and everything else steps back, so the eye
 * follows the field rather than the graph. While a trail is shown the ring means
 * "on the trail" for every node, the root included — the root's own ring would
 * otherwise claim the same mark, and its "Root node" chip still names it.
 */
const cardClass = computed(() => {
  if (props.data.dimmed) {
    return "opacity-40";
  }
  if (props.data.onTrail) {
    return "ring-2 ring-amber-500/70";
  }
  return props.data.isRoot ? "ring-2 ring-ring" : "hover:border-foreground/30";
});

const trailColumns = computed(() => [...props.data.highlightedColumns].sort());

/** The path line truncates, so its tooltip carries every part of it. */
const pathTitle = computed(() =>
  [props.data.qualifier, ...trailColumns.value].filter(Boolean).join(" · ")
);

const nodeIcon = computed(() => {
  if (props.data.kind === "external") {
    return CloudIcon;
  }
  return props.data.kind === "view" ? ViewIcon : TableIcon;
});

/**
 * Brings the field the trail runs through into view. The list is alphabetical and
 * capped at 200px, so the field the graph is about can sit below the fold of the
 * list's own scroll area — which is exactly what happens to a node whose list was
 * opened for an expansion. Only a row that is out of view moves, and only far enough
 * to reach the edge, so a reader who scrolled the list themselves is left alone.
 * `scrollTop` is written rather than `scrollIntoView`, which would scroll every
 * scrollable ancestor the row has.
 *
 * Returns whether there is nothing left to do, which lets the caller re-check after
 * the layout settles — the card is restacked while an expansion finishes, so the
 * row's place inside the list can still move after the render that opened it.
 */
function revealTrailField(): boolean {
  const list = fieldList.value;
  const column = trailColumns.value[0];
  if (!props.data.fieldsVisible || !list || !column) {
    return true;
  }
  const row = [
    ...list.querySelectorAll<HTMLElement>("button[data-column]"),
  ].find((candidate) => candidate.dataset.column === column);
  if (!row) {
    return true;
  }

  const listBox = list.getBoundingClientRect();
  const rowBox = row.getBoundingClientRect();
  // The canvas scales the cards, so a rect is in screen pixels while `scrollTop`
  // and `offsetHeight` are in layout pixels. The list's own ratio converts: it is
  // the layout height over the height it is drawn at.
  const scale = listBox.height > 0 ? list.offsetHeight / listBox.height : 1;
  const top = (rowBox.top - listBox.top) * scale;
  const bottom = top + row.offsetHeight;
  if (top < 0) {
    list.scrollTop += top;
  } else if (bottom > list.clientHeight) {
    list.scrollTop += bottom - list.clientHeight;
  } else {
    return true;
  }
  return false;
}

/** Reveals the field now, and again over the next frames while the layout settles. */
function scheduleReveal() {
  let frames = 0;
  const step = () => {
    if (revealTrailField() || frames++ >= 20) {
      return;
    }
    requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
}

onMounted(scheduleReveal);
onUpdated(scheduleReveal);
</script>
