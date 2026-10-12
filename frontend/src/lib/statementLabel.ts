/**
 * The label a statement shows where only one line fits: a table row, a graph node, a
 * breadcrumb.
 *
 * It mirrors the server's `statementLabel`, which fills the registry mirror's `name`.
 * The client computes it too because the two places that need it hold the statement's
 * text and not its mirror row — the Application SQL list reads the operational
 * `ApplicationSQL`, and the graph resolves a node's label from `GetApplicationSQL` once
 * per statement. Both would otherwise disagree with the breadcrumb beside them.
 *
 * The whole statement is the label, not its first line: a multi-line statement would
 * otherwise be named by whatever sits on line one, which is `WITH recent AS (` for a CTE
 * and the keyword after a leading comment for anything that opens with one.
 */

/** The bound, in runes, the server also applies. */
export const STATEMENT_LABEL_RUNES = 120;

export function statementLabel(sqlText: string): string {
  const collapsed = sqlText.split(/\s+/).filter(Boolean).join(" ");
  const runes = Array.from(collapsed);
  if (runes.length > STATEMENT_LABEL_RUNES) {
    return `${runes.slice(0, STATEMENT_LABEL_RUNES).join("")}…`;
  }
  return collapsed;
}
