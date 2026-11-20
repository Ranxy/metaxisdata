/**
 * Declared enum labels, as the metadata registry records them.
 *
 * PostgreSQL keeps enum types in the schema's metadata and a column names one
 * schema-qualified (`public.product_status`). The MySQL family has no type-level
 * enum, so the declared labels live inline in the column type string
 * (`enum('draft','published')`). Both surfaces are best effort: an unknown
 * spelling yields no labels, and the caller then offers no hint instead of
 * guessing.
 */

/**
 * Parses the labels of an inline MySQL-family ENUM column type. Returns an empty
 * list for every other type (`set(...)`, `varchar(64)`, …). The parser follows
 * the engine's quoting: single-quoted labels, a doubled quote for an embedded
 * quote, and the characters the engine cannot print escaped with a backslash.
 */
export function enumValuesFromColumnType(type: string): string[] {
  const trimmed = type.trim();
  const prefix = "enum(";
  if (
    trimmed.length <= prefix.length + 1 ||
    !trimmed.slice(0, prefix.length).toLowerCase().startsWith(prefix) ||
    !trimmed.endsWith(")")
  ) {
    return [];
  }
  const body = trimmed.slice(prefix.length, -1);
  const values: string[] = [];
  let index = 0;
  while (index < body.length) {
    while (
      index < body.length &&
      (body[index] === " " || body[index] === ",")
    ) {
      index++;
    }
    if (index >= body.length) {
      break;
    }
    if (body[index] !== "'") {
      return [];
    }
    index++;
    let label = "";
    let closed = false;
    while (index < body.length && !closed) {
      const char = body[index];
      if (char === "'") {
        if (body[index + 1] === "'") {
          label += "'";
          index += 2;
          continue;
        }
        index++;
        closed = true;
        continue;
      }
      if (char === "\\") {
        const escaped = body[index + 1];
        if (escaped === undefined) {
          label += "\\";
          index++;
          continue;
        }
        switch (escaped) {
          case "0":
            label += "\0";
            break;
          case "n":
            label += "\n";
            break;
          case "r":
            label += "\r";
            break;
          case "Z":
            label += "\u001a";
            break;
          default:
            label += escaped;
        }
        index += 2;
        continue;
      }
      label += char;
      index++;
    }
    if (!closed) {
      return [];
    }
    values.push(label);
  }
  return values;
}

/**
 * Labels of a schema-qualified PostgreSQL enum reference, looked up by type name
 * in the schema's enum types (`public.product_status` → `product_status`). Empty
 * when the spelling is not a schema-qualified type or the schema does not declare
 * that enum.
 */
export function enumValuesFromSchemaType(
  type: string,
  enumTypes: Record<string, string[]>
): string[] {
  const trimmed = type.trim();
  if (trimmed === "" || trimmed.endsWith("[]")) {
    return [];
  }
  if (trimmed.includes("(") || trimmed.includes(")") || trimmed.includes(" ")) {
    return [];
  }
  const dot = trimmed.lastIndexOf(".");
  if (dot <= 0 || dot === trimmed.length - 1) {
    return [];
  }
  return enumTypes[trimmed.slice(dot + 1)] ?? [];
}
