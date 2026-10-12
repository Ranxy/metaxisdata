<template>
  <div class="flex flex-col gap-1.5 [&>select]:hidden">
    <Label
      v-if="label"
      :for="selectId"
      class="text-sm font-medium leading-none"
    >
      {{ label }}
    </Label>

    <Popover v-model:open="open">
      <PopoverTrigger as-child>
        <button
          :id="selectId"
          type="button"
          :disabled="disabled"
          class="flex h-10 w-full items-center justify-between rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm transition-shadow focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <span
            v-if="selected"
            class="flex min-w-0 items-center gap-2"
          >
            <span class="truncate">{{ selected.displayName || selected.applicationId }}</span>
            <span class="shrink-0 text-xs text-muted-foreground">
              {{ selected.applicationId }}
            </span>
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
          :filter-function="filterFunction"
          @update:model-value="choose"
        >
          <CommandInput :placeholder="t('applicationSql.searchApplication')" />
          <CommandList>
            <CommandEmpty>{{ t("applicationSql.noApplicationMatch") }}</CommandEmpty>
            <CommandGroup>
              <CommandItem
                :value="ANY_VALUE"
                @select="choose(ANY_VALUE)"
              >
                <span class="truncate">{{ t("applicationSql.anyApplication") }}</span>
                <Check
                  v-if="modelValue === ''"
                  class="ml-auto h-4 w-4"
                />
              </CommandItem>
              <CommandItem
                v-for="application in applications"
                :key="application.applicationId"
                :value="application.applicationId"
                @select="choose(application.applicationId)"
              >
                <span class="truncate">{{ application.displayName || application.applicationId }}</span>
                <span class="ml-2 shrink-0 text-xs text-muted-foreground">
                  {{ application.applicationId }}
                </span>
                <span
                  v-if="application.disabled"
                  class="ml-2 shrink-0 text-xs text-muted-foreground"
                >
                  {{ t("applicationManagement.stopped") }}
                </span>
                <Check
                  v-if="application.applicationId === modelValue"
                  class="ml-auto h-4 w-4"
                />
              </CommandItem>
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  </div>
</template>

<script setup lang="ts">
import { Check, ChevronDown } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { listApplications } from "@/api/application";
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
import { useErrorMessage } from "@/composables/useErrorHandler";
import type { Application } from "@/types/proto-es/v1/application_service_pb";

interface Props {
  /** The application id, or an empty string for "any application". */
  modelValue?: string;
  label?: string;
  placeholder?: string;
  disabled?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: "",
  label: "",
  placeholder: "",
  disabled: false,
});

const emit = defineEmits<{
  "update:modelValue": [value: string];
}>();

/** Sentinel for the "any application" row: a command item cannot carry an empty value. */
const ANY_VALUE = "__any_application__";

const { t } = useI18n();
const { formatError } = useErrorMessage();

const open = ref(false);
const query = ref("");
const loadError = ref("");
const applications = ref<Application[]>([]);

const selectId = `application-select-${Math.random().toString(36).substring(2, 9)}`;

const selected = computed(() =>
  applications.value.find(
    (application) => application.applicationId === props.modelValue
  )
);

onMounted(async () => {
  try {
    applications.value = await loadAllApplications();
  } catch (error) {
    // A filter that cannot fill itself leaves the rest of the page working.
    loadError.value = formatError(error);
    console.error(`Failed to load the applications: ${loadError.value}`);
  }
});

// The filter lists every application, stopped ones included: a statement a stopped
// application reported is still a row, and the reader may want to find it by name.
async function loadAllApplications(): Promise<Application[]> {
  const all: Application[] = [];
  let pageToken = "";
  for (let page = 0; page < 10; page++) {
    const response = await listApplications({
      pageSize: 100,
      pageToken,
      showDisabled: true,
    });
    all.push(...response.applications);
    pageToken = response.nextPageToken;
    if (!pageToken) {
      break;
    }
  }
  return all;
}

// A command item's value is the application id, so the search has to reach the
// display name the row shows.
function filterFunction(
  values: string[] | number[] | boolean[] | Record<string, unknown>[],
  term: string
): string[] {
  const search = term.trim().toLowerCase();
  return (values as string[]).filter((value) => {
    // "Any application" is the reset, so it is offered whenever the box is empty rather
    // than pinned above every result: a search that matches nothing shows the empty state.
    if (value === ANY_VALUE) {
      return search === "";
    }
    const application = applications.value.find(
      (candidate) => candidate.applicationId === value
    );
    if (!application) {
      return value.toLowerCase().includes(search);
    }
    return (
      application.displayName.toLowerCase().includes(search) ||
      value.toLowerCase().includes(search)
    );
  });
}

function choose(value: unknown) {
  if (typeof value !== "string") {
    return;
  }
  open.value = false;
  emit("update:modelValue", value === ANY_VALUE ? "" : value);
}
</script>
