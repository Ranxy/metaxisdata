import { describe, expect, it } from "vitest";
import {
  formatDate,
  formatDateTime,
  formatRelativeTime,
  formatTime,
} from "./datetime";

const sample = { seconds: 1_758_800_000n, nanos: 0 };

describe("formatDateTime", () => {
  it("follows the locale it is given", () => {
    const english = formatDateTime(sample, "en-US");
    const chinese = formatDateTime(sample, "zh-CN");
    expect(english).not.toBe("");
    expect(chinese).not.toBe("");
    expect(english).not.toBe(chinese);
  });

  it("renders 24-hour time without an AM/PM marker", () => {
    const date = new Date(2025, 8, 25, 14, 30);
    expect(formatDateTime(date, "en-US")).not.toMatch(/[AP]M/i);
  });

  it("adds seconds only when asked", () => {
    const date = new Date(2025, 8, 25, 14, 30, 45);
    expect(formatDateTime(date, "en-US")).toBe("09/25/2025, 14:30");
    expect(formatDateTime(date, "en-US", { seconds: true })).toBe(
      "09/25/2025, 14:30:45"
    );
  });

  it("can render a month name", () => {
    const date = new Date(2025, 8, 25, 14, 30);
    expect(formatDateTime(date, "en-US", { month: "short" })).toBe(
      "Sep 25, 2025, 14:30"
    );
  });

  it("falls back for a missing or unset timestamp", () => {
    expect(formatDateTime(undefined, "en-US")).toBe("-");
    expect(formatDateTime(null, "en-US")).toBe("-");
    expect(formatDateTime({ seconds: 0n }, "en-US")).toBe("-");
    expect(
      formatDateTime({ seconds: 1_758_800_000n, nanos: Number.NaN }, "en-US")
    ).not.toBe("-");
    expect(formatDateTime("not a date", "en-US")).toBe("-");
    expect(formatDateTime(undefined, "en-US", { fallback: "" })).toBe("");
  });
});

describe("formatDate", () => {
  it("treats a bare calendar date as local midnight", () => {
    expect(formatDate("2025-09-25", "en-US")).toBe("09/25/2025");
  });

  it("returns the given fallback for an unparsable value", () => {
    expect(formatDate("2025-09-25 is not a date", "en-US")).toBe("-");
    expect(formatDate("", "en-US", "raw")).toBe("raw");
  });
});

describe("formatTime", () => {
  it("renders a 24-hour clock", () => {
    expect(formatTime(new Date(2025, 8, 25, 9, 5), "en-US")).toBe("09:05");
  });

  it("adds seconds when asked", () => {
    expect(
      formatTime(new Date(2025, 8, 25, 9, 5, 7), "en-US", { seconds: true })
    ).toBe("09:05:07");
  });

  it("defaults to an empty string when there is no value", () => {
    expect(formatTime(null, "en-US")).toBe("");
  });

  it("returns the given fallback for a missing value", () => {
    expect(formatTime(undefined, "en-US", { fallback: "-" })).toBe("-");
  });
});

describe("formatRelativeTime", () => {
  it("describes a past timestamp", () => {
    const oneHourAgo = Math.floor(Date.now() / 1000) - 3600;
    expect(formatRelativeTime({ seconds: oneHourAgo }, "en-US")).toContain(
      "hour"
    );
  });

  it("falls back for a missing timestamp", () => {
    expect(formatRelativeTime(undefined, "en-US")).toBe("-");
  });
});
