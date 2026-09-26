/**
 * CSV building blocks. A cell is written as a quoted string: strings are used
 * as-is (with line endings normalized to `\n`), primitives are printed, and a
 * nested object (the request/response columns) is JSON.
 *
 * `bigint` is why this is not just `JSON.stringify`: an int64 proto field is a
 * bigint, which JSON.stringify refuses to serialize at all.
 */
export function csvCell(value: unknown): string {
  if (value == null) {
    return "";
  }
  switch (typeof value) {
    case "string":
      return value;
    case "bigint":
    case "number":
    case "boolean":
      return String(value);
    default:
      return JSON.stringify(value);
  }
}

export function csvEscape(value: unknown): string {
  const normalized = csvCell(value).replace(/\r\n?/g, "\n");
  return `"${normalized.replace(/"/g, '""')}"`;
}

export function toCsv(headers: string[], rows: unknown[][]): string {
  return [headers, ...rows]
    .map((row) => row.map((value) => csvEscape(value)).join(","))
    .join("\n");
}

/** A local-time stamp for a filename: 20260926-120830. */
export function fileTimestamp(date = new Date()): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return [
    date.getFullYear(),
    pad(date.getMonth() + 1),
    pad(date.getDate()),
    "-",
    pad(date.getHours()),
    pad(date.getMinutes()),
    pad(date.getSeconds()),
  ].join("");
}

export function downloadCsv(content: string, fileName: string) {
  const blob = new Blob([content], { type: "text/csv;charset=utf-8;" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = fileName;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}
