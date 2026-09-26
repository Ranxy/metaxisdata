import { defineStore } from "pinia";
import {
  type CreateInstanceInput,
  createInstance,
  deleteInstance,
  listAllInstances,
  syncInstance,
  type UpdateInstanceInput,
  undeleteInstance,
  updateInstance,
} from "@/api/instance";
import { State } from "@/types/proto-es/v1/common_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";

interface InstanceState {
  /** Every instance, soft-deleted rows included, so one fetch serves both lists. */
  instances: Instance[];
  loaded: boolean;
  loading: boolean;
}

/** The id of an instance, the last segment of its resource name. */
function instanceId(name: string): string {
  return name.split("/").pop() ?? name;
}

/**
 * Shared cache of the workspace instances.
 *
 * Six pages used to call ListInstances on their own, five of them with a page
 * size they never checked a `next_page_token` against — the tail of a large
 * estate simply never rendered. One walked fetch here removes both the duplicate
 * RPCs and the truncation, and every write refreshes it so the lists cannot
 * disagree with the forms that changed them.
 */
export const useInstanceStore = defineStore("instance", {
  state: (): InstanceState => ({
    instances: [],
    loaded: false,
    loading: false,
  }),

  getters: {
    active: (state) =>
      state.instances.filter((instance) => instance.state !== State.DELETED),
    deleted: (state) =>
      state.instances.filter((instance) => instance.state === State.DELETED),
    // Resolve a resource name (`instances/x`) or a bare id (`x`) to a display
    // title. An unknown or not-yet-loaded value falls back to the id so a legacy
    // row never renders empty.
    titleOf:
      (state) =>
      (value: string): string => {
        const instance = state.instances.find(
          (item) => item.name === value || instanceId(item.name) === value
        );
        return instance?.title || instanceId(value);
      },
    byName:
      (state) =>
      (name: string): Instance | undefined =>
        state.instances.find((instance) => instance.name === name),
  },

  actions: {
    async fetch(force = false) {
      if (this.loading || (this.loaded && !force)) {
        return;
      }
      this.loading = true;
      try {
        this.instances = await listAllInstances({ showDeleted: true });
        this.loaded = true;
      } finally {
        this.loading = false;
      }
    },

    async ensureLoaded() {
      await this.fetch();
    },

    /** A write through the store refreshes the cache the readers share. */
    async create(input: CreateInstanceInput) {
      const created = await createInstance(input);
      await this.fetch(true);
      return created;
    },

    async update(input: UpdateInstanceInput) {
      const updated = await updateInstance(input);
      await this.fetch(true);
      return updated;
    },

    async remove(name: string) {
      await deleteInstance(name);
      await this.fetch(true);
    },

    async restore(name: string) {
      await undeleteInstance(name);
      await this.fetch(true);
    },

    async sync(name: string, enableFullSync = false) {
      await syncInstance(name, enableFullSync);
      await this.fetch(true);
    },
  },
});
