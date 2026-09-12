# Plan: IAM Permission Management

> **Status: implemented.** Permission catalog, predefined + custom roles, the
> workspace IAM policy Get/Set, group management, the `component/iam` engine,
> the annotation-driven `ACLInterceptor`, `GetCurrentUser` permissions, the Vue
> Roles / Groups / Members & Permissions pages and permission-gated navigation
> all landed, with hermetic tests plus a real-server end-to-end test
> (`backend/test/integration/runner/iam_service_test.go`). Divergences from the
> decisions below: `GroupService` gained a Vue management page (groups were
> otherwise unusable as bindings), and `EvalBindingCondition` rejects
> unevaluable conditions at **write** time in `validateIamPolicy` in addition to
> failing closed at read time.

## TL;DR

Port laelia's IAM model into metaxisdata: a single-sourced **permission catalog**
(`metaxisdata.<resource>.<verb>`), **roles** as named permission bundles
(predefined in Go + custom in a re-added `role` table), and a **workspace IAM
policy** that binds principals (`users/{id}`, `groups/{email}`, `allUsers`) to
roles. A new `component/iam` manager resolves a caller's effective permission
set, the rebuilt `ACLInterceptor` enforces the `metaxisdata.v1.permission`
annotation on **every** RPC (read paths included), and new `IamService`,
`RoleService`, and `GroupService` expose management over ConnectRPC. The Vue SPA
gains a Roles page, a Members (IAM) page, and permission-gated navigation.

This closes the two items `02-auth-authorization.md` C4 and the roadmap's item 2
left open: **read-path authorization** and the **fine-grained role → permission
mapping** (previously "any non-empty permission ⇒ workspace admin").

## Decisions

- **Workspace-scoped only.** metaxisdata is single-workspace and the `policy`
  table only ever produces `WORKSPACE`/`IAM` rows, so `CheckPermission` resolves
  against the workspace policy. No per-resource policy resolution, no
  `ResourceRef` plumbing — that would be dead weight.
- **`workspaceMember` baseline is an implicit `allUsers` binding**, resolved
  in-memory: every authenticated principal gets the member permission set before
  any policy lookup, so a fresh install (empty policy) is usable.
- **`workspaceAdmin` holds the whole catalog**, so admin access falls out of the
  normal role → permission resolution rather than a special case.
- **Custom roles are real** (the `role` table was dropped in `904fb09`; this plan
  re-adds it) so operators can grant, say, "sync instances but not touch users"
  without a code change.
- **Groups are IAM principals.** `user_group` already exists with a read path;
  this plan adds create/update/delete plus a management API so bindings can
  reference `groups/{email}` through the UI. The last-admin guard must expand
  groups with the departing member **excluded** (fixes `02` H4).
- **Every RPC is annotated**, including reads and `GetCurrentUser`'s own
  permissions field. `GetCurrentUser` stays unannotated so the client can always
  bootstrap.
- **Predefined roles are resolved in memory**; custom roles via `GetRoleSnapshot`
  with an LRU cache, so the hot auth path costs one map lookup plus one cached
  policy read.
- **Permission catalog is generated** from `permission.json` by a committed `gen`
  program (`go generate ./backend/common/permission`), matching laelia. The
  generated Go is committed and never hand-edited.

## Permission catalog

`metaxisdata.<resource>.<verb>` — one entry per RPC, grouped by service:

| Resource | Permissions |
| --- | --- |
| `instances` | `get` `list` `create` `update` `delete` `undelete` `sync` |
| `dataSources` | `create` `update` `delete` |
| `databases` | `list` `read` (all metadata reads) `sync` |
| `manualSqls` | `get` `list` `create` `update` `delete` |
| `lineage` | `get` |
| `openlineage` | `read`, `namespaceMappings.{get,list,create,update,delete}`, `apiKeys.{get,list,create,delete}` |
| `llm` | `profiles.{get,list,create,update,delete}` |
| `explainSql` | `explain` |
| `users` | `get` `list` `create` `update` `delete` `undelete` |
| `groups` | `get` `list` `create` `update` `delete` |
| `roles` | `get` `list` `create` `update` `delete` |
| `iam` | `getPolicy` `setPolicy` |
| `settings` | `get` `update` |
| `auditLogs` | `search` |

### Predefined roles

- `workspaceAdmin` — the full catalog.
- `workspaceMember` — the authenticated-principal baseline: data-plane reads
  (`instances.get/list`, `dataSources` none, `databases.list/read`,
  `manualSqls` full, `lineage.get`, `openlineage.read`,
  `llm.profiles.get/list`, `explainSql.explain`, `settings.get`,
  `users.get/list`), i.e. everything a non-admin can do today. Deliberately
  **absent**: user/group/role/IAM administration, instance and datasource
  writes, settings writes, audit logs, OpenLineage writes, LLM profile writes.

## Layout

```
backend/common/permission/         permission.json, gen/, permission_gen.go, permission.go
backend/component/iam/             manager.go (CheckPermission, EffectiveWorkspacePermissions)
backend/store/predefined_roles.go  workspaceAdmin / workspaceMember
backend/store/role.go              custom role CRUD + snapshot cache
backend/store/policy.go            + SetWorkspaceIamPolicy (etag-guarded)
backend/store/group.go             + CreateGroup / DeleteGroup
backend/api/v1/acl_interceptor.go  rebuilt on top of iam.Manager
backend/api/v1/iam_service.go      Get/SetWorkspaceIamPolicy, bindings validation
backend/api/v1/role_service.go     custom role CRUD
backend/api/v1/group_service.go    group CRUD
proto/v1/v1/{iam_service,role_service,group_service}.proto
proto/store/store/role.proto       RolePermissions
frontend/src/pages/settings/{RoleManagementPage,IamPage}.vue
```

## Migration

`migration/0.1/0006##iam_role.sql` + the same idempotent DDL appended to
`LATEST.sql`:

```sql
CREATE TABLE IF NOT EXISTS role (
    resource_id text PRIMARY KEY,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    -- Stored as RolePermissions (proto/store/store/role.proto)
    permissions jsonb NOT NULL DEFAULT '{}'
);
```

`user_group` and `policy` already exist and are unchanged.

## Enforcement

1. `AuthInterceptor` populates `AuthContext.Permission` from the proto annotation
   (already done) and the caller in the context.
2. `ACLInterceptor` looks up the caller's effective permission set through
   `iam.Manager.CheckPermission` and returns `PermissionDenied` when the caller
   is unauthenticated or lacks the permission. The "non-empty ⇒ admin" shortcut
   is gone.
3. Handlers keep only the checks a catalog permission cannot express:
   `CreateUser` (signup/`disallow_signup`), `UpdateUser` (self-service password
   change, requires `current_password`), and the last-admin guard.
4. `GetCurrentUser` returns `User.permissions` (output-only) so the SPA can gate
   navigation and actions without probing each RPC.

## Tests

- `permission` catalog: every catalog entry is unique and well-formed; every
  proto annotation exists in the catalog (guard test over the generated
  descriptors).
- `iam.Manager`: baseline applies to every principal; admin gets everything; a
  custom role grants exactly its permissions; unknown roles grant nothing;
  group-expanded and `allUsers` bindings match; conditions fail closed.
- `store/role`: CRUD round-trip plus snapshot-cache invalidation guard.
- `store/policy`: `SetWorkspaceIamPolicy` etag match/mismatch.
- `acl_interceptor`: table-driven allow/deny per permission tier.
- `iam_service`: binding validation (unknown role, malformed member, missing
  principal, last-admin protection).
- Frontend: Vitest over the permission helpers.
