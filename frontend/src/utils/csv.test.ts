import { describe, expect, it } from "vitest";
import { csvCell, csvEscape, fileTimestamp, toCsv } from "./csv";

describe("csv", () => {
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
});
