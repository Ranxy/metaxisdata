import { createApp } from "vue";
import App from "./App.vue";
import { i18n } from "./locales";
import router from "./router";
import { pinia } from "./store";
import "./assets/styles/main.css";
import "markstream-vue/index.css";

async function bootstrap() {
  const app = createApp(App);

  app.use(pinia);
  app.use(router);
  app.use(i18n);

  // Wait for the initial navigation (and any redirects in guards) to finish
  // before mounting, to avoid flashing protected layouts/pages.
  await router.isReady();

  app.mount("#app");
}

void bootstrap();
