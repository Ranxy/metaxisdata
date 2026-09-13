import { defineStore } from "pinia";
import * as environmentApi from "@/api/environment";
import { environmentId } from "@/api/environment";
import type { Environment } from "@/types/proto-es/v1/environment_service_pb";

interface EnvironmentState {
  environments: Environment[];
  loaded: boolean;
  loading: boolean;
}

/**
 * Shared cache of the workspace environments. The instance forms, the metadata
 * filters and the settings page all read it, so a single fetch keeps their
 * labels and colors consistent.
 */
export const useEnvironmentStore = defineStore("environment", {
  state: (): EnvironmentState => ({
    environments: [],
    loaded: false,
    loading: false,
  }),

  getters: {
    // The picker and the filter menu both need {value: resource name, label}.
    options: (state) =>
      state.environments.map((environment) => ({
        value: environment.name,
        label: environmentTitle(environment),
        environment,
      })),
    // Resolve a resource name to its display title. An unknown or not-yet-loaded
    // value falls back to the raw id so a legacy row never renders empty.
    titleOf:
      (state) =>
      (name: string): string => {
        const environment = state.environments.find((e) => e.name === name);
        return environment
          ? environmentTitle(environment)
          : environmentId(name);
      },
    // The Environment message for a resource name, when it is loaded.
    byName:
      (state) =>
      (name: string): Environment | undefined =>
        state.environments.find((e) => e.name === name),
  },

  actions: {
    async fetch(force = false) {
      if (this.loading || (this.loaded && !force)) {
        return;
      }
      this.loading = true;
      try {
        const response = await environmentApi.listEnvironments();
        this.environments = response.environments;
        this.loaded = true;
      } finally {
        this.loading = false;
      }
    },

    async ensureLoaded() {
      await this.fetch();
    },

    async create(title: string, color = "") {
      const created = await environmentApi.createEnvironment(title, color);
      await this.fetch(true);
      return created;
    },

    async update(environment: Environment, updateMask: string[]) {
      const updated = await environmentApi.updateEnvironment(
        environment,
        updateMask
      );
      await this.fetch(true);
      return updated;
    },

    async remove(name: string) {
      await environmentApi.deleteEnvironment(name);
      await this.fetch(true);
    },
  },
});

function environmentTitle(environment: Environment): string {
  return environment.title || environmentId(environment.name);
}
