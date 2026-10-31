import { type ComputedRef, computed, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

export interface LineageSelectionOptions {
  /** The graph's root object; its details need no pre-fetch. */
  currentGuid: ComputedRef<string>;
  /** Loads a node's relations into the detail-panel cache. */
  fetchLineage: (guid: string) => Promise<void>;
  /** Re-render after the selection changed something the canvas shows. */
  onChanged: () => void;
}

/**
 * The lineage canvas' selection: which node the detail panel names and which
 * field the trail follows.
 *
 * The selected node lives in the URL (`?node=<guid>`), so a detail column and its
 * field trail survive a reload and a shared link; the selected field lives in
 * memory because it depends on what is currently drawn. A node click prefetches
 * both directions for the panel without expanding the graph, and a field click
 * selects its node first for the same reason.
 */
export function useLineageSelection(options: LineageSelectionOptions) {
  const route = useRoute();
  const router = useRouter();

  const selectedColumnGuid = ref<string | null>(null);
  const selectedColumnName = ref<string | null>(null);

  const selectedNodeGuid = computed(() => {
    const node = route.query.node;
    return typeof node === "string" && node.length > 0 ? node : null;
  });

  const selectedColumnContext = computed(() => {
    if (
      selectedColumnGuid.value !== selectedNodeGuid.value ||
      !selectedColumnName.value
    ) {
      return null;
    }
    return selectedColumnName.value;
  });

  function setSelectedNode(guid: string | null) {
    const nextQuery = { ...route.query };
    if (guid) {
      nextQuery.node = guid;
    } else {
      delete nextQuery.node;
    }
    void router.replace({ query: nextQuery });
  }

  function clearColumnSelection() {
    selectedColumnGuid.value = null;
    selectedColumnName.value = null;
    options.onChanged();
  }

  async function handleSelectNode(guid: string) {
    if (selectedNodeGuid.value === guid) {
      closeSelectedNode();
      return;
    }
    setSelectedNode(guid);

    // Pre-fetch lineage data for non-root nodes so the drawer shows accurate
    // upstream/downstream counts and related runs immediately, without expanding
    // the graph (the user can do that via "Expand"). Every non-root node is
    // asked, not just the ones the map has never seen: a node the user expanded
    // in one direction only holds an empty list for the other, and the detail
    // panel would otherwise report that empty direction as "no lineage".
    // `fetchLineage` is the cache — it returns at once when both directions are
    // already loaded.
    if (guid !== options.currentGuid.value) {
      await options.fetchLineage(guid);
      // The fetch marks both directions as loaded but deliberately leaves the
      // graph alone, so the node still renders the pre-fetch state. Refresh the
      // node data (counts, and the expand affordances, which key off the
      // expansion set rather than the fetch flags) without adding neighbours.
      options.onChanged();
    }
  }

  function closeSelectedNode() {
    setSelectedNode(null);
  }

  async function handleSelectColumn(guid: string, column: string) {
    // Toggle off if the same field is clicked again.
    if (
      selectedColumnGuid.value === guid &&
      selectedColumnName.value === column
    ) {
      clearColumnSelection();
      return;
    }

    await markSelectedColumn(guid, column);
    // The trail is walked by the page's redraw, over the same drawn graph the
    // edges come from — one implementation for both, so a column can never be
    // highlighted without the edge that carries it.
    options.onChanged();
  }

  /**
   * Points the detail panel and the trail at one field, without redrawing: the
   * caller decides which redraw follows, so an action that expands a field only
   * pays for one. A field click has to do what a node click does — select the
   * node and make sure its relations are loaded. Otherwise the detail panel
   * stays empty (and with it the field's trail summary) for every node whose
   * relations were never fetched, which is every node the user has not expanded
   * or clicked before.
   */
  async function markSelectedColumn(guid: string, column: string) {
    if (selectedNodeGuid.value !== guid) {
      setSelectedNode(guid);
    }
    selectedColumnGuid.value = guid;
    selectedColumnName.value = column;

    if (guid !== options.currentGuid.value) {
      await options.fetchLineage(guid);
    }
  }

  return {
    selectedNodeGuid,
    selectedColumnGuid,
    selectedColumnName,
    selectedColumnContext,
    setSelectedNode,
    closeSelectedNode,
    clearColumnSelection,
    handleSelectNode,
    handleSelectColumn,
    markSelectedColumn,
  };
}
