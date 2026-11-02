/**
 * Code table values as the settings editor reads them.
 *
 * A code table is a plain list of accepted codes. The editor takes one code per
 * line, or imports a CSV whose first column is the code; both paths trim each
 * value, skip blank and comment lines, and de-duplicate so the same value is
 * never sent twice. The server sorts and de-duplicates again, but doing it here
 * keeps the textarea and the request showing the same list.
 */

/** One code per line; `#` starts a comment. Blank lines are ignored. */
export function parseCodeTableLines(text: string): string[] {
  const values: string[] = [];
  const seen = new Set<string>();
  for (const line of lines(text)) {
    const value = line.trim();
    if (value === "" || value.startsWith("#") || seen.has(value)) {
      continue;
    }
    seen.add(value);
    values.push(value);
  }
  return values;
}

/**
 * The first field of every record in a CSV document. Quoted fields may contain
 * commas and escaped quotes; a UTF-8 BOM and CRLF line endings are accepted. The
 * header row is not special: a code table's values are codes, and a header that
 * is not a code is removed by de-duplication only if it repeats — callers that
 * import an exported file should therefore keep the header out of the editor's
 * textarea, which is why the importer appends the parsed codes to the text.
 */
export function parseCodeTableCsv(text: string): string[] {
  const values: string[] = [];
  let field = "";
  let inQuotes = false;
  /** Whether the current record has started; a blank line has not. */
  let started = false;
  /** Whether the reader is still inside the record's first field. */
  let inFirstField = true;

  const endRecord = () => {
    if (started) {
      values.push(field);
    }
    field = "";
    inQuotes = false;
    started = false;
    inFirstField = true;
  };

  const source = stripBom(text);
  for (let index = 0; index < source.length; index += 1) {
    const character = source[index] ?? "";
    if (inQuotes) {
      if (character === '"') {
        if (source[index + 1] === '"') {
          field += '"';
          index += 1;
        } else {
          inQuotes = false;
        }
      } else {
        field += character;
      }
      continue;
    }
    if (character === '"' && !started) {
      inQuotes = true;
      started = true;
      continue;
    }
    if (character === ",") {
      // The first field of the record is complete; the rest is not a code.
      started = true;
      inFirstField = false;
      continue;
    }
    if (character === "\n" || character === "\r") {
      if (character === "\r" && source[index + 1] === "\n") {
        index += 1;
      }
      endRecord();
      continue;
    }
    if (inFirstField) {
      field += character;
      started = true;
    }
  }
  endRecord();
  return values;
}

/**
 * The textarea text for a code table's values. `joinCodeTableValues` is the
 * inverse of `parseCodeTableLines` for an already normalised list.
 */
export function joinCodeTableValues(values: readonly string[]): string {
  return values.join("\n");
}

/**
 * Append the codes a CSV contributed to whatever the reader already typed,
 * keeping one code per line and de-duplicating across the two sources.
 */
export function mergeCodeTableValues(
  existingText: string,
  importedValues: readonly string[]
): string {
  return joinCodeTableValues(
    parseCodeTableLines([existingText, ...importedValues].join("\n"))
  );
}

function lines(text: string): string[] {
  return stripBom(text).replace(/\r\n?/g, "\n").split("\n");
}

function stripBom(text: string): string {
  return text.charCodeAt(0) === 0xfe_ff ? text.slice(1) : text;
}
