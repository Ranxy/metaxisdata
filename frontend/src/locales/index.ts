import { createI18n } from "vue-i18n";
import zhCN from "./zh-CN.json";
import enUS from "./en-US.json";

export type MessageSchema = typeof zhCN;

const STORAGE_KEY = "metaxisdata-app-state";
const DEFAULT_LOCALE = "en-US";

function getStoredLocale(): "zh-CN" | "en-US" {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved) {
      const state = JSON.parse(saved);
      if (state.locale === "zh-CN" || state.locale === "en-US") {
        return state.locale;
      }
    }
  } catch {
    // ignore parse errors
  }
  return DEFAULT_LOCALE;
}

export const i18n = createI18n<[MessageSchema], "zh-CN" | "en-US">({
  legacy: false,
  locale: getStoredLocale(),
  fallbackLocale: "en-US",
  messages: {
    "zh-CN": zhCN,
    "en-US": enUS,
  },
});

export function setLocale(locale: "zh-CN" | "en-US") {
  i18n.global.locale = locale;
}
