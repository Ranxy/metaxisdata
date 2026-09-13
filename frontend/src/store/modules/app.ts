import { defineStore } from "pinia";

interface AppState {
  sidebarCollapsed: boolean;
  /** Keys of the sidebar sections the user collapsed. */
  collapsedSections: string[];
  locale: string;
  theme: "light" | "dark";
}

const STORAGE_KEY = "metaxisdata-app-state";

function loadState(): Partial<AppState> {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved) {
      const parsed = JSON.parse(saved) as Partial<AppState>;
      // A hand-edited or stale payload must not break the sidebar.
      if (!Array.isArray(parsed.collapsedSections)) {
        delete parsed.collapsedSections;
      }
      return parsed;
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
    collapsedSections: [],
    locale: "zh-CN",
    theme: "light",
    ...loadState(),
  }),

  actions: {
    toggleSidebar() {
      this.sidebarCollapsed = !this.sidebarCollapsed;
      saveState(this.$state);
    },

    setSectionCollapsed(key: string, collapsed: boolean) {
      this.collapsedSections = collapsed
        ? [...new Set([...this.collapsedSections, key])]
        : this.collapsedSections.filter((section) => section !== key);
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
