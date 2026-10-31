import { describe, expect, it } from "vitest";
import {
  enumFilterValue,
  enumFromFilter,
  queryString,
  readFlag,
  sameQuery,
} from "./queryFilters";

describe("queryString", () => {
  it("reads the first value of a repeated parameter", () => {
    expect(queryString(["a", "b"])).toBe("a");
    expect(queryString([])).toBe("");
  });

  it("reads missing parameters as empty", () => {
    expect(queryString(undefined)).toBe("");
    expect(queryString(null)).toBe("");
  });

  it("reads a single value as-is", () => {
    expect(queryString("42")).toBe("42");
  });
});

describe("readFlag", () => {
  it("only treats 1 as true", () => {
    expect(readFlag("1")).toBe(true);
    expect(readFlag("0")).toBe(false);
    expect(readFlag(undefined)).toBe(false);
  });
});

describe("enumFilterValue", () => {
  const members = [0, 1, 2];

  it("keeps a member the enum contains", () => {
    expect(enumFilterValue("2", members)).toBe("2");
  });

  it("reads any unknown value as no filter", () => {
    expect(enumFilterValue("9", members)).toBe("-");
    expect(enumFilterValue("0", members)).toBe("-");
    expect(enumFilterValue("1.5", members)).toBe("-");
    expect(enumFilterValue("x", members)).toBe("-");
    expect(enumFilterValue("-", members)).toBe("-");
    expect(enumFilterValue(undefined, members)).toBe("-");
  });
});

describe("enumFromFilter", () => {
  const members = [0, 1, 2];

  it("returns the member a filter names", () => {
    expect(enumFromFilter("1", members)).toBe(1);
  });

  it("falls back to the first member", () => {
    expect(enumFromFilter("-", members)).toBe(0);
    expect(enumFromFilter("7", members)).toBe(0);
  });
});

describe("sameQuery", () => {
  it("compares parameters by name and first value", () => {
    expect(sameQuery({ a: "1", b: ["2", "3"] }, { a: "1", b: ["2"] })).toBe(
      true
    );
    expect(sameQuery({ a: "1" }, { a: "2" })).toBe(false);
    expect(sameQuery({ a: "1" }, { a: "1", b: "2" })).toBe(false);
  });
});
