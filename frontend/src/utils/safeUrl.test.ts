import { describe, expect, it } from "vitest";
import { safeExternalUrl } from "./safeUrl";

describe("safeExternalUrl", () => {
  it("keeps absolute http and https URLs, normalized", () => {
    expect(safeExternalUrl("https://airflow.example.com/dags/x/runs/1")).toBe(
      "https://airflow.example.com/dags/x/runs/1"
    );
    expect(safeExternalUrl("  http://airflow.example.com/dags/x  ")).toBe(
      "http://airflow.example.com/dags/x"
    );
    expect(safeExternalUrl("HTTPS://airflow.example.com/dags/x")).toBe(
      "https://airflow.example.com/dags/x"
    );
    expect(safeExternalUrl("http://user:pass@airflow.example.com/dags/x")).toBe(
      "http://user:pass@airflow.example.com/dags/x"
    );
  });

  it.each([
    ["javascript:alert(document.cookie)", "javascript scheme"],
    ["JaVaScRiPt:alert(1)", "mixed-case javascript scheme"],
    ["data:text/html,<script>alert(1)</script>", "data scheme"],
    ["file:///etc/passwd", "file scheme"],
    ["//evil.example.com/dags/x/runs/1", "protocol-relative URL"],
    ["/dags/x/runs/1", "relative path"],
    ["airflow.example.com/dags/x", "scheme-less address"],
    ["https:", "scheme without host"],
    ["http://:8080/dags/x", "port without host"],
    ["not a url", "not a URL"],
    ["   ", "blank"],
    ["", "empty"],
  ])("refuses %s (%s)", (value) => {
    expect(safeExternalUrl(value)).toBe("");
  });

  it("refuses a missing value", () => {
    expect(safeExternalUrl(undefined)).toBe("");
    expect(safeExternalUrl(null)).toBe("");
  });
});
