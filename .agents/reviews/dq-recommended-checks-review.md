# P1-0 (deterministic recommended checks) — adversarial review

Reviewer: an independent adversarial review ran from a fresh context, read-only, on
branch `feat/dq-recommend-checks` (commits `dc96e6b`..`5ed3a31`) in
`.agents/worktrees/feat-dq-recommend-checks`, base `62c3eed`.

Reviewed the whole diff, the P1-0 row of
[`data-quality-competitive-gap-analysis.md`](data-quality-competitive-gap-analysis.md),
D-78/D-79/I-45/I-46 and §3.6–§3.8/§6/§7 of
[`.agents/docs/data-quality.md`](../docs/data-quality.md), `docs/data-quality.md`, the
nested `AGENTS.md` files, and the existing decision set (D-30/I-12 pending-only,
D-58/D-63 identity, D-69 scheduling). Existing decisions were not re-litigated; where
code or docs contradicted them, that is reported as a defect.

## What was exercised

- `git diff 62c3eed..HEAD` read in full; `git status --porcelain` clean before and after
  the review. Probes used `go test -overlay` with test files outside the repository.
- `go build ./...`; `go test ./backend/component/dataquality ./backend/api/v1 ./backend/store`;
  `buf format -d` / `buf lint` / `buf generate` idempotent.
- `go test -tags=integration ./backend/test/integration/runner -run TestDataQuality`
  against real PostgreSQL + MySQL (testcontainers), including the new recommendation
  scenario and the existing label/suggestion-center scenarios.
- Frontend: targeted vitest, `pnpm i18n`, `vue-tsc`.
- Overlay probes: freshness lag at 10/20/30 days; a multi-byte value cut by the 64-byte
  display bound; a PostgreSQL `jsonb` enumeration; a per-type budget that cuts candidates.
- Not run: `pnpm audit:ui` (no live app/CDP endpoint in this environment; no layout
  budget changed by this diff).

## Findings and closure

| # | Severity | Finding | Status |
| --- | --- | --- | --- |
| 1 | major | The freshness bound fell through to 30 days when twice the observed lag exceeded it, so a table whose newest value was ~30 days old got a bound below its own observation and failed immediately; the rationale still claimed "twice the observed lag". | Fixed in `ea01ff1`: no freshness candidate when `2 × lag > 30d` (observed lag > 15d), and the rationale says "rounded up". New test `TestRecommendChecksFreshnessBoundKeepsHeadroom` covers 7/10/20 days. |
| 2 | major | The accepted-values truncation guard compared a character length (`MAX(CHAR_LENGTH)`) with the 64-**byte** display bound and was skipped when the column had no length function, so a multi-byte or long `jsonb` value could be queued as a cut spelling no row matches. | Fixed in `059ce9f` + `ea01ff1`: the store profile marks a truncated top value (`ProfileValue.truncated`, JSONB-only, no migration), the profiler sets it from the difference between the full and stored rendering, and the pass drops a candidate for a truncated or binary spelling. Tests: `TestProfileTopValuesMarksTruncation`, `TestRecommendChecksAcceptedValuesSkipsUnsafeSpellings`. |
| 3 | minor | Cross-pass precedence was a read-then-write TOCTOU: the upsert rewrote `source` unconditionally, so a label/recommendation interleave could still put default parameters over observed ones. | Fixed in `ea01ff1`: the rule is enforced by the upsert's `ON CONFLICT … DO UPDATE … WHERE` (same source, or RECOMMENDATION over PROFILE); a refused refresh returns `(nil,false,nil)` and all three passes count it as `skipped_pending`. Guarded by a query-shape test. |
| 4 | minor | A request that scoped `columns` could still get a referential candidate whose child column was outside the scope. | Fixed in `ea01ff1`: referential candidates are filtered to the scope; the proto documents that table-level checks describe the relation as a whole. Test `TestRecommendChecksReferentialScope`. |
| 5 | minor | At exactly the 50% null-rate cap the widened bound equalled the observation (zero headroom) while the rationale claimed 1.5×. | Fixed in `ea01ff1`: the pass skips at `rate >= 50`; test `TestRecommendChecksNullRateCap`. |
| 6 | minor | `skipped_columns` still documented "the columns a label pass left out"; the recommendation reuses it with the different meaning "no candidate derivable", and the client toast read "Skipped columns". | Fixed in `059ce9f` (proto comments for both generation paths) and `c55f4e4` (neutral wording "Columns with no proposal"). |
| 7 | minor | A failed workspace-settings read silently hid the AI label action (`catch → llmEnabled=false`). | Fixed in `c55f4e4`: the read failure keeps the action available; the server refuses with an actionable error when the model is really off. |
| 8 | nit | Range bounds are parsed/rendered as float64, so integers above 2^53 can be off by one ulp. | Documented in §7 #27; the 10% span slack dominates the error, and tiny spans are dropped. |
| — | — | Doc mismatches: D-78's "rounded up" and `RuleTypeForTarget`-filtering claims, §3.7's pre-separation entry-point paragraph, §3.8's `source` enumeration, §6's filtering wording, and the operator tolerance paragraph (no 15-day/50% ceilings). | Fixed in `9e7faba`. |

## Post-fix verification

- Must-fix and recommended items above re-verified by the implementing side:
  `go build ./...`, `go test ./backend/...`, `golangci-lint` on the changed packages
  (0 issues in touched files), `buf format/lint/generate`, frontend `biome:check` /
  `lint` / `i18n` / `type-check` / `test run` (166 files / 1691 tests).
- The cross-pass path that was only covered by a pure-function test is now proven end to
  end with a **real model** (opt-in scenario, `5ed3a31`): recommendations keep their
  payload and source when the label pass runs; an accepted format label lets the
  recommendation replace the label pass's default-pattern draft in place; a later label
  pass leaves the replacement alone. It passed in 5.45 s, and the full
  `TestDataQuality` integration suite with the real provider available passed in 52.7 s.
- The scenario skips without `METAXISDATA_INTEGRATION_LLM_API_KEY`; the credential is
  read from the environment only and never written to the repository or the output.

## Verified good

- Identity and queue mechanics: one PENDING row per `(target_guid, target_type,
  signature)` from the partial unique index, upsert convergence, `RuleSignature`
  ignoring thresholds but keeping semantic discriminators, supersede-on-fact and
  decided-identity suppression.
- The new `PendingSource` column of the signature-state read is correct given the
  partial unique index (at most one pending row), and both the store and v1 enums gained
  value 3 with no mapping hole (list filter, conversion, frontend source options, URL
  round-trip, i18n).
- Determinism and validation: every generated candidate passes `RequireTarget` +
  `ValidateConfig` (and `ValidateRule` for referential checks) on the fixture target;
  the pass is a pure function of its inputs; `skipped_columns` is computed from all
  derived candidates, so a column whose candidate lost the per-type budget is not
  reported as skipped.
- No migration, no DB CHECK on `quality_suggestion.source`, audit/permission are the
  proto annotations the interceptor reads dynamically, and the RPC touches no target
  database and no LLM quota (default unary deadline is appropriate).
- The label-pass change is scoped to cross-pass identities; the pass still refreshes its
  own pending drafts, and label suggestions use a different signature namespace.

## Residual risks (documented, not blocking)

- A real LLM run is opt-in and assumes the model labels the seeded email column as a
  format type; the scenario asserts that assumption explicitly when it runs.
- No real-interleave concurrency test: the precedence rule is enforced by the database
  but the race itself is not reproduced in an integration scenario.
- Integration coverage for the generator's capability paths is MySQL-only; PostgreSQL
  `jsonb`/`MSSQL` gates are pinned by unit tests and the capability tables.
- Freshness chooses the "event-time" column heuristically (label > name > newest value)
  and a labeled format pattern is not checked against historic values; both are recorded
  in §7 #27.

## Verdict

`fix-then-ship`. Both major findings and all recommended minors were fixed and covered
by tests; the remaining items are documented boundaries. Ship.
