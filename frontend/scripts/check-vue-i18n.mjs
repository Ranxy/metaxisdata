// frontend/scripts/check-vue-i18n.mjs
//
// Enforces a strict mapping between source code and the canonical locale
// files (src/locales/en-US.json, src/locales/zh-CN.json):
//   1. Missing keys       — t("key") in code but key not in the locale files
//   2. Unused keys        — key in the locale files but not referenced in code
//   3. Consistency        — every locale file must have the exact same key set
//   4. Placeholder syntax — vue-i18n interpolates {name}; a double-brace
//                           {{name}} fails to compile and renders literally
//   5. Placeholder parity — a key's {name} set must match across locales
//
// This is the Vue/vue-i18n counterpart of the react-i18next checker. The two
// frameworks differ where it matters here: vue-i18n uses single braces (not
// {{name}}) and pipe-separated plural forms (not _one/_other key suffixes), so
// there is no base-key normalization and the brace check is inverted.
//
// Indirect references the checker CAN trace statically:
//   - titleKey/descriptionKey/messageKey/labelKey/keypath string literals in
//     data objects, props and <i18n-t keypath="…">
//   - t(cond ? "a" : "b") ternary literals
//   - handleError(err, "key") and showSuccess("key") / showError / showWarning
//     / showInfo key literals in src/composables/useErrorHandler.ts
// Anything else (template-literal or variable keys) must be listed in
// DYNAMIC_PREFIXES below with a pointer to the caller.
//
// Usage: node frontend/scripts/check-vue-i18n.mjs

import { readFileSync, readdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, "..");
const SOURCE_DIR = resolve(ROOT, "src");
const LOCALES_DIR = resolve(ROOT, "src/locales");
const LOCALES = ["en-US", "zh-CN"];

// Keys whose usage the checker cannot trace statically — exempt from the
// unused-key check. Matched with String.startsWith, so an entry ending in "."
// is a prefix family (matches every key under it) and an entry without a
// trailing "." matches itself (and anything that starts with it).
export const DYNAMIC_PREFIXES = [];

// t("key") / t('key') / $t("key") / te("key") / tm("key").
const CALL_RE = /(?<![A-Za-z0-9_$])(?:\$t|te|tm|t)\(\s*["']([^"']+)["']/g;
// t(cond ? "a" : "b") — conditional literal pairs.
const TERNARY_RE = /(?<![A-Za-z0-9_$])t\([^)]*\?\s*["']([^"']+)["']\s*:\s*["']([^"']+)["']/g;
// titleKey/descriptionKey/messageKey/labelKey/keypath literals in data objects
// and props, translated later via t(variable).
const KEY_PROP_RE =
  /\b(titleKey|descriptionKey|messageKey|labelKey|keypath)\s*[:=]\s*["']([^"']+)["']/g;
// handleError(err, "key") in composables/useErrorHandler.ts — the second
// argument is an i18n key used when the error carries no message.
const HANDLE_ERROR_RE = /\bhandleError\(\s*[^,()\n]+,\s*["']([^"']+)["']/g;
// showSuccess("key") and friends in composables/useErrorHandler.ts.
const TOAST_RE = /\bshow(?:Success|Error|Warning|Info)\(\s*["']([^"']+)["']/g;
// v-t="'key'" directive.
const V_T_RE = /\bv-t="'([^"']+)'"/g;

const SOURCE_EXTENSIONS = [".vue", ".ts", ".tsx"];
const SKIP_DIRS = ["locales", "proto-es"];

export function flatten(obj, prefix = "") {
  const result = {};
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (typeof v === "object" && v !== null && !Array.isArray(v)) {
      Object.assign(result, flatten(v, key));
    } else {
      result[key] = v;
    }
  }
  return result;
}

function findSourceFiles(dir) {
  const results = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = resolve(dir, entry.name);
    if (entry.isDirectory()) {
      if (!SKIP_DIRS.includes(entry.name)) {
        results.push(...findSourceFiles(full));
      }
    } else if (entry.isFile() && SOURCE_EXTENSIONS.some((ext) => entry.name.endsWith(ext))) {
      results.push(full);
    }
  }
  return results;
}

// Keys referenced by a single source string (one .vue/.ts file).
export function collectKeysFromSource(src) {
  const keys = new Set();
  for (const re of [CALL_RE, KEY_PROP_RE, HANDLE_ERROR_RE, TOAST_RE, V_T_RE]) {
    re.lastIndex = 0;
    let m;
    while ((m = re.exec(src))) keys.add(m[m.length - 1]);
  }
  TERNARY_RE.lastIndex = 0;
  let m;
  while ((m = TERNARY_RE.exec(src))) {
    keys.add(m[1]);
    keys.add(m[2]);
  }
  return keys;
}

export function collectSourceKeys(sourceDir) {
  const keys = new Set();
  for (const file of findSourceFiles(sourceDir)) {
    for (const key of collectKeysFromSource(readFileSync(file, "utf-8"))) {
      keys.add(key);
    }
  }
  return keys;
}

export function loadLocaleKeys(localeFile) {
  const data = JSON.parse(readFileSync(localeFile, "utf-8"));
  return new Set(Object.keys(flatten(data)));
}

function isDynamic(key) {
  return DYNAMIC_PREFIXES.some((prefix) => key.startsWith(prefix));
}

// vue-i18n interpolates {name}; {{name}} triggers "Not allowed nest
// placeholder" and is emitted literally.
const DOUBLE_BRACE_RE = /\{\{/g;
// A single-brace {name} that is not part of a {{name}} pair.
const PLACEHOLDER_RE = /(?<!\{)\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}(?!\})/g;

function findDoubleBraces(obj, path = "") {
  const issues = [];
  if (obj && typeof obj === "object" && !Array.isArray(obj)) {
    for (const [k, v] of Object.entries(obj)) {
      issues.push(...findDoubleBraces(v, path ? `${path}.${k}` : k));
    }
  } else if (typeof obj === "string") {
    DOUBLE_BRACE_RE.lastIndex = 0;
    if (DOUBLE_BRACE_RE.test(obj)) issues.push({ key: path, value: obj });
  }
  return issues;
}

export function extractPlaceholders(value) {
  const names = new Set();
  if (typeof value !== "string") return names;
  PLACEHOLDER_RE.lastIndex = 0;
  let m;
  while ((m = PLACEHOLDER_RE.exec(value))) names.add(m[1]);
  return names;
}

// Runs every check and returns the report plus the number of errors. Tests can
// point this at fixture directories.
export function runChecks({
  sourceDir = SOURCE_DIR,
  localesDir = LOCALES_DIR,
  locales = LOCALES,
} = {}) {
  const report = [];
  const fail = (line) => report.push(line);
  let errorCount = 0;
  const error = (line) => {
    errorCount++;
    fail(`  - ${line}`);
  };

  const sourceKeys = collectSourceKeys(sourceDir);
  const localeData = new Map();
  const localeKeys = new Map();
  for (const locale of locales) {
    const data = JSON.parse(readFileSync(resolve(localesDir, `${locale}.json`), "utf-8"));
    localeData.set(locale, data);
    localeKeys.set(locale, new Set(Object.keys(flatten(data))));
  }
  const referenceLocale = locales[0];
  const referenceKeys = localeKeys.get(referenceLocale);

  // Check 1: Missing keys (in code but not in locale).
  const missing = [];
  for (const key of sourceKeys) {
    if (referenceKeys.has(key) || isDynamic(key)) continue;
    missing.push(key);
  }
  if (missing.length > 0) {
    fail(
      `Missing keys (${missing.length}) — used in code but not in ${referenceLocale}.json:\n`
    );
    for (const key of missing.sort()) error(key);
    fail("");
  }

  // Check 2: Unused keys (in locale but not in code).
  const unused = [];
  for (const key of referenceKeys) {
    if (sourceKeys.has(key) || isDynamic(key)) continue;
    unused.push(key);
  }
  if (unused.length > 0) {
    fail(`Unused keys (${unused.length}) — in locale files but not in code:\n`);
    for (const key of unused.sort()) error(key);
    fail(
      "\nRemove from frontend/src/locales/ or add to DYNAMIC_PREFIXES if referenced indirectly (helper return, template literal).\n"
    );
  }

  // Check 3: Cross-locale consistency (every locale must match the first one).
  for (const locale of locales) {
    if (locale === referenceLocale) continue;
    const keys = localeKeys.get(locale);
    const missingInLocale = [...referenceKeys].filter((k) => !keys.has(k));
    const extraInLocale = [...keys].filter((k) => !referenceKeys.has(k));

    if (missingInLocale.length > 0) {
      fail(
        `${locale}: missing ${missingInLocale.length} key(s) (present in ${referenceLocale}):\n`
      );
      for (const key of missingInLocale.sort()) error(key);
      fail("");
    }
    if (extraInLocale.length > 0) {
      fail(`${locale}: extra ${extraInLocale.length} key(s) (not in ${referenceLocale}):\n`);
      for (const key of extraInLocale.sort()) error(key);
      fail("");
    }
  }

  // Check 4: Double-brace {{name}} placeholders (vue-i18n needs {name}).
  for (const locale of locales) {
    const issues = findDoubleBraces(localeData.get(locale));
    if (issues.length > 0) {
      fail(
        `${locale}: ${issues.length} string(s) with double {{name}} placeholders — vue-i18n interpolates {name}:\n`
      );
      for (const { key, value } of issues) error(`${key} → ${JSON.stringify(value)}`);
      fail("");
    }
  }

  // Check 5: Placeholder parity across locales for the same key.
  const referenceFlat = flatten(localeData.get(referenceLocale));
  for (const locale of locales) {
    if (locale === referenceLocale) continue;
    const flat = flatten(localeData.get(locale));
    const mismatches = [];
    for (const [key, enValue] of Object.entries(referenceFlat)) {
      if (!(key in flat)) continue;
      const expected = extractPlaceholders(enValue);
      const actual = extractPlaceholders(flat[key]);
      const missingNames = [...expected].filter((name) => !actual.has(name));
      const extraNames = [...actual].filter((name) => !expected.has(name));
      if (missingNames.length > 0 || extraNames.length > 0) {
        mismatches.push(
          `${key} — ${referenceLocale}: {${[...expected].join(", ")}}, ${locale}: {${[...actual].join(", ")}}`
        );
      }
    }
    if (mismatches.length > 0) {
      fail(
        `${locale}: ${mismatches.length} key(s) whose {name} placeholders differ from ${referenceLocale}:\n`
      );
      for (const line of mismatches) error(line);
      fail("");
    }
  }

  return { errorCount, report };
}

function main() {
  const { errorCount, report } = runChecks();
  if (errorCount > 0) {
    for (const line of report) console.error(line);
    process.exit(1);
  }
  console.log(
    "Vue i18n: all checks passed (missing keys, unused keys, cross-locale consistency, placeholder syntax, placeholder parity)."
  );
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main();
}
