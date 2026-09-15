<template>
  <nav
    class="flex flex-wrap gap-1 md:flex-col md:flex-nowrap md:gap-0.5"
    :aria-label="t('menu.settings')"
  >
    <router-link
      v-for="item in items"
      :key="item.key"
      :to="item.path"
      :class="[
        'flex items-center gap-2 whitespace-nowrap rounded-md px-3 py-2 text-sm transition-colors',
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
  ClipboardList,
  Globe,
  KeyRound,
  Network,
  Shield,
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

// Settings is a local section: its nine pages live behind one sidebar entry and
// are navigated here, so they stop competing with the product navigation.
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
