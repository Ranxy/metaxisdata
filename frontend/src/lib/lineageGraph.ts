import type { Edge } from "@vue-flow/core";
import {
  type LineageRelation,
  RelationType,
} from "@/types/proto-es/v1/lineage_service_pb";

/**
 * The lineage graph engine: which layer a node sits in, where it is drawn, and
 * which edges exist (and how the selected column highlights them).
 *
 * It is a pure module so both graph updates share one implementation — the page
 * used to carry a copy of the column-edge collection and the edge construction in
 * each of its two update paths.
 */

/** One node's loaded lineage; the page keeps one entry per fetched GUID. */
export interface NodeLineageData {
  upstream: LineageRelation[];
  downstream: LineageRelation[];
  upstreamLoaded: boolean;
  downstreamLoaded: boolean;
}

/** A column the user selected, which filters the graph to its own lineage. */
export interface ColumnFilter {
  guid: string;
  column: string;
}

export interface NodePosition {
  x: number;
  y: number;
}

/** Horizontal distance between two layers. */
export const LAYER_GAP_X = 280;

const BASE_NODE_HEIGHT = 120;
const COLUMN_ROW_HEIGHT = 24;
const MAX_FIELDS_HEIGHT = 200;
const NODE_GAP_Y = 20;

/** The node's drawn height, which depends on whether its field list is open. */
export function nodeHeight(
  columnCount: number,
  fieldsVisible: boolean
): number {
  if (!fieldsVisible) {
    return BASE_NODE_HEIGHT;
  }
  const fields = Math.min(columnCount * COLUMN_ROW_HEIGHT, MAX_FIELDS_HEIGHT);
  return BASE_NODE_HEIGHT + fields;
}

/**
 * A layer per node, by breadth-first walk from the root: upstream neighbours land
 * one layer to the left, downstream one to the right. Every other GUID the loaded
 * relations mention — and any node the walk could not reach — sits with the root.
 */
export function assignLayers(
  rootGuid: string,
  nodeDataMap: Map<string, NodeLineageData>
): Map<string, number> {
  const upstreamOf = new Map<string, Set<string>>();
  const downstreamOf = new Map<string, Set<string>>();
  const allGuids = new Set<string>([rootGuid]);

  for (const [guid, data] of nodeDataMap) {
    allGuids.add(guid);
    if (data.upstream.length > 0 && !upstreamOf.has(guid)) {
      upstreamOf.set(guid, new Set());
    }
    if (data.downstream.length > 0 && !downstreamOf.has(guid)) {
      downstreamOf.set(guid, new Set());
    }
    for (const rel of data.upstream) {
      upstreamOf.get(guid)?.add(rel.sourceGuid);
      allGuids.add(rel.sourceGuid);
    }
    for (const rel of data.downstream) {
      downstreamOf.get(guid)?.add(rel.targetGuid);
      allGuids.add(rel.targetGuid);
    }
  }

  const layers = new Map<string, number>();
  layers.set(rootGuid, 0);
  const queue = [rootGuid];
  let head = 0;

  while (head < queue.length) {
    const current = queue[head++];
    const layer = layers.get(current) ?? 0;
    for (const upstream of upstreamOf.get(current) ?? []) {
      if (!layers.has(upstream)) {
        layers.set(upstream, layer - 1);
        queue.push(upstream);
      }
    }
    for (const downstream of downstreamOf.get(current) ?? []) {
      if (!layers.has(downstream)) {
        layers.set(downstream, layer + 1);
        queue.push(downstream);
      }
    }
  }

  for (const guid of allGuids) {
    if (!layers.has(guid)) {
      layers.set(guid, 0);
    }
  }

  return layers;
}

/**
 * Positions every layered node: `x` follows the layer (the leftmost layer becomes
 * 0) and `y` stacks the layer's nodes in the order `layers` lists them.
 */
export function layoutNodes(
  layers: Map<string, number>,
  heightOf: (guid: string) => number
): Map<string, NodePosition> {
  const byLayer = new Map<number, string[]>();
  for (const [guid, layer] of layers) {
    const group = byLayer.get(layer);
    if (group) {
      group.push(guid);
    } else {
      byLayer.set(layer, [guid]);
    }
  }

  const minLayer = Math.min(...byLayer.keys());
  const positions = new Map<string, NodePosition>();
  for (const [layer, guids] of byLayer) {
    const x = (layer - minLayer) * LAYER_GAP_X;
    let y = 0;
    for (const guid of guids) {
      positions.set(guid, { x, y });
      y += heightOf(guid) + NODE_GAP_Y;
    }
  }
  return positions;
}

/**
 * The ids of the edges whose relation has the selected column on either end, in
 * either direction. Only those edges stay highlighted; the rest are dimmed.
 */
export function collectColumnEdgeIds(
  nodeDataMap: Map<string, NodeLineageData>,
  filter: ColumnFilter | null
): Set<string> {
  const edgeIds = new Set<string>();
  if (!filter) {
    return edgeIds;
  }

  const touches = (guid: string, column: string) =>
    guid === filter.guid && column === filter.column;

  for (const [guid, data] of nodeDataMap) {
    for (const rel of data.upstream) {
      if (
        touches(rel.targetGuid, rel.targetColumn) ||
        touches(rel.sourceGuid, rel.sourceColumn)
      ) {
        edgeIds.add(`${rel.sourceGuid}->${guid}`);
      }
    }
    for (const rel of data.downstream) {
      if (
        touches(rel.sourceGuid, rel.sourceColumn) ||
        touches(rel.targetGuid, rel.targetColumn)
      ) {
        edgeIds.add(`${guid}->${rel.targetGuid}`);
      }
    }
  }
  return edgeIds;
}

/**
 * The Vue Flow edges for every loaded relation whose two ends are known nodes.
 * Relations that are not `DIRECT` carry a transformation mark, and while a column
 * is selected every unrelated edge is dimmed — including when nothing matches it.
 */
export function buildLineageEdges(
  nodeDataMap: Map<string, NodeLineageData>,
  options: {
    validNodeIds: ReadonlySet<string>;
    columnFilter: ColumnFilter | null;
  }
): Edge[] {
  const columnEdgeIds = collectColumnEdgeIds(nodeDataMap, options.columnFilter);
  const hasColumnFilter = options.columnFilter !== null;
  const edges: Edge[] = [];
  const seen = new Set<string>();

  const add = (
    id: string,
    source: string,
    target: string,
    rel: LineageRelation
  ) => {
    if (
      seen.has(id) ||
      !options.validNodeIds.has(source) ||
      !options.validNodeIds.has(target)
    ) {
      return;
    }
    seen.add(id);
    const highlighted = columnEdgeIds.has(id);
    const dimmed = hasColumnFilter && !highlighted;
    edges.push({
      id,
      source,
      target,
      animated: !dimmed,
      style: {
        stroke: dimmed
          ? "hsl(var(--muted-foreground) / 0.2)"
          : highlighted
            ? "hsl(var(--primary))"
            : "hsl(var(--primary) / 0.6)",
        strokeWidth: highlighted ? 3 : 2,
      },
      label: rel.relationType === RelationType.DIRECT ? "" : "T",
      labelStyle: {
        fontSize: "10px",
        fill: "hsl(var(--muted-foreground))",
      },
    });
  };

  for (const [guid, data] of nodeDataMap) {
    for (const rel of data.upstream) {
      add(`${rel.sourceGuid}->${guid}`, rel.sourceGuid, guid, rel);
    }
    for (const rel of data.downstream) {
      add(`${guid}->${rel.targetGuid}`, guid, rel.targetGuid, rel);
    }
  }

  return edges;
}
