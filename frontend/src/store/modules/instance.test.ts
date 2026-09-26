import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { State } from "@/types/proto-es/v1/common_pb";
import { InstanceSchema } from "@/types/proto-es/v1/instance_service_pb";
import { useInstanceStore } from "./instance";

const mocks = vi.hoisted(() => ({
  listAllInstances: vi.fn(),
  createInstance: vi.fn(),
  updateInstance: vi.fn(),
  deleteInstance: vi.fn(),
  undeleteInstance: vi.fn(),
  syncInstance: vi.fn(),
}));

vi.mock("@/api/instance", () => ({
  listAllInstances: mocks.listAllInstances,
  createInstance: mocks.createInstance,
  updateInstance: mocks.updateInstance,
  deleteInstance: mocks.deleteInstance,
  undeleteInstance: mocks.undeleteInstance,
  syncInstance: mocks.syncInstance,
}));

function instance(id: string, state = State.ACTIVE, title = "") {
  return create(InstanceSchema, {
    name: `instances/${id}`,
    title: title || id,
    state,
  });
}

const ESTATE = [
  instance("prod"),
  instance("staging", State.ACTIVE, "Staging"),
  instance("old", State.DELETED, "Retired"),
];

describe("instance store", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    mocks.listAllInstances.mockResolvedValue(ESTATE);
  });

  it("fetches the whole estate once and reuses it", async () => {
    const store = useInstanceStore();

    await store.fetch();
    await store.fetch();
    await store.ensureLoaded();

    expect(mocks.listAllInstances).toHaveBeenCalledTimes(1);
    expect(mocks.listAllInstances).toHaveBeenCalledWith({ showDeleted: true });
    expect(store.instances).toHaveLength(3);
  });

  it("refetches when asked to force", async () => {
    const store = useInstanceStore();

    await store.fetch();
    await store.fetch(true);

    expect(mocks.listAllInstances).toHaveBeenCalledTimes(2);
  });

  it("splits the deleted rows out of the active list", async () => {
    const store = useInstanceStore();
    await store.fetch();

    expect(store.active.map((item) => item.name)).toEqual([
      "instances/prod",
      "instances/staging",
    ]);
    expect(store.deleted.map((item) => item.name)).toEqual(["instances/old"]);
  });

  it("resolves a title by resource name or bare id", async () => {
    const store = useInstanceStore();
    await store.fetch();

    expect(store.titleOf("instances/staging")).toBe("Staging");
    expect(store.titleOf("staging")).toBe("Staging");
    // Unknown values fall back to the id so a legacy row never renders empty.
    expect(store.titleOf("instances/gone")).toBe("gone");
  });

  it("refreshes the cache after every write", async () => {
    const store = useInstanceStore();
    await store.fetch();
    mocks.listAllInstances.mockClear();

    mocks.createInstance.mockResolvedValue(instance("new"));
    mocks.deleteInstance.mockResolvedValue(undefined);
    mocks.undeleteInstance.mockResolvedValue(undefined);

    await store.create({} as never);
    await store.remove("instances/new");
    await store.restore("instances/old");

    expect(mocks.listAllInstances).toHaveBeenCalledTimes(3);
  });

  it("keeps the previous cache when a fetch fails", async () => {
    const store = useInstanceStore();
    await store.fetch();
    mocks.listAllInstances.mockRejectedValue(new Error("offline"));

    await expect(store.fetch(true)).rejects.toThrow("offline");

    expect(store.instances).toHaveLength(3);
    expect(store.loading).toBe(false);
  });
});
