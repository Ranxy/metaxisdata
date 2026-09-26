import { describe, expect, it } from "vitest";
import {
  aggregateOpenLineageDatasets,
  extractOpenLineageDatasets,
  hasOpenLineageUnparsedSQL,
} from "./openlineage";

function payload(inputs: unknown, outputs: unknown): string {
  return JSON.stringify({ inputs, outputs });
}

function run(rawPayload: string) {
  return { rawPayload };
}

describe("extractOpenLineageDatasets", () => {
  it("reads the inputs and outputs of an event", () => {
    const extracted = extractOpenLineageDatasets(
      payload(
        [{ namespace: "mysql://shop", name: "shop.orders" }],
        [{ namespace: "s3://lake", name: "raw/orders" }]
      )
    );

    expect(extracted.inputs).toEqual([
      { namespace: "mysql://shop", name: "shop.orders" },
    ]);
    expect(extracted.outputs).toEqual([
      { namespace: "s3://lake", name: "raw/orders" },
    ]);
  });

  it("returns nothing for an empty or unparseable payload", () => {
    for (const rawPayload of ["", "not json", '"a string"', "null", "[]"]) {
      expect(extractOpenLineageDatasets(rawPayload)).toEqual({
        inputs: [],
        outputs: [],
      });
    }
  });

  it("drops entries that carry neither a namespace nor a name", () => {
    const extracted = extractOpenLineageDatasets(
      payload(
        [
          null,
          "shop.orders",
          42,
          { other: "field" },
          { namespace: "", name: "" },
          { namespace: 7, name: "shop.orders" },
          { name: "shop.orders" },
          { namespace: "mysql://shop" },
        ],
        []
      )
    );

    // A half-identified dataset is kept: the lineage graph still has an endpoint
    // to draw, and dropping it would hide a real edge.
    expect(extracted.inputs).toEqual([
      { namespace: "", name: "shop.orders" },
      { namespace: "", name: "shop.orders" },
      { namespace: "mysql://shop", name: "" },
    ]);
  });

  it("ignores a payload whose dataset lists are not lists", () => {
    const extracted = extractOpenLineageDatasets(
      payload({ inputs: ["nope"] }, "outputs")
    );

    expect(extracted).toEqual({ inputs: [], outputs: [] });
  });
});

describe("aggregateOpenLineageDatasets", () => {
  it("counts a dataset once per run and sums it across runs", () => {
    const aggregated = aggregateOpenLineageDatasets([
      run(
        payload(
          [
            { namespace: "mysql://shop", name: "shop.orders" },
            // The same dataset twice in one event is still one occurrence.
            { namespace: "mysql://shop", name: "shop.orders" },
            { namespace: "s3://lake", name: "raw/orders" },
          ],
          []
        )
      ),
      run(payload([{ namespace: "mysql://shop", name: "shop.orders" }], [])),
    ]);

    expect(aggregated.inputs).toEqual([
      { namespace: "mysql://shop", name: "shop.orders", runCount: 2 },
      { namespace: "s3://lake", name: "raw/orders", runCount: 1 },
    ]);
    expect(aggregated.outputs).toEqual([]);
  });

  it("sorts equally seen datasets by name and then namespace", () => {
    const aggregated = aggregateOpenLineageDatasets([
      run(
        payload(
          [
            { namespace: "mysql://b", name: "zeta" },
            { namespace: "mysql://b", name: "alpha" },
            { namespace: "mysql://a", name: "alpha" },
          ],
          []
        )
      ),
    ]);

    expect(aggregated.inputs).toEqual([
      { namespace: "mysql://a", name: "alpha", runCount: 1 },
      { namespace: "mysql://b", name: "alpha", runCount: 1 },
      { namespace: "mysql://b", name: "zeta", runCount: 1 },
    ]);
  });

  it("keeps the same name in different namespaces apart", () => {
    const aggregated = aggregateOpenLineageDatasets([
      run(payload([{ namespace: "mysql://a", name: "orders" }], [])),
      run(payload([{ namespace: "mysql://b", name: "orders" }], [])),
    ]);

    expect(aggregated.inputs).toEqual([
      { namespace: "mysql://a", name: "orders", runCount: 1 },
      { namespace: "mysql://b", name: "orders", runCount: 1 },
    ]);
  });

  it("has nothing to report for runs without datasets", () => {
    expect(aggregateOpenLineageDatasets([run(""), run("{}")])).toEqual({
      inputs: [],
      outputs: [],
    });
  });
});

describe("hasOpenLineageUnparsedSQL", () => {
  function payloadWithSQL(inputs: unknown, outputs: unknown): string {
    return JSON.stringify({
      job: {
        namespace: "default",
        name: "dag.task",
        facets: { sql: { query: "select 1" } },
      },
      inputs,
      outputs,
    });
  }

  it("reports a SQL facet that produced no dataset", () => {
    expect(hasOpenLineageUnparsedSQL(payloadWithSQL([], []))).toBe(true);
  });

  it("stays quiet once the SQL facet produced datasets", () => {
    expect(
      hasOpenLineageUnparsedSQL(
        payloadWithSQL(
          [
            {
              namespace: "postgres://localhost:5432",
              name: "e2e.e2e_ods.orders",
            },
          ],
          []
        )
      )
    ).toBe(false);
    expect(
      hasOpenLineageUnparsedSQL(
        payloadWithSQL(
          [],
          [
            {
              namespace: "postgres://localhost:5432",
              name: "e2e.e2e_dwd.dwd_order_fact",
            },
          ]
        )
      )
    ).toBe(false);
  });

  it("stays quiet for events without a SQL facet", () => {
    const noSQLFacet = JSON.stringify({
      job: {
        namespace: "default",
        name: "dag.task",
        facets: { processing_engine: {} },
      },
      inputs: [],
      outputs: [],
    });
    expect(hasOpenLineageUnparsedSQL(noSQLFacet)).toBe(false);
  });

  it("stays quiet for payloads it cannot read", () => {
    for (const rawPayload of [
      "",
      "not json",
      "null",
      "[]",
      "{}",
      JSON.stringify({ job: "x" }),
    ]) {
      expect(hasOpenLineageUnparsedSQL(rawPayload)).toBe(false);
    }
  });
});
