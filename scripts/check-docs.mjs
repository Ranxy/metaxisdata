// scripts/check-docs.mjs
//
// Documentation gate. Run: node scripts/check-docs.mjs
//
// The repository keeps two documentation trees, and both drifted badly before this gate existed:
//
//   docs/          operator and integrator documentation
//   .agents/docs/  maintenance references an agent reads before changing a subsystem
//
// The rules this enforces, in the order a reviewer would care about them:
//
//   1. Links        every relative Markdown link resolves.
//   2. Index        every reference is listed in its tree's index, and no index row is dead.
//   3. Links-only   no `file.go:123` anchors: line numbers rot, so a reference names a symbol.
//   4. No history   no work-stream/phase/PR/commit/status/tombstone content: that history belongs
//                   in the pull request, not in a committed reference to shipped behavior.
//   5. Budget       a reference stays readable: size and line caps per tree.
//   6. Language     .agents/docs and the non-translated docs/ pages are English; only the two
//                   onboarding pages are bilingual, and a translated pair must stay in parity.
//   7. Generated    never hand-edit generated or embedded Markdown (proto docs, the MCP prompt,
//                   the CLI skill) — check the assets an embed needs still exist.
//
// Exit status is 0 when clean, 1 with a grouped report otherwise.

import { execFileSync } from "node:child_process";
import { readFileSync, existsSync } from "node:fs";
import { dirname, extname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

// Generated documentation: buf output, never edited and never gated.
const GENERATED_PREFIXES = ["proto/gen/"];
// Markdown that is a runtime asset, not a document. It is embedded in the binary.
const EMBEDDED_ASSETS = ["backend/mcp/guide.md", "cli/skill/SKILL.md"];

// Exempt from the content rules, but not from links or index coverage: the pinned-dependency
// defect ledger. Its length tracks upstream defects rather than our prose, and ~29 code
// comments cite its sections, so it is frozen pending a pass of its own.
const FROZEN = new Set([".agents/docs/omni-upstream-defects.md"]);

// Trees and their index. Operator docs are allowed to be longer than references.
const TREES = {
  "docs/": { index: "docs/README.md", maxBytes: 100 * 1024, maxLines: 720 },
  ".agents/docs/": { index: ".agents/docs/README.md", maxBytes: 100 * 1024, maxLines: 460 },
};

const violations = [];
function fail(rule, file, line, message) {
  violations.push({ rule, file, line, message });
}

function trackedMarkdown() {
  const out = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "*.md"], { cwd: ROOT, encoding: "utf8" });
  return out
    .split("\n")
    .filter(Boolean)
    .filter((f) => !GENERATED_PREFIXES.some((p) => f.startsWith(p)))
    // git keeps a staged deletion in the index, so a file listed here may be gone from the tree.
    .filter((f) => existsSync(join(ROOT, f)));
}

const files = trackedMarkdown();
const text = new Map(files.map((f) => [f, readFileSync(join(ROOT, f), "utf8")]));
const linesOf = (f) => text.get(f).split("\n");

// ---------------------------------------------------------------------------
// 1. Links
// ---------------------------------------------------------------------------
const LINKS = /\]\(([^)\s]+)\)/g;
const linkedFrom = new Map();
for (const file of files) {
  const dir = dirname(file);
  for (const m of text.get(file).matchAll(LINKS)) {
    const raw = m[1];
    if (/^(https?:|mailto:|#)/.test(raw)) continue;
    const target = decodeURIComponent(raw.split("#")[0]);
    if (!target) continue;
    const resolved = relative(ROOT, resolve(ROOT, dir, target)).split("\\").join("/");
    if (!existsSync(join(ROOT, resolved))) {
      fail("link", file, lineOf(file, m.index), `link target does not exist: ${raw}`);
      continue;
    }
    if (!linkedFrom.has(resolved)) linkedFrom.set(resolved, new Set());
    linkedFrom.get(resolved).add(file);
  }
}

function lineOf(file, index) {
  return text.get(file).slice(0, index).split("\n").length;
}

// ---------------------------------------------------------------------------
// 2. Index coverage
// ---------------------------------------------------------------------------
for (const [tree, { index }] of Object.entries(TREES)) {
  const indexed = new Set();
  for (const m of text.get(index).matchAll(LINKS)) {
    const raw = m[1];
    if (/^(https?:|mailto:|#)/.test(raw)) continue;
    const target = decodeURIComponent(raw.split("#")[0]);
    if (!target || extname(target) !== ".md") continue;
    indexed.add(relative(ROOT, resolve(ROOT, dirname(index), target)).split("\\").join("/"));
  }
  for (const file of files) {
    if (!file.startsWith(tree) || file === index) continue;
    if (file.endsWith("_zh.md") && indexed.has(file)) continue;
    if (!indexed.has(file)) {
      fail("index", file, 0, `not listed in ${index}`);
    }
  }
  for (const target of indexed) {
    if (!existsSync(join(ROOT, target))) {
      fail("index", index, 0, `index names a missing file: ${target}`);
    }
  }
}

// ---------------------------------------------------------------------------
// 3. No line-number anchors
// ---------------------------------------------------------------------------
const LINE_ANCHOR =
  /\b[\w@./-]+\.(?:go|vue|ts|tsx|js|mjs|sql|proto|json|ya?ml):\d+(?:[-,]\d+)?/;
for (const file of files) {
  if (FROZEN.has(file)) continue;
  linesOf(file).forEach((line, i) => {
    const m = line.match(LINE_ANCHOR);
    if (m) {
      fail("line-anchor", file, i + 1, `line number instead of a symbol: ${m[0]}`);
    }
  });
}

// ---------------------------------------------------------------------------
// 4. No process history
// ---------------------------------------------------------------------------
// The glossary's generated term table carries the Chinese rendering of each term on purpose;
// only the prose around it is a document. Strip it before the content rules.
const GENERATED_BLOCK = /<!-- BEGIN GENERATED GLOSSARY -->[\s\S]*?<!-- END GENERATED GLOSSARY -->/g;
const authored = (file) => text.get(file).replace(GENERATED_BLOCK, "");

const HISTORY_PATTERNS = [
  [/^\s*>?\s*Status:\s*\*\*(implemented|done|complete|draft)/i, "status header"],
  [/已删除|已作废|已失去意义|实施分期|阶段计划|本分支/, "process note (ZH)"],
  [/\bPhase\s+P?[0-9]\b/, "phase/plan step"],
  [/\bPR\s*#\d+/, "pull-request reference"],
  [/\bcommit\s+[0-9a-f]{7,40}\b/i, "commit hash"],
  [/~~[^~]+~~/, "struck-through landed item"],
];
for (const file of files) {
  if (FROZEN.has(file)) continue;
  authored(file).split("\n").forEach((line, i) => {
    for (const [re, what] of HISTORY_PATTERNS) {
      if (re.test(line)) {
        fail("history", file, i + 1, `${what}: ${line.trim().slice(0, 80)}`);
      }
    }
  });
}

// ---------------------------------------------------------------------------
// 5. Size budget
// ---------------------------------------------------------------------------
for (const [tree, { maxBytes, maxLines }] of Object.entries(TREES)) {
  for (const file of files) {
    if (!file.startsWith(tree) || FROZEN.has(file)) continue;
    const body = text.get(file);
    const bytes = Buffer.byteLength(body, "utf8");
    const lineCount = body.split("\n").length;
    if (bytes > maxBytes) {
      fail("budget", file, 0, `${Math.round(bytes / 1024)} KB exceeds the ${Math.round(maxBytes / 1024)} KB cap`);
    }
    if (lineCount > maxLines) {
      fail("budget", file, 0, `${lineCount} lines exceeds the ${maxLines}-line cap`);
    }
  }
}

// ---------------------------------------------------------------------------
// 6. Language
// ---------------------------------------------------------------------------
const cjk = (s) => (s.match(/[\u4e00-\u9fff]/g) ?? []).length;
for (const file of files) {
  const translated = file.endsWith("_zh.md");
  const inAgentsDocs = file.startsWith(".agents/docs/");
  const isOperatorDoc = file.startsWith("docs/");
  if (!inAgentsDocs && !isOperatorDoc) continue;
  if (translated || FROZEN.has(file)) continue;
  const body = authored(file);
  const count = cjk(body);
  const words = body.split(/\s+/).filter(Boolean).length;
  if (count > Math.max(20, words * 0.05)) {
    fail("language", file, 0, `${count} CJK characters in a document that must be English`);
  }
}

// ---------------------------------------------------------------------------
// 7. Bilingual parity, and embedded assets still present
// ---------------------------------------------------------------------------
for (const file of files) {
  if (!file.endsWith("_zh.md")) continue;
  const base = file.slice(0, -"_zh.md".length) + ".md";
  if (!existsSync(join(ROOT, base))) {
    fail("bilingual", file, 0, `translation has no ${base}`);
    continue;
  }
  const envs = (s) => new Set(s.match(/METAXISDATA_[A-Z_]+/g) ?? []);
  const a = envs(text.get(base));
  const b = envs(text.get(file));
  const onlyBase = [...a].filter((v) => !b.has(v));
  const onlyZh = [...b].filter((v) => !a.has(v));
  if (onlyBase.length || onlyZh.length) {
    fail(
      "bilingual",
      file,
      0,
      `environment variables out of parity with ${base}: missing ${onlyBase.join(", ") || "-"}; extra ${onlyZh.join(", ") || "-"}`,
    );
  }
  const headings = (s) => (s.match(/^#{2,3} /gm) ?? []).length;
  const hb = headings(text.get(base));
  const hz = headings(text.get(file));
  if (hb && Math.abs(hb - hz) / hb > 0.25) {
    fail("bilingual", file, 0, `${hz} headings against ${hb} in ${base}`);
  }
}

for (const asset of EMBEDDED_ASSETS) {
  if (!existsSync(join(ROOT, asset))) {
    fail("generated", asset, 0, "embedded Markdown asset is missing; the binary will not build");
  }
}
const goFiles = execFileSync("git", ["ls-files", "*.go"], { cwd: ROOT, encoding: "utf8" })
  .split("\n")
  .filter((f) => f && existsSync(join(ROOT, f)));
for (const goFile of goFiles) {
  readFileSync(join(ROOT, goFile), "utf8")
    .split("\n")
    .forEach((line, i) => {
      const m = line.match(/^\s*\/\/go:embed\s+(?:(?:all:)?(\S+))/);
      if (!m) return;
      for (const token of m[1].split(/\s+/)) {
        if (!token.endsWith(".md")) continue;
        if (!existsSync(join(ROOT, dirname(goFile), token))) {
          fail("generated", goFile, i + 1, `//go:embed ${token} matches no file`);
        }
      }
    });
}

// ---------------------------------------------------------------------------
// Report
// ---------------------------------------------------------------------------
if (violations.length === 0) {
  console.log(`docs: ${files.length} files checked, no violations${FROZEN.size ? ` (${FROZEN.size} frozen file exempt from the content rules)` : ""}.`);
  process.exit(0);
}

const byRule = new Map();
for (const v of violations) {
  if (!byRule.has(v.rule)) byRule.set(v.rule, []);
  byRule.get(v.rule).push(v);
}
const ORDER = ["link", "index", "line-anchor", "history", "budget", "language", "bilingual", "generated"];
for (const rule of ORDER) {
  const group = byRule.get(rule);
  if (!group) continue;
  console.error(`\n${rule} (${group.length}):`);
  for (const v of group) {
    console.error(`  ${v.file}${v.line ? `:${v.line}` : ""}: ${v.message}`);
  }
}
console.error(`\ndocs: ${violations.length} violation(s) in ${files.length} files.`);
process.exit(1);
