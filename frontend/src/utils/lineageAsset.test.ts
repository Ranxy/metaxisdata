import { describe, expect, it } from "vitest";
import type { ExternalDatasetInfo } from "@/types/proto-es/v1/lineage_service_pb";
import {
  buildLineageScopeColors,
  buildScopeColorMap,
  compareScopeKeys,
  isExternalGuid,
  lineageAssetView,
  scopeColor,
} from "./lineageAsset";

const titles = new Map([
  ["test-pg-1", "testPG"],
  ["mysql-dev-1", "localmysql"],
]);

function external(
  overrides: Partial<ExternalDatasetInfo>
): ExternalDatasetInfo {
  return {
    guid: "",
    namespace: "",
    name: "",
    datasetType: "",
    ...overrides,
  } as ExternalDatasetInfo;
}

describe("isExternalGuid", () => {
  it("recognizes only the external prefix", () => {
    expect(isExternalGuid("external:ns:orders")).toBe(true);
    expect(isExternalGuid("test-pg-1;e2e;public;orders")).toBe(false);
  });
});

describe("lineageAssetView", () => {
  it("names a PostgreSQL object with its instance and schema path", () => {
    const view = lineageAssetView(
      "test-pg-1;e2e;e2e_dwd;v_customer_360",
      titles
    );
    expect(view).toEqual({
      scopeKey: "test-pg-1",
      scopeLabel: "testPG",
      name: "v_customer_360",
      qualifier: "e2e.e2e_dwd",
      fullLabel: "testPG · e2e.e2e_dwd.v_customer_360",
      isExternal: false,
    });
  });

  it("skips the empty MySQL schema segment", () => {
    const view = lineageAssetView("mysql-dev-1;analytics;;v_orders", titles);
    expect(view.scopeLabel).toBe("localmysql");
    expect(view.qualifier).toBe("analytics");
    expect(view.fullLabel).toBe("localmysql · analytics.v_orders");
  });

  it("distinguishes two instances that share a database and table name", () => {
    const one = lineageAssetView("test-pg-1;e2e;e2e_dwd;dwd_order", titles);
    const two = lineageAssetView("mysql-dev-1;e2e;e2e_dwd;dwd_order", titles);
    expect(one.name).toBe(two.name);
    expect(one.fullLabel).not.toBe(two.fullLabel);
    expect(one.scopeKey).not.toBe(two.scopeKey);
  });

  it("falls back to the instance id when the instance is unknown", () => {
    const view = lineageAssetView("ghost;db;;t", titles);
    expect(view.scopeLabel).toBe("ghost");
    expect(view.fullLabel).toBe("ghost · db.t");
  });

  it("drops a trailing empty segment instead of naming the object empty", () => {
    const view = lineageAssetView("mysql-dev-1;eve;", titles);
    expect(view.name).toBe("eve");
    expect(view.qualifier).toBe("");
    expect(view.fullLabel).toBe("localmysql · eve");
  });

  it("names a top-level object by its own single segment", () => {
    const view = lineageAssetView("test-pg-1", titles);
    expect(view).toMatchObject({
      scopeKey: "test-pg-1",
      name: "test-pg-1",
      qualifier: "",
      fullLabel: "testPG · test-pg-1",
    });
  });

  it("names an external dataset from the response metadata", () => {
    const view = lineageAssetView(
      "external:postgres://127.0.0.1:5432:e2e.e2e_dwd.dwd_order_fact",
      titles,
      external({
        namespace: "postgres://127.0.0.1:5432",
        name: "e2e.e2e_dwd.dwd_order_fact",
      })
    );
    expect(view).toEqual({
      scopeKey: "external:postgres://127.0.0.1:5432",
      scopeLabel: "postgres://127.0.0.1:5432",
      name: "dwd_order_fact",
      qualifier: "e2e.e2e_dwd",
      fullLabel: "postgres://127.0.0.1:5432 · e2e.e2e_dwd.dwd_order_fact",
      isExternal: true,
    });
  });

  it("splits an external GUID at the last colon when no metadata arrived", () => {
    const view = lineageAssetView(
      "external:postgres://host:5432:db.tbl",
      titles
    );
    expect(view.scopeLabel).toBe("postgres://host:5432");
    expect(view.name).toBe("tbl");
    expect(view.qualifier).toBe("db");
  });

  it("keeps an external GUID with no colon usable", () => {
    const view = lineageAssetView("external:orders", titles);
    expect(view.scopeLabel).toBe("");
    expect(view.name).toBe("orders");
    expect(view.qualifier).toBe("");
    expect(view.fullLabel).toBe("orders");
  });

  it("keeps an external GUID with no name usable", () => {
    const view = lineageAssetView("external:", titles);
    expect(view.scopeLabel).toBe("");
    expect(view.name).toBe("external:");
    expect(view.qualifier).toBe("");
  });
});

describe("compareScopeKeys", () => {
  it("orders by code point, not by locale", () => {
    expect(["b", "a", "B"].sort(compareScopeKeys)).toEqual(["B", "a", "b"]);
    expect(compareScopeKeys("same", "same")).toBe(0);
    expect(compareScopeKeys("a", "b")).toBe(-1);
    expect(compareScopeKeys("b", "a")).toBe(1);
  });
});

describe("buildScopeColorMap", () => {
  it("assigns one accent per scope in the given order, deduplicated", () => {
    const map = buildScopeColorMap(["a", "b", "a"]);
    expect([...map.entries()]).toEqual([
      ["a", "hsl(var(--lineage-scope-1))"],
      ["b", "hsl(var(--lineage-scope-2))"],
    ]);
  });

  it("wraps once the palette runs out", () => {
    const keys = ["1", "2", "3", "4", "5", "6", "7"];
    expect(buildScopeColorMap(keys).get("7")).toBe(
      "hsl(var(--lineage-scope-1))"
    );
  });

  it("returns a neutral accent for an unknown scope", () => {
    expect(scopeColor(new Map(), "nope")).toBe("hsl(var(--muted-foreground))");
  });
});

describe("buildLineageScopeColors", () => {
  const datasets = new Map([
    [
      "external:postgres://127.0.0.1:5432:e2e.e2e_dwd.dwd_order_fact",
      external({
        namespace: "postgres://127.0.0.1:5432",
        name: "e2e.e2e_dwd.dwd_order_fact",
      }),
    ],
  ]);

  it("colours the instances first, then one accent per external namespace", () => {
    const map = buildLineageScopeColors({
      instanceIds: ["test-pg-1"],
      externalDatasetGuids: datasets.keys(),
      externalOf: (guid) => datasets.get(guid),
    });

    expect([...map.keys()]).toEqual([
      "test-pg-1",
      "external:postgres://127.0.0.1:5432",
    ]);
    // The lookup a row performs is by scope key, so the map has to be keyed that
    // way — keying it by the raw GUID is what left every dataset a neutral dot.
    expect(scopeColor(map, "external:postgres://127.0.0.1:5432")).toBe(
      "hsl(var(--lineage-scope-2))"
    );
  });

  it("keeps an instance's accent when an external dataset appears later", () => {
    const before = buildLineageScopeColors({
      instanceIds: ["a", "b"],
      externalDatasetGuids: [],
      externalOf: () => undefined,
    });
    const after = buildLineageScopeColors({
      instanceIds: ["a", "b"],
      externalDatasetGuids: datasets.keys(),
      externalOf: (guid) => datasets.get(guid),
    });

    for (const key of ["a", "b"]) {
      expect(after.get(key)).toBe(before.get(key));
    }
  });

  it("gives an undescribed external dataset its own scope", () => {
    const map = buildLineageScopeColors({
      instanceIds: [],
      externalDatasetGuids: ["external:unknown-producer:orders"],
      externalOf: () => undefined,
    });
    expect([...map.keys()]).toEqual(["external:unknown-producer"]);
  });
});
