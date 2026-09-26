import { describe, expect, it } from "vitest";
import {
  cloneDateRange,
  dateRangeBound,
  defaultDateRange,
  emptyDateRange,
  formatDateRangeChip,
  isSameDateRange,
} from "./dateRange";

const day = (iso: string) => ({ toString: () => iso });
const range = (start?: string, end?: string) => ({
  start: start ? day(start) : undefined,
  end: end ? day(end) : undefined,
});

describe("date range helpers", () => {
  it("defaults to the last month and can be emptied", () => {
    const fallback = defaultDateRange();
    expect(fallback.start?.toString()).toBeTruthy();
    expect(fallback.end?.toString()).toBeTruthy();
    expect(fallback.start?.toString() < fallback.end?.toString()).toBe(true);

    const empty = emptyDateRange();
    expect(empty.start).toBeUndefined();
    expect(empty.end).toBeUndefined();
  });

  it("compares ranges by their endpoints", () => {
    expect(isSameDateRange(range("2026-09-01"), range("2026-09-01"))).toBe(
      true
    );
    expect(isSameDateRange(range("2026-09-01"), range("2026-09-02"))).toBe(
      false
    );
  });

  it("clones without sharing the endpoints", () => {
    const source = range("2026-09-01", "2026-09-02");
    const copy = cloneDateRange(source);
    expect(copy).toEqual(source);
    expect(copy).not.toBe(source);
  });

  it("turns a day into the inclusive filter bounds", () => {
    expect(dateRangeBound(day("2026-09-01"), "start")).toBe(
      new Date("2026-09-01T00:00:00.000").toISOString()
    );
    expect(dateRangeBound(day("2026-09-01"), "end")).toBe(
      new Date("2026-09-01T23:59:59.999").toISOString()
    );
    expect(dateRangeBound(undefined, "start")).toBe("");
  });

  it("labels a full, half-open and empty range", () => {
    const full = formatDateRangeChip(
      range("2026-09-01", "2026-09-02"),
      "en-US"
    );
    expect(full).toContain(" - ");
    expect(formatDateRangeChip(range("2026-09-01"), "en-US")).toMatch(/^>= /);
    expect(
      formatDateRangeChip(range(undefined, "2026-09-02"), "en-US")
    ).toMatch(/^<= /);
    expect(formatDateRangeChip(emptyDateRange(), "en-US")).toBe("");
  });
});
