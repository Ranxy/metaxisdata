<template>
  <aside
    :inert="!isDesktop && !appStore.mobileNavOpen ? true : undefined"
    :class="[
      'flex w-64 flex-col border-r bg-background',
      'fixed inset-y-0 left-0 z-50 transition-transform duration-200 lg:static lg:z-auto lg:translate-x-0',
      appStore.mobileNavOpen ? 'translate-x-0' : '-translate-x-full',
      appStore.sidebarCollapsed ? 'lg:w-16' : 'lg:w-64',
    ]"
  >
    <!-- Brand and rail controls. The collapse toggle is desktop-only; the
         drawer gets a close button instead.

         The toggle's `ml-auto` is deliberately conditional: the rail turns this
         row into a column, and in a column flex container an auto side margin
         absorbs the free space and overrides `items-center`, which pushed the
         toggle off the axis the brand and every nav icon share. -->
    <div
      :class="[
        'flex shrink-0 items-center gap-2 border-b py-3',
        rail ? 'flex-col px-2' : 'px-3',
      ]"
    >
      <router-link
        to="/"
        :title="rail ? brandName : undefined"
        class="flex min-w-0 items-center gap-2 rounded-md"
      >
        <span
          class="grid h-7 w-7 shrink-0 place-items-center rounded-md bg-primary text-sm font-bold text-primary-foreground"
        >
          M
        </span>
        <span
          v-if="!rail"
          class="truncate text-base font-bold tracking-tight"
        >
          {{ brandName }}
        </span>
      </router-link>

      <Button
        variant="ghost"
        size="icon"
        :class="['hidden h-8 w-8 lg:inline-flex', rail ? '' : 'ml-auto']"
        :title="appStore.sidebarCollapsed ? t('header.expandSidebar') : t('header.collapseSidebar')"
        :aria-label="appStore.sidebarCollapsed ? t('header.expandSidebar') : t('header.collapseSidebar')"
        @click="appStore.toggleSidebar"
      >
        <PanelLeftOpen
          v-if="appStore.sidebarCollapsed"
          :class="rail ? 'h-5 w-5' : 'h-4 w-4'"
        />
        <PanelLeftClose
          v-else
          :class="rail ? 'h-5 w-5' : 'h-4 w-4'"
        />
      </Button>

      <Button
        variant="ghost"
        size="icon"
        class="ml-auto h-8 w-8 lg:hidden"
        :aria-label="t('header.closeNav')"
        @click="appStore.setMobileNavOpen(false)"
      >
        <X class="h-4 w-4" />
      </Button>
    </div>

    <nav class="flex-1 overflow-y-auto py-2">
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
              v-if="!rail"
              class="ml-3 truncate"
            >
              {{ item.label }}
            </span>
          </router-link>

          <!-- Collapsible Section -->
          <template v-else>
            <!-- Icon rail: a header would not fit, so the group's members read as
                 a flat run of icons and a rule marks where one group ends. -->
            <template v-if="rail">
              <div
                v-if="hasRailDivider(item)"
                class="mx-auto my-1.5 h-px w-6 bg-border"
                aria-hidden="true"
              />
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
                  'flex w-full items-center justify-between gap-2 rounded-md px-4 py-2 text-xs font-semibold uppercase tracking-wider transition-colors',
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

    <!-- Set-once controls (account, language, theme) live at the bottom of the
         rail instead of occupying a permanent top bar. -->
    <div class="shrink-0 border-t p-2">
      <UserMenu />
    </div>
  </aside>
</template>

<script setup lang="ts">
import { useMediaQuery } from "@vueuse/core";
import {
  ChevronRight,
  Database,
  FileCode2,
  Files,
  Home,
  LayoutDashboard,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
  Server,
  Settings,
  Sparkles,
  Table2,
  X,
} from "lucide-vue-next";
import { computed, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { useAppStore } from "@/store/modules/app";
import { useAuthStore } from "@/store/modules/auth";
import UserMenu from "./UserMenu.vue";

const { t } = useI18n();
const route = useRoute();
const appStore = useAppStore();
const authStore = useAuthStore();

const brandName = "MetaxisData";

// Below `lg` the sidebar is an overlay drawer, so the rail never applies there.
const isDesktop = useMediaQuery("(min-width: 1024px)");
const rail = computed(() => isDesktop.value && appStore.sidebarCollapsed);

interface MenuItem {
  key: string;
  label: string;
  path: string;
  icon: typeof Home;
  /** The permission that makes this entry reachable; the server enforces it. */
  permission?: string;
  /** Visible when the caller holds at least one of these. */
  anyPermission?: string[];
  children?: MenuItem[];
}

// Settings is one destination with its own in-page navigation, so the nine
// administration pages no longer occupy nine permanent sidebar rows.
const SETTINGS_PERMISSIONS = [
  "metaxisdata.settings.get",
  "metaxisdata.iam.getPolicy",
  "metaxisdata.roles.list",
  "metaxisdata.groups.list",
  "metaxisdata.users.list",
  "metaxisdata.auditLogs.search",
  "metaxisdata.llm.profiles.list",
  "metaxisdata.openlineage.namespaceMappings.list",
];

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
          icon: Server,
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
          icon: Table2,
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
      path: "/settings",
      icon: Settings,
      anyPermission: SETTINGS_PERMISSIONS,
    },
  ];
}

// The settings sub-navigation labels, kept next to the sidebar so the two
// cannot drift apart.
const menuItems = computed<MenuItem[]>(() => {
  const allowed = (item: MenuItem) =>
    (!item.permission || authStore.hasPermission(item.permission)) &&
    (!item.anyPermission ||
      item.anyPermission.some((permission) =>
        authStore.hasPermission(permission)
      ));
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
    rail.value
      ? "mx-auto flex h-10 w-10 items-center justify-center rounded-md transition-colors"
      : "flex items-center rounded-md px-4 py-2 transition-colors",
    isActive(path) ? "bg-accent text-accent-foreground" : idle,
  ];
}

// Icon-rail links keep their name as a hover tooltip and screen-reader label.
function railLabel(label: string): string | undefined {
  return rail.value ? label : undefined;
}

// The rail hides section headers, so a rule stands in for them. The first
// group needs none: the top-level entries above it already separate it.
function hasRailDivider(item: MenuItem): boolean {
  return menuItems.value.findIndex((entry) => entry.key === item.key) > 0;
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

// The drawer is a modal overlay, so a navigation has to dismiss it.
watch(
  () => route.path,
  () => appStore.setMobileNavOpen(false)
);
</script>
