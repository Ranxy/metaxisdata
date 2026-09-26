import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { EnvironmentSchema } from "@/types/proto-es/v1/environment_service_pb";
import { useEnvironmentStore } from "./environment";

const mocks = vi.hoisted(() => ({
  listAllEnvironments: vi.fn(),
  createEnvironment: vi.fn(),
  updateEnvironment: vi.fn(),
  deleteEnvironment: vi.fn(),
}));

vi.mock("@/api/environment", async (importOriginal) => ({
  // `environmentId` stays real: the store's own title fallback is part of what
  // these tests check.
  ...(await importOriginal<typeof import("@/api/environment")>()),
  listAllEnvironments: mocks.listAllEnvironments,
  createEnvironment: mocks.createEnvironment,
  updateEnvironment: mocks.updateEnvironment,
  deleteEnvironment: mocks.deleteEnvironment,
}));

function environment(id: string, title = "") {
  return create(EnvironmentSchema, {
    name: `environments/${id}`,
    title,
    color: "blue",
  });
}

const ESTATE = [environment("prod", "Production"), environment("dev")];

describe("environment store", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    mocks.listAllEnvironments.mockResolvedValue(ESTATE);
  });

  it("loads every page once and then reuses the cache", async () => {
    const store = useEnvironmentStore();

    await store.fetch();
    await store.fetch();
    await store.ensureLoaded();

    expect(mocks.listAllEnvironments).toHaveBeenCalledTimes(1);
    expect(store.environments).toHaveLength(2);
    expect(store.loaded).toBe(true);
    expect(store.loading).toBe(false);
  });

  it("refetches when asked to force", async () => {
    const store = useEnvironmentStore();

    await store.fetch();
    await store.fetch(true);

    expect(mocks.listAllEnvironments).toHaveBeenCalledTimes(2);
  });

  it("does not start a second request while one is in flight", async () => {
    const store = useEnvironmentStore();

    const first = store.fetch();
    await store.fetch();
    await first;

    expect(mocks.listAllEnvironments).toHaveBeenCalledTimes(1);
  });

  it("clears the loading flag and keeps the cache when a fetch fails", async () => {
    const store = useEnvironmentStore();
    await store.fetch();
    mocks.listAllEnvironments.mockRejectedValue(new Error("offline"));

    await expect(store.fetch(true)).rejects.toThrow("offline");

    expect(store.loading).toBe(false);
    expect(store.environments).toHaveLength(2);
  });

  it("labels an option with its title, or its raw id when it has none", async () => {
    const store = useEnvironmentStore();
    await store.fetch();

    expect(store.options.map((option) => [option.value, option.label])).toEqual(
      [
        ["environments/prod", "Production"],
        ["environments/dev", "dev"],
      ]
    );
  });

  it("resolves a title by resource name, falling back to the id", async () => {
    const store = useEnvironmentStore();
    await store.fetch();

    expect(store.titleOf("environments/prod")).toBe("Production");
    // Unknown values fall back to the id so a legacy row never renders empty.
    expect(store.titleOf("environments/gone")).toBe("gone");
  });

  it("looks an environment up by resource name", async () => {
    const store = useEnvironmentStore();
    await store.fetch();

    expect(store.byName("environments/dev")?.title).toBe("");
    expect(store.byName("environments/gone")).toBeUndefined();
  });

  it("refreshes the cache after every write", async () => {
    const store = useEnvironmentStore();
    await store.fetch();
    mocks.listAllEnvironments.mockClear();
    mocks.createEnvironment.mockResolvedValue(environment("new", "New"));
    mocks.updateEnvironment.mockResolvedValue(environment("prod", "Prod"));
    mocks.deleteEnvironment.mockResolvedValue(undefined);

    await store.create("New", "blue");
    await store.update(ESTATE[0], ["title"]);
    await store.remove("environments/dev");

    expect(mocks.createEnvironment).toHaveBeenCalledWith("New", "blue");
    expect(mocks.updateEnvironment).toHaveBeenCalledWith(ESTATE[0], ["title"]);
    expect(mocks.deleteEnvironment).toHaveBeenCalledWith("environments/dev");
    expect(mocks.listAllEnvironments).toHaveBeenCalledTimes(3);
  });
});
