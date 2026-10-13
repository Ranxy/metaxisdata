// @vitest-environment node
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  BEGIN_MARKER,
  END_MARKER,
  compilePattern,
  matchesPattern,
  renderBlock,
  renderTable,
  runChecks,
  spliceDoc,
  validateGlossary,
  whenMatches,
} from "./check-i18n-glossary.mjs";

const fixtures = [];

afterEach(() => {
  while (fixtures.length > 0) {
    rmSync(fixtures.pop(), { recursive: true, force: true });
  }
});

const CATEGORIES = { Data: "Data objects." };

function baseGlossary(overrides = {}) {
  return {
    locales: ["en-US", "zh-CN"],
    categories: CATEGORIES,
    terms: [
      {
        id: "column",
        category: "Data",
        definition: "A field.",
        translations: { "en-US": "column", "zh-CN": "字段" },
        scope: ["browser."],
        when: { "en-US": "/\\bcolumns?\\b/i" },
        avoid: { "zh-CN": ["/列(?!级|表)/"] },
      },
    ],
    ...overrides,
  };
}

function makeFixture({
  glossary = baseGlossary(),
  catalogs = {
    "en-US": { browser: { search: "Table or column name", order: "Sorted by name" } },
    "zh-CN": { browser: { search: "表名或字段名", order: "按名称排列" } },
  },
  doc,
} = {}) {
  const dir = mkdtempSync(join(tmpdir(), "i18n-glossary-"));
  fixtures.push(dir);
  const localesDir = join(dir, "locales");
  mkdirSync(localesDir);
  const glossaryPath = join(localesDir, "glossary.json");
  writeFileSync(glossaryPath, JSON.stringify(glossary, null, 2));
  for (const [locale, messages] of Object.entries(catalogs)) {
    writeFileSync(join(localesDir, `${locale}.json`), JSON.stringify(messages, null, 2));
  }
  const docPath = join(dir, "i18n-glossary.md");
  const body = doc ?? `${BEGIN_MARKER}${renderBlock(glossary)}${END_MARKER}`;
  writeFileSync(docPath, body);
  return { glossaryPath, docPath, localesDir };
}

describe("compilePattern and matchesPattern", () => {
  it("treats a bare word as a literal substring", () => {
    expect(compilePattern("任务")).toBeNull();
    expect(matchesPattern("OpenLineage 任务", "任务")).toBe(true);
    expect(matchesPattern("OpenLineage 作业", "任务")).toBe(false);
  });

  it("compiles a slash-delimited pattern with flags", () => {
    expect(matchesPattern("Disallow password signin", "/\\bsignin\\b/i")).toBe(true);
    expect(matchesPattern("The sign-in command", "/\\bsignin\\b/i")).toBe(false);
  });
});

describe("validateGlossary", () => {
  it("accepts a well-formed glossary", () => {
    expect(validateGlossary(baseGlossary())).toEqual([]);
  });

  it("flags a duplicate id", () => {
    const glossary = baseGlossary({
      terms: [
        baseGlossary().terms[0],
        { ...baseGlossary().terms[0], translations: { "en-US": "table", "zh-CN": "表" } },
      ],
    });
    expect(validateGlossary(glossary).join("\n")).toContain("duplicate id");
  });

  it("flags a category that was never declared", () => {
    const glossary = baseGlossary();
    glossary.terms[0].category = "Nope";
    expect(validateGlossary(glossary).join("\n")).toContain("category must be one of Data");
  });

  it("requires a translation for every declared locale", () => {
    const glossary = baseGlossary();
    glossary.terms[0].translations = { "en-US": "column" };
    expect(validateGlossary(glossary).join("\n")).toContain("missing a value for zh-CN");
  });

  it("flags an unknown locale in avoid", () => {
    const glossary = baseGlossary();
    glossary.terms[0].avoid = { "fr-FR": ["colonne"] };
    expect(validateGlossary(glossary).join("\n")).toContain('unknown locale "fr-FR"');
  });

  it("flags a malformed regex pattern", () => {
    const glossary = baseGlossary();
    glossary.terms[0].avoid = { "zh-CN": ["/[unclosed/"] };
    expect(validateGlossary(glossary).join("\n")).toContain("is not a valid regex");
  });
});

describe("whenMatches", () => {
  const catalogs = { "en-US": { "browser.search": "Table or column name" } };

  it("requires every conditioned locale to match", () => {
    expect(whenMatches({ "en-US": "/\\bcolumns?\\b/i" }, catalogs, "browser.search")).toBe(true);
    expect(whenMatches({ "en-US": "/table/i" }, catalogs, "browser.search")).toBe(true);
    expect(whenMatches({ "en-US": "/\\bviews?\\b/i" }, catalogs, "browser.search")).toBe(false);
  });

  it("treats a key with no value in that locale as not matching", () => {
    expect(whenMatches({ "en-US": "column" }, catalogs, "browser.missing")).toBe(false);
  });
});

describe("runChecks", () => {
  it("passes when the glossary, catalogs and generated table agree", () => {
    const { errorCount, report } = runChecks(makeFixture());
    expect(report).toEqual([]);
    expect(errorCount).toBe(0);
  });

  it("flags a forbidden literal variant inside the scope", () => {
    const fixture = makeFixture({
      catalogs: {
        "en-US": { browser: { search: "Table or column name" } },
        "zh-CN": { browser: { search: "表名或列名" } },
      },
    });
    const { errorCount, report } = runChecks(fixture);
    expect(errorCount).toBe(1);
    expect(report.join("\n")).toContain("zh-CN browser.search");
    expect(report.join("\n")).toContain('glossary "column" fixes "字段"');
  });

  it("ignores a forbidden variant outside the scope", () => {
    const fixture = makeFixture({
      catalogs: {
        "en-US": {
          browser: { search: "Table or column name" },
          other: { search: "Table or column name" },
        },
        "zh-CN": { browser: { search: "表名或字段名" }, other: { search: "表名或列名" } },
      },
    });
    expect(runChecks(fixture).errorCount).toBe(0);
  });

  it("applies a rule only where the conditioned locale matches", () => {
    const fixture = makeFixture({
      catalogs: {
        "en-US": { browser: { search: "Table or column name", order: "Sorted by name" } },
        "zh-CN": { browser: { search: "表名或列名", order: "按名称排列" } },
      },
    });
    const { errorCount, report } = runChecks(fixture);
    expect(errorCount).toBe(1);
    expect(report.join("\n")).toContain("browser.search");
    expect(report.join("\n")).not.toContain("browser.order");
  });

  it("flags a forbidden regular expression", () => {
    const glossary = baseGlossary();
    delete glossary.terms[0].scope;
    delete glossary.terms[0].when;
    glossary.terms[0].avoid = { "en-US": ["/\\bsignin\\b/i"] };
    const fixture = makeFixture({
      glossary,
      catalogs: {
        "en-US": { browser: { search: "Disallow password signin" } },
        "zh-CN": { browser: { search: "禁止密码登录" } },
      },
    });
    expect(runChecks(fixture).report.join("\n")).toContain("en-US browser.search");
  });

  it("flags a scope prefix that matches no key", () => {
    const glossary = baseGlossary();
    glossary.terms[0].scope = ["nowhere."];
    const fixture = makeFixture({ glossary });
    expect(runChecks(fixture).report.join("\n")).toContain('scope "nowhere." matches no key');
  });

  it("flags a generated table that no longer matches the glossary", () => {
    const stale = makeFixture({
      doc: `${BEGIN_MARKER}\n| old |\n${END_MARKER}`,
    });
    const { errorCount, report } = runChecks(stale);
    expect(errorCount).toBe(1);
    expect(report.join("\n")).toContain("--write-doc");
  });

  it("flags a document without the marker pair", () => {
    const fixture = makeFixture({ doc: "# no markers here\n" });
    expect(runChecks(fixture).report.join("\n")).toContain("is missing");
  });

  it("flags a document that mentions a marker twice", () => {
    const glossary = baseGlossary();
    const fixture = makeFixture({
      glossary,
      doc: `${BEGIN_MARKER}${renderBlock(glossary)}${END_MARKER}\n\nprose quoting ${BEGIN_MARKER} again\n`,
    });
    expect(runChecks(fixture).report.join("\n")).toContain("more than once");
  });

  it("reports an unreadable glossary instead of throwing", () => {
    const fixture = makeFixture();
    writeFileSync(fixture.glossaryPath, "{ not json");
    const { errorCount, report } = runChecks(fixture);
    expect(errorCount).toBe(1);
    expect(report.join("\n")).toContain("glossary unreadable");
  });
});

describe("renderTable and spliceDoc", () => {
  it("renders one row per term with the locale columns first", () => {
    const table = renderTable(baseGlossary());
    const [header, divider, row] = table.split("\n");
    expect(header).toContain("| en-US | zh-CN | Category | Definition | Scope | Do not use |");
    expect(divider).toBe("| --- | --- | --- | --- | --- | --- |");
    expect(row).toContain("| column | 字段 | Data | A field. | browser. |");
    expect(row).toContain("when en-US ~ `/\\bcolumns?\\b/i`");
  });

  it("escapes a pipe so the row stays one table row", () => {
    const glossary = baseGlossary();
    glossary.terms[0].definition = "A | B";
    expect(renderTable(glossary)).toContain("A \\| B");
  });

  it("replaces only the block between the markers, and is idempotent", () => {
    const glossary = baseGlossary();
    const original = `# Doc\n\n${BEGIN_MARKER}\nstale\n${END_MARKER}\n\nfooter\n`;
    const next = spliceDoc(original, renderBlock(glossary));
    expect(next).toContain("# Doc");
    expect(next).toContain("footer");
    expect(next).not.toContain("stale");
    expect(spliceDoc(next, renderBlock(glossary))).toBe(next);
  });

  it("refuses a document without the markers", () => {
    expect(() => spliceDoc("# Doc\n", "\nblock\n")).toThrow(/missing/);
  });
});
