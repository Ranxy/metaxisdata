import { describe, expect, it } from "vitest";
import {
  type LineageRelation,
  RelationType,
} from "@/types/proto-es/v1/lineage_service_pb";
import type { NodeLineageData } from "./lineageGraph";
import {
  collectFieldTrail,
  columnStep,
  walkColumnExpansion,
} from "./lineageTrail";

/** A column-level relation, the shape the analyzers store. */
function rel(
  sourceGuid: string,
  sourceColumn: string,
  targetGuid: string,
  targetColumn: string,
  overrides: Partial<LineageRelation> = {}
): LineageRelation {
  return {
    metaGuid: targetGuid,
    metaType: 5,
    sourceGuid,
    sourceColumn,
    targetGuid,
    targetColumn,
    relationType: RelationType.DIRECT,
    ...overrides,
  } as LineageRelation;
}

/**
 * A graph from a list of nodes, each naming the relations it reports. A relation
 * is reported by both of its ends in the real graph, so `linked` wires it the
 * same way: the source's downstream and the target's upstream.
 */
function graph(
  nodes: Record<string, LineageRelation[]>
): Map<string, NodeLineageData> {
  const map = new Map<string, NodeLineageData>();
  for (const [guid, relations] of Object.entries(nodes)) {
    map.set(guid, {
      upstream: relations.filter((r) => r.targetGuid === guid),
      downstream: relations.filter((r) => r.sourceGuid === guid),
      upstreamLoaded: true,
      downstreamLoaded: true,
    });
  }
  return map;
}

function trailOf(
  nodes: Record<string, LineageRelation[]>,
  pivot: { guid: string; column: string },
  maxRelations?: number
) {
  const map = graph(nodes);
  return collectFieldTrail(map, {
    pivot,
    validNodeIds: new Set(map.keys()),
    maxRelations,
  });
}

describe("collectFieldTrail", () => {
  it("follows ancestors upstream and descendants downstream, hop after hop", () => {
    // a -> b -> c -> d, all on the same column name.
    const chain = [
      rel("a", "id", "b", "id"),
      rel("b", "id", "c", "id"),
      rel("c", "id", "d", "id"),
    ];
    const trail = trailOf(
      {
        a: [chain[0]],
        b: [chain[0], chain[1]],
        c: [chain[1], chain[2]],
        d: [chain[2]],
      },
      { guid: "b", column: "id" }
    );

    expect([...trail.nodeIds].sort()).toEqual(["a", "b", "c", "d"]);
    expect([...trail.edgeIds].sort()).toEqual(["a->b", "b->c", "c->d"]);
    // The pivot is on neither side, and each side is counted from it.
    expect([...trail.upstream.nodeIds]).toEqual(["a"]);
    expect([...trail.downstream.nodeIds].sort()).toEqual(["c", "d"]);
    expect(trail.upstream.relationCount).toBe(1);
    expect(trail.downstream.relationCount).toBe(2);
    expect(trail.truncated).toBe(false);
  });

  it("keeps the trail directed: a sibling column is not on it", () => {
    // a.id -> b.id, and a.name -> b.name. Upstream from b.id is a.id only: an
    // undirected walk would fall back down into a.name and then b.name.
    const relations = [
      rel("a", "id", "b", "id"),
      rel("a", "name", "b", "name"),
    ];
    const trail = trailOf(
      { a: relations, b: relations },
      {
        guid: "b",
        column: "id",
      }
    );

    expect([...trail.columns.get("a")!]).toEqual(["id"]);
    expect([...trail.columns.get("b")!]).toEqual(["id"]);
    expect([...trail.edgeIds]).toEqual(["a->b"]);
    // Both relations connect a and b, so the edge set cannot tell them apart;
    // the relation count can.
    expect(trail.upstream.relationCount).toBe(1);
  });

  it("walks the pivot's upstream and downstream independently through a cycle", () => {
    // a -> b -> c -> a, so every pair is both an ancestor and a descendant.
    const cycle = [
      rel("a", "id", "b", "id"),
      rel("b", "id", "c", "id"),
      rel("c", "id", "a", "id"),
    ];
    const trail = trailOf(
      { a: cycle, b: cycle, c: cycle },
      {
        guid: "a",
        column: "id",
      }
    );

    expect([...trail.nodeIds].sort()).toEqual(["a", "b", "c"]);
    expect([...trail.edgeIds].sort()).toEqual(["a->b", "b->c", "c->a"]);
    expect(trail.truncated).toBe(false);
  });

  it("collects both arms of a diamond without visiting either twice", () => {
    const relations = [
      rel("a", "id", "b", "id"),
      rel("a", "id", "c", "id"),
      rel("b", "id", "d", "id"),
      rel("c", "id", "d", "id"),
    ];
    const trail = trailOf(
      { a: relations, b: relations, c: relations, d: relations },
      {
        guid: "a",
        column: "id",
      }
    );

    expect([...trail.edgeIds].sort()).toEqual(["a->b", "a->c", "b->d", "c->d"]);
    expect(trail.downstream.relationCount).toBe(4);
  });

  it("stops at a relation that names no column, but keeps its object and edge", () => {
    // A join key: the source column decides which rows the target receives, and
    // the relation has no target column to continue through.
    const join = rel("v", "order_id", "fact", "", {
      relationType: RelationType.JOIN,
    });
    const beyond = rel("fact", "order_id", "report", "order_id");
    const trail = trailOf(
      { v: [join], fact: [join, beyond], report: [beyond] },
      { guid: "v", column: "order_id" }
    );

    expect([...trail.edgeIds]).toEqual(["v->fact"]);
    expect(trail.nodeIds.has("fact")).toBe(true);
    expect(trail.nodeIds.has("report")).toBe(false);
    // The influenced object carries no column of the trail.
    expect(trail.columns.has("fact")).toBe(false);
    expect([...trail.downstream.nodeIds]).toEqual(["fact"]);
  });

  it("ignores a relation whose ends are not both drawn", () => {
    const relation = rel("a", "id", "b", "id");
    const map = graph({ a: [relation], b: [relation] });
    const trail = collectFieldTrail(map, {
      pivot: { guid: "b", column: "id" },
      // `a` is not on the canvas, so the relation cannot carry the trail.
      validNodeIds: new Set(["b"]),
    });

    expect(trail.edgeIds.size).toBe(0);
    expect([...trail.nodeIds]).toEqual(["b"]);
    expect(trail.upstream.relationCount).toBe(0);
  });

  it("counts one stored relation once, however many times it is reported", () => {
    const relation = rel("a", "id", "b", "id");
    // The source reports it downstream and the target upstream: same row twice.
    const trail = trailOf(
      { a: [relation], b: [relation] },
      {
        guid: "a",
        column: "id",
      }
    );
    expect(trail.downstream.relationCount).toBe(1);
    expect([...trail.edgeIds]).toEqual(["a->b"]);
  });

  it("counts the two writers of one relation apart", () => {
    const same = {
      sourceGuid: "a",
      sourceColumn: "id",
      targetGuid: "b",
      targetColumn: "id",
    };
    const trail = trailOf(
      {
        a: [
          rel("a", "id", "b", "id", { ...same, metaGuid: "view", metaType: 5 }),
          rel("a", "id", "b", "id", {
            ...same,
            metaGuid: "run",
            metaType: 100,
          }),
        ],
        b: [
          rel("a", "id", "b", "id", { ...same, metaGuid: "view", metaType: 5 }),
          rel("a", "id", "b", "id", {
            ...same,
            metaGuid: "run",
            metaType: 100,
          }),
        ],
      },
      { guid: "a", column: "id" }
    );

    expect(trail.downstream.relationCount).toBe(2);
    expect([...trail.edgeIds]).toEqual(["a->b"]);
  });

  it("reports a trail it had to cut short", () => {
    const relations = [
      rel("a", "id", "b", "id"),
      rel("b", "id", "c", "id"),
      rel("c", "id", "d", "id"),
    ];
    const trail = trailOf(
      {
        a: [relations[0]],
        b: relations.slice(0, 2),
        c: relations.slice(1),
        d: [relations[2]],
      },
      { guid: "a", column: "id" },
      // Two relations fit, the third does not.
      2
    );

    expect(trail.truncated).toBe(true);
    expect(trail.downstream.relationCount).toBe(2);
    expect(trail.nodeIds.has("d")).toBe(false);
  });

  it("keeps only the pivot when nothing on the canvas carries the column", () => {
    const relation = rel("a", "other", "b", "other");
    const trail = trailOf(
      { a: [relation], b: [relation] },
      {
        guid: "b",
        column: "id",
      }
    );

    // An empty trail is a real answer: the caller must not dim the canvas for a
    // field whose relations are simply not drawn.
    expect([...trail.nodeIds]).toEqual(["b"]);
    expect(trail.edgeIds.size).toBe(0);
    expect(trail.upstream.relationCount).toBe(0);
    expect(trail.downstream.relationCount).toBe(0);
  });
});

describe("columnStep", () => {
  it("lands on the field the far object names, one hop in one direction", () => {
    const up = rel("orders", "buyer_id", "v", "customer_id");
    const down = rel("v", "customer_id", "report", "buyer_id");
    const sibling = rel("regions", "country", "v", "country");
    const data = graph({ v: [up, sibling, down] }).get("v");

    expect(
      columnStep(data!, { guid: "v", column: "customer_id" }, "upstream")
    ).toEqual({
      pairs: [{ guid: "orders", column: "buyer_id" }],
      objects: [],
    });
    expect(
      columnStep(data!, { guid: "v", column: "customer_id" }, "downstream")
    ).toEqual({
      pairs: [{ guid: "report", column: "buyer_id" }],
      objects: [],
    });
  });

  it("ignores the relations that name another field, or another pair", () => {
    const other = rel("orders", "country", "v", "country");
    const elsewhere = rel("orders", "customer_id", "w", "customer_id");
    const data = graph({ v: [other], w: [elsewhere] }).get("v");

    // `elsewhere` belongs to `w`, so it is not this pair's hop however the server
    // grouped the response it arrived in.
    expect(
      columnStep(data!, { guid: "v", column: "customer_id" }, "upstream")
    ).toEqual({ pairs: [], objects: [] });
  });

  it("draws the object but names no next pair when the relation has no far field", () => {
    // A table-level edge carries `customer_id` to `v` without saying which field of
    // `regions` it came from, so `regions` is on the canvas and the walk stops.
    const tableLevel = rel("regions", "", "v", "customer_id", {
      relationType: RelationType.JOIN,
    });
    const data = graph({ v: [tableLevel] }).get("v");

    expect(
      columnStep(data!, { guid: "v", column: "customer_id" }, "upstream")
    ).toEqual({ pairs: [], objects: ["regions"] });
  });
});

describe("walkColumnExpansion", () => {
  /** The walk over a fixed graph, with the fetch the caller would do. */
  function walkOf(
    nodes: Record<string, LineageRelation[]>,
    start: { guid: string; column: string },
    direction: "upstream" | "downstream",
    depth: number
  ) {
    const map = graph(nodes);
    return walkColumnExpansion({
      start,
      direction,
      depth,
      load: async (guid) => map.get(guid) ?? graph({}).get(guid)!,
    });
  }

  it("scopes the pivot alone at depth 1, one level less for each hop below", async () => {
    const one = rel("a", "id", "b", "id");
    const two = rel("b", "id", "c", "id");
    const three = rel("c", "id", "d", "id");
    const nodes = { a: [one], b: [one, two], c: [two, three], d: [three] };

    const shallow = await walkOf(
      nodes,
      { guid: "c", column: "id" },
      "upstream",
      1
    );
    expect(shallow.walked).toEqual([
      { pair: { guid: "c", column: "id" }, depth: 0 },
    ]);
    // `b` is drawn by the pivot's own relations; `a` is one hop further, and a
    // depth-1 walk never asks for it.
    expect([...shallow.objects].sort()).toEqual(["b", "c"]);

    const deep = await walkOf(
      nodes,
      { guid: "c", column: "id" },
      "upstream",
      3
    );
    expect(deep.walked).toEqual([
      { pair: { guid: "c", column: "id" }, depth: 2 },
      { pair: { guid: "b", column: "id" }, depth: 1 },
      { pair: { guid: "a", column: "id" }, depth: 0 },
    ]);
    expect([...deep.objects].sort()).toEqual(["a", "b", "c"]);
  });

  it("keeps every object the walk draws, the ones with no field of their own included", async () => {
    // `a` is reached through a table-level edge: drawn, never walked through.
    const tableLevel = rel("a", "", "b", "id", {
      relationType: RelationType.JOIN,
    });
    const beyond = rel("b", "id", "c", "id");
    const walk = await walkOf(
      { a: [tableLevel], b: [tableLevel, beyond], c: [beyond] },
      { guid: "b", column: "id" },
      "upstream",
      3
    );

    expect(walk.walked.map((step) => step.pair.guid)).toEqual(["b"]);
    expect([...walk.objects].sort()).toEqual(["a", "b"]);
  });

  it("walks a diamond once and never past the pairs it has seen", async () => {
    const toA = rel("a", "id", "d", "id");
    const left = rel("b", "id", "a", "id");
    const right = rel("c", "id", "a", "id");
    const apexLeft = rel("e", "id", "b", "id");
    const apexRight = rel("e", "id", "c", "id");
    const walk = await walkOf(
      {
        a: [toA, left, right],
        b: [left, apexLeft],
        c: [right, apexRight],
        d: [toA],
        e: [apexLeft, apexRight],
      },
      { guid: "d", column: "id" },
      "upstream",
      4
    );

    expect(walk.walked.map((step) => step.pair.guid)).toEqual([
      "d",
      "a",
      "b",
      "c",
      "e",
    ]);
    expect([...walk.objects].sort()).toEqual(["a", "b", "c", "d", "e"]);
  });

  it("asks the loader for each object once, and only for the levels it walks", async () => {
    const one = rel("a", "id", "b", "id");
    const two = rel("b", "id", "c", "id");
    const map = graph({ a: [one], b: [one, two], c: [two] });
    const asked: string[] = [];

    await walkColumnExpansion({
      start: { guid: "b", column: "id" },
      direction: "upstream",
      depth: 2,
      load: async (guid) => {
        asked.push(guid);
        return map.get(guid)!;
      },
    });

    expect(asked).toEqual(["b", "a"]);
  });
});
