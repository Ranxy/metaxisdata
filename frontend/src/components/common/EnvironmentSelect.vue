<template>
  <div class="w-full space-y-2.5">
    <Label
      v-if="label"
      :for="selectId"
    >
      {{ label }}
      <span
        v-if="required"
        class="text-destructive"
      >*</span>
    </Label>

    <Popover v-model:open="open">
      <PopoverTrigger as-child>
        <button
          :id="selectId"
          type="button"
          :disabled="disabled"
          class="flex h-10 w-full items-center justify-between rounded-md bg-background px-3 py-2 text-sm shadow-[inset_0_0_0_1px_rgb(0_0_0/0.1)] transition-shadow focus-visible:outline-none focus-visible:shadow-[inset_0_0_0_1.5px_rgb(0_0_0/0.2)] disabled:cursor-not-allowed disabled:opacity-50"
          :class="error ? 'shadow-[inset_0_0_0_1px_var(--destructive)]' : ''"
        >
          <span
            v-if="selectedTitle"
            class="flex min-w-0 items-center gap-2"
          >
            <span
              v-if="selectedEnvironment"
              class="h-2.5 w-2.5 shrink-0 rounded-full"
              :style="{ backgroundColor: selectedHex }"
            />
            <span class="truncate">{{ selectedTitle }}</span>
          </span>
          <span
            v-else
            class="truncate text-muted-foreground"
          >
            {{ placeholder }}
          </span>
          <ChevronDown class="h-4 w-4 shrink-0 opacity-50" />
        </button>
      </PopoverTrigger>

      <PopoverContent
        class="w-[var(--radix-popover-trigger-width)] p-0"
        align="start"
      >
        <Command
          v-model:search-term="query"
          :model-value="modelValue"
          :filter-function="filterFunction"
          @update:model-value="choose"
        >
          <CommandInput :placeholder="t('environment.searchPlaceholder')" />
          <CommandList>
            <CommandEmpty>{{ t("environment.noResults") }}</CommandEmpty>
            <CommandGroup>
              <CommandItem
                v-for="environment in environments"
                :key="environment.name"
                :value="environment.name"
                @select="choose(environment.name)"
              >
                <span
                  class="mr-2 h-2.5 w-2.5 rounded-full"
                  :style="{ backgroundColor: environmentColorHex(environment) }"
                />
                <span class="truncate">{{ environmentTitle(environment) }}</span>
                <Check
                  v-if="environment.name === modelValue"
                  class="ml-auto h-4 w-4"
                />
              </CommandItem>
              <CommandItem
                v-if="canCreate"
                :value="CREATE_VALUE"
                @select="openCreate"
              >
                <Plus class="mr-2 h-4 w-4" />
                <span class="truncate">{{
                  t("environment.createOption", { name: query.trim() })
                }}</span>
              </CommandItem>
              <CommandItem
                v-else
                :value="CREATE_VALUE"
                disabled
              >
                <span class="truncate text-muted-foreground">{{
                  t("environment.createHint")
                }}</span>
              </CommandItem>
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>

    <p
      v-if="error"
      class="text-sm text-destructive"
    >
      {{ error }}
    </p>
  </div>

  <AppModal
    v-model="createOpen"
    :title="t('environment.createTitle')"
    size="sm"
  >
    <div class="space-y-4">
      <AppInput
        v-model="newTitle"
        :label="t('environment.nameLabel')"
        :placeholder="t('environment.namePlaceholder')"
        required
        :error="createError"
        @keyup.enter="submitCreate"
      />
      <p class="text-sm text-muted-foreground">
        {{ t("environment.nameHint") }}
      </p>
    </div>
    <template #footer>
      <Button
        variant="outline"
        @click="createOpen = false"
      >
        {{ t("environment.cancel") }}
      </Button>
      <AppButton
        :loading="creating"
        @click="submitCreate"
      >
        {{ t("environment.create") }}
      </AppButton>
    </template>
  </AppModal>
</template>

<script setup lang="ts">
import { Check, ChevronDown, Plus } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import AppButton from "@/components/common/AppButton.vue";
import AppInput from "@/components/common/AppInput.vue";
import AppModal from "@/components/common/AppModal.vue";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Label } from "@/components/ui/label";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useAuthStore } from "@/store/modules/auth";
import { useEnvironmentStore } from "@/store/modules/environment";
import type { Environment } from "@/types/proto-es/v1/environment_service_pb";
import { environmentColorHex } from "@/utils/environment";
import { extractErrorMessage } from "@/utils/error";

interface Props {
  modelValue?: string;
  label?: string;
  placeholder?: string;
  disabled?: boolean;
  required?: boolean;
  error?: string;
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: "",
  label: "",
  placeholder: "",
  disabled: false,
  required: false,
  error: "",
});

const emit = defineEmits<{
  "update:modelValue": [value: string];
}>();

/** Sentinel value for the "create a new environment" row. */
const CREATE_VALUE = "__create_environment__";

const { t } = useI18n();
const authStore = useAuthStore();
const environmentStore = useEnvironmentStore();

const open = ref(false);
const query = ref("");
const createOpen = ref(false);
const newTitle = ref("");
const creating = ref(false);
const createError = ref("");

const selectId = `environment-select-${Math.random().toString(36).substring(2, 9)}`;

const environments = computed(() => environmentStore.environments);
const selectedEnvironment = computed<Environment | undefined>(() =>
  environmentStore.byName(props.modelValue)
);
const selectedTitle = computed(() =>
  props.modelValue ? environmentStore.titleOf(props.modelValue) : ""
);
const selectedHex = computed(() =>
  selectedEnvironment.value
    ? environmentColorHex(selectedEnvironment.value)
    : undefined
);
const canCreate = computed(() =>
  authStore.hasPermission("metaxisdata.settings.update")
);
// Offer creation only for a term that does not already name an environment, so
// an exact match selects the existing one instead of adding a duplicate.
const showCreateRow = computed(() => {
  const value = query.value.trim().toLowerCase();
  if (!value) {
    return false;
  }
  return !environments.value.some(
    (environment) => environmentTitle(environment).toLowerCase() === value
  );
});

onMounted(() => {
  void environmentStore.ensureLoaded();
});

// The create row is registered but filtered out unless the term justifies it,
// so it never competes with a real match in the keyboard navigation order.
// Combobox values are always strings here; the widened parameter matches
// radix-vue's filterFunction signature.
function filterFunction(
  values: string[] | number[] | boolean[] | Record<string, unknown>[],
  term: string
): string[] {
  const list = values as string[];
  const search = term.trim().toLowerCase();
  const matched = list.filter((value) => {
    if (value === CREATE_VALUE) {
      return false;
    }
    const environment = environmentStore.byName(value);
    if (!environment) {
      return value.toLowerCase().includes(search);
    }
    return (
      environmentTitle(environment).toLowerCase().includes(search) ||
      value.toLowerCase().includes(search)
    );
  });
  if (!search || !canCreate.value || !showCreateRow.value) {
    return matched;
  }
  return [...matched, CREATE_VALUE];
}

function choose(value: unknown) {
  if (value === CREATE_VALUE) {
    openCreate();
    return;
  }
  if (typeof value !== "string") {
    return;
  }
  open.value = false;
  emit("update:modelValue", value);
}

function openCreate() {
  open.value = false;
  newTitle.value = query.value.trim();
  createError.value = "";
  createOpen.value = true;
}

async function submitCreate() {
  const title = newTitle.value.trim();
  if (!title) {
    createError.value = t("environment.nameRequired");
    return;
  }
  creating.value = true;
  createError.value = "";
  try {
    const created = await environmentStore.create(title);
    createOpen.value = false;
    query.value = "";
    emit("update:modelValue", created.name);
  } catch (error) {
    createError.value = extractErrorMessage(error);
  } finally {
    creating.value = false;
  }
}

function environmentTitle(environment: Environment): string {
  const id = environment.name.replace(/^environments\//, "");
  return environment.title || id;
}
</script>
