import { ConnectError } from "@connectrpc/connect";

/**
 * Extract the error message from various error types
 */
export function extractErrorMessage(error: unknown): string {
  if (error instanceof ConnectError) {
    return error.message;
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
