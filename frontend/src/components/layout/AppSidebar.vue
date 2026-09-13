<template>
  <aside
    :class="[
      'bg-background border-r transition-all duration-300 flex flex-col',
      appStore.sidebarCollapsed ? 'w-16' : 'w-64',
    ]"
  >
    <nav class="flex-1 py-4 overflow-y-auto">
      <!-- The horizontal inset lives on the list: a <button> is shrink-to-fit
           even with `display:flex`, so the section header needs `w-full` and
           must not also carry side margins (that overflows the rail). -->
      <ul class="space-y-1 px-2">
        <li
          v-for="item in menuItems"
          :key="item.key"
        >
          <!-- Menu Item without children -->
          <router-link
            v-if="!item.children"
            :to="item.path"
            :title="railLabel(item.label)"
            :aria-label="railLabel(item.label)"
            :class="navLinkClass(item.path)"
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

          <!-- Collapsible Section -->
          <template v-else>
            <!-- Icon rail: a header would not fit, so the section stays flat. -->
            <template v-if="appStore.sidebarCollapsed">
              <router-link
                v-for="child in item.children"
                :key="child.key"
                :to="child.path"
                :title="railLabel(child.label)"
                :aria-label="railLabel(child.label)"
                :class="navLinkClass(child.path, { muted: true })"
              >
                <component
                  :is="child.icon"
                  class="h-5 w-5 flex-shrink-0"
                />
              </router-link>
            </template>

            <Collapsible
              v-else
              :open="isSectionExpanded(item.key)"
              @update:open="(open: boolean) => appStore.setSectionCollapsed(item.key, !open)"
            >
              <CollapsibleTrigger
                :class="[
                  'flex w-full items-center justify-between gap-2 px-4 py-2 rounded-md text-xs font-semibold uppercase tracking-wider transition-colors',
                  hasActiveChild(item)
                    ? 'text-foreground'
                    : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
                ]"
              >
                <span class="truncate">{{ item.label }}</span>
                <ChevronRight
                  :class="[
                    'h-3.5 w-3.5 flex-shrink-0 transition-transform',
                    isSectionExpanded(item.key) && 'rotate-90',
                  ]"
                />
              </CollapsibleTrigger>

              <CollapsibleContent>
                <router-link
                  v-for="child in item.children"
                  :key="child.key"
                  :to="child.path"
                  :class="navLinkClass(child.path, { muted: true })"
                >
                  <component
                    :is="child.icon"
                    class="h-5 w-5 flex-shrink-0"
                  />
                  <span class="ml-3 truncate">
                    {{ child.label }}
                  </span>
                </router-link>
              </CollapsibleContent>
            </Collapsible>
          </template>
        </li>
      </ul>
    </nav>
  </aside>
</template>

<script setup lang="ts">
import {
  ChevronRight,
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
import { computed, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
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

interface NavLinkOptions {
  /** Section children sit below a header, so their idle label reads muted. */
  muted?: boolean;
}

// The icon rail hides labels, so a link becomes a centred square that fills the
// rail evenly instead of a left-aligned row with a wide empty right side. The
// horizontal inset lives on the list, so the section header's `w-full` (a
// `<button>` never fills its parent on its own) stays inside the sidebar.
function navLinkClass(path: string, options: NavLinkOptions = {}): string[] {
  const idle = options.muted
    ? "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
    : "text-foreground hover:bg-accent hover:text-accent-foreground";
  return [
    appStore.sidebarCollapsed
      ? "mx-auto flex h-10 w-10 items-center justify-center rounded-md transition-colors"
      : "flex items-center rounded-md px-4 py-2 transition-colors",
    isActive(path) ? "bg-accent text-accent-foreground" : idle,
  ];
}

// Icon-rail links keep their name as a hover tooltip and screen-reader label.
function railLabel(label: string): string | undefined {
  return appStore.sidebarCollapsed ? label : undefined;
}

function isSectionExpanded(key: string): boolean {
  return !appStore.collapsedSections.includes(key);
}

function hasActiveChild(item: MenuItem): boolean {
  return (item.children ?? []).some((child) => isActive(child.path));
}

// A persisted collapsed section must not hide the page the user is on, so
// navigation into a section reopens it. Collapsing does not change the route,
// which keeps the user's choice intact while they stay on the current page.
watch(
  () => route.path,
  () => {
    for (const item of menuItems.value) {
      if (
        item.children &&
        !isSectionExpanded(item.key) &&
        hasActiveChild(item)
      ) {
        appStore.setSectionCollapsed(item.key, false);
      }
    }
  },
  { immediate: true }
);
</script>
