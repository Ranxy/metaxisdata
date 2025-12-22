<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button
        variant="ghost"
        size="sm"
        class="gap-1"
      >
        <Languages class="h-4 w-4" />
        <span class="text-sm">{{ currentLocaleLabel }}</span>
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end">
      <DropdownMenuItem
        v-for="item in locales"
        :key="item.value"
        :class="currentLocale === item.value ? 'bg-accent' : ''"
        @click="changeLocale(item.value)"
      >
        <span class="flex-1">{{ item.label }}</span>
        <Check
          v-if="currentLocale === item.value"
          class="h-4 w-4"
        />
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<script setup lang="ts">
import { Check, Languages } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useAppStore } from "@/store/modules/app";

const { locale } = useI18n();
const appStore = useAppStore();

const locales = [
  { value: "zh-CN", label: "简体中文" },
  { value: "en-US", label: "English" },
];

const currentLocale = computed(() => appStore.locale);

const currentLocaleLabel = computed(() => {
  const found = locales.find((l) => l.value === currentLocale.value);
  return found ? found.label : "Language";
});

function changeLocale(newLocale: string) {
  appStore.setLocale(newLocale);
  locale.value = newLocale as "zh-CN" | "en-US";
}
</script>
