# Plan: Environment Management (Dropdown + Inline Creation + Settings Page)

> **Status: implemented.** Landed: `EnvironmentService` (List/Create/Update/
> Delete) in `backend/api/v1/environment_service.go`, the locked store mutations
> in `backend/store/environment.go` + `environment_mutation.go`, the
> `EnvironmentSelect` combobox, the Settings → Environments page, dynamic
> environment labels/colors/filter options, hermetic tests on both sides, and a
> real-server integration test
> (`backend/test/integration/runner/environment_service_test.go`) covering
> create → assign to an instance → delete-blocked-while-in-use → delete.
> **Divergences from the decisions below:** the ACL guard
> (`TestEveryMethodIsPermissionGated`) requires every read path to be gated, so
> `ListEnvironments` got a new catalog entry `metaxisdata.environments.list`,
> granted to the `workspaceMember` baseline; and the non-ASCII id fallback
> generates `env`, `env-1`, … rather than random hex, so the derivation stays
> deterministic and testable. A read-only `Environment.instance_count` was also
> added so the settings page can show in-use usage without a count query per row.

## TL;DR

"Environment" is **not** a database enum. It is already a configurable list stored
in the `setting` table under `SettingName.ENVIRONMENT` (`value 15`) as JSONB
(`proto/store/store/setting.proto` → `EnvironmentSetting.{id, title, tags, color}`),
seeded with `test` / `prod` on first startup (`backend/server/init.go`). What is
missing is any way to read or write that list over the API, and any UI that
consumes it. Today the instance forms take **free text**; the client prepends
`environments/`, and `CreateInstance` / `UpdateInstance` reject an unknown value
with `CodeNotFound: environment "..." not found` at submit time
(`backend/api/v1/instance_service.go`).

This plan exposes the list as a resource-shaped `EnvironmentService`
(List / Create / Update / Delete), adds the store mutations behind it, replaces
the free-text field with a searchable `EnvironmentSelect` combobox whose empty
state offers an inline "create" dialog, and adds a **Settings → Environments**
management page for rename / recolor / delete. Every place that currently
hardcodes environment ids or guesses colors (`AdvancedSearchBar.vue`,
`DatabaseManagementPage.vue`) is switched to the real list.

**No database migration is required.** The storage shape already exists.

## Decisions

Settled with the reporter before writing this plan:

- **Both entry points.** Inline create from the picker (the primary ask) **and** a
  Settings → Environments page for rename / recolor / delete.
- **Write access is `metaxisdata.settings.update`.** A member without it sees the
  picker read-only with a hint to contact an admin. Reading the list needs its
  own catalog entry (`metaxisdata.environments.list`, granted to the member
  baseline) because the ACL guard refuses unannotated reads and no existing
  permission covers the instance form, the database page and the settings page
  at once.
- **The create dialog asks for a display name only.** The server derives an
  immutable id from the name and assigns a color from a preset palette. Users
  never type an id.
- **Environment stays required** on instances (`environmentRequired` validation
  is kept, only the input widget changes).
- **Stored `tags` are preserved** across edits but are not surfaced in the UI in
  this iteration (the field is carried in the API for round-trip fidelity).

## Current state (what exists, what is missing)

| Layer | Exists | Missing |
| --- | --- | --- |
| Storage | `setting` row `ENVIRONMENT`, JSONB `EnvironmentSetting`; seed `test`/`prod` | — |
| Store | `GetEnvironmentByID`, `GetEnvironmentSetting` | create/update/delete, in-use count, safe mutation |
| API | instance create/update validates existence | any environment RPC; `SettingService` exposes only workspace profile + debug config |
| Frontend | free-text `AppInput` in both instance forms | picker, create flow, settings page, dynamic filter options, title/color display |
| Display | id shown (`getEnvironmentId` strips `environments/`); badge color guessed from the string | title + stored color |

## Data model

No schema change. Environments stay in `setting.value`, JSONB under
`SettingName_ENVIRONMENT`:

```jsonc
{ "environments": [ { "id": "prod", "title": "Prod", "tags": {}, "color": "red" } ] }
```

- `id` is the value stored in `instance.environment` / `db.environment`
  (raw, no `environments/` prefix — see `backend/store/instance.go`). It is
  **immutable** after creation so existing references never dangle.
- `title` is what users see. Renaming a title is safe; it touches no reference.
- `color` is a bounded preset key (`slate`, `blue`, `green`, `amber`, `orange`,
  `red`, `violet`, `pink`). Empty means "let the server pick". Kept as a
  `string` in proto (the store field is already a string); the server validates
  membership in the palette.

`db.environment` is still never written directly — databases inherit the
instance's environment as `effective_environment`.

## Proto / API

New `proto/v1/v1/environment_service.proto` (package `metaxisdata.v1`). A
dedicated service rather than extending `SettingService`, because environments
are addressed as resources (`environments/{id}`) and because the inline-create
flow is a resource create, not a whole-blob settings update — a blob
update would force read-modify-write on every client and lose updates.

```proto
message Environment {
  string name  = 1;  // OUTPUT_ONLY, "environments/{id}"
  string title = 2;  // REQUIRED on create; the user-facing name
  string color = 3;  // OPTIONAL; one of the preset palette keys
  map<string, string> tags = 4;  // OPTIONAL; preserved, not surfaced in the UI yet
  int32 instance_count = 5;      // OUTPUT_ONLY, populated by ListEnvironments only
}
```

| RPC | HTTP | Permission | Audit | Notes |
| --- | --- | --- | --- | --- |
| `ListEnvironments` | `GET /v1/environments` | `metaxisdata.environments.list` | no | Paginated with the existing `parseLimitAndOffset` / `paginateInMemory` helpers so it matches `ListLLMProviderProfiles`. Readable by every member because the instance forms and the DB filter need it. Populates `instance_count`. |
| `CreateEnvironment` | `POST /v1/environments` | `metaxisdata.settings.update` | yes | Body carries `environment.title` (and optional `color`). Server derives `id`, rejects a duplicate title (`CodeAlreadyExists`), returns the created resource. |
| `UpdateEnvironment` | `PATCH /v1/environments/{environment.name}` | `metaxisdata.settings.update` | yes | `update_mask` supports `title`, `color`, `tags`. `name` (id) is not updatable. |
| `DeleteEnvironment` | `DELETE /v1/environments/{environment.name}` | `metaxisdata.settings.update` | yes | Rejected with `CodeFailedPrecondition` while any live instance references it, with the count in the message. |

ID derivation (server-side, immutable): lowercase the title, replace runs of
anything outside `[a-z0-9]` with `-`, trim leading/trailing `-`. If the result is
empty (e.g. an all-CJK title such as "预发布"), fall back to `env`, suffixed with
a counter (`env-1`, `env-2`, …) until it is free — deterministic, so it is
unit-testable. Because the UI displays `title`, the generated id is
internal-only. Duplicate detection is on the derived id **and** the title,
case-insensitively.

Registration: add `NewEnvironmentService` to `backend/server/grpc_routes.go`
alongside the other handlers — connect handler map, `grpcreflect` static
reflector list, and the REST gateway `RegisterEnvironmentServiceHandler` call.
Then `buf format -w proto && buf lint proto && cd proto && buf generate` and
commit the regenerated `backend/generated-go/`, `frontend/src/types/proto-es/`
and `proto/gen/grpc-doc/` output.

## Store layer

`backend/store/environment.go`:

- Keep `GetEnvironmentByID` (reads through the setting cache).
- Add `CountInstancesByEnvironment(ctx, id) (int, error)` —
  `SELECT count(*) FROM instance WHERE environment = $1 AND deleted = false`.
  Used by Delete and by the settings page's in-use badge.
- Add `CreateEnvironment` / `UpdateEnvironment` / `DeleteEnvironment`.

Each mutation must avoid the lost-update race of a naive read-modify-write
against the JSONB blob. Do it in one transaction:

```
BEGIN
SELECT value FROM setting WHERE name = 'ENVIRONMENT' FOR UPDATE
<unmarshal> <apply mutation> <marshal>
INSERT ... ON CONFLICT (name) DO UPDATE ...
COMMIT
then refresh s.settingCache with the new value
```

`UpsertSetting` already owns the upsert + cache refresh; factor a tx-scoped
helper or extend it so the mutation and the cache update stay consistent. Keep
the row lock so two concurrent creates cannot drop one another.

`backend/store/environment_mutation.go` (pure, hermetic):

- `slugifyEnvironmentID(title) string`
- `appendEnvironment(setting, title, color) (*Environment, error)`
- `applyEnvironmentUpdate(setting, id, patch, updateMask) (*Environment, error)`
- `removeEnvironment(setting, id) error`
- `pickEnvironmentColor(environments, id) string`

These carry the id/title uniqueness rules and the palette fallback, so they get
fast guard tests with no database (matching `backend/store/setting_test.go`).

## Backend service

`backend/api/v1/environment_service.go`, mirroring the shape of
`setting_service.go` / `llm_service.go`:

- Map store rows to `v1pb.Environment` (`name = common.FormatEnvironment(id)`).
- `ListEnvironments`: parse the page token, call the store, `paginate`.
- `CreateEnvironment`: validate `title` is non-empty; call the store; map
  duplicate errors to `CodeAlreadyExists`, bad color/empty title to
  `CodeInvalidArgument`.
- `UpdateEnvironment`: parse the resource name with
  `common.GetEnvironmentID`; reject an empty/absent `update_mask`; unknown mask
  paths → `CodeInvalidArgument` (same style as `UpdateInstance`).
- `DeleteEnvironment`: parse the id, `CountInstancesByEnvironment` first, and
  return `CodeFailedPrecondition` when non-zero.

The instance create/update existence guard stays exactly as it is — it is the
final backstop for API clients that bypass the picker.

## Frontend

### API + state

- `frontend/src/types/proto-es` — regenerated.
- `frontend/src/api/environment.ts` — `listEnvironments`, `createEnvironment`,
  `updateEnvironment`, `deleteEnvironment`; add `environmentClient` to
  `frontend/src/api/client.ts`.
- `frontend/src/store/modules/environment.ts` (Pinia, following `auth.ts`):
  caches the list, `ensureLoaded()` for pages that only need to resolve a title,
  `refresh()` after a mutation, and a getter `titleOf(name)` falling back to the
  id when the list has not loaded or the value is unknown. Also
  `colorOf(name)` with the same fallback.

A shared store is what keeps the instance table, the DB page, the search bar and
the settings page consistent without four independent fetches.

### `EnvironmentSelect` combobox

New `frontend/src/components/common/EnvironmentSelect.vue`, composed from the
existing shadcn-vue primitives (`Popover` + `Command`, the same pair the
`AdvancedSearchBar` filter already uses) plus `AppModal` for the create dialog.

- `v-model` carries the **full resource name** `environments/{id}`, so the
  instance forms stop doing their own prefix stripping/prepending.
- Trigger shows a color dot + title; placeholder otherwise.
- Typing filters `title` and `id`.
- When the query is non-empty and does not case-insensitively equal an existing
  title, the list ends with `Create "<query>"`.
- Choosing it opens a small dialog prefilled with the query and a single
  **name** field; submit calls `createEnvironment`, refreshes the store, and
  selects the new environment.
- The create item is hidden without `metaxisdata.settings.update`; instead a
  muted row tells the user to ask an admin to add it.
- If the current value is not in the list (legacy/dangling id), render it as a
  selected "unknown" entry rather than silently dropping it — otherwise editing
  an old instance could clear or rewrite its environment.

### Wiring the pages

- `InstanceManagementPage.vue` (create form) and `InstanceDetailPage.vue` (edit
  form): replace the `AppInput` with `EnvironmentSelect`; drop the manual
  `environments/` prefixing in the submit handlers. `environmentRequired`
  validation stays.
- Display: `InstanceManagementPage.vue`, `InstanceDetailPage.vue` and
  `DatabaseManagementPage.vue` show `titleOf(environment)` instead of the raw
  id; `getEnvironmentVariant` (substring guessing) is replaced by a static
  color-key → Tailwind class map. Tailwind needs literal class names, so the
  map must list them explicitly.
- `AdvancedSearchBar.vue`: delete the hardcoded `environmentOptions`
  (dev/test/staging/prod) and build the options from the store (`value:
  environments/{id}`, `label: title`). The CEL filter value is unchanged.

### Settings → Environments page

- `frontend/src/pages/settings/EnvironmentSettingsPage.vue`: table of
  environments (color dot, title, id, in-use count) with create / rename /
  recolor / delete. Delete is disabled with an in-use tooltip when the count is
  non-zero. Read-only when the caller lacks `metaxisdata.settings.update`,
  matching the `GeneralSettingsPage.vue` `canUpdate` pattern.
- Route `/settings/environments` in `frontend/src/router/index.ts`
  (`permission: "metaxisdata.settings.get"`).
- Sidebar entry under Settings in `frontend/src/components/layout/AppSidebar.vue`
  (same `permission`), e.g. an icon such as `Globe`.
- i18n keys for the picker, create/rename dialogs, the settings page and the
  sidebar go into **both** `frontend/src/locales/en-US.json` and
  `frontend/src/locales/zh-CN.json` (ESLint enforces missing/unused keys).

## Tests

- **Store (hermetic):** `backend/store/environment_mutation_test.go` — slug
  derivation including the all-CJK fallback, duplicate id/title rejection,
  rename not touching the id, color palette fallback, tags preserved across a
  title update, delete.
- **API:** `backend/api/v1/environment_service_test.go` — permission/mask
  validation and error-code mapping where testable without a database; the
  handler paths for delete-in-use and duplicate title.
- **Frontend (Vitest):** the picker's create-vs-select decision (exact title
  match selects, otherwise offers create), title/color fallback for unknown
  ids, and the slug/format helpers.
- **Integration:** extend `backend/test/integration` with a create → assign to
  an instance → rename → delete-blocked-while-in-use → delete flow, since this
  touches store SQL and server wiring.

## Delivery order

1. Store mutations + pure helpers + hermetic tests (no proto dependency).
2. Proto + regenerate + `EnvironmentService` + registration.
3. Frontend API/store + `EnvironmentSelect`, wire the two instance forms.
4. Display (titles/colors) + dynamic search-bar options.
5. Settings page + route + sidebar + i18n.
6. Integration test + `golangci-lint`, `pnpm biome:check`, `pnpm lint`,
   `pnpm type-check`, `pnpm test run`.

## Deferred / explicitly out of scope

- Editing `tags` from the UI (the field round-trips but has no screen).
- Moving environments out of the settings blob into a dedicated table. Possible
  later if the list needs its own permissions or ordering; not needed now.
- A new `metaxisdata.environments.create` permission. If non-admins should ever
  self-serve environments, that is a follow-up that adds the catalog entry and
  widens the member baseline.
- Per-environment policy (e.g. only certain roles may assign `prod`).
