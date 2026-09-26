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
    meta: {
      requiresAuth: true,
      layout: "default",
      contentWidth: "full",
    },
  },
  {
    path: "/instances/:instanceId",
    name: "InstanceDetail",
    component: () => import("@/pages/InstanceDetailPage.vue"),
    meta: {
      requiresAuth: true,
      layout: "default",
      contentWidth: "full",
    },
  },
  {
    path: "/databases",
    name: "DatabaseManagement",
    component: () => import("@/pages/DatabaseManagementPage.vue"),
    meta: {
      requiresAuth: true,
      layout: "default",
      contentWidth: "full",
    },
  },
  {
    path: "/settings",
    component: () => import("@/pages/settings/SettingsLayout.vue"),
    redirect: { name: "GeneralSettings" },
    // The whole section opts out of the generic content cap: SettingsLayout
    // owns one width for every settings page, so the section navigation cannot
    // shift when the user switches between them.
    meta: { requiresAuth: true, layout: "default", contentWidth: "full" },
    children: [
      {
        path: "general",
        name: "GeneralSettings",
        component: () => import("@/pages/settings/GeneralSettingsPage.vue"),
        meta: { requiresAuth: true, layout: "default" },
      },
      {
        path: "environments",
        name: "EnvironmentSettings",
        component: () => import("@/pages/settings/EnvironmentSettingsPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.settings.get",
        },
      },
      {
        path: "users",
        name: "UserManagement",
        component: () => import("@/pages/settings/UserManagementPage.vue"),
        meta: { requiresAuth: true, layout: "default" },
      },
      {
        path: "roles",
        name: "RoleManagement",
        component: () => import("@/pages/settings/RoleManagementPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.roles.list",
        },
      },
      {
        path: "groups",
        name: "GroupManagement",
        component: () => import("@/pages/settings/GroupManagementPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.groups.list",
        },
      },
      {
        path: "iam",
        name: "IamPolicy",
        component: () => import("@/pages/settings/IamPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.iam.getPolicy",
        },
      },
      {
        path: "llm-providers",
        name: "LLMProviderManagement",
        component: () =>
          import("@/pages/settings/LLMProviderManagementPage.vue"),
        // Width and height both come from the section layout.
        meta: { requiresAuth: true, layout: "default" },
      },
      {
        path: "openlineage",
        name: "OpenLineageSettings",
        component: () => import("@/pages/settings/OpenLineageSettingsPage.vue"),
        meta: { requiresAuth: true, layout: "default" },
      },
      {
        path: "audit-logs",
        name: "AuditLogs",
        component: () => import("@/pages/settings/AuditLogsPage.vue"),
        // The audit table is eight columns of long RPC names; inside the
        // section's reading column four of them fell behind a horizontal
        // scrollbar, so this page takes the full content width instead.
        meta: {
          requiresAuth: true,
          layout: "default",
          settingsContentWidth: "full",
        },
      },
    ],
  },
  {
    path: "/explain-sql",
    name: "ExplainSQL",
    component: () => import("@/pages/ExplainSQLPage.vue"),
    meta: {
      requiresAuth: true,
      layout: "default",
      contentWidth: "full",
    },
  },
  {
    path: "/explain-sql/:guid+",
    name: "ExplainSQLWithGuid",
    component: () => import("@/pages/ExplainSQLPage.vue"),
    meta: {
      requiresAuth: true,
      layout: "default",
      contentWidth: "full",
    },
  },
  {
    // The section carries the shared flags once so no child can drift out of
    // step: `/openlineage/overview` used to be the only route here without
    // `contentWidth: "full"`, which made the landing page render narrower than
    // every page it links to.
    path: "/openlineage",
    redirect: { name: "OpenLineageOverview" },
    meta: { requiresAuth: true, layout: "default", contentWidth: "full" },
    children: [
      {
        path: "overview",
        name: "OpenLineageOverview",
        component: () =>
          import("@/pages/openlineage/OpenLineageOverviewPage.vue"),
      },
      {
        path: "jobs",
        name: "OpenLineageTasks",
        alias: ["tasks"],
        component: () => import("@/pages/openlineage/OpenLineageRunsPage.vue"),
      },
      {
        path: "jobs/:guid(.+)",
        name: "OpenLineageTaskDetail",
        alias: ["tasks/:guid(.+)"],
        component: () =>
          import("@/pages/openlineage/OpenLineageTaskDetailPage.vue"),
      },
      {
        path: "datasets",
        name: "OpenLineageDatasets",
        component: () =>
          import("@/pages/openlineage/OpenLineageDatasetsPage.vue"),
      },
      {
        path: "events",
        name: "OpenLineageEvents",
        component: () =>
          import("@/pages/openlineage/OpenLineageEventsPage.vue"),
      },
      {
        path: "events/:guid(.+)",
        name: "OpenLineageRunDetail",
        alias: ["runs/:guid(.+)"],
        component: () =>
          import("@/pages/openlineage/OpenLineageRunDetailPage.vue"),
      },
      {
        // Repeated so every GUID segment keeps its own path segment: a name
        // containing `/` survives, which a single `(.+)` param cannot do.
        path: "column-lineage/:guid+",
        name: "OpenLineageColumnLineage",
        component: () =>
          import("@/pages/openlineage/OpenLineageColumnLineagePage.vue"),
      },
    ],
  },
  {
    path: "/metadata",
    name: "MetadataBrowser",
    component: () => import("@/pages/MetadataBrowserPage.vue"),
    meta: {
      requiresAuth: true,
      layout: "default",
      contentWidth: "full",
    },
  },
  {
    path: "/manual-sql",
    name: "ManualSQLManagement",
    component: () => import("@/pages/ManualSQLManagementPage.vue"),
    meta: {
      requiresAuth: true,
      layout: "default",
      contentWidth: "full",
    },
  },
  {
    // Repeated for the same reason as `column-lineage/:guid+` above: the GUID is
    // pushed as one param array element per segment, so nothing has to be
    // re-split (and a `/` inside a name is preserved).
    path: "/metadata/:guid+",
    name: "MetadataDetail",
    component: () => import("@/pages/MetadataBrowserPage.vue"),
    meta: {
      requiresAuth: true,
      layout: "default",
      contentWidth: "full",
    },
  },
  {
    path: "/lineage/:guid+",
    name: "LineageGraph",
    component: () => import("@/pages/LineageGraphPage.vue"),
    meta: {
      requiresAuth: true,
      layout: "default",
      contentWidth: "full",
    },
  },
  {
    path: "/device",
    name: "DeviceLogin",
    component: () => import("@/pages/DeviceLoginPage.vue"),
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

  // An authenticated page may only render once the permission-bearing profile
  // is loaded: the sidebar and the dashboard hide everything they cannot match
  // a permission for. Without this a fresh session — and the moment right after
  // login, whose response carries the user without permissions — shows an
  // almost empty app until the next full page load.
  if (to.meta.requiresAuth !== false) {
    await authStore.ensurePermissionsLoaded();
  }

  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    next({
      name: "Login",
      query: {
        redirect: to.fullPath,
        // The server rejected the session, not the user's intent: say so on the
        // form instead of showing a bare login page.
        ...(authStore.sessionExpired ? { expired: "1" } : {}),
      },
    });
  } else if (authStore.requireResetPassword && to.name !== "Login") {
    // A forced password reset has to be completed first: the server only
    // accepts the password change from the token the login issued.
    next({ name: "Login" });
  } else if (to.name === "Login" && authStore.isAuthenticated) {
    next({ name: "Home" });
  } else if (
    typeof to.meta.permission === "string" &&
    authStore.isAuthenticated &&
    !authStore.hasPermission(to.meta.permission)
  ) {
    // The server enforces the same permission on every RPC; this only keeps a
    // caller from landing on a page whose every request would be denied.
    next({ name: "Home" });
  } else {
    next();
  }
});

export default router;
