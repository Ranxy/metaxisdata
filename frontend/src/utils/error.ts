import { ConnectError } from "@connectrpc/connect";

/**
 * Extract the error message from various error types
 */
export function extractErrorMessage(error: unknown): string {
  if (error instanceof ConnectError) {
    // `message` is prefixed with the Connect code (`[invalid_argument] ...`),
    // which is developer noise wherever this text reaches a user — a toast, an
    // inline alert. `rawMessage` is the server's own wording.
    return error.rawMessage || error.message;
  }
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === "string") {
    return error;
  }
  if (error && typeof error === "object" && "message" in error) {
    return String((error as { message: unknown }).message);
  }
  return "";
}
