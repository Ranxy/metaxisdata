import { createApp } from "vue";
import App from "./App.vue";
import { i18n } from "./locales";
import router from "./router";
import { pinia } from "./store";
import { initTheme } from "./store/modules/app";
import "./assets/styles/main.css";
import "markstream-vue/index.css";
// vue-sonner's ESM build does not inject its own styles, so without this the
// Toaster renders unstyled and unpositioned and every toast is invisible.
import "vue-sonner/style.css";

async function bootstrap() {
  const app = createApp(App);

  app.use(pinia);
  // Resolve the persisted theme before the first paint so the app never
  // flashes the wrong palette.
  initTheme();
  app.use(router);
  app.use(i18n);

  // Wait for the initial navigation (and any redirects in guards) to finish
  // before mounting, to avoid flashing protected layouts/pages.
  await router.isReady();

  app.mount("#app");
}

void bootstrap();
