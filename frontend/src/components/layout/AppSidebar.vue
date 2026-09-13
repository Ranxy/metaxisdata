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
  ClipboardList,
  Database,
  FileCode2,
  Files,
  Globe,
  Home,
  KeyRound,
  LayoutDashboard,
  Network,
  Settings,
  Shield,
  SlidersHorizontal,
  Sparkles,
  UserRound,
  Users,
} from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { useAppStore } from "@/store/modules/app";
import { useAuthStore } from "@/store/modules/auth";

const { t } = useI18n();
const route = useRoute();
const appStore = useAppStore();
const authStore = useAuthStore();

interface MenuItem {
  key: string;
  label: string;
  path: string;
  icon: typeof Home;
  /** The permission that makes this entry reachable; the server enforces it. */
  permission?: string;
  children?: MenuItem[];
}

function buildMenuItems(): MenuItem[] {
  return [
    {
      key: "home",
      label: t("menu.home"),
      path: "/",
      icon: Home,
    },
    {
      key: "explainSQL",
      label: t("menu.explainSQL"),
      path: "/explain-sql",
      icon: Sparkles,
      permission: "metaxisdata.explainSql.explain",
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
          permission: "metaxisdata.instances.list",
        },
        {
          key: "databases",
          label: t("menu.databases"),
          path: "/databases",
          icon: Database,
          permission: "metaxisdata.databases.list",
        },
        {
          key: "metadata",
          label: t("menu.metadata"),
          path: "/metadata",
          icon: Database,
          permission: "metaxisdata.databases.read",
        },
        {
          key: "manualSql",
          label: t("menu.manualSql"),
          path: "/manual-sql",
          icon: FileCode2,
          permission: "metaxisdata.manualSqls.list",
        },
      ],
    },
    {
      key: "openlineage",
      label: t("menu.openlineage"),
      path: "#",
      icon: Network,
      children: [
        {
          key: "openlineageOverview",
          label: t("menu.overview"),
          path: "/openlineage/overview",
          icon: LayoutDashboard,
          permission: "metaxisdata.openlineage.read",
        },
        {
          key: "openlineageJobs",
          label: t("menu.jobs"),
          path: "/openlineage/jobs",
          icon: Network,
          permission: "metaxisdata.openlineage.read",
        },
        {
          key: "openlineageDatasets",
          label: t("menu.datasets"),
          path: "/openlineage/datasets",
          icon: Database,
          permission: "metaxisdata.openlineage.read",
        },
        {
          key: "openlineageEvents",
          label: t("menu.events"),
          path: "/openlineage/events",
          icon: Files,
          permission: "metaxisdata.openlineage.read",
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
          key: "general",
          label: t("menu.generalSettings"),
          path: "/settings/general",
          icon: SlidersHorizontal,
          permission: "metaxisdata.settings.get",
        },
        {
          key: "environments",
          label: t("menu.environments"),
          path: "/settings/environments",
          icon: Globe,
          permission: "metaxisdata.settings.get",
        },
        {
          key: "iam",
          label: t("menu.iam"),
          path: "/settings/iam",
          icon: KeyRound,
          permission: "metaxisdata.iam.getPolicy",
        },
        {
          key: "roles",
          label: t("menu.roles"),
          path: "/settings/roles",
          icon: Shield,
          permission: "metaxisdata.roles.list",
        },
        {
          key: "groups",
          label: t("menu.groups"),
          path: "/settings/groups",
          icon: UserRound,
          permission: "metaxisdata.groups.list",
        },
        {
          key: "users",
          label: t("menu.users"),
          path: "/settings/users",
          icon: Users,
          permission: "metaxisdata.users.list",
        },
        {
          key: "auditLogs",
          label: t("menu.auditLogs"),
          path: "/settings/audit-logs",
          icon: ClipboardList,
          permission: "metaxisdata.auditLogs.search",
        },
        {
          key: "llmProviders",
          label: t("llmProvider.sidebar"),
          path: "/settings/llm-providers",
          icon: Sparkles,
          permission: "metaxisdata.llm.profiles.list",
        },
        {
          key: "openlineage",
          label: t("openlineage.ingestionSettings"),
          path: "/settings/openlineage",
          icon: Network,
          permission: "metaxisdata.openlineage.namespaceMappings.list",
        },
      ],
    },
  ];
}

// An entry is hidden when the caller lacks its permission; a parent is hidden
// when it has no reachable child left. This only avoids dead ends — every RPC
// is independently authorized by the ACL interceptor.
const menuItems = computed<MenuItem[]>(() => {
  const allowed = (item: MenuItem) =>
    !item.permission || authStore.hasPermission(item.permission);
  return buildMenuItems()
    .filter(allowed)
    .map((item) =>
      item.children
        ? { ...item, children: item.children.filter(allowed) }
        : item
    )
    .filter((item) => !item.children || item.children.length > 0);
});

function isActive(path: string): boolean {
  if (path === "/") {
    return route.path === path;
  }

  return route.path === path || route.path.startsWith(`${path}/`);
}
</script>
