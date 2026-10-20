<template>
  <div class="w-full space-y-2.5">
    <Label
      v-if="label"
      :for="fieldId"
    >
      {{ label }}
    </Label>

    <!-- Without users.list the control still shows the current owner read-only. -->
    <div
      v-if="!canList"
      :id="fieldId"
      class="flex h-10 w-full items-center rounded-md border border-input bg-muted/30 px-3 py-2 text-sm text-muted-foreground"
    >
      {{ selectedLabel || t("userSelect.permissionValue") }}
    </div>
    <p
      v-if="!canList"
      class="text-sm text-muted-foreground"
    >
      {{ t("userSelect.permissionHint") }}
    </p>

    <template v-else>
      <div class="relative">
        <Popover v-model:open="open">
          <PopoverTrigger as-child>
            <button
              :id="fieldId"
              type="button"
              :disabled="disabled"
              class="flex h-10 w-full items-center justify-between rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm transition-shadow focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
              :aria-invalid="loadError ? true : undefined"
            >
              <span
                v-if="selectedUser"
                class="flex min-w-0 items-center gap-2"
              >
                <span class="truncate">{{ selectedUser.email }}</span>
                <span
                  v-if="selectedUser.title"
                  class="shrink-0 truncate text-xs text-muted-foreground"
                >
                  {{ selectedUser.title }}
                </span>
              </span>
              <span
                v-else
                class="truncate text-muted-foreground"
              >
                {{ placeholder || t("userSelect.placeholder") }}
              </span>
              <ChevronDown class="h-4 w-4 shrink-0 opacity-50" />
            </button>
          </PopoverTrigger>

          <PopoverContent
            class="w-[var(--radix-popover-trigger-width)] p-0"
            align="start"
          >
            <Command
              :filter-function="identity"
              @update:model-value="choose"
            >
              <CommandInput
                :placeholder="t('userSelect.searchPlaceholder')"
                @input="onSearch"
              />
              <CommandList>
                <CommandEmpty>{{ t("userSelect.noMatch") }}</CommandEmpty>
                <CommandGroup>
                  <CommandItem
                    v-if="query === ''"
                    :value="NO_OWNER"
                  >
                    <span class="truncate">{{ t("userSelect.noOwner") }}</span>
                    <Check
                      v-if="!modelValue"
                      class="ml-auto h-4 w-4"
                    />
                  </CommandItem>
                  <CommandItem
                    v-for="user in visibleUsers"
                    :key="user.name"
                    :value="user.name"
                  >
                    <span class="truncate">{{ user.email }}</span>
                    <span
                      v-if="user.title"
                      class="ml-2 shrink-0 truncate text-xs text-muted-foreground"
                    >
                      {{ user.title }}
                    </span>
                    <Check
                      v-if="user.name === modelValue"
                      class="ml-auto h-4 w-4"
                    />
                  </CommandItem>
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>

        <button
          v-if="modelValue && !disabled"
          type="button"
          class="absolute right-8 top-1/2 -translate-y-1/2 rounded-sm p-0.5 text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          :aria-label="t('userSelect.clear')"
          :title="t('userSelect.clear')"
          @click.stop="clear"
        >
          <X class="h-3.5 w-3.5" />
        </button>
      </div>
      <p
        v-if="loadError"
        class="text-sm text-destructive"
      >
        {{ loadError }}
      </p>
      <p
        v-else-if="hint"
        class="text-sm text-muted-foreground"
      >
        {{ hint }}
      </p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { Check, ChevronDown, X } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listAllUsers } from "@/api/user";
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
import { useAuthStore } from "@/store/modules/auth";
import type { User } from "@/types/proto-es/v1/user_service_pb";

/**
 * A searchable workspace-member picker: the owner of a rule is a user resource
 * name, and typing `users/{id}` by hand was both error-prone and unusable for a
 * member who does not know the ids. Without `users.list` the field degrades to a
 * read-only value instead of calling an API the caller cannot read.
 */

defineOptions({ inheritAttrs: false });

const props = withDefaults(
  defineProps<{
    modelValue?: string;
    label?: string;
    hint?: string;
    placeholder?: string;
    disabled?: boolean;
    id?: string;
  }>(),
  {
    modelValue: "",
    label: "",
    hint: "",
    placeholder: "",
    disabled: false,
    id: "",
  }
);

const emit = defineEmits<{
  "update:modelValue": [value: string];
}>();

const { t } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const canList = computed(() =>
  authStore.hasPermission("metaxisdata.users.list")
);

const NO_OWNER = "__no_owner__";
const fieldId = computed(
  () => props.id || `user-select-${Math.random().toString(36).slice(2, 9)}`
);

const open = ref(false);
const searchText = ref("");
const users = ref<User[]>([]);
const loadError = ref("");
let loaded = false;

const selectedUser = computed(() =>
  users.value.find((user) => user.name === props.modelValue)
);
const selectedLabel = computed(
  () => selectedUser.value?.email || props.modelValue
);

/** The picker filters its own rows; radix's filter is a pass-through. */
function identity<T>(values: T): T {
  return values;
}

function onSearch(event: Event) {
  searchText.value = (event.target as HTMLInputElement).value;
}

const query = computed(() => searchText.value.trim().toLowerCase());

const visibleUsers = computed(() =>
  query.value === ""
    ? users.value
    : users.value.filter(
        (user) =>
          user.email.toLowerCase().includes(query.value) ||
          user.title.toLowerCase().includes(query.value) ||
          user.name.toLowerCase().includes(query.value)
      )
);

async function loadUsers() {
  if (loaded || !canList.value) {
    return;
  }
  try {
    users.value = await listAllUsers();
    loaded = true;
  } catch (error: unknown) {
    loadError.value = formatError(error, "userSelect.loadFailed");
  }
}

watch(open, (isOpen) => {
  if (isOpen) {
    void loadUsers();
  } else {
    searchText.value = "";
  }
});

onMounted(() => {
  void loadUsers();
});

function choose(value: unknown) {
  if (typeof value !== "string") {
    return;
  }
  open.value = false;
  emit("update:modelValue", value === NO_OWNER ? "" : value);
}

function clear() {
  emit("update:modelValue", "");
}
</script>
