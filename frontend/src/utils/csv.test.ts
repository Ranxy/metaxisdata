import { afterEach, describe, expect, it, vi } from "vitest";
import { csvCell, csvEscape, downloadCsv, fileTimestamp, toCsv } from "./csv";

let downloaded: Blob | undefined;

describe("csv", () => {
  afterEach(() => {
    downloaded = undefined;
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("quotes, doubles inner quotes and normalizes line endings", () => {
    expect(csvEscape('say "hi"')).toBe('"say ""hi"""');
    expect(csvEscape("a\r\nb\rc")).toBe('"a\nb\nc"');
    expect(csvEscape(null)).toBe('""');
  });

  it("writes objects as JSON and leaves strings alone", () => {
    expect(csvCell({ a: 1 })).toBe('{"a":1}');
    expect(csvCell("plain")).toBe("plain");
    expect(csvCell(undefined)).toBe("");
  });

  it("prints an int64 instead of failing on its bigint", () => {
    // JSON.stringify throws on a bigint, which every int64 proto field is.
    expect(csvCell(12n)).toBe("12");
    expect(csvEscape(12n)).toBe('"12"');
    expect(() => JSON.stringify(12n)).toThrow();
  });

  it("joins a header with its rows", () => {
    expect(
      toCsv(
        ["a", "b"],
        [
          [1, "x"],
          [2, 'y"z'],
        ]
      )
    ).toBe('"a","b"\n"1","x"\n"2","y""z"');
  });

  it("stamps a filename-safe local time", () => {
    expect(fileTimestamp(new Date(2026, 8, 26, 12, 8, 30))).toBe(
      "20260926-120830"
    );
  });

  it("downloads through a temporary anchor and revokes the object URL", () => {
    // jsdom implements Blob but not the object-URL half of the download path.
    const createObjectURL = vi.fn((blob: Blob) => {
      downloaded = blob;
      return "blob:csv";
    });
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", { createObjectURL, revokeObjectURL });
    const clicked: HTMLAnchorElement[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
      this: HTMLAnchorElement
    ) {
      clicked.push(this);
    });

    downloadCsv("a,b\n1,2", "audit-logs.csv");

    expect(downloaded?.type).toBe("text/csv;charset=utf-8;");
    expect(downloaded?.size).toBe(7);
    expect(clicked).toHaveLength(1);
    expect(clicked[0]?.download).toBe("audit-logs.csv");
    expect(clicked[0]?.href).toContain("blob:csv");
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:csv");
    // The anchor is only a vehicle for the click; it must not stay in the DOM.
    expect(document.body.querySelector("a")).toBeNull();
  });
});
