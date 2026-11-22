# IAM and permissions — Reference

> Maintenance reference for `backend/common/permission/`, `backend/component/iam/`, `backend/api/v1/{acl_interceptor,iam_service,role_service,group_service,connected_app_service}.go`, and the IAM store. Related: [resource-iam.md](resource-iam.md) (the resource-scoped target), [docs/security-posture.md](../../docs/security-posture.md) (the `allUsers` invariant).

## What it is

One permission catalog, roles as named permission bundles (predefined in Go, custom in the `role` table), and a single workspace IAM policy binding principals (`users/{id}`, `groups/{email}`, `allUsers`) to roles. `iam.Manager` resolves a caller's effective set; `ACLInterceptor` enforces the `metaxisdata.v1.permission` annotation on every RPC, reads included; `IamService`/`RoleService`/`GroupService` manage the model over ConnectRPC; `GetCurrentUser` returns `User.permissions` (what the workspace policy grants) and `User.scoped_permissions` (what a scoped grant adds outside it), so the SPA can gate navigation without probing each RPC.

## Decisions

- **The workspace policy is the root; scoped grants are additive** — `CheckPermission` resolves the workspace policy first, then the scoped authorizer's grants and derived agent relations on the resource. The model is in [resource-iam.md](resource-iam.md).
- **Scoped grants are additive and managed beside the policy** — `IamService.{List,Create,Delete}IamGrant` write the `iam_grant` ledger, and the IAM page's scope picker names an instance or one of its databases. A grant adds a role over that subtree; the workspace policy remains the root editor, and no grant can deny or narrow it. `IamService.CheckPermissions` is the unannotated self-service query a page uses to gate an action on one resource; it answers only about the caller.
- **`workspaceMember` is an implicit `allUsers` binding resolved in memory** before any policy read, so a fresh install with an empty policy is usable; **`workspaceAdmin` holds the whole catalog**, so admin falls out of normal role→permission resolution instead of being a special case.
- **Custom roles are real** — an operator can grant "sync instances but not touch users" without a code change.
- **Groups are IAM principals** — a binding can name `groups/{email}`; the last-admin guard expands groups with the departing member excluded, so a group containing only the departing admin does not count as a survivor.
- **Predefined roles resolve in memory, custom roles through `GetRoleSnapshot`** (LRU-cached and invalidated after a role write) — the hot path costs one map lookup plus one cached policy read.
- **The catalog is generated** from `backend/common/permission/permission.json` (`go generate ./backend/common/permission`); `permission_gen.go` is committed and never hand-edited.
- **`EvalBindingCondition` is validated at write time** in `validateIamPolicy` as well as failing closed at read time; only `request.time` is bound, so a condition referencing other attributes is rejected rather than stored.
- **Unannotated RPCs are an explicit allowlist** (auth and device/OAuth self-service, `GetCurrentUser`, `CreateUser`, `UpdateUser`, the notification inbox, connected apps, and the self-service `IamService.CheckPermissions`); everything else must be gated. `CheckPermissions` answers only about the caller's own permissions on one resource.
- **A mixed-audience RPC is unannotated and handler-scoped** — `ConnectedAppService.List/Revoke` serve an administrator (the whole workspace) and an ordinary user (their own rows) from one call, so each handler narrows first and widens only when `connectedApps.list` / `.revoke` resolves; `backend/api/v1/connected_app_test.go` is the matrix guard.

## Permission catalog

`metaxisdata.<resource>.<verb>`, single-sourced from `backend/common/permission/permission.json` (101 entries) and regenerated into `backend/common/permission/permission_gen.go`. The JSON is the source of truth — do not keep a hand-maintained list here; `TestCatalogMatchesSource` and the frontend drift test fail on a mismatch. Resources currently are `instances`, `environments`, `dataSources`, `databases`, `manualSqls`, `applications` (including `apiKeys.*`), `applicationSqls`, `connectedApps`, `lineage`, `explainSql`, `openlineage` (including `namespaceMappings.*` and `apiKeys.*`), `llm.profiles`, `users`, `groups`, `roles`, `identityProviders`, `iam`, `settings`, `auditLogs`, and `dataQuality` (28 permissions: rules, executions, issues, explain, suggest, contracts, plans, profile, labels, agents, templates, code tables).

Predefined roles (`backend/store/predefined_roles.go`) are read-only over `RoleService` and never rows in `role`:

- **`workspaceAdmin`** holds the whole catalog.
- **`workspaceMember`** is the authenticated-principal baseline of 29 entries (`memberBaselinePermissions`): the read/discovery surface plus the manual-SQL writes and `users.list`. It deliberately omits instance and data-source writes, user writes, group/role/IAM administration, `settings.get`/`settings.update`, audit logs, OpenLineage writes, LLM profiles, `applications.apiKeys.*` (issuing a key is a credential that writes data), and `connectedApps.*` — a member still manages their own connections because the handler forces self-scoping, while seeing the workspace or revoking someone else's needs the permission.
- **`qualityAgent`** is the built-in quality agent's IAM actor: quality-rule reads plus `dataQuality.suggest` and `dataQuality.plans.update`; its permission set is unchanged, and it still cannot publish, approve, edit rules or touch settings. The agent principal holds no workspace policy binding: the authorizer derives the role on the agent's databases while the agent is active, and the tool pipeline checks the session target (or the capability, for a conversation) through the same authorizer. The owner-requested adoption tool reuses the adoption RPC's `dataQuality.agents.get`, but its real gate is the product path — the owner's request in the current wake, adoption enabled, database scope, the daily cap and rule validation — not IAM, and every adoption is ledgered and audited. The adoption RPC (`AdoptQualityAgentPendingSuggestions`) carries the same `agents.get` annotation and an owner-or-administrator handler gate; it is how a member owner, who holds no `rules.create`, approves drafts. The approval runs through the internal create path, which does not check the caller's rule permissions because its only input is the agent's already-queued drafts; the check's created-by and the draft decision are the caller's, the check is owned by the agent's owner (or the approver), it does not spend the daily cap, and the audit row names the person.
- **`qualityOwner`** is the data-quality manager bundle a scoped grant gives the owner of a database: rule get/list/create/update/delete/run, execution get/list, issue get/list/update, explain, suggest, contract publish, plan update, profile run and label update. It is a role, not a baseline: it takes effect where an `iam_grant` names it or where the principal owns a quality agent (derived on the agent's databases), and it is additive to the workspace policy.

## Invariants

| Rule | Breakage |
| --- | --- |
| Edit `permission.json`, then `go generate ./backend/common/permission` | `TestCatalogMatchesSource` and the frontend drift test (`frontend/src/lib/permissions.test.ts`) fail; a hand-edited `permission_gen.go` is overwritten. |
| A new RPC carries a catalog annotation, or joins `unannotatedMethods` | `TestEveryMethodIsPermissionGated` fails. |
| A mixed-audience RPC on the allowlist narrows before it widens | The allowlist test only checks membership; `connected_app_test.go` pins the matrix — a member sees only their own rows, naming another user is `PermissionDenied`, only their own connection is revocable without a permission, another user's needs `connectedApps.revoke` (otherwise `NotFound`), and a checker error fails closed. |
| `GetCurrentUser` stays unannotated | The SPA cannot bootstrap its own permission set. |
| `allUsers` is bound only to `roles/workspaceMember`, and a full replace may not drop that binding | Every authenticated principal, future registrations included, would gain a role nobody granted per user; checked at the API and again in the store. |
| Resolution fails closed: unknown role, role-read error, unevaluable condition ⇒ no permissions | A policy typo would silently widen access instead of narrowing it. |
| A workspace keeps at least one active admin whose binding is unconditional | One `SetWorkspaceIamPolicy` or user deletion would leave the workspace unmanageable. The guard counts any role granting `iam.setPolicy`, not the predefined name, and requires at least one binding whose condition holds and is empty. |
| `role.permissions` is protojson of `store.RolePermissions` | The column is read back with unknown fields discarded, so a wrong shape silently grants nothing. |
| `SetWorkspaceIamPolicy` stays etag-guarded | Concurrent writers would overwrite each other instead of getting `CodeAborted`. |

## Failure modes

| Scenario | Behavior |
| --- | --- |
| Unauthenticated, or authenticated without the permission | `CodeUnauthenticated`, otherwise `CodePermissionDenied` (from `ACLInterceptor.authorize`). |
| A binding names a role that does not exist; or its condition is unevaluable (residual `resource.*`) | An unknown role resolves to an empty set with no error (`iam.Manager.rolePermissions`); an unevaluable condition is rejected at write time and dropped while evaluating at read time. |
| `SetWorkspaceIamPolicy` with a stale etag, or a write that would leave zero unconditional active admins | `CodeAborted` on the etag mismatch, `CodeInvalidArgument` on the admin guard. |
| A custom role is edited | The role snapshot cache is invalidated after the write, so the next check sees the new bundle. |

## Open items

Resource-scoped IAM is shipped for the data-quality surface; its model, invariants and remaining gaps are in [resource-iam.md](resource-iam.md). `frontend/src/lib/permissions.ts` is a hand-maintained mirror of the catalog, guarded by a drift test.

## Where things live

- Catalog: `backend/common/permission/permission.json`, generator `backend/common/permission/gen/main.go`, generated `backend/common/permission/permission_gen.go`; engine `backend/component/iam/manager.go` (`CheckPermission`, `EffectivePermissions`), with principal and group expansion in `backend/utils/member.go`.
- Store: `backend/store/predefined_roles.go`, `backend/store/role.go`, `backend/store/policy.go` (etag and `allUsers` guards), `backend/store/group.go`, `backend/store/iam_grant.go`; schema `backend/migrator/migration/0.1/0006##iam_role.sql` and `backend/migrator/migration/LATEST.sql`.
- API and frontend: `backend/api/v1/{acl_interceptor,iam_service,iam_grant_service,role_service,group_service,connected_app_service,user_service}.go`, the authorizer in `backend/component/iam/{authorizer,scoped_authorizer}.go`; `frontend/src/pages/settings/{RoleManagementPage,IamPage,GroupManagementPage,ConnectedAppsSettingsPage}.vue`, `frontend/src/pages/ConnectedAppsPage.vue`, `frontend/src/lib/permissions.ts`, `frontend/src/api/{role,group,iam,connected-app}.ts`.
- Tests: `backend/common/permission/permission_test.go`, `backend/component/iam/manager_test.go`, `backend/api/v1/acl_interceptor_test.go`, `backend/api/v1/connected_app_test.go`, `backend/store/{predefined_roles,role,policy,policy_all_users}_test.go`, `frontend/src/lib/permissions.test.ts`, end-to-end `backend/test/integration/runner/iam_service_test.go`.
- Gates: `go test ./backend/...` plus `golangci-lint run --allow-parallel-runners`; `make test-integration` for the end-to-end file; inside `frontend/` the order is `pnpm biome:check`, `pnpm lint`, `pnpm i18n` (`pnpm i18n:sort` after locale edits), `pnpm type-check`, `pnpm test run`.
