import {
  createRouter,
  createWebHistory,
  type NavigationGuard,
  type RouteRecordRaw,
} from "vue-router";
import { isKnownSpaRouteName, resolveSignInTarget } from "@/lib/signInRedirect";
import { useAuthStore } from "@/store/modules/auth";

const routes: RouteRecordRaw[] = [
  {
    path: "/login",
    name: "Login",
    component: () => import("@/pages/LoginPage.vue"),
    meta: { requiresAuth: false, layout: "auth" },
  },
  {
    // Where the identity provider sends the browser back. Public: the guard must
    // not bounce a signed-out user mid-flow, and the page exchanges the code
    // itself. A provider test also lands here, so it has to render for an
    // authenticated administrator too.
    path: "/oauth/callback",
    name: "OAuthCallback",
    component: () => import("@/pages/LoginCallbackPage.vue"),
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
        path: "data-quality",
        name: "DataQualitySettings",
        component: () => import("@/pages/settings/DataQualitySettingsPage.vue"),
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
        path: "identity-providers",
        name: "IdentityProviderSettings",
        component: () =>
          import("@/pages/settings/IdentityProviderSettingsPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.identityProviders.list",
        },
      },
      {
        path: "connected-apps",
        name: "ConnectedAppsManagement",
        component: () =>
          import("@/pages/settings/ConnectedAppsSettingsPage.vue"),
        // Which MCP clients were authorized on whose behalf is access
        // governance, so it is an administrative errand beside users and
        // identity providers. The table is seven columns wide, so it opts out
        // of the section's reading column like audit-logs and applications.
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.connectedApps.list",
          settingsContentWidth: "full",
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
        path: "applications",
        name: "ApplicationManagement",
        component: () => import("@/pages/ApplicationManagementPage.vue"),
        // Managing applications is an administrative errand, not daily work, so it lives
        // in the settings section beside the other credential pages. Its table is six
        // columns wide and opts out of the section's reading column.
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.applications.list",
          settingsContentWidth: "full",
        },
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
    path: "/application-sql",
    name: "ApplicationSQL",
    component: () => import("@/pages/ApplicationSQLPage.vue"),
    meta: {
      requiresAuth: true,
      layout: "default",
      contentWidth: "full",
      permission: "metaxisdata.applicationSqls.list",
    },
  },
  {
    // A quality rule watches the data itself rather than the registry's record of
    // it, so this is a product destination of its own, not a Data sub-page. The
    // parent carries the shell flags once, like the OpenLineage section: a target
    // is the directory, an execution is the evidence, and the rule editor joins
    // them next release.
    path: "/data-quality",
    meta: { requiresAuth: true, layout: "default", contentWidth: "full" },
    children: [
      {
        // The section's landing page: the workspace summary, not a directory.
        path: "",
        name: "DataQualityOverview",
        component: () =>
          import("@/pages/data-quality/DataQualityOverviewPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.dataQuality.rules.list",
        },
      },
      {
        // The directory is grouped by what the rules watch: one row per table or
        // view, which is the unit a reader maintains. The cross-target rule list
        // stays reachable behind "All rules" and the overview's stat cards.
        path: "targets",
        name: "DataQualityTargets",
        component: () =>
          import("@/pages/data-quality/DataQualityTargetsPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.dataQuality.rules.list",
        },
      },
      {
        // One target's rules as one workspace. The GUID travels as the repeated
        // `:targetGuid` param (see utils/guid.ts); a GUID does not carry the
        // object type, so `metaType` arrives as a query.
        path: "targets/:targetGuid+",
        name: "DataQualityTarget",
        component: () =>
          import("@/pages/data-quality/DataQualityTargetPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.dataQuality.rules.list",
        },
      },
      {
        // Every check across targets, for audit and search. It is a directory,
        // not an authoring surface: a check is created and edited inside the
        // target's contract workspace, where its column and siblings are visible.
        path: "checks",
        name: "DataQualityChecks",
        component: () =>
          import("@/pages/data-quality/DataQualityChecksPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.dataQuality.rules.list",
        },
      },
      // Old rule-centric URLs survive as redirects: the product no longer has a
      // standalone rule directory or single-rule editor page.
      {
        path: "rules",
        redirect: { name: "DataQualityChecks" },
      },
      {
        path: "rules/new",
        redirect: { name: "DataQualityTargets" },
      },
      {
        path: "rules/:ruleId+/edit",
        name: "DataQualityRuleEditRedirect",
        component: () =>
          import("@/pages/data-quality/RedirectQualityRulePage.vue"),
        meta: { requiresAuth: true, layout: "default" },
      },
      {
        // The old per-rule page becomes the target workspace with the check
        // drawer open, so existing links keep landing on the check.
        path: "rules/:ruleId+",
        name: "DataQualityRuleRedirect",
        component: () =>
          import("@/pages/data-quality/RedirectQualityRulePage.vue"),
        meta: { requiresAuth: true, layout: "default" },
      },
      {
        path: "suggestions",
        name: "DataQualitySuggestions",
        component: () =>
          import("@/pages/data-quality/DataQualitySuggestionsPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.dataQuality.suggest",
        },
      },
      {
        path: "issues",
        name: "DataQualityIssues",
        component: () =>
          import("@/pages/data-quality/DataQualityIssuesPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.dataQuality.issues.list",
        },
      },
      {
        // The issue's resource name is carried whole, one path segment per name
        // segment: a Get needs the real workspace, not the `-` shorthand.
        path: "issues/:issueId+",
        name: "DataQualityIssueDetail",
        component: () =>
          import("@/pages/data-quality/DataQualityIssueDetailPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.dataQuality.issues.get",
        },
      },
      {
        path: "executions",
        name: "DataQualityExecutions",
        component: () =>
          import("@/pages/data-quality/DataQualityExecutionsPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.dataQuality.executions.list",
        },
      },
      {
        // The execution's resource name is carried whole, one path segment per
        // `name` segment (`workspaces/{workspace}/qualityExecutions/{id}`): a
        // Get needs the real workspace, not the `-` shorthand the list accepts.
        path: "executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: () =>
          import("@/pages/data-quality/DataQualityExecutionDetailPage.vue"),
        meta: {
          requiresAuth: true,
          layout: "default",
          permission: "metaxisdata.dataQuality.executions.get",
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
    // Reached from the avatar menu, not from a nav entry: managing the apps a
    // person connected is an account control, not a product destination. No
    // permission — every signed-in user owns the connections it lists.
    path: "/connected-apps",
    name: "ConnectedApps",
    component: () => import("@/pages/ConnectedAppsPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    // Reached from the sidebar's notification bell, not from a nav entry: the
    // bell is the permanent affordance, and this page is where it expands to.
    path: "/notifications",
    name: "Notifications",
    component: () => import("@/pages/NotificationsPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    // The OAuth authorization server redirects here, so the request id arrives
    // in the query string and survives the sign-in round trip.
    path: "/oauth/consent",
    name: "OAuthConsent",
    component: () => import("@/pages/OAuthConsentPage.vue"),
    meta: { requiresAuth: true, layout: "default" },
  },
  {
    // A mistyped deep link should not eject a signed-in user from the app, so
    // the layout is chosen per navigation in the guard below: the default shell
    // when there is a session, the bare auth layout when there is not.
    path: "/:pathMatch(.*)*",
    name: "NotFound",
    component: () => import("@/pages/NotFoundPage.vue"),
    meta: { requiresAuth: false, layout: "default" },
  },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
});

// vue-router 5 deprecates the `next` callback: a guard states its outcome by
// returning it, and falling off the end of the function means "carry on".
//
// Exported for the test to call directly: the redirect below and the login-form
// shortcut further down could bounce a caller between `Login` and `Home`, and a
// router-level test would hang rather than fail, because that cycle is pure
// microtasks and starves the timer queue a test timeout would need.
export const authGuard: NavigationGuard = async (to) => {
  const authStore = useAuthStore();

  // The 404 route cannot declare one layout up front: a signed-in user needs the
  // shell and its navigation, an anonymous visitor needs the bare auth card.
  // Resolving the profile here (a no-op once loaded) picks the same shell every
  // other authenticated page gets.
  if (to.name === "NotFound") {
    await authStore.ensurePermissionsLoaded();
    to.meta.layout = authStore.isAuthenticated ? "default" : "auth";
  }

  // An authenticated page may only render once the permission-bearing profile
  // is loaded: the sidebar and the dashboard hide everything they cannot match
  // a permission for. Without this a fresh session — and the moment right after
  // login, whose response carries the user without permissions — shows an
  // almost empty app until the next full page load.
  if (to.meta.requiresAuth !== false) {
    await authStore.ensurePermissionsLoaded();
  } else if (to.name === "Login" && !authStore.sessionChecked) {
    // The form itself is public, but a caller who already has a session must not
    // be parked on it. A full page load starts with an empty store, so the
    // cookie is only discovered by asking; the shortcut below can only send the
    // caller on once that question has a definitive answer. `sessionChecked`
    // keeps the form from asking again after one — including the refusal that
    // bounced a caller here a moment ago.
    await authStore.ensurePermissionsLoaded();
  }

  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    return {
      name: "Login",
      query: {
        redirect: to.fullPath,
        // The server rejected the session, not the user's intent: say so on the
        // form instead of showing a bare login page.
        ...(authStore.sessionExpired ? { expired: "1" } : {}),
      },
    };
  }

  if (authStore.requireResetPassword && to.name !== "Login") {
    // A forced password reset has to be completed first: the server only
    // accepts the password change from the token the login issued.
    return { name: "Login" };
  }

  // A pending reset owns the login form — `ensurePermissionsLoaded` leaves that
  // session alone for the same reason — so the shortcut below must not send the
  // caller back into the app, where the redirect above would only return them
  // here. That pair is what would otherwise never settle.
  if (
    to.name === "Login" &&
    authStore.isAuthenticated &&
    !authStore.requireResetPassword
  ) {
    // Continue where the caller was headed rather than dropping them on the
    // default page: a saved `/login?redirect=...` link (and the sign-in URL an
    // MCP client hands out) has to reach its target once the session is found.
    const target = resolveSignInTarget(
      typeof to.query.redirect === "string" ? to.query.redirect : undefined,
      (path) => isKnownSpaRouteName(router.resolve(path).name)
    );
    if (target.kind === "server") {
      // A server route — the MCP client's /oauth/authorize is the one that
      // matters — only resumes through a full page load; the router's catch-all
      // would answer NotFound instead. `replace` keeps the login form out of the
      // history, or Back would immediately resume the same redirect.
      window.location.replace(target.path);
      return false;
    }
    if (target.kind === "spa") {
      const resolved = router.resolve(target.path);
      // A redirect that names the form itself would loop the two branches of
      // this guard, so the default page is the only sane answer to it.
      if (resolved.name !== "Login") {
        return {
          path: resolved.path,
          query: resolved.query,
          hash: resolved.hash,
        };
      }
    }
    return { name: "Home" };
  }

  if (
    typeof to.meta.permission === "string" &&
    authStore.isAuthenticated &&
    !authStore.hasPermission(to.meta.permission)
  ) {
    // The server enforces the same permission on every RPC; this only keeps a
    // caller from landing on a page whose every request would be denied.
    return { name: "Home" };
  }
};

/**
 * Answers a refused request: forget the user and, when a session the app
 * believed in was refused, send the caller to the login form with the path they
 * were on. A visitor who never signed in is left alone — the guard already
 * routes them (or is doing so right now), and a second navigation from here
 * would race it and claim an expiry that never happened.
 */
export function handleSessionExpired() {
  const authStore = useAuthStore();
  authStore.handleUnauthenticated();
  if (!authStore.sessionExpired) {
    return;
  }
  const current = router.currentRoute.value;
  if (current.name === "Login") {
    // Already on the login form (e.g. a failed password reset): a pushed
    // navigation would only duplicate the entry.
    return;
  }
  return router.push({
    name: "Login",
    query: { expired: "1", redirect: current.fullPath },
  });
}

router.beforeEach(authGuard);

export default router;
