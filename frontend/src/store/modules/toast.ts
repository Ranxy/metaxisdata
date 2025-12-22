import { defineStore } from "pinia";

export type ToastType = "success" | "error" | "warning" | "info";

export interface Toast {
  id: string;
  type: ToastType;
  message: string;
  title?: string;
  duration?: number;
}

interface ToastState {
  toasts: Toast[];
  counter: number;
}

const DEFAULT_DURATION = 5000;
const ERROR_DURATION = 8000;

export const useToastStore = defineStore("toast", {
  state: (): ToastState => ({
    toasts: [],
    counter: 0,
  }),

  actions: {
    addToast(toast: Omit<Toast, "id">) {
      const id = `toast-${++this.counter}`;
      const duration =
        toast.duration ??
        (toast.type === "error" ? ERROR_DURATION : DEFAULT_DURATION);

      this.toasts.push({
        ...toast,
        id,
        duration,
      });

      // Auto remove after duration
      if (duration > 0) {
        setTimeout(() => {
          this.removeToast(id);
        }, duration);
      }
    },

    removeToast(id: string) {
      const index = this.toasts.findIndex((t) => t.id === id);
      if (index > -1) {
        this.toasts.splice(index, 1);
      }
    },

    clearAll() {
      this.toasts = [];
    },

    // Convenience methods
    success(message: string, title?: string) {
      this.addToast({ type: "success", message, title });
    },

    error(message: string, title?: string) {
      this.addToast({ type: "error", message, title });
    },

    warning(message: string, title?: string) {
      this.addToast({ type: "warning", message, title });
    },

    info(message: string, title?: string) {
      this.addToast({ type: "info", message, title });
    },
  },
});
