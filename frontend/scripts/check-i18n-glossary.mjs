// frontend/scripts/check-i18n-glossary.mjs
//
// Keeps the project's professional terminology frozen across locales.
//
//   glossary.json                    source of truth: one canonical rendering
//                                    per term per locale, plus the variants that
//                                    must not appear where the term applies
//   .agents/docs/i18n-glossary.md    the human- and agent-facing table, generated
//                                    from the JSON between two markers
//
// Checks:
//   1. Integrity — the glossary itself is well formed: unique ids, a declared
//      category, every declared locale translated, an existing scope prefix,
//      compilable patterns.
//   2. Drift — no catalog string contains a term's forbidden variant. A pattern
//      is a literal substring unless written as /regex/flags; `scope` limits the
//      check to the i18n keys the term applies to (absent scope = every key) and
//      `when` to the keys whose value in a named locale matches.
//   3. Doc sync — the generated table matches the glossary.
//
// Usage:
//   node frontend/scripts/check-i18n-glossary.mjs               # check
//   node frontend/scripts/check-i18n-glossary.mjs --write-doc   # rewrite the table
//   node frontend/scripts/check-i18n-glossary.mjs --show job    # look a term up

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { flatten } from "./check-vue-i18n.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, "..");
const GLOSSARY_PATH = resolve(ROOT, "glossary.json");
const LOCALES_DIR = resolve(ROOT, "src/locales");
const DOC_PATH = resolve(ROOT, "../.agents/docs/i18n-glossary.md");

export const BEGIN_MARKER = "<!-- BEGIN GENERATED GLOSSARY -->";
export const END_MARKER = "<!-- END GENERATED GLOSSARY -->";

// A pattern written as /regex/flags is a regular expression; anything else is a
// literal substring. Returns null for a literal.
export function compilePattern(pattern) {
  const match = /^\/([\s\S]*)\/([a-z]*)$/.exec(pattern);
  if (!match) return null;
  try {
    return new RegExp(match[1], match[2]);
  } catch {
    return null;
  }
}

export function matchesPattern(text, pattern) {
  const regex = compilePattern(pattern);
  return regex ? regex.test(text) : text.includes(pattern);
}

function isRegexLiteral(pattern) {
  return compilePattern(pattern) !== null;
}

// Every locale's catalog as a flat { "section.key": value } map.
export function loadCatalogs(localesDir, locales) {
  const catalogs = {};
  for (const locale of locales) {
    const file = resolve(localesDir, `${locale}.json`);
    catalogs[locale] = flatten(JSON.parse(readFileSync(file, "utf-8")));
  }
  return catalogs;
}

function isNonEmptyString(value) {
  return typeof value === "string" && value.trim() !== "";
}

export function validateGlossary(glossary) {
  const errors = [];
  const locales = glossary?.locales;
  if (!Array.isArray(locales) || locales.length === 0 || !locales.every(isNonEmptyString)) {
    errors.push("locales must be a non-empty array of locale codes.");
    return errors;
  }
  if (new Set(locales).size !== locales.length) {
    errors.push("locales contains a duplicate.");
  }
  const categories = glossary.categories ?? {};
  if (!Array.isArray(glossary.terms) || glossary.terms.length === 0) {
    errors.push("terms must be a non-empty array.");
    return errors;
  }

  const seenIds = new Set();
  for (const [index, term] of glossary.terms.entries()) {
    const where = isNonEmptyString(term?.id) ? `"${term.id}"` : `terms[${index}]`;

    if (!isNonEmptyString(term?.id) || !/^[a-z0-9]+(-[a-z0-9]+)*$/.test(term.id)) {
      errors.push(`${where}: id must be lower-kebab-case.`);
    } else if (seenIds.has(term.id)) {
      errors.push(`${where}: duplicate id.`);
    } else {
      seenIds.add(term.id);
    }
    if (!isNonEmptyString(term?.category) || !(term.category in categories)) {
      errors.push(`${where}: category must be one of ${Object.keys(categories).join(", ")}.`);
    }
    if (!isNonEmptyString(term?.definition)) {
      errors.push(`${where}: definition must be a non-empty sentence.`);
    }

    const translations = term?.translations;
    if (translations === null || typeof translations !== "object") {
      errors.push(`${where}: translations must be an object keyed by locale.`);
    } else {
      for (const locale of Object.keys(translations)) {
        if (!locales.includes(locale)) errors.push(`${where}: translations has unknown locale "${locale}".`);
      }
      for (const locale of locales) {
        if (!isNonEmptyString(translations[locale])) {
          errors.push(`${where}: translations is missing a value for ${locale}.`);
        }
      }
    }

    if ("scope" in (term ?? {})) {
      const scope = term.scope;
      if (!Array.isArray(scope) || scope.length === 0 || !scope.every(isNonEmptyString)) {
        errors.push(`${where}: scope must be a non-empty array of key prefixes when present.`);
      }
    }

    if ("when" in (term ?? {})) {
      const when = term.when;
      if (when === null || typeof when !== "object" || Array.isArray(when) || Object.keys(when).length === 0) {
        errors.push(`${where}: when must be an object keyed by locale when present.`);
      } else {
        for (const [locale, pattern] of Object.entries(when)) {
          if (!locales.includes(locale)) {
            errors.push(`${where}: when has unknown locale "${locale}".`);
          } else if (!isNonEmptyString(pattern)) {
            errors.push(`${where}: when[${locale}] must be one pattern.`);
          } else if (pattern.startsWith("/") && !isRegexLiteral(pattern)) {
            errors.push(`${where}: when[${locale}] pattern ${JSON.stringify(pattern)} is not a valid regex.`);
          }
        }
      }
    }

    if ("avoid" in (term ?? {})) {
      const avoid = term.avoid;
      if (avoid === null || typeof avoid !== "object" || Array.isArray(avoid)) {
        errors.push(`${where}: avoid must be an object keyed by locale.`);
      } else {
        for (const [locale, patterns] of Object.entries(avoid)) {
          if (!locales.includes(locale)) {
            errors.push(`${where}: avoid has unknown locale "${locale}".`);
            continue;
          }
          if (!Array.isArray(patterns) || patterns.length === 0 || !patterns.every(isNonEmptyString)) {
            errors.push(`${where}: avoid[${locale}] must be a non-empty array of patterns.`);
            continue;
          }
          for (const pattern of patterns) {
            if (pattern.startsWith("/") && !isRegexLiteral(pattern)) {
              errors.push(`${where}: avoid[${locale}] pattern ${JSON.stringify(pattern)} is not a valid regex.`);
            }
          }
        }
      }
    }
  }
  return errors;
}

// A scope prefix that matches nothing is a typo or a term whose keys are gone.
export function checkScopePrefixes(glossary, catalogs, referenceLocale) {
  const errors = [];
  const keys = Object.keys(catalogs[referenceLocale] ?? {});
  for (const term of glossary.terms ?? []) {
    for (const prefix of term.scope ?? []) {
      if (!keys.some((key) => key === prefix || key.startsWith(prefix))) {
        errors.push(`"${term.id}": scope ${JSON.stringify(prefix)} matches no key in ${referenceLocale}.json.`);
      }
    }
  }
  return errors;
}

// `when` narrows a rule to the keys whose value in some locale matches a
// pattern — usually "only where the English source actually says the term".
export function whenMatches(when, catalogs, key) {
  for (const [locale, pattern] of Object.entries(when ?? {})) {
    const value = catalogs[locale]?.[key];
    const text = Array.isArray(value) ? value.join(" | ") : value;
    if (typeof text !== "string" || !matchesPattern(text, pattern)) return false;
  }
  return true;
}

export function checkAvoidRules(glossary, catalogs) {
  const errors = [];
  for (const term of glossary.terms ?? []) {
    for (const [locale, patterns] of Object.entries(term.avoid ?? {})) {
      const entries = Object.entries(catalogs[locale] ?? {});
      const scoped = term.scope
        ? entries.filter(([key]) => term.scope.some((prefix) => key === prefix || key.startsWith(prefix)))
        : entries;
      for (const [key, value] of scoped) {
        if (!whenMatches(term.when, catalogs, key)) continue;
        const text = Array.isArray(value) ? value.join(" | ") : value;
        if (typeof text !== "string") continue;
        for (const pattern of patterns) {
          if (!matchesPattern(text, pattern)) continue;
          errors.push(
            `${locale} ${key}: contains ${JSON.stringify(pattern)}, but glossary "${term.id}" fixes ` +
              `${JSON.stringify(term.translations[locale])} — ${JSON.stringify(text)}`
          );
        }
      }
    }
  }
  return errors;
}

function escapeCell(value) {
  return String(value).replace(/\|/g, "\\|").replace(/\s*\n\s*/g, " ");
}

// The markdown table is generated from the JSON in file order, so reordering a
// term only ever changes the JSON.
export function renderTable(glossary) {
  const locales = glossary.locales ?? [];
  const header = [...locales, "Category", "Definition", "Scope", "Do not use"];
  const rows = (glossary.terms ?? []).map((term) => {
    const when = Object.entries(term.when ?? {})
      .map(([locale, pattern]) => `${locale} ~ \`${pattern}\``)
      .join(", ");
    const avoid = Object.entries(term.avoid ?? {})
      .map(([locale, patterns]) => `${locale}: ${patterns.map((p) => `\`${p}\``).join(", ")}`)
      .join("; ");
    return [
      ...locales.map((locale) => term.translations?.[locale] ?? ""),
      term.category ?? "",
      term.definition ?? "",
      (term.scope ?? []).join(", ") || "all keys",
      [when ? `when ${when}` : "", avoid].filter(Boolean).join("; ") || "—",
    ];
  });
  const lines = [header, header.map(() => "---"), ...rows];
  return lines.map((cells) => `| ${cells.map(escapeCell).join(" | ")} |`).join("\n");
}

export function renderBlock(glossary) {
  return `\n${renderTable(glossary)}\n`;
}

// Exactly one occurrence of each marker, in order. A marker quoted elsewhere in
// the prose would otherwise move the generated block into the middle of a
// sentence.
export function markerRange(doc) {
  const locate = (marker) => {
    const first = doc.indexOf(marker);
    if (first === -1) return -1;
    if (doc.indexOf(marker, first + marker.length) !== -1) {
      throw new Error(`the document mentions ${marker} more than once.`);
    }
    return first;
  };
  const start = locate(BEGIN_MARKER);
  const end = locate(END_MARKER);
  if (start === -1 || end === -1 || end < start) {
    throw new Error(`the document is missing ${BEGIN_MARKER} and ${END_MARKER}.`);
  }
  return { start, end };
}

export function generatedBlock(doc) {
  const { start, end } = markerRange(doc);
  return doc.slice(start + BEGIN_MARKER.length, end);
}

export function spliceDoc(doc, block) {
  const { start, end } = markerRange(doc);
  return doc.slice(0, start + BEGIN_MARKER.length) + block + doc.slice(end);
}

// Runs every check and returns the report plus the number of errors. Tests can
// point this at fixture directories.
export function runChecks({
  glossaryPath = GLOSSARY_PATH,
  docPath = DOC_PATH,
  localesDir = LOCALES_DIR,
} = {}) {
  const report = [];
  const fail = (line) => report.push(line);
  let errorCount = 0;
  const error = (line) => {
    errorCount++;
    fail(`  - ${line}`);
  };

  let glossary;
  try {
    glossary = JSON.parse(readFileSync(glossaryPath, "utf-8"));
  } catch (error_) {
    return { errorCount: 1, report: [`  - glossary unreadable: ${error_.message}`] };
  }

  const integrity = validateGlossary(glossary);
  if (integrity.length > 0) {
    fail(`Glossary integrity (${integrity.length}):\n`);
    integrity.forEach(error);
    fail("");
  }

  const locales = Array.isArray(glossary.locales) ? glossary.locales : [];
  let catalogs = null;
  try {
    catalogs = loadCatalogs(localesDir, locales);
  } catch (error_) {
    error(`catalogs unreadable: ${error_.message}`);
  }

  if (catalogs && locales.length > 0) {
    const scopeErrors = checkScopePrefixes(glossary, catalogs, locales[0]);
    if (scopeErrors.length > 0) {
      fail(`Scope prefixes matching no key (${scopeErrors.length}):\n`);
      scopeErrors.forEach(error);
      fail("");
    }

    const drift = checkAvoidRules(glossary, catalogs);
    if (drift.length > 0) {
      fail(`Terminology drift (${drift.length}) — a forbidden variant is in a catalog:\n`);
      drift.forEach(error);
      fail(
        `\nFix the catalog entries, or change glossary.json if the term itself should move on.\n`
      );
    }
  }

  let doc = null;
  try {
    doc = readFileSync(docPath, "utf-8");
  } catch (error_) {
    error(`generated table unreadable: ${error_.message}`);
  }
  if (doc !== null) {
    try {
      if (generatedBlock(doc) !== renderBlock(glossary)) {
        error(
          `generated table out of sync: run \`pnpm --dir frontend i18n:glossary --write-doc\` and commit the result.`
        );
      }
    } catch (error_) {
      error(`generated table out of sync: ${error_.message}`);
    }
  }

  return { errorCount, report };
}

export function countRules(glossary) {
  return (glossary.terms ?? []).reduce(
    (total, term) => total + Object.values(term.avoid ?? {}).reduce((n, p) => n + p.length, 0),
    0
  );
}

function readGlossary() {
  return JSON.parse(readFileSync(GLOSSARY_PATH, "utf-8"));
}

function printTerms(query) {
  const glossary = readGlossary();
  if (!query) {
    console.log(renderTable(glossary));
    return;
  }
  const needle = query.toLowerCase();
  const matches = (glossary.terms ?? []).filter(
    (term) =>
      term.id.includes(needle) ||
      Object.values(term.translations ?? {}).some((value) => value.toLowerCase().includes(needle))
  );
  if (matches.length === 0) {
    console.error(`i18n glossary: no term matches ${JSON.stringify(query)}.`);
    process.exitCode = 1;
    return;
  }
  for (const term of matches) {
    console.log(`${term.id} (${term.category})`);
    for (const locale of glossary.locales) {
      console.log(`  ${locale}: ${term.translations[locale]}`);
    }
    console.log(`  definition: ${term.definition}`);
    console.log(`  scope: ${(term.scope ?? []).join(", ") || "all keys"}`);
    const when = Object.entries(term.when ?? {});
    if (when.length > 0) {
      console.log(`  when: ${when.map(([locale, pattern]) => `${locale} ~ ${pattern}`).join("; ")}`);
    }
    const avoid = Object.entries(term.avoid ?? {});
    if (avoid.length > 0) {
      console.log(`  do not use: ${avoid.map(([l, p]) => `${l}: ${p.join(", ")}`).join("; ")}`);
    }
  }
}

function writeDoc() {
  const glossary = readGlossary();
  const integrity = validateGlossary(glossary);
  if (integrity.length > 0) {
    for (const line of integrity) console.error(`  - ${line}`);
    process.exit(1);
  }
  const doc = readFileSync(DOC_PATH, "utf-8");
  writeFileSync(DOC_PATH, spliceDoc(doc, renderBlock(glossary)));
  console.log(
    `i18n glossary: rewrote ${relative(process.cwd(), DOC_PATH)} (${glossary.terms.length} terms).`
  );
}

function main() {
  const args = process.argv.slice(2);
  if (args[0] === "--show") {
    printTerms(args[1]);
    return;
  }
  if (args.includes("--write-doc")) {
    writeDoc();
    return;
  }

  const { errorCount, report } = runChecks();
  if (errorCount > 0) {
    for (const line of report) console.error(line);
    process.exit(1);
  }
  const glossary = readGlossary();
  console.log(
    `i18n glossary: ${glossary.terms.length} terms and ${countRules(glossary)} rules match ` +
      `glossary.json.`
  );
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main();
}
