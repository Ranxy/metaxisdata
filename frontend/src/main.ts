import { createApp } from "vue";
import App from "./App.vue";
import { refresh } from "./api/auth";
import { setSessionRefresher, setUnauthenticatedHandler } from "./api/session";
import { i18n } from "./locales";
import router from "./router";
import { pinia } from "./store";
import { initTheme } from "./store/modules/app";
import { useAuthStore } from "./store/modules/auth";
import "./assets/styles/main.css";
import "markstream-vue/index.css";
// vue-sonner's ESM build does not inject its own styles, so without this the
// Toaster renders unstyled and unpositioned and every toast is invisible.
import "vue-sonner/style.css";

/**
 * A cookie that expires mid-session has to end the session everywhere at once:
 * clear the cached user and send the next render to the login form with a notice.
 * The redirect target is kept so the user lands back where they were.
 */
function handleSessionExpired() {
  useAuthStore().handleUnauthenticated();

  const current = router.currentRoute.value;
  if (current.name === "Login") {
    // Already on the login form (e.g. a failed password reset): a pushed
    // navigation would only duplicate the entry.
    return;
  }
  void router.push({
    name: "Login",
    query: { expired: "1", redirect: current.fullPath },
  });
}

async function bootstrap() {
  const app = createApp(App);

  app.use(pinia);
  // Resolve the persisted theme before the first paint so the app never
  // flashes the wrong palette.
  initTheme();
  app.use(router);
  app.use(i18n);

  setUnauthenticatedHandler(handleSessionExpired);
  // A refused request is answered with a renewal first; the handler above runs
  // only when the refresh cookie is gone too.
  setSessionRefresher(refresh);

  // Wait for the initial navigation (and any redirects in guards) to finish
  // before mounting, to avoid flashing protected layouts/pages.
  await router.isReady();

  app.mount("#app");
}

void bootstrap();
