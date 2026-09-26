import { useI18n } from "vue-i18n";
import { notify } from "@/lib/notify";
import { errorText } from "@/utils/error";

/**
 * Turns an error into the sentence a page renders inline — no toast, so a
 * component that only displays an error does not fire one the page already shows
 * in place.
 *
 * `errorText` decides between the server's own wording and a code-to-i18n
 * sentence; pages should reach for it instead of reading `error.message`, so the
 * same failure reads the same everywhere.
 */
export function useErrorMessage() {
  const { t } = useI18n();

  /** The message to render for `error`, in a toast or an inline alert. */
  function formatError(error: unknown, fallbackKey?: string): string {
    return errorText(error, (key) => t(key), fallbackKey);
  }

  return { formatError };
}

/**
 * The toast half of the same entry point: `formatError` for an inline alert next
 * to the toast, `handleError` for a failure that only deserves a toast.
 */
export function useErrorHandler() {
  const { t } = useI18n();
  const { formatError } = useErrorMessage();

  /**
   * Handle an error and show a toast notification.
   * @param error - The error to handle
   * @param fallbackKey - i18n key naming the failed operation, used when the
   *   error carries no Connect code (the text itself comes from `errorText`)
   * @returns the message that was toasted, so a page that also renders it inline
   *   shows the same sentence
   */
  function handleError(error: unknown, fallbackKey?: string): string {
    console.error("Error:", error);
    const message = formatError(error, fallbackKey);
    notify.error(message);
    return message;
  }

  /**
   * Show a success toast
   * @param messageKey - i18n key for the message
   */
  function showSuccess(messageKey: string) {
    notify.success(t(messageKey));
  }

  /**
   * Show an error toast
   * @param messageKey - i18n key for the message
   */
  function showError(messageKey: string) {
    notify.error(t(messageKey));
  }

  /**
   * Show a warning toast
   * @param messageKey - i18n key for the message
   */
  function showWarning(messageKey: string) {
    notify.warning(t(messageKey));
  }

  /**
   * Show an info toast
   * @param messageKey - i18n key for the message
   */
  function showInfo(messageKey: string) {
    notify.info(t(messageKey));
  }

  return {
    formatError,
    handleError,
    showSuccess,
    showError,
    showWarning,
    showInfo,
  };
}
