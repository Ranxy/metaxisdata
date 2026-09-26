# frontend/AGENTS.md

The Vue 3 + TypeScript SPA (Vite, Pinia, vue-router, Tailwind, shadcn-vue). The root [AGENTS.md](../AGENTS.md) still applies; this file wins for anything under `frontend/`.

## Module Boundaries

Where a new module belongs follows from what it depends on, not from its size. From the bottom up:

| Path | Holds | May import |
| --- | --- | --- |
| `src/utils/` | Pure helpers over plain values and proto messages: encodings, formatting, parsing, domain catalogs (`guid`, `datetime`, `metaType`, `error`, `csv`, `dateRange`). No Vue, no Pinia, no `@/api`. | proto types, other `utils/` |
| `src/lib/` | Domain logic without view state: the lineage graph engine, the client-side permission mirror, the OpenLineage payload aggregator, `notify` (the one toast entry point), `cn`. | `utils/`, proto types |
| `src/composables/` | Anything that owns a `ref`, a lifecycle hook or a request: `usePagedFetch`, `useDashboard`, `useErrorHandler`. | `api/`, `lib/`, `utils/`, stores |
| `src/api/` | One module per ConnectRPC service: request building, `listAll` paging, transport errors. No rendering, no cached state. | `lib/`, `utils/`, proto types |
| `src/store/modules/` | Pinia stores shared across pages (app, auth, instance and environment caches). | `api/`, `utils/`, proto types |
| `src/components/` | `ui/` is unmodified shadcn-vue; `<feature>/` holds feature components built from it; `common/` holds the cross-feature ones. | anything below |
| `src/pages/` | Route-level views. They compose; reusable logic belongs in a layer below. | anything below |

Rule of thumb: once a helper grows a `ref`, imports `@/api`, or calls `useI18n()`, it has outgrown `utils/`. When domain logic needs no reactivity, keep it in `lib/` — `src/lib/lineageGraph.ts` and `src/utils/dateRange.ts` are pure functions precisely so they can be unit tested without mounting anything.

`utils/`, `lib/` and `composables/` are the coverage-guarded layer: `vitest.config.ts` sets per-file thresholds (95% lines/functions/statements, 85% branches) and scopes `coverage.include` to those three directories, so a new file there with no test fails the run at 0% instead of being absent from the report. Components and pages are outside the thresholds deliberately — test a concrete interaction, not a percentage.

## i18n

- All user-facing display text goes through vue-i18n in `src/locales/{en-US,zh-CN}.json` — add the key to both locales. ESLint enforces missing and unused keys.
- Keys are kept sorted: run `pnpm --dir frontend i18n:sort` after editing locale files.
- vue-i18n uses single-brace `{name}` placeholders and pipe-separated plurals, unlike react-i18next. A double-brace `{{name}}` renders literally and is flagged.
- `scripts/check-vue-i18n.mjs` (part of `pnpm --dir frontend i18n`) checks cross-locale key and `{name}` placeholder parity. Indirect keys it cannot trace statically — template-literal or variable keys such as `t(messageKey)` — must be listed in its `DYNAMIC_PREFIXES`.

## Frontend Workflow

After any frontend change, run the gate in this order — exact commands below:

1. **Format + lint + imports** — `biome:check`
2. **Lint** — `lint` (applies `--fix`)
3. **i18n** — `i18n`, plus `i18n:sort` if you touched locale files
4. **Type check** — `type-check`
5. **Test** — `test run`

## Commands

```bash
pnpm --dir frontend i                 # install dependencies
pnpm --dir frontend dev               # dev server; http://localhost:3000, proxies /v1 and /metaxisdata.v1 to localhost:8080

pnpm --dir frontend biome:check       # format + lint + organize imports (src/, excluding generated src/types/proto-es/)
pnpm --dir frontend lint              # ESLint (Vue + i18n rules), applies --fix
pnpm --dir frontend i18n              # missing/unused keys, cross-locale parity, sort check
pnpm --dir frontend i18n:sort         # rewrite locale catalogs with recursively sorted keys
pnpm --dir frontend type-check
pnpm --dir frontend test              # watch mode
pnpm --dir frontend test run
pnpm --dir frontend test:coverage

# CI forms: coverage plus the json reports the metrics script reads, and a type
# check that also reports peak heap, file count and build time
pnpm --dir frontend test:ci
pnpm --dir frontend type-check:diagnostics
pnpm --dir frontend metrics           # render this machine's metrics block

pnpm --dir frontend build             # production build
```

## Testing

Vitest with jsdom (`vitest.config.ts`), colocated with source as `*.test.ts(x)`; tests for the `scripts/*.mjs` tooling run in the node environment. Coverage is scoped to the shared layer with per-file thresholds — see "Module Boundaries".

## Conventions

- Prefer shared shadcn-vue primitives from `src/components/ui/` over hand-rolled markup.
- Call the API through the ConnectRPC clients in `src/api/client.ts`, not ad-hoc fetch.
- `src/types/proto-es/` is buf output — regenerate with `cd proto && buf generate`, never hand-edit.
- `scripts/` holds the tooling behind the i18n, metrics and UI-audit package scripts.
