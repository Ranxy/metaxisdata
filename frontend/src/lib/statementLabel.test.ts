import { describe, expect, it } from "vitest";
import { STATEMENT_LABEL_RUNES, statementLabel } from "./statementLabel";

describe("statementLabel", () => {
  it("leaves a one-line statement alone", () => {
    expect(statementLabel("SELECT a FROM t")).toBe("SELECT a FROM t");
  });

  // The server's rule, mirrored: the same statement has to read the same in a graph node
  // and in the breadcrumb above it.
  it("joins the lines of a multi-line statement", () => {
    expect(statementLabel("SELECT a\nFROM t\nWHERE b = ?")).toBe(
      "SELECT a FROM t WHERE b = ?"
    );
  });

  it("collapses runs of whitespace and trims the ends", () => {
    expect(statementLabel("  SELECT  a\n\t\tFROM   t  ")).toBe(
      "SELECT a FROM t"
    );
  });

  it("names a CTE by the whole statement", () => {
    const cte = [
      "WITH recent AS (",
      "    SELECT id",
      "    FROM t",
      ")",
      "INSERT INTO audit (id)",
      "SELECT id FROM recent",
    ].join("\n");

    expect(statementLabel(cte)).toBe(
      "WITH recent AS ( SELECT id FROM t ) INSERT INTO audit (id) SELECT id FROM recent"
    );
  });

  it("bounds the label in runes, not bytes", () => {
    const label = statementLabel("查".repeat(STATEMENT_LABEL_RUNES + 10));

    expect(Array.from(label)).toHaveLength(STATEMENT_LABEL_RUNES + 1);
    expect(label.endsWith("…")).toBe(true);
  });
});
