import { createI18n } from "vue-i18n";
import zhCN from "./zh-CN.json";
import enUS from "./en-US.json";

export type MessageSchema = typeof zhCN;
export type AppLocale = "zh-CN" | "en-US";

const STORAGE_KEY = "metaxisdata-app-state";

/** The app store owns the locale; this is only its initial value. */
export const DEFAULT_LOCALE: AppLocale = "en-US";

export function isAppLocale(value: unknown): value is AppLocale {
  return value === "zh-CN" || value === "en-US";
}

// Read before Pinia is installed so the first paint is already translated.
function getStoredLocale(): AppLocale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved) {
      const state = JSON.parse(saved) as { locale?: unknown };
      if (isAppLocale(state.locale)) {
        return state.locale;
      }
    }
  } catch {
    // ignore parse errors
  }
  return DEFAULT_LOCALE;
}

// `false` is stated explicitly: without it, TypeScript picks the legacy shape of
// the factory and types `i18n.global.locale` as a plain string.
export const i18n = createI18n<[MessageSchema], AppLocale, false>({
  legacy: false,
  locale: getStoredLocale(),
  fallbackLocale: DEFAULT_LOCALE,
  messages: {
    "zh-CN": zhCN,
    "en-US": enUS,
  },
});

/** Applies a locale to vue-i18n; called by the app store when the user switches. */
export function applyLocale(locale: AppLocale) {
  i18n.global.locale.value = locale;
}
