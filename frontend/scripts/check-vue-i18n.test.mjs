// @vitest-environment node
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  collectKeysFromSource,
  extractPlaceholders,
  flatten,
  runChecks,
} from "./check-vue-i18n.mjs";

const fixtures = [];

afterEach(() => {
  while (fixtures.length > 0) {
    rmSync(fixtures.pop(), { recursive: true, force: true });
  }
});

function makeFixture({ en, zh, source = {} }) {
  const dir = mkdtempSync(join(tmpdir(), "vue-i18n-"));
  fixtures.push(dir);
  mkdirSync(join(dir, "locales"));
  mkdirSync(join(dir, "src"));
  writeFileSync(join(dir, "locales/en-US.json"), JSON.stringify(en, null, 2));
  writeFileSync(join(dir, "locales/zh-CN.json"), JSON.stringify(zh, null, 2));
  for (const [name, content] of Object.entries(source)) {
    writeFileSync(join(dir, "src", name), content);
  }
  return {
    sourceDir: join(dir, "src"),
    localesDir: join(dir, "locales"),
    locales: ["en-US", "zh-CN"],
  };
}

describe("flatten", () => {
  it("flattens nested objects into dotted keys", () => {
    expect(flatten({ a: { b: "1", c: { d: "2" } }, e: "3" })).toEqual({
      "a.b": "1",
      "a.c.d": "2",
      e: "3",
    });
  });
});

describe("collectKeysFromSource", () => {
  it("collects direct and indirect key literals", () => {
    const src = `
      const { t } = useI18n();
      t("a.one");
      $t('a.two');
      te("a.three");
      tm("a.four");
      showSuccess("a.five");
      handleError(err, "a.six");
      handleError(err, t("a.seven"));
      const props = { titleKey: "a.eight" };
      const cond = flag ? t("a.nine") : t("a.ten");
    `;
    const keys = collectKeysFromSource(src);
    const expected = [
      "a.one",
      "a.two",
      "a.three",
      "a.four",
      "a.five",
      "a.six",
      "a.seven",
      "a.eight",
      "a.nine",
      "a.ten",
    ];
    for (const key of expected) {
      expect(keys.has(key), `expected ${key} to be collected`).toBe(true);
    }
  });

  it("collects <i18n-t keypath> and v-t directive literals", () => {
    const src = `
      <i18n-t keypath="a.keypath" tag="span" />
      <p v-t="'a.directive'" />
    `;
    const keys = collectKeysFromSource(src);
    expect(keys.has("a.keypath")).toBe(true);
    expect(keys.has("a.directive")).toBe(true);
  });
});

describe("extractPlaceholders", () => {
  it("extracts single-brace names and ignores double braces", () => {
    expect([...extractPlaceholders("Hi {name}, {count} left")].sort()).toEqual([
      "count",
      "name",
    ]);
    expect([...extractPlaceholders("Hi {{name}}")]).toEqual([]);
  });
});

describe("runChecks", () => {
  it("passes when code and both locales agree", () => {
    const options = makeFixture({
      en: { a: { one: "Hi {name}" } },
      zh: { a: { one: "你好 {name}" } },
      source: { "App.vue": `<template>{{ t("a.one") }}</template>` },
    });
    expect(runChecks(options).errorCount).toBe(0);
  });

  it("reports keys used in code but absent from the locales", () => {
    const options = makeFixture({
      en: { a: { one: "One" } },
      zh: { a: { one: "一" } },
      source: {
        "App.vue": `<template>{{ t("a.one") }} {{ t("a.missing") }}</template>`,
      },
    });
    const { errorCount, report } = runChecks(options);
    expect(errorCount).toBe(1);
    expect(report.join("\n")).toContain("a.missing");
  });

  it("reports locale keys that no code references", () => {
    const options = makeFixture({
      en: { a: { one: "One", unused: "Unused" } },
      zh: { a: { one: "一", unused: "未使用" } },
      source: { "App.vue": `<template>{{ t("a.one") }}</template>` },
    });
    const { errorCount, report } = runChecks(options);
    expect(errorCount).toBe(1);
    expect(report.join("\n")).toContain("Unused keys");
    expect(report.join("\n")).toContain("a.unused");
  });

  it("reports keys missing from a non-reference locale", () => {
    const options = makeFixture({
      en: { a: { one: "One" } },
      zh: {},
      source: { "App.vue": `<template>{{ t("a.one") }}</template>` },
    });
    const { errorCount, report } = runChecks(options);
    expect(errorCount).toBe(1);
    expect(report.join("\n")).toContain("zh-CN: missing 1 key(s)");
  });

  it("flags double-brace placeholders in every locale", () => {
    const options = makeFixture({
      en: { a: { one: "Hi {{name}}" } },
      zh: { a: { one: "你好 {{name}}" } },
      source: { "App.vue": `<template>{{ t("a.one") }}</template>` },
    });
    const { errorCount, report } = runChecks(options);
    expect(errorCount).toBe(2);
    expect(report.join("\n")).toContain("double {{name}} placeholders");
  });

  it("flags placeholder sets that differ across locales", () => {
    const options = makeFixture({
      en: { a: { one: "Hi {name}, you have {count}" } },
      zh: { a: { one: "你好 {name}" } },
      source: { "App.vue": `<template>{{ t("a.one") }}</template>` },
    });
    const { errorCount, report } = runChecks(options);
    expect(errorCount).toBe(1);
    expect(report.join("\n")).toContain("placeholders differ from en-US");
    expect(report.join("\n")).toContain("count");
  });
});
