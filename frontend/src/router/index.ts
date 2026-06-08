import {
  createRouter,
  createWebHistory,
  type RouteRecordRaw,
} from "vue-router";
import { useAuthStore } from "@/store/modules/auth";

const routes: RouteRecordRaw[] = [
  {
    path: "/login",
    name: "Login",
    component: () => import("@/pages/LoginPage.vue"),
    meta: { requiresAuth: false, layout: "auth" },
  },
  {
    path: "/",
    name: "Home",
    component: () => import("@/pages/HomePage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/instances",
    name: "InstanceManagement",
    component: () => import("@/pages/InstanceManagementPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/instances/:instanceId",
    name: "InstanceDetail",
    component: () => import("@/pages/InstanceDetailPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/databases",
    name: "DatabaseManagement",
    component: () => import("@/pages/DatabaseManagementPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/settings/users",
    name: "UserManagement",
    component: () => import("@/pages/settings/UserManagementPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/settings/llm-providers",
    name: "LLMProviderManagement",
    component: () => import("@/pages/settings/LLMProviderManagementPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/settings/openlineage",
    name: "OpenLineageSettings",
    component: () => import("@/pages/settings/OpenLineageSettingsPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/settings/audit-logs",
    name: "AuditLogs",
    component: () => import("@/pages/settings/AuditLogsPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/openlineage",
    redirect: { name: "OpenLineageOverview" },
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/openlineage/overview",
    name: "OpenLineageOverview",
    component: () => import("@/pages/openlineage/OpenLineageOverviewPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/openlineage/jobs",
    name: "OpenLineageTasks",
    alias: ["/openlineage/tasks"],
    component: () => import("@/pages/openlineage/OpenLineageRunsPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/openlineage/jobs/:guid(.+)",
    name: "OpenLineageTaskDetail",
    alias: ["/openlineage/tasks/:guid(.+)"],
    component: () =>
      import("@/pages/openlineage/OpenLineageTaskDetailPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/openlineage/datasets",
    name: "OpenLineageDatasets",
    component: () => import("@/pages/openlineage/OpenLineageDatasetsPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/openlineage/events",
    name: "OpenLineageEvents",
    component: () => import("@/pages/openlineage/OpenLineageEventsPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/openlineage/events/:guid(.+)",
    name: "OpenLineageRunDetail",
    alias: ["/openlineage/runs/:guid(.+)"],
    component: () => import("@/pages/openlineage/OpenLineageRunDetailPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/openlineage/column-lineage/:guid(.+)",
    name: "OpenLineageColumnLineage",
    component: () =>
      import("@/pages/openlineage/OpenLineageColumnLineagePage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/metadata",
    name: "MetadataBrowser",
    component: () => import("@/pages/MetadataBrowserPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/manual-sql",
    name: "ManualSQLManagement",
    component: () => import("@/pages/ManualSQLManagementPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/metadata/:guid(.+)",
    name: "MetadataDetail",
    component: () => import("@/pages/MetadataBrowserPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/lineage/:guid(.+)",
    name: "LineageGraph",
    component: () => import("@/pages/LineageGraphPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    path: "/:pathMatch(.*)*",
    name: "NotFound",
    component: () => import("@/pages/NotFoundPage.vue"),
    meta: { requiresAuth: false, layout: "auth" },
  },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
});

router.beforeEach(async (to, _from, next) => {
  const authStore = useAuthStore();

  // Try to fetch current user if not authenticated and route requires auth
  if (!authStore.isAuthenticated && to.meta.requiresAuth !== false) {
    try {
      await authStore.fetchCurrentUser();
    } catch {
      // Ignore errors, will redirect to login below
    }
  }

  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    next({ name: "Login", query: { redirect: to.fullPath } });
  } else if (to.name === "Login" && authStore.isAuthenticated) {
    next({ name: "Home" });
  } else {
    next();
  }
});

export default router;
