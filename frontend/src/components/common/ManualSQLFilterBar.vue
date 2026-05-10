<script setup lang="ts">
import { ChevronDown, Plus, X } from "lucide-vue-next";
import { computed, ref } from "vue";
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

type FilterType = "database" | "schema" | "tags";

interface DatabaseOption {
  value: string;
  label: string;
}

interface Props {
  databaseOptions: DatabaseOption[];
  selectedDatabase: string;
  searchQuery: string;
  schemaFilter: string;
  tagsFilter: string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  (e: "update:selectedDatabase", value: string): void;
  (e: "update:searchQuery", value: string): void;
  (e: "update:schemaFilter", value: string): void;
  (e: "update:tagsFilter", value: string): void;
}>();

const { t } = useI18n();

const showFilterMenu = ref(false);
const selectedFilterType = ref<FilterType | null>(null);
const filterTypeSearchQuery = ref("");
const filterSearchQuery = ref("");
const customFilterValue = ref("");

const searchValue = computed({
  get: () => props.searchQuery,
  set: (value: string) => emit("update:searchQuery", value),
});

const activeFilters = computed(() => {
  const filters: Array<{
    type: FilterType;
    label: string;
    displayValue: string;
  }> = [];

  if (props.selectedDatabase) {
    filters.push({
      type: "database",
      label: t("manualSqlManagement.databaseFilter"),
      displayValue: selectedDatabaseLabel.value,
    });
  }

  if (props.schemaFilter.trim()) {
    filters.push({
      type: "schema",
      label: t("manualSqlManagement.schemaFilter"),
      displayValue: props.schemaFilter.trim(),
    });
  }

  if (props.tagsFilter.trim()) {
    filters.push({
      type: "tags",
      label: t("manualSqlManagement.tagsFilter"),
      displayValue: props.tagsFilter.trim(),
    });
  }

  return filters;
});

const activeFilterTypes = computed(
  () => new Set(activeFilters.value.map((filter) => filter.type))
);

const availableFilterTypes = computed(() => {
  const allTypes = [
    {
      type: "database" as const,
      label: t("manualSqlManagement.databaseFilter"),
      icon: "🗄️",
    },
    {
      type: "schema" as const,
      label: t("manualSqlManagement.schemaFilter"),
      icon: "#",
    },
    {
      type: "tags" as const,
      label: t("manualSqlManagement.tagsFilter"),
      icon: "🏷️",
    },
  ];

  return allTypes.filter((type) => !activeFilterTypes.value.has(type.type));
});

const selectedDatabaseLabel = computed(() => {
  return (
    props.databaseOptions.find(
      (option) => option.value === props.selectedDatabase
    )?.label || props.selectedDatabase
  );
});

const filteredDatabaseOptions = computed(() => {
  if (!filterSearchQuery.value) {
    return props.databaseOptions;
  }

  const query = filterSearchQuery.value.toLowerCase();
  return props.databaseOptions.filter(
    (option) =>
      option.label.toLowerCase().includes(query) ||
      option.value.toLowerCase().includes(query)
  );
});

const activeFilterCount = computed(
  () => activeFilters.value.length + (props.searchQuery.trim() ? 1 : 0)
);

function selectFilterType(type: FilterType) {
  selectedFilterType.value = type;
  filterSearchQuery.value = "";
  filterTypeSearchQuery.value = "";
  customFilterValue.value =
    type === "schema"
      ? props.schemaFilter
      : type === "tags"
        ? props.tagsFilter
        : "";
}

function backToFilterTypes() {
  selectedFilterType.value = null;
  filterSearchQuery.value = "";
  filterTypeSearchQuery.value = "";
  customFilterValue.value = "";
}

function closeFilterMenu() {
  showFilterMenu.value = false;
  backToFilterTypes();
}

function applyDatabaseFilter(value: string) {
  emit("update:selectedDatabase", value);
  closeFilterMenu();
}

function applyCustomFilter() {
  const value = customFilterValue.value.trim();

  if (!selectedFilterType.value || !value) {
    return;
  }

  if (selectedFilterType.value === "schema") {
    emit("update:schemaFilter", value);
  } else if (selectedFilterType.value === "tags") {
    emit("update:tagsFilter", value);
  }

  closeFilterMenu();
}

function removeFilter(type: FilterType) {
  if (type === "database") {
    emit("update:selectedDatabase", "");
    return;
  }

  if (type === "schema") {
    emit("update:schemaFilter", "");
    return;
  }

  emit("update:tagsFilter", "");
}

function clearFilters() {
  emit("update:selectedDatabase", "");
  emit("update:searchQuery", "");
  emit("update:schemaFilter", "");
  emit("update:tagsFilter", "");
}
</script>

<template>
  <div class="space-y-3">
    <div
      class="flex min-h-[42px] w-full flex-wrap items-center gap-2 rounded-md border border-input bg-background px-3 py-2 text-sm transition-colors hover:border-ring focus-within:outline-hidden focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2"
    >
      <Badge
        v-for="filter in activeFilters"
        :key="filter.type"
        variant="secondary"
        class="flex items-center gap-1 px-2 py-1"
      >
        <span class="text-xs font-medium">{{ filter.label }}:</span>
        <span class="text-xs">{{ filter.displayValue }}</span>
        <button
          type="button"
          class="ml-1 rounded-full transition-colors hover:bg-secondary-foreground/20"
          @click="removeFilter(filter.type)"
        >
          <X class="h-3 w-3" />
        </button>
      </Badge>

      <input
        v-model="searchValue"
        type="text"
        :placeholder="activeFilters.length === 0 ? t('manualSqlManagement.searchPlaceholder') : ''"
        class="min-w-[120px] flex-1 bg-transparent outline-hidden placeholder:text-muted-foreground"
      >

      <Popover v-model:open="showFilterMenu">
        <PopoverTrigger as-child>
          <Button
            variant="ghost"
            size="sm"
            class="h-6 shrink-0 px-2 text-xs"
          >
            <Plus class="mr-1 h-3 w-3" />
            {{ t("manualSqlManagement.addFilter") }}
          </Button>
        </PopoverTrigger>
        <PopoverContent
          class="w-[320px] p-0"
          align="start"
        >
          <Command
            v-if="!selectedFilterType"
            v-model="filterTypeSearchQuery"
          >
            <CommandInput :placeholder="t('manualSqlManagement.searchFilters')" />
            <CommandList>
              <CommandEmpty>{{ t("manualSqlManagement.noFiltersFound") }}</CommandEmpty>
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

          <Command
            v-else-if="selectedFilterType === 'database'"
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
                :placeholder="t('manualSqlManagement.searchDatabases')"
                class="border-0"
              />
            </div>
            <CommandList>
              <CommandEmpty>{{ t("manualSqlManagement.noDatabasesFound") }}</CommandEmpty>
              <CommandGroup>
                <CommandItem
                  v-for="database in filteredDatabaseOptions"
                  :key="database.value"
                  :value="database.value"
                  @select="applyDatabaseFilter(database.value)"
                >
                  {{ database.label }}
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>

          <div
            v-else
            class="p-3"
          >
            <div class="mb-3 flex items-center gap-2 border-b pb-3">
              <Button
                variant="ghost"
                size="sm"
                class="h-8 px-2"
                @click="backToFilterTypes"
              >
                <ChevronDown class="h-4 w-4 rotate-90" />
              </Button>
              <div class="text-sm font-medium">
                {{ selectedFilterType === "schema" ? t("manualSqlManagement.schemaFilter") : t("manualSqlManagement.tagsFilter") }}
              </div>
            </div>

            <input
              v-model="customFilterValue"
              type="text"
              :placeholder="selectedFilterType === 'schema' ? t('manualSqlManagement.schemaPlaceholder') : t('manualSqlManagement.tagsPlaceholder')"
              class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-hidden transition-colors focus-visible:ring-2 focus-visible:ring-ring"
              @keydown.enter.prevent="applyCustomFilter"
            >

            <p
              v-if="selectedFilterType === 'tags'"
              class="mt-2 text-xs text-muted-foreground"
            >
              {{ t("manualSqlManagement.tagsHint") }}
            </p>

            <div class="mt-3 flex justify-end">
              <Button
                size="sm"
                :disabled="!customFilterValue.trim()"
                @click="applyCustomFilter"
              >
                {{ t("manualSqlManagement.applyFilter") }}
              </Button>
            </div>
          </div>
        </PopoverContent>
      </Popover>
    </div>

    <div
      v-if="activeFilterCount > 0"
      class="flex items-center justify-between text-xs text-muted-foreground"
    >
      <span>{{ t("manualSqlManagement.filterActive", { count: activeFilterCount }) }}</span>
      <Button
        variant="ghost"
        size="sm"
        class="h-auto p-0 text-xs hover:underline"
        @click="clearFilters"
      >
        {{ t("manualSqlManagement.clearFilters") }}
      </Button>
    </div>
  </div>
</template>