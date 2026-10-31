/**
 * Client-side mirror of the permission catalog.
 *
 * The authoritative catalog is `backend/common/permission/permission.json`;
 * this file groups it for the role editor's checkbox matrix. The drift test in
 * permissions.test.ts reads that JSON and fails if the two disagree, so adding
 * a permission means updating both.
 *
 * Permission strings are identifiers, not prose: they are rendered verbatim so
 * an operator can paste one into an API call.
 */
export interface PermissionGroup {
  /** The resource segment shown as the group heading, e.g. "instances". */
  resource: string;
  permissions: readonly string[];
}

export const PERMISSION_GROUPS = [
  {
    resource: "instances",
    permissions: [
      "metaxisdata.instances.get",
      "metaxisdata.instances.list",
      "metaxisdata.instances.create",
      "metaxisdata.instances.update",
      "metaxisdata.instances.delete",
      "metaxisdata.instances.undelete",
      "metaxisdata.instances.sync",
    ],
  },
  {
    resource: "environments",
    permissions: ["metaxisdata.environments.list"],
  },
  {
    resource: "dataSources",
    permissions: [
      "metaxisdata.dataSources.create",
      "metaxisdata.dataSources.update",
      "metaxisdata.dataSources.delete",
    ],
  },
  {
    resource: "databases",
    permissions: [
      "metaxisdata.databases.list",
      "metaxisdata.databases.read",
      "metaxisdata.databases.sync",
    ],
  },
  {
    resource: "applications",
    permissions: [
      "metaxisdata.applications.get",
      "metaxisdata.applications.list",
      "metaxisdata.applications.create",
      "metaxisdata.applications.update",
      "metaxisdata.applications.delete",
      "metaxisdata.applications.apiKeys.list",
      "metaxisdata.applications.apiKeys.create",
      "metaxisdata.applications.apiKeys.delete",
    ],
  },
  {
    resource: "applicationSqls",
    permissions: [
      "metaxisdata.applicationSqls.get",
      "metaxisdata.applicationSqls.list",
      "metaxisdata.applicationSqls.delete",
    ],
  },
  {
    resource: "connectedApps",
    permissions: [
      "metaxisdata.connectedApps.list",
      "metaxisdata.connectedApps.revoke",
    ],
  },
  {
    resource: "manualSqls",
    permissions: [
      "metaxisdata.manualSqls.get",
      "metaxisdata.manualSqls.list",
      "metaxisdata.manualSqls.create",
      "metaxisdata.manualSqls.update",
      "metaxisdata.manualSqls.delete",
    ],
  },
  {
    resource: "lineage",
    permissions: ["metaxisdata.lineage.get"],
  },
  {
    resource: "openlineage",
    permissions: [
      "metaxisdata.openlineage.read",
      "metaxisdata.openlineage.namespaceMappings.list",
      "metaxisdata.openlineage.namespaceMappings.create",
      "metaxisdata.openlineage.namespaceMappings.update",
      "metaxisdata.openlineage.namespaceMappings.delete",
      "metaxisdata.openlineage.apiKeys.list",
      "metaxisdata.openlineage.apiKeys.create",
      "metaxisdata.openlineage.apiKeys.delete",
    ],
  },
  {
    resource: "llm",
    permissions: [
      "metaxisdata.llm.profiles.list",
      "metaxisdata.llm.profiles.create",
      "metaxisdata.llm.profiles.update",
      "metaxisdata.llm.profiles.delete",
      "metaxisdata.llm.profiles.fetchModels",
    ],
  },
  {
    resource: "explainSql",
    permissions: ["metaxisdata.explainSql.explain"],
  },
  {
    resource: "dataQuality",
    permissions: [
      "metaxisdata.dataQuality.rules.get",
      "metaxisdata.dataQuality.rules.list",
      "metaxisdata.dataQuality.rules.create",
      "metaxisdata.dataQuality.rules.update",
      "metaxisdata.dataQuality.rules.delete",
      "metaxisdata.dataQuality.rules.run",
      "metaxisdata.dataQuality.executions.get",
      "metaxisdata.dataQuality.executions.list",
      "metaxisdata.dataQuality.issues.get",
      "metaxisdata.dataQuality.issues.list",
      "metaxisdata.dataQuality.issues.update",
      "metaxisdata.dataQuality.explain",
      "metaxisdata.dataQuality.suggest",
      "metaxisdata.dataQuality.profile.run",
      "metaxisdata.dataQuality.labels.update",
      "metaxisdata.dataQuality.contracts.publish",
    ],
  },
  {
    resource: "users",
    permissions: [
      "metaxisdata.users.get",
      "metaxisdata.users.list",
      "metaxisdata.users.create",
      "metaxisdata.users.update",
      "metaxisdata.users.delete",
      "metaxisdata.users.undelete",
    ],
  },
  {
    resource: "groups",
    permissions: [
      "metaxisdata.groups.get",
      "metaxisdata.groups.list",
      "metaxisdata.groups.create",
      "metaxisdata.groups.update",
      "metaxisdata.groups.delete",
    ],
  },
  {
    resource: "roles",
    permissions: [
      "metaxisdata.roles.get",
      "metaxisdata.roles.list",
      "metaxisdata.roles.create",
      "metaxisdata.roles.update",
      "metaxisdata.roles.delete",
    ],
  },
  {
    resource: "iam",
    permissions: ["metaxisdata.iam.getPolicy", "metaxisdata.iam.setPolicy"],
  },
  {
    resource: "settings",
    permissions: ["metaxisdata.settings.get", "metaxisdata.settings.update"],
  },
  {
    resource: "identityProviders",
    permissions: [
      "metaxisdata.identityProviders.get",
      "metaxisdata.identityProviders.list",
      "metaxisdata.identityProviders.create",
      "metaxisdata.identityProviders.update",
      "metaxisdata.identityProviders.delete",
    ],
  },
  {
    resource: "auditLogs",
    permissions: ["metaxisdata.auditLogs.search"],
  },
] as const satisfies readonly PermissionGroup[];

/**
 * One catalog entry. Deriving the union from the catalog means a typo in a
 * `hasPermission` call or in a route/sidebar entry fails to compile, instead of
 * silently hiding the UI it guards.
 */
export type Permission =
  (typeof PERMISSION_GROUPS)[number]["permissions"][number];

export const ALL_PERMISSIONS: readonly Permission[] = PERMISSION_GROUPS.flatMap(
  (group) => group.permissions
);

/**
 * The part of a permission shown in the role editor, i.e. everything after
 * "metaxisdata.<resource>." A sub-resourced permission keeps its sub-resource
 * segment, so "metaxisdata.llm.profiles.fetchModels" reads
 * "profiles.fetchModels".
 */
export function permissionSuffix(permission: string): string {
  const parts = permission.split(".");
  return parts.length > 2 ? parts.slice(2).join(".") : permission;
}
