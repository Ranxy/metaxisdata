// @vitest-environment node
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  assetLabel,
  formatBytes,
  formatMetrics,
  parseArgs,
  parseTypeCheckDiagnostics,
  readDistEntries,
  summarizeCoverage,
  summarizeDist,
  summarizeResults,
} from "./frontend-metrics.mjs";

describe("parseTypeCheckDiagnostics", () => {
  it("reads the aggregate line of a multi-project build", () => {
    const diagnostics =
      "Files:                         265\n" +
      "Memory used:               187648K\n" +
      "Files:                        1539\n" +
      "Memory used:               891064K\n" +
      "Aggregate Files:                        1804\n" +
      "Aggregate Memory used:               891064K\n" +
      "Build time:                           12.70s\n";

    expect(parseTypeCheckDiagnostics(diagnostics)).toEqual({
      memoryBytes: 891064 * 1024,
      files: 1804,
      buildTimeSeconds: 12.7,
    });
  });

  it("has nothing to report without diagnostics", () => {
    expect(parseTypeCheckDiagnostics("")).toBeNull();
    expect(parseTypeCheckDiagnostics("src/a.ts(3,1): error TS2322")).toBeNull();
  });

  it("keeps whatever the run did print", () => {
    expect(parseTypeCheckDiagnostics("Build time:                           3.50s\n")).toEqual(
      { memoryBytes: null, files: null, buildTimeSeconds: 3.5 }
    );
  });
});

describe("formatBytes", () => {
  it("scales to the unit that keeps the number readable", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(1_572_864)).toBe("1.5 MB");
    expect(formatBytes(6_710_886)).toBe("6.4 MB");
    expect(formatBytes(2_684_354_560)).toBe("2.5 GB");
  });

  it("says so when there is nothing to measure", () => {
    expect(formatBytes(null)).toBe("not measured");
    expect(formatBytes(undefined)).toBe("not measured");
  });
});

describe("readDistEntries and summarizeDist", () => {
  it("walks the bundle, sorts the biggest assets first and totals it", () => {
    const dir = mkdtempSync(join(tmpdir(), "frontend-metrics-"));
    mkdirSync(join(dir, "assets"));
    writeFileSync(join(dir, "index.html"), "x".repeat(100));
    writeFileSync(join(dir, "assets/app.js"), "x".repeat(300));
    writeFileSync(join(dir, "assets/monaco.js"), "x".repeat(200));

    const entries = readDistEntries(dir);

    expect(entries.map((entry) => entry.name)).toEqual([
      "assets/app.js",
      "assets/monaco.js",
      "index.html",
    ]);
    expect(summarizeDist(entries)).toEqual({
      totalBytes: 600,
      fileCount: 3,
      largest: [
        { name: "assets/app.js", bytes: 300 },
        { name: "assets/monaco.js", bytes: 200 },
        { name: "index.html", bytes: 100 },
      ],
    });
  });

  it("reports an empty bundle rather than throwing", () => {
    expect(summarizeDist(readDistEntries(join(tmpdir(), "nope-none")))).toEqual({
      totalBytes: 0,
      fileCount: 0,
      largest: [],
    });
  });
});

describe("assetLabel", () => {
  it("drops the content hash so two builds read the same", () => {
    expect(assetLabel("assets/index-ctQPmSIV.js")).toBe("assets/index.js");
    expect(assetLabel("assets/index-Ba1cD2eF.css")).toBe("assets/index.css");
  });

  it("leaves a name that has no hash alone", () => {
    expect(assetLabel("index.html")).toBe("index.html");
  });
});

describe("summarizeCoverage", () => {
  it("reads the totals the shared-layer include produced", () => {
    const summary = {
      total: {
        lines: { pct: 99.16 },
        branches: { pct: 95.6 },
        functions: { pct: 100 },
      },
    };

    expect(summarizeCoverage(summary)).toEqual({
      lines: 99.16,
      branches: 95.6,
      functions: 100,
    });
  });

  it("has nothing to report without a summary", () => {
    expect(summarizeCoverage(null)).toBeNull();
    expect(summarizeCoverage({})).toBeNull();
  });
});

describe("summarizeResults", () => {
  it("counts files, cases and failures", () => {
    expect(
      summarizeResults({
        numTotalTests: 245,
        numPassedTests: 244,
        numFailedTests: 1,
        testResults: [{}, {}],
      })
    ).toEqual({ files: 2, total: 245, passed: 244, failed: 1 });
  });

  it("has nothing to report without a report", () => {
    expect(summarizeResults(null)).toBeNull();
    expect(summarizeResults({})).toBeNull();
  });
});

describe("formatMetrics", () => {
  const measured = {
    results: { files: 39, total: 245, passed: 245, failed: 0 },
    coverage: { lines: 99.16, branches: 95.6, functions: 100 },
    typeCheck: { memoryBytes: 891064 * 1024, files: 1804, buildTimeSeconds: 12.7 },
    dist: {
      totalBytes: 6_710_886,
      fileCount: 42,
      largest: [{ name: "assets/monaco-abcdef12.js", bytes: 2_940_000 }],
    },
  };

  it("renders every measurement as a row", () => {
    const markdown = formatMetrics(measured);

    expect(markdown).toContain("| Vitest | 39 files · 245 tests · 245 passed |");
    expect(markdown).toContain(
      "| Shared-layer coverage | 99.16% lines · 95.6% branches · 100% functions |"
    );
    expect(markdown).toContain(
      "| Type check | 870.2 MB peak heap · 1804 files · 12.7s |"
    );
    expect(markdown).toContain("| dist | 6.4 MB · 42 files |");
    expect(markdown).toContain("| Largest assets | assets/monaco.js 2.8 MB |");
    expect(markdown).not.toContain("not measured");
  });

  it("says what was not measured instead of failing", () => {
    const markdown = formatMetrics({
      results: null,
      coverage: null,
      typeCheck: null,
      dist: { totalBytes: 0, fileCount: 0, largest: [] },
    });

    expect(markdown.match(/not measured/g)).toHaveLength(4);
  });

  it("reports failures when a suite went red", () => {
    const markdown = formatMetrics({
      ...measured,
      results: { files: 39, total: 245, passed: 240, failed: 5 },
    });

    expect(markdown).toContain("| Vitest | 39 files · 245 tests · 240 passed · 5 failed |");
  });
});

describe("parseArgs", () => {
  const defaults = { dist: "/default/dist", summary: "" };

  it("keeps the defaults and takes overrides", () => {
    expect(parseArgs([], defaults)).toEqual(defaults);
    expect(parseArgs(["--dist", "/tmp/dist"], defaults)).toEqual({
      dist: "/tmp/dist",
      summary: "",
    });
  });

  it("rejects an option it does not know and a flag without a value", () => {
    expect(() => parseArgs(["--nope", "x"], defaults)).toThrow(
      "unexpected argument"
    );
    expect(() => parseArgs(["--dist"], defaults)).toThrow(
      "unexpected argument"
    );
  });
});
