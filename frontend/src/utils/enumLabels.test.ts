import { describe, expect, it } from "vitest";
import {
  enumValuesFromColumnType,
  enumValuesFromSchemaType,
} from "./enumLabels";

describe("enumValuesFromColumnType", () => {
  it("reads the labels of an inline MySQL-family enum", () => {
    expect(enumValuesFromColumnType("enum('small','medium','large')")).toEqual([
      "small",
      "medium",
      "large",
    ]);
    expect(enumValuesFromColumnType("ENUM('a', 'b')")).toEqual(["a", "b"]);
  });

  it("unescapes doubled quotes, commas and backslash escapes", () => {
    expect(enumValuesFromColumnType(`enum('it''s ok','a,b','c\\\\d')`)).toEqual(
      ["it's ok", "a,b", "c\\d"]
    );
    expect(
      enumValuesFromColumnType("enum('a\tb','c\\nd','q\"z','r\\0s')")
    ).toEqual(["a\tb", "c\nd", 'q"z', "r\u0000s"]);
  });

  it("returns nothing for a non-enum type", () => {
    for (const type of [
      "set('a','b')",
      "varchar(255)",
      "text",
      "enum()",
      "enum('a'",
      "enum(a,b)",
      "",
    ]) {
      expect(enumValuesFromColumnType(type)).toEqual([]);
    }
  });
});

describe("enumValuesFromSchemaType", () => {
  const enumTypes = {
    product_status: ["active", "inactive", "archived", "draft"],
  };

  it("looks a PostgreSQL enum up by its type name", () => {
    expect(
      enumValuesFromSchemaType("public.product_status", enumTypes)
    ).toEqual(["active", "inactive", "archived", "draft"]);
  });

  it("returns nothing when the type is not a registered enum", () => {
    for (const type of [
      "public.citext",
      "character varying(64)",
      "numeric(10,2)",
      "text[]",
      "_int4",
      "public.",
      "",
    ]) {
      expect(enumValuesFromSchemaType(type, enumTypes)).toEqual([]);
    }
  });
});
