<script setup lang="ts">
import { ChevronDown, Plus, X } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
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
import { useEnvironmentStore } from "@/store/modules/environment";

export interface FilterOption {
  value: string;
  label: string;
}

/** One step of a cascading filter, asked for the levels already picked. */
export interface FilterLevel {
  key: string;
  label: string;
  options: (picked: Record<string, string>) => Promise<FilterOption[]>;
}

/**
 * A filterable dimension. `kind` decides how its value is entered:
 * - `options` (default): one of `options`, which may be loaded asynchronously,
 * - `input`: a free-text value,
 * - `tags`: a list of values,
 * - `cascade`: walk `levels` in order, then apply the values picked so far.
 */
export interface FilterCategory {
  type: string;
  label: string;
  icon: string;
  kind?: "options" | "input" | "tags" | "cascade";
  options?: FilterOption[] | (() => Promise<FilterOption[]>);
  levels?: FilterLevel[];
  placeholder?: string;
  hint?: string;
  note?: string;
}

export interface ActiveFilter {
  id: string;
  type: string;
  label: string;
  value: string;
  displayValue: string;
}

interface Props {
  instances?: Array<{ name: string; title: string }>;
  engineOptions?: FilterOption[];
  /** When provided, replaces the built-in instance/environment/engine filters. */
  filterCategories?: FilterCategory[];
  searchPlaceholder?: string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  (e: "update:filters", filters: ActiveFilter[]): void;
}>();

const { t } = useI18n();
const environmentStore = useEnvironmentStore();

onMounted(() => {
  void environmentStore.ensureLoaded();
});

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
const categorySearchQuery = ref("");

const environmentOptions = computed<FilterOption[]>(() =>
  environmentStore.options.map((option) => ({
    value: option.value,
    label: option.label,
  }))
);

const categories = computed<FilterCategory[]>(() => {
  if (props.filterCategories) {
    return props.filterCategories;
  }
  const builtIn: FilterCategory[] = [
    {
      type: "environment",
      label: t("databaseManagement.environmentFilter"),
      icon: "🌍",
      options: environmentOptions.value,
    },
  ];
  if (props.instances) {
    builtIn.unshift({
      type: "instance",
      label: t("databaseManagement.instanceFilter"),
      icon: "🗄️",
      options: props.instances.map((instance) => ({
        value: instance.name,
        label: instance.title,
      })),
    });
  }
  if (props.engineOptions) {
    builtIn.push({
      type: "engine",
      label: t("databaseManagement.engineFilter"),
      icon: "⚙️",
      options: props.engineOptions,
    });
  }
  return builtIn;
});

const availableCategories = computed(() => {
  const active = new Set(activeFilters.value.map((filter) => filter.type));
  return categories.value.filter((category) => !active.has(category.type));
});

const selectedType = ref<string | null>(null);
const selectedCategory = computed(
  () =>
    categories.value.find((category) => category.type === selectedType.value) ??
    null
);
const kind = computed(() => selectedCategory.value?.kind ?? "options");
const isOptionsKind = computed(() => kind.value === "options");
const isInputKind = computed(() => kind.value === "input");
const isTagsKind = computed(() => kind.value === "tags");
const isCascadeKind = computed(() => kind.value === "cascade");

// ---- options ----
const loadedOptions = ref<FilterOption[]>([]);
const isLoadingOptions = ref(false);

const visibleOptions = computed(() =>
  matches(filterSearchQuery.value, loadedOptions.value)
);

// ---- free text / tags ----
const inputValue = ref("");
const tagValues = ref<string[]>([]);
const tagDraft = ref("");

// ---- cascade ----
const cascadePicked = ref<Record<string, string>>({});
const cascadeLabels = ref<Record<string, string>>({});
const cascadeOptions = ref<FilterOption[]>([]);
const cascadeLevelIndex = ref(0);
const cascadeLevels = computed(() => selectedCategory.value?.levels ?? []);
const currentLevel = computed(
  () => cascadeLevels.value[cascadeLevelIndex.value]
);
const visibleCascadeOptions = computed(() =>
  matches(filterSearchQuery.value, cascadeOptions.value)
);
const cascadeDisplay = computed(() =>
  cascadeLevels.value
    .map((level) => cascadeLabels.value[level.key])
    .filter(Boolean)
    .join(" > ")
);

function matches(query: string, options: FilterOption[]): FilterOption[] {
  const q = query.trim().toLowerCase();
  if (!q) return options;
  return options.filter(
    (option) =>
      option.label.toLowerCase().includes(q) ||
      option.value.toLowerCase().includes(q)
  );
}

function emitFilters() {
  const allFilters = [...activeFilters.value];
  if (searchQuery.value.trim()) {
    allFilters.push({
      id: "name-search",
      type: "name",
      label: t("common.filter.name"),
      value: searchQuery.value.trim(),
      displayValue: searchQuery.value.trim(),
    });
  }
  emit("update:filters", allFilters);
}

function addFilter(type: string, value: string, displayValue: string) {
  activeFilters.value = activeFilters.value.filter(
    (filter) => filter.type !== type
  );
  activeFilters.value.push({
    id: generateUniqueId(),
    type,
    label:
      categories.value.find((category) => category.type === type)?.label ??
      type,
    value,
    displayValue,
  });
  closePanel();
  emitFilters();
}

function removeFilter(id: string) {
  activeFilters.value = activeFilters.value.filter(
    (filter) => filter.id !== id
  );
  emitFilters();
}

function clearAll() {
  activeFilters.value = [];
  searchQuery.value = "";
  emitFilters();
}

function backToCategories() {
  selectedType.value = null;
  filterSearchQuery.value = "";
  categorySearchQuery.value = "";
  loadedOptions.value = [];
  cascadePicked.value = {};
  cascadeLabels.value = {};
  cascadeOptions.value = [];
  cascadeLevelIndex.value = 0;
  inputValue.value = "";
  tagValues.value = [];
  tagDraft.value = "";
}

function closePanel() {
  showFilterMenu.value = false;
  backToCategories();
}

async function selectCategory(type: string) {
  backToCategories();
  selectedType.value = type;
  const category = selectedCategory.value;
  if (!category) return;

  if (category.kind === "cascade") {
    await loadCascadeLevel();
    return;
  }
  if (category.kind === "input" || category.kind === "tags") {
    return;
  }
  await loadOptions(category);
}

async function loadOptions(category: FilterCategory) {
  const source = category.options;
  if (typeof source !== "function") {
    loadedOptions.value = source ?? [];
    return;
  }
  isLoadingOptions.value = true;
  try {
    loadedOptions.value = await source();
  } finally {
    isLoadingOptions.value = false;
  }
}

function applyOption(option: FilterOption) {
  if (!selectedCategory.value) return;
  addFilter(selectedCategory.value.type, option.value, option.label);
}

function applyInput() {
  const value = inputValue.value.trim();
  if (!selectedCategory.value || !value) return;
  addFilter(selectedCategory.value.type, value, value);
}

function applyTags() {
  commitTagDraft();
  if (!selectedCategory.value || tagValues.value.length === 0) return;
  addFilter(
    selectedCategory.value.type,
    tagValues.value.join(","),
    tagValues.value.join(", ")
  );
}

function addTag(tag: string) {
  const normalized = tag.trim();
  if (!normalized || tagValues.value.includes(normalized)) return;
  tagValues.value.push(normalized);
}

function commitTagDraft() {
  const draft = tagDraft.value.trim();
  if (!draft) {
    tagDraft.value = "";
    return;
  }
  for (const tag of draft
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean)) {
    addTag(tag);
  }
  tagDraft.value = "";
}

function removeTag(tag: string) {
  tagValues.value = tagValues.value.filter((value) => value !== tag);
}

function handleTagKeydown(event: KeyboardEvent) {
  if (event.key === "Enter" || event.key === ",") {
    event.preventDefault();
    commitTagDraft();
    return;
  }
  if (
    event.key === "Backspace" &&
    !tagDraft.value &&
    tagValues.value.length > 0
  ) {
    tagValues.value.pop();
  }
}

async function loadCascadeLevel() {
  const level = currentLevel.value;
  if (!level) return;
  isLoadingOptions.value = true;
  try {
    cascadeOptions.value = await level.options(cascadePicked.value);
  } finally {
    isLoadingOptions.value = false;
  }
}

async function pickCascadeOption(option: FilterOption) {
  const level = currentLevel.value;
  if (!level) return;
  cascadePicked.value = { ...cascadePicked.value, [level.key]: option.value };
  cascadeLabels.value = { ...cascadeLabels.value, [level.key]: option.label };
  filterSearchQuery.value = "";

  if (cascadeLevelIndex.value < cascadeLevels.value.length - 1) {
    cascadeLevelIndex.value += 1;
    await loadCascadeLevel();
  }
}

function cascadeBack() {
  filterSearchQuery.value = "";
  if (cascadeLevelIndex.value === 0) {
    backToCategories();
    return;
  }
  // Stepping back drops the level being left, so the cascade never carries a
  // value the user cannot see.
  const level = cascadeLevels.value[cascadeLevelIndex.value];
  if (level) {
    const picked = { ...cascadePicked.value };
    const labels = { ...cascadeLabels.value };
    delete picked[level.key];
    delete labels[level.key];
    cascadePicked.value = picked;
    cascadeLabels.value = labels;
  }
  cascadeLevelIndex.value -= 1;
  void loadCascadeLevel();
}

function applyCascade() {
  const category = selectedCategory.value;
  if (!category) return;
  const values = cascadeLevels.value
    .map((level) => cascadePicked.value[level.key])
    .filter((value): value is string => Boolean(value));
  if (values.length === 0) return;
  addFilter(category.type, values.join(";"), cascadeDisplay.value);
}

watch(searchQuery, () => {
  emitFilters();
});
</script>

<template>
  <div class="space-y-3">
    <!-- Main Search Bar -->
    <div
      class="flex items-center flex-wrap gap-2 min-h-11 w-full rounded-md border border-input bg-background px-3 py-2 text-sm transition-colors hover:border-ring focus-within:outline-hidden"
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
          :aria-label="t('common.removeFilter')"
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
        :placeholder="activeFilters.length === 0 ? (searchPlaceholder ?? t('databaseManagement.searchPlaceholder')) : ''"
        class="flex-1 min-w-[120px] bg-transparent outline-hidden placeholder:text-muted-foreground"
      >

      <!-- Add Filter Button -->
      <Popover v-model:open="showFilterMenu">
        <PopoverTrigger as-child>
          <Button
            v-if="availableCategories.length > 0"
            variant="ghost"
            size="sm"
            class="h-6 px-2 text-xs shrink-0"
          >
            <Plus class="h-3 w-3 mr-1" />
            {{ t("common.filter.add") }}
          </Button>
        </PopoverTrigger>
        <PopoverContent
          class="w-[320px] p-0"
          align="start"
        >
          <!-- Category selection -->
          <Command
            v-if="!selectedCategory"
            v-model="categorySearchQuery"
          >
            <CommandInput :placeholder="t('common.filter.search')" />
            <CommandList>
              <CommandEmpty>{{ t("common.filter.none") }}</CommandEmpty>
              <CommandGroup>
                <CommandItem
                  v-for="category in availableCategories"
                  :key="category.type"
                  :value="category.type"
                  @select="selectCategory(category.type)"
                >
                  <span class="mr-2">{{ category.icon }}</span>
                  <span>{{ category.label }}</span>
                  <ChevronDown class="ml-auto h-4 w-4 -rotate-90" />
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>

          <!-- One-level options, static or loaded -->
          <Command
            v-else-if="isOptionsKind"
            v-model="filterSearchQuery"
          >
            <div class="flex items-center border-b px-3">
              <Button
                variant="ghost"
                size="sm"
                class="h-8 px-2"
                :aria-label="t('common.back')"
                @click="backToCategories"
              >
                <ChevronDown class="h-4 w-4 rotate-90" />
              </Button>
              <CommandInput
                :placeholder="selectedCategory.label"
                class="border-0"
              />
            </div>
            <CommandList>
              <CommandEmpty>
                {{ isLoadingOptions ? t("common.loading") : t("common.filter.none") }}
              </CommandEmpty>
              <CommandGroup>
                <CommandItem
                  v-for="option in visibleOptions"
                  :key="option.value"
                  :value="option.value"
                  @select="applyOption(option)"
                >
                  {{ option.label }}
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>

          <!-- Cascading levels -->
          <div v-else-if="isCascadeKind">
            <div class="flex items-center gap-2 border-b px-3 py-2">
              <Button
                variant="ghost"
                size="sm"
                class="h-8 px-2"
                :aria-label="t('common.back')"
                @click="cascadeBack"
              >
                <ChevronDown class="h-4 w-4 rotate-90" />
              </Button>
              <div class="flex min-w-0 flex-wrap items-center gap-1 text-xs">
                <template
                  v-for="(level, index) in cascadeLevels"
                  :key="level.key"
                >
                  <span
                    v-if="index > 0"
                    class="text-muted-foreground"
                  >&gt;</span>
                  <span
                    :class="index === cascadeLevelIndex ? 'font-semibold text-foreground' : 'text-muted-foreground'"
                  >
                    {{ cascadeLabels[level.key] ?? level.label }}
                  </span>
                </template>
              </div>
            </div>
            <Command v-model="filterSearchQuery">
              <CommandInput :placeholder="currentLevel?.label ?? ''" />
              <CommandList>
                <CommandEmpty>
                  {{ isLoadingOptions ? t("common.loading") : t("common.filter.none") }}
                </CommandEmpty>
                <CommandGroup>
                  <CommandItem
                    v-for="option in visibleCascadeOptions"
                    :key="option.value"
                    :value="option.value"
                    @select="pickCascadeOption(option)"
                  >
                    {{ option.label }}
                  </CommandItem>
                </CommandGroup>
              </CommandList>
            </Command>
            <div class="flex items-center justify-between gap-2 border-t p-3">
              <span class="min-w-0 truncate text-xs text-muted-foreground">
                {{ cascadeDisplay }}
              </span>
              <Button
                size="sm"
                :disabled="!cascadeDisplay"
                @click="applyCascade"
              >
                {{ t("common.apply") }}
              </Button>
            </div>
          </div>

          <!-- Free text or tags -->
          <div
            v-else
            class="p-3"
          >
            <div class="mb-3 flex items-center gap-2 border-b pb-3">
              <Button
                variant="ghost"
                size="sm"
                class="h-8 px-2"
                :aria-label="t('common.back')"
                @click="backToCategories"
              >
                <ChevronDown class="h-4 w-4 rotate-90" />
              </Button>
              <div class="text-sm font-medium">
                {{ selectedCategory.label }}
              </div>
            </div>

            <input
              v-if="isInputKind"
              v-model="inputValue"
              type="text"
              :placeholder="selectedCategory.placeholder"
              class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-hidden transition-colors focus-visible:ring-2 focus-visible:ring-ring"
              @keydown.enter.prevent="applyInput"
            >

            <div
              v-else-if="isTagsKind"
              class="space-y-2"
            >
              <div class="flex min-h-10 w-full flex-wrap items-center gap-2 rounded-md border border-input bg-background px-3 py-2">
                <Badge
                  v-for="tag in tagValues"
                  :key="tag"
                  variant="secondary"
                  class="flex items-center gap-1"
                >
                  <span>{{ tag }}</span>
                  <button
                    type="button"
                    class="rounded-full p-0.5 text-muted-foreground transition-colors hover:text-foreground"
                    :aria-label="t('common.removeTag', { tag })"
                    @click="removeTag(tag)"
                  >
                    <X class="h-3 w-3" />
                  </button>
                </Badge>
                <input
                  v-model="tagDraft"
                  type="text"
                  :placeholder="selectedCategory.placeholder"
                  class="min-w-40 flex-1 bg-transparent text-sm outline-hidden placeholder:text-muted-foreground"
                  @keydown="handleTagKeydown"
                  @blur="commitTagDraft"
                >
              </div>
              <p
                v-if="selectedCategory.hint"
                class="text-xs text-muted-foreground"
              >
                {{ selectedCategory.hint }}
              </p>
              <p
                v-if="selectedCategory.note"
                class="text-xs text-muted-foreground"
              >
                {{ selectedCategory.note }}
              </p>
            </div>

            <p
              v-else-if="selectedCategory.hint"
              class="mt-2 text-xs text-muted-foreground"
            >
              {{ selectedCategory.hint }}
            </p>

            <div class="mt-3 flex justify-end">
              <Button
                size="sm"
                :disabled="isInputKind ? !inputValue.trim() : (tagValues.length === 0 && !tagDraft.trim())"
                @click="isInputKind ? applyInput() : applyTags()"
              >
                {{ t("common.apply") }}
              </Button>
            </div>
          </div>
        </PopoverContent>
      </Popover>
    </div>

    <!-- Active Filters Summary -->
    <div
      v-if="activeFilters.length > 0 || searchQuery"
      class="flex items-center justify-between text-xs text-muted-foreground"
    >
      <span>
        {{ t("common.filter.active", { count: activeFilters.length + (searchQuery ? 1 : 0) }) }}
      </span>
      <Button
        variant="ghost"
        size="sm"
        class="h-auto p-0 text-xs hover:underline"
        @click="clearAll"
      >
        {{ t("common.filter.clear") }}
      </Button>
    </div>
  </div>
</template>
