import { afterEach, describe, expect, it, vi } from "vitest";
import {
  collectCsvStream,
  csvCell,
  csvEscape,
  downloadBlob,
  downloadCsv,
  fileTimestamp,
  toCsv,
} from "./csv";

let downloaded: Blob | undefined;

/** The chunks of a streamed export, as the server's slices arrive. */
async function* slices(...parts: Array<[string, bigint]>) {
  for (const [text, rowCount] of parts) {
    yield { data: new TextEncoder().encode(text), rowCount };
  }
}

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

  it("concatenates the slices of a streamed export verbatim", async () => {
    // The server writes the BOM and the header into the first slice; splitting
    // the document again must not change a byte of it.
    const file = await collectCsvStream(
      slices(['\uFEFF"a","b"\n"1"', 1n], [',"x"\n"2","y"', 2n])
    );

    expect(file.rowCount).toBe(2n);
    expect(file.blob.type).toBe("text/csv;charset=utf-8;");
    const bytes = new Uint8Array(await file.blob.arrayBuffer());
    expect([...bytes.slice(0, 3)]).toEqual([0xef, 0xbb, 0xbf]);
    // `Blob.text()` strips the BOM while decoding; the bytes above keep it.
    expect(await file.blob.text()).toBe('"a","b"\n"1","x"\n"2","y"');
  });

  it("rejects a stream that delivered no bytes at all", async () => {
    // Even a zero-row export carries the BOM and the header row, so an empty
    // stream is a broken export rather than an empty selection.
    await expect(collectCsvStream(slices())).rejects.toThrow(
      "The export returned no data."
    );
    await expect(
      collectCsvStream(
        (async function* () {
          yield { data: new Uint8Array(0), rowCount: 0n };
        })()
      )
    ).rejects.toThrow("The export returned no data.");
  });

  it("rejects a stream whose row count goes backwards", async () => {
    async function* backwards() {
      yield { data: new Uint8Array([1]), rowCount: 5n };
      yield { data: new Uint8Array([2]), rowCount: 2n };
    }

    await expect(collectCsvStream(backwards())).rejects.toThrow(
      "The export stream's row count went backwards."
    );
  });

  it("reports a file the server's row ceiling cut short", async () => {
    async function* capped() {
      yield { data: new Uint8Array([1]), rowCount: 1n, truncated: false };
      yield { data: new Uint8Array([2]), rowCount: 100_000n, truncated: true };
    }

    const file = await collectCsvStream(capped());

    expect(file.rowCount).toBe(100_000n);
    expect(file.truncated).toBe(true);
  });

  it("downloads an assembled blob under its name", () => {
    const createObjectURL = vi.fn(() => "blob:report");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", { createObjectURL, revokeObjectURL });
    const clicked: HTMLAnchorElement[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
      this: HTMLAnchorElement
    ) {
      clicked.push(this);
    });
    const blob = new Blob(["<html></html>"], { type: "text/html" });

    downloadBlob(blob, "quality-report.html");

    expect(createObjectURL).toHaveBeenCalledWith(blob);
    expect(clicked[0]?.download).toBe("quality-report.html");
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:report");
    expect(document.body.querySelector("a")).toBeNull();
  });
});
