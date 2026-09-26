import { Code, ConnectError } from "@connectrpc/connect";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApp } from "vue";
import { i18n } from "@/locales";
import { useErrorHandler, useErrorMessage } from "./useErrorHandler";

const mocks = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
  info: vi.fn(),
}));

vi.mock("@/lib/notify", () => ({ notify: mocks }));

/** The composables call `useI18n()`, so they have to run inside a setup. */
function inSetup<T>(composable: () => T): T {
  let result!: T;
  const app = createApp({
    setup() {
      result = composable();
      return () => null;
    },
  });
  app.use(i18n);
  app.mount(document.createElement("div"));
  return result;
}

describe("useErrorMessage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    i18n.global.locale.value = "en-US";
  });

  it("keeps the server's wording for a code the caller can act on", () => {
    const { formatError } = inSetup(useErrorMessage);
    const error = new ConnectError("title is required", Code.InvalidArgument);

    expect(formatError(error)).toBe("title is required");
  });

  it("replaces a code the caller cannot act on with one sentence", () => {
    const { formatError } = inSetup(useErrorMessage);
    // The raw text of an internal failure is developer noise, not a message.
    const error = new ConnectError("[internal] rpc error: boom", Code.Internal);

    expect(formatError(error)).toBe("The server hit an internal error.");
  });

  it("names the failed operation when the error carries no Connect code", () => {
    const { formatError } = inSetup(useErrorMessage);

    expect(
      formatError(new Error("offline"), "instanceManagement.deleteError")
    ).toBe("Failed to delete instance");
  });

  it("falls back to the error's own message, then to a generic sentence", () => {
    const { formatError } = inSetup(useErrorMessage);

    expect(formatError(new Error("offline"))).toBe("offline");
    expect(formatError("plain string")).toBe("plain string");
    expect(formatError(42)).toBe(
      "An unknown error occurred. Please try again later"
    );
  });
});

describe("useErrorHandler", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    i18n.global.locale.value = "en-US";
  });

  it("toasts a failure and returns the sentence an inline alert shows", () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});
    const { handleError } = inSetup(useErrorHandler);

    const message = handleError(
      new ConnectError("connection refused", Code.Unavailable),
      "instanceManagement.testConnectionError"
    );

    expect(message).toBe(
      "The service is temporarily unavailable, please retry."
    );
    expect(mocks.error).toHaveBeenCalledWith(message);
    expect(consoleError).toHaveBeenCalled();
    consoleError.mockRestore();
  });

  it("routes the convenience helpers through the matching severity", () => {
    const { showSuccess, showError, showWarning, showInfo } =
      inSetup(useErrorHandler);

    showSuccess("common.save");
    showError("error.notFound");
    showWarning("error.unavailable");
    showInfo("common.loading");

    expect(mocks.success).toHaveBeenCalledWith("Save");
    expect(mocks.error).toHaveBeenCalledWith("That resource no longer exists.");
    expect(mocks.warning).toHaveBeenCalledWith(
      "The service is temporarily unavailable, please retry."
    );
    expect(mocks.info).toHaveBeenCalledWith("Loading...");
  });
});
