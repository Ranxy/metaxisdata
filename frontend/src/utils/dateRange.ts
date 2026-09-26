import { getLocalTimeZone, today } from "@internationalized/date";
import { formatDate } from "./datetime";

/**
 * Anything that renders as a calendar date. The helpers below only ever read its
 * string form, which keeps this module free of the calendar library's nominal
 * types (radix's `DateRange` accepts these structurally).
 */
type DateLike = { toString(): string } | undefined;

interface DateRangeLike {
  start?: DateLike;
  end?: DateLike;
}

export function emptyDateRange() {
  return { start: undefined, end: undefined };
}

/** The default range: the last month, ending today. */
export function defaultDateRange() {
  const end = today(getLocalTimeZone());
  return { start: end.subtract({ months: 1 }), end };
}

/** Keeps the caller's endpoint types, which radix's calendar requires. */
export function cloneDateRange<T extends DateRangeLike>(
  value: T
): { start: T["start"]; end: T["end"] } {
  return { start: value.start, end: value.end };
}

export function isSameDateRange(
  left: DateRangeLike,
  right: DateRangeLike
): boolean {
  return (
    dateString(left.start) === dateString(right.start) &&
    dateString(left.end) === dateString(right.end)
  );
}

/** An RFC 3339 instant for a filter bound: the start or the end of that day. */
export function dateRangeBound(
  value: DateLike,
  bound: "start" | "end"
): string {
  const date = dateString(value);
  if (!date) {
    return "";
  }
  const time = bound === "start" ? "T00:00:00.000" : "T23:59:59.999";
  const parsed = new Date(`${date}${time}`);
  return Number.isNaN(parsed.getTime()) ? "" : parsed.toISOString();
}

/** The pill text for a range: "a - b", ">= a", "<= b" or nothing. */
export function formatDateRangeChip(
  value: DateRangeLike,
  locale: string
): string {
  const from = formatBound(value.start, locale);
  const to = formatBound(value.end, locale);
  if (from && to) {
    return `${from} - ${to}`;
  }
  if (from) {
    return `>= ${from}`;
  }
  if (to) {
    return `<= ${to}`;
  }
  return "";
}

function dateString(value: DateLike): string {
  return value ? value.toString() : "";
}

function formatBound(value: DateLike, locale: string): string {
  const date = dateString(value);
  return date ? formatDate(date, locale, date) : "";
}
