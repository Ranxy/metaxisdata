<template>
  <aside
    :class="[
      'bg-background border-r transition-all duration-300 flex flex-col',
      appStore.sidebarCollapsed ? 'w-16' : 'w-64',
    ]"
  >
    <nav class="flex-1 py-4 overflow-y-auto">
      <ul class="space-y-1">
        <li
          v-for="item in menuItems"
          :key="item.key"
        >
          <!-- Menu Section Header -->
          <div
            v-if="item.children && !appStore.sidebarCollapsed"
            class="px-4 py-2 text-xs font-semibold text-muted-foreground uppercase tracking-wider"
          >
            {{ item.label }}
          </div>

          <!-- Menu Item without children -->
          <router-link
            v-if="!item.children"
            :to="item.path"
            :class="[
              'flex items-center px-4 py-2 mx-2 rounded-md transition-colors',
              isActive(item.path)
                ? 'bg-accent text-accent-foreground'
                : 'text-foreground hover:bg-accent hover:text-accent-foreground',
            ]"
          >
            <component
              :is="item.icon"
              class="h-5 w-5 flex-shrink-0"
            />
            <span
              v-if="!appStore.sidebarCollapsed"
              class="ml-3 truncate"
            >
              {{ item.label }}
            </span>
          </router-link>

          <!-- Child Menu Items -->
          <template v-if="item.children">
            <router-link
              v-for="child in item.children"
              :key="child.key"
              :to="child.path"
              :class="[
                'flex items-center px-4 py-2 mx-2 rounded-md transition-colors',
                isActive(child.path)
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground',
              ]"
            >
              <component
                :is="child.icon"
                class="h-5 w-5 flex-shrink-0"
              />
              <span
                v-if="!appStore.sidebarCollapsed"
                class="ml-3 truncate"
              >
                {{ child.label }}
              </span>
            </router-link>
          </template>
        </li>
      </ul>
    </nav>
  </aside>
</template>

<script setup lang="ts">
import {
  Database,
  FileCode2,
  Home,
  Network,
  Settings,
  Users,
} from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { useAppStore } from "@/store/modules/app";

const { t } = useI18n();
const route = useRoute();
const appStore = useAppStore();

interface MenuItem {
  key: string;
  label: string;
  path: string;
  icon: typeof Home;
  children?: MenuItem[];
}

const menuItems = computed<MenuItem[]>(() => [
  {
    key: "home",
    label: t("menu.home"),
    path: "/",
    icon: Home,
  },
  {
    key: "datasource",
    label: t("menu.datasource"),
    path: "#",
    icon: Database,
    children: [
      {
        key: "connections",
        label: t("menu.connections"),
        path: "/instances",
        icon: Database,
      },
      {
        key: "databases",
        label: t("menu.databases"),
        path: "/databases",
        icon: Database,
      },
      {
        key: "metadata",
        label: t("menu.metadata"),
        path: "/metadata",
        icon: Database,
      },
      {
        key: "manualSql",
        label: t("menu.manualSql"),
        path: "/manual-sql",
        icon: FileCode2,
      },
    ],
  },
  {
    key: "settings",
    label: t("menu.settings"),
    path: "#",
    icon: Settings,
    children: [
      {
        key: "users",
        label: t("menu.users"),
        path: "/settings/users",
        icon: Users,
      },
      {
        key: "openlineage",
        label: t("menu.openlineage"),
        path: "/settings/openlineage",
        icon: Network,
      },
    ],
  },
]);

function isActive(path: string): boolean {
  return route.path === path;
}
</script>
