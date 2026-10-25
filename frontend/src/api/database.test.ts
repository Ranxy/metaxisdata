import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  MetaType,
  type StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";

const mocks = vi.hoisted(() => ({
  listMetadata: vi.fn(),
  searchMetadata: vi.fn(),
}));

vi.mock("./client", () => ({
  databaseClient: {
    listMetadata: mocks.listMetadata,
    searchMetadata: mocks.searchMetadata,
  },
}));

import { listAllMetadata, listMetadata, searchMetadata } from "./database";

function stored(guid: string): StoredMetadata {
  return {
    guid,
    type: { case: "columnMetadata", value: { name: guid } },
  } as unknown as StoredMetadata;
}

function page(
  groups: Array<{
    metaType: MetaType;
    list: StoredMetadata[];
    nextPageToken?: string;
  }>
) {
  return {
    typesStoredMetadata: groups.map((group) => ({
      metaType: group.metaType,
      list: group.list,
      nextPageToken: group.nextPageToken ?? "",
    })),
  };
}

describe("listAllMetadata", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("walks every page of one meta type and ignores sibling groups", async () => {
    mocks.listMetadata
      .mockResolvedValueOnce(
        page([
          {
            metaType: MetaType.COLUMN,
            list: [stored("c1")],
            nextPageToken: "page-2",
          },
          { metaType: MetaType.INDEX, list: [stored("i1")] },
        ])
      )
      .mockResolvedValueOnce(
        page([{ metaType: MetaType.COLUMN, list: [stored("c2")] }])
      );

    const columns = await listAllMetadata({
      parentGuid: "inst;db;schema;table",
      metaType: MetaType.COLUMN,
    });

    expect(columns.map((column) => column.guid)).toEqual(["c1", "c2"]);
    expect(mocks.listMetadata).toHaveBeenCalledTimes(2);
    expect(mocks.listMetadata).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        parentGuid: "inst;db;schema;table",
        metaType: MetaType.COLUMN,
        pageSize: 1000,
        pageToken: "",
      }),
      expect.anything()
    );
    expect(mocks.listMetadata).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ pageToken: "page-2" }),
      expect.anything()
    );
  });

  it("forwards paging and the abort signal through the wrappers", async () => {
    const signal = new AbortController().signal;
    mocks.listMetadata.mockResolvedValueOnce({ typesStoredMetadata: [] });
    await listMetadata({
      parentGuid: "inst;db",
      pageSize: 20,
      pageToken: "next",
      metaType: MetaType.DATABASE,
      signal,
    });
    expect(mocks.listMetadata).toHaveBeenLastCalledWith(
      expect.objectContaining({
        parentGuid: "inst;db",
        pageSize: 20,
        pageToken: "next",
        metaType: MetaType.DATABASE,
      }),
      { signal }
    );

    mocks.searchMetadata.mockResolvedValueOnce({
      results: [],
      nextPageToken: "",
    });
    await searchMetadata({
      searchStr: "ord",
      parentGuidPrefix: "inst;db;schema",
      metaType: MetaType.TABLE,
      pageSize: 30,
      pageToken: "s2",
      signal,
    });
    expect(mocks.searchMetadata).toHaveBeenLastCalledWith(
      expect.objectContaining({
        searchStr: "ord",
        parentGuidPrefix: "inst;db;schema",
        metaType: MetaType.TABLE,
        pageSize: 30,
        pageToken: "s2",
      }),
      { signal }
    );
  });

  it("stops as soon as the response has no group of the wanted type", async () => {
    mocks.listMetadata.mockResolvedValueOnce(page([]));

    await expect(
      listAllMetadata({
        parentGuid: "inst;db;schema;table",
        metaType: MetaType.COLUMN,
      })
    ).resolves.toEqual([]);
    expect(mocks.listMetadata).toHaveBeenCalledTimes(1);
  });
});
