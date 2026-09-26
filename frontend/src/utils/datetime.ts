// Timestamp rendering is centralized here so every page shows the same shape and
// always follows the application locale. Callers pass `locale.value` from
// `useI18n()`; passing `"default"` or `undefined` would silently follow the OS.

/** Structural shape of a `google.protobuf.Timestamp`, without generated code. */
export interface TimestampLike {
  seconds?: bigint | number | string | null;
  nanos?: number | null;
}

export type DateTimeInput = TimestampLike | Date | string | null | undefined;

export interface DateTimeFormatOptions {
  /** `short` renders a month name (`Sep`) instead of a zero-padded number (`09`). */
  month?: "2-digit" | "short";
  seconds?: boolean;
  /** Returned when the value is missing or unparsable. Defaults to `-`. */
  fallback?: string;
}

export interface TimeFormatOptions {
  seconds?: boolean;
  /** Returned when the value is missing or unparsable. Defaults to an empty string. */
  fallback?: string;
}

const DEFAULT_FALLBACK = "-";

/**
 * A `Timestamp` at or before the epoch counts as missing, which is how the store
 * represents an unset column. A bare `YYYY-MM-DD` is a calendar date, so it
 * parses as local midnight rather than UTC midnight.
 */
function parseDateValue(value: DateTimeInput): Date | null {
  if (value === null || value === undefined) {
    return null;
  }
  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? null : value;
  }
  if (typeof value === "string") {
    if (!value) {
      return null;
    }
    const date = /^\d{4}-\d{2}-\d{2}$/.test(value)
      ? new Date(`${value}T00:00:00.000`)
      : new Date(value);
    return Number.isNaN(date.getTime()) ? null : date;
  }

  const seconds = Number(value.seconds ?? 0);
  const milliseconds = Number.isFinite(seconds) ? seconds * 1000 : Number.NaN;
  const nanos = Number(value.nanos ?? 0);
  const date = new Date(
    Number.isFinite(nanos)
      ? milliseconds + Math.floor(nanos / 1_000_000)
      : milliseconds
  );
  if (
    !Number.isFinite(seconds) ||
    seconds <= 0 ||
    Number.isNaN(date.getTime())
  ) {
    return null;
  }
  return date;
}

/** `2025-09-25 14:30`, 24-hour, in the given locale. */
export function formatDateTime(
  value: DateTimeInput,
  locale: string,
  options: DateTimeFormatOptions = {}
): string {
  const date = parseDateValue(value);
  if (!date) {
    return options.fallback ?? DEFAULT_FALLBACK;
  }
  return new Intl.DateTimeFormat(locale, {
    year: "numeric",
    month: options.month ?? "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    ...(options.seconds ? { second: "2-digit" } : {}),
    hour12: false,
  }).format(date);
}

/** `2025-09-25`, in the given locale. */
export function formatDate(
  value: DateTimeInput,
  locale: string,
  fallback = DEFAULT_FALLBACK
): string {
  const date = parseDateValue(value);
  if (!date) {
    return fallback;
  }
  return new Intl.DateTimeFormat(locale, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date);
}

/** `14:30` (`14:30:05` with `seconds`), 24-hour, in the given locale. */
export function formatTime(
  value: DateTimeInput,
  locale: string,
  options: TimeFormatOptions = {}
): string {
  const date = parseDateValue(value);
  if (!date) {
    return options.fallback ?? "";
  }
  return new Intl.DateTimeFormat(locale, {
    hour: "2-digit",
    minute: "2-digit",
    ...(options.seconds ? { second: "2-digit" } : {}),
    hour12: false,
  }).format(date);
}

/** `in 3 minutes` / `2 days ago`, in the given locale. */
export function formatRelativeTime(
  value: DateTimeInput,
  locale: string,
  fallback = DEFAULT_FALLBACK
): string {
  const date = parseDateValue(value);
  if (!date) {
    return fallback;
  }
  const diffMs = date.getTime() - Date.now();
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ["year", 365 * 24 * 60 * 60 * 1000],
    ["month", 30 * 24 * 60 * 60 * 1000],
    ["day", 24 * 60 * 60 * 1000],
    ["hour", 60 * 60 * 1000],
    ["minute", 60 * 1000],
    ["second", 1000],
  ];
  for (const [unit, unitMs] of units) {
    if (Math.abs(diffMs) >= unitMs || unit === "second") {
      return formatter.format(Math.round(diffMs / unitMs), unit);
    }
  }
  return formatter.format(0, "second");
}
