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
  permissions: string[];
}

export const PERMISSION_GROUPS: PermissionGroup[] = [
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
    resource: "auditLogs",
    permissions: ["metaxisdata.auditLogs.search"],
  },
];

export const ALL_PERMISSIONS: string[] = PERMISSION_GROUPS.flatMap(
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

/** Whether a permission string is one this build knows about. */
export function isKnownPermission(permission: string): boolean {
  return ALL_PERMISSIONS.includes(permission);
}

/** Permission strings granted by a role, without duplicates. */
export function uniquePermissions(permissions: string[]): string[] {
  return [...new Set(permissions)];
}
