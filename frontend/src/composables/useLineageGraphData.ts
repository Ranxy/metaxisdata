import { type Ref, ref } from "vue";
import { getLineage, getLineageCounts, type LineageCount } from "@/api/lineage";
import { type LineageDirection, parseExpandDepth } from "@/lib/lineageGraph";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  type ExternalDatasetInfo,
  type LineageRelation,
  LineageType,
} from "@/types/proto-es/v1/lineage_service_pb";
import { extractErrorMessage } from "@/utils/error";

export interface NodeLineageData {
  upstream: LineageRelation[];
  downstream: LineageRelation[];
  upstreamLoaded: boolean;
  downstreamLoaded: boolean;
}

export interface LineageGraphDataOptions {
  /** The metadata type a GUID should be fetched as. */
  metaTypeFor: (guid: string) => MetaType;
  /** Records the metadata types and statement labels the relations carry. */
  recordRelations: (relations: LineageRelation[]) => void;
  /** Cache the response's external dataset facts land in. */
  externalDatasetMap: Ref<Map<string, ExternalDatasetInfo>>;
  /** The nodes currently drawn, for the batched degree read. */
  drawnNodeIds: () => string[];
  /** Re-render after the degree read lands. */
  onCountsLoaded: () => void;
}

/**
 * The lineage canvas' data layer: what has been fetched, which directions and
 * fields are expanded, and the batched degree read.
 *
 * The page carried this next to its layout and interaction code, so the fetch
 * cache (a node's two directions are loaded independently, and selecting a node
 * must not expand it) could not be tested apart from the canvas. Everything here
 * is about data; rendering and routing stay in the page.
 */
export function useLineageGraphData(options: LineageGraphDataOptions) {
  // Which (node, direction) pairs the user has drawn into the graph. This is
  // deliberately separate from `NodeLineageData`'s loaded flags, which only mean
  // the data was fetched: selecting a node fetches both directions for the
  // detail panel without expanding anything, so a loaded node is not an expanded
  // one.
  const expandedDirections = ref<Set<string>>(new Set());
  // The field-scoped expansions, keyed like `expandedDirections` and holding,
  // per field, how many levels below it the walk was taken: 0 is a field whose
  // own relations are drawn and nothing past them, which is where a depth-1
  // expansion leaves its pivot. A direction is either expanded whole —
  // `expandedDirections` — or field by field, never both: a whole-node expansion
  // supersedes the fields recorded before it.
  const expandedColumns = ref<Map<string, Map<string, number>>>(new Map());
  // A request for one card to bring its field into view, counted per guid. The
  // action that opens a list bumps it, so the card reveals once for that action
  // instead of on every redraw — a redraw must not drag a reader back to a list
  // they scrolled away from.
  const revealRequest = ref<Map<string, number>>(new Map());
  const nodeDataMap = ref<Map<string, NodeLineageData>>(new Map());
  // Every visible node's degree, from the batched counts RPC: a node cannot be
  // labelled from its neighbours' relations, and fetching each node's relations
  // just to print two numbers downloads the whole graph. Kept apart from
  // `nodeDataMap`, which only holds the relations of expanded objects.
  const lineageCounts = ref<Map<string, LineageCount>>(new Map());
  const expandDepth = ref("1");

  function createEmptyNodeLineageData(): NodeLineageData {
    return {
      upstream: [],
      downstream: [],
      upstreamLoaded: false,
      downstreamLoaded: false,
    };
  }

  function getNodeLineageData(guid: string): NodeLineageData {
    return nodeDataMap.value.get(guid) ?? createEmptyNodeLineageData();
  }

  /** Whether the requested directions are already in `nodeDataMap`. */
  function isDirectionFetched(
    data: NodeLineageData,
    lineageType: LineageType
  ): boolean {
    switch (lineageType) {
      case LineageType.SOURCE:
        return data.upstreamLoaded;
      case LineageType.TARGET:
        return data.downstreamLoaded;
      default:
        return data.upstreamLoaded && data.downstreamLoaded;
    }
  }

  /** Whether the user has already drawn this direction's neighbours into the graph. */
  function isDirectionExpanded(
    guid: string,
    direction: LineageDirection
  ): boolean {
    return expandedDirections.value.has(expansionKey(guid, direction));
  }

  /** The fields this node's direction draws, for `directionView`. */
  function fieldScopesFor(
    guid: string,
    direction: LineageDirection
  ): Map<string, number> | undefined {
    return expandedColumns.value.get(expansionKey(guid, direction));
  }

  /**
   * Records that one field's lineage is drawn on this node's direction, as deep
   * as the walk took it. A direction the node itself has expanded draws all of
   * its relations already, so nothing is recorded there — a field scope only
   * exists while it is what narrows the canvas.
   */
  function addColumnExpansion(
    guid: string,
    direction: LineageDirection,
    column: string,
    depth: number
  ) {
    if (isDirectionExpanded(guid, direction)) {
      return;
    }
    const key = expansionKey(guid, direction);
    let fields = expandedColumns.value.get(key);
    if (!fields) {
      fields = new Map<string, number>();
      expandedColumns.value.set(key, fields);
    }
    fields.set(column, Math.max(fields.get(column) ?? 0, depth));
  }

  /**
   * The fields of one direction that expanding right now would still draw something
   * for: the field names a relation on this node's own side, and its walk has not
   * already been taken as deep as the depth control now asks. `null` when the
   * direction has not been fetched — not fetched is not nothing there, and
   * answering "nothing" would hide an item that works.
   */
  function expandableColumns(
    guid: string,
    direction: LineageDirection
  ): Set<string> | null {
    const data = getNodeLineageData(guid);
    const loaded =
      direction === "upstream" ? data.upstreamLoaded : data.downstreamLoaded;
    if (!loaded) {
      return null;
    }

    const depth = parseExpandDepth(expandDepth.value);
    const fields = new Set<string>();
    const scopes = fieldScopesFor(guid, direction);
    for (const relation of direction === "upstream"
      ? data.upstream
      : data.downstream) {
      const column =
        direction === "upstream"
          ? relation.targetColumn
          : relation.sourceColumn;
      // The field's own relations are the first level of its walk, so a field
      // already walked to `depth - 1` levels below it has nothing further to draw.
      if (column && (scopes?.get(column) ?? -1) < depth - 1) {
        fields.add(column);
      }
    }
    return fields;
  }

  /** Asks these cards to bring their field into view, once each. */
  function requestFieldReveal(guids: Iterable<string>) {
    for (const guid of guids) {
      revealRequest.value.set(guid, (revealRequest.value.get(guid) ?? 0) + 1);
    }
  }

  /** A GUID may contain `:`, so the direction is the prefix, not part of the key. */
  function expansionKey(guid: string, direction: LineageDirection): string {
    return `${direction}:${guid}`;
  }

  function directionToLineageType(direction: LineageDirection): LineageType {
    return direction === "upstream" ? LineageType.SOURCE : LineageType.TARGET;
  }

  async function fetchLineageForGuid(
    guid: string,
    lineageType: LineageType = LineageType.LINEAGE_TYPE_UNSPECIFIED
  ): Promise<NodeLineageData> {
    const existingData = getNodeLineageData(guid);
    if (isDirectionFetched(existingData, lineageType)) {
      return existingData;
    }

    const metaType = options.metaTypeFor(guid);

    try {
      const response = await getLineage({ guid, metaType, lineageType });
      const data: NodeLineageData = {
        upstream:
          lineageType === LineageType.TARGET
            ? existingData.upstream
            : response.relationsSource,
        downstream:
          lineageType === LineageType.SOURCE
            ? existingData.downstream
            : response.relationsTarget,
        upstreamLoaded:
          lineageType === LineageType.TARGET
            ? existingData.upstreamLoaded
            : true,
        downstreamLoaded:
          lineageType === LineageType.SOURCE
            ? existingData.downstreamLoaded
            : true,
      };
      nodeDataMap.value.set(guid, data);

      for (const ext of response.externalDatasets) {
        if (ext.guid && !options.externalDatasetMap.value.has(ext.guid)) {
          options.externalDatasetMap.value.set(ext.guid, ext);
        }
      }

      options.recordRelations(data.upstream);
      options.recordRelations(data.downstream);

      return data;
    } catch (e) {
      console.error(
        `Failed to fetch lineage for ${guid}:`,
        extractErrorMessage(e)
      );
      const empty: NodeLineageData = {
        upstream:
          lineageType === LineageType.TARGET ? existingData.upstream : [],
        downstream:
          lineageType === LineageType.SOURCE ? existingData.downstream : [],
        upstreamLoaded:
          lineageType === LineageType.TARGET
            ? existingData.upstreamLoaded
            : true,
        downstreamLoaded:
          lineageType === LineageType.SOURCE
            ? existingData.downstreamLoaded
            : true,
      };
      nodeDataMap.value.set(guid, empty);
      return empty;
    }
  }

  /**
   * Labels every node on the canvas that has no degree yet. One click can reveal
   * a whole layer, and the request is batched, so twenty new nodes still cost one
   * call — the server answers from two aggregates rather than one GetLineage per
   * node. A failure leaves those nodes unlabelled (their own relations, once
   * expanded, still supply the number) rather than failing the redraw.
   */
  async function ensureLineageCounts() {
    const missing = options
      .drawnNodeIds()
      .filter((guid) => !lineageCounts.value.has(guid));
    if (missing.length === 0) {
      return;
    }

    let fetched: Map<string, LineageCount>;
    try {
      fetched = await getLineageCounts(missing);
    } catch (e) {
      console.error(
        `Failed to load lineage counts for ${missing.length} nodes:`,
        extractErrorMessage(e)
      );
      return;
    }

    for (const [guid, count] of fetched) {
      lineageCounts.value.set(guid, count);
    }
    options.onCountsLoaded();
  }

  return {
    expandedDirections,
    expandedColumns,
    revealRequest,
    nodeDataMap,
    lineageCounts,
    expandDepth,
    getNodeLineageData,
    isDirectionExpanded,
    fieldScopesFor,
    addColumnExpansion,
    expandableColumns,
    requestFieldReveal,
    directionToLineageType,
    expansionKey,
    fetchLineageForGuid,
    ensureLineageCounts,
  };
}
