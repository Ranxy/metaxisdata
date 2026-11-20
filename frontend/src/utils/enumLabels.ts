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
          case "\\":
          case "'":
          case '"':
            label += escaped;
            break;
          default:
            // An escape this build does not know could mean a label it would
            // read wrongly; report the set as unknown instead.
            return [];
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
 * Labels of a schema-qualified PostgreSQL enum reference, looked up in the enum
 * types of the schema the caller loaded (`public.product_status` →
 * `product_status` in schema `public`). Empty when the spelling is not a
 * schema-qualified type, the type belongs to another schema (its labels were not
 * loaded, and a same-named type elsewhere is not it), or the schema does not
 * declare that enum.
 */
export function enumValuesFromSchemaType(
  type: string,
  enumTypes: Record<string, string[]>,
  schema: string
): string[] {
  if (qualifiedTypeSchema(type) !== schema) {
    return [];
  }
  const name = type.trim().slice(type.trim().lastIndexOf(".") + 1);
  return enumTypes[name] ?? [];
}

/**
 * The schema a schema-qualified type belongs to, or the empty string for a
 * built-in, array, parameterized or unqualified spelling. The backend resolves
 * labels in this schema, so the caller only offers them when it read that one.
 */
export function qualifiedTypeSchema(type: string): string {
  const trimmed = type.trim();
  if (trimmed === "" || trimmed.endsWith("[]")) {
    return "";
  }
  if (trimmed.includes("(") || trimmed.includes(")") || trimmed.includes(" ")) {
    return "";
  }
  const dot = trimmed.lastIndexOf(".");
  if (dot <= 0 || dot === trimmed.length - 1) {
    return "";
  }
  return trimmed.slice(0, dot);
}
