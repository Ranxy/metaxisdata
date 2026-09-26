import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { defineComponent, h, nextTick } from "vue";
import { useI18n } from "vue-i18n";
import { i18n } from "@/locales";
import { useAppStore } from "./app";

const STORAGE_KEY = "metaxisdata-app-state";

function freshStore() {
  setActivePinia(createPinia());
  return useAppStore();
}

describe("app store sidebar sections", () => {
  beforeEach(() => {
    localStorage.clear();
    i18n.global.locale.value = "en-US";
  });

  it("defaults to every section collapsed", () => {
    expect(freshStore().collapsedSections).toEqual([
      "datasource",
      "openlineage",
    ]);
  });

  it("persists a collapsed section", () => {
    const store = freshStore();
    store.setSectionCollapsed("settings", true);

    expect(store.collapsedSections).toContain("settings");
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}")).toMatchObject(
      {
        collapsedSections: ["datasource", "openlineage", "settings"],
      }
    );
  });

  it("expands a section again without duplicating entries", () => {
    const store = freshStore();
    store.setSectionCollapsed("settings", true);
    store.setSectionCollapsed("openlineage", true);
    store.setSectionCollapsed("settings", false);

    // `openlineage` is collapsed by default, so setting it again must not add a
    // duplicate entry.
    expect(store.collapsedSections).toEqual(["datasource", "openlineage"]);
  });

  it("restores collapsed sections when the store is recreated", () => {
    freshStore().setSectionCollapsed("datasource", false);
    freshStore().setSectionCollapsed("settings", true);

    expect(freshStore().collapsedSections).toEqual(["openlineage", "settings"]);
  });

  it("ignores a stored payload whose collapsed sections are malformed", () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ collapsedSections: "datasource" })
    );

    expect(freshStore().collapsedSections).toEqual([
      "datasource",
      "openlineage",
    ]);
  });

  it("never persists transient navigation state", () => {
    const store = freshStore();
    store.setMobileNavOpen(true);

    expect(
      JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}")
    ).not.toHaveProperty("mobileNavOpen");
    expect(freshStore().mobileNavOpen).toBe(false);
  });
});

describe("app store locale", () => {
  beforeEach(() => {
    localStorage.clear();
    i18n.global.locale.value = "en-US";
  });

  it("defaults to the locale vue-i18n bootstraps with", () => {
    expect(freshStore().locale).toBe("en-US");
    expect(i18n.global.locale.value).toBe("en-US");
  });

  it("switches the store, the persistence and vue-i18n together", () => {
    const store = freshStore();
    store.setLocale("zh-CN");

    expect(store.locale).toBe("zh-CN");
    expect(i18n.global.locale.value).toBe("zh-CN");
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}").locale).toBe(
      "zh-CN"
    );
  });

  it("ignores a stored locale the app does not ship", () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ locale: "fr-FR" }));

    expect(freshStore().locale).toBe("en-US");
  });

  it("is the locale a component reads through useI18n", async () => {
    // Proves the store acts on the same composer `useI18n()` hands to pages,
    // which is what used to be duplicated next to every switcher.
    const Probe = defineComponent({
      setup() {
        const { locale } = useI18n();
        return () => h("span", locale.value);
      },
    });
    const wrapper = mount(Probe, { global: { plugins: [i18n] } });
    expect(wrapper.text()).toBe("en-US");

    freshStore().setLocale("zh-CN");
    await nextTick();

    expect(wrapper.text()).toBe("zh-CN");
  });
});
