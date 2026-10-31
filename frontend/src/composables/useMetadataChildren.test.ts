import { beforeEach, describe, expect, it, vi } from "vitest";
import { effectScope } from "vue";
import {
  MetaType,
  type SearchMetadataResult,
  type StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import { useMetadataChildren } from "./useMetadataChildren";

const mocks = vi.hoisted(() => ({
  listMetadata: vi.fn(),
  searchMetadata: vi.fn(),
}));

vi.mock("@/api/database", () => ({
  listMetadata: mocks.listMetadata,
  searchMetadata: mocks.searchMetadata,
}));

/** A stored metadata row as the list/search responses carry it. */
function stored(guid: string, name?: string): StoredMetadata {
  return {
    guid,
    type:
      name === undefined
        ? { case: undefined, value: undefined }
        : { case: "tableMetadata", value: { name } },
  } as unknown as StoredMetadata;
}

function listResponse(
  groups: Array<{
    metaType: MetaType;
    rows: Array<[guid: string, name?: string]>;
    nextPageToken?: string;
  }>
) {
  return {
    typesStoredMetadata: groups.map((group) => ({
      metaType: group.metaType,
      list: group.rows.map(([guid, name]) => stored(guid, name)),
      nextPageToken: group.nextPageToken ?? "",
    })),
  };
}

function searchResponse(
  results: Array<[guid: string, name?: string] | null>,
  nextPageToken = ""
) {
  return {
    results: results.flatMap((entry): SearchMetadataResult[] => {
      if (entry === null) {
        return [];
      }
      const [guid, name] = entry;
      return [
        {
          guid,
          metaType: MetaType.TABLE,
          metadata: name === undefined ? undefined : stored(guid, name),
        } as SearchMetadataResult,
      ];
    }),
    nextPageToken,
  };
}

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (reason: unknown) => void;
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

/** The signal the API mock received for call `index`. */
function signalOf(fn: ReturnType<typeof vi.fn>, index: number): AbortSignal {
  return fn.mock.calls[index][0].signal as AbortSignal;
}

describe("useMetadataChildren", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("loads only the requested meta types, in the requested order", async () => {
    mocks.listMetadata.mockResolvedValue(
      listResponse([
        {
          metaType: MetaType.TABLE,
          rows: [
            ["inst;db;public;orders", "orders"],
            ["inst;db;public;users", "users"],
          ],
        },
        { metaType: MetaType.VIEW, rows: [["inst;db;public;v", "v"]] },
        // A sibling type the caller did not ask for must not leak in.
        { metaType: MetaType.FUNCTION, rows: [["inst;db;public;f", "f"]] },
      ])
    );
    const browser = useMetadataChildren();

    const loaded = await browser.load("inst;db;public", [
      MetaType.TABLE,
      MetaType.VIEW,
    ]);

    expect(mocks.listMetadata).toHaveBeenCalledWith(
      expect.objectContaining({ parentGuid: "inst;db;public", pageSize: 50 })
    );
    expect(loaded?.map((child) => child.name)).toEqual([
      "orders",
      "users",
      "v",
    ]);
    expect(browser.items.value.map((child) => child.guid)).toEqual([
      "inst;db;public;orders",
      "inst;db;public;users",
      "inst;db;public;v",
    ]);
    expect(browser.items.value[0].metaType).toBe(MetaType.TABLE);
    expect(browser.items.value[2].metaType).toBe(MetaType.VIEW);
    expect(browser.loading.value).toBe(false);
    expect(browser.hasNext.value).toBe(false);
    expect(browser.error.value).toBeUndefined();
    expect(browser.query.value).toBe("");
  });

  it("honors a custom page size", async () => {
    mocks.listMetadata.mockResolvedValue(
      listResponse([{ metaType: MetaType.DATABASE, rows: [] }])
    );
    const browser = useMetadataChildren({ pageSize: 10 });

    await browser.load("inst", [MetaType.DATABASE]);

    expect(mocks.listMetadata).toHaveBeenCalledWith(
      expect.objectContaining({ pageSize: 10 })
    );
  });

  it("drops blank names and falls back to the GUID as the name", async () => {
    mocks.listMetadata.mockResolvedValue(
      listResponse([
        {
          metaType: MetaType.SCHEMA,
          rows: [
            ["inst;db;public", "public"],
            ["inst;db;blank", "   "],
            ["inst;db;no-name"],
          ],
        },
      ])
    );
    const browser = useMetadataChildren();

    await browser.load("inst;db", [MetaType.SCHEMA]);

    expect(
      browser.items.value.map(({ guid, name, metaType }) => ({
        guid,
        name,
        metaType,
      }))
    ).toEqual([
      { guid: "inst;db;public", name: "public", metaType: MetaType.SCHEMA },
      {
        guid: "inst;db;no-name",
        name: "inst;db;no-name",
        metaType: MetaType.SCHEMA,
      },
    ]);
  });

  it("reports a first-page failure and clears the level", async () => {
    const failure = new Error("boom");
    mocks.listMetadata.mockRejectedValue(failure);
    const browser = useMetadataChildren();

    const loaded = await browser.load("inst", [MetaType.DATABASE]);

    expect(loaded).toEqual([]);
    expect(browser.error.value).toBe(failure);
    expect(browser.items.value).toEqual([]);
    expect(browser.loading.value).toBe(false);
    expect(browser.hasNext.value).toBe(false);
  });

  it("advances every meta-type source independently on load more", async () => {
    mocks.listMetadata
      .mockResolvedValueOnce(
        listResponse([
          {
            metaType: MetaType.TABLE,
            rows: [["t1", "orders"]],
            nextPageToken: "table-token",
          },
          {
            metaType: MetaType.VIEW,
            rows: [["v1", "order_view"]],
            nextPageToken: "view-token",
          },
        ])
      )
      .mockResolvedValueOnce(
        listResponse([
          {
            metaType: MetaType.TABLE,
            rows: [["t2", "users"]],
            nextPageToken: "",
          },
        ])
      )
      .mockResolvedValueOnce(
        listResponse([
          {
            metaType: MetaType.VIEW,
            rows: [["v2", "user_view"]],
            nextPageToken: "",
          },
        ])
      );
    const browser = useMetadataChildren();

    await browser.load("inst;db;public", [MetaType.TABLE, MetaType.VIEW]);
    expect(browser.hasNext.value).toBe(true);

    await browser.loadMore();

    expect(mocks.listMetadata).toHaveBeenCalledWith(
      expect.objectContaining({
        parentGuid: "inst;db;public",
        pageToken: "table-token",
        metaType: MetaType.TABLE,
      })
    );
    expect(mocks.listMetadata).toHaveBeenCalledWith(
      expect.objectContaining({
        parentGuid: "inst;db;public",
        pageToken: "view-token",
        metaType: MetaType.VIEW,
      })
    );
    expect(browser.items.value.map((child) => child.guid)).toEqual([
      "t1",
      "t2",
      "v1",
      "v2",
    ]);
    expect(browser.hasNext.value).toBe(false);
    expect(browser.loadingMore.value).toBe(false);
  });

  it("load more is a no-op without a token or while a first page runs", async () => {
    mocks.listMetadata.mockResolvedValue(
      listResponse([{ metaType: MetaType.DATABASE, rows: [["db1", "db1"]] }])
    );
    const browser = useMetadataChildren();
    await browser.load("inst", [MetaType.DATABASE]);

    await browser.loadMore();
    expect(mocks.listMetadata).toHaveBeenCalledTimes(1);

    // A fresh first page is in flight: load more must not interleave.
    const pending = deferred<unknown>();
    mocks.listMetadata.mockReturnValueOnce(pending.promise);
    const loading = browser.load("inst", [MetaType.DATABASE]);
    await browser.loadMore();
    expect(mocks.listMetadata).toHaveBeenCalledTimes(2);
    pending.resolve(listResponse([{ metaType: MetaType.DATABASE, rows: [] }]));
    await loading;
  });

  it("searches once per meta type scoped to the parent and merges in order", async () => {
    mocks.searchMetadata
      .mockResolvedValueOnce(searchResponse([["t1", "orders"]]))
      .mockResolvedValueOnce(
        searchResponse([["v1", "orders_view"]], "view-token")
      );
    const browser = useMetadataChildren();

    await browser.search(
      "inst;db;public",
      [MetaType.TABLE, MetaType.VIEW],
      "ord"
    );

    expect(mocks.searchMetadata).toHaveBeenCalledTimes(2);
    expect(mocks.searchMetadata).toHaveBeenCalledWith(
      expect.objectContaining({
        searchStr: "ord",
        parentGuidPrefix: "inst;db;public",
        metaType: MetaType.TABLE,
        pageSize: 50,
      })
    );
    expect(mocks.searchMetadata).toHaveBeenCalledWith(
      expect.objectContaining({
        searchStr: "ord",
        parentGuidPrefix: "inst;db;public",
        metaType: MetaType.VIEW,
      })
    );
    expect(browser.items.value.map((child) => child.guid)).toEqual([
      "t1",
      "v1",
    ]);
    expect(browser.query.value).toBe("ord");
    expect(browser.hasNext.value).toBe(true);
  });

  it("trims the search term and a blank one restores the list", async () => {
    mocks.listMetadata.mockResolvedValue(
      listResponse([{ metaType: MetaType.TABLE, rows: [["t1", "orders"]] }])
    );
    const browser = useMetadataChildren();

    await browser.search("inst;db;public", [MetaType.TABLE], "   ");

    expect(mocks.listMetadata).toHaveBeenCalledWith(
      expect.objectContaining({ parentGuid: "inst;db;public", pageSize: 50 })
    );
    expect(mocks.searchMetadata).not.toHaveBeenCalled();
    expect(browser.query.value).toBe("");
    expect(browser.items.value.map((child) => child.guid)).toEqual(["t1"]);
  });

  it("drops search results that carry no name", async () => {
    mocks.searchMetadata.mockResolvedValue(
      searchResponse([null, ["t1", undefined], ["t2", "users"]])
    );
    const browser = useMetadataChildren();

    await browser.search("inst;db;public", [MetaType.TABLE], "ord");

    expect(browser.items.value.map((child) => child.guid)).toEqual(["t2"]);
  });

  it("appends search pages with the per-type token", async () => {
    mocks.searchMetadata
      .mockResolvedValueOnce(searchResponse([["t1", "orders"]], "page-2"))
      .mockResolvedValueOnce(searchResponse([["t2", "orders_archive"]]));
    const browser = useMetadataChildren();

    await browser.search("inst;db;public", [MetaType.TABLE], "ord");
    await browser.loadMore();

    expect(mocks.searchMetadata).toHaveBeenLastCalledWith(
      expect.objectContaining({
        searchStr: "ord",
        parentGuidPrefix: "inst;db;public",
        pageToken: "page-2",
        metaType: MetaType.TABLE,
      })
    );
    expect(browser.items.value.map((child) => child.guid)).toEqual([
      "t1",
      "t2",
    ]);
  });

  it("reports a search failure without leaving the previous rows", async () => {
    const failure = new Error("search failed");
    mocks.searchMetadata.mockRejectedValue(failure);
    const browser = useMetadataChildren();

    await browser.search("inst;db;public", [MetaType.TABLE], "ord");

    expect(browser.error.value).toBe(failure);
    expect(browser.items.value).toEqual([]);
    expect(browser.loading.value).toBe(false);
    expect(browser.query.value).toBe("ord");
  });

  it("keeps the loaded rows when a follow-up page fails", async () => {
    mocks.listMetadata
      .mockResolvedValueOnce(
        listResponse([
          {
            metaType: MetaType.TABLE,
            rows: [["t1", "orders"]],
            nextPageToken: "page-2",
          },
        ])
      )
      .mockRejectedValueOnce(new Error("page failed"));
    const browser = useMetadataChildren();
    await browser.load("inst;db;public", [MetaType.TABLE]);

    await browser.loadMore();

    expect(browser.moreError.value).toBeInstanceOf(Error);
    expect(browser.items.value.map((child) => child.guid)).toEqual(["t1"]);
    expect(browser.hasNext.value).toBe(true);
    expect(browser.loadingMore.value).toBe(false);
  });

  it("a stale first page cannot overwrite a newer level", async () => {
    const first = deferred<unknown>();
    mocks.listMetadata
      .mockReturnValueOnce(first.promise)
      .mockResolvedValueOnce(
        listResponse([{ metaType: MetaType.TABLE, rows: [["new", "new"]] }])
      );
    const browser = useMetadataChildren();

    const staleLoad = browser.load("old-parent", [MetaType.TABLE]);
    const freshLoad = browser.load("new-parent", [MetaType.TABLE]);
    await freshLoad;

    expect(signalOf(mocks.listMetadata, 0).aborted).toBe(true);
    first.resolve(
      listResponse([{ metaType: MetaType.TABLE, rows: [["old", "old"]] }])
    );
    await staleLoad;

    expect(browser.items.value.map((child) => child.guid)).toEqual(["new"]);
    expect(browser.error.value).toBeUndefined();
  });

  it("reports a superseded load as null rather than an empty level", async () => {
    const first = deferred<unknown>();
    mocks.listMetadata
      .mockReturnValueOnce(first.promise)
      .mockResolvedValueOnce(
        listResponse([{ metaType: MetaType.TABLE, rows: [["new", "new"]] }])
      );
    const browser = useMetadataChildren();

    const staleLoad = browser.load("old-parent", [MetaType.TABLE]);
    const fresh = await browser.load("new-parent", [MetaType.TABLE]);
    first.resolve(listResponse([]));

    await expect(staleLoad).resolves.toBeNull();
    expect(fresh?.map((child) => child.guid)).toEqual(["new"]);
  });

  it("pages only the meta-type sources that returned a token", async () => {
    mocks.listMetadata
      .mockResolvedValueOnce(
        listResponse([
          // TABLE has no rows at all, so the response has no TABLE group.
          {
            metaType: MetaType.VIEW,
            rows: [["v1", "view"]],
            nextPageToken: "view-page-2",
          },
        ])
      )
      .mockResolvedValueOnce(
        listResponse([{ metaType: MetaType.VIEW, rows: [["v2", "view2"]] }])
      );
    const browser = useMetadataChildren();

    const loaded = await browser.load("inst;db;public", [
      MetaType.TABLE,
      MetaType.VIEW,
    ]);
    expect(loaded?.map((child) => child.guid)).toEqual(["v1"]);
    expect(browser.hasNext.value).toBe(true);

    await browser.loadMore();

    expect(mocks.listMetadata).toHaveBeenCalledTimes(2);
    expect(mocks.listMetadata).toHaveBeenLastCalledWith(
      expect.objectContaining({
        metaType: MetaType.VIEW,
        pageToken: "view-page-2",
      })
    );
    expect(browser.items.value.map((child) => child.guid)).toEqual([
      "v1",
      "v2",
    ]);
  });

  it("ignores an abort rejection from the superseded request", async () => {
    mocks.listMetadata
      .mockImplementationOnce(
        (options: { signal: AbortSignal }) =>
          new Promise((_, reject) => {
            options.signal.addEventListener("abort", () =>
              reject(new DOMException("aborted", "AbortError"))
            );
          })
      )
      .mockResolvedValueOnce(
        listResponse([{ metaType: MetaType.TABLE, rows: [["new", "new"]] }])
      );
    const browser = useMetadataChildren();

    const staleLoad = browser.load("old-parent", [MetaType.TABLE]);
    await browser.load("new-parent", [MetaType.TABLE]);

    await expect(staleLoad).resolves.toBeNull();
    expect(browser.error.value).toBeUndefined();
    expect(browser.loading.value).toBe(false);
    expect(browser.items.value.map((child) => child.guid)).toEqual(["new"]);
  });

  it("a stale search cannot overwrite a newer one", async () => {
    const first = deferred<unknown>();
    mocks.searchMetadata
      .mockReturnValueOnce(first.promise)
      .mockResolvedValueOnce(searchResponse([["new", "new"]]));
    const browser = useMetadataChildren();

    const staleSearch = browser.search(
      "inst;db;public",
      [MetaType.TABLE],
      "old"
    );
    await browser.search("inst;db;public", [MetaType.TABLE], "new");
    first.resolve(searchResponse([["old", "old"]]));
    await staleSearch;

    expect(browser.items.value.map((child) => child.guid)).toEqual(["new"]);
    expect(browser.query.value).toBe("new");
  });

  it("a new level clears the loading flag of a follow-up page in flight", async () => {
    const page = deferred<unknown>();
    mocks.listMetadata
      .mockResolvedValueOnce(
        listResponse([
          {
            metaType: MetaType.TABLE,
            rows: [["t1", "orders"]],
            nextPageToken: "page-2",
          },
        ])
      )
      .mockReturnValueOnce(page.promise)
      .mockResolvedValueOnce(
        listResponse([{ metaType: MetaType.TABLE, rows: [["t9", "other"]] }])
      );
    const browser = useMetadataChildren();
    await browser.load("inst;db;public", [MetaType.TABLE]);

    const more = browser.loadMore();
    expect(browser.loadingMore.value).toBe(true);
    await browser.load("inst;db;other", [MetaType.TABLE]);
    page.resolve(listResponse([{ metaType: MetaType.TABLE, rows: [] }]));
    await more;

    expect(browser.loadingMore.value).toBe(false);
    expect(browser.items.value.map((child) => child.guid)).toEqual(["t9"]);
  });

  it("reset invalidates requests still on the wire", async () => {
    const pending = deferred<unknown>();
    mocks.listMetadata.mockReturnValueOnce(pending.promise);
    const browser = useMetadataChildren();

    const load = browser.load("inst", [MetaType.DATABASE]);
    browser.reset();
    pending.resolve(
      listResponse([{ metaType: MetaType.DATABASE, rows: [["db1", "db1"]] }])
    );
    await load;

    expect(browser.items.value).toEqual([]);
    expect(browser.loading.value).toBe(false);
    expect(browser.error.value).toBeUndefined();
    expect(browser.query.value).toBe("");
    expect(signalOf(mocks.listMetadata, 0).aborted).toBe(true);
  });

  it("aborts an in-flight first page when the level changes", async () => {
    const pending = deferred<unknown>();
    mocks.listMetadata
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValueOnce(
        listResponse([{ metaType: MetaType.TABLE, rows: [["t", "t"]] }])
      );
    const browser = useMetadataChildren();

    void browser.load("inst;db;public", [MetaType.TABLE]);
    await browser.load("inst;db;other", [MetaType.TABLE]);

    expect(signalOf(mocks.listMetadata, 0).aborted).toBe(true);
    pending.resolve(
      listResponse([{ metaType: MetaType.TABLE, rows: [["stale", "stale"]] }])
    );
    expect(browser.items.value.map((child) => child.guid)).toEqual(["t"]);
  });

  it("aborts a request when the owning scope is disposed", async () => {
    const pending = deferred<unknown>();
    mocks.listMetadata.mockReturnValueOnce(pending.promise);
    const scope = effectScope();
    const browser = scope.run(() => useMetadataChildren())!;

    const load = browser.load("inst", [MetaType.DATABASE]);
    scope.stop();
    pending.resolve(
      listResponse([{ metaType: MetaType.DATABASE, rows: [["db1", "db1"]] }])
    );
    await load;

    expect(signalOf(mocks.listMetadata, 0).aborted).toBe(true);
    expect(browser.items.value).toEqual([]);
  });

  it("keeps every group the response carried when no meta types are requested", async () => {
    mocks.listMetadata.mockResolvedValue(
      listResponse([
        { metaType: MetaType.DATABASE, rows: [["db1", "db1"]] },
        { metaType: MetaType.SCHEMA, rows: [["db1;public", "public"]] },
      ])
    );
    const browser = useMetadataChildren();

    const children = await browser.load("inst", []);

    expect(children?.map((child) => child.guid)).toEqual(["db1", "db1;public"]);
    expect(children?.[0].metadata?.guid).toBe("db1");
    expect(browser.items.value.map((child) => child.metaType)).toEqual([
      MetaType.DATABASE,
      MetaType.SCHEMA,
    ]);
  });

  it("searches once without a type when no meta types are requested", async () => {
    mocks.searchMetadata
      .mockResolvedValueOnce(searchResponse([["g1", "g1"]], "next"))
      .mockResolvedValueOnce(searchResponse([["g2", "g2"]]));
    const browser = useMetadataChildren();

    await browser.search("inst", [], "orders");
    expect(mocks.searchMetadata).toHaveBeenCalledTimes(1);
    const first = mocks.searchMetadata.mock.calls[0][0] as {
      metaType?: MetaType;
    };
    expect(first.metaType).toBeUndefined();
    expect(browser.items.value.map((child) => child.guid)).toEqual(["g1"]);

    await browser.loadMore();
    expect(mocks.searchMetadata).toHaveBeenCalledTimes(2);
    const second = mocks.searchMetadata.mock.calls[1][0] as {
      metaType?: MetaType;
    };
    expect(second.metaType).toBeUndefined();
    expect(browser.items.value.map((child) => child.guid)).toEqual([
      "g1",
      "g2",
    ]);
  });

  it("load more can advance one meta type only", async () => {
    mocks.listMetadata
      .mockResolvedValueOnce(
        listResponse([
          {
            metaType: MetaType.TABLE,
            rows: [["t1", "t1"]],
            nextPageToken: "table-page",
          },
          {
            metaType: MetaType.VIEW,
            rows: [["v1", "v1"]],
            nextPageToken: "view-page",
          },
        ])
      )
      .mockResolvedValueOnce(
        listResponse([{ metaType: MetaType.TABLE, rows: [["t2", "t2"]] }])
      );
    const browser = useMetadataChildren();

    await browser.load("inst;db;public", []);
    expect(browser.hasNextFor(MetaType.TABLE)).toBe(true);
    expect(browser.hasNextFor(MetaType.VIEW)).toBe(true);

    await browser.loadMore(MetaType.TABLE);
    expect(mocks.listMetadata).toHaveBeenCalledTimes(2);
    expect(browser.hasNextFor(MetaType.TABLE)).toBe(false);
    expect(browser.hasNextFor(MetaType.VIEW)).toBe(true);
    expect(browser.items.value.map((child) => child.guid)).toEqual([
      "t1",
      "t2",
      "v1",
    ]);

    // A type with no page left must not start a request.
    await browser.loadMore(MetaType.FUNCTION);
    expect(mocks.listMetadata).toHaveBeenCalledTimes(2);
  });
});
