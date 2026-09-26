<template>
  <span
    v-if="environment"
    class="flex items-center gap-2"
  >
    <span
      class="h-2.5 w-2.5 shrink-0 rounded-full"
      :style="{ backgroundColor: color }"
    />
    {{ title }}
  </span>
  <span
    v-else
    class="text-muted-foreground"
  >-</span>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useEnvironmentStore } from "@/store/modules/environment";
import { environmentColorHex } from "@/utils/environment";

// One environment cell for every table that lists one. The colored dot is the
// signal the Environments settings page lets an admin configure, so it has to
// appear wherever an environment does; before this component only the databases
// table drew it, and the instance tables showed a bare, muted name.
const props = defineProps<{ environment?: string }>();

const environmentStore = useEnvironmentStore();

const title = computed(() =>
  props.environment ? environmentStore.titleOf(props.environment) : ""
);

// Falls back to the muted foreground while the catalog is still loading, so an
// unresolved row never renders an invisible dot.
const color = computed(() => {
  const environment = props.environment
    ? environmentStore.byName(props.environment)
    : undefined;
  return environment
    ? environmentColorHex(environment)
    : "var(--muted-foreground)";
});
</script>
