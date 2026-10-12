import { describe, expect, it } from "vitest";
import { resolveSignInTarget, sanitizeRedirect } from "@/lib/signInRedirect";

describe("sanitizeRedirect", () => {
  it("accepts a same-origin absolute path", () => {
    expect(sanitizeRedirect("/instances/3")).toBe("/instances/3");
  });

  it("keeps a query string", () => {
    expect(sanitizeRedirect("/openlineage/tasks/7?tab=events")).toBe(
      "/openlineage/tasks/7?tab=events"
    );
  });

  it.each([
    ["an absolute URL", "https://evil.example.com/"],
    ["a protocol-relative path", "//evil.example.com/"],
    ["a backslash smuggled past the prefix check", "/\\evil.example.com/"],
    ["a relative path", "instances"],
    ["an empty value", ""],
    ["nothing", undefined],
    ["nothing at all", null],
  ])("rejects %s", (_label, value) => {
    expect(sanitizeRedirect(value)).toBeUndefined();
  });
});

describe("resolveSignInTarget", () => {
  const spaOnly = (path: string) => path.startsWith("/settings");

  it("falls back to home when there is no redirect", () => {
    expect(resolveSignInTarget(undefined, spaOnly)).toEqual({ kind: "home" });
    expect(resolveSignInTarget("https://evil.example.com/", spaOnly)).toEqual({
      kind: "home",
    });
  });

  it("navigates inside the app for a route the router owns", () => {
    expect(resolveSignInTarget("/settings/users", spaOnly)).toEqual({
      kind: "spa",
      path: "/settings/users",
    });
  });

  // The MCP client's browser sign-in: `/oauth/authorize` is served by the server,
  // and the router's catch-all would otherwise swallow it and show a 404.
  it("loads a path the router does not own from the server", () => {
    expect(
      resolveSignInTarget("/oauth/authorize?client_id=abc&state=xyz", spaOnly)
    ).toEqual({
      kind: "server",
      path: "/oauth/authorize?client_id=abc&state=xyz",
    });
  });
});
