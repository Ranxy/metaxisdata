import { toast } from "vue-sonner";

/**
 * The app's toast entry point.
 *
 * It wraps vue-sonner only to keep the duration policy in one place — an error
 * needs longer on screen than a confirmation. There is deliberately no store in
 * between: vue-sonner already owns the visible queue, and the Pinia store plus
 * deep watch this replaced could drop a toast (the watcher saw only the last
 * array element, and the store's own `setTimeout` removal ran after the entry had
 * already been removed).
 */
const DEFAULT_DURATION = 5000;
const ERROR_DURATION = 8000;

export const notify = {
  success(message: string) {
    toast.success(message, { duration: DEFAULT_DURATION });
  },
  error(message: string) {
    toast.error(message, { duration: ERROR_DURATION });
  },
  warning(message: string) {
    toast.warning(message, { duration: DEFAULT_DURATION });
  },
  info(message: string) {
    toast.info(message, { duration: DEFAULT_DURATION });
  },
};
