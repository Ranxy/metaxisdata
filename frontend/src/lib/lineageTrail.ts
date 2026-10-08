import type { LineageRelation } from "@/types/proto-es/v1/lineage_service_pb";
import type {
  ColumnFilter,
  LineageDirection,
  NodeLineageData,
} from "./lineageGraph";

/**
 * The trail a selected column runs through the drawn graph: its ancestors, its
 * descendants, and every relation on a path between them and it.
 *
 * It is walked as **two directed closures**, never as one connected component.
 * Upstream only ever follows a relation that *names the current pair as its
 * target*; downstream only ever follows one that names it as its **source**.
 * Merging the two into a single walk lets a hop go up and then back down, and the
 * column graph is dense enough for that to swallow everything: on the dev estate
 * the undirected component of `v_customer_360.customer_id` is 1302 pairs, where
 * its two directed closures are 2 upstream and 6 downstream.
 *
 * A relation whose far end names no column — a join key deciding which rows the
 * target receives, or a table-level edge — is part of the trail, and so is the
 * object it reaches, but the walk stops there: the relation does not say which of
 * that object's columns is affected.
 */

/** The pair separator: a GUID may contain `;`, a column may contain anything. */
const PAIR_SEPARATOR = "\u0000";

/** One field of one object: what both the trail walk and a field expansion move in. */
export interface ColumnPair {
  guid: string;
  column: string;
}

/** The identity of one pair, for the sets and maps a walk keeps it in. */
export function columnPairKey(guid: string, column: string): string {
  return `${guid}${PAIR_SEPARATOR}${column}`;
}

/** One side of the trail; the pivot itself is on neither. */
export interface FieldTrailSide {
  /** Objects on this side, the pivot excluded. */
  nodeIds: Set<string>;
  /** How many relations the trail crosses on this side. */
  relationCount: number;
}

export interface FieldTrail {
  /**
   * Whether a walk stopped at its cap instead of exhausting the drawn graph. The
   * caller says so rather than presenting a partial trail as the whole one.
   */
  truncated: boolean;
  /** Every object on the trail, the pivot included. */
  nodeIds: Set<string>;
  /** object guid -> the columns of that object that are on the trail. */
  columns: Map<string, Set<string>>;
  /** The `source->target` edge ids the trail runs through. */
  edgeIds: Set<string>;
  upstream: FieldTrailSide;
  downstream: FieldTrailSide;
}

/** The identity of one stored relation, across the two responses that carry it. */
function relationKey(relation: LineageRelation): string {
  return [
    relation.metaGuid,
    relation.sourceGuid,
    relation.sourceColumn,
    relation.targetGuid,
    relation.targetColumn,
    relation.relationType,
  ].join(PAIR_SEPARATOR);
}

function pairGuid(key: string): string {
  return key.slice(0, key.lastIndexOf(PAIR_SEPARATOR));
}

function pairColumn(key: string): string {
  return key.slice(key.lastIndexOf(PAIR_SEPARATOR) + 1);
}

interface WalkResult {
  /** The pairs this side of the trail stands on, the pivot included. */
  pairs: Set<string>;
  /** The relations it crossed, by identity. */
  relations: Map<string, LineageRelation>;
  /** Objects it reached through a relation that names no column. */
  objects: Set<string>;
}

/**
 * One directed closure. `index` maps a pair to the relations that lead *into* it
 * (upstream) or *out of* it (downstream), so the direction is chosen by the index
 * and this walk only ever moves one way.
 */
function walk(
  index: ReadonlyMap<string, LineageRelation[]>,
  start: ColumnFilter,
  onward: (relation: LineageRelation) => { guid: string; column: string },
  budget: { left: number; hit: boolean }
): WalkResult {
  const pairs = new Set<string>([columnPairKey(start.guid, start.column)]);
  const relations = new Map<string, LineageRelation>();
  const objects = new Set<string>();
  const queue: Array<{ guid: string; column: string }> = [start];

  let head = 0;
  while (head < queue.length) {
    if (budget.left <= 0) {
      budget.hit = true;
      break;
    }
    const current = queue[head++];
    for (const relation of index.get(
      columnPairKey(current.guid, current.column)
    ) ?? []) {
      const key = relationKey(relation);
      if (!relations.has(key)) {
        relations.set(key, relation);
        budget.left -= 1;
      }

      const next = onward(relation);
      if (!next.column) {
        // The relation reaches the object without naming a column of it, so the
        // object is on the trail but the walk cannot continue through it.
        objects.add(next.guid);
        continue;
      }
      const nextKey = columnPairKey(next.guid, next.column);
      if (pairs.has(nextKey)) {
        continue;
      }
      pairs.add(nextKey);
      queue.push(next);
    }
  }

  return { pairs, relations, objects };
}

/**
 * The trail of one selected column, over the relations the graph has drawn.
 *
 * `view` is the expanded graph the edges are built from, so a relation nobody has
 * expanded is not walked: the trail can only show a path the canvas can show.
 * `validNodeIds` matches the edge builder's — a relation with an end that is not
 * drawn cannot carry the trail anywhere.
 */
export function collectFieldTrail(
  view: Map<string, NodeLineageData>,
  options: {
    pivot: ColumnFilter;
    validNodeIds: ReadonlySet<string>;
    /**
     * How many relations the two walks may cross before they give up. It exists so
     * a pathological fan-out degrades into an honest `truncated` rather than a
     * frozen tab; on real data a trail is a dozen relations.
     */
    maxRelations?: number;
  }
): FieldTrail {
  const incoming = new Map<string, LineageRelation[]>();
  const outgoing = new Map<string, LineageRelation[]>();
  const index = (
    map: Map<string, LineageRelation[]>,
    key: string,
    relation: LineageRelation
  ) => {
    const list = map.get(key);
    if (list) {
      list.push(relation);
    } else {
      map.set(key, [relation]);
    }
  };

  for (const [, data] of view) {
    // A relation is reported by both of its ends — as the source's downstream and
    // as the target's upstream — so it is indexed twice and deduplicated by
    // identity where it is counted.
    for (const relation of [...data.upstream, ...data.downstream]) {
      if (
        !options.validNodeIds.has(relation.sourceGuid) ||
        !options.validNodeIds.has(relation.targetGuid)
      ) {
        continue;
      }
      index(
        outgoing,
        columnPairKey(relation.sourceGuid, relation.sourceColumn),
        relation
      );
      index(
        incoming,
        columnPairKey(relation.targetGuid, relation.targetColumn),
        relation
      );
    }
  }

  const budget = { left: options.maxRelations ?? 2000, hit: false };
  const upstream = walk(
    incoming,
    options.pivot,
    (relation) => ({
      guid: relation.sourceGuid,
      column: relation.sourceColumn,
    }),
    budget
  );
  const downstream = walk(
    outgoing,
    options.pivot,
    (relation) => ({
      guid: relation.targetGuid,
      column: relation.targetColumn,
    }),
    budget
  );

  const trail: FieldTrail = {
    truncated: budget.hit,
    nodeIds: new Set(),
    columns: new Map(),
    edgeIds: new Set(),
    upstream: side(upstream, options.pivot),
    downstream: side(downstream, options.pivot),
  };

  for (const result of [upstream, downstream]) {
    for (const guid of result.objects) {
      trail.nodeIds.add(guid);
    }
    for (const key of result.pairs) {
      const guid = pairGuid(key);
      trail.nodeIds.add(guid);
      const columns = trail.columns.get(guid);
      const column = pairColumn(key);
      if (columns) {
        columns.add(column);
      } else {
        trail.columns.set(guid, new Set([column]));
      }
    }
    for (const relation of result.relations.values()) {
      trail.edgeIds.add(`${relation.sourceGuid}->${relation.targetGuid}`);
    }
  }

  return trail;
}

/** One hop of a field expansion, before the walk decides what to do with it. */
export interface ColumnStep {
  /** The pairs the walk goes on with: the far object's own field. */
  pairs: ColumnPair[];
  /**
   * The far objects this hop draws whose relation names no field of theirs — a join
   * key, a table-level edge. They are the field's lineage and they are on the canvas,
   * but there is no field to carry the walk on with.
   */
  objects: string[];
}

/**
 * One hop of a field expansion: the fields this pair reaches in `direction` through
 * relations that name its own field. `data` is the loaded lineage of `pair.guid`, so
 * the walk only sees relations the server has answered with.
 *
 * It is the same step `collectFieldTrail` takes, and it stops the same way: a relation
 * that reaches the far object without naming a field of it ends the walk there.
 */
export function columnStep(
  data: NodeLineageData,
  pair: ColumnPair,
  direction: LineageDirection
): ColumnStep {
  const relations = direction === "upstream" ? data.upstream : data.downstream;
  const pairs: ColumnPair[] = [];
  const objects: string[] = [];
  for (const relation of relations) {
    // The server answers with this node's own relations, but the pair is the contract:
    // a relation belongs to this hop only when this node is the end it names.
    const near =
      direction === "upstream"
        ? { guid: relation.targetGuid, column: relation.targetColumn }
        : { guid: relation.sourceGuid, column: relation.sourceColumn };
    if (near.guid !== pair.guid || near.column !== pair.column) {
      continue;
    }
    const far =
      direction === "upstream"
        ? { guid: relation.sourceGuid, column: relation.sourceColumn }
        : { guid: relation.targetGuid, column: relation.targetColumn };
    if (far.column) {
      pairs.push(far);
    } else {
      objects.push(far.guid);
    }
  }
  return { pairs, objects };
}

/** One pair a field expansion expanded, and how far below it the walk went. */
export interface ColumnWalkStep {
  pair: ColumnPair;
  /**
   * The levels the walk still had below this pair, the pivot of a depth-1 expansion
   * being 0. It is what tells "already drawn, nothing more to gain" from "drawn, and
   * a deeper expansion would still add a hop".
   */
  depth: number;
}

export interface ColumnWalk {
  /** Every pair the walk expanded, the pivot first. */
  walked: ColumnWalkStep[];
  /** Every object the walk draws, the pairs' own objects included. */
  objects: Set<string>;
}

/**
 * The field expansion of one node's direction, level by level: `load` is the caller's
 * fetch, so a node's relations are in hand before the level below it is walked, and a
 * caller that has already fetched them pays only the cache hit.
 *
 * Deduplicated across the whole walk, so a diamond is walked once and a pair reached at
 * a shallower level is not walked again from a deeper one.
 */
export async function walkColumnExpansion(options: {
  start: ColumnFilter;
  direction: LineageDirection;
  /** How many levels to walk, the pivot's own relations being the first. */
  depth: number;
  load: (guid: string) => Promise<NodeLineageData>;
}): Promise<ColumnWalk> {
  const walked: ColumnWalkStep[] = [];
  const objects = new Set<string>([options.start.guid]);
  const seen = new Set<string>([
    columnPairKey(options.start.guid, options.start.column),
  ]);
  let frontier: ColumnPair[] = [
    { guid: options.start.guid, column: options.start.column },
  ];

  for (let level = 0; level < options.depth && frontier.length > 0; level++) {
    const pending = frontier;
    for (const pair of pending) {
      walked.push({ pair, depth: options.depth - 1 - level });
    }

    const loaded = await Promise.all(
      pending.map((pair) => options.load(pair.guid))
    );

    const next: ColumnPair[] = [];
    for (let index = 0; index < pending.length; index++) {
      const step = columnStep(loaded[index], pending[index], options.direction);
      for (const guid of step.objects) {
        objects.add(guid);
      }
      for (const pair of step.pairs) {
        const key = columnPairKey(pair.guid, pair.column);
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
        // Every pair the walk reaches is drawn, whether or not the walk goes on
        // through it: the last level's neighbours are on the canvas too.
        objects.add(pair.guid);
        next.push(pair);
      }
    }
    frontier = next;
  }

  return { walked, objects };
}

function side(result: WalkResult, pivot: ColumnFilter): FieldTrailSide {
  const nodeIds = new Set<string>();
  for (const key of result.pairs) {
    const guid = pairGuid(key);
    if (
      guid !== pivot.guid ||
      key !== columnPairKey(pivot.guid, pivot.column)
    ) {
      nodeIds.add(guid);
    }
  }
  for (const guid of result.objects) {
    nodeIds.add(guid);
  }
  return { nodeIds, relationCount: result.relations.size };
}
