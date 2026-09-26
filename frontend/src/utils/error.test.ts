import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import {
  errorCode,
  errorMessageKey,
  errorText,
  extractErrorMessage,
  prefersServerMessage,
} from "./error";

/** A stand-in for vue-i18n: the key is the translation. */
const t = (key: string) => `[${key}]`;

describe("extractErrorMessage", () => {
  it("returns the message of a ConnectError without the code prefix", () => {
    // ConnectError.message carries a "[code] " prefix; this text is shown to
    // users, so the server's own wording is returned instead.
    expect(extractErrorMessage(new ConnectError("instance not found"))).toBe(
      "instance not found"
    );
    expect(extractErrorMessage(new ConnectError("nope", Code.Internal))).toBe(
      "nope"
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

describe("errorCode", () => {
  it("reads the code of a ConnectError", () => {
    expect(errorCode(new ConnectError("nope", Code.NotFound))).toBe(
      Code.NotFound
    );
  });

  it("is undefined for anything else", () => {
    expect(errorCode(new Error("boom"))).toBeUndefined();
    expect(errorCode("boom")).toBeUndefined();
  });
});

describe("errorMessageKey", () => {
  it("names a sentence for every code", () => {
    expect(errorMessageKey(Code.PermissionDenied)).toBe(
      "error.permissionDenied"
    );
    expect(errorMessageKey(Code.Unavailable)).toBe("error.unavailable");
    expect(errorMessageKey(Code.Unknown)).toBe("error.unknown");
  });
});

describe("prefersServerMessage", () => {
  it("keeps the server's wording where it names something actionable", () => {
    for (const code of [
      Code.InvalidArgument,
      Code.FailedPrecondition,
      Code.NotFound,
      Code.AlreadyExists,
      Code.PermissionDenied,
      // A wrong password on Login is also Unauthenticated.
      Code.Unauthenticated,
    ]) {
      expect(prefersServerMessage(code), `code ${code}`).toBe(true);
    }
  });

  it("prefers the code's sentence for infrastructure failures", () => {
    for (const code of [
      Code.Internal,
      Code.Unavailable,
      Code.Unknown,
      Code.DeadlineExceeded,
      Code.ResourceExhausted,
    ]) {
      expect(prefersServerMessage(code), `code ${code}`).toBe(false);
    }
  });
});

describe("errorText", () => {
  it("shows the server's reason for a code the caller can act on", () => {
    expect(
      errorText(
        new ConnectError(
          "connection refused by db:3306",
          Code.FailedPrecondition
        ),
        t,
        "instanceManagement.testConnectionError"
      )
    ).toBe("connection refused by db:3306");
  });

  it("shows the code's sentence when the code is the whole story", () => {
    expect(
      errorText(
        new ConnectError("rpc error: code = Internal", Code.Internal),
        t,
        "instanceManagement.deleteError"
      )
    ).toBe("[error.internal]");
  });

  it("uses the code's sentence when a specific code carries no message", () => {
    expect(errorText(new ConnectError("", Code.NotFound), t)).toBe(
      "[error.notFound]"
    );
  });

  it("names the failed operation for a non-Connect error", () => {
    expect(errorText(new Error("boom"), t, "instanceDetail.updateError")).toBe(
      "[instanceDetail.updateError]"
    );
  });

  it("shows a non-Connect error's own message when there is no fallback", () => {
    expect(errorText(new Error("boom"), t)).toBe("boom");
  });

  it("ends at the unknown sentence for an opaque error", () => {
    expect(errorText({}, t)).toBe("[error.unknown]");
    expect(errorText(undefined, t, "login.loginFailed")).toBe(
      "[login.loginFailed]"
    );
  });
});
