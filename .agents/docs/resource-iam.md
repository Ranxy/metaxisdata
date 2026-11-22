# Resource-scoped IAM — Reference

> Maintenance reference for the resource-scoped authorization subsystem: the resource model, scoped grants, derived ownership, resource-aware lists and the frontend permission contract. Read it before adding a permission-gated RPC, a scoped grant, a resource resolver or a permission-aware list. The shipped workspace-wide model is in [iam-and-permissions.md](iam-and-permissions.md); this file is the target that model is being replaced by, and the compatibility contract between them.

## The authorization question

Every protected operation answers one question: **may this principal perform this action on this resource?**

- **principal** — a user, group member, service account (the built-in quality agent), or the system.
- **action** — one permission from the catalog (`permission.json`).
- **resource** — one `resource.Ref`: a stable type plus an id. A list or an export answers the same question for a *scope set* and filters at the query.

A workspace-wide permission is the special case where the grant sits on the workspace root and therefore covers every resource. The current `iam.Manager.CheckPermission` answers only that special case; the `iam.Authorizer` interface adds the resource (see [authorizer.go](../../backend/component/iam/authorizer.go)).

## Resource types and the hierarchy

`resource.Type` is the stable name stored in a grant ([resource.go](../../backend/common/resource/resource.go)). A `resource.Ref` is `{Type, ID}`; the workspace's ID is empty, a metadata resource's ID is its GUID, a product resource's ID is its resource id.

```
workspace
├── instance
│   └── database
│       └── schema (optional)
│           └── table / view / external_table
│               ├── column
│               ├── quality_rule ── quality_execution / quality_issue
│               ├── quality_contract
│               ├── quality_suggestion
│               ├── column_profile / column_semantic_label
│               └── manual_sql
├── application ── application_sql / api_key
├── quality_agent ── (scope edges: a set of databases)
├── openlineage run / task / dataset
├── llm_profile / notification_channel / notification_route / idp / role / group / user
└── self (the caller's own notifications, connected apps and profile)
```

**Metadata ancestors are encoded in the GUID.** `resource.MetadataAncestors` parses the GUID and returns the object, its parent and so on to the workspace ([resource.go](../../backend/common/resource/resource.go)). A table has three segments (`instance;database;object`, an engine without schemas) or four (`instance;database;schema;object`); a column has one more. A column's relation ancestor is reported as a table because the registry does not encode the relation kind in the column GUID; scope matching compares metadata ids, not kinds.

**A derived resource resolves to its target.** A quality rule's parent is its target table; an execution or an issue resolves through its rule; a contract, a suggestion, a profile and a label resolve to their target. The parent is not in the resource id, so a resolver in the authorization layer supplies it. `application_sql` has two relations: its home scope is the application; a target-scoped check is an explicit second question only where the product asks it.

**The quality agent's scope is a set of databases**, not a tree parent. It is expressed as one grant per database over the agent's `quality_agent_database` rows, derived from ownership (below) rather than stored as a tree edge.

**Self is a scope kind**, not a resource: the caller's own notifications and connected apps are filtered by the caller's id and never by a grant.

## Grants

A grant binds one role to one member at one resource scope. The scoped-grant store owns an `iam_grant` table ([LATEST.sql](../../backend/migrator/migration/LATEST.sql)):

```sql
CREATE TABLE iam_grant (
    resource_type TEXT NOT NULL,      -- workspace/instance/database/…/application/quality_agent
    resource_id   TEXT NOT NULL,      -- empty for the workspace; the GUID or resource id otherwise
    role          TEXT NOT NULL,      -- roles/{id}
    member        TEXT NOT NULL,      -- users/{id}, groups/{email}, allUsers, the agent's service account
    condition     TEXT NOT NULL DEFAULT '',
    created_by    INT4,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

- **Inheritance is down the resource tree.** A permission is allowed when a grant on the resource, or on any ancestor, names a role that contains it. A database grant therefore covers every table under it; an instance grant covers every database.
- **Grants are additive.** There is no deny in the first model: a scoped grant adds authority, it does not restrict a root grant. The root `workspaceMember` baseline stays the discovery read surface; a scoped `qualityOwner` grant adds write authority on the scope. Restricting a read surface needs a baseline decision or a deny model, and is not part of this design.
- **Grants name users and groups.** The API accepts `users/{id}` and `groups/{email}`; `allUsers` is the workspace read baseline and is rejected as a scoped grant, so a grant cannot turn the every-principal baseline into a write role.
- **The workspace policy stays the root.** The workspace IAM policy remains the editor and the decision for every principal it covers; `workspaceAdmin` and the implicit `workspaceMember` baseline keep their current meaning. An `iam_grant` row adds a role at a resource subtree; a workspace-typed row is accepted and treated as `All`, but the policy API is still the root editor.
- **Conditions name time only.** A binding condition is validated at write time and evaluated against `request.time`; resource attributes in a condition are rejected because a list cannot push them into SQL and a check could not explain them. A condition that does not evaluate fails closed.
- **Cache and invalidation.** A grant read is cached with the role snapshot, keyed by principal and resource, and invalidated by a grant-generation bump on every grant/role write. The cache is advisory: a miss re-reads, a failed read fails closed.
- **Explicit grants are rows; ownership is derived.** An administrator creating a grant writes a row through `IamService.{List,Create,Delete}IamGrant`; the IAM page's scope picker names an instance or one of its databases. Ownership (below) is computed at check time, so changing an agent's owner or scope takes effect without a second source of truth.

## Derived ownership

A derived source answers "which roles does this principal hold on this resource because of a relationship?" and is evaluated beside the explicit grants.

- **The quality agent's owner** holds the `qualityOwner` role on every database in the agent's scope (`quality_agent.owner_principal_id` + `quality_agent_database`). The role contains the data-quality surface: rule get/list/create/update/delete/run, execution get/list, issue get/list/update, suggest, labels.update, profile.run, contract publish, template and code-table reads. The owner is accountable for the agent's databases, so `contracts.publish` is part of the bundle.
- **The quality agent's own service account** holds its role (`qualityAgent`) on the same databases. Its tools then pass the same authorizer as a person's call instead of relying on a product-gate bypass.
- **Derived, not materialized.** The owner and the agent principal are resolved from the agent tables and cached. A materialized copy would need a transaction on every agent create, edit, owner change, scope change and disable; the derived source cannot drift. Explicit administrator grants remain rows.
- **A resource owner is not a general rule.** A rule's `owner_id` labels accountability today; it does not automatically grant edit rights. A future owner-derived source must name the permissions it grants, per resource type.

## Enforcement

`iam.Authorizer` ([authorizer.go](../../backend/component/iam/authorizer.go)) is the one decision point:

```go
Check(ctx, principal, permission, ref) (bool, error)
Scopes(ctx, principal, permission) (ScopeSet, error)
```

- **A single-resource RPC** resolves the resource and calls `Check`. The shipped quality-rule handlers do this explicitly: the create path resolves the target (the draft's target when a suggestion is named), the update and delete paths check the stored rule's target, and the read path maps a denial to `NotFound`. A resolver registry remains the target for a derived resource the request does not name.
- **A create RPC** checks the **target** the new resource will live under (the table for a rule), not the not-yet-existing resource.
- **A batch RPC** checks each item and reports its own error; a denied item is not written.
- **An in-process tool** (the quality agent) carries the same reference in its `Invocation` and uses the same authorizer; the pipeline's permission check is the scoped one.
- **The system** (background runners) is not a user principal and does not pass through the authorizer; its writes are internal. A process that must act as a user takes that user's principal.
- **The ACL interceptor's scoped methods** are listed in `scopedMethods`. For those, it asks the authorizer for the caller's scopes and denies an empty set; the handler then checks the specific target. Every other method keeps the workspace-wide check. Adding a method without its handler check would let a grant on one target reach another.
- **Compatibility.** `iam.WorkspaceAuthorizer` answers every `Check` from `iam.Manager.CheckPermission` and returns `All` for `Scopes`; `iam.ScopedAuthorizer` adds the grant and relation sources on top and is the production implementation.

## List authorization

A list is answered with `ScopeSet`: `All` when a root grant covers everything, `Self` for a self-scoped list, and otherwise the normalized set of scopes the principal holds the permission on ([scope.go](../../backend/common/resource/scope.go)).

- **Filter in SQL, never after.** Each `Find*Message` that backs a list gains a scope field; the query builder ORs the subtree predicates (the metadata GUID prefix condition already exists in `appendGUIDSubtreeCondition`). Pagination, counts and exports then see only authorized rows.
- **An empty scope set is `WHERE FALSE`**, not an unfiltered scan, and returns an empty page. `All` adds no predicate.
- **A narrow request resolves the target directly.** A request that names one target checks that resource and does not compute a full scope set.
- **Self lists** filter by the caller's id regardless of grants; a self row and a granted row are a union only where the product says so.
- **Aggregates and exports use the same predicate** as the matching list. A dashboard count that ignores the scope set is an authorization bug.

## Frontend contract

- `User.permissions` stays a **union summary** for navigation: a permission appears when the principal holds it on any scope. It is not evidence that an action is allowed on the page's target.
- A page that acts on a resource asks the server: a batch `CheckPermissions` keyed by resource, or a `viewer_permissions` field on the resource read. A list may carry a per-row summary when the page renders actions inline.
- `useResourcePermissions(ref)` is the page-facing composable; `hasPermission(permission)` remains for workspace-level settings and navigation.
- The client permission mirror is generated from `permission.json`, not hand-maintained, so a new permission cannot drift from the catalog.

## The data-quality slice

The first scoped path is the data-quality surface, because it already has a target, a queue and a real ownership relationship:

- A `qualityOwner` grant on a database covers every table in it; a rule create, update, run or delete on a table under that database passes, and the same action outside the scope is denied.
- The quality-rule RPCs are the first scoped methods: they check the target, and `ListQualityRules` applies the caller's scope set to the target GUID. The rule, contract, suggestion, execution and issue lists follow the same pattern.
- The quality agent's owner and its own service account are the next relation sources: the owner receives `qualityOwner` on the agent's databases and the service account receives its agent role there, so adoption and drafting run through the same authorizer. The proposal attention keeps its product gate (only the agent's already-queued drafts), but the write itself is authorized rather than bypassing IAM.

## Decisions

- **Additive only.** No deny and no baseline change in the first model; a scoped grant adds authority on a subtree.
- **Derived ownership, cached.** The agent owner and the agent principal come from the agent tables; explicit administrator grants are rows.
- **Metadata ancestors from the GUID; everything else through a resolver.** No global closure table for metadata.
- **The catalog carries the resource.** A permission declares the resource type it is checked against; the frontend mirror is generated from the same source.
- **List authorization is part of the permission, not a post-filter.** An endpoint that cannot filter at the query is not ready for a scoped grant.
- **Conditions stay time-only.** Resource-attribute conditions need a query planner and an explanation model; neither is in this design.
- **No general ReBAC engine.** The product's resources are a tree with a few derived relations; a scoped RBAC over that tree is smaller and easier to explain than a tuple store.
- **Single-workspace root.** The workspace is the root resource. Multi-tenancy would add a workspace column to every table and a tenant root above it; that is not in scope.

## Invariants

| Rule | Breakage |
| --- | --- |
| Every protected RPC resolves a resource or declares itself workspace-scoped | A handler that forgets `Check` is an unguarded write; a boot-time resolver completeness check is the guard. |
| A list applies the scope set in SQL before pagination | Post-filtering leaks counts, breaks page sizes and can return unauthorized rows. |
| An empty scope set matches nothing | An empty scope must not degrade into "no predicate". |
| A read of an unauthorized resource is `NotFound`; a write is `PermissionDenied` | The read path must not reveal existence; the write path names the missing permission. |
| A grant/role write bumps the authorization generation | A stale cache would keep a revoked grant alive. |
| The workspace root keeps one unconditional active admin | The existing admin guard moves with the root grants; a scoped grant cannot lock the workspace. |
| `allUsers` binds only to `roles/workspaceMember` | The baseline must stay the only all-principals grant. |
| A condition that cannot be evaluated fails closed | A policy typo must narrow access, never widen it. |
| A scoped grant never widens a list beyond the requested filter | The query intersects the caller's scopes with the request's filters. |
| The agent's derived grant follows its current scope and owner | A stale materialized copy would grant authority after a scope or owner change. |

## Where things live

- Types, ancestors, scopes: `backend/common/resource/` ([resource.go](../../backend/common/resource/resource.go), [scope.go](../../backend/common/resource/scope.go)).
- Authorizer and compatibility: `backend/component/iam/authorizer.go` and `backend/component/iam/scoped_authorizer.go`; the workspace engine stays in `backend/component/iam/manager.go`; the ACL interceptor and its scoped-method set are `backend/api/v1/acl_interceptor.go` and `backend/api/v1/data_quality_scoped.go`.
- Catalog: `backend/common/permission/permission.json` and its generator; the frontend mirror is generated from it.
- Grants: `backend/store/iam_grant.go`, `backend/api/v1/iam_grant_service.go` and `backend/migrator/migration/LATEST.sql`; the workspace policy remains `backend/store/policy.go`. The `qualityOwner` bundle is in `backend/store/predefined_roles.go`; the IAM page is `frontend/src/pages/settings/IamPage.vue`.
- Resource-aware lists: the `Find*Message` scope fields and the store query builders, starting with `backend/store/quality_rule.go`.
- The quality slice: `backend/api/v1/data_quality_*.go`, `backend/store/quality_agent*.go`, `backend/store/quality_suggestion.go`.
- Frontend: `frontend/src/store/modules/auth.ts`, `frontend/src/lib/permissions.ts`, `frontend/src/composables/`, the target and data-quality pages.
- Tests: `backend/common/resource/*_test.go`, `backend/component/iam/{authorizer,scoped_authorizer}_test.go`, the store query-shape guards, and the scoped-grant integration scenario under `backend/test/integration/`.
- Gates: `go test ./backend/...`, `golangci-lint run --allow-parallel-runners`, `make check-docs`, and the frontend gate (`biome:check`, `lint`, `i18n`, `type-check`, `test run`).
