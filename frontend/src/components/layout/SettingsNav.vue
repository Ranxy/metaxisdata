<template>
  <!-- On a phone the nine entries are one horizontally scrolling chip row:
       wrapping them stacked three rows above the page title (the h1 started at
       y=236 instead of y=88). From `md` the rail is the pinned vertical list it
       always was; the layout passes the sticky offsets. -->
  <nav
    class="flex gap-1 overflow-x-auto pb-1 [-ms-overflow-style:none] [mask-image:linear-gradient(to_right,black_calc(100%-1.5rem),transparent)] [scrollbar-width:none] md:flex-col md:gap-0.5 md:overflow-visible md:pb-0 md:[mask-image:none] [&::-webkit-scrollbar]:hidden"
    :aria-label="t('menu.settings')"
  >
    <router-link
      v-for="item in items"
      :key="item.key"
      :to="item.path"
      :class="[
        'flex shrink-0 items-center gap-2 whitespace-nowrap rounded-md px-3 py-2 text-sm transition-colors md:shrink',
        isActive(item.path)
          ? 'bg-accent font-medium text-accent-foreground'
          : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
      ]"
    >
      <component
        :is="item.icon"
        class="h-4 w-4 shrink-0"
      />
      <span class="truncate">{{ item.label }}</span>
    </router-link>
  </nav>
</template>

<script setup lang="ts">
import {
  AppWindow,
  ClipboardList,
  Gauge,
  Globe,
  KeyRound,
  Link2,
  Network,
  Shield,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  UserRound,
  Users,
} from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import { useAuthStore } from "@/store/modules/auth";

const { t } = useI18n();
const route = useRoute();
const authStore = useAuthStore();

// Settings is a local section: its pages live behind one sidebar entry and are
// navigated here, so they stop competing with the product navigation.
const ALL_ITEMS = computed(() => [
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
    key: "dataQuality",
    label: t("menu.dataQualitySettings"),
    path: "/settings/data-quality",
    icon: Gauge,
    permission: "metaxisdata.settings.get",
  },
  // The policy page and the account list it governs are one job, so they stay
  // adjacent: a reader looking for "who is in this workspace" should not have
  // to notice that the answer is spread across two entries.
  {
    key: "iam",
    label: t("menu.iam"),
    path: "/settings/iam",
    icon: KeyRound,
    permission: "metaxisdata.iam.getPolicy",
  },
  {
    key: "users",
    label: t("menu.users"),
    path: "/settings/users",
    icon: Users,
    permission: "metaxisdata.users.list",
  },
  {
    key: "identityProviders",
    label: t("menu.identityProviders"),
    path: "/settings/identity-providers",
    icon: ShieldCheck,
    permission: "metaxisdata.identityProviders.list",
  },
  // The connected-app entry takes the page's own title key, for the same reason
  // the applications entry below does: the entry and the heading it opens have
  // to read the same, and two keys drift apart.
  {
    key: "connectedApps",
    label: t("connectedApps.title"),
    path: "/settings/connected-apps",
    icon: Link2,
    permission: "metaxisdata.connectedApps.list",
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
    key: "llmProviders",
    label: t("llmProvider.sidebar"),
    path: "/settings/llm-providers",
    icon: Sparkles,
    permission: "metaxisdata.llm.profiles.list",
  },
  // The two ingestion surfaces sit together, after everything an administrator sets up
  // once for the workspace itself. The application-SQL entry takes the page's own title
  // key rather than holding a second copy of the string: the entry and the heading it
  // opens have to read the same, and two keys drift apart.
  {
    key: "applications",
    label: t("applicationManagement.title"),
    path: "/settings/applications",
    icon: AppWindow,
    permission: "metaxisdata.applications.list",
  },
  {
    key: "openlineage",
    label: t("openlineage.ingestionSettings"),
    path: "/settings/openlineage",
    icon: Network,
    permission: "metaxisdata.openlineage.namespaceMappings.list",
  },
  // The audit ledger is a read-only record of what the rest of the section
  // configured, not a setting, so it closes the list rather than interleaving
  // with the pages an administrator changes.
  {
    key: "auditLogs",
    label: t("menu.auditLogs"),
    path: "/settings/audit-logs",
    icon: ClipboardList,
    permission: "metaxisdata.auditLogs.search",
  },
]);

const items = computed(() =>
  ALL_ITEMS.value.filter(
    (item) => !item.permission || authStore.hasPermission(item.permission)
  )
);

function isActive(path: string): boolean {
  return route.path === path || route.path.startsWith(`${path}/`);
}
</script>
