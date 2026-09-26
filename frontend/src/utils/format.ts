/** Presentation helpers shared by the metadata surfaces. */

/** `rowCount` and friends are int64, so they arrive as bigints. */
export function formatNumber(value: bigint | number): string {
  return new Intl.NumberFormat().format(Number(value));
}

/** A byte count in binary units: 8.0 KB, 1.5 MB. */
export function formatBytes(bytes: bigint | number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = Number(bytes);
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex++;
  }
  return `${size.toFixed(1)} ${units[unitIndex]}`;
}
