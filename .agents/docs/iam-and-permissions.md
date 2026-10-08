# IAM and Permissions — Reference

> Status: **implemented** (`6fddae6`; `environments.list` added by `51f6d5a`). Maintenance reference for `backend/common/permission/`, `backend/component/iam/`, `backend/api/v1/{acl_interceptor,iam_service,role_service,group_service}.go`, `backend/store/{predefined_roles,role,policy,group}.go`.
> Related: `docs/security-posture.md` (workspace-scoped authorization; the `allUsers` invariant), `.agents/docs/backend-review/02-auth-authorization.md` (C4/H4 history).

## What it is

One permission catalog, roles as named permission bundles (predefined in Go, custom in the `role` table), and a single workspace IAM policy binding principals (`users/{id}`, `groups/{email}`, `allUsers`) to roles. `iam.Manager` resolves a caller's effective set; `ACLInterceptor` enforces the `metaxisdata.v1.permission` annotation on every RPC, reads included; `IamService`/`RoleService`/`GroupService` manage it over ConnectRPC; `GetCurrentUser` returns `User.permissions` so the SPA can gate navigation without probing each RPC.

## Decisions

- **Workspace-scoped only** — `CheckPermission` resolves against the workspace policy; per-resource lookup and `ResourceRef` plumbing would be dead weight in a single-workspace product.
- **`workspaceMember` is an implicit `allUsers` binding resolved in memory** before any policy read, so a fresh install with an empty policy is usable; **`workspaceAdmin` holds the whole catalog**, so admin falls out of normal role→permission resolution instead of being a special case.
- **Custom roles are real** — `role` was dropped in `904fb09` as dead Bytebase-era schema and re-added, so an operator can grant "sync instances but not touch users" without a code change.
- **Groups are IAM principals** — `user_group` gained CRUD plus an API so a binding can name `groups/{email}`; the last-admin guard expands groups with the departing member excluded.
- **Predefined roles resolve in memory, custom roles via `GetRoleSnapshot` (LRU-cached)** — the hot path costs one map lookup plus one cached policy read.
- **The catalog is generated** from `backend/common/permission/permission.json` (`go generate ./backend/common/permission`); `backend/common/permission/permission_gen.go` is committed and never hand-edited.
- **`EvalBindingCondition` is validated at write time** in `validateIamPolicy` (`backend/api/v1/iam_service.go:114`) as well as failing closed at read time. This reference is the only record of that write-time check.
- **Unannotated RPCs are an explicit allowlist** (auth and device/OAuth self-service, `GetCurrentUser`, `CreateUser`, `UpdateUser`, the notification inbox); everything else must be gated.

## Permission catalog

`metaxisdata.<resource>.<verb>`, single-sourced from `backend/common/permission/permission.json` (55 entries) and regenerated into `backend/common/permission/permission_gen.go`:

| Resource | Permissions |
| --- | --- |
| `instances` | `get` `list` `create` `update` `delete` `undelete` `sync` |
| `environments` | `list` |
| `dataSources` | `create` `update` `delete` |
| `databases` | `list` `read` `sync` |
| `manualSqls` | `get` `list` `create` `update` `delete` |
| `lineage` · `explainSql` | `get` · `explain` |
| `openlineage` | `read`; `namespaceMappings.{list,create,update,delete}`; `apiKeys.{list,create,delete}` |
| `llm` | `profiles.{list,create,update,delete}` `profiles.fetchModels` |
| `users` · `groups` · `roles` | `{get,list,create,update,delete}` (`users` also `undelete`) |
| `iam` · `settings` · `auditLogs` | `{getPolicy,setPolicy}` · `{get,update}` · `search` |

Predefined roles (`backend/store/predefined_roles.go:60`), read-only over `RoleService`, never a row in `role`: **`workspaceAdmin`** holds the whole catalog; **`workspaceMember`** is the authenticated-principal baseline of 13 entries — `instances.get/list`, `environments.list`, `databases.list/read`, `manualSqls.{get,list,create,update,delete}`, `lineage.get`, `openlineage.read`, `explainSql.explain`. Deliberately absent: user/group/role/IAM administration, instance and data-source writes, `settings.get`/`settings.update`, audit logs, OpenLineage writes, LLM profiles.

## Invariants

| Rule | Breakage |
| --- | --- |
| Edit `backend/common/permission/permission.json`, then `go generate ./backend/common/permission` | `TestCatalogMatchesSource` and the frontend drift test (`frontend/src/lib/permissions.test.ts`) fail; a hand-edited `backend/common/permission/permission_gen.go` is overwritten. |
| A new RPC carries a catalog annotation, or joins `unannotatedMethods` | `TestEveryMethodIsPermissionGated` fails. |
| `GetCurrentUser` stays unannotated | the SPA cannot bootstrap its own permission set. |
| `allUsers` is bound only to `roles/workspaceMember`, and a full replace may not drop that binding | every authenticated principal, future registrations included, would gain a role nobody granted per user; checked at the API and again in the store — see `docs/security-posture.md`. |
| Resolution fails closed: unknown role, role-read error, unevaluable condition ⇒ no permissions | a policy typo would silently widen access instead of narrowing it. |
| A workspace keeps at least one active admin whose binding is unconditional | one `SetWorkspaceIamPolicy` or `DeleteUser` would leave the workspace unmanageable (`backend/api/v1/iam_service.go:47`, `backend/api/v1/user_service.go:503`). |
| `role.permissions` is protojson of `store.RolePermissions` | the column is read back with unknown fields discarded, so a wrong shape silently grants nothing. |
| `SetWorkspaceIamPolicy` stays etag-guarded | concurrent writers overwrite each other instead of getting `CodeAborted`. |

## Failure modes

| Scenario | Behavior |
| --- | --- |
| Unauthenticated, or authenticated without the permission | `CodeUnauthenticated` (`backend/api/v1/acl_interceptor.go:80`), otherwise `CodePermissionDenied` (`:87`). |
| A binding names a role that does not exist; or its condition is unevaluable (residual `resource.*`) | an unknown role resolves to an empty set with no error (`backend/component/iam/manager.go:113`); an unevaluable condition is rejected at write time and dropped while evaluating at read time. |
| `SetWorkspaceIamPolicy` with a stale etag, or a write that would leave zero unconditional active admins | `CodeAborted` (`backend/api/v1/iam_service.go:59`) or `CodeInvalidArgument` (`:54`). |
| A custom role is edited | the role snapshot cache is invalidated after the write, so the next check sees the new bundle. |

## Open items

None tracked here. Per-resource IAM is a deliberate non-goal (`docs/security-posture.md`); `frontend/src/lib/permissions.ts` is a hand-maintained mirror of the catalog, guarded by a drift test rather than generated.

## Where things live

- Catalog: `backend/common/permission/permission.json`, generator `backend/common/permission/gen/main.go`, generated `backend/common/permission/permission_gen.go`; engine `backend/component/iam/manager.go` (`CheckPermission`, `EffectivePermissions`) with expansion in `backend/utils/member.go`.
- Store: `backend/store/predefined_roles.go`, `backend/store/role.go`, `backend/store/policy.go` (etag and `allUsers` guards), `backend/store/group.go`; schema `backend/migrator/migration/0.1/0006##iam_role.sql`, `backend/migrator/migration/LATEST.sql:123`; `User.permissions` in `backend/api/v1/user_service.go:102`.
- API and frontend: `backend/api/v1/{acl_interceptor,iam_service,role_service,group_service}.go`; `frontend/src/pages/settings/{RoleManagementPage,IamPage,GroupManagementPage}.vue`, `frontend/src/lib/permissions.ts`, `frontend/src/api/{role,group,iam}.ts`.
- Tests: `backend/common/permission/permission_test.go`, `backend/component/iam/manager_test.go`, `backend/api/v1/acl_interceptor_test.go`, `backend/store/{predefined_roles,role,policy,policy_all_users}_test.go`, `frontend/src/lib/permissions.test.ts`, end-to-end `backend/test/integration/runner/iam_service_test.go`.
- Gates: `go test ./backend/...` plus `golangci-lint run --allow-parallel-runners`; `make test-integration` for the end-to-end file; inside `frontend/` the order is `pnpm biome:check`, `pnpm lint`, `pnpm i18n` (`pnpm i18n:sort` after locale edits), `pnpm type-check`, `pnpm test run`.
