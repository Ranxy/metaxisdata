import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import { extractErrorMessage } from "./error";

describe("extractErrorMessage", () => {
  it("returns the message of a ConnectError", () => {
    // ConnectError.message carries a "[code] " prefix; callers display it as-is.
    expect(extractErrorMessage(new ConnectError("instance not found"))).toBe(
      "[unknown] instance not found"
    );
    expect(extractErrorMessage(new ConnectError("nope", Code.Internal))).toBe(
      "[internal] nope"
    );
  });

  it("returns the message of a plain Error", () => {
    expect(extractErrorMessage(new Error("boom"))).toBe("boom");
  });

  it("returns a string error unchanged", () => {
    expect(extractErrorMessage("already a string")).toBe("already a string");
  });

  it("reads message from object-shaped errors", () => {
    expect(extractErrorMessage({ message: "from an object" })).toBe(
      "from an object"
    );
    expect(extractErrorMessage({ message: 42 })).toBe("42");
  });

  it("returns an empty string for unknown shapes", () => {
    expect(extractErrorMessage(undefined)).toBe("");
    expect(extractErrorMessage(null)).toBe("");
    expect(extractErrorMessage(7)).toBe("");
    expect(extractErrorMessage({})).toBe("");
  });
});
