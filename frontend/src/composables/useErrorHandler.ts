import { useI18n } from "vue-i18n";
import { useToastStore } from "@/store/modules/toast";
import { extractErrorMessage } from "@/utils/error";

/**
 * Composable for handling errors with toast notifications
 */
export function useErrorHandler() {
  const { t } = useI18n();
  const toast = useToastStore();

  /**
   * Handle an error and show a toast notification
   * @param error - The error to handle
   * @param fallbackMessage - Fallback i18n key if no error message available
   */
  function handleError(error: unknown, fallbackMessage?: string) {
    console.error("Error:", error);
    const message =
      extractErrorMessage(error) ||
      (fallbackMessage ? t(fallbackMessage) : t("error.unknown"));
    toast.error(message);
  }

  /**
   * Show a success toast
   * @param messageKey - i18n key for the message
   */
  function showSuccess(messageKey: string) {
    toast.success(t(messageKey));
  }

  /**
   * Show an error toast
   * @param messageKey - i18n key for the message
   */
  function showError(messageKey: string) {
    toast.error(t(messageKey));
  }

  /**
   * Show a warning toast
   * @param messageKey - i18n key for the message
   */
  function showWarning(messageKey: string) {
    toast.warning(t(messageKey));
  }

  /**
   * Show an info toast
   * @param messageKey - i18n key for the message
   */
  function showInfo(messageKey: string) {
    toast.info(t(messageKey));
  }

  return {
    handleError,
    showSuccess,
    showError,
    showWarning,
    showInfo,
  };
}
