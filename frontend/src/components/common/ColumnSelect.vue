<template>
  <div class="w-full space-y-2.5">
    <Label
      v-if="label"
      :for="fieldId"
    >
      {{ label }}
      <span
        v-if="required"
        class="text-destructive"
      >*</span>
    </Label>

    <div class="relative">
      <Popover v-model:open="open">
        <PopoverTrigger as-child>
          <button
            :id="fieldId"
            type="button"
            :disabled="disabled"
            :aria-required="required ? true : undefined"
            :aria-invalid="error ? true : undefined"
            :class="[
              'flex min-h-10 w-full items-center justify-between gap-2 rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm transition-shadow focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50',
              error ? 'border-destructive focus-visible:ring-destructive' : '',
            ]"
          >
            <span
              v-if="selected.length > 0"
              class="flex min-w-0 flex-1 flex-wrap items-center gap-1"
            >
              <template v-if="multiple">
                <Badge
                  v-for="column in selected"
                  :key="column.name"
                  variant="secondary"
                  class="max-w-full truncate"
                >
                  {{ column.name }}
                </Badge>
              </template>
              <template v-else>
                <span class="min-w-0 flex-1 truncate font-medium">{{ selected[0]?.name }}</span>
                <Badge
                  v-if="selected[0]?.type"
                  variant="outline"
                  class="max-w-[50%] overflow-hidden"
                >
                  <span class="truncate">{{ selected[0]?.type }}</span>
                </Badge>
              </template>
            </span>
            <span
              v-else
              class="truncate text-muted-foreground"
            >
              {{ placeholder || t("columnSelect.placeholder") }}
            </span>
            <ChevronDown class="h-4 w-4 shrink-0 opacity-50" />
          </button>
        </PopoverTrigger>

        <PopoverContent
          class="w-[var(--radix-popover-trigger-width)] min-w-72 max-w-[calc(100vw-2rem)] p-0"
          align="start"
        >
          <Command
            :filter-function="identity"
            @update:model-value="choose"
          >
            <CommandInput
              :placeholder="t('columnSelect.searchPlaceholder')"
              @input="onSearch"
            />
            <CommandList class="max-h-[min(300px,calc(var(--radix-popper-available-height,22rem)-3rem))]">
              <CommandEmpty>{{ t("columnSelect.noMatch") }}</CommandEmpty>
              <CommandGroup>
                <CommandItem
                  v-for="column in visibleColumns"
                  :key="column.name"
                  :value="column.name"
                >
                  <span class="min-w-0 flex-1 truncate font-medium">{{ column.name }}</span>
                  <Badge
                    v-if="column.type"
                    variant="outline"
                    class="ml-2 max-w-[45%] overflow-hidden"
                  >
                    <span class="truncate">{{ column.type }}</span>
                  </Badge>
                  <Check
                    v-if="isSelected(column.name)"
                    class="ml-2 h-4 w-4 shrink-0"
                  />
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>

      <button
        v-if="selected.length > 0 && !disabled"
        type="button"
        class="absolute right-8 top-1/2 -translate-y-1/2 rounded-sm p-0.5 text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        :aria-label="t('columnSelect.clear')"
        :title="t('columnSelect.clear')"
        @click.stop="clear"
      >
        <X class="h-3.5 w-3.5" />
      </button>
    </div>

    <p
      v-if="error"
      class="text-sm text-destructive"
    >
      {{ error }}
    </p>
    <p
      v-else-if="hint"
      class="text-sm text-muted-foreground"
    >
      {{ hint }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { Check, ChevronDown, X } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
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
import type { TargetColumnDetail } from "@/utils/dataQualityForm";

/**
 * A searchable column picker for the rule config and its pre-filter: the old
 * text input with a `datalist` hid the column type and could not be searched
 * usefully. Multi-select serves the CSV fields (unique combination, grouping,
 * CEL columns) without a second component.
 */

const props = withDefaults(
  defineProps<{
    modelValue?: string | string[];
    /** The target's columns; the picker renders what the registry reported. */
    columns: TargetColumnDetail[];
    multiple?: boolean;
    label?: string;
    hint?: string;
    placeholder?: string;
    disabled?: boolean;
    required?: boolean;
    error?: string;
    id?: string;
  }>(),
  {
    modelValue: "",
    multiple: false,
    label: "",
    hint: "",
    placeholder: "",
    disabled: false,
    required: false,
    error: "",
    id: "",
  }
);

const emit = defineEmits<{
  "update:modelValue": [value: string | string[]];
}>();

const { t } = useI18n();

const fieldId = computed(
  () => props.id || `column-select-${Math.random().toString(36).slice(2, 9)}`
);

const open = ref(false);
const searchText = ref("");

const selectedNames = computed(() =>
  props.multiple
    ? Array.isArray(props.modelValue)
      ? props.modelValue
      : []
    : typeof props.modelValue === "string" && props.modelValue
      ? [props.modelValue]
      : []
);

const selected = computed(() =>
  selectedNames.value.map(
    (name) =>
      props.columns.find((column) => column.name === name) ?? {
        name,
        type: "",
        nullable: true,
      }
  )
);

function isSelected(name: string): boolean {
  return selectedNames.value.includes(name);
}

/** The picker filters its own rows; radix's filter is a pass-through so the
 * list cannot be filtered twice by two different search fields. */
function identity<T>(values: T): T {
  return values;
}

function onSearch(event: Event) {
  searchText.value = (event.target as HTMLInputElement).value;
}

const visibleColumns = computed(() => {
  const search = searchText.value.trim().toLowerCase();
  return search
    ? props.columns.filter((column) =>
        column.name.toLowerCase().includes(search)
      )
    : props.columns;
});

watch(open, (isOpen) => {
  if (!isOpen) {
    searchText.value = "";
  }
});

function choose(value: unknown) {
  if (typeof value !== "string") {
    return;
  }
  if (!props.multiple) {
    open.value = false;
    emit("update:modelValue", value);
    return;
  }
  const next = isSelected(value)
    ? selectedNames.value.filter((name) => name !== value)
    : [...selectedNames.value, value];
  emit("update:modelValue", next);
}

function clear() {
  emit("update:modelValue", props.multiple ? [] : "");
}
</script>
