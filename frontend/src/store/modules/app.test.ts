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

  it("defaults to every section expanded", () => {
    expect(freshStore().collapsedSections).toEqual([]);
  });

  it("persists a collapsed section", () => {
    const store = freshStore();
    store.setSectionCollapsed("settings", true);

    expect(store.collapsedSections).toEqual(["settings"]);
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}")).toMatchObject(
      {
        collapsedSections: ["settings"],
      }
    );
  });

  it("expands a section again without duplicating entries", () => {
    const store = freshStore();
    store.setSectionCollapsed("settings", true);
    store.setSectionCollapsed("openlineage", true);
    store.setSectionCollapsed("settings", false);

    expect(store.collapsedSections).toEqual(["openlineage"]);
  });

  it("restores collapsed sections when the store is recreated", () => {
    freshStore().setSectionCollapsed("datasource", true);

    expect(freshStore().collapsedSections).toEqual(["datasource"]);
  });

  it("ignores a stored payload whose collapsed sections are malformed", () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ collapsedSections: "datasource" })
    );

    expect(freshStore().collapsedSections).toEqual([]);
  });
});
