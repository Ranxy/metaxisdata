import { Code, ConnectError } from "@connectrpc/connect";
import type { MessageSchema } from "@/locales";
import type { Translate } from "./i18n";

/** Every `error.*` key the locale files define; a typo here fails the build. */
type ErrorMessageKey = LeafKeys<MessageSchema["error"], "error.">;

type LeafKeys<T, Prefix extends string> = {
  [K in keyof T & string]: T[K] extends string
    ? `${Prefix}${K}`
    : LeafKeys<T[K], `${Prefix}${K}.`>;
}[keyof T & string];

/**
 * Extract the error message from various error types
 */
export function extractErrorMessage(error: unknown): string {
  if (error instanceof ConnectError) {
    // `message` is `[code] <raw>` and `rawMessage` is the server's own wording.
    // Only the latter can be shown: a code prefix is developer noise, and when
    // the server sent no message at all the code's own sentence is the better
    // answer (see `errorText`).
    return error.rawMessage;
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

/** The Connect status code of an error, when it carries one. */
export function errorCode(error: unknown): Code | undefined {
  return error instanceof ConnectError ? error.code : undefined;
}

/**
 * Whether `error` is the refusal of a request the caller aborted — a replaced
 * stream's AbortSignal or a superseded fetch. Callers drop these instead of
 * reporting them, because nothing the user did failed.
 */
export function isAborted(error: unknown): boolean {
  return (
    errorCode(error) === Code.Canceled ||
    (error instanceof DOMException && error.name === "AbortError")
  );
}

/**
 * Codes whose server wording is the only place the actual reason appears — the
 * name of a rejected field, the driver's own connection error, or (for a login)
 * which credential was wrong. Those keep the server's text.
 */
const SPECIFIC_CODES: ReadonlySet<Code> = new Set([
  Code.InvalidArgument,
  Code.FailedPrecondition,
  Code.NotFound,
  Code.AlreadyExists,
  Code.PermissionDenied,
  Code.Unauthenticated,
]);

/**
 * Every Connect code maps to one sentence, so a toast never carries raw
 * `[internal] rpc error: ...` text. Exhaustive twice over: adding a code fails
 * the type check here, and a key that the locale files do not define fails it via
 * {@link ErrorMessageKey}.
 */
const CODE_MESSAGE_KEYS: Record<Code, ErrorMessageKey> = {
  [Code.Canceled]: "error.canceled",
  [Code.Unknown]: "error.unknown",
  [Code.InvalidArgument]: "error.invalidArgument",
  [Code.DeadlineExceeded]: "error.deadlineExceeded",
  [Code.NotFound]: "error.notFound",
  [Code.AlreadyExists]: "error.alreadyExists",
  [Code.PermissionDenied]: "error.permissionDenied",
  [Code.ResourceExhausted]: "error.resourceExhausted",
  [Code.FailedPrecondition]: "error.failedPrecondition",
  [Code.Aborted]: "error.aborted",
  [Code.OutOfRange]: "error.outOfRange",
  [Code.Unimplemented]: "error.unimplemented",
  [Code.Internal]: "error.internal",
  [Code.Unavailable]: "error.unavailable",
  [Code.DataLoss]: "error.dataLoss",
  [Code.Unauthenticated]: "error.sessionExpired",
};

/** The i18n key that describes a Connect code on its own. */
export function errorMessageKey(code: Code): ErrorMessageKey {
  return CODE_MESSAGE_KEYS[code];
}

/** Whether the server's own wording is more useful than the code's sentence. */
export function prefersServerMessage(code: Code): boolean {
  return SPECIFIC_CODES.has(code);
}

/**
 * Every Connect code's wire name. A batch call reports each item's failure as
 * the `Error()` string of its own error — `aborted: ...`, `not_found: ...` — and
 * the client has to recover the code from that name to say what happened.
 */
const CODE_BY_NAME: Readonly<Record<string, Code>> = {
  canceled: Code.Canceled,
  unknown: Code.Unknown,
  invalid_argument: Code.InvalidArgument,
  deadline_exceeded: Code.DeadlineExceeded,
  not_found: Code.NotFound,
  already_exists: Code.AlreadyExists,
  permission_denied: Code.PermissionDenied,
  resource_exhausted: Code.ResourceExhausted,
  failed_precondition: Code.FailedPrecondition,
  aborted: Code.Aborted,
  out_of_range: Code.OutOfRange,
  unimplemented: Code.Unimplemented,
  internal: Code.Internal,
  unavailable: Code.Unavailable,
  data_loss: Code.DataLoss,
  unauthenticated: Code.Unauthenticated,
};

/** The Connect code a wire name (e.g. `aborted`) names, when it names one. */
export function codeFromName(name: string): Code | undefined {
  return CODE_BY_NAME[name.trim().toLowerCase()];
}

/** One batch item's failure, split into its code and the server's wording. */
export interface BatchItemError {
  /** The code the string's prefix named, or undefined when it named none. */
  code?: Code;
  /** The message with the code prefix removed. */
  message: string;
}

/**
 * Split one batch result's error string, which the server serializes as
 * `code: message`. A caller can then render a sentence that names the remedy
 * instead of showing the wire code, and still present the server's wording for
 * codes that describe something the caller can fix.
 */
export function parseBatchItemError(error: string): BatchItemError {
  const text = error.trim();
  const separator = text.indexOf(":");
  if (separator < 0) {
    return { message: text };
  }
  const code = codeFromName(text.slice(0, separator));
  if (code === undefined) {
    // The first token is not a code name: keep the string whole rather than
    // silently dropping the part before its first colon.
    return { message: text };
  }
  return { code, message: text.slice(separator + 1).trim() };
}

/**
 * The text to put in front of a user for `error`.
 *
 * A Connect error is described by its code (see {@link CODE_MESSAGE_KEYS}), or by
 * the server's wording when the code names something the caller can fix. Only a
 * non-Connect error falls through to the caller's fallback key, which names the
 * operation that failed — the caller's context is more useful there than a
 * generic sentence.
 */
export function errorText(
  error: unknown,
  t: Translate,
  fallbackKey?: string
): string {
  const code = errorCode(error);
  const raw = extractErrorMessage(error);

  if (code !== undefined) {
    if (prefersServerMessage(code) && raw) {
      return raw;
    }
    return t(errorMessageKey(code));
  }
  if (fallbackKey) {
    return t(fallbackKey);
  }
  return raw || t("error.unknown");
}
