import { defineStore } from "pinia";

type Theme = "light" | "dark" | "system";

interface AppState {
  sidebarCollapsed: boolean;
  /** Keys of the sidebar sections the user collapsed. */
  collapsedSections: string[];
  locale: string;
  theme: Theme;
  /** Transient: the below-`lg` navigation drawer. Never persisted. */
  mobileNavOpen: boolean;
}

const STORAGE_KEY = "metaxisdata-app-state";

// Collapsible sections start closed so a fresh workspace shows five sidebar
// rows instead of thirteen. AppSidebar reopens the section that owns the active
// route, so landing on a page never hides where the user is.
const DEFAULT_COLLAPSED_SECTIONS = ["datasource", "openlineage"];

function loadState(): Partial<AppState> {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved) {
      const parsed = JSON.parse(saved) as Partial<AppState>;
      // A hand-edited or stale payload must not break the sidebar.
      if (!Array.isArray(parsed.collapsedSections)) {
        delete parsed.collapsedSections;
      }
      delete parsed.mobileNavOpen;
      return parsed;
    }
  } catch {
    // ignore parse errors
  }
  return {};
}

function saveState(state: AppState) {
  try {
    // Only durable preferences are stored, so transient UI state such as
    // `mobileNavOpen` can never reopen the drawer on the next visit.
    const { sidebarCollapsed, collapsedSections, locale, theme } = state;
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ sidebarCollapsed, collapsedSections, locale, theme })
    );
  } catch {
    // ignore storage errors
  }
}

function prefersDark(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-color-scheme: dark)").matches
  );
}

// The dark tokens live on `html.dark`, so resolving a theme is one class toggle.
function applyTheme(theme: Theme) {
  if (typeof document === "undefined") {
    return;
  }
  const dark = theme === "dark" || (theme === "system" && prefersDark());
  document.documentElement.classList.toggle("dark", dark);
}

/** Applies the persisted theme and keeps "system" in step with the OS. */
export function initTheme() {
  const store = useAppStore();
  applyTheme(store.theme);
  if (
    typeof window === "undefined" ||
    typeof window.matchMedia !== "function"
  ) {
    return;
  }
  window
    .matchMedia("(prefers-color-scheme: dark)")
    .addEventListener("change", () => {
      if (store.theme === "system") {
        applyTheme("system");
      }
    });
}

export const useAppStore = defineStore("app", {
  state: (): AppState => ({
    sidebarCollapsed: false,
    collapsedSections: [...DEFAULT_COLLAPSED_SECTIONS],
    locale: "zh-CN",
    theme: "system",
    mobileNavOpen: false,
    ...loadState(),
  }),

  actions: {
    toggleSidebar() {
      this.sidebarCollapsed = !this.sidebarCollapsed;
      saveState(this.$state);
    },

    setMobileNavOpen(open: boolean) {
      this.mobileNavOpen = open;
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

    setTheme(theme: Theme) {
      this.theme = theme;
      saveState(this.$state);
      applyTheme(theme);
    },
  },
});
