<script setup lang="ts">
import { ChevronDown, Plus, X } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

export interface FilterOption {
  type: "instance" | "environment" | "engine";
  label: string;
  value: string;
}

export interface ActiveFilter {
  id: string;
  type: "name" | "instance" | "environment" | "engine";
  label: string;
  value: string;
  displayValue: string;
}

interface Props {
  instances?: Array<{ name: string; title: string }>;
  engineOptions?: Array<{ value: string; label: string }>;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  (e: "update:filters", filters: ActiveFilter[]): void;
  (e: "search", query: string): void;
}>();

const { t } = useI18n();

function generateUniqueId(): string {
  if (typeof crypto !== "undefined" && crypto.randomUUID) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(36).substring(2, 9)}`;
}

const searchQuery = ref("");
const activeFilters = ref<ActiveFilter[]>([]);
const showFilterMenu = ref(false);
const searchInputRef = ref<HTMLInputElement>();
const filterSearchQuery = ref("");
const filterTypeSearchQuery = ref("");

const environmentOptions = [
  { value: "environments/dev", label: "Dev" },
  { value: "environments/test", label: "Test" },
  { value: "environments/staging", label: "Staging" },
  { value: "environments/prod", label: "Prod" },
];

const availableFilterTypes = computed(() => {
  const allTypes = [
    {
      type: "instance" as const,
      label: t("databaseManagement.instanceFilter"),
      icon: "🗄️",
    },
    {
      type: "environment" as const,
      label: t("databaseManagement.environmentFilter"),
      icon: "🌍",
    },
    {
      type: "engine" as const,
      label: t("databaseManagement.engineFilter"),
      icon: "⚙️",
    },
  ];

  // Filter out types that already have an active filter
  const activeFilterTypes = new Set(activeFilters.value.map((f) => f.type));
  return allTypes.filter((type) => !activeFilterTypes.has(type.type));
});

const filteredInstances = computed(() => {
  if (!props.instances) return [];
  if (!filterSearchQuery.value) return props.instances;
  const query = filterSearchQuery.value.toLowerCase();
  return props.instances.filter(
    (i) =>
      i.title.toLowerCase().includes(query) ||
      i.name.toLowerCase().includes(query)
  );
});

const filteredEnvironments = computed(() => {
  if (!filterSearchQuery.value) return environmentOptions;
  const query = filterSearchQuery.value.toLowerCase();
  return environmentOptions.filter((e) =>
    e.label.toLowerCase().includes(query)
  );
});

const filteredEngines = computed(() => {
  if (!props.engineOptions) return [];
  if (!filterSearchQuery.value) return props.engineOptions;
  const query = filterSearchQuery.value.toLowerCase();
  return props.engineOptions.filter((e) =>
    e.label.toLowerCase().includes(query)
  );
});

const selectedFilterType = ref<"instance" | "environment" | "engine" | null>(
  null
);

function addFilter(
  type: "instance" | "environment" | "engine",
  value: string,
  displayValue: string
) {
  // Remove any existing filter of the same type
  activeFilters.value = activeFilters.value.filter((f) => f.type !== type);

  const id = generateUniqueId();
  const label =
    type === "instance"
      ? t("databaseManagement.instanceFilter")
      : type === "environment"
        ? t("databaseManagement.environmentFilter")
        : t("databaseManagement.engineFilter");

  activeFilters.value.push({
    id,
    type,
    label,
    value,
    displayValue,
  });

  selectedFilterType.value = null;
  filterSearchQuery.value = "";
  showFilterMenu.value = false;

  emitFilters();
}

function removeFilter(id: string) {
  activeFilters.value = activeFilters.value.filter((f) => f.id !== id);
  emitFilters();
}

function emitFilters() {
  const allFilters = [...activeFilters.value];

  if (searchQuery.value.trim()) {
    allFilters.push({
      id: "name-search",
      type: "name",
      label: t("databaseManagement.nameFilter"),
      value: searchQuery.value.trim(),
      displayValue: searchQuery.value.trim(),
    });
  }

  emit("update:filters", allFilters);
}

function handleSearchInput() {
  emitFilters();
}

function selectFilterType(type: "instance" | "environment" | "engine") {
  selectedFilterType.value = type;
  filterSearchQuery.value = "";
  filterTypeSearchQuery.value = "";
}

function backToFilterTypes() {
  selectedFilterType.value = null;
  filterSearchQuery.value = "";
  filterTypeSearchQuery.value = "";
}

watch(searchQuery, () => {
  handleSearchInput();
});
</script>

<template>
  <div class="space-y-3">
    <!-- Main Search Bar -->
    <div 
      class="flex items-center flex-wrap gap-2 min-h-[42px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm transition-colors hover:border-ring focus-within:outline-hidden focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2"
    >
      <!-- Active Filter Pills -->
      <Badge
        v-for="filter in activeFilters"
        :key="filter.id"
        variant="secondary"
        class="flex items-center gap-1 px-2 py-1"
      >
        <span class="text-xs font-medium">{{ filter.label }}:</span>
        <span class="text-xs">{{ filter.displayValue }}</span>
        <button
          type="button"
          class="ml-1 rounded-full hover:bg-secondary-foreground/20 transition-colors"
          @click="removeFilter(filter.id)"
        >
          <X class="h-3 w-3" />
        </button>
      </Badge>

      <!-- Search Input -->
      <input
        ref="searchInputRef"
        v-model="searchQuery"
        type="text"
        :placeholder="activeFilters.length === 0 ? t('databaseManagement.searchPlaceholder') : ''"
        class="flex-1 min-w-[120px] bg-transparent outline-hidden placeholder:text-muted-foreground"
      >

      <!-- Add Filter Button -->
      <Popover v-model:open="showFilterMenu">
        <PopoverTrigger as-child>
          <Button
            variant="ghost"
            size="sm"
            class="h-6 px-2 text-xs shrink-0"
          >
            <Plus class="h-3 w-3 mr-1" />
            {{ t("databaseManagement.addFilter") }}
          </Button>
        </PopoverTrigger>
        <PopoverContent
          class="w-[300px] p-0"
          align="start"
        >
          <!-- Filter Type Selection -->
          <Command
            v-if="!selectedFilterType"
            v-model="filterTypeSearchQuery"
          >
            <CommandInput :placeholder="t('databaseManagement.searchFilters')" />
            <CommandList>
              <CommandEmpty>{{ t("databaseManagement.noFiltersFound") }}</CommandEmpty>
              <CommandGroup>
                <CommandItem
                  v-for="filterType in availableFilterTypes"
                  :key="filterType.type"
                  :value="filterType.type"
                  @select="selectFilterType(filterType.type)"
                >
                  <span class="mr-2">{{ filterType.icon }}</span>
                  <span>{{ filterType.label }}</span>
                  <ChevronDown class="ml-auto h-4 w-4 -rotate-90" />
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>

          <!-- Instance Selection -->
          <Command
            v-else-if="selectedFilterType === 'instance'"
            v-model="filterSearchQuery"
          >
            <div class="flex items-center border-b px-3">
              <Button
                variant="ghost"
                size="sm"
                class="h-8 px-2"
                @click="backToFilterTypes"
              >
                <ChevronDown class="h-4 w-4 rotate-90" />
              </Button>
              <CommandInput 
                :placeholder="t('databaseManagement.searchInstances')" 
                class="border-0"
              />
            </div>
            <CommandList>
              <CommandEmpty>{{ t("databaseManagement.noInstancesFound") }}</CommandEmpty>
              <CommandGroup>
                <CommandItem
                  v-for="instance in filteredInstances"
                  :key="instance.name"
                  :value="instance.name"
                  @select="addFilter('instance', instance.name, instance.title)"
                >
                  {{ instance.title }}
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>

          <!-- Environment Selection -->
          <Command
            v-else-if="selectedFilterType === 'environment'"
            v-model="filterSearchQuery"
          >
            <div class="flex items-center border-b px-3">
              <Button
                variant="ghost"
                size="sm"
                class="h-8 px-2"
                @click="backToFilterTypes"
              >
                <ChevronDown class="h-4 w-4 rotate-90" />
              </Button>
              <CommandInput 
                :placeholder="t('databaseManagement.searchEnvironments')" 
                class="border-0"
              />
            </div>
            <CommandList>
              <CommandEmpty>{{ t("databaseManagement.noEnvironmentsFound") }}</CommandEmpty>
              <CommandGroup>
                <CommandItem
                  v-for="env in filteredEnvironments"
                  :key="env.value"
                  :value="env.value"
                  @select="addFilter('environment', env.value, env.label)"
                >
                  {{ env.label }}
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>

          <!-- Engine Selection -->
          <Command
            v-else-if="selectedFilterType === 'engine'"
            v-model="filterSearchQuery"
          >
            <div class="flex items-center border-b px-3">
              <Button
                variant="ghost"
                size="sm"
                class="h-8 px-2"
                @click="backToFilterTypes"
              >
                <ChevronDown class="h-4 w-4 rotate-90" />
              </Button>
              <CommandInput 
                :placeholder="t('databaseManagement.searchEngines')" 
                class="border-0"
              />
            </div>
            <CommandList>
              <CommandEmpty>{{ t("databaseManagement.noEnginesFound") }}</CommandEmpty>
              <CommandGroup>
                <CommandItem
                  v-for="engine in filteredEngines"
                  :key="engine.value"
                  :value="engine.value"
                  @select="addFilter('engine', engine.value, engine.label)"
                >
                  {{ engine.label }}
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>

    <!-- Active Filters Summary -->
    <div 
      v-if="activeFilters.length > 0 || searchQuery"
      class="flex items-center justify-between text-xs text-muted-foreground"
    >
      <span>
        {{ t("databaseManagement.filterActive", { count: activeFilters.length + (searchQuery ? 1 : 0) }) }}
      </span>
      <Button
        variant="ghost"
        size="sm"
        class="h-auto p-0 text-xs hover:underline"
        @click="() => { activeFilters = []; searchQuery = ''; emitFilters(); }"
      >
        {{ t("databaseManagement.clearFilters") }}
      </Button>
    </div>
  </div>
</template>
