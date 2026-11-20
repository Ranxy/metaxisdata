import { describe, expect, it } from "vitest";
import {
  enumValuesFromColumnType,
  enumValuesFromSchemaType,
  qualifiedTypeSchema,
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

  it("keeps leading and trailing spaces of a declared label", () => {
    expect(enumValuesFromColumnType("enum(' draft ','active')")).toEqual([
      " draft ",
      "active",
    ]);
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

  it("handles trailing separators, escapes and unterminated labels", () => {
    expect(enumValuesFromColumnType("enum('a',)")).toEqual(["a"]);
    expect(enumValuesFromColumnType("enum('a\\)")).toEqual([]);
    expect(enumValuesFromColumnType("enum('a\\rb','a\\Zb')")).toEqual([
      "a\rb",
      "a\u001ab",
    ]);
    expect(enumValuesFromColumnType("enum('a)")).toEqual([]);
  });

  it("treats an unknown backslash escape as an unknown label set", () => {
    expect(enumValuesFromColumnType("enum('a\\qb')")).toEqual([]);
  });
});

describe("enumValuesFromSchemaType", () => {
  const enumTypes = {
    product_status: ["active", "inactive", "archived", "draft"],
  };

  it("looks a PostgreSQL enum up in the schema that was read", () => {
    expect(
      enumValuesFromSchemaType("public.product_status", enumTypes, "public")
    ).toEqual(["active", "inactive", "archived", "draft"]);
  });

  it("refuses a type from another schema, even with a matching name", () => {
    expect(
      enumValuesFromSchemaType("shared.product_status", enumTypes, "public")
    ).toEqual([]);
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
      expect(enumValuesFromSchemaType(type, enumTypes, "public")).toEqual([]);
    }
  });
});

describe("qualifiedTypeSchema", () => {
  it("returns the schema of a schema-qualified type", () => {
    expect(qualifiedTypeSchema("public.product_status")).toBe("public");
    expect(qualifiedTypeSchema("shared.status_type")).toBe("shared");
  });

  it("returns nothing for built-in, array and parameterized spellings", () => {
    for (const type of [
      "text",
      "character varying(64)",
      "numeric(10,2)",
      "text[]",
      "_int4",
      "public.",
      "",
    ]) {
      expect(qualifiedTypeSchema(type)).toBe("");
    }
  });
});
