import { describe, expect, it, vi } from "vitest";
import { type ListPage, listAll } from "./list";

describe("listAll", () => {
  it("walks every page and concatenates the items", async () => {
    const pages: Record<string, ListPage<string>> = {
      "": { items: ["a", "b"], nextPageToken: "p2" },
      p2: { items: ["c"], nextPageToken: "p3" },
      p3: { items: ["d"], nextPageToken: "" },
    };
    const fetchPage = vi.fn(async (token: string) => pages[token]);

    await expect(listAll(fetchPage)).resolves.toEqual(["a", "b", "c", "d"]);
    expect(fetchPage.mock.calls.map(([token]) => token)).toEqual([
      "",
      "p2",
      "p3",
    ]);
  });

  it("stops after one page when there is no token", async () => {
    const fetchPage = vi.fn(async () => ({ items: ["a"], nextPageToken: "" }));

    await expect(listAll(fetchPage)).resolves.toEqual(["a"]);
    expect(fetchPage).toHaveBeenCalledTimes(1);
  });

  it("stops on a token that does not advance", async () => {
    // A server that echoes the token back must not loop forever.
    const fetchPage = vi.fn(async () => ({ items: ["a"], nextPageToken: "x" }));

    await expect(listAll(fetchPage)).resolves.toEqual(["a", "a"]);
    expect(fetchPage).toHaveBeenCalledTimes(2);
  });

  it("returns nothing for an empty first page", async () => {
    const fetchPage = vi.fn(async () => ({ items: [], nextPageToken: "" }));

    await expect(listAll(fetchPage)).resolves.toEqual([]);
  });

  it("propagates a failed page instead of returning a partial list", async () => {
    const fetchPage = vi
      .fn<(token: string) => Promise<ListPage<string>>>()
      .mockResolvedValueOnce({ items: ["a"], nextPageToken: "p2" })
      .mockRejectedValueOnce(new Error("boom"));

    await expect(listAll(fetchPage)).rejects.toThrow("boom");
  });
});
