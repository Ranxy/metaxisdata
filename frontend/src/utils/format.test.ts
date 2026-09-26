import { describe, expect, it } from "vitest";
import { formatBytes, formatNumber } from "./format";

describe("formatNumber", () => {
  it("groups digits and accepts the bigint an int64 field gives", () => {
    expect(formatNumber(1234n)).toBe(new Intl.NumberFormat().format(1234));
    expect(formatNumber(0)).toBe("0");
  });
});

describe("formatBytes", () => {
  it("uses binary units", () => {
    expect(formatBytes(0n)).toBe("0.0 B");
    expect(formatBytes(512n)).toBe("512.0 B");
    expect(formatBytes(8192n)).toBe("8.0 KB");
    expect(formatBytes(1_572_864n)).toBe("1.5 MB");
  });

  it("stops at the largest unit it knows", () => {
    expect(formatBytes(1024 ** 5)).toBe("1024.0 TB");
  });
});
