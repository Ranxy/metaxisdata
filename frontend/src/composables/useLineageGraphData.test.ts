import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ref } from "vue";
import type { LineageCount } from "@/api/lineage";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  ExternalDatasetInfoSchema,
  LineageRelationSchema,
  LineageType,
} from "@/types/proto-es/v1/lineage_service_pb";
import { useLineageGraphData } from "./useLineageGraphData";

const mocks = vi.hoisted(() => ({
  getLineage: vi.fn(),
  getLineageCounts: vi.fn(),
}));

vi.mock("@/api/lineage", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/lineage")>()),
  getLineage: mocks.getLineage,
  getLineageCounts: mocks.getLineageCounts,
}));

function relation(sourceGuid: string, targetGuid: string) {
  return create(LineageRelationSchema, {
    sourceGuid,
    targetGuid,
    sourceColumn: "id",
    targetColumn: "order_id",
    sourceType: MetaType.TABLE,
    targetType: MetaType.TABLE,
  });
}

const UPSTREAM = relation("up", "pivot");
const DOWNSTREAM = relation("pivot", "down");

function setup() {
  const externalDatasetMap = ref(new Map());
  const recordRelations = vi.fn();
  let drawn: string[] = [];
  const onCountsLoaded = vi.fn();
  const data = useLineageGraphData({
    metaTypeFor: () => MetaType.TABLE,
    recordRelations,
    externalDatasetMap,
    drawnNodeIds: () => drawn,
    onCountsLoaded,
  });
  return {
    ...data,
    externalDatasetMap,
    recordRelations,
    onCountsLoaded,
    setDrawn: (ids: string[]) => {
      drawn = ids;
    },
  };
}

beforeEach(() => {
  mocks.getLineage.mockReset();
  mocks.getLineageCounts.mockReset();
});

describe("lineage data cache", () => {
  it("starts empty and fills one direction at a time", async () => {
    mocks.getLineage.mockResolvedValue({
      relationsSource: [UPSTREAM],
      relationsTarget: [],
      externalDatasets: [],
    });
    const data = setup();

    expect(data.getNodeLineageData("pivot")).toEqual({
      upstream: [],
      downstream: [],
      upstreamLoaded: false,
      downstreamLoaded: false,
    });

    await data.fetchLineageForGuid("pivot", LineageType.SOURCE);
    expect(mocks.getLineage).toHaveBeenCalledWith({
      guid: "pivot",
      metaType: MetaType.TABLE,
      lineageType: LineageType.SOURCE,
    });

    // The second direction is a separate read; the third call is a cache hit.
    mocks.getLineage.mockResolvedValue({
      relationsSource: [],
      relationsTarget: [DOWNSTREAM],
      externalDatasets: [],
    });
    await data.fetchLineageForGuid("pivot", LineageType.TARGET);
    await data.fetchLineageForGuid("pivot");

    expect(mocks.getLineage).toHaveBeenCalledTimes(2);
    expect(data.getNodeLineageData("pivot")).toEqual({
      upstream: [UPSTREAM],
      downstream: [DOWNSTREAM],
      upstreamLoaded: true,
      downstreamLoaded: true,
    });
    expect(data.recordRelations).toHaveBeenCalledWith([UPSTREAM]);
    expect(data.recordRelations).toHaveBeenCalledWith([DOWNSTREAM]);
  });

  it("stores a new external dataset fact once", async () => {
    const fact = create(ExternalDatasetInfoSchema, {
      guid: "ext-1",
      namespace: "snowflake://acct",
    });
    mocks.getLineage.mockResolvedValue({
      relationsSource: [],
      relationsTarget: [],
      externalDatasets: [fact],
    });
    const data = setup();

    await data.fetchLineageForGuid("pivot");

    expect(data.externalDatasetMap.value.get("ext-1")?.namespace).toBe(
      "snowflake://acct"
    );
  });

  it("stores only new external dataset facts", async () => {
    const fact = create(ExternalDatasetInfoSchema, {
      guid: "ext-1",
      namespace: "snowflake://acct",
    });
    mocks.getLineage.mockResolvedValue({
      relationsSource: [],
      relationsTarget: [],
      externalDatasets: [fact],
    });
    const data = setup();
    data.externalDatasetMap.value.set("ext-1", fact);

    await data.fetchLineageForGuid("pivot");

    expect(data.externalDatasetMap.value.size).toBe(1);
  });

  it("caches a failed read as loaded so it is not retried forever", async () => {
    mocks.getLineage.mockRejectedValue(new Error("boom"));
    const data = setup();

    const result = await data.fetchLineageForGuid("pivot", LineageType.SOURCE);

    expect(result.upstreamLoaded).toBe(true);
    expect(result.downstreamLoaded).toBe(false);
    expect(data.getNodeLineageData("pivot").upstreamLoaded).toBe(true);
  });

  it("reports a direction as expanded only once the page drew it", () => {
    const data = setup();

    expect(data.isDirectionExpanded("pivot", "upstream")).toBe(false);
    data.expandedDirections.value.add(data.expansionKey("pivot", "upstream"));
    expect(data.isDirectionExpanded("pivot", "upstream")).toBe(true);
  });

  it("maps a direction to the RPC filter it means", () => {
    const data = setup();

    expect(data.directionToLineageType("upstream")).toBe(LineageType.SOURCE);
    expect(data.directionToLineageType("downstream")).toBe(LineageType.TARGET);
  });
});

describe("field-scoped expansion", () => {
  async function loaded() {
    mocks.getLineage.mockResolvedValue({
      relationsSource: [UPSTREAM],
      relationsTarget: [DOWNSTREAM],
      externalDatasets: [],
    });
    const data = setup();
    await data.fetchLineageForGuid("pivot");
    return data;
  }

  it("answers null until the direction's relations are loaded", () => {
    const data = setup();
    expect(data.expandableColumns("pivot", "upstream")).toBeNull();
  });

  it("lists the fields whose walk can still go deeper", async () => {
    const data = await loaded();
    data.expandDepth.value = "1";

    expect([...(data.expandableColumns("pivot", "upstream") ?? [])]).toEqual([
      "order_id",
    ]);

    data.expandedColumns.value.set(
      data.expansionKey("pivot", "upstream"),
      new Map()
    );
    expect([...(data.expandableColumns("pivot", "upstream") ?? [])]).toEqual([
      "order_id",
    ]);

    data.expandedColumns.value.set(
      data.expansionKey("pivot", "upstream"),
      new Map([["order_id", 0]])
    );
    expect([...(data.expandableColumns("pivot", "upstream") ?? [])]).toEqual(
      []
    );
  });

  it("records the deepest walk a field has taken", async () => {
    const data = await loaded();

    data.addColumnExpansion("pivot", "upstream", "id", 1);
    data.addColumnExpansion("pivot", "upstream", "id", 2);
    data.addColumnExpansion("pivot", "upstream", "id", 0);

    expect(data.fieldScopesFor("pivot", "upstream")?.get("id")).toBe(2);
  });

  it("records no field scope under a whole-node expansion", async () => {
    const data = await loaded();
    data.expandedDirections.value.add(data.expansionKey("pivot", "upstream"));

    data.addColumnExpansion("pivot", "upstream", "id", 1);

    expect(data.fieldScopesFor("pivot", "upstream")).toBeUndefined();
  });

  it("counts one reveal request per card", () => {
    const data = setup();

    data.requestFieldReveal(["a", "a", "b"]);

    expect(data.revealRequest.value.get("a")).toBe(2);
    expect(data.revealRequest.value.get("b")).toBe(1);
  });
});

describe("batched degree read", () => {
  it("reads only the drawn nodes it has no degree for", async () => {
    mocks.getLineageCounts.mockResolvedValue(
      new Map<string, LineageCount>([["b", { upstream: 1 } as LineageCount]])
    );
    const data = setup();
    data.lineageCounts.value.set("a", { upstream: 0 } as LineageCount);
    data.setDrawn(["a", "b", "b"]);

    await data.ensureLineageCounts();

    expect(mocks.getLineageCounts).toHaveBeenCalledWith(["b"]);
    expect(data.lineageCounts.value.get("b")).toEqual({ upstream: 1 });
    expect(data.onCountsLoaded).toHaveBeenCalledOnce();
  });

  it("does nothing when every drawn node already has a degree", async () => {
    const data = setup();
    data.lineageCounts.value.set("a", { upstream: 0 } as LineageCount);
    data.setDrawn(["a"]);

    await data.ensureLineageCounts();

    expect(mocks.getLineageCounts).not.toHaveBeenCalled();
    expect(data.onCountsLoaded).not.toHaveBeenCalled();
  });

  it("keeps the canvas usable when the degree read fails", async () => {
    mocks.getLineageCounts.mockRejectedValue(new Error("down"));
    const data = setup();
    data.setDrawn(["a"]);

    await data.ensureLineageCounts();

    expect(data.lineageCounts.value.size).toBe(0);
    expect(data.onCountsLoaded).not.toHaveBeenCalled();
  });
});
