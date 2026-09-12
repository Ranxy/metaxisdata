import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  ExternalDatasetInfoSchema,
  GetLineageResponseSchema,
  LineageRelationSchema,
  LineageType,
} from "@/types/proto-es/v1/lineage_service_pb";
import { getLineage } from "./lineage";

const { getLineageMock } = vi.hoisted(() => ({ getLineageMock: vi.fn() }));

vi.mock("./client", () => ({
  lineageClient: { getLineage: getLineageMock },
}));

function page(
  options: {
    source?: string[];
    target?: string[];
    datasets?: { guid: string; name?: string }[];
    nextPageToken?: string;
  } = {}
) {
  return create(GetLineageResponseSchema, {
    relationsSource: (options.source ?? []).map((guid) =>
      create(LineageRelationSchema, { sourceGuid: guid })
    ),
    relationsTarget: (options.target ?? []).map((guid) =>
      create(LineageRelationSchema, { sourceGuid: guid })
    ),
    externalDatasets: (options.datasets ?? []).map((dataset) =>
      create(ExternalDatasetInfoSchema, {
        guid: dataset.guid,
        name: dataset.name ?? dataset.guid,
      })
    ),
    nextPageToken: options.nextPageToken ?? "",
  });
}

beforeEach(() => {
  getLineageMock.mockReset();
});

describe("getLineage", () => {
  it("returns a single page as-is", async () => {
    getLineageMock.mockResolvedValueOnce(
      page({ source: ["s1"], target: ["t1"], datasets: [{ guid: "d1" }] })
    );

    const lineage = await getLineage({ guid: "g1", metaType: MetaType.TABLE });

    expect(lineage.relationsSource.map((r) => r.sourceGuid)).toEqual(["s1"]);
    expect(lineage.relationsTarget.map((r) => r.sourceGuid)).toEqual(["t1"]);
    expect(lineage.externalDatasets.map((d) => d.guid)).toEqual(["d1"]);
    expect(getLineageMock).toHaveBeenCalledTimes(1);
  });

  it("walks every page and merges both relation lists", async () => {
    getLineageMock
      .mockResolvedValueOnce(
        page({
          source: ["s1"],
          datasets: [{ guid: "d1" }],
          nextPageToken: "t1",
        })
      )
      .mockResolvedValueOnce(
        page({
          target: ["t2"],
          datasets: [{ guid: "d2" }],
          nextPageToken: "t2",
        })
      )
      .mockResolvedValueOnce(page({ source: ["s3"], target: ["t3"] }));

    const lineage = await getLineage({ guid: "g1", metaType: MetaType.VIEW });

    expect(lineage.relationsSource.map((r) => r.sourceGuid)).toEqual([
      "s1",
      "s3",
    ]);
    expect(lineage.relationsTarget.map((r) => r.sourceGuid)).toEqual([
      "t2",
      "t3",
    ]);
    expect(lineage.externalDatasets.map((d) => d.guid)).toEqual(["d1", "d2"]);
    expect(getLineageMock).toHaveBeenCalledTimes(3);
  });

  it("sends the page token of the previous response", async () => {
    getLineageMock
      .mockResolvedValueOnce(page({ nextPageToken: "next" }))
      .mockResolvedValueOnce(page());

    await getLineage({
      guid: "g1",
      metaType: MetaType.TABLE,
      lineageType: LineageType.TARGET,
    });

    const first = getLineageMock.mock.calls[0][0];
    const second = getLineageMock.mock.calls[1][0];
    expect(first.guid).toBe("g1");
    expect(first.metaType).toBe(MetaType.TABLE);
    expect(first.lineageType).toBe(LineageType.TARGET);
    expect(first.pageToken).toBe("");
    expect(second.pageToken).toBe("next");
  });

  it("defaults the lineage type to unspecified", async () => {
    getLineageMock.mockResolvedValueOnce(page());

    await getLineage({ guid: "g1", metaType: MetaType.TABLE });

    expect(getLineageMock.mock.calls[0][0].lineageType).toBe(
      LineageType.LINEAGE_TYPE_UNSPECIFIED
    );
  });

  it("deduplicates external datasets by guid, keeping the newest", async () => {
    getLineageMock
      .mockResolvedValueOnce(
        page({ datasets: [{ guid: "d1", name: "old" }], nextPageToken: "t1" })
      )
      .mockResolvedValueOnce(page({ datasets: [{ guid: "d1", name: "new" }] }));

    const lineage = await getLineage({ guid: "g1", metaType: MetaType.TABLE });

    expect(lineage.externalDatasets).toHaveLength(1);
    expect(lineage.externalDatasets[0].name).toBe("new");
  });

  it("stops when the server repeats the page token", async () => {
    getLineageMock
      .mockResolvedValueOnce(page({ nextPageToken: "stuck" }))
      .mockResolvedValue(page({ nextPageToken: "stuck" }));

    const lineage = await getLineage({ guid: "g1", metaType: MetaType.TABLE });

    expect(lineage.relationsSource).toEqual([]);
    expect(getLineageMock).toHaveBeenCalledTimes(2);
  });

  it("returns empty lists for an object without lineage", async () => {
    getLineageMock.mockResolvedValueOnce(page());

    const lineage = await getLineage({ guid: "g1", metaType: MetaType.TABLE });

    expect(lineage.relationsSource).toEqual([]);
    expect(lineage.relationsTarget).toEqual([]);
    expect(lineage.externalDatasets).toEqual([]);
  });
});
