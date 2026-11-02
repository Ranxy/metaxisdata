import { describe, expect, it } from "vitest";
import {
  joinCodeTableValues,
  mergeCodeTableValues,
  parseCodeTableCsv,
  parseCodeTableLines,
} from "@/utils/qualityCodeTable";

describe("parseCodeTableLines", () => {
  it("trims, ignores blanks and comments, and removes duplicates", () => {
    expect(
      parseCodeTableLines(
        " 110000\n\n# municipality\n110000\n  310000  \n#another\n310000\n"
      )
    ).toEqual(["110000", "310000"]);
  });

  it("accepts CRLF and a UTF-8 BOM", () => {
    expect(parseCodeTableLines("\uFEFFa\r\nb\r\n\r\nc")).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  it("returns an empty list for empty text", () => {
    expect(parseCodeTableLines("")).toEqual([]);
    expect(parseCodeTableLines("  \n# only a comment\n")).toEqual([]);
  });
});

describe("parseCodeTableCsv", () => {
  it("reads the first column of every record", () => {
    expect(
      parseCodeTableCsv("code,title\n110000,Beijing\n310000,Shanghai")
    ).toEqual(["code", "110000", "310000"]);
  });

  it("handles quoted commas and escaped quotes", () => {
    expect(parseCodeTableCsv('"a,b",title\n"say ""hi""",title')).toEqual([
      "a,b",
      'say "hi"',
    ]);
  });

  it("handles CRLF, a trailing newline and a BOM", () => {
    expect(parseCodeTableCsv("\uFEFFa,b\r\nc,d\r\n")).toEqual(["a", "c"]);
  });

  it("ignores everything after the first comma outside quotes", () => {
    expect(parseCodeTableCsv("a,b,c\n,,ignored\nd")).toEqual(["a", "", "d"]);
  });

  it("returns an empty list for an empty document", () => {
    expect(parseCodeTableCsv("")).toEqual([]);
    expect(parseCodeTableCsv("\n\n")).toEqual([]);
  });
});

describe("joinCodeTableValues", () => {
  it("writes one value per line", () => {
    expect(joinCodeTableValues(["a", "b"])).toBe("a\nb");
    expect(joinCodeTableValues([])).toBe("");
  });
});

describe("mergeCodeTableValues", () => {
  it("appends imported codes to the typed ones without duplicates", () => {
    expect(mergeCodeTableValues("a\nb\n# note\n", ["b", "c", "a", "d"])).toBe(
      "a\nb\nc\nd"
    );
  });

  it("keeps the typed order and ignores blank input", () => {
    expect(mergeCodeTableValues("", ["b", "a"])).toBe("b\na");
    expect(mergeCodeTableValues("a", [])).toBe("a");
  });
});
