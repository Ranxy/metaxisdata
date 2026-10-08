import { describe, expect, it } from "vitest";
import {
  type LineageRelation,
  RelationType,
} from "@/types/proto-es/v1/lineage_service_pb";
import {
  assignLayers,
  buildLineageEdges,
  collectColumnEdgeIds,
  distinctRelationCounts,
  layoutNodes,
  type NodeLineageData,
  nodeHeight,
} from "./lineageGraph";

function rel(overrides: Partial<LineageRelation>): LineageRelation {
  return {
    sourceGuid: "",
    targetGuid: "",
    sourceColumn: "",
    targetColumn: "",
    relationType: RelationType.DIRECT,
    ...overrides,
  } as LineageRelation;
}

function node(
  upstream: LineageRelation[] = [],
  downstream: LineageRelation[] = []
): NodeLineageData {
  return {
    upstream,
    downstream,
    upstreamLoaded: true,
    downstreamLoaded: true,
  };
}

describe("assignLayers", () => {
  it("walks upstream left and downstream right, per hop", () => {
    const map = new Map([
      ["b", node([rel({ sourceGuid: "a", targetGuid: "b" })])],
      [
        "c",
        node(
          [rel({ sourceGuid: "b", targetGuid: "c" })],
          [rel({ sourceGuid: "c", targetGuid: "d" })]
        ),
      ],
    ]);
    const layers = assignLayers("c", map);
    expect(layers.get("a")).toBe(-2);
    expect(layers.get("b")).toBe(-1);
    expect(layers.get("c")).toBe(0);
    expect(layers.get("d")).toBe(1);
  });

  it("keeps the root layer for a node no relation connects", () => {
    const layers = assignLayers("root", new Map([["lonely", node()]]));
    expect(layers.get("lonely")).toBe(0);
  });
});

describe("layoutNodes", () => {
  it("starts the leftmost layer at x = 0 and stacks the rest", () => {
    const layers = new Map([
      ["a", -1],
      ["b", 0],
      ["c", 0],
    ]);
    const positions = layoutNodes(layers, (guid) => (guid === "b" ? 200 : 100));
    expect(positions.get("a")).toEqual({ x: 0, y: 0 });
    expect(positions.get("b")).toEqual({ x: 280, y: 0 });
    // stacked below b, whose height is 200 plus the 20px gap
    expect(positions.get("c")).toEqual({ x: 280, y: 220 });
  });

  it("adds the field list to the node's height only when it is visible", () => {
    expect(nodeHeight(3, false)).toBe(120);
    expect(nodeHeight(3, true)).toBe(120 + 3 * 24);
    // capped at 200px of fields however many columns there are
    expect(nodeHeight(100, true)).toBe(320);
  });
});

describe("column filtering", () => {
  const map = new Map([
    ["b", node([rel({ sourceGuid: "a", targetGuid: "b", targetColumn: "x" })])],
    ["c", node([rel({ sourceGuid: "b", targetGuid: "c", sourceColumn: "x" })])],
  ]);

  it("collects the edges that touch the selected column", () => {
    const ids = collectColumnEdgeIds(map, { guid: "b", column: "x" });
    expect([...ids].sort()).toEqual(["a->b", "b->c"]);
  });

  it("collects nothing without a selection", () => {
    expect(collectColumnEdgeIds(map, null).size).toBe(0);
  });

  it("dims every unrelated edge once a column is selected", () => {
    const all = buildLineageEdges(map, {
      validNodeIds: new Set(["a", "b", "c"]),
      columnFilter: null,
    });
    expect(all.every((edge) => edge.animated === false)).toBe(true);

    const filtered = buildLineageEdges(map, {
      validNodeIds: new Set(["a", "b", "c"]),
      columnFilter: { guid: "b", column: "x" },
    });
    expect(filtered).toHaveLength(2);
    expect(
      filtered.every(
        (edge) =>
          (edge.style as { strokeWidth?: number } | undefined)?.strokeWidth ===
          3
      )
    ).toBe(true);

    const unrelated = buildLineageEdges(map, {
      validNodeIds: new Set(["a", "b", "c"]),
      columnFilter: { guid: "b", column: "nope" },
    });
    expect(unrelated.every((edge) => edge.animated === false)).toBe(true);
  });

  it("marks only the transformed relations and drops edges to unknown nodes", () => {
    const derived = new Map([
      [
        "b",
        node([
          rel({
            sourceGuid: "a",
            targetGuid: "b",
            relationType: RelationType.INDIRECT,
          }),
          rel({ sourceGuid: "ghost", targetGuid: "b" }),
        ]),
      ],
    ]);
    const edges = buildLineageEdges(derived, {
      validNodeIds: new Set(["a", "b"]),
      columnFilter: null,
    });
    expect(edges).toHaveLength(1);
    expect(edges[0].label).toBe("T");
  });

  it("builds each edge once even when both directions report it", () => {
    const both = new Map([
      ["b", node([rel({ sourceGuid: "a", targetGuid: "b" })])],
      ["a", node([], [rel({ sourceGuid: "a", targetGuid: "b" })])],
    ]);
    const edges = buildLineageEdges(both, {
      validNodeIds: new Set(["a", "b"]),
      columnFilter: null,
    });
    expect(edges.map((edge) => edge.id)).toEqual(["a->b"]);
  });
});

describe("distinctRelationCounts", () => {
  it("counts objects, not relations, on both sides", () => {
    const counts = distinctRelationCounts(
      node(
        [
          rel({ sourceGuid: "a", targetGuid: "me", sourceColumn: "c1" }),
          rel({ sourceGuid: "a", targetGuid: "me", sourceColumn: "c2" }),
          rel({ sourceGuid: "b", targetGuid: "me", sourceColumn: "c3" }),
        ],
        [rel({ sourceGuid: "me", targetGuid: "d" })]
      )
    );

    expect(counts).toEqual({ upstream: 2, downstream: 1 });
  });

  it("reports zero for a node with no relations", () => {
    expect(distinctRelationCounts(node())).toEqual({
      upstream: 0,
      downstream: 0,
    });
  });

  it("counts the far end of each direction", () => {
    // A relation between two other objects says nothing about this node.
    const counts = distinctRelationCounts(
      node([rel({ sourceGuid: "x", targetGuid: "y" })], [])
    );
    expect(counts).toEqual({ upstream: 1, downstream: 0 });
  });
});

describe("edge origins", () => {
  const openlineage = { metaType: 100 } as Partial<LineageRelation>;
  const analyzer = { metaType: 5 } as Partial<LineageRelation>;

  const edgesOf = (map: Map<string, NodeLineageData>) => {
    const valid = new Set(map.keys());
    for (const data of map.values()) {
      for (const relation of [...data.upstream, ...data.downstream]) {
        valid.add(relation.sourceGuid);
        valid.add(relation.targetGuid);
      }
    }
    return buildLineageEdges(map, { validNodeIds: valid, columnFilter: null });
  };

  it("colours an analyzer relation and dashes an OpenLineage one", () => {
    const edges = edgesOf(
      new Map([
        ["b", node([rel({ sourceGuid: "a", targetGuid: "b", ...analyzer })])],
        [
          "c",
          node([rel({ sourceGuid: "b", targetGuid: "c", ...openlineage })]),
        ],
      ])
    );

    expect(edges.map((edge) => edge.data?.origin)).toEqual([
      "sql",
      "openlineage",
    ]);
    const [sql, ol] = edges;
    expect((sql.style as { stroke?: string }).stroke).toBe(
      "hsl(var(--lineage-sql) / 0.65)"
    );
    expect((sql.style as { strokeDasharray?: string }).strokeDasharray).toBe(
      undefined
    );
    expect((ol.style as { stroke?: string }).stroke).toBe(
      "hsl(var(--lineage-openlineage) / 0.65)"
    );
    expect((ol.style as { strokeDasharray?: string }).strokeDasharray).toBe(
      "7 5"
    );
    expect(ol.animated).toBe(true);
    expect(sql.animated).toBe(false);
  });

  it("marks an edge both writers stored as mixed", () => {
    const edges = edgesOf(
      new Map([
        [
          "b",
          node([
            rel({ sourceGuid: "a", targetGuid: "b", ...analyzer }),
            rel({ sourceGuid: "a", targetGuid: "b", ...openlineage }),
          ]),
        ],
      ])
    );

    expect(edges).toHaveLength(1);
    expect(edges[0].data?.origin).toBe("mixed");
    expect(
      (edges[0].style as { strokeDasharray?: string }).strokeDasharray
    ).toBe("2 3");
  });

  it("keeps only the origins the filter names", () => {
    const map = new Map([
      ["b", node([rel({ sourceGuid: "a", targetGuid: "b", ...analyzer })])],
      ["c", node([rel({ sourceGuid: "b", targetGuid: "c", ...openlineage })])],
    ]);

    const kept = (origin: "sql" | "openlineage") =>
      buildLineageEdges(map, {
        validNodeIds: new Set(["a", "b", "c"]),
        columnFilter: null,
        originFilter: new Set([origin]),
      }).map((edge) => edge.id);

    expect(kept("sql")).toEqual(["a->b"]);
    expect(kept("openlineage")).toEqual(["b->c"]);

    expect(
      buildLineageEdges(map, {
        validNodeIds: new Set(["a", "b", "c"]),
        columnFilter: null,
        originFilter: new Set(),
      })
    ).toEqual([]);
  });

  it("keeps a bundled edge under either origin's filter", () => {
    const mixed = new Map([
      [
        "b",
        node([
          rel({ sourceGuid: "a", targetGuid: "b", ...analyzer }),
          rel({ sourceGuid: "a", targetGuid: "b", ...openlineage }),
        ]),
      ],
    ]);

    for (const origin of ["sql", "openlineage"] as const) {
      const edges = buildLineageEdges(mixed, {
        validNodeIds: new Set(["a", "b"]),
        columnFilter: null,
        originFilter: new Set([origin]),
      });
      expect(edges.map((edge) => edge.id)).toEqual(["a->b"]);
    }
  });

  it("draws a highlighted edge at full strength with its origin's arrowhead", () => {
    const map = new Map([
      [
        "b",
        node([
          rel({
            sourceGuid: "a",
            targetGuid: "b",
            targetColumn: "x",
            ...openlineage,
          }),
        ]),
      ],
    ]);
    const [edge] = buildLineageEdges(map, {
      validNodeIds: new Set(["a", "b"]),
      columnFilter: { guid: "b", column: "x" },
    });

    expect((edge.style as { stroke?: string }).stroke).toBe(
      "hsl(var(--lineage-openlineage))"
    );
    expect(edge.markerEnd).toMatchObject({
      color: "hsl(var(--lineage-openlineage))",
    });
  });

  it("fades the arrowhead of a dimmed edge", () => {
    const map = new Map([
      ["b", node([rel({ sourceGuid: "a", targetGuid: "b", ...analyzer })])],
    ]);
    const [edge] = buildLineageEdges(map, {
      validNodeIds: new Set(["a", "b"]),
      columnFilter: { guid: "b", column: "missing" },
    });

    expect(edge.markerEnd).toMatchObject({
      color: "hsl(var(--muted-foreground) / 0.4)",
    });
    expect((edge.style as { stroke?: string }).stroke).toBe(
      "hsl(var(--muted-foreground) / 0.2)"
    );
  });
});
