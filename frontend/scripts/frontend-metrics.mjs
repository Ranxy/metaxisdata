// frontend/scripts/frontend-metrics.mjs
//
// Collects the numbers this project wants to watch but not enforce: the
// type-check's time (TypeScript 6 dropped the aggregate heap and file counts),
// the production bundle, the test count and the shared layer's coverage. CI
// appends the result to the job summary, so a drift shows up as a diff next to
// the run instead of landing silently.
//
//   node frontend/scripts/frontend-metrics.mjs
//
// Every input is a file another step already produced, and a missing one is
// rendered as "not measured" rather than failing the run: this is an
// observation, not a budget. The pure helpers are exported so Vitest can
// exercise them.

import {
  appendFileSync,
  existsSync,
  readdirSync,
  readFileSync,
  statSync,
} from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, "..");

const DEFAULTS = {
  results: resolve(ROOT, "coverage/vitest-results.json"),
  coverage: resolve(ROOT, "coverage/coverage-summary.json"),
  typecheck: resolve(ROOT, "coverage/typecheck.txt"),
  dist: resolve(ROOT, "dist"),
  summary: process.env.GITHUB_STEP_SUMMARY ?? "",
};

/**
 * The tail of `vue-tsc --build --force --extendedDiagnostics`. A TypeScript 6
 * run prints only the timings — 5.9's `Aggregate Memory used` and `Aggregate
 * Files` lines are gone — so the two stay optional and the renderer reports
 * just the build time.
 */
export function parseTypeCheckDiagnostics(text) {
  const memory = /Aggregate Memory used:\s+(\d+)K/.exec(text);
  const files = /Aggregate Files:\s+(\d+)/.exec(text);
  const buildTime = /^Build time:\s+([\d.]+)s$/m.exec(text);
  if (!memory && !files && !buildTime) {
    return null;
  }
  return {
    memoryBytes: memory ? Number(memory[1]) * 1024 : null,
    files: files ? Number(files[1]) : null,
    buildTimeSeconds: buildTime ? Number(buildTime[1]) : null,
  };
}

export function formatBytes(bytes) {
  if (typeof bytes !== "number") {
    return "not measured";
  }
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded = unit === 0 ? Math.round(value) : value.toFixed(1);
  return `${rounded} ${units[unit]}`;
}

/** Every file below `dir`, as `{ name, bytes }` relative paths. */
export function readDistEntries(dir) {
  const entries = [];
  function walk(current, prefix) {
    const children = readdirSync(current, { withFileTypes: true }).sort((a, b) =>
      a.name.localeCompare(b.name)
    );
    for (const child of children) {
      const full = join(current, child.name);
      const name = prefix ? `${prefix}/${child.name}` : child.name;
      if (child.isDirectory()) {
        walk(full, name);
        continue;
      }
      entries.push({ name, bytes: statSync(full).size });
    }
  }
  if (existsSync(dir)) {
    walk(dir, "");
  }
  return entries;
}

export function summarizeDist(entries) {
  const totalBytes = entries.reduce((sum, entry) => sum + entry.bytes, 0);
  const largest = [...entries]
    .sort((a, b) => b.bytes - a.bytes || a.name.localeCompare(b.name))
    .slice(0, 3);
  return { totalBytes, fileCount: entries.length, largest };
}

/**
 * An asset name without Vite's content hash, so the same chunk reads the same
 * in every run's summary and two builds can be compared by eye.
 */
export function assetLabel(name) {
  return name.replace(/-[A-Za-z0-9_-]{8}\.(js|css)$/, ".$1");
}

/** The shared layer's own totals; `coverage.include` scopes them. */
export function summarizeCoverage(summary) {
  const total = summary?.total;
  if (!total?.lines) {
    return null;
  }
  return {
    lines: total.lines.pct ?? null,
    branches: total.branches?.pct ?? null,
    functions: total.functions?.pct ?? null,
  };
}

export function summarizeResults(report) {
  if (typeof report?.numTotalTests !== "number") {
    return null;
  }
  return {
    files: Array.isArray(report.testResults)
      ? report.testResults.length
      : null,
    total: report.numTotalTests,
    passed: report.numPassedTests ?? null,
    failed: report.numFailedTests ?? null,
  };
}

export function formatMetrics({ results, coverage, typeCheck, dist }) {
  const rows = [];

  if (results) {
    const files = results.files === null ? "" : `${results.files} files · `;
    const failed = results.failed ? ` · ${results.failed} failed` : "";
    rows.push([
      "Vitest",
      `${files}${results.total} tests · ${results.passed} passed${failed}`,
    ]);
  } else {
    rows.push(["Vitest", "not measured"]);
  }

  if (coverage) {
    rows.push([
      "Shared-layer coverage",
      `${coverage.lines}% lines · ${coverage.branches}% branches · ${coverage.functions}% functions`,
    ]);
  } else {
    rows.push(["Shared-layer coverage", "not measured"]);
  }

  if (typeCheck) {
    const parts = [];
    if (typeCheck.memoryBytes !== null) {
      parts.push(`${formatBytes(typeCheck.memoryBytes)} peak heap`);
    }
    if (typeCheck.files !== null) {
      parts.push(`${typeCheck.files} files`);
    }
    if (typeCheck.buildTimeSeconds !== null) {
      parts.push(`${typeCheck.buildTimeSeconds}s`);
    }
    rows.push(["Type check", parts.join(" · ") || "not measured"]);
  } else {
    rows.push(["Type check", "not measured"]);
  }

  if (dist.fileCount > 0) {
    rows.push([
      "dist",
      `${formatBytes(dist.totalBytes)} · ${dist.fileCount} files`,
    ]);
    rows.push([
      "Largest assets",
      dist.largest
        .map((entry) => `${assetLabel(entry.name)} ${formatBytes(entry.bytes)}`)
        .join(" · "),
    ]);
  } else {
    rows.push(["dist", "not measured"]);
  }

  return [
    "### Frontend metrics",
    "",
    "| Metric | Value |",
    "| --- | --- |",
    ...rows.map(([metric, value]) => `| ${metric} | ${value} |`),
    "",
    "<sub>Observations, not budgets — reported by `frontend/scripts/frontend-metrics.mjs`.</sub>",
  ].join("\n");
}

export function parseArgs(argv, defaults = DEFAULTS) {
  const args = { ...defaults };
  for (let index = 0; index < argv.length; index += 2) {
    const flag = argv[index];
    const value = argv[index + 1];
    const name = typeof flag === "string" ? flag.replace(/^--/, "") : "";
    if (!flag?.startsWith("--") || value === undefined || !(name in args)) {
      throw new Error(`unexpected argument: ${flag ?? ""} ${value ?? ""}`);
    }
    args[name] = value;
  }
  return args;
}

function readJson(path) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch {
    return null;
  }
}

function readText(path) {
  try {
    return readFileSync(path, "utf8");
  } catch {
    return null;
  }
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const markdown = formatMetrics({
    results: summarizeResults(readJson(args.results)),
    coverage: summarizeCoverage(readJson(args.coverage)),
    typeCheck: parseTypeCheckDiagnostics(readText(args.typecheck) ?? ""),
    dist: summarizeDist(readDistEntries(args.dist)),
  });

  console.log(markdown);
  if (args.summary) {
    appendFileSync(args.summary, `${markdown}\n`);
  }
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  main();
}
