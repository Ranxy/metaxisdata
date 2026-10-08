import { type Edge, MarkerType } from "@vue-flow/core";
import {
  type LineageRelation,
  RelationType,
} from "@/types/proto-es/v1/lineage_service_pb";
import {
  LINEAGE_ORIGIN_DASH,
  type LineageOrigin,
  originColor,
  type RelationOrigin,
  relationOrigin,
} from "./lineageOrigin";

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

/** The direction a lineage expansion walks in. */
export type LineageDirection = "upstream" | "downstream";

export interface NodePosition {
  x: number;
  y: number;
}

/**
 * How many distinct objects one node is connected to, at table level: the
 * objects its column-level relations name. A join on five columns is one
 * upstream object rather than five, which is what the graph labels the node
 * with — and what the server's counts RPC returns for a node whose relations
 * have not been fetched.
 */
export function distinctRelationCounts(data: NodeLineageData): {
  upstream: number;
  downstream: number;
} {
  return {
    upstream: new Set(data.upstream.map((rel) => rel.sourceGuid)).size,
    downstream: new Set(data.downstream.map((rel) => rel.targetGuid)).size,
  };
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
 * The relations of one direction that a field-scoped expansion may draw: only
 * those naming one of the expanded fields *on this node's side* — the target's
 * field for an upstream relation, the source's for a downstream one. A relation
 * that names another field is not this field's lineage, so revealing its far
 * object would put an object on the canvas that the field never reaches.
 */
export function fieldScopedRelations(
  relations: readonly LineageRelation[],
  direction: LineageDirection,
  fields: ReadonlySet<string>
): LineageRelation[] {
  return relations.filter((relation) =>
    fields.has(
      direction === "upstream" ? relation.targetColumn : relation.sourceColumn
    )
  );
}

/**
 * The Vue Flow edges for every loaded relation whose two ends are known nodes.
 *
 * One edge per object pair, coloured and patterned by which writer stored its
 * relations, with an arrowhead for the direction. `highlightedEdgeIds` is the
 * selected column's trail (see `lineageTrail`): the edges on it stay at full
 * strength and every other edge is dimmed. A null set dims nothing — an empty
 * trail is a real answer, and fading the whole canvas for a field whose relations
 * merely are not drawn would read as "this field has no lineage".
 *
 * An origin filter hides the edges of the origins it excludes, including trail
 * edges: the trail is computed over the drawn relations before that filter, so
 * hiding a source breaks the trail visibly rather than quietly rewriting it.
 */
export function buildLineageEdges(
  nodeDataMap: Map<string, NodeLineageData>,
  options: {
    validNodeIds: ReadonlySet<string>;
    /** The trail's edges; every other edge is dimmed. Null dims nothing. */
    highlightedEdgeIds?: ReadonlySet<string> | null;
    /** Origins to keep; `null` keeps every origin. A bundled edge survives any
     * filter it shares at least one origin with. */
    originFilter?: ReadonlySet<RelationOrigin> | null;
  }
): Edge<{ origin: LineageOrigin }>[] {
  const highlighted = options.highlightedEdgeIds ?? null;
  const dimsOthers = highlighted !== null;
  const originFilter = options.originFilter ?? null;
  const edges: Edge<{ origin: LineageOrigin }>[] = [];
  const seen = new Set<string>();

  const add = (
    id: string,
    source: string,
    target: string,
    rel: LineageRelation,
    origins: ReadonlySet<RelationOrigin>
  ) => {
    if (
      seen.has(id) ||
      !options.validNodeIds.has(source) ||
      !options.validNodeIds.has(target)
    ) {
      return;
    }
    if (
      originFilter &&
      ![...origins].some((origin) => originFilter.has(origin))
    ) {
      return;
    }
    seen.add(id);

    // Bundling relations of one pair can yield both writers, and the edge says
    // so rather than claiming the origin of whichever relation was stored first.
    const origin: LineageOrigin = origins.size > 1 ? "mixed" : [...origins][0];
    const onTrail = highlighted?.has(id) ?? false;
    const dimmed = dimsOthers && !onTrail;

    edges.push({
      id,
      source,
      target,
      data: { origin },
      animated: !dimmed && origin !== "sql",
      markerEnd: {
        type: MarkerType.ArrowClosed,
        color: dimmed
          ? "hsl(var(--muted-foreground) / 0.4)"
          : originColor(origin),
        width: 14,
        height: 14,
      },
      style: {
        stroke: dimmed
          ? "hsl(var(--muted-foreground) / 0.2)"
          : onTrail
            ? originColor(origin)
            : // Faded enough to sit behind the trail, strong enough to stay above
              // 3:1 against the canvas: the tokens carry the contrast, and an edge
              // must not lean on its opacity to stay legible.
              originColor(origin, 0.85),
        strokeWidth: onTrail ? 3 : 2,
        strokeDasharray: LINEAGE_ORIGIN_DASH[origin],
      },
      label: rel.relationType === RelationType.DIRECT ? "" : "T",
      labelStyle: {
        fontSize: "10px",
        fill: "hsl(var(--muted-foreground))",
      },
    });
  };

  // The relations of one pair are bundled into one edge, so their origins are
  // collected first: whether the bundle is `sql`, `openlineage` or both is a
  // property of the pair, not of the relation the loop happens to meet first.
  const bundles = new Map<
    string,
    {
      source: string;
      target: string;
      rel: LineageRelation;
      origins: Set<RelationOrigin>;
    }
  >();
  const bundle = (
    id: string,
    source: string,
    target: string,
    rel: LineageRelation
  ) => {
    const existing = bundles.get(id);
    if (existing) {
      existing.origins.add(relationOrigin(rel));
      return;
    }
    bundles.set(id, {
      source,
      target,
      rel,
      origins: new Set([relationOrigin(rel)]),
    });
  };

  for (const [guid, data] of nodeDataMap) {
    for (const rel of data.upstream) {
      bundle(`${rel.sourceGuid}->${guid}`, rel.sourceGuid, guid, rel);
    }
    for (const rel of data.downstream) {
      bundle(`${guid}->${rel.targetGuid}`, guid, rel.targetGuid, rel);
    }
  }

  for (const [id, item] of bundles) {
    add(id, item.source, item.target, item.rel, item.origins);
  }

  return edges;
}
