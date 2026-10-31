import { computed } from "vue";
import type { Permission } from "@/lib/permissions";
import { useAuthStore } from "@/store/modules/auth";

/**
 * The data-quality permissions the pages and the check drawer ask about.
 *
 * The same matrix used to be restated as `canX` computeds in each view — eight in
 * the target workspace, seven in the drawer — so a permission change meant
 * hunting for every copy. The migrated views read their checks from here;
 * `hasPermission` itself stays the single entry point for the check. Pages not
 * migrated yet still hold local `canX` computeds.
 */
export function useDataQualityPermissions() {
  const authStore = useAuthStore();
  const check = (permission: Permission) =>
    computed(() => authStore.hasPermission(permission));

  return {
    canViewRule: check("metaxisdata.dataQuality.rules.get"),
    canRun: check("metaxisdata.dataQuality.rules.run"),
    canCreate: check("metaxisdata.dataQuality.rules.create"),
    canUpdate: check("metaxisdata.dataQuality.rules.update"),
    canDelete: check("metaxisdata.dataQuality.rules.delete"),
    canSuggest: check("metaxisdata.dataQuality.suggest"),
    /** The one-click label pass profiles stale columns before asking the model. */
    canProfile: check("metaxisdata.dataQuality.profile.run"),
    canListIssues: check("metaxisdata.dataQuality.issues.list"),
    canViewIssue: check("metaxisdata.dataQuality.issues.get"),
    canUpdateIssue: check("metaxisdata.dataQuality.issues.update"),
    canPublish: check("metaxisdata.dataQuality.contracts.publish"),
    canViewExecution: check("metaxisdata.dataQuality.executions.get"),
    canListExecutions: check("metaxisdata.dataQuality.executions.list"),
    canExplain: check("metaxisdata.dataQuality.explain"),
  };
}
