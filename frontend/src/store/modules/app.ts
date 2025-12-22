import { defineStore } from "pinia";

interface AppState {
  sidebarCollapsed: boolean;
  locale: string;
  theme: "light" | "dark";
}

const STORAGE_KEY = "metaxisdata-app-state";

function loadState(): Partial<AppState> {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved) {
      return JSON.parse(saved);
    }
  } catch {
    // ignore parse errors
  }
  return {};
}

function saveState(state: AppState) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  } catch {
    // ignore storage errors
  }
}

export const useAppStore = defineStore("app", {
  state: (): AppState => ({
    sidebarCollapsed: false,
    locale: "zh-CN",
    theme: "light",
    ...loadState(),
  }),

  actions: {
    toggleSidebar() {
      this.sidebarCollapsed = !this.sidebarCollapsed;
      saveState(this.$state);
    },

    setLocale(locale: string) {
      this.locale = locale;
      saveState(this.$state);
    },

    setTheme(theme: "light" | "dark") {
      this.theme = theme;
      saveState(this.$state);
    },
  },
});
