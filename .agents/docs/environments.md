# Environments

An environment is a workspace-scoped catalog entry that instances are tagged with, not a database enum: the catalog lives in the `ENVIRONMENT` setting, seeded with `test` and `prod` on first start. Instances store the raw id, and databases may carry their own. The RPC surface, field shapes and id-derivation rule live in `proto/v1/v1/environment_service.proto` and `backend/store/environment_mutation.go`.

## Selection rule

An instance either names an existing environment or carries none, and a named id must exist. A database's environment is an optional override; the effective environment is `COALESCE(db.environment, instance.environment)`, which the database list filters use, while the quality overview's coverage breakdown groups by the instance's own environment. The instance forms still require a selection (`environmentRequired` is a UI check) while the API accepts an absent environment, and renaming an entry changes only its display title.

## Decisions

| Decision | Why |
| --- | --- |
| Writes need `metaxisdata.settings.update`; reads need `metaxisdata.environments.list` in the member baseline | Environments are workspace settings, not a resource family, and the ACL guard refuses an unannotated read; no existing permission covers the instance forms, database page and settings page at once. |
| The id is derived from the title and immutable; the server derives a default palette color | Stored references name the raw id, so only the title may change; users never type an id, and a palette-derived color always has a badge style. |
| `tags` round-trips but has no editor | The update mask accepts it; no UI needs it yet. |
| An environment referenced by a live instance cannot be deleted | Otherwise the stored id no longer resolves. |

## Invariants

- The id never changes after creation: stored references would not resolve.
- Every write locks the `ENVIRONMENT` row before rewriting the JSONB blob; otherwise two writers overwrite each other's whole blob and one addition is silently lost.
- The color is a server-validated palette key, because the frontend maps keys to style classes.
- Deletion counts live instances (excluding soft-deleted ones) in the service, not the store; a new store caller must repeat the count.
- `instance_count` is filled by `ListEnvironments` only.

## Failure modes

| Scenario | Behavior |
| --- | --- |
| Duplicate title (case-insensitive) | `AlreadyExists`. |
| Empty title, unknown color, or an unsupported update path | `InvalidArgument`. |
| Deleting a referenced environment | `FailedPrecondition`, naming the live instance count. |
| A caller without `settings.update` | Selection works, creation is disabled, and the settings page is read-only. |
| A stored id no longer in the catalog | The UI shows the raw id; the value is not cleared. |

## Where it is used

Instance metadata and forms; database inheritance and the instance/database list filters; the data-quality overview's coverage breakdown by environment; the metadata MCP tools.

## Where things live

`backend/api/v1/environment_service.go`, `backend/store/environment.go`, `backend/store/environment_mutation.go`, registration in `backend/server/grpc_routes.go` and seeding in `backend/server/init.go`. Frontend: `frontend/src/components/environment/`, `frontend/src/store/modules/environment.ts` and `frontend/src/pages/settings/EnvironmentSettingsPage.vue`. Tests: `environment_mutation_test.go`, `environment_service_test.go`, the frontend store/color tests and the integration journey under `backend/test/integration/runner/`.
