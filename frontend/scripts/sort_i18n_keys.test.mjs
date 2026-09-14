// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  checkLocaleFiles,
  sortLocaleFiles,
  sortObjectKeys,
} from "./sort_i18n_keys.mjs";

describe("sortObjectKeys", () => {
  it("sorts nested object keys recursively and preserves values", () => {
    const input = { b: 1, a: { d: [3, 2], c: "x" } };
    expect(sortObjectKeys(input)).toEqual({
      a: { c: "x", d: [3, 2] },
      b: 1,
    });
  });

  it("leaves arrays, scalars and null untouched", () => {
    expect(sortObjectKeys([3, 1])).toEqual([3, 1]);
    expect(sortObjectKeys("value")).toBe("value");
    expect(sortObjectKeys(null)).toBeNull();
  });
});

describe("checkLocaleFiles", () => {
  it("reports only the files whose formatting is not normalized", () => {
    const contents = {
      "/sorted.json": '{\n  "a": 2,\n  "b": 1\n}\n',
      "/unsorted.json": '{"b": 1, "a": 2}',
    };
    const readFileSync = (file) => contents[file];

    expect(
      checkLocaleFiles(["/sorted.json", "/unsorted.json"], { readFileSync })
    ).toEqual(["/unsorted.json"]);
  });

  it("throws a descriptive error for invalid JSON", () => {
    const readFileSync = () => "{ not json";
    expect(() => checkLocaleFiles(["/bad.json"], { readFileSync })).toThrow(
      /Failed to parse locale file/
    );
  });
});

describe("sortLocaleFiles", () => {
  it("writes the normalized content and returns the updated files", () => {
    const writes = [];
    const updated = sortLocaleFiles(["/a.json"], {
      readFileSync: () => '{"b":1,"a":2}',
      writeFileSync: (file, content) => writes.push([file, content]),
    });

    expect(updated).toEqual(["/a.json"]);
    expect(writes).toEqual([["/a.json", '{\n  "a": 2,\n  "b": 1\n}\n']]);
  });

  it("skips files that are already normalized", () => {
    const writes = [];
    const updated = sortLocaleFiles(["/a.json"], {
      readFileSync: () => '{\n  "a": 2\n}\n',
      writeFileSync: (file, content) => writes.push([file, content]),
    });

    expect(updated).toEqual([]);
    expect(writes).toEqual([]);
  });

  it("rolls back earlier writes when a later write fails", () => {
    const contents = {
      "/a.json": '{"b":1,"a":2}',
      "/b.json": '{"d":1,"c":2}',
    };
    const writes = [];
    const writeFileSync = (file, content) => {
      if (file === "/b.json") {
        throw new Error("disk full");
      }
      writes.push([file, content]);
    };

    expect(() =>
      sortLocaleFiles(["/a.json", "/b.json"], {
        readFileSync: (file) => contents[file],
        writeFileSync,
      })
    ).toThrow(/disk full/);

    // /a.json was written, then restored to its original content.
    expect(writes).toEqual([
      ["/a.json", '{\n  "a": 2,\n  "b": 1\n}\n'],
      ["/a.json", contents["/a.json"]],
    ]);
  });
});
