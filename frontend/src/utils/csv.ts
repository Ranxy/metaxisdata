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

/** One slice of a streamed CSV export: its bytes and the server's running count. */
export interface CsvStreamSlice {
  data: Uint8Array;
  rowCount: bigint;
  /** Whether the server's row ceiling cut the export short. */
  truncated?: boolean;
}

/** An assembled streamed export: the file's bytes and how many rows it holds. */
export interface CsvDownload {
  blob: Blob;
  rowCount: bigint;
  /** Whether the file holds only part of the selection because of the row cap. */
  truncated: boolean;
}

/**
 * Concatenate the slices of a streamed CSV export into one file. The slices
 * arrive in document order and may split a value's UTF-8 sequence, so they are
 * joined as bytes rather than decoded. The server writes the UTF-8 BOM and the
 * header row into the first slice; the client adds neither a BOM nor a
 * separator.
 *
 * A stream that delivered no bytes, or whose running row count went backwards,
 * cannot be a valid CSV document: fail loudly rather than saving a file that a
 * spreadsheet would open as empty or misordered.
 */
export async function collectCsvStream(
  slices: AsyncIterable<CsvStreamSlice>
): Promise<CsvDownload> {
  const parts: BlobPart[] = [];
  let rowCount = 0n;
  let truncated = false;
  let bytes = 0;
  for await (const slice of slices) {
    // A byte view is typed over `ArrayBufferLike`, which BlobPart excludes, but
    // Blob copies any view at run time.
    parts.push(slice.data as BlobPart);
    bytes += slice.data.byteLength;
    if (slice.rowCount < rowCount) {
      throw new Error("The export stream's row count went backwards.");
    }
    rowCount = slice.rowCount;
    truncated ||= slice.truncated === true;
  }
  if (bytes === 0) {
    throw new Error("The export returned no data.");
  }
  return {
    blob: new Blob(parts, { type: "text/csv;charset=utf-8;" }),
    rowCount,
    truncated,
  };
}

/** Hand an already-built file to the browser under `fileName`. */
export function downloadBlob(blob: Blob, fileName: string) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = fileName;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

export function downloadCsv(content: string, fileName: string) {
  downloadBlob(
    new Blob([content], { type: "text/csv;charset=utf-8;" }),
    fileName
  );
}
