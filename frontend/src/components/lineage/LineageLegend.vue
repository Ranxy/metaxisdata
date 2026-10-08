<template>
  <div
    class="pointer-events-auto w-60 rounded-lg border bg-popover text-xs text-popover-foreground shadow-md"
  >
    <button
      class="flex w-full items-center gap-2 px-3 py-2 text-left font-medium"
      :title="collapsed ? t('lineageGraph.legendExpand') : t('lineageGraph.legendCollapse')"
      @click="collapsed = !collapsed"
    >
      <ListTree class="size-3.5 text-muted-foreground" />
      {{ t("lineageGraph.legend") }}
      <component
        :is="collapsed ? ChevronDown : ChevronUp"
        class="ml-auto size-3.5 text-muted-foreground"
      />
    </button>

    <div v-if="!collapsed" class="space-y-3 border-t px-3 py-2">
      <!-- Origin: an edge's colour and stroke say which writer stored it, and
           the rows themselves are the filter, so the legend doubles as a way to
           strip one source off a busy canvas. A `mixed` edge bundles both writers,
           so it has nothing of its own to filter and is not a button. -->
      <section class="space-y-1.5">
        <div
          class="text-[11px] uppercase tracking-wide text-muted-foreground"
          :title="t('lineageGraph.filterOriginHint')"
        >
          {{ t("lineageGraph.originLabel") }}
        </div>
        <template v-for="item in origins" :key="item.origin">
          <button
            v-if="item.origin !== 'mixed'"
            type="button"
            class="flex w-full items-center gap-2 rounded px-1 py-0.5 text-left hover:bg-muted/60"
            :class="{ 'opacity-40': !isOriginDrawn(item.origin) }"
            :title="t(originHintKey(item.origin))"
            @click="toggleOrigin(item.origin)"
          >
            <OriginSwatch :origin="item.origin" />
            <span class="truncate">{{ t(originLabelKey(item.origin)) }}</span>
            <span class="ml-auto whitespace-nowrap tabular-nums text-muted-foreground">
              {{ t("lineageGraph.legendEdgeCount", { count: item.count }) }}
            </span>
          </button>
          <div
            v-else
            class="flex w-full items-center gap-2 rounded px-1 py-0.5"
            :title="t(originHintKey(item.origin))"
          >
            <OriginSwatch :origin="item.origin" />
            <span class="truncate">{{ t(originLabelKey(item.origin)) }}</span>
            <span class="ml-auto whitespace-nowrap tabular-nums text-muted-foreground">
              {{ t("lineageGraph.legendEdgeCount", { count: item.count }) }}
            </span>
          </div>
        </template>
      </section>

      <!-- Scope: two objects in different instances can share a name, so every
           node is marked with its instance's colour and named in full. -->
      <section v-if="scopes.length > 0" class="space-y-1.5">
        <div class="text-[11px] uppercase tracking-wide text-muted-foreground">
          {{ t("lineageGraph.legendScopes") }}
        </div>
        <div
          v-for="scope in scopes"
          :key="scope.key"
          class="flex items-center gap-2 px-1"
          :title="scope.key"
        >
          <span
            class="size-2.5 shrink-0 rounded-full"
            :style="{ backgroundColor: scope.color }"
          />
          <span class="truncate">{{ scope.label }}</span>
          <span class="ml-auto whitespace-nowrap tabular-nums text-muted-foreground">
            {{ t("lineageGraph.legendObjectCount", { count: scope.count }) }}
          </span>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ChevronDown, ChevronUp, ListTree } from "lucide-vue-next";
import { ref } from "vue";
import { useI18n } from "vue-i18n";
import OriginSwatch from "@/components/lineage/OriginSwatch.vue";
import {
  type LineageOrigin,
  originLabelKey,
  type RelationOrigin,
} from "@/lib/lineageOrigin";

export interface LegendScope {
  key: string;
  label: string;
  color: string;
  count: number;
}

export interface LegendOrigin {
  origin: LineageOrigin;
  count: number;
}

const props = defineProps<{
  /** The instances (and external namespaces) the drawn nodes belong to. */
  scopes: LegendScope[];
  /** Every origin on the canvas, with how many edges each drew. */
  origins: LegendOrigin[];
  /** The origins currently drawn; the rest are filtered out of the graph. */
  selected: RelationOrigin[];
}>();

const emit = defineEmits<{
  "update:selected": [origins: RelationOrigin[]];
}>();

const { t } = useI18n();
const collapsed = ref(false);

function originHintKey(origin: LineageOrigin): string {
  switch (origin) {
    case "sql":
      return "lineageGraph.originSqlHint";
    case "openlineage":
      return "lineageGraph.originOpenlineageHint";
    case "mixed":
      return "lineageGraph.originMixedHint";
  }
}

/** `mixed` is a bundle of both origins rather than a filter of its own, so it is
 * never dimmed and never toggles. */
function isOriginDrawn(origin: LineageOrigin): boolean {
  return origin === "mixed" || props.selected.includes(origin);
}

/** Toggling an origin needs a `RelationOrigin`; `mixed` is a bundle, not a filter. */
function toggleOrigin(origin: LineageOrigin) {
  if (origin === "mixed") {
    return;
  }

  const next: RelationOrigin[] = props.selected.includes(origin)
    ? props.selected.filter((item) => item !== origin)
    : [...props.selected, origin];

  // An empty filter would empty the canvas of edges and leave no row to bring
  // them back; the last source stays on.
  if (next.length === 0) {
    return;
  }

  emit("update:selected", next);
}
</script>
