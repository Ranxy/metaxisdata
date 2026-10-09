<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button
        variant="ghost"
        :title="appStore.sidebarCollapsed ? userName : undefined"
        :aria-label="appStore.sidebarCollapsed ? userName : undefined"
        :class="
          appStore.sidebarCollapsed
            ? 'mx-auto h-10 w-10 justify-center p-0'
            : 'h-auto w-full justify-start gap-2 px-2 py-2'
        "
      >
        <Avatar class="h-7 w-7 shrink-0">
          <AvatarFallback class="bg-primary text-primary-foreground text-xs">
            {{ userInitial }}
          </AvatarFallback>
        </Avatar>

        <template v-if="!appStore.sidebarCollapsed">
          <span class="min-w-0 flex-1 text-left">
            <span class="block truncate text-sm font-medium leading-tight">
              {{ userName }}
            </span>
            <span
              v-if="userEmail"
              class="block truncate text-xs leading-tight text-muted-foreground"
            >
              {{ userEmail }}
            </span>
          </span>
          <ChevronUp class="h-4 w-4 shrink-0 text-muted-foreground" />
        </template>
      </Button>
    </DropdownMenuTrigger>

    <DropdownMenuContent
      side="top"
      align="start"
      class="w-56"
    >
      <DropdownMenuLabel class="font-normal">
        <div class="flex flex-col space-y-1">
          <p class="text-sm font-medium leading-none">
            {{ userName }}
          </p>
          <p class="text-xs leading-none text-muted-foreground">
            {{ userEmail }}
          </p>
        </div>
      </DropdownMenuLabel>
      <DropdownMenuSeparator />

      <!-- Language and theme are set-once preferences, so they live behind the
           avatar instead of occupying permanent header space. -->
      <DropdownMenuSub>
        <DropdownMenuSubTrigger>
          <Languages class="mr-2 h-4 w-4" />
          {{ t("header.language") }}
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent>
          <DropdownMenuItem
            v-for="item in locales"
            :key="item.value"
            @click="appStore.setLocale(item.value)"
          >
            <span class="flex-1">{{ item.label }}</span>
            <Check
              v-if="currentLocale === item.value"
              class="h-4 w-4"
            />
          </DropdownMenuItem>
        </DropdownMenuSubContent>
      </DropdownMenuSub>

      <DropdownMenuSub>
        <DropdownMenuSubTrigger>
          <component
            :is="currentThemeIcon"
            class="mr-2 h-4 w-4"
          />
          {{ t("header.theme") }}
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent>
          <DropdownMenuItem
            v-for="item in themes"
            :key="item.value"
            @click="appStore.setTheme(item.value)"
          >
            <component
              :is="item.icon"
              class="mr-2 h-4 w-4"
            />
            <span class="flex-1">{{ item.label }}</span>
            <Check
              v-if="appStore.theme === item.value"
              class="h-4 w-4"
            />
          </DropdownMenuItem>
        </DropdownMenuSubContent>
      </DropdownMenuSub>

      <DropdownMenuSeparator />

      <!-- The build metadata of the server that served this bundle, one click
           from any page (Settings → General shows the same values). It renders
           nothing when the server did not answer, so an old build stays clean
           instead of claiming a version it does not know. -->
      <div
        v-if="buildInfo"
        class="px-2 py-1.5 text-xs text-muted-foreground"
      >
        <div class="truncate">
          {{ t("common.version") }}: {{ buildInfo.version }}
        </div>
        <div class="truncate">
          {{ t("common.commit") }}: {{ shortCommit }}
        </div>
        <div class="truncate">
          {{ t("common.buildTime") }}: {{ buildInfo.build_time }}
        </div>
      </div>

      <DropdownMenuSeparator />

      <DropdownMenuItem
        class="text-destructive focus:text-destructive"
        @click="handleLogout"
      >
        <LogOut class="mr-2 h-4 w-4" />
        {{ t("header.logout") }}
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<script setup lang="ts">
import {
  Check,
  ChevronUp,
  Languages,
  LogOut,
  Monitor,
  Moon,
  Sun,
} from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useBuildInfo } from "@/composables/useBuildInfo";
import type { AppLocale } from "@/locales";
import { useAppStore } from "@/store/modules/app";
import { useAuthStore } from "@/store/modules/auth";

const { t } = useI18n();
const router = useRouter();
const appStore = useAppStore();
const authStore = useAuthStore();
const { buildInfo } = useBuildInfo();

const userName = computed(() => authStore.userName || "User");
const userEmail = computed(() => authStore.userEmail || "");
const userInitial = computed(() => userName.value.charAt(0).toUpperCase());

// The commit is a 40-character hash whose leading 8 characters identify it, and
// the full value would be truncated by the menu anyway.
const shortCommit = computed(() => buildInfo.value?.git_commit.slice(0, 8));

const locales: Array<{ value: AppLocale; label: string }> = [
  { value: "zh-CN", label: "简体中文" },
  { value: "en-US", label: "English" },
];

const themes = computed(() => [
  { value: "light" as const, label: t("header.themeLight"), icon: Sun },
  { value: "dark" as const, label: t("header.themeDark"), icon: Moon },
  { value: "system" as const, label: t("header.themeSystem"), icon: Monitor },
]);

const currentLocale = computed(() => appStore.locale);

const currentThemeIcon = computed(
  () =>
    themes.value.find((item) => item.value === appStore.theme)?.icon ?? Monitor
);

async function handleLogout() {
  await authStore.logout();
  router.push({ name: "Login" });
}
</script>
