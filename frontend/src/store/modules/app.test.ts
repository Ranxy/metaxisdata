import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { useAppStore } from "./app";

const STORAGE_KEY = "metaxisdata-app-state";

function freshStore() {
  setActivePinia(createPinia());
  return useAppStore();
}

describe("app store sidebar sections", () => {
  beforeEach(() => {
    localStorage.clear();
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
